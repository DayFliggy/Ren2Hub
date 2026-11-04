"""Shared isolated target, private-file, account, pricing and settlement-evidence contracts."""
import base64
import hashlib
import json
import os
from pathlib import Path
import re
import secrets
import stat
import subprocess
import urllib.parse

_verified_target = None


class RequirementError(RuntimeError):
    """A stable non-secret diagnostic, safe to put in an operator report."""


def require(condition, code):
    if not condition:
        raise RequirementError(code)


def protect_private(path, directory=False):
    os.chmod(path, 0o700 if directory else 0o600)


def regular_file(path, private=False):
    path = Path(path)
    metadata = path.lstat()
    require(stat.S_ISREG(metadata.st_mode) and not path.is_symlink(), "regular_file_required")
    if private and os.name == "posix":
        require(stat.S_IMODE(metadata.st_mode) == 0o600, "private_file_mode_0600_required")
    require(metadata.st_nlink == 1, "hardlinked_file_rejected")
    return path


def atomic_private(path, data):
    path = Path(path)
    require(not path.is_symlink(), "symlink_target_rejected")
    if path.exists():
        regular_file(path, private=True)
    temporary = path.with_name(path.name + "." + secrets.token_hex(8) + ".new")
    descriptor = os.open(temporary, os.O_WRONLY | os.O_CREAT | os.O_EXCL | getattr(os, "O_NOFOLLOW", 0), 0o600)
    try:
        protect_private(temporary)
        with os.fdopen(descriptor, "wb") as output:
            output.write(data if isinstance(data, bytes) else data.encode())
            output.flush()
            os.fsync(output.fileno())
        os.replace(temporary, path)
        if os.name == 'posix':
            parent = os.open(path.parent, os.O_RDONLY | os.O_DIRECTORY)
            try:
                os.fsync(parent)
            finally:
                os.close(parent)
    finally:
        if temporary.exists():
            temporary.unlink()


def _inspect(container):
    result = subprocess.run(['sudo', '-n', 'docker', '--host', 'unix:///var/run/docker.sock', 'inspect', container],
                            capture_output=True, timeout=20, check=True)
    return json.loads(result.stdout)[0]


def configure_target(path):
    global _verified_target
    os.environ['REN2HUB_ACCEPTANCE_TARGET'] = str(regular_file(path, private=True).absolute())
    _verified_target = None


def acceptance_target():
    path = os.environ.get('REN2HUB_ACCEPTANCE_TARGET')
    require(bool(path), 'internal_candidate_manifest_required')
    target = json.loads(regular_file(path, private=True).read_text())
    keys = {'app_container', 'postgres_container', 'redis_container', 'mock_container',
            'database_user', 'database_name', 'database_host', 'redis_host', 'network',
            'run_id', 'container_ids', 'network_id', 'image_id', 'revision', 'ca_path', 'network_guard',
            'traffic_host_interface'}
    require(isinstance(target, dict) and set(target) in (keys, keys | {'attachment_volume'}), 'invalid_candidate_manifest')
    require(re.fullmatch(r'[0-9a-f]{24}', target['run_id']), 'invalid_candidate_run_id')
    # 空串为监听器模式；非空为宿主网卡模式。
    require(isinstance(target['traffic_host_interface'], str)
            and re.fullmatch(r'[A-Za-z0-9_.-]{0,15}', target['traffic_host_interface']), 'invalid_candidate_traffic_mode')
    prefix = 'ren2hub-candidate-local-' + target['run_id']
    if 'attachment_volume' in target:
        require(target['attachment_volume'] == prefix + '-attachments', 'foreign_candidate_attachment_volume')
    for role in ('app', 'postgres', 'redis', 'mock'):
        require(target[role + '_container'] == prefix + '-' + role, 'foreign_candidate_name')
        require(re.fullmatch(r'[0-9a-f]{64}', target['container_ids'][role]), 'invalid_candidate_container_id')
    require(target['network'] == prefix + '-network'
            and re.fullmatch(r'[0-9a-f]{64}', target['network_id']), 'invalid_candidate_network')
    require(target['database_name'] == 'ren2hub_candidate' and target['database_user'] == 'ren2hub'
            and target['database_host'] == 'postgres:5432' and target['redis_host'] == 'redis:6379',
            'candidate_datastore_contract')
    return target


