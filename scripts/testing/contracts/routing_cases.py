"""自动/手动路由的补充 HTTP 合同；供应商状态仅经独立服务控制接口操作。"""
import http.client
import json
import socket
import time
from concurrent.futures import ThreadPoolExecutor
from urllib.parse import urlencode

from .common import require


def wait_received(e, cursor, channel, outcome='held'):
    started = time.monotonic()
    deadline = time.monotonic() + 8
    while True:
        rows = e.sim.records(cursor)
        if any(row['channel'] == channel and row['outcome'] == outcome for row in rows):
            return
        if time.monotonic() >= deadline:
            e.statistics['barrier_timeout'] = {'channel':channel, 'expected_outcome':outcome,
                'elapsed_seconds':time.monotonic()-started, 'records':rows}
            require(False, 'supplier_' + outcome + '_barrier_not_reached_' + channel)
        time.sleep(.02)


def ordered_unavailable(e):
    e.route(['a', 'b'])
    e.status('a', 2)
    e.check(['b'])
    e.status('a', 1)
    e.configure('a', models='mapped-test')
    e.check(['b'])
    e.configure('a', models='route-test', channel_ratio='2')
    e.route(['a', 'b'], cap='1')
    e.check(['b'])
    e.status('b', 2)
    e.check([], status=503, amount=0)


def automatic_selected_channels(e):
    e.configure('a', priority=100)
    e.configure('b', priority=10)
    e.route(['c','b'], mode='auto')
    e.check(['b'])
    e.route(['c','b'], mode='auto', disabled=['b','c'])
    e.check([], status=503, amount=0)


def route_management(e):
    path = f'/api/token/{e.token_id}/routing'
    old = e.user.api('GET', path)
    saved = e.route(['b', 'a'])
    status, raw = e.user.request('PUT', path, {'mode':'auto', 'version':old['version'], 'entries':[]})
    require(status == 409 and not json.loads(raw).get('success'), 'stale_route_version_not_rejected')
    require(e.user.api('GET', path) == saved, 'stale_route_write_changed_state')
    for method, suffix, body in [('GET','',None),('GET','/preview?model=route-test',None),
                                 ('PUT','',{'mode':'auto','version':saved['version'],'entries':[]})]:
        status, raw = e.other.request(method, path+suffix, body)
        value = json.loads(raw)
        require(status in (200,400,403,404) and value.get('success') is False,
                'foreign_route_access_not_rejected')
        require('base_url' not in value and 'key' not in value, 'foreign_route_information_leak')
    token = e.user.api('GET', f'/api/token/{e.token_id}')
    edited = e.user.api('PUT', '/api/token/', {**token, 'name':'edited-'+e.suffix})
    require(edited['type']=='ordered' and e.user.api('GET', f'/api/token/{e.token_id}')['type']=='ordered',
            'edited_token_mode_not_ordered')


def nonretryable(e):
    e.route(['a','b'])
    e.policy(RetryTimes='2', AutomaticRetryStatusCodes='429,500-599')
    e.sim.scenario('a', steps=[{'kind':'http_error','status':400}])
    e.check(['a'],status=400,amount=0)


def retry_after(e):
    e.route(['a','b'])
    e.configure('b',weight=0)
    e.policy(RetryTimes='1', AutomaticRetryStatusCodes='429,500-599')
    e.sim.scenario('a', steps=[{'kind':'http_error','status':429,'retry_after':'30'}])
    e.check(['a','b'])
    e.statistics['cooldown_before_automatic'] = e.routing_observations(('a','b'))
    e.sim.scenario('a')
    e.status('c',2)
    e.route(mode='auto')
    e.check(['b'])
    return {'retry_after_seconds':30, 'assertion':'后续请求在冷却内顺延，不等待墙钟30秒'}


