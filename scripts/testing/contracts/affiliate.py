"""Run referral mutations only after the caller has verified the isolated target."""
import json
import secrets

from .common import require
from .client import Client
from .extensions import business, rejected
from .websocket import logout_verified


def run_affiliate(experiment):
    root, sponsor = experiment.root, experiment.user
    before = sponsor.api('GET', '/api/user/affiliate')
    policy = before['policy']

    def option(name, value):
        root.api('PUT', '/api/option/', {'key': 'affiliate_setting.' + name,
            'value': str(value).lower() if isinstance(value, bool) else str(value)})

    def add(identifier, value=10000):
        return root.request('POST', '/api/user/manage', {'id': invitee_id,
            'action': 'add_quota', 'mode': 'add', 'value': value, 'operation_id': identifier})

    invitee = Client(experiment.base)
    invitee_id = None
    try:
        for key, value in {'enabled': True, 'admin_recharge_enabled': True,
                          'rate_bps': 2000, 'freeze_hours': 0,
                          'duration_days': 0, 'per_invitee_cap_quota': 0}.items():
            option(key, value)
        username, password = 'a' + experiment.suffix, secrets.token_urlsafe(32)
        # Registration, rather than administrator creation, binds attribution.
        business(invitee.request('POST', '/api/user/register', {'username': username,
            'password': password, 'aff_code': before['code']}), 'affiliate_registration')
        person = invitee.login(username, password)
        invitee_id = person['id']
        wallet = invitee.api('GET', '/api/user/self')['quota']
        operation = 'acceptance-add-' + experiment.suffix
        business(add(operation), 'affiliate_admin_add')
        business(add(operation), 'affiliate_admin_replay')
        require(invitee.api('GET', '/api/user/self')['quota'] == wallet + 10000,
                'affiliate_admin_add_not_once')
        require(add(operation, 10001)[0] == 409, 'affiliate_admin_fingerprint_conflict')
        accrued = sponsor.api('GET', '/api/user/affiliate')
        require(accrued['available_reward'] == before['available_reward'] + 2000
                and accrued['total_reward'] == before['total_reward'] + 2000,
                'affiliate_reward_rate_or_idempotency')
        listing = sponsor.api('GET', '/api/user/affiliate/invitees?p=1&page_size=100')
        require(listing['total'] == before['invited'] + 1, 'affiliate_invitee_missing')
        require(all(set(row) == {'masked_name', 'invited_at', 'reward_quota'}
                    for row in listing['items']), 'affiliate_invitee_private_fields')
        require(username not in json.dumps(listing), 'affiliate_invitee_raw_name_exposed')
        foreign = experiment.other.api('GET', '/api/user/affiliate/invitees?p=1&page_size=100')
        require(foreign['total'] == 0, 'affiliate_invitee_ownership')
        experiment.record('affiliate_registration_attribution_masked_list_and_admin_idempotency')

        option('admin_recharge_enabled', False)
        skipped = 'acceptance-disabled-' + experiment.suffix
        business(add(skipped), 'affiliate_disabled_admin_add')
        option('admin_recharge_enabled', True)
        business(add(skipped), 'affiliate_disabled_admin_replay')
        require(sponsor.api('GET', '/api/user/affiliate')['total_reward'] == accrued['total_reward'],
                'affiliate_disabled_operation_backfilled_reward')
        experiment.record('affiliate_policy_change_cannot_backfill_completed_source')

        wallet = sponsor.api('GET', '/api/user/self')['quota']
        transfer = {'operation_id': 'acceptance-transfer-' + experiment.suffix}
        first = sponsor.api('POST', '/api/user/affiliate/transfer', transfer)
        second = sponsor.api('POST', '/api/user/affiliate/transfer', transfer)
        require(first['transferred_quota'] == accrued['available_reward']
                and second['replayed'] is True
                and second['transferred_quota'] == first['transferred_quota'],
                'affiliate_transfer_replay')
        require(sponsor.api('GET', '/api/user/self')['quota'] == wallet + first['transferred_quota'],
                'affiliate_transfer_wallet_not_once')
        rejected(sponsor.request('POST', '/api/user/affiliate/transfer',
            {'operation_id': 'reject-amount-' + experiment.suffix, 'amount': 1}),
            'affiliate_legacy_amount_accepted')
        require(sponsor.api('GET', '/api/user/affiliate')['available_reward'] == 0,
                'affiliate_transfer_not_full')
        experiment.record('affiliate_full_transfer_once_and_legacy_amount_rejected')
    finally:
        for key, value in policy.items():
            option(key, value)
        if invitee.bearer:
            logout_verified(invitee)
