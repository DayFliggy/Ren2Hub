"""仅向已验证的候选 Redis 注入合成用户缓存故障，随后经真实 HTTP 验证。"""
import time
from concurrent.futures import ThreadPoolExecutor

from candidate import execute
from .common import RequirementError, acceptance_target, require, require_charge
from .routing_cases import wait_received


def user_cache(e, command, *arguments):
    require(command in ('HGET', 'HSET', 'HDEL', 'DEL')
            and (command != 'DEL' or not arguments)
            and type(e.user_id) is int and e.user_id > 0,
            'invalid_cache_fault_operation')
    target = acceptance_target()
    # REDISCLI_AUTH already belongs to this temporary container. Never put a
    # password, session token or cache MAC in the command line or the report.
    result = execute(['sudo', '-n', 'docker', '--host', 'unix:///var/run/docker.sock',
                      'exec', target['container_ids']['redis'], 'redis-cli', '--raw', command,
                      'user:' + str(e.user_id), *arguments])
    return result.stdout.decode().strip()


def clear_settled_user_cache(e, before):
    """只在请求结束且预扣已结算/退款后删除本轮用户哈希。"""
    e.zero()
    deadline = time.monotonic() + 10
    while True:
        after = e.balances()
        charged = after['wallet_used'] - before['wallet_used']
        try:
            # 清理也用于失败路径，按实际已记用量核对钱包和 Token，不能猜请求成功。
            require_charge(before, after, charged)
            break
        except RequirementError:
            require(time.monotonic() < deadline, 'redis_cleanup_unsettled_reservation')
            time.sleep(.05)
    user_cache(e, 'DEL')
    require(user_cache(e, 'HGET', 'Quota') == '', 'redis_fixture_cache_not_cleared')


def real_redis_repair(e):
    e.route(['a', 'b'])
    e.configure('a', max_concurrency=1)
    e.check(['a'])
    e.sim.scenario('a', steps=[{'kind': 'hold'}, {'kind': 'success'}])
    before, cursor = e.balances(), e.sim.cursor()
    try:
        with ThreadPoolExecutor(max_workers=1) as pool:
            held = pool.submit(e.request)
            try:
                wait_received(e, cursor, 'a')
                reserved = e.balances()
                require(before['wallet'] - reserved['wallet'] == 1000
                        and before['token'] - reserved['token'] == 1000
                        and not held.done(), 'redis_fixture_preconsume_not_observed')
                reserved_balance = int(user_cache(e, 'HGET', 'Quota'))
                # 直写模式下 Redis 与 DB 同步，必须制造不同值才能识别错误的 DB 覆盖。
                sentinel = min(reserved_balance, reserved['wallet']) // 2
                require(sentinel > 2000 and sentinel != reserved['wallet'], 'redis_invalid_quota_sentinel')
                require(user_cache(e, 'HGET', 'Role') == '1', 'unexpected_fixture_user_role')
                user_cache(e, 'HSET', 'Role', '100', 'MaxConcurrency', '99', 'Quota', str(sentinel))
                require(e.request()[0] == 200, 'redis_metadata_repair_request_failed')
                e.settled(reserved, 1000)
                require(user_cache(e, 'HGET', 'Role') == '1'
                        and user_cache(e, 'HGET', 'MaxConcurrency') == '0', 'redis_metadata_not_repaired')
                require(user_cache(e, 'HGET', 'Quota') == str(sentinel - 1000),
                        'redis_repair_lost_inflight_reservation')
                require(not held.done(), 'redis_fixture_barrier_released_early')
            finally:
                # 先禁止尚未到达的请求进入下一批屏障，再放行已挂起请求。
                try:
                    e.sim.scenario('a', steps=[{'kind': 'success'}])
                finally:
                    e.sim.release('a')
                require(held.result(timeout=25)[0] == 200, 'redis_held_request_failed')
        e.settled(before, 2000)
        e.zero()

        # 此时没有在途预扣；非法计数应从最新 DB 水合，再扣本次真实请求的 1000。
        invalid_before = e.balances()
        user_cache(e, 'HSET', 'Quota', 'not-an-integer')
        require(e.request()[0] == 200, 'redis_noninteger_repair_request_failed')
        invalid_after = e.settled(invalid_before, 1000)
        e.zero()
        require(user_cache(e, 'HGET', 'Quota') == str(invalid_after['wallet'])
                and invalid_after['wallet'] == invalid_before['wallet'] - 1000,
                'redis_noninteger_quota_not_refilled_from_db')

        for fault in ('unsigned', 'expired'):
            if fault == 'unsigned':
                user_cache(e, 'HDEL', 'CacheMAC')
            else:
                user_cache(e, 'HSET', 'CacheExpiresAt', '1')
            e.check(['a'])
            require(bool(user_cache(e, 'HGET', 'CacheMAC'))
                    and int(user_cache(e, 'HGET', 'CacheExpiresAt')) > time.time() * 1000,
                    'redis_' + fault + '_cache_not_repaired')
    finally:
        clear_settled_user_cache(e, before)
    return {'engine': 'isolated real Redis',
            'faults': ['metadata_tamper', 'noninteger_counter', 'unsigned', 'expired'],
            'inflight_reservation_preserved': True,
            'quota_sentinel': sentinel, 'held_db_preconsumed': before['wallet'] - reserved['wallet'],
            'noninteger_counter_db_refilled': True, 'fixture_cache_cleared': True,
            'limits': '数据库不可用、会话撤销及 Token 缓存篡改由另列 Go/三库合同覆盖。'}
