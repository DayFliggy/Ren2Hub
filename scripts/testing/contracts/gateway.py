"""NewAPI 核心候选验收。只接受已验证的隔离目标和回环模拟供应商。"""
import base64
import hashlib
import http.server
import json
import math
import os
import re
from pathlib import Path
import secrets
import subprocess
import threading
import time
import traceback

from .common import (RequirementError, acceptance_target, atomic_private,
    native_balances, operation_proof, candidate_query,
    regular_file, require, require_charge, save_model_pricing, verify_candidate_database)
from .client import APIError, Client
from .websocket import WebSocket, credentials, logout_verified


def upgrade_attachment_state(args):
    if getattr(args, 'upgrade_attachment_phase', None) != 'verify':
        return None
    path = args.upgrade_attachment_state
    require(path.is_file(), 'upgrade_attachment_state_required')
    state = json.loads(regular_file(path, private=True).read_text())
    previous = json.loads(regular_file(args.previous_target, private=True).read_text())
    current = acceptance_target()
    expected = dict(current)
    expected.update(revision=previous.get('revision'), image_id=previous.get('image_id'),
                    container_ids={**current['container_ids'], 'app': previous.get('container_ids', {}).get('app')})
    require(previous == expected and previous.get('revision') != args.revision
            and state.get('format') == 1 and state.get('acceptance_target') == previous,
            'upgrade_attachment_target_mismatch')
    require(type(state.get('ticket_id')) is int and state['ticket_id'] > 0
            and isinstance(state.get('attachment_path'), str)
            and re.fullmatch(r'/api/tickets/attachments/[1-9][0-9]*', state['attachment_path'])
            and isinstance(state.get('sha256'), str) and re.fullmatch(r'[0-9a-f]{64}', state['sha256']),
            'upgrade_attachment_record_invalid')
    for role in ('owner', 'foreign'):
        account = state.get(role, {})
        require(type(account.get('id')) is int and account['id'] > 0
                and all(isinstance(account.get(key), str) and account[key] for key in ('username', 'password')),
                'upgrade_attachment_account_invalid')
    require(state['owner']['id'] != state['foreign']['id'], 'upgrade_attachment_owner_must_differ')
    return state