def automatic_breaker_admission(e):
    # Populate the model breaker through the same relay used by clients.
    e.configure('a', priority=10)
    e.configure('b', priority=1)
    e.status('c', 2)
    e.route(['a'])
    e.sim.scenario('a', steps=[{'kind': 'http_error', 'status': 502}])
    for _ in range(5):
        e.check(['a'], status=502, amount=0)
    e.route(mode='auto')
    e.check(['b'])
    e.status('b', 2)
    raw, _ = e.check([], status=503, amount=0)
    require(json.loads(raw)['error']['code'] == 'channel_temporarily_unavailable',
            'all_open_breakers_require_temporary_refusal')
    delay = e.exchanges[-1].get('retry_after')
    require(isinstance(delay, str) and delay.isdigit() and 1 <= int(delay) <= 60,
            'all_open_refusal_missing_probe_deadline')


def prefer_escapes_cooling_channel(e):
    rules = [{'name': 'routing-cache-escape', 'model_regex': ['^route-test$'],
              'path_regex': ['/v1/chat/completions'], 'session_mode': 'prefer',
              'key_sources': [{'type': 'request_header', 'key': 'X-Routing-Session'}]}]
    e.policy(**{'channel_affinity_setting.enabled': 'true',
                'channel_affinity_setting.rules': json.dumps(rules),
                'channel_affinity_setting.switch_on_success': 'true'})
    e.status('b', 2)
    e.status('c', 2)
    e.configure('a', priority=10)
    e.route(mode='auto')
    headers = {'X-Routing-Session': 'cooldown-' + e.suffix}
    e.check(['a'], headers=headers)
    e.status('b', 1)
    e.sim.scenario('a', steps=[{'kind': 'http_error', 'status': 429, 'retry_after': '30'}])
    e.check(['a'], status=429, amount=0, headers=headers)
    e.check(['b'], headers=headers)
    # Publish A's configuration, clearing its pause. The successful escape wrote B.
    e.sim.scenario('a')
    e.configure('a', priority=11)
    e.check(['b'], headers=headers)


def automatic_nonfull_load(e):
    e.route(['a'])
    e.configure('a',weight=1000000,max_concurrency=2)
    e.configure('b',weight=1,max_concurrency=8)
    e.status('c',2)
    e.sim.scenario('a',steps=[{'kind':'hold'}])
    cursor,before=e.sim.cursor(),e.balances()
    with ThreadPoolExecutor(max_workers=1) as pool:
        held=pool.submit(e.request)
        try:
            wait_received(e,cursor,'a')
            state=e.routing_observations(('a','b'))
            channels={row['id']:row for row in state['channels']}
            require(channels[e.channel_map['a']]['in_flight']==1
                    and channels[e.channel_map['a']]['limit']==2
                    and channels[e.channel_map['b']]['in_flight']==0, 'nonfull_load_fixture_missing')
            e.route(mode='auto')
            for _ in range(3):
                require(e.request()[0]==200,'nonfull_load_request_failed')
            require([row['channel'] for row in e.sim.records(cursor)]==['a','b','b','b'],
                    'nonfull_load_did_not_precede_weight')
            e.statistics['nonfull_load_before_release']=state
        finally:
            e.sim.release('a')
        require(held.result(timeout=10)[0]==200,'nonfull_held_request_failed')
    e.settled(before,4000)
    e.zero()


def expensive_retry(e):
    e.route(['a','b'])
    e.configure('b',channel_ratio='3')
    e.policy(RetryTimes='1')
    e.sim.scenario('a',steps=[{'kind':'http_error','status':502}])
    e.root.api('POST','/api/user/manage',{'id':e.user_id,'action':'add_quota','mode':'override','value':1500,
                                       'operation_id':'poor-'+e.suffix})
    try:
        e.check(['a'],status=403,amount=0)
    finally:
        e.root.api('POST','/api/user/manage',{'id':e.user_id,'action':'add_quota','mode':'override','value':100000000,
                                           'operation_id':'restore-'+e.suffix})