def require_candidate_mounts(role, item, target):
    """升级仅允许本轮专用附件卷；其余数据保持 tmpfs，不放宽宿主路径限制。"""
    ca = (target['ca_path'], '/etc/ren2hub/mock-ca.crt', True)
    counter = ('/proc/1/net/dev', '/host/proc/net/dev', True)
    counters = 0
    attachments = 0
    for mount in item.get('Mounts', []):
        if role == 'app' and mount.get('Type') == 'bind':
            actual = (mount.get('Source'), mount.get('Destination'), mount.get('RW') is False)
            if actual == counter and target['traffic_host_interface']:
                counters += 1
            else:
                require(actual == ca, 'candidate_unexpected_bind_mount')
        elif role == 'app' and mount.get('Type') == 'volume':
            require(bool(target.get('attachment_volume')) and mount.get('Name') == target['attachment_volume']
                    and mount.get('Destination') == '/app/ticket-attachments' and mount.get('RW') is True,
                    'candidate_attachment_volume_mismatch')
            attachments += 1
        else:
            require(mount.get('Type') == 'tmpfs', 'candidate_persistent_storage_rejected')
    if role == 'app' and target['traffic_host_interface']:
        require(counters == 1, 'candidate_traffic_counter_mount_mismatch')
    if role == 'app' and target.get('attachment_volume'):
        require(attachments == 1, 'candidate_attachment_volume_missing')


def verify_candidate_database(revision):
    """每个写入阶段核对本轮 ID、label、内部网络、tmpfs、DSN 和网络命名空间。"""
    global _verified_target
    _verified_target = None
    target = acceptance_target()
    require(re.fullmatch(r'[0-9a-f]{40}', revision) and revision == target['revision'], 'candidate_revision_mismatch')
    network = _inspect(target['network_id'])
    label = 'renai.local-test.run'
    require(network.get('Internal') is True and network['Name'] == target['network']
            and network.get('Labels', {}).get(label) == target['run_id'], 'candidate_internal_network_required')
    containers = {}
    for role in ('app', 'postgres', 'redis', 'mock'):
        item = _inspect(target['container_ids'][role])
        containers[role] = item
        require(item['Id'] == target['container_ids'][role]
                and item['Name'] == '/' + target[role + '_container']
                and item['Config'].get('Labels', {}).get(label) == target['run_id']
                and item['State']['Running'], 'candidate_container_identity_mismatch')
        require(not item['HostConfig'].get('Privileged') and not item['HostConfig'].get('CapAdd')
                and not item['HostConfig'].get('PortBindings')
                and not any(item['NetworkSettings'].get('Ports', {}).values()), 'candidate_privilege_or_port_exposed')
        require_candidate_mounts(role, item, target)
        if role == 'app':
            require(item['HostConfig']['NetworkMode'] == 'container:' + target['container_ids']['mock'],
                    'candidate_app_must_share_mock_namespace')
            require(item['State'].get('Health', {}).get('Status') == 'healthy', 'candidate_app_unhealthy')
        else:
            require(set(item['NetworkSettings']['Networks']) == {target['network']}
                    and item['NetworkSettings']['Networks'][target['network']]['NetworkID'] == target['network_id'],
                    'candidate_network_membership_mismatch')
        if role in ('postgres', 'redis'):
            require(item['State'].get('Health', {}).get('Status') == 'healthy', 'candidate_datastore_unhealthy')
            require(role in item['NetworkSettings']['Networks'][target['network']].get('Aliases', []),
                    'candidate_datastore_alias_mismatch')
            destination = '/var/lib/postgresql/data' if role == 'postgres' else '/data'
            require(destination in item['HostConfig'].get('Tmpfs', {}), 'candidate_datastore_tmpfs_required')
    app = containers['app']
    if target.get('attachment_volume'):
        volume = _inspect(target['attachment_volume'])
        require(volume.get('Name') == target['attachment_volume']
                and volume.get('Labels', {}).get(label) == target['run_id'],
                'candidate_attachment_volume_identity_mismatch')
    require(app['Image'] == target['image_id'] and app['Config']['Labels'].get('org.opencontainers.image.revision') == revision,
            'candidate_image_identity_changed')
    settings = dict(item.split('=', 1) for item in app['Config']['Env'])
    require(settings.get('TASK_POLL_MAX_FAILURES', '') == ('0' if target.get('attachment_volume') else ''),
            'candidate_mock_downtime_policy_mismatch')
    dsn = urllib.parse.urlsplit(settings.get('SQL_DSN', ''))
    redis = urllib.parse.urlsplit(settings.get('REDIS_CONN_STRING', ''))
    require((dsn.scheme, dsn.username, dsn.hostname, dsn.port, dsn.path) ==
            ('postgresql', 'ren2hub', 'postgres', 5432, '/ren2hub_candidate') and len(dsn.password or '') >= 32
            and not dsn.query and not dsn.fragment, 'candidate_postgres_dsn_mismatch')
    require((redis.scheme, redis.hostname, redis.port, redis.path) == ('redis', 'redis', 6379, '/0')
            and len(redis.password or '') >= 32 and not redis.query and not redis.fragment,
            'candidate_redis_dsn_mismatch')
    require(not settings.get('LOG_SQL_DSN') and not settings.get('SQLITE_PATH')
            and len(settings.get('SESSION_SECRET', '')) >= 32 and len(settings.get('CRYPTO_SECRET', '')) >= 32
            and settings.get('TICKET_UPLOAD_DIR') == '/app/ticket-attachments'
            and (bool(target.get('attachment_volume'))
                 or '/app/ticket-attachments' in app['HostConfig'].get('Tmpfs', {})), 'candidate_storage_or_secrets_invalid')
    # 与发布合同一致：宿主模式路径为空或固定挂载点，监听器模式两变量都为空，避免读到容器自己的 net/dev。
    interface = target['traffic_host_interface']
    require(settings.get('TRAFFIC_HOST_INTERFACE', '') == interface
            and settings.get('TRAFFIC_HOST_NET_DEV', '') in (('', '/host/proc/net/dev') if interface else ('',)),
            'candidate_traffic_environment_mismatch')
    namespace = subprocess.run(['sudo', '-n', 'readlink', '/proc/' + str(containers['mock']['State']['Pid']) + '/ns/net'],
                               capture_output=True, text=True, timeout=20)
    require(namespace.returncode == 0 and os.readlink('/proc/self/ns/net') == namespace.stdout.strip(),
            'candidate_mock_network_namespace_required')
    for role in ('postgres', 'redis', 'mock'):
        info = containers[role]
        address = info['NetworkSettings']['Networks'][target['network']]['IPAddress']
        require(address == target['network_guard']['addresses'][role], 'candidate_network_address_changed')
        for family, binary in (('ipv4', '/usr/sbin/iptables'), ('ipv6', '/usr/sbin/ip6tables')):
            rules = subprocess.run(['sudo', '-n', 'nsenter', '--target', str(info['State']['Pid']), '--net',
                                    binary, '-w', '5', '-S', 'OUTPUT'], capture_output=True, timeout=20)
            require(rules.returncode == 0 and hashlib.sha256(rules.stdout).hexdigest() ==
                    target['network_guard']['output_rules_sha256'][role + '_' + family],
                    'candidate_network_output_rules_changed')
    _verified_target = target
    tables = {row['table_name'] for row in candidate_query("SELECT table_name FROM information_schema.tables WHERE table_schema='public'")}
    expected = {'users', 'tokens', 'channels', 'tasks', 'top_ups', 'task_plugins', 'token_route_profiles',
                'token_route_entries', 'tickets', 'gmpay_orders', 'affiliate_records', 'admin_quota_operations'}
    require(expected <= tables and not tables & {'billing_schemas', 'billing_events', 'subscription_plans',
            'user_subscriptions', 'system_instances'}, 'native_core_database_contract_mismatch')
    groups = candidate_query("SELECT table_name FROM information_schema.columns WHERE table_schema='public' "
                           "AND table_name IN ('users','tokens','channels','abilities','logs') "
                           "AND column_name IN ('group','using_group','auto_groups')")
    require(not groups, 'retired_business_group_columns_present')
    return target