class Experiment:
    def __init__(self, args):
        verify_candidate_database(args.revision)
        self.args, self.revision = args, args.revision
        # Validate provenance before any login or synthetic account creation.
        self.saved_attachment = upgrade_attachment_state(args)
        self.upgrade_clients = []
        self.base = 'http://127.0.0.1:3000'
        self.root, self.user, self.other = (Client(self.base) for _ in range(3))
        self.suffix = secrets.token_hex(4)
        self.checks, self.channels, self.tokens = [], [], []
        self.sends, self.mode, self.mock_error = 0, 'success', None
        self.model = 'core-' + self.suffix
        self.mock_key = secrets.token_urlsafe(24)
        self.lock = threading.Lock()
        self.hold_port = None
        self.hold_entered, self.hold_release = threading.Event(), threading.Event()
        self.mock_tls_cert = args.mock_tls_cert
        self.mock_tls_key = args.mock_tls_key
        self.traffic = None

    def rows(self, sql):
        return candidate_query(sql)

    def record(self, name):
        self.checks.append(name)

    def initialize(self):
        if self.args.credentials:
            secret = credentials(self.args.credentials)
        else:
            require(self.args.credentials_output is not None, 'credentials_or_output_required')
            require(not self.args.credentials_output.exists(), 'credentials_output_exists')
            setup = self.root.api('GET', '/api/setup')
            require(not setup['status'] and not setup['root_init'], 'fresh_candidate_required')
            secret = {'ren2hub_username': 'coreadmin', 'ren2hub_password': secrets.token_urlsafe(32)}
            atomic_private(self.args.credentials_output, json.dumps(secret))
            self.root.api('POST', '/api/setup', {'username': secret['ren2hub_username'],
                'password': secret['ren2hub_password'], 'confirmPassword': secret['ren2hub_password'],
                'SelfUseModeEnabled': False, 'DemoSiteEnabled': False})
        self.root_password = secret['ren2hub_password']
        self.root.login(secret['ren2hub_username'], self.root_password)
        for client, prefix in [(self.user, 'u'), (self.other, 'v')]:
            password = secrets.token_urlsafe(32)
            name = prefix + self.suffix
            self.root.api('POST', '/api/user/', {'username': name, 'password': password,
                'role': 1, 'status': 1, 'display_name': name})
            user = client.login(name, password)
            if client is self.user:
                self.user_id, self.user_name, self.user_password = user['id'], name, password
            else:
                self.other_id, self.other_name, self.other_password = user['id'], name, password
        self.root.api('POST', '/api/user/manage', {'id': self.user_id, 'action': 'add_quota', 'mode': 'override',
            'value': 1000000, 'operation_id': f'core-init-{self.suffix}'})
        self.record('native_initialization_login_and_numeric_wallet')

    def add_channel(self, port, name=None, channel_type=1, settings=None):
        name = name or self.model
        payload = {'name': name, 'type': channel_type, 'key': self.mock_key,
            'base_url': f'http://127.0.0.1:{port}', 'models': self.model,
            'status': 1, 'priority': 0, 'weight': 100,
            'setting': json.dumps({'responses_websocket_enabled': True, **(settings or {})})}
        self.root.api('POST', '/api/channel/', {'mode': 'single', 'channel': payload})
        rows = self.rows("SELECT id FROM channels WHERE name='" + name + "'")
        require(len(rows) == 1, 'mock_channel_lookup_failed')
        channel_id = rows[0]['id']
        self.channels.append(channel_id)
        # Native creation persists abilities; saving the configuration refreshes the selection cache.
        self.root.api('PUT', '/api/channel/', {'id': channel_id, **{key: value for key, value in payload.items() if key != 'status'}})
        return channel_id

    def add_token(self, mode='auto'):
        self.user.api('POST', '/api/token/', {'name': self.model, 'remain_quota': 1000000,
            'expired_time': -1, 'unlimited_quota': False, 'type': mode,
            'model_limits_enabled': True, 'model_limits': self.model})
        items = self.user.api('GET', '/api/token/?p=1&page_size=100')['items']
        item = next(row for row in items if row['name'] == self.model)
        self.token_id = item['id']
        self.tokens.append(self.token_id)
        self.key = self.user.api('POST', f'/api/token/{self.token_id}/key')['key']
        return self.token_id

    def routing(self, ids):
        route = self.user.api('GET', f'/api/token/{self.token_id}/routing')
        return self.user.api('PUT', f'/api/token/{self.token_id}/routing', {'mode': 'manual',
            'version': route['version'], 'entries': [{'channel_id': cid, 'enabled': True} for cid in ids]})

    def price(self):
        save_model_pricing(self.root, self.model, {'ModelPrice': 0.002})

    def balances(self):
        return native_balances(self.user_id, self.token_id)

    def settled(self, before, amount, timeout=10):
        deadline = time.monotonic() + timeout
        while time.monotonic() < deadline:
            after = self.balances()
            try:
                require_charge(before, after, amount)
                return after
            except RequirementError:
                time.sleep(0.05)
        require_charge(before, self.balances(), amount)

    def relay(self, path='/v1/chat/completions', **extra):
        body = {'model': self.model, 'messages': [{'role': 'user', 'content': 'local acceptance'}], 'max_tokens': 8}
        body.update(extra)
        return self.user.request('POST', path, body, bearer=self.key)

    def handler(self):
        experiment = self
        class Mock(http.server.BaseHTTPRequestHandler):
            protocol_version = 'HTTP/1.1'
            def log_message(self, *_): pass
            def do_POST(self):
                try:
                    require(self.headers.get('Authorization') == 'Bearer ' + experiment.mock_key, 'wrong_mock_key')
                    length = int(self.headers.get('Content-Length', '0'))
                    require(0 < length < 1048576, 'mock_body_size')
                    body = json.loads(self.rfile.read(length))
                    require(body.get('model') == experiment.model, 'mock_model_mismatch')
                    with experiment.lock: experiment.sends += 1
                    if self.server.server_port == experiment.hold_port:
                        experiment.hold_entered.set()
                        require(experiment.hold_release.wait(15), 'held_attempt_not_released')
                    status, content_type = 200, 'application/json'
                    if experiment.mode == 'fail':
                        status, value = 503, {'error': {'message': 'local retry fixture', 'type': 'server_error', 'code': 'local_failure'}}
                    elif self.path == '/v1/responses/compact':
                        value = {'id':'cmp_'+experiment.suffix, 'object':'response.compaction', 'created_at':int(time.time()), 'model':experiment.model,
                            'output':[{'type':'compaction','id':'cmpitem','encrypted_content':'local-data'}],
                            'usage':{'input_tokens':10,'output_tokens':2,'total_tokens':12}}
                    elif self.path == '/v1/responses':
                        require(body.get('input') == 'local acceptance', 'mock_responses_input_mismatch')
                        part = {'type':'output_text', 'text':'local answer', 'annotations':[]}
                        item = {'type':'message', 'id':'msg_'+experiment.suffix, 'status':'completed', 'role':'assistant', 'content':[part]}
                        value = {'id':'resp_'+experiment.suffix, 'object':'response', 'created_at':int(time.time()),
                            'model':experiment.model, 'status':'completed', 'error':None, 'incomplete_details':None,
                            'output':[item], 'parallel_tool_calls':True, 'tools':[], 'tool_choice':'auto',
                            'usage':{'input_tokens':10,'input_tokens_details':{'cached_tokens':0},
                                'output_tokens':2,'output_tokens_details':{'reasoning_tokens':0},'total_tokens':12}}
                        if body.get('stream'):
                            content_type = 'text/event-stream'
                            in_progress = {**value, 'status':'in_progress', 'output':[], 'usage':None}
                            events = [
                                {'type':'response.created', 'response':in_progress},
                                {'type':'response.output_item.added', 'output_index':0, 'item':{**item, 'status':'in_progress', 'content':[]}},
                                {'type':'response.content_part.added', 'item_id':item['id'], 'output_index':0, 'content_index':0, 'part':{**part, 'text':''}},
                                {'type':'response.output_text.delta', 'item_id':item['id'], 'output_index':0, 'content_index':0, 'delta':'local answer'},
                                {'type':'response.output_text.done', 'item_id':item['id'], 'output_index':0, 'content_index':0, 'text':'local answer'},
                                {'type':'response.content_part.done', 'item_id':item['id'], 'output_index':0, 'content_index':0, 'part':part},
                                {'type':'response.output_item.done', 'output_index':0, 'item':item},
                                {'type':'response.completed', 'response':value},
                            ]
                            value = ''.join('event: '+event['type']+'\ndata: '+json.dumps({**event, 'sequence_number':index})+'\n\n'
                                for index, event in enumerate(events))
                    else:
                        require(self.path == '/v1/chat/completions', 'mock_path_mismatch')
                        value = {'id':'chatcmpl_'+experiment.suffix, 'object':'chat.completion', 'created':int(time.time()), 'model':experiment.model,
                            'choices':[{'index':0,'message':{'role':'assistant','content':'local answer'},'finish_reason':'stop'}],
                            'usage':{'prompt_tokens':10,'completion_tokens':2,'total_tokens':12}}
                        if body.get('stream'):
                            content_type = 'text/event-stream'
                            chunk = {**value, 'object':'chat.completion.chunk','choices':[{'index':0,'delta':{'content':'local answer'},'finish_reason':None}]}
                            value = 'data: '+json.dumps(chunk)+'\n\ndata: '+json.dumps({**chunk,'choices':[{'index':0,'delta':{},'finish_reason':'stop'}]})+'\n\ndata: [DONE]\n\n'
                    raw = value.encode() if isinstance(value,str) else json.dumps(value).encode()
                    self.send_response(status);self.send_header('Content-Type',content_type);self.send_header('Content-Length',str(len(raw)));self.end_headers();self.wfile.write(raw)
                except Exception as error:
                    experiment.mock_error = type(error).__name__
                    self.close_connection = True
            def do_GET(self):
                try:
                    require(self.path == '/v1/responses' and self.headers.get('Authorization') == 'Bearer '+experiment.mock_key, 'mock_ws_identity')
                    key = self.headers['Sec-WebSocket-Key']
                    accept = base64.b64encode(hashlib.sha1((key+'258EAFA5-E914-47DA-95CA-C5AB0DC85B11').encode()).digest()).decode()
                    self.send_response(101);self.send_header('Upgrade','websocket');self.send_header('Connection','Upgrade');self.send_header('Sec-WebSocket-Accept',accept);self.end_headers()
                    socket = WebSocket(self.connection,self.rfile,False)
                    self.connection.settimeout(20)
                    while True:
                        event = socket.receive()
                        if event is None: break
                        body = event
                        require(body.get('type') == 'response.create' and body.get('model') == experiment.model, 'mock_ws_request')
                        with experiment.lock: experiment.sends += 1
                        response = {'id':'resp_'+secrets.token_hex(5),'object':'response','model':experiment.model,'created_at':int(time.time()),'status':'in_progress','output':[]}
                        socket.send({'type':'response.created','response':response})
                        socket.send({'type':'response.output_text.delta','response_id':response['id'],'item_id':'msg_1','output_index':0,'content_index':0,'delta':'local answer'})
                        response.update(status='completed',output=[{'type':'message','id':'msg_1','status':'completed','role':'assistant','content':[{'type':'output_text','text':'local answer','annotations':[]}]}],usage={'input_tokens':10,'output_tokens':2,'total_tokens':12})
                        socket.send({'type':'response.completed','response':response})
                except (EOFError, ConnectionError, TimeoutError): pass
                except Exception as error: experiment.mock_error = type(error).__name__
                finally: self.close_connection = True
        return Mock

    def billing(self):
        self.price()
        for streaming in [False, True]:
            before = self.balances(); sends = self.sends
            status, raw = self.relay(stream=streaming, **({'stream_options':{'include_usage':True}} if streaming else {}))
            require(status == 200 and b'local answer' in raw, 'native_relay_failed')
            require(self.sends == sends+1, 'relay_replayed')
            self.settled(before,1000)
            self.record('native_sse_settlement' if streaming else 'native_http_settlement')
        before = self.balances(); sends = self.sends; self.mode='fail'
        status,_ = self.relay(); self.mode='success'
        require(status >= 400, 'upstream_failure_hidden')
        require(self.sends == sends + 1, 'failed_upstream_must_send_once')
        self.settled(before,0)
        self.record('failed_upstream_refunds_wallet_and_token')
        logs=self.rows(f"SELECT quota FROM logs WHERE type=2 AND user_id={self.user_id} AND model_name='{self.model}'")
        require(len(logs)==2 and all(row['quota']==1000 for row in logs), 'consume_log_contract')
        self.record('one_native_consume_log_per_success')
        from .channels import run_channel_contracts
        run_channel_contracts(self)

    def compact(self):
        self.price(); before=self.balances(); sends=self.sends
        status,raw=self.user.request('POST','/v1/responses/compact',{'model':self.model,'input':'local acceptance'},bearer=self.key)
        require(status==200 and json.loads(raw).get('object')=='response.compaction','compact_response')
        require(self.sends==sends+1,'compact_replayed');self.settled(before,1000)
        self.record('compact_single_upstream_and_native_settlement')

    def sdk(self):
        """One native charge per official SDK call; credentials only cross stdin."""
        self.price()
        scripts = Path(__file__).resolve().parent
        pinned = json.loads((scripts / 'sdk-versions.json').read_text())
        runtimes = [('python', os.environ.get('GATEWAY_SDK_PYTHON'), scripts / 'sdk_python.py'),
                    ('node', os.environ.get('GATEWAY_SDK_NODE'), scripts / 'sdk_node.mjs')]
        for language, executable, _ in runtimes:
            require(executable and Path(executable).is_absolute() and os.access(executable, os.X_OK),
                    'explicit_sdk_' + language + '_runtime_required')
        module = os.environ.get('GATEWAY_SDK_NODE_MODULE', '')
        require(module and Path(module).is_absolute() and Path(module).exists(), 'explicit_sdk_node_module_required')
        env = {key: value for key, value in os.environ.items()
               if not key.upper().startswith('OPENAI_') and key.lower() not in ('http_proxy', 'https_proxy', 'all_proxy')}
        for language, executable, script in runtimes:
            version = None
            metadata_hash = None
            for case in ('chat_json', 'chat_sse', 'responses_json', 'responses_sse'):
                before, sends = self.balances(), self.sends
                previous = self.rows(f'SELECT request_count FROM users WHERE id={self.user_id}')
                require(len(previous) == 1, 'sdk_native_user_missing')
                payload = {'base_url':self.base+'/v1', 'api_key':self.key, 'model':self.model, 'case':case}
                result = subprocess.run([executable, str(script)], input=json.dumps(payload), text=True,
                    stdout=subprocess.PIPE, stderr=subprocess.PIPE, timeout=45, env=env, check=False)
                require(result.returncode == 0 and len(result.stdout) <= 16384, 'sdk_' + language + '_' + case + '_failed')
                output = json.loads(result.stdout)
                require(output.get('success') is True and output.get('sdk') == language and output.get('case') == case
                        and output.get('requests') == 1 and output.get('max_retries') == 0,
                        'sdk_result_contract_mismatch')
                require(output.get('text') == 'local answer' and output.get('usage') == {'input_tokens':10,'output_tokens':2,'total_tokens':12}
                        and output.get('terminal') == ('stop' if case.startswith('chat_') else 'completed'),
                        'sdk_decoded_result_mismatch')
                identity = (output.get('sdk_version'), output.get('runtime_version'))
                require(all(isinstance(value,str) and 0 < len(value) < 64 for value in identity), 'sdk_version_missing')
                require(identity[0] == pinned[language]['version'], 'sdk_pinned_version_mismatch')
                require(version is None or identity == version, 'sdk_runtime_changed_between_calls')
                version = identity
                metadata = output.get('package_metadata_sha256', '')
                require(re.fullmatch(r'[0-9a-f]{64}', metadata), 'sdk_package_metadata_hash_required')
                require(metadata_hash is None or metadata_hash == metadata, 'sdk_package_metadata_changed_between_calls')
                metadata_hash = metadata
                require(self.sends == sends+1 and self.mock_error is None, 'sdk_upstream_send_contract')
                self.settled(before,1000)
                current = self.rows(f'SELECT request_count FROM users WHERE id={self.user_id}')
                require(current == [{'request_count':previous[0]['request_count']+1}], 'sdk_native_request_count_mismatch')
                self.checks.append({'name':'official_sdk_'+case, 'sdk':language, 'sdk_version':identity[0],
                    'runtime_version':identity[1], 'package_metadata_sha256':metadata, 'max_retries':0, 'upstream_requests':1, 'native_quota':1000,
                    'native_request_count_delta':1})

    def websocket(self):
        self.price(); self.routing([self.channel_id])
        socket=WebSocket.connect(self.base,self.key)
        try:
            for _ in range(2):
                before=self.balances();sends=self.sends
                socket.send({'type':'response.create','model':self.model,'input':'local acceptance'})
                seen=[]
                for _ in range(12):
                    frame=socket.receive();require(frame is not None,'ws_closed_before_terminal')
                    event=frame;seen.append(event['type'])
                    if event['type']=='response.completed':break
                require(seen[-1]=='response.completed' and 'response.output_text.delta' in seen,'ws_terminal_missing')
                self.settled(before,1000);require(self.sends==sends+1,'ws_accepted_request_replayed')
            self.record('ws_two_creates_single_charge_each')
            self.routing([]);before=self.balances();sends=self.sends
            socket.send({'type':'response.create','model':self.model,'input':'must reject'})
            try: frame=socket.receive()
            except EOFError: frame=None
            require(frame is None or frame.get('type') in ('error','response.failed'),'ws_stale_private_route_accepted')
            require(self.sends==sends,'ws_changed_route_sent');self.settled(before,0)
            self.record('ws_private_route_revalidated_each_create')
        finally:socket.close()

    def traffic_source(self, timeout=45):
        """Root 流量来源与候选参数一致。

        监听器模式下本组轮询、健康检查都经过主监听器，两个方向必须大于 0，主监听器漏接计数时速率恒为 0。
        宿主模式下候选在 internal 网络里，它的请求不经过宿主网卡，0 是有效读数。
        """
        interface = acceptance_target()['traffic_host_interface']
        expected = ('host_interface', interface) if interface else ('application_listener', '')
        deadline = time.monotonic() + timeout
        while True:
            observation = self.root.api('GET', '/api/performance/stats')['observability']
            traffic = observation['traffic']
            require((traffic['scope'], traffic['interface']) == expected, 'traffic_source_mismatch')
            require(traffic['reason'] == '', 'traffic_reason_reported')
            rates = (traffic['inbound_mbps'], traffic['outbound_mbps'])
            # 采样间隔 10 秒，两次采样后才有速率。
            if None not in rates:
                require(all(type(rate) in (int, float) and math.isfinite(rate) and rate >= 0 for rate in rates),
                        'traffic_rate_invalid')
                if interface or min(rates) > 0:
                    break
            require(time.monotonic() < deadline,
                    'traffic_rates_unavailable' if None in rates else 'listener_traffic_not_counted')
            time.sleep(2)
        require(observation['stale'] is False, 'traffic_observation_stale')
        self.traffic = {'scope': traffic['scope'], 'interface': traffic['interface']}
        self.record('traffic_source_matches_configuration')

    def management(self):
        self.traffic_source()
        self.price()
        for path in ['/api/subscription/plans','/api/deployments','/api/system-info','/api/next/admin/users','/api/billing/self/events','/pg/chat/completions']:
            status,_=self.root.request('GET',path);require(status==404,'retired_route_'+path)
        self.record('retired_routes_unavailable')
        before=self.balances();self.routing([]);sends=self.sends
        require(self.relay()[0]>=400,'empty_private_route_accepted')
        require(self.sends==sends,'private_route_fell_back_to_auto');self.settled(before,0)
        route=self.routing([self.channel_id])
        status,_=self.user.request('PUT',f'/api/token/{self.token_id}/routing',{'mode':'manual','version':route['version']-1,'entries':[]})
        require(status==409,'stale_route_revision_accepted')
        require(self.other.request('GET',f'/api/token/{self.token_id}/routing')[0] in (403,404),'foreign_route_exposed')
        require(self.relay()[0]==200,'private_route_no_longer_eligible');self.settled(before,1000)
        self.record('private_routing_ownership_empty_list_version_and_delivery')
        context={'scopes':['profile:read'],'expires_at':0}
        proof=operation_proof(self.user,'access_token.generate',self.user_password,context)
        created=self.user.api('POST','/api/user/access_tokens',{'name':'core-'+self.suffix,**context},proof=proof)
        pat=created['token'];identifier=created['item']['id']
        require(self.user.request('GET','/api/user/self',bearer=pat)[0]==200,'pat_granted_scope_failed')
        require(self.user.request('GET','/api/token/',bearer=pat)[0]==403,'pat_scope_bypass')
        listing=self.user.api('GET','/api/user/access_tokens');require(pat not in json.dumps(listing),'pat_secret_exposed')
        proof=operation_proof(self.user,'access_token.revoke',self.user_password,{'token_id':identifier})
        self.user.api('DELETE',f'/api/user/access_tokens/{identifier}',proof=proof)
        require(self.user.request('GET','/api/user/self',bearer=pat)[0]==401,'revoked_pat_accepted')
        self.record('native_pat_scope_proof_secret_and_revocation')
        from .extensions import run_extensions
        run_extensions(self)
        from .affiliate import run_affiliate
        run_affiliate(self)
        if getattr(self.args, 'upgrade_attachment_phase', None) == 'capture':
            self.capture_upgrade_attachment()

    def capture_upgrade_attachment(self):
        from .extensions import PNG
        path = self.args.upgrade_attachment_state
        require(not path.exists() and not path.is_symlink(), 'new_upgrade_attachment_state_required')
        rows = self.rows(f'SELECT id FROM tickets WHERE user_id={int(self.user_id)} AND deleted_at IS NULL')
        require(len(rows) == 1, 'upgrade_attachment_ticket_not_unique')
        ticket_id = int(rows[0]['id'])
        detail = self.user.api('GET', f'/api/tickets/{ticket_id}')
        images = [image for message in detail['messages'] for image in message['images']]
        require(len(images) == 1 and re.fullmatch(r'/api/tickets/attachments/[1-9][0-9]*', images[0]),
                'upgrade_attachment_fixture_missing')
        status, content = self.user.request('GET', images[0])
        require(status == 200 and content == PNG, 'upgrade_attachment_fixture_content_mismatch')
        state = {'format': 1, 'acceptance_target': acceptance_target(), 'ticket_id': ticket_id,
                 'attachment_path': images[0], 'sha256': hashlib.sha256(content).hexdigest(),
                 'owner': {'id': self.user_id, 'username': self.user_name, 'password': self.user_password},
                 'foreign': {'id': self.other_id, 'username': self.other_name, 'password': self.other_password}}
        atomic_private(path, json.dumps(state))
        self.record('old_ticket_attachment_saved_for_upgrade')

    def verify_upgrade_attachment(self):
        state = self.saved_attachment
        if state is None:
            return
        rows = self.rows(f"SELECT user_id FROM tickets WHERE id={int(state['ticket_id'])} AND deleted_at IS NULL")
        require(len(rows) == 1, 'upgrade_attachment_ticket_missing')
        require(rows[0]['user_id'] == state['owner']['id'], 'upgrade_attachment_owner_changed')
        from .extensions import rejected
        owner, foreign = Client(self.base), Client(self.base)
        self.upgrade_clients.extend((owner, foreign))
        for client, role in ((owner, 'owner'), (foreign, 'foreign')):
            account = state[role]
            user = client.login(account['username'], account['password'])
            require(user['id'] == account['id'], 'upgrade_attachment_account_identity_changed')
        path = f"/api/tickets/{state['ticket_id']}"
        detail = owner.api('GET', path)
        require(detail['ticket']['id'] == state['ticket_id']
                and state['attachment_path'] in [image for message in detail['messages'] for image in message['images']],
                'upgrade_attachment_mapping_changed')
        status, content = owner.request('GET', state['attachment_path'])
        require(status == 200 and hashlib.sha256(content).hexdigest() == state['sha256'],
                'upgrade_attachment_content_changed')
        rejected(foreign.request('GET', path), 'upgrade_ticket_cross_user_read', 'NOT_FOUND')
        rejected(foreign.request('GET', state['attachment_path']), 'upgrade_attachment_cross_user_read', 'NOT_FOUND')
        self.record('old_ticket_attachment_content_and_owner_boundary_survive_upgrade')

    def cleanup(self):
        failures=[]
        for token_id in self.tokens:
            try:self.user.api('PUT','/api/token/?status_only=true',{'id':token_id,'status':2})
            except Exception:failures.append('disable_token')
        for client in [self.root,self.user,self.other,*self.upgrade_clients]:
            if client.bearer:
                try:logout_verified(client)
                except Exception:failures.append('revoke_session')
        return failures