def delivered_stream(e):
    e.route(['a','b'])
    e.policy(RetryTimes='2')
    e.sim.scenario('a',steps=[{'kind':'stream_disconnect'}],text='partial-routing-answer')
    raw, rows = e.check(['a'], body={'model':e.model,'messages':[{'role':'user','content':'stream fixture'}],
                                    'stream':True,'max_tokens':8})
    require(b'partial-routing-answer' in raw, 'stream_did_not_deliver_content')
    require(rows[0]['outcome']=='stream_disconnected', 'supplier_stream_fault_not_applied')


def disconnect_retry(e):
    e.route(['a','b'])
    e.policy(RetryTimes='1')
    e.sim.scenario('a',steps=[{'kind':'disconnect'}])
    e.check(['a','b'])


def client_disconnect_settles_once_and_releases(e):
    """普通 HTTP 成功路径：下游断开后，上游 attempt 继续占槽直到完成。"""
    e.route(['a'])
    e.configure('a', max_concurrency=1)
    e.policy(RetryTimes='0')
    e.sim.scenario('a', steps=[{'kind':'hold'}])
    cursor, before = e.sim.cursor(), e.balances()
    log_cursor = e.rows(f'SELECT COALESCE(MAX(id),0) AS id FROM logs WHERE user_id={int(e.user_id)}')[0]['id']
    connection = http.client.HTTPConnection('127.0.0.1', 3000, timeout=5)
    evidence = {'case':e.current_case, 'request_id':None,
                'request_id_unavailable':'client_closed_before_response_headers',
                'method':'POST', 'path':'/v1/chat/completions', 'model':e.model,
                'expected_charge':1000, 'balances_before':before, 'client_disconnected':False}
    e.exchanges.append(evidence)
    released = False

    def occupied():
        state = e.root.api('GET', '/api/performance/routing')
        user = next(row for row in state['users'] if row['id'] == e.user_id)
        channel = next(row for row in state['channels'] if row['id'] == e.channel_map['a'])
        observed = {'user':user['in_flight'], 'channel':channel['in_flight']}
        require(observed == {'user':1, 'channel':1}, 'disconnected_attempt_released_early')
        return observed

    try:
        connection.request('POST', '/v1/chat/completions', json.dumps({
            'model':e.model, 'messages':[{'role':'user','content':'disconnect success fixture'}],
            'max_tokens':8}), {'Authorization':'Bearer '+e.key, 'Content-Type':'application/json'})
        wait_received(e, cursor, 'a')
        evidence['occupancy_before_disconnect'] = occupied()
        connection.sock.shutdown(socket.SHUT_RDWR)
        connection.close()
        evidence['client_disconnected'] = True
        evidence['occupancy_after_disconnect'] = occupied()
        # 实际准入请求验证槽位仍被持有；该拒绝请求不能命中供应商或扣款。
        require(e.request(expected_charge=0)[0] == 429, 'disconnected_attempt_capacity_not_held')
        held = e.sim.records(cursor)
        require(len(held) == 1 and held[0]['channel'] == 'a' and held[0]['outcome'] == 'held',
                'disconnected_attempt_replayed_or_cancelled')
        evidence['supplier_at_hold_barrier'] = held
        e.sim.release('a')
        released = True
        after = e.settled(before, 1000)
        evidence['concurrency_after'] = e.zero()
        rows = e.sim.records(cursor)
        require(len(rows) == 1 and rows[0]['channel'] == 'a' and rows[0]['outcome'] == 'success'
                and rows[0].get('finished_at') and rows[0]['status'] == 200,
                'disconnected_success_supplier_not_once')
        deadline = time.monotonic() + 8
        while True:
            logs = e.rows(f'SELECT request_id, type, quota FROM logs WHERE user_id={int(e.user_id)} '
                          f'AND id>{int(log_cursor)} AND type IN (2,6) ORDER BY id')
            if logs:
                break
            require(time.monotonic() < deadline, 'disconnected_success_settlement_log_missing')
            time.sleep(.02)
        require(len(logs) == 1 and logs[0]['type'] == 2 and logs[0]['quota'] == 1000
                and isinstance(logs[0].get('request_id'), str) and logs[0]['request_id'],
                'disconnected_success_settlement_not_once')
        evidence.pop('request_id_unavailable')
        evidence.update(request_id=logs[0]['request_id'], request_id_source='isolated_native_financial_log',
                        balances_after=after, actual_charge=before['wallet']-after['wallet'],
                        supplier_after_release=rows, settlement_logs=logs)
        return {'supplier_hits':1, 'charge':1000, 'settlements':1,
                'permit_lifecycle':'held_after_disconnect_then_zero_after_success',
                'scope':'ordinary_http_success_only'}
    finally:
        connection.close()
        if not released:
            e.sim.release('a')
        e.zero()
        after = e.balances()
        evidence.update(balances_after_cleanup=after, actual_charge=before['wallet']-after['wallet'],
                        supplier_after_cleanup=e.sim.records(cursor))


