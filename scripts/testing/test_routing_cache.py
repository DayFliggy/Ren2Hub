"""用分离的 DB/Redis 余额及真实线程屏障保护缓存修复验收的判别能力。"""
import threading
import time
from concurrent.futures import ThreadPoolExecutor
from types import SimpleNamespace
import unittest
from unittest.mock import patch

from contracts import routing_cache
from contracts.common import RequirementError, require, require_charge
from contracts.routing_cases import wait_received


class RepairFixture:
    """只模拟原生预扣/结算边界；可切换错误修复策略，不调用 Docker 或 HTTP。"""
    user_id = 73

    def __init__(self, *, overwrite_valid=False, stale_refill=False, fail_on=None):
        self.lock = threading.RLock()
        self.released = threading.Event()
        self.refunded = threading.Event()
        self.active = 0
        self.hold_next = False
        self.db = {'wallet': 100000, 'wallet_used': 0, 'token': 100000, 'token_used': 0}
        self.cache = {}
        self.overwrite_valid = overwrite_valid
        self.stale_refill = stale_refill
        self.fail_on = fail_on
        self.events, self.requests, self.repaired = [], [], []
        self.sim = SimpleNamespace(scenario=self.scenario, release=self.release,
                                   cursor=lambda: len(self.requests), records=self.records)

    def route(self, names):
        require(names == ['a', 'b'], 'fixture_route')

    def configure(self, name, **values):
        require(name == 'a' and values == {'max_concurrency': 1}, 'fixture_channel')

    def scenario(self, name, **value):
        require(name == 'a', 'fixture_scenario_channel')
        with self.lock:
            self.hold_next = value.get('steps', [{}])[0].get('kind') == 'hold'
            if self.hold_next:
                self.released.clear()

    def release(self, name):
        require(name == 'a', 'fixture_release_channel')
        self.events.append('release')
        self.released.set()

    def records(self, cursor):
        with self.lock:
            return [dict(row) for row in self.requests[cursor:]]

    def balances(self):
        with self.lock:
            return dict(self.db)

    def settled(self, before, amount):
        after = self.balances()
        require_charge(before, after, amount)
        return after

    def zero(self):
        with self.lock:
            require(self.active == 0, 'fixture_requests_still_active')

    def user_cache(self, experiment, command, *args):
        require(experiment is self, 'fixture_foreign_user')
        with self.lock:
            if command == 'HGET':
                return self.cache.get(args[0], '')
            if command == 'HSET':
                self.cache.update(zip(args[::2], args[1::2]))
                return '1'
            if command == 'HDEL':
                for name in args:
                    self.cache.pop(name, None)
                return '1'
            if command == 'DEL':
                require(not args and self.active == 0, 'fixture_deleted_during_request')
                require_charge({'wallet': 100000, 'wallet_used': 0, 'token': 100000, 'token_used': 0},
                               self.db, self.db['wallet_used'])
                self.events.append('delete')
                self.cache.clear()
                return '1'
            raise AssertionError('unexpected Redis command')

    def repair(self):
        if not self.cache:
            self.cache['Quota'] = str(self.db['wallet'])
            fault = 'cold'
        elif self.cache.get('Role') != '1':
            fault = 'metadata_tamper'
        elif not self.cache.get('CacheMAC'):
            fault = 'unsigned'
        elif int(self.cache['CacheExpiresAt']) < time.time() * 1000:
            fault = 'expired'
        else:
            fault = None
        try:
            int(self.cache['Quota'])
        except ValueError:
            fault = 'noninteger_counter'
        if fault:
            self.repaired.append(fault)
            if fault == 'noninteger_counter':
                # 错误实现使用已过期快照；HTTP 仍成功，额度断言必须识别它。
                quota = 100000 if self.stale_refill else self.db['wallet']
                self.cache['Quota'] = str(quota)
            elif self.overwrite_valid:
                self.cache['Quota'] = str(self.db['wallet'])
            self.cache.update(Role='1', MaxConcurrency='0', CacheMAC='fixture-signature',
                              CacheExpiresAt=str(int(time.time() * 1000) + 60000))
        return fault

    def refund(self):
        with self.lock:
            self.db['wallet'] += 1000
            self.db['token'] += 1000
            self.db['token_used'] -= 1000
            self.cache['Quota'] = str(int(self.cache['Quota']) + 1000)
            self.events.append('refund')
            self.refunded.set()

    def request(self):
        with self.lock:
            fault = self.repair()
            held = self.hold_next
            self.hold_next = False
            channel = 'b' if self.active else 'a'
            self.active += 1
            # 供应商收到请求前，钱包/Token 已预扣；wallet_used 在成功结束后更新。
            self.db['wallet'] -= 1000
            self.db['token'] -= 1000
            self.db['token_used'] += 1000
            self.cache['Quota'] = str(int(self.cache['Quota']) - 1000)
            row = {'channel': channel, 'outcome': 'held' if held else 'active'}
            self.requests.append(row)
        if held:
            require(self.released.wait(2), 'fixture_barrier_timeout')
        with self.lock:
            self.active -= 1
            row['outcome'] = 'finished'
            if fault == self.fail_on and fault is not None:
                # 失败请求先返回，原生退款稍后完成；清理不能只看并发归零。
                threading.Timer(.02, self.refund).start()
                return 500, b''
            self.db['wallet_used'] += 1000
            self.events.append('held_finished' if held else 'finished')
        return 200, b''

    def check(self, channels):
        before = self.balances()
        require(self.request()[0] == 200, 'fixture_request_failed')
        self.settled(before, 1000)
        self.zero()


