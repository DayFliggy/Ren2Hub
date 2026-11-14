"""真实网关到独立标准供应商的路由合同；配置只走原生管理 API。"""
import hashlib
import json
import secrets
import time
import urllib.request
import traceback
from concurrent.futures import ThreadPoolExecutor
from pathlib import Path
from decimal import Decimal
from datetime import datetime, timezone

from .client import APIError, Client
from .common import (RequirementError, acceptance_target, atomic_private, require,
                     save_model_pricing, expression_pricing, require_charge)
from .gateway import Experiment
from .websocket import WebSocket
from routing_result import (REQUIRED_CASES, evaluate, websocket_request_identity,
                            weight_tail_probability)

BLOCKED_REMIX = ('native_remix_upstream_id', 'native_remix_plugin_action', 'native_remix_inherited_billing',
                 'native_remix_platform', 'native_remix_expression_usage', 'native_remix_unsupported_provider')


class Simulator:
    def __init__(self):
        self.target = acceptance_target()['simulator']
        self.config = json.loads(Path(self.target['configuration_path']).read_text())
        self.channels = {row['id']: row for row in self.config['channels']}
        self.base = 'http://' + self.target['address'] + ':19090'
        self.opener = urllib.request.build_opener(urllib.request.ProxyHandler({}))

    def call(self, method, path, body=None):
        request = urllib.request.Request(self.base + path, method=method,
            headers={'Authorization': 'Bearer ' + self.config['control_key'], 'Content-Type': 'application/json'},
            data=None if body is None else json.dumps(body).encode())
        with self.opener.open(request, timeout=5) as response:
            require(response.status == 200, 'simulator_control_failed')
            return json.load(response)

    def scenario(self, name, **scenario):
        self.call('POST', '/control/channels/' + name + '/scenario', scenario)

    def records(self, after=0):
        return self.call('GET', '/control/requests?after=' + str(after))['requests']

    def cursor(self):
        return max((row['sequence'] for row in self.records()), default=0)

    def release(self, name):
        self.call('POST', '/control/channels/' + name + '/release', {})