def automatic_busy(e):
    e.route(mode='auto')
    e.configure('a',priority=10,max_concurrency=1)
    e.configure('b',priority=5)
    e.sim.scenario('a',steps=[{'kind':'hold'}])
    cursor, before = e.sim.cursor(), e.balances()
    with ThreadPoolExecutor(max_workers=1) as pool:
        held = pool.submit(e.request)
        try:
            wait_received(e,cursor,'a')
            require(e.request()[0]==200,'automatic_busy_not_skipped')
            rows = e.sim.records(cursor)
            require([row['channel'] for row in rows]==['a','b'],'automatic_busy_wrong_channel')
            e.configure('a',max_concurrency=2)
            state = e.root.api('GET','/api/performance/routing')
            require(next(row for row in state['channels'] if row['id']==e.channel_map['a'])['in_flight']==1,
                    'hot_update_lost_existing_occupancy')
            e.configure('a',max_concurrency=1)
            require(e.request()[0]==200,'hot_lower_limit_not_skipped')
        finally:
            e.sim.release('a')
        require(held.result(timeout=10)[0]==200,'held_auto_request_failed')
    e.settled(before,3000)
    e.zero()


def endpoint_eligibility(e):
    e.route(['claude','a'])
    e.configure('claude',models='claude-sonnet-4-5,route-test')
    try:
        # Claude supports Responses conversion, but not Responses compaction.
        e.check(['claude'], path='/v1/responses', body={'model':e.model,'input':'endpoint fixture'})
        e.route(['claude'])
        e.check([], status=400, amount=0, path='/v1/responses/compact',
                body={'model':e.model,'input':'unsupported compaction'})
    finally:
        e.configure('claude',models='claude-sonnet-4-5')


def preview_ws(e):
    e.route(['a'])
    e.configure('a',setting=json.dumps({'responses_websocket_enabled':False}))
    try:
        query=urlencode({'model':e.model,'path':'/v1/responses','stream':'true','transport':'websocket'})
        preview=e.user.api('GET',f'/api/token/{e.token_id}/routing/preview?'+query)
        require(preview.get('transport')=='websocket', 'preview_ws_transport_missing')
        from .websocket import WebSocket
        cursor,before=e.sim.cursor(),e.balances()
        connection=WebSocket.connect(e.base,e.key)
        evidence={'case':e.current_case,'request_id':None,'handshake_request_id':connection.request_id,
                            'request_id_unavailable':'upgrade_and_rejected_create_do_not_expose_gateway_id',
                            'method':'WS','path':'/v1/responses','model':e.model,'status':101,'expected_charge':0}
        e.exchanges.append(evidence)
        try:
            connection.send({'type':'response.create','response':{'model':e.model,'input':'preview'}})
            event=connection.receive()
        finally:
            connection.close()
        require(event.get('type')=='error' and not e.sim.records(cursor), 'ws_ineligible_channel_was_used')
        after=e.settled(before,0)
        evidence.update(balances_before=before,balances_after=after,actual_charge=before['wallet']-after['wallet'])
        actual=next(row for row in preview['entries'] if row['channel_id']==e.channel_map['a'])
        e.statistics['preview_ws']={'preview_eligible':actual['eligible'],'actual_event':event['type'],
                                    'supplier_hits':len(e.sim.records(cursor))}
        require(not actual['eligible'],'local_preview_ws_eligibility_mismatch')
        e.configure('a',setting=json.dumps({'responses_websocket_enabled':True}))
        enabled=e.user.api('GET',f'/api/token/{e.token_id}/routing/preview?'+query)
        entry=next(row for row in enabled['entries'] if row['channel_id']==e.channel_map['a'])
        require(enabled.get('transport')=='websocket' and entry['eligible'],
                'local_preview_enabled_ws_eligibility_mismatch')
        e.websocket()
        e.statistics['preview_ws']['enabled_preview_eligible']=entry['eligible']
        e.statistics['preview_ws']['enabled_actual_event']='response.completed'
        return {'preview_eligible':actual['eligible'],'actual_event':event['type'],
                'enabled_preview_eligible':entry['eligible'],'enabled_actual_event':'response.completed'}
    finally:
        e.configure('a',setting=json.dumps({'responses_websocket_enabled':True}))


