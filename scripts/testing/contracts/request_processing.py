"""新请求处理与通用提供商账号的真实候选 API 合同，仅使用合成凭据。"""
import hashlib
import json
from contextlib import contextmanager
import secrets
import time

from .common import atomic_private, operation_proof, regular_file, require
from .client import Client
from .extensions import business, rejected
from .websocket import logout_verified


@contextmanager
def root_pat(experiment, scopes):
    root = experiment.root
    context = {'scopes': scopes, 'expires_at': int(time.time()) + 7200}
    proof = operation_proof(root, 'access_token.generate', experiment.root_password, context)
    token = root.api('POST', '/api/user/access_tokens',
                     {'name': 'plugin-' + experiment.suffix, **context}, proof=proof)
    try:
        yield token['token']
    finally:
        identifier = int(token['item']['id'])
        proof = operation_proof(root, 'access_token.revoke', experiment.root_password, {'token_id': identifier})
        root.api('DELETE', f'/api/user/access_tokens/{identifier}', proof=proof)


def check_instance_permissions(experiment, instances):
    root = experiment.root
    path = '/api/request_plugin/instances'
    with root_pat(experiment, ['task_plugin:bind']) as pat:
        read = business(root.request('GET', path, bearer=pat), 'request_plugin_binding_pat_read')
        require(read['instances'] == instances, 'request_plugin_binding_pat_read_changed_values')
        for method, target, body in [('PUT', path, {'instances': instances}),
                                     ('POST', '/api/request_plugin/dry_run', {})]:
            require(root.request(method, target, body, bearer=pat)[0] == 403,
                    'request_plugin_binding_pat_wrote')
    with root_pat(experiment, ['plugin:write']) as pat:
        require(root.request('GET', path, bearer=pat)[0] == 403, 'request_plugin_write_pat_read')
        business(root.request('PUT', path, {'instances': instances}, bearer=pat), 'request_plugin_write_pat_save')
    admin, password = Client(experiment.base), secrets.token_urlsafe(32)
    name = 'pa' + experiment.suffix
    proof = operation_proof(root, 'admin.user.create', experiment.root_password, {'role': 10})
    root.api('POST', '/api/user/', {'username': name, 'password': password, 'role': 10,
             'admin_permissions': {'task_plugin': {'bind': True}}}, proof=proof)
    try:
        admin.login(name, password)
        read = admin.api('GET', path)['instances']
        require(len(read) == len(instances) and all(item.get('config') is None for item in read),
                'request_plugin_admin_instance_config_exposed')
        require(admin.request('PUT', path, {'instances': instances})[0] == 403
                and admin.request('POST', '/api/request_plugin/dry_run', {})[0] == 403,
                'request_plugin_admin_root_boundary_bypassed')
    finally:
        if admin.bearer:
            logout_verified(admin)
    experiment.record('request_plugin_pat_scopes_and_admin_config_redaction')


def seed_sensitive_migration(experiment):
    marker = 'candidate-migration-' + experiment.suffix
    values = {'SensitiveWords': ' ' + marker + '\n ', 'CheckSensitiveEnabled': 'true',
              'CheckSensitiveOnPromptEnabled': 'true', 'StopOnSensitiveEnabled': 'false',
              'StreamCacheQueueLength': '7'}
    for key, value in values.items():
        experiment.root.api('PUT', '/api/option/', {'key': key, 'value': value})
    path = experiment.args.upgrade_attachment_state.with_name('upgrade-sensitive.json')
    require(not path.exists(), 'upgrade_sensitive_fixture_already_exists')
    atomic_private(path, json.dumps({'keyword': marker}))
    experiment.record('old_sensitive_settings_saved_for_upgrade')


def assert_filtered(experiment, word):
    before, sends = experiment.balances(), experiment.sends
    status, raw = experiment.relay(messages=[{'role': 'user', 'content': word.upper()}])
    require(status == 400 and json.loads(raw).get('error', {}).get('code') == 'request_rejected',
            'request_filter_did_not_reject')
    require(experiment.sends == sends, 'rejected_request_reached_provider')
    experiment.settled(before, 0)


