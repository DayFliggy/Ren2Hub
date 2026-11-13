"""真实易支付下单与 RSA 回调合同；密钥临时生成，回调仅投递本地候选。"""
import base64
from concurrent.futures import ThreadPoolExecutor
from decimal import Decimal
import hashlib
import json
from pathlib import Path
import secrets
import shutil
import subprocess
import tempfile
import time
import urllib.parse

from .common import atomic_private, regular_file, require
from .extensions import payload, rejected, topup_logs, wallet

SIGN_TYPE = 'payment_setting.epay_sign_type'
PRIVATE_KEY = 'payment_setting.epay_merchant_private_key'
PUBLIC_KEY = 'payment_setting.epay_platform_public_key'


def openssl(*arguments, data=None):
    executable = shutil.which('openssl')
    require(executable is not None, 'epay_openssl_required')
    result = subprocess.run([executable, *arguments], input=data, capture_output=True, timeout=30)
    require(result.returncode == 0, 'epay_local_crypto_failed')
    return result.stdout


def signing_content(values):
    return '&'.join(f'{key}={values[key]}' for key in sorted(values)
                    if key not in ('sign', 'sign_type') and str(values[key]).strip()).encode()


def run_epay_rsa(experiment):
    root, user = experiment.root, experiment.user
    options = {item['key']: item['value'] for item in root.api('GET', '/api/option/')}
    keys = (SIGN_TYPE, PRIVATE_KEY, PUBLIC_KEY, 'PayAddress', 'EpayId', 'EpayKey', 'PayMethods')
    persisted = {item['key']: item['value'] for item in experiment.rows(
        'SELECT key,value FROM options WHERE key IN (' + ','.join("'" + key + "'" for key in keys) + ')')}
    original = {key: persisted.get(key, options.get(key, '')) for key in keys}
    require(original[SIGN_TYPE] == 'MD5', 'epay_candidate_expected_initial_md5')
    put = lambda key, value: root.api('PUT', '/api/option/', {'key': key, 'value': value})
    with tempfile.TemporaryDirectory(prefix='epay-rsa-') as directory:
        directory = Path(directory)
        merchant = regular_file(experiment.mock_tls_key, private=True)
        platform = directory / 'platform.pem'
        atomic_private(platform, openssl('genpkey', '-algorithm', 'RSA', '-pkeyopt', 'rsa_keygen_bits:2048'))
        merchant_public = directory / 'merchant.pub'
        atomic_private(merchant_public, openssl('pkey', '-in', str(merchant), '-pubout'))
        public = openssl('pkey', '-in', str(platform), '-pubout').decode()
        md5_key = secrets.token_hex(24)
        try:
            for key, value in ((SIGN_TYPE, 'SHA1'), (SIGN_TYPE, 'RSA'),
                               (PRIVATE_KEY, 'invalid-private'), (PUBLIC_KEY, 'invalid-public')):
                rejected(root.request('PUT', '/api/option/', {'key': key, 'value': value}),
                         'epay_invalid_signing_settings_accepted')
            for key, value in ((PRIVATE_KEY, merchant.read_text()), (PUBLIC_KEY, public),
                               ('PayAddress', 'https://127.0.0.1:41749'), ('EpayId', 'candidate-merchant'),
                               ('EpayKey', md5_key), ('PayMethods', '[{"name":"Local Alipay","type":"alipay"}]'),
                               (SIGN_TYPE, 'RSA')):
                put(key, value)
            exposed = root.api('GET', '/api/option/')
            require(PRIVATE_KEY not in {item['key'] for item in exposed}
                    and merchant.read_text() not in json.dumps(exposed), 'epay_merchant_private_key_exposed')
            for key in (PRIVATE_KEY, PUBLIC_KEY):
                rejected(root.request('PUT', '/api/option/', {'key': key, 'value': ''}),
                         'epay_active_rsa_key_clearing_accepted')
            info, status = user.api('GET', '/api/user/topup/info'), user.api('GET', '/api/status')
            amount = max(1, int(info['min_topup']))
            if status['quota_display_type'] == 'TOKENS':
                amount *= int(status['quota_per_unit'])
            quote = payload(user.request('POST', '/api/user/amount', {'amount': amount}), 'epay_quote')
            require(quote.get('message') == 'success', 'epay_quote_failed')
            money = str(quote['data'])
            before, count = wallet(experiment), topup_logs(experiment)
            request = {'amount': amount, 'payment_method': 'alipay',
                       'expected_pay_amount': money, 'expected_currency': 'CNY'}
            wrong = payload(user.request('POST', '/api/user/pay',
                            {**request, 'expected_pay_amount': str(Decimal(money) + Decimal('.01'))}),
                            'epay_quote_change')
            require(wrong.get('message') == 'error' and wrong.get('code') == 'topup_quote_changed',
                    'epay_changed_quote_accepted')
            purchase = payload(user.request('POST', '/api/user/pay', request), 'epay_rsa_purchase')
            require(purchase.get('message') == 'success' and purchase['data']['sign_type'] == 'RSA'
                    and purchase['url'] == 'https://127.0.0.1:41749/api/pay/submit', 'epay_rsa_purchase_failed')
            order = purchase['data']
            signature = directory / 'order.sig'
            atomic_private(signature, base64.b64decode(order['sign'], validate=True))
            openssl('dgst', '-sha256', '-verify', str(merchant_public), '-signature', str(signature),
                    data=signing_content(order))
            trade = order['out_trade_no']
            require(trade.isalnum(), 'epay_trade_identifier_invalid')
            row = experiment.rows("SELECT expected_quota,pay_amount,status FROM top_ups WHERE trade_no='" + trade + "'")
            require(len(row) == 1 and row[0]['status'] == 'pending'
                    and Decimal(str(row[0]['pay_amount'])) == Decimal(money), 'epay_order_quote_not_preserved')
            expected_quota = int(row[0]['expected_quota'])
            require(expected_quota > 0 and wallet(experiment) == before, 'epay_order_credited_before_payment')

            def signed(values, key=platform):
                result = {**values, 'sign_type': 'RSA'}
                result['sign'] = base64.b64encode(openssl('dgst', '-sha256', '-sign', str(key),
                                                       data=signing_content(result))).decode()
                return result

            def deliver(values):
                return user.request('POST', '/api/user/epay/notify', urllib.parse.urlencode(values).encode(),
                                    headers={'Content-Type': 'application/x-www-form-urlencoded'})

            fields = {'pid': 'candidate-merchant', 'type': 'alipay', 'money': money, 'name': order['name'],
                      'out_trade_no': trade, 'trade_no': 'local-' + experiment.suffix,
                      'trade_status': 'TRADE_SUCCESS', 'timestamp': str(int(time.time()))}
            paid = signed(fields)
            md5 = {**fields, 'sign_type': 'MD5',
                   'sign': hashlib.md5(signing_content(fields) + md5_key.encode()).hexdigest()}
            invalid = [{**paid, 'money': str(Decimal(money) + 1)}, signed(fields, merchant), md5,
                       {**md5, 'sign_type': 'RSA'}, signed({**fields, 'timestamp': str(int(time.time()) - 600)}),
                       {key: value for key, value in paid.items() if key != 'sign'}]
            for callback in invalid:
                require(deliver(callback) == (200, b'fail'), 'epay_invalid_callback_accepted')
                require(wallet(experiment) == before and topup_logs(experiment) == count,
                        'epay_rejected_callback_changed_funds')
            with ThreadPoolExecutor(max_workers=2) as pool:
                for response in pool.map(deliver, (paid, paid)):
                    require(response == (200, b'success'), 'epay_valid_parallel_callback_failed')
            require(deliver(paid) == (200, b'success'), 'epay_replayed_callback_failed')
            root.api('POST', '/api/user/topup/complete', {'trade_no': trade})
            require(wallet(experiment) == before + expected_quota and topup_logs(experiment) == count + 1,
                    'epay_rsa_callback_or_manual_completion_not_exactly_once')
            experiment.record('epay_rsa_quote_signature_wrong_mixed_expired_callbacks_and_replay_once')
        finally:
            put(SIGN_TYPE, 'MD5')
            for key in keys:
                if key != SIGN_TYPE:
                    put(key, original[key])


def run_system_retention(experiment):
    key = 'system_task_setting.retention_days'
    root = experiment.root
    values = {item['key']: item['value'] for item in root.api('GET', '/api/option/')}
    require(values.get(key) == '30', 'system_task_retention_default_changed')
    for invalid in ('0', '-1', '1.5', 'invalid'):
        rejected(root.request('PUT', '/api/option/', {'key': key, 'value': invalid}),
                 'invalid_system_task_retention_accepted')
    try:
        root.api('PUT', '/api/option/', {'key': key, 'value': '14'})
        saved = {item['key']: item['value'] for item in root.api('GET', '/api/option/')}
        require(saved.get(key) == '14', 'system_task_retention_not_saved')
    finally:
        root.api('PUT', '/api/option/', {'key': key, 'value': '30'})
    experiment.record('system_task_retention_default_thirty_days_and_validated_update')