def candidate_query(query, timeout=60):
    """Run one read-only query in the verified isolated PostgreSQL.

    The local socket is used, so no password is passed or logged. Ordering inside
    the aggregate is fixed by an explicit row number, keeping ORDER BY meaningful.
    """
    target = acceptance_target()
    require(target == _verified_target, 'isolated_target_not_verified')
    statement = query.strip().rstrip(';')
    if not statement:
        raise ValueError('empty_query_rejected')
    # Explicit whole-row syntax prevents a native `source` column from
    # shadowing the composite alias and turning row_to_json into a text call.
    wrapped = ('BEGIN READ ONLY; SELECT COALESCE(json_agg(payload.value ORDER BY payload.position), '
               "'[]'::json)::text FROM (SELECT row_number() OVER () AS position, "
               'row_to_json(source.*) AS value FROM (' + statement + ') AS source) AS payload; COMMIT;')
    result = subprocess.run(['sudo', '-n', 'docker', '--host', 'unix:///var/run/docker.sock', 'exec', '--user', 'postgres',
                             target['container_ids']['postgres'], 'psql', '--no-psqlrc', '--quiet',
                             '-v', 'ON_ERROR_STOP=1', '-At', '-U', target['database_user'],
                             '-d', target['database_name'], '-c', wrapped],
                            capture_output=True, text=True, timeout=timeout)
    require(result.returncode == 0, 'candidate_query_failed')
    return json.loads(result.stdout.strip() or '[]')