def verify_migrated_filter(experiment):
    path = experiment.args.upgrade_attachment_state.with_name('upgrade-sensitive.json')
    saved = json.loads(regular_file(path, private=True).read_text())
    require(set(saved) == {'keyword'} and isinstance(saved['keyword'], str)
            and saved['keyword'].startswith('candidate-migration-'), 'upgrade_sensitive_fixture_invalid')
    instances = experiment.root.api('GET', '/api/request_plugin/instances')['instances']
    matched = [item for item in instances if item['plugin'] == 'request-filter'
               and item['config'].get('keywords') == saved['keyword']]
    require(len(matched) == 1 and matched[0]['enabled'] is True and matched[0]['global'] is True,
            'migrated_sensitive_instance_missing_or_duplicate')
    assert_filtered(experiment, saved['keyword'])
    experiment.record('migrated_sensitive_filter_rejects_without_send_or_charge')


def run_request_processing(experiment):
    root, user = experiment.root, experiment.user
    original = root.api('GET', '/api/request_plugin/instances')['instances']
    marker = 'request-filter-' + experiment.suffix
    item = {'name': 'Candidate request filter', 'plugin': 'request-filter', 'enabled': True,
            'global': True, 'models': [experiment.model],
            'config': {'rules': [], 'keywords': marker, 'keywordsCaseSensitive': False,
                       'message': '', 'replies': True}}
    before = experiment.balances()
    for method, path, data in [('GET', '/api/request_plugin/instances', None),
                               ('PUT', '/api/request_plugin/instances', {'instances': []}),
                               ('POST', '/api/request_plugin/dry_run', {})]:
        require(user.request(method, path, data)[0] == 403, 'request_plugin_user_permission_bypass')
    experiment.record('request_plugin_administration_rejects_regular_user')
    try:
        saved = root.api('PUT', '/api/request_plugin/instances', {'instances': [*original, item]})['instances']
        require(saved[:-1] == original and saved[-1]['models'] == [experiment.model],
                'request_plugin_save_changed_existing_instances')
        check_instance_permissions(experiment, saved)
        draft = {'format': 'openai', 'path': '/v1/chat/completions', 'model': experiment.model,
                 'body': {'model': experiment.model, 'messages': [{'role': 'user', 'content': marker}]}}
        dry = root.api('POST', '/api/request_plugin/dry_run', draft)
        require(dry['rejected'] is not None and dry['failed'] is None, 'request_filter_dry_run_failed')
        assert_filtered(experiment, marker)
        draft['body']['messages'][0]['content'] = 'ordinary candidate request'
        draft['reply'] = marker
        dry = root.api('POST', '/api/request_plugin/dry_run', draft)
        require(dry['rejected'] is None and dry['failed'] is None
                and dry['reply']['stopped'] is not None, 'request_filter_reply_dry_run_failed')
        experiment.settled(before, 0)
        experiment.record('request_filter_request_reply_dry_run_and_real_rejection_no_charge')
        root.api('PUT', '/api/request_plugin/instances', {'instances': original})
        before = experiment.balances()
        require(experiment.relay()[0] == 200, 'request_filter_removal_did_not_restore_relay')
        experiment.settled(before, 1000)
        experiment.record('request_filter_removal_restores_normal_settlement')
    finally:
        root.api('PUT', '/api/request_plugin/instances', {'instances': original})


LOGIN_SOURCE = '''
export const meta = {apiVersion: 1, key: "%KEY%", name: "Candidate provider login", version: "1.0.0",
 author: {name: "Candidate"}, models: ["%MODEL%"],
 relay: [{incoming_path: "/v1/chat/completions", upstream_path: "/v1/chat/completions"}]};
export function login(ctx, input) {
 if (input === null) return {type: "fields", session: {secret: "candidate-session-secret"},
  fields: [{name: "api_token", label: "Token", type: "password", required: true}]};
 return {type: "done", credential: {token: input.fields.api_token},
  headers: {Authorization: "Bearer unrefreshed"}, expiresAt: 1,
  accountId: "candidate-account", label: "Candidate account"};
}
export function refresh(ctx) {
 return {credential: ctx.credential, headers: {Authorization: "Bearer " + ctx.credential.token},
  expiresAt: Math.floor(Date.now() / 1000) + 3600, accountId: "candidate-account", label: "Refreshed account"};
}
'''


