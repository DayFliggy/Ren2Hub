"""Exercise the live channel contract through an already isolated candidate.

Mutations use native authenticated APIs. SQL is used only for settlement evidence;
the upstream hold is event driven, so a rejected request cannot pass by waiting.
"""
from concurrent.futures import ThreadPoolExecutor
from decimal import Decimal
import http.server
import json
import threading
import time
import urllib.parse

from .common import require
from .client import Client


def run_channel_contracts(experiment):
    root, user = experiment.root, experiment.user
    channel_id, token_id = experiment.channel_id, experiment.token_id
    token_path = f'/api/token/{token_id}/routing'
    channel = root.api('GET', f'/api/channel/{channel_id}')
    person = root.api('GET', f'/api/user/{experiment.user_id}')

    def channel_config(identifier, **changes):
        root.api('PUT', '/api/channel/', {'id': identifier, **changes})

    def user_limit(limit):
        root.api('PUT', '/api/user/', {key: person[key] for key in
            ('id', 'username', 'display_name', 'email')} | {'max_concurrency': limit})

    def profile(cap=None, entries=None):
        current = user.api('GET', token_path)
        wanted = [channel_id] if entries is None else entries
        saved = user.api('PUT', token_path, {'mode': 'manual', 'version': current['version'],
            'entries': [{'channel_id': value, 'enabled': True} for value in
                wanted], 'max_channel_ratio': cap})
        reread = user.api('GET', token_path)
        require(saved == reread and reread['mode'] == 'manual'
                and reread['version'] == current['version'] + 1
                and [row['channel_id'] for row in reread['entries']] == wanted
                and all(row['enabled'] for row in reread['entries']), 'routing_save_readback_mismatch')
        actual_cap = reread.get('max_channel_ratio')
        require(actual_cap is None if cap is None else
                isinstance(actual_cap, str) and Decimal(actual_cap) == Decimal(cap),
                'routing_cap_save_readback_mismatch')
        return reread

    def relay(key=None):
        # Independent transport avoids sharing urllib cookie state across threads.
        return Client(experiment.base).request('POST', '/v1/chat/completions',
            {'model': experiment.model, 'messages': [{'role': 'user', 'content': 'local acceptance'}],
             'max_tokens': 8}, bearer=key or experiment.key)

    def reject_full(key=None):
        started = time.monotonic()
        require(relay(key)[0] == 429, 'concurrency_full_did_not_reject')
        require(time.monotonic() - started < 2, 'concurrency_full_waited_in_queue')

    def preview_reason(reason):
        query = urllib.parse.urlencode({'model': experiment.model, 'path': '/v1/chat/completions'})
        preview = user.api('GET', token_path + '/preview?' + query)
        require(len(preview['entries']) == 1 and preview['entries'][0]['channel_id'] == channel_id
                and not preview['entries'][0]['eligible']
                and preview['entries'][0]['reason'] == reason, 'routing_preview_candidate_mismatch')

    def occupancy():
        state = root.api('GET', '/api/performance/routing')
        users = {row['id']: row for row in state['users']}
        channels = {row['id']: row for row in state['channels']}
        require(experiment.user_id in users and set(experiment.channels) <= channels.keys(),
                'candidate_concurrency_snapshot_missing_resources')
        return users[experiment.user_id], {cid: channels[cid] for cid in experiment.channels}

    def zero_active():
        deadline = time.monotonic() + 5
        while True:
            person_state, channel_states = occupancy()
            rows = [person_state, *channel_states.values()]
            if all(row['in_flight'] == 0 for row in rows):
                return
            require(time.monotonic() < deadline, 'candidate_concurrency_permit_leaked')
            time.sleep(.03)

    second = None
    try:
        channel_config(channel_id, channel_ratio='1.5', max_concurrency=0)
        profile()
        before = experiment.balances()
        require(relay()[0] == 200, 'channel_ratio_relay_failed')
        experiment.settled(before, 1500)
        profile('1')
        preview_reason('channel_ratio_exceeded')
        before, sends = experiment.balances(), experiment.sends
        require(relay()[0] == 503, 'channel_ratio_cap_not_enforced')
        require(experiment.sends == sends, 'channel_ratio_cap_sent_upstream')
        experiment.settled(before, 0)
        experiment.record('channel_ratio_once_and_key_cap_before_send')

        channel_config(channel_id, channel_ratio='1', max_concurrency=1)
        profile()
        catalog_path = f'/api/user/model-catalog?token_id={token_id}'
        catalog = user.api('GET', catalog_path)
        require(any(row['model_name'] == experiment.model for row in catalog['models']),
                'shared_catalog_missing_allowed_model')
        offer = next((row for row in catalog['offers'] if row['model_id'] == experiment.model
                      and row['channel_id'] == channel_id), None)
        public_channel = next((row for row in catalog['channels'] if row['id'] == channel_id), None)
        require(offer is not None and public_channel is not None and public_channel['eligible']
                and experiment.model in public_channel['models']
                and public_channel['max_concurrency'] == 1 and 'in_flight' not in public_channel
                and offer['supported_endpoint_types']
                and offer['supported_endpoint_types']
                == public_channel['supported_endpoints'][experiment.model]
                and Decimal(offer['channel_ratio']) == Decimal('1'), 'shared_catalog_candidate_mismatch')
        serialized = json.dumps(catalog)
        require(experiment.mock_key not in serialized and channel['base_url'] not in serialized,
                'shared_catalog_exposed_channel_identity')
        status, raw = experiment.other.request('GET', catalog_path)
        denied = json.loads(raw)
        require(status in (200, 403, 404) and denied.get('success') is False
                and not denied.get('data'), 'catalog_token_ownership_not_enforced')
        experiment.record('shared_catalog_real_permissions_and_public_fields')

        # A separate Key belongs to the same user; the user permit must span both.
        name = experiment.model + '-concurrency'
        user.api('POST', '/api/token/', {'name': name, 'remain_quota': 1000000,
            'expired_time': -1, 'model_limits_enabled': True, 'model_limits': experiment.model})
        rows = user.api('GET', '/api/token/?p=1&page_size=100')['items']
        other_id = next(row['id'] for row in rows if row['name'] == name)
        experiment.tokens.append(other_id)
        other_key = user.api('POST', f'/api/token/{other_id}/key')['key']
        # Prepare the fallback before holding a real request, keeping the critical
        # window bounded by admission checks rather than fixture creation.
        second = http.server.ThreadingHTTPServer(('127.0.0.1', 0), experiment.handler())
        threading.Thread(target=second.serve_forever, daemon=True).start()
        second_id = experiment.add_channel(second.server_port, name=experiment.model+'-fallback')
        user_limit(1)
        experiment.hold_entered.clear()
        experiment.hold_release.clear()
        experiment.hold_port = int(channel['base_url'].rsplit(':', 1)[1])
        before, sends = experiment.balances(), experiment.sends
        with ThreadPoolExecutor(max_workers=1) as pool:
            pending = pool.submit(relay)
            try:
                require(experiment.hold_entered.wait(5), 'candidate_hold_not_entered')
                person_state, channel_states = occupancy()
                require(person_state['limit'] == 1 and person_state['in_flight'] == 1
                        and channel_states[channel_id]['limit'] == 1
                        and channel_states[channel_id]['in_flight'] == 1,
                        'held_attempt_missing_user_or_channel_permit')
                reject_full(other_key)
                require(experiment.sends == sends + 1, 'user_full_sent_upstream')
                # A hot limit update keeps the active count; channel remains full.
                user_limit(0)
                preview_reason('channel_concurrency_full')
                reject_full()
                require(experiment.sends == sends + 1, 'channel_full_sent_upstream')
                person_state, channel_states = occupancy()
                require(person_state['in_flight'] == 1 and person_state['limit'] == 0
                        and channel_states[channel_id]['in_flight'] == 1,
                        'hot_limit_lost_active_count')
                profile(entries=[channel_id, second_id])
                require(relay()[0] == 200, 'manual_busy_candidate_not_scanned')
                require(experiment.sends == sends + 2, 'manual_fallback_wrong_attempt_count')
            finally:
                experiment.hold_release.set()
            require(pending.result(timeout=5)[0] == 200, 'held_attempt_failed')
        experiment.settled(before, 2000)
        zero_active()
        experiment.record('user_cross_key_channel_capacity_hot_update_manual_fallback_and_release')
    finally:
        experiment.hold_release.set()
        experiment.hold_port = None
        if second:
            second.shutdown()
            second.server_close()
        user_limit(person['max_concurrency'])
        channel_config(channel_id, channel_ratio=channel['channel_ratio'], max_concurrency=channel['max_concurrency'])
        profile()