class Routing(Experiment):
    def __init__(self, args):
        super().__init__(args)
        self.sim = Simulator()
        self.model = 'route-test'
        self.channel_map, self.configs = {}, {}
        self.results, self.statistics, self.exchanges = [], {}, []
        self.required = REQUIRED_CASES
        self.current_case = None

    def setup(self):
        self.initialize()
        from .routing_tasks import register_origin_plugin
        self.origin_plugin = register_origin_plugin(self)
        self.root.api('POST', '/api/user/manage', {'id': self.user_id, 'action': 'add_quota',
            'mode': 'override', 'value': 100000000, 'operation_id': 'routing-' + self.suffix})
        self.add_token()
        token = self.user.api('GET', f'/api/token/{self.token_id}')
        self.user.api('PUT', '/api/token/', {**token, 'remain_quota': 100000000,
            'model_limits_enabled': False, 'model_limits': ''})
        types = {'openai': 1, 'anthropic': 14, 'gemini': 24, 'sora': 61, 'hailuo': 61, 'mj': 2}
        models = {'openai': 'route-test,route-test@temperature:0.7,mapped-test', 'anthropic': 'claude-sonnet-4-5',
                  'gemini': 'gemini-2.5-flash', 'sora': 'sora-2', 'hailuo': 'MiniMax-Hailuo-02',
                  'mj': 'mj_imagine,mj_upscale,mj_modal,mj_change,mj_action,mj_video'}
        for name, provider in self.sim.channels.items():
            protocol = provider['protocol']
            settings = {'responses_websocket_enabled': True}
            if protocol in ('sora', 'hailuo'):
                settings['task_plugin_key'] = self.origin_plugin if name == 'origin' else protocol
            payload = {'name': 'routing-' + self.suffix + '-' + name, 'type': types[protocol],
                'key': provider['keys'][0]['value'],
                'base_url': 'http://' + self.sim.target['address'] + provider['listen'],
                'models': 'routing-origin' if name == 'origin' else models[protocol], 'priority': 0, 'weight': 100,
                'channel_ratio': '1', 'max_concurrency': 0, 'setting': json.dumps(settings)}
            self.root.api('POST', '/api/channel/', {'mode': 'single', 'channel': payload})
            rows = self.root.api('GET', '/api/channel/search?keyword=' + payload['name'] + '&page_size=100')['items']
            rows = [row for row in rows if row['name'] == payload['name']]
            require(len(rows) == 1, 'routing_channel_identity_not_unique')
            channel_id = rows[0]['id']
            self.channels.append(channel_id)
            self.channel_map[name] = channel_id
            self.configs[name] = {'id': channel_id, **payload}
            self.configure(name)
        for model in set(','.join(models.values()).split(',')) | {'routing-origin'}:
            save_model_pricing(self.root, model, {'ModelPrice': .002})
        save_model_pricing(self.root, 'MiniMax-Hailuo-02',
                           expression_pricing('tier("seconds", u("seconds") * 0.002)'))
        # Only the dedicated provider IP and ports may serve task artifacts.
        options = {'fetch_setting.enable_ssrf_protection': 'true', 'fetch_setting.domain_filter_mode': 'true',
            'fetch_setting.domain_list': '[]', 'fetch_setting.ip_filter_mode': 'true',
            'fetch_setting.ip_list': json.dumps([self.sim.target['address'] + '/32']),
            'fetch_setting.allowed_ports': json.dumps([row['listen'][1:] for row in self.sim.channels.values()]),
            'fetch_setting.apply_ip_filter_for_domain': 'true', 'fetch_setting.allow_private_ip': 'true'}
        self.original_fetch = {row['key']: row['value'] for row in self.root.api('GET', '/api/option/') if row['key'] in options}
        for key, value in options.items():
            self.root.api('PUT', '/api/option/', {'key': key, 'value': value})
        self.original_policy = self.root.api('GET', '/api/option/request_policy')['options']
        self.policy(RetryTimes='0', AutomaticDisableChannelEnabled='false', **{'channel_affinity_setting.enabled': 'false'})
        self.defaults()

    def configure(self, name, **values):
        self.configs[name].update(values)
        self.root.api('PUT', '/api/channel/', self.configs[name])
        saved = self.root.api('GET', f'/api/channel/{self.channel_map[name]}')
        for key, value in values.items():
            actual = saved.get(key)
            require(Decimal(str(actual)) == Decimal(str(value)) if key == 'channel_ratio' else actual == value,
                    'channel_configuration_readback_' + key)

    def status(self, name, value):
        self.root.api('POST', f'/api/channel/{self.channel_map[name]}/status', {'status': value})
        require(self.root.api('GET', f'/api/channel/{self.channel_map[name]}')['status'] == value,
                'channel_status_readback_mismatch')

    def policy(self, **options):
        self.root.api('PATCH', '/api/option/request_policy', {'options': options})
        actual = self.root.api('GET', '/api/option/request_policy')['options']
        require(all(actual.get(key) == value for key, value in options.items()), 'request_policy_readback_mismatch')

    def route(self, names=(), mode='manual', cap=None, disabled=()):
        path = f'/api/token/{self.token_id}/routing'
        current = self.user.api('GET', path)
        saved = self.user.api('PUT', path, {'mode': mode, 'version': current['version'],
            'entries': [{'channel_id': self.channel_map[name], 'enabled': name not in disabled} for name in names],
            'max_channel_ratio': cap})
        require(saved == self.user.api('GET', path) and saved['mode'] == mode
                and saved['version'] == current['version'] + 1, 'route_configuration_readback_mismatch')
        require([row['channel_id'] for row in saved['entries']] == [self.channel_map[name] for name in names],
                'route_order_readback_mismatch')
        actual = saved.get('max_channel_ratio')
        require(actual is None if cap is None else Decimal(actual) == Decimal(cap), 'route_cap_readback_mismatch')
        return saved

    def defaults(self):
        for name in self.channel_map:
            self.sim.release(name)
            # Continuations still finish through the real background poller;
            # two observable states keep a large routing matrix bounded.
            self.sim.scenario(name, task_states=['queued','completed'])
            self.status(name, 1)
            self.configure(name, channel_ratio='1', max_concurrency=0)
        for name in ('a', 'b', 'c'):
            self.configure(name, priority=0, weight=100, channel_ratio='1', max_concurrency=0,
                           models='route-test,route-test@temperature:0.7,mapped-test', model_mapping='')
            self.status(name, 1)
        self.route(['a'])
        self.policy(RetryTimes='0', **{'channel_affinity_setting.enabled': 'false'})

    def zero(self):
        deadline = time.monotonic() + 8
        while True:
            state = self.root.api('GET', '/api/performance/routing')
            users = {row['id']: row for row in state['users']}
            channels = {row['id']: row for row in state['channels']}
            require(self.user_id in users and set(self.channels) <= channels.keys(), 'routing_occupancy_resources_missing')
            rows = [users[self.user_id], *(channels[cid] for cid in self.channels)]
            if all(row['in_flight'] == 0 for row in rows):
                return {'user': users[self.user_id]['in_flight'],
                        'channels': {name: channels[cid]['in_flight'] for name, cid in self.channel_map.items()}}
            require(time.monotonic() < deadline, 'routing_permit_leak')
            time.sleep(.05)

    def request(self, path='/v1/chat/completions', body=None, key=None, headers=None, method='POST', expected_charge=1000):
        if body is None and method == 'POST':
            body = {'model': self.model, 'messages': [{'role': 'user', 'content': 'routing fixture'}], 'max_tokens': 8}
        status, raw, received = Client(self.base).request(method, path, body, bearer=key or self.key,
                                                        headers=headers, response_headers=True)
        normalized = {key.lower(): value for key, value in received.items()}
        request_id = normalized.get('x-oneapi-request-id')
        require(bool(request_id), 'gateway_request_id_missing')
        self.exchanges.append({'case': self.current_case, 'request_id': request_id,
            'method': method, 'path': path.split('?')[0], 'model': (body or {}).get('model'), 'status': status,
            'expected_charge':expected_charge})
        return status, raw

    def settled(self, before, amount, timeout=10):
        after = super().settled(before, amount, timeout)
        if amount == 0:
            self.zero()
            # Bounded quiet-window check supplements the per-request log check;
            # it is not a claim about arbitrary future asynchronous writes.
            deadline = time.monotonic() + .25
            while time.monotonic() < deadline:
                after = self.balances()
                require_charge(before, after, 0)
                time.sleep(.02)
        return after

    def routing_observations(self, names):
        state = self.root.api('GET', '/api/performance/routing')
        ids = {self.channel_map[name] for name in names}
        return {'channels':[row for row in state['channels'] if row['id'] in ids],
                'observations':[row for row in state['observations'] if row['channel_id'] in ids]}

    def check(self, expected, status=200, amount=1000, **request):
        before, cursor = self.balances(), self.sim.cursor()
        actual, raw = self.request(expected_charge=amount, **request)
        exchange = self.exchanges[-1]
        exchange.update(expected_charge=amount, expected_status=status, expected_channels=expected,
                        balances_before=before)
        try:
            require(actual == status, 'routing_status_' + str(actual) + '_expected_' + str(status))
            self.settled(before, amount)
            exchange['concurrency'] = self.zero()
            rows = [row for row in self.sim.records(cursor)
                    if row['method'] == request.get('method','POST') and '/list-by-condition' not in row['path']]
            if request.get('method') == 'GET':
                rows = [row for row in rows if row['path'].endswith('/image-seed')]
            require([row['channel'] for row in rows] == expected, 'routing_actual_supplier_order')
            require(all(row['auth_valid'] and row['key_alias'] == row['channel'] + '-primary' for row in rows),
                    'routing_supplier_key_address_mismatch')
        finally:
            # Failed assertions need the same debit and supplier evidence as passes.
            # Evidence retrieval failure also fails the case explicitly.
            try:
                after = self.balances()
                exchange.update(balances_after=after, actual_charge=before['wallet']-after['wallet'],
                    retry_trace=[{'sequence': row['sequence'], 'channel': row['channel'], 'status': row['status']}
                                 for row in self.sim.records(cursor)])
            except Exception as error:
                exchange['evidence_error'] = type(error).__name__
                raise
        return raw, rows

    def case(self, name, action):
        started, cursor = time.monotonic(), self.sim.cursor()
        result = {'name': name, 'status': 'fail', 'contract_scope':
                  'synthetic_plugin_extension' if name == 'plugin_origin_pin_price_and_capacity'
                  else 'standard_protocol'}
        self.current_case = name
        try:
            self.defaults()
            detail = action()
            result.update(status='pass', detail=detail)
        except Exception as error:
            result.update(error_code=str(error) if isinstance(error, (RequirementError, APIError)) else type(error).__name__,
                          failure_location=[{'file':Path(frame.filename).name,'line':frame.lineno,'function':frame.name}
                                            for frame in traceback.extract_tb(error.__traceback__)[-4:]])
        finally:
            try:
                for channel in self.channel_map:
                    self.sim.release(channel)
                result['concurrency_after'] = self.zero()
                result['requests'] = self.sim.records(cursor)
                # Native financial logs associate individual requests even when
                # concurrent provider arrival/completion orders differ.
                logs = self.rows(f'SELECT request_id, channel_id, type, quota FROM logs WHERE user_id={int(self.user_id)} AND type IN (2,6) ORDER BY id')
                aliases = {value:key for key,value in self.channel_map.items()}
                for exchange in self.exchanges:
                    if exchange['case'] != name:
                        continue
                    rows = [row for row in logs if row['request_id'] == exchange.get('request_id')]
                    exchange['settlement_logs'] = [{**row,'channel_alias':aliases.get(row['channel_id'])} for row in rows]
                    exchange['logged_net_charge'] = (sum(row['quota'] if row['type']==2 else -row['quota'] for row in rows)
                                                     if exchange.get('request_id') else None)
                    exchange['concurrency_after_case'] = result['concurrency_after']
                    exchange['supplier_evidence_case'] = name
                    if exchange.get('request_id'):
                        require(exchange['logged_net_charge'] == exchange['expected_charge'],
                                'per_request_settlement_mismatch')
            except Exception as error:
                result.update(status='fail', cleanup_error=str(error) if isinstance(error, RequirementError) else type(error).__name__)
            result.update(duration_seconds=round(time.monotonic() - started, 3))
            self.results.append(result)
            atomic_private(self.args.report.with_name('routing-progress.json'),
                           json.dumps({'complete':False,'cases':self.results},ensure_ascii=False))
        return result

    def automatic(self):
        self.route(mode='auto')
        self.configure('a', priority=10)
        self.check(['a'])
        self.configure('a', channel_ratio='2')
        self.route(mode='auto', cap='1')
        self.configure('b', priority=5)
        self.check(['b'])
        self.status('b', 2)
        self.check(['c'])
        self.configure('c', models='mapped-test')
        self.check([], status=503, amount=0)

    def exact_model(self):
        self.route(['b', 'a'])
        self.configure('b', models='route-test')
        self.check(['a'], body={'model': 'route-test@temperature:0.7', 'messages': [{'role': 'user', 'content': 'exact'}]})
        self.configure('a', channel_ratio='2')
        self.route(['b', 'a'], cap='1')
        self.check(['b'], body={'model': 'route-test@temperature:0.7', 'messages': [{'role': 'user', 'content': 'normalized'}]})

    def manual(self):
        self.configure('a', priority=100)
        self.route(['b', 'a'])
        self.check(['b'])
        self.route(['b', 'a'], disabled=['b'])
        self.check(['a'])
        self.route([])
        self.check([], status=503, amount=0)
        self.route(['a'])
        self.configure('a', model_mapping=json.dumps({'route-test': 'mapped-test'}), channel_ratio='1.5')
        _, rows = self.check(['a'], amount=1500)
        require(rows[0]['model'] == 'mapped-test', 'routing_model_mapping_not_sent')
        self.route(['a'], cap='1')
        self.check([], status=503, amount=0)

    def zero_weight(self):
        self.route(mode='auto')
        self.configure('a', weight=0)
        self.configure('b', weight=100)
        self.configure('c', weight=0)
        for _ in range(8):
            self.check(['b'])
        self.status('b', 2)
        before, cursor = self.balances(), self.sim.cursor()
        for _ in range(16):
            require(self.request()[0] == 200, 'zero_weight_fallback_failed')
        rows = self.sim.records(cursor)
        require(len(rows) == 16 and all(row['channel'] in ('a', 'c') for row in rows), 'zero_weight_fallback_wrong_channel')
        self.settled(before, 16000)
        self.zero()

    def weight_distribution(self):
        self.route(mode='auto')
        self.configure('a', weight=1)
        self.configure('b', weight=3)
        self.status('c', 2)
        observations_before = self.routing_observations(('a','b'))
        require(not observations_before['observations']
                and all(row['in_flight']==0 for row in observations_before['channels']),
                'weight_sample_requires_fresh_idle_channels')
        before, cursor = self.balances(), self.sim.cursor()
        for _ in range(80):
            require(self.request()[0] == 200, 'weight_sample_failed')
        rows = self.sim.records(cursor)
        require(len(rows) == 80 and all(row['channel'] in ('a', 'b') for row in rows), 'weight_sample_wrong_channel')
        self.settled(before, 80000)
        self.zero()
        observations_after = self.routing_observations(('a','b'))
        require(all(row['in_flight']==0 for row in observations_after['channels'])
                and all(row['error_rate']==0 and (not row['latency_known'] or row['latency_ms']<3000)
                        for row in observations_after['observations']), 'weight_sample_ranking_changed')
        result = {'samples': 80, 'configured_weights': {'a': 1, 'b': 3},
                  'observed_counts': {name: sum(row['channel'] == name for row in rows) for name in ('a', 'b')},
                  'observations_before':observations_before, 'observations_after':observations_after,
                  'interpretation': '统计审计；不将随机有限样本当精确比例证明'}
        result['binomial_two_sided_p'] = weight_tail_probability(result['observed_counts']['a'], 80, .25)
        self.statistics['weights'] = result
        require(result['binomial_two_sided_p'] >= .001, 'weight_sample_significant_bias_review_required')
        return result

    def retry(self):
        self.route(['a', 'b'])
        self.policy(RetryTimes='1')
        self.sim.scenario('a', steps=[{'kind': 'http_error', 'status': 503}])
        self.check(['a', 'b'])
        self.route(['a'])
        self.check(['a'], status=503, amount=0)

    def health(self):
        self.route(['a'])
        # Healthy zero-weight backup outranks a degraded positive-weight row.
        # If degradation breaks, a always wins: no random tie can hide it.
        self.configure('b', weight=0)
        self.sim.scenario('a', steps=[{'kind': 'http_error', 'status': 503}])
        for _ in range(5):
            self.check(['a'], status=503, amount=0)
        self.statistics['health_before_automatic'] = self.routing_observations(('a','b'))
        self.sim.scenario('a')
        self.status('c', 2)
        self.route(mode='auto')
        for _ in range(4):
            self.check(['b'])
        self.route(['a', 'b'])
        self.check(['a'])

    def concurrency(self):
        self.configure('a', max_concurrency=1)
        person = self.root.api('GET', f'/api/user/{self.user_id}')
        def limit(value):
            self.root.api('PUT', '/api/user/', {key: person[key] for key in ('id','username','display_name','email')} | {'max_concurrency': value})
        name = 'routing-secondary-' + self.suffix
        self.user.api('POST', '/api/token/', {'name': name, 'remain_quota': 1000000, 'expired_time': -1})
        token = next(row for row in self.user.api('GET', '/api/token/?p=1&page_size=100')['items'] if row['name'] == name)
        self.tokens.append(token['id'])
        key = self.user.api('POST', f'/api/token/{token["id"]}/key')['key']
        limit(1)
        self.sim.scenario('a', steps=[{'kind': 'hold'}])
        before, cursor = self.balances(), self.sim.cursor()
        with ThreadPoolExecutor(max_workers=1) as pool:
            pending = pool.submit(self.request)
            try:
                deadline = time.monotonic() + 5
                while not self.sim.records(cursor):
                    require(time.monotonic() < deadline, 'held_request_not_received')
                    time.sleep(.02)
                started = time.monotonic()
                require(self.request(key=key,expected_charge=0)[0] == 429, 'cross_key_user_limit_missing')
                require(time.monotonic() - started < 2, 'user_limit_queued')
                limit(0)
                require(self.request(expected_charge=0)[0] == 429, 'channel_limit_missing')
                self.route(['a', 'b'])
                require(self.request()[0] == 200, 'manual_busy_fallback_failed')
                require([row['channel'] for row in self.sim.records(cursor)] == ['a','b'], 'rejected_request_sent')
            finally:
                self.sim.release('a')
                limit(0)
            require(pending.result(timeout=10)[0] == 200, 'held_request_failed')
        self.settled(before, 2000)
        self.zero()

    def native_protocols(self):
        bodies = [
            ('a', '/v1/chat/completions', {'model':'route-test','messages':[{'role':'user','content':'hello'}]}, '[DONE]'),
            ('a', '/v1/responses', {'model':'route-test','input':'hello'}, 'response.completed'),
            ('claude', '/v1/messages', {'model':'claude-sonnet-4-5','max_tokens':8,'messages':[{'role':'user','content':'hello'}]}, 'message_stop'),
            ('gemini', '/v1beta/models/gemini-2.5-flash:generateContent', {'contents':[{'parts':[{'text':'hello'}]}]}, None)]
        for name, path, body, terminal in bodies:
            self.route([name])
            self.check([name], path=path, body=body)
            if terminal:
                raw, _ = self.check([name], path=path, body={**body, 'stream': True})
                require(terminal.encode() in raw, 'stream_terminal_missing_' + name)
            else:
                raw, _ = self.check([name], path=path.replace(':generateContent', ':streamGenerateContent?alt=sse'), body=body)
                require(b'data:' in raw and b'usageMetadata' in raw, 'gemini_stream_usage_missing')
        # Existing gateway conversion must generate the actual vendor request.
        for name, model in [('claude','claude-sonnet-4-5'),('gemini','gemini-2.5-flash')]:
            self.route([name])
            self.check([name], body={'model':model,'messages':[{'role':'user','content':'conversion'}], 'max_tokens':8})

    def websocket(self):
        before, cursor = self.balances(), self.sim.cursor()
        log_cursor = self.rows(f'SELECT COALESCE(MAX(id),0) AS id FROM logs WHERE user_id={int(self.user_id)}')[0]['id']
        connection = WebSocket.connect(self.base, self.key)
        evidence = {'case':self.current_case,'request_id':None,'handshake_request_id':connection.request_id,
            'method':'WS','path':'/v1/responses','model':self.model,'status':101,'expected_charge':1000,
            'balances_before':before}
        self.exchanges.append(evidence)
        try:
            connection.send({'type':'response.create','response':{'model':'route-test','input':'hello'}})
            events = []
            for _ in range(25):
                event = connection.receive()
                events.append(event['type'])
                if event['type'] == 'response.completed':
                    break
            require('response.completed' in events, 'responses_ws_terminal_missing')
        finally:
            connection.close()
        after = self.settled(before, 1000)
        self.zero()
        logs = self.rows(f'SELECT request_id,type,quota FROM logs WHERE user_id={int(self.user_id)} '
                         f'AND token_id={int(self.token_id)} AND id>{int(log_cursor)} AND type IN (2,6) ORDER BY id')
        evidence.update(request_id=websocket_request_identity(logs), request_id_source='native_settlement_log',
                        balances_after=after, actual_charge=before['wallet']-after['wallet'])
        rows = self.sim.records(cursor)
        require(len(rows) >= 1 and all(row['channel'] == 'a' and row['auth_valid'] for row in rows), 'responses_ws_wrong_supplier')

    def affinity(self):
        rules = [{'name':'routing-session','model_regex':['^route-test$'],'path_regex':['/v1/chat/completions'],
                  'key_sources':[{'type':'request_header','key':'X-Routing-Session'}],
                  'session_mode':'inherit','include_rule_name':True,'include_model_name':True}]
        self.policy(**{'channel_affinity_setting.enabled':'true','channel_affinity_setting.rules':json.dumps(rules),
                       'channel_affinity_setting.session_mode':'prefer',
                       'channel_affinity_setting.switch_on_success':'true',
                       'channel_affinity_setting.keep_on_channel_disabled':'true'})
        # Private ordered routes intentionally bypass session affinity.
        self.route(mode='auto')
        self.configure('a',priority=10)
        self.configure('b',priority=5)
        headers = {'X-Routing-Session':secrets.token_hex(8)}
        self.check(['a'], headers=headers)
        self.configure('b',priority=20)
        self.check(['a'], headers=headers)
        self.status('a',2)
        self.check(['b'], headers=headers)
        self.policy(**{'channel_affinity_setting.session_mode':'strict'})
        headers = {'X-Routing-Session':secrets.token_hex(8)}
        self.check(['b'], headers=headers)
        self.status('b',2)
        self.check([], status=503, amount=0, headers=headers)
        self.status('b',1)
        self.configure('c',priority=30)
        self.check(['b'],headers=headers)
        # Explicit manual order still wins even while a strict binding exists.
        self.route(['c','b'])
        self.check(['c'],headers=headers)

    def native_remix_probe(self):
        from .routing_tasks import submit
        from candidate import execute
        self.route(['sora'])
        task_id = submit(self,'/v1/tasks/sora',{'model':'sora-2','prompt':'origin','seconds':'4','size':'1280x720'},4000,'sora')
        cursor, before = self.sim.cursor(), self.balances()
        since = datetime.now(timezone.utc).isoformat()
        status, raw = self.request('/v1/videos/'+task_id+'/remix',{'prompt':'continue'},expected_charge=0)
        rows = [row for row in self.sim.records(cursor) if row['method']=='POST']
        error = json.loads(raw).get('error', {})
        require(status == 500 and error.get('type') == 'new_api_panic'
                and 'invalid memory address or nil pointer dereference' in error.get('message','')
                and not rows, 'native_remix_unclassified_result')
        logs = execute(['sudo','-n','docker','--host','unix:///var/run/docker.sock','logs','--since',since,
                        acceptance_target()['container_ids']['app']])
        trace = logs.stdout + logs.stderr
        # Gin omits function names when source files are absent from the image.
        # Bind the frame to the exact nil-promoted-field statement in the
        # fingerprint-verified build source, not just a filename or any 500.
        from routing_result import origin_panic_frame
        source = Path(__file__).resolve().parents[3] / 'relay/relay_task.go'
        frame = origin_panic_frame(trace, source.read_text())
        require(frame is not None, 'native_remix_panic_origin_not_confirmed')
        self.settled(before,0)
        self.zero()
        return {'status':status,'upstream_posts':0,'diagnosis':'nil_ChannelMeta_in_ResolveOriginTask',
                'trace_sha256':hashlib.sha256(trace).hexdigest(), 'source_frame':frame,
                'upstream':['QuantumNous/new-api#4121','QuantumNous/new-api#7676']}

    def run(self):
        cases = [('automatic_eligibility_priority_ratio',self.automatic),('exact_normalized_model',self.exact_model),
                 ('manual_order_disabled_empty_mapping_price',self.manual),('zero_weight_backup',self.zero_weight),
                 ('weight_distribution_audit',self.weight_distribution),('retry_order_error_and_refund',self.retry),
                 ('automatic_health_manual_independence',self.health),('cross_key_concurrency_and_release',self.concurrency),
                 ('native_protocols_streams_and_conversion',self.native_protocols),('responses_websocket',self.websocket),
                 ('prefer_strict_affinity',self.affinity),
                 ]
        from .routing_cases import CASES as extra
        from .routing_tasks import CASES as tasks
        from .routing_cache import real_redis_repair
        cases.append(('real_redis_cache_repair_preserves_reservation', lambda: real_redis_repair(self)))
        cases += [(name,lambda action=action:action(self)) for name,action in (*extra,*tasks)]
        self.required = REQUIRED_CASES
        for name, action in cases:
            self.case(name,action)
        probe = self.case('upstream_native_remix_probe',self.native_remix_probe)
        if probe['status']=='pass':
            probe.update(status='known_upstream',upstream=probe['detail']['upstream'],diagnosis=probe['detail']['diagnosis'])
            for name in BLOCKED_REMIX:
                self.results.append({'name':name,'status':'blocked','blocked_by':'upstream_native_remix_probe'})
        else:
            self.results.extend({'name':name,'status':'not_run','reason':'native_remix_diagnosis_failed'} for name in BLOCKED_REMIX)