def run_provider_login(experiment):
    root, user = experiment.root, experiment.user
    key = 'candidate_provider_' + experiment.suffix
    source = LOGIN_SOURCE.replace('%KEY%', key).replace('%MODEL%', experiment.model)
    from .marketplace import upload_community_plugin
    upload_community_plugin(experiment, {'source': source, 'enabled': True,
             'sourceSha256': hashlib.sha256(source.encode()).hexdigest(), 'remark': 'isolated candidate'})
    ordinary = root.api('GET', '/api/channel/' + str(experiment.channel_id))
    result = root.api('POST', '/api/channel/', {'mode': 'single', 'channel': {
        'name': key, 'type': 61, 'key': '', 'models': experiment.model, 'base_url': ordinary['base_url'],
        'status': 1, 'priority': 0, 'weight': 100, 'setting': json.dumps({'task_plugin_key': key})}})
    require(len(result['ids']) == 1, 'provider_channel_create_missing_id')
    channel = result['ids'][0]
    login = f'/api/channel/{channel}/plugin_login'
    summary = f'/api/channel/{channel}/plugin_credentials'
    another_session = Client(experiment.base)
    try:
        require(user.request('POST', login, {})[0] == 403
                and user.request('GET', summary)[0] == 403, 'provider_login_permission_bypass')
        flow = root.api('POST', login, {})
        require(flow['step']['type'] == 'fields' and 'candidate-session-secret' not in json.dumps(flow),
                'provider_login_fields_or_secret_boundary_failed')
        another_session.login(root.api('GET', '/api/user/self')['username'], experiment.root_password)
        submitted = {'flow': flow['flow'], 'input': {'fields': {'api_token': experiment.mock_key}}}
        rejected(another_session.request('POST', login + '/continue', submitted), 'provider_login_cross_session')
        done = root.api('POST', login + '/continue', submitted)
        require(done['step']['type'] == 'done', 'provider_login_not_completed')
        rejected(root.request('POST', login + '/continue', submitted), 'provider_login_replayed_flow')
        views = root.api('GET', summary)['items']
        require(len(views) == 1 and views[0]['account_id'] == 'candidate-account'
                and experiment.mock_key not in json.dumps([done, views]), 'provider_credential_summary_leaks_secret')
        with root_pat(experiment, ['channel:read']) as pat:
            scoped = business(root.request('GET', summary, bearer=pat), 'provider_read_pat_summary')
            require(scoped['items'] == views and root.request('POST', login, {}, bearer=pat)[0] == 403,
                    'provider_read_pat_started_sensitive_login')
        experiment.record('provider_login_permission_session_replay_and_credential_summary')
        experiment.routing([channel])
        before, sends = experiment.balances(), experiment.sends
        require(experiment.relay()[0] == 200 and experiment.sends == sends + 1,
                'provider_credential_relay_not_delivered')
        experiment.settled(before, 1000)
        refreshed = root.api('GET', summary)['items']
        require(len(refreshed) == 1 and refreshed[0]['label'] == 'Refreshed account'
                and refreshed[0]['expires_at'] > time.time() + 3000
                and experiment.mock_key not in json.dumps(refreshed), 'provider_refresh_not_persisted_or_leaks_secret')
        experiment.record('provider_expired_credential_refreshes_relays_and_settles_once')
    finally:
        experiment.routing([experiment.channel_id])
        root.api('POST', f'/api/channel/{channel}/status', {'status': 2})
        if another_session.bearer:
            logout_verified(another_session)