def preview_plugin(e):
    e.configure('hailuo',models='MiniMax-Hailuo-02,sora-2')
    e.route(['hailuo'])
    try:
        query=urlencode({'model':'sora-2','path':'/v1/tasks/sora','stream':'false'})
        preview=e.user.api('GET',f'/api/token/{e.token_id}/routing/preview?'+query)
        e.check([],status=503,amount=0,path='/v1/tasks/sora',
                body={'model':'sora-2','prompt':'wrong plugin','seconds':4,'size':'1280x720'})
        entry=next(row for row in preview['entries'] if row['channel_id']==e.channel_map['hailuo'])
        e.statistics['preview_plugin']={'preview_eligible':entry['eligible'],'actual_status':503,'supplier_hits':0}
        require(not entry['eligible'],'local_preview_plugin_eligibility_mismatch')
    finally:
        e.configure('hailuo',models='MiniMax-Hailuo-02')


def error_privacy(e):
    e.sim.scenario('a',steps=[{'kind':'http_error','status':400}])
    raw,_=e.check(['a'],status=400,amount=0)
    value=json.loads(raw)
    require(isinstance(value.get('error'),dict),'error_envelope_missing')
    for channel in e.sim.channels.values():
        require(all(key['value'].encode() not in raw for key in channel['keys']), 'provider_credential_exposed')
    require(all(row['name'].encode() not in raw for row in e.configs.values()), 'private_channel_name_exposed')
    from routing_result import has_private_error_field
    require(not has_private_error_field(value), 'nested_private_routing_field_exposed')
    require(e.key.encode() not in raw and e.sim.target['address'].encode() not in raw,
            'client_error_exposes_private_routing_data')
    status,raw=e.request(key='invalid-routing-fixture-key',expected_charge=0)
    require(status==401 and not has_private_error_field(json.loads(raw)),
            'invalid_token_error_not_private')


CASES = [('automatic_selected_channels',automatic_selected_channels),('ordered_unavailable_no_auto_fallback',ordered_unavailable),
         ('route_version_ownership_edit_mode',route_management),('nonretryable_no_replay',nonretryable),
         ('retry_after_cooldown',retry_after),('automatic_breaker_admission',automatic_breaker_admission),
         ('prefer_escapes_cooling_channel',prefer_escapes_cooling_channel),('expensive_retry_insufficient_reserve',expensive_retry),
         ('delivered_stream_not_replayed',delivered_stream),('disconnect_retry',disconnect_retry),
         ('client_disconnect_settles_once_and_releases',client_disconnect_settles_once_and_releases),
         ('automatic_busy_hot_limit',automatic_busy),('automatic_nonfull_load_bucket',automatic_nonfull_load),
         ('endpoint_eligibility',endpoint_eligibility),
         ('preview_websocket_eligibility',preview_ws),('preview_plugin_eligibility',preview_plugin),
         ('error_classification_and_privacy',error_privacy)]