def run_routing(args):
    experiment = Routing(args)
    report = {'success':False,'layer':'routing','cases':experiment.results,'statistics':experiment.statistics,
              'limits':['供应商响应为合成协议夹具；不验证真实推理质量。','synthetic_plugin_extension 为原生 API 注册的合成插件扩展合同，与标准协议接入分开。','精确冷却与锁序交由 Go／三库测试。',
                        'WS 续作倍率、数据库不可用、撤销会话及 Token 缓存篡改由 Go/三库合同覆盖，非本层 HTTP 结果。',
                        'WS 握手不返回网关请求 ID；成功 create 从独立日志窗口关联，拒绝且无日志时明确记为不可见。',
                        '普通 HTTP 成功路径的客户端断开不撤销上游 attempt；只验证单次结算及许可释放，不约束断开后错误请求的重试。']}
    try:
        experiment.setup()
        experiment.run()
    except Exception as error:
        report['error_code'] = str(error) if isinstance(error,(RequirementError,APIError)) else type(error).__name__
        report['failure_location'] = [{'file':Path(frame.filename).name,'line':frame.lineno,'function':frame.name}
                                     for frame in traceback.extract_tb(error.__traceback__)[-4:]]
    finally:
        cleanup_errors = []
        try:
            report['requests'] = experiment.sim.records()
        except Exception:
            cleanup_errors.append('read_final_supplier_evidence')
        report['exchanges'] = experiment.exchanges
        if hasattr(experiment,'original_policy'):
            try:
                experiment.root.api('PATCH','/api/option/request_policy',{'options':experiment.original_policy})
            except Exception:
                cleanup_errors.append('restore_request_policy')
        if hasattr(experiment,'original_fetch'):
            for key,value in experiment.original_fetch.items():
                try:
                    experiment.root.api('PUT','/api/option/',{'key':key,'value':value})
                except Exception:
                    cleanup_errors.append('restore_fetch_option')
        if hasattr(experiment,'origin_plugin'):
            try:
                experiment.root.api('POST','/api/plugin/task/'+experiment.origin_plugin+'/status?cascade=true&force=true',{'enabled':False})
            except Exception:
                cleanup_errors.append('disable_origin_fixture_plugin')
        cleanup_errors.extend(experiment.cleanup())
        report['cleanup_errors'] = cleanup_errors
        report.update(evaluate(experiment.results,getattr(experiment,'required', ['setup']),cleanup_errors))
        if report.get('error_code'):
            report.update(success=False,contracts_success=False,exit_code=1)
        atomic_private(args.report,json.dumps(report,indent=2,ensure_ascii=False))
    return report