def run_group(group, args):
    os.umask(0o077)
    descriptor=os.open(args.report,os.O_WRONLY|os.O_CREAT|os.O_EXCL|getattr(os,'O_NOFOLLOW',0),0o600)
    report={'success':False,'group':group,'source_sha':args.revision,'checks':[]}
    experiment=server=None
    try:
        experiment=Experiment(args)
        experiment.verify_upgrade_attachment()
        experiment.initialize()
        server=http.server.ThreadingHTTPServer(('127.0.0.1',0),experiment.handler())
        thread=threading.Thread(target=server.serve_forever,daemon=True);thread.start()
        experiment.channel_id=experiment.add_channel(server.server_port)
        experiment.add_token()
        getattr(experiment,group)()
        require(experiment.mock_error is None,'mock_protocol_contract_failed')
        report.update(success=True,database=acceptance_target()['database_name'],provider='loopback mock',upstream_requests=experiment.sends)
    except Exception as error:
        report.update(error_type=type(error).__name__,error=str(error) if isinstance(error,(RequirementError,APIError)) else 'candidate_contract_failed',error_line=traceback.extract_tb(error.__traceback__)[-1].lineno)
    finally:
        if experiment:
            report['checks']=experiment.checks
            if experiment.traffic:report['traffic']=experiment.traffic
            errors=experiment.cleanup()
            if errors:report.update(success=False,cleanup_errors=errors)
        if server:server.shutdown();server.server_close()
        with os.fdopen(descriptor,'w') as output:json.dump(report,output,indent=2);output.write('\n')
    return report