def password_payload(api, password):
    key = api("GET", "/api/user/login/encryption-key")
    if not isinstance(key, dict) or type(key.get("enabled")) is not bool:
        raise RuntimeError("invalid_password_encryption_capability")
    if not key["enabled"]:
        return {"password": password}
    # Optional encryption is off by default. Enabled deployments fail closed if
    # their acceptance runtime lacks the standard cryptography implementation.
    try:
        from cryptography.hazmat.primitives import hashes, serialization
        from cryptography.hazmat.primitives.asymmetric import padding, rsa
        from cryptography.hazmat.primitives.ciphers.aead import AESGCM
    except ImportError:
        raise RuntimeError("password_encryption_dependency_unavailable") from None
    kid = key.get("kid")
    if not isinstance(kid, str) or not kid:
        raise RuntimeError("invalid_password_encryption_key")
    public = serialization.load_pem_public_key(key["public_key"].encode())
    if not isinstance(public, rsa.RSAPublicKey) or public.key_size < 2048:
        raise RuntimeError("invalid_password_encryption_key")
    secret, nonce = os.urandom(32), os.urandom(12)
    ciphertext = AESGCM(secret).encrypt(nonce, password.encode(), ("password-v2:" + kid).encode())
    wrapped = public.encrypt(secret, padding.OAEP(mgf=padding.MGF1(hashes.SHA256()),
                                                algorithm=hashes.SHA256(), label=b"password-v2"))
    envelope = "v2." + ".".join(base64.b64encode(part).decode() for part in (wrapped, nonce, ciphertext))
    return {"password_encrypted": envelope, "encryption_key_id": kid}


def login(client, username, password):
    bundle = client.api("POST", "/api/user/login", {"username": username, **password_payload(client.api, password)})
    if not isinstance(bundle, dict) or bundle.get("require_verification") or not bundle.get("access_token"):
        raise RuntimeError("dedicated_password_only_acceptance_account_required")
    client.bearer = bundle["access_token"]
    return bundle


def operation_proof(client, scope, password, context=None):
    policy = client.api("GET", "/api/verify/methods?scope=" + scope)
    if not any(item.get("method") == "password" and item.get("available") is True
               for item in policy.get("methods", [])):
        raise RuntimeError("interactive_security_factor_required")
    proof = client.api("POST", "/api/verify", {"scope": scope, "method": "password",
        **({"context": context} if context else {}), **password_payload(client.api, password)})
    if not isinstance(proof, dict) or not proof.get("proof_token") or proof.get("scope") != scope:
        raise RuntimeError("invalid_security_proof")
    return proof["proof_token"]


def model_pricing(client, model):
    snapshot = client.api('GET', '/api/option/model_pricing?model=' + urllib.parse.quote(model, safe=''))
    entries = [entry for entry in snapshot['entries'] if entry['model_name'] == model]
    require(len(entries) <= 1, 'duplicate_model_pricing_entry')
    return entries[0] if entries else {'model_name': model, 'version': snapshot['empty_version'], 'configured': {}}


def save_model_pricing(client, model, pricing, expected=None):
    if expected is None:
        expected = model_pricing(client, model)['version']
    client.api('PATCH', '/api/option/model_pricing', {'changes': [
        {'model_name': model, 'expected_version': expected, 'pricing': pricing}]})
    result = model_pricing(client, model)
    require(result['configured'] == pricing, 'saved_model_pricing_mismatch')
    return result


def expression_pricing(expression):
    return {'billing_setting.billing_mode': 'tiered_expr', 'billing_setting.billing_expr': expression}


def consume_logs(client, path, request_id):
    query = urllib.parse.urlencode({'type': 2, 'request_id': request_id, 'p': 1, 'page_size': 100})
    return client.api('GET', path + '?' + query)['items']


def native_balances(user_id, token_id):
    require(type(user_id) is int and user_id > 0 and type(token_id) is int and token_id > 0, 'invalid_balance_identity')
    rows = candidate_query(f"SELECT u.quota AS wallet, u.used_quota AS wallet_used, t.remain_quota AS token, t.used_quota AS token_used FROM users u JOIN tokens t ON t.user_id=u.id WHERE u.id={user_id} AND t.id={token_id}")
    require(len(rows) == 1 and all(type(value) is int for value in rows[0].values()), 'native_balance_contract_mismatch')
    return rows[0]


def require_charge(before, after, amount):
    require(type(amount) is int and amount >= 0, 'invalid_expected_charge')
    require(after == {'wallet': before['wallet'] - amount, 'wallet_used': before['wallet_used'] + amount,
                      'token': before['token'] - amount, 'token_used': before['token_used'] + amount},
            'wallet_token_settlement_mismatch')