class RedisRepairContractTests(unittest.TestCase):
    def run_contract(self, fixture):
        with patch.object(routing_cache, 'user_cache', side_effect=fixture.user_cache):
            return routing_cache.real_redis_repair(fixture)

    def test_correct_repair_preserves_sentinel_and_refills_invalid_counter(self):
        fixture = RepairFixture()
        result = self.run_contract(fixture)
        self.assertEqual(result['held_db_preconsumed'], 1000)
        self.assertEqual(result['faults'], ['metadata_tamper', 'noninteger_counter', 'unsigned', 'expired'])
        self.assertTrue(result['noninteger_counter_db_refilled'])
        self.assertTrue(result['fixture_cache_cleared'])
        self.assertEqual(fixture.repaired, ['cold', *result['faults']])
        self.assertEqual(fixture.balances(), {'wallet': 94000, 'wallet_used': 6000,
                                              'token': 94000, 'token_used': 6000})
        self.assertFalse(fixture.cache)
        self.assertLess(fixture.events.index('held_finished'), fixture.events.index('delete'))
        # 下一次访问从 DB 水合，不会沿用本轮人为注入的余额偏移。
        fixture.check(['a'])
        self.assertEqual(int(fixture.cache['Quota']), fixture.db['wallet'])

    def test_old_equal_balance_probe_misses_db_overwrite_but_new_probe_rejects_it(self):
        old = RepairFixture(overwrite_valid=True)
        old.check(['a'])
        old.sim.scenario('a', steps=[{'kind': 'hold'}])
        cursor = old.sim.cursor()
        with ThreadPoolExecutor(max_workers=1) as pool:
            held = pool.submit(old.request)
            try:
                wait_received(old, cursor, 'a')
                reserved = int(old.user_cache(old, 'HGET', 'Quota'))
                self.assertEqual(reserved, old.balances()['wallet'])
                old.user_cache(old, 'HSET', 'Role', '100', 'MaxConcurrency', '99')
                self.assertEqual(old.request()[0], 200)
                self.assertEqual(int(old.user_cache(old, 'HGET', 'Quota')), reserved - 1000)
            finally:
                old.sim.release('a')
                held.result(timeout=2)
        new = RepairFixture(overwrite_valid=True)
        with self.assertRaisesRegex(RequirementError, 'redis_repair_lost_inflight_reservation'):
            self.run_contract(new)
        self.assertFalse(new.cache)
        self.assertEqual(new.active, 0)
        self.assertLess(new.events.index('held_finished'), new.events.index('delete'))

    def test_noninteger_repair_must_use_current_database_balance(self):
        fixture = RepairFixture(stale_refill=True)
        with self.assertRaisesRegex(RequirementError, 'redis_noninteger_quota_not_refilled_from_db'):
            self.run_contract(fixture)
        self.assertFalse(fixture.cache)
        self.assertEqual(fixture.db['wallet'], 96000)

    def test_failure_cleanup_waits_for_release_and_asynchronous_refund(self):
        for fault in ('metadata_tamper', 'noninteger_counter', 'unsigned', 'expired'):
            with self.subTest(fault=fault):
                fixture = RepairFixture(fail_on=fault)
                with self.assertRaises(RequirementError):
                    self.run_contract(fixture)
                self.assertTrue(fixture.refunded.is_set())
                self.assertFalse(fixture.cache)
                self.assertEqual(fixture.active, 0)
                self.assertLess(fixture.events.index('held_finished'), fixture.events.index('delete'))
                self.assertLess(fixture.events.index('refund'), fixture.events.index('delete'))

    def test_cleanup_refuses_to_delete_unsettled_reservation(self):
        fixture = RepairFixture()
        before = fixture.balances()
        fixture.db.update(wallet=99000, token=99000, token_used=1000)
        fixture.cache['Quota'] = '99000'
        with patch.object(routing_cache, 'user_cache', side_effect=fixture.user_cache), \
                patch.object(routing_cache.time, 'monotonic', side_effect=[0, 11]):
            with self.assertRaisesRegex(RequirementError, 'redis_cleanup_unsettled_reservation'):
                routing_cache.clear_settled_user_cache(fixture, before)
        self.assertEqual(fixture.cache['Quota'], '99000')
        self.assertNotIn('delete', fixture.events)

    def test_cleanup_refuses_to_delete_while_request_is_active(self):
        fixture = RepairFixture()
        fixture.active = 1
        with patch.object(routing_cache, 'user_cache') as cache:
            with self.assertRaisesRegex(RequirementError, 'fixture_requests_still_active'):
                routing_cache.clear_settled_user_cache(fixture, fixture.balances())
        cache.assert_not_called()

    def test_delete_is_limited_to_the_synthetic_user_hash(self):
        target = {'container_ids': {'redis': 'f' * 64}}
        with patch.object(routing_cache, 'acceptance_target', return_value=target), \
                patch.object(routing_cache, 'execute', return_value=SimpleNamespace(stdout=b'1\n')) as execute:
            self.assertEqual(routing_cache.user_cache(SimpleNamespace(user_id=73), 'DEL'), '1')
        command = execute.call_args.args[0]
        self.assertEqual(command[-2:], ['DEL', 'user:73'])
        self.assertIn(target['container_ids']['redis'], command)
        for user_id, arguments in ((0, ()), (True, ()), ('73', ()), (73, ('user:74',))):
            with self.subTest(user_id=user_id, arguments=arguments), patch.object(routing_cache, 'execute') as execute:
                with self.assertRaisesRegex(RequirementError, 'invalid_cache_fault_operation'):
                    routing_cache.user_cache(SimpleNamespace(user_id=user_id), 'DEL', *arguments)
                execute.assert_not_called()


if __name__ == '__main__':
    unittest.main()
