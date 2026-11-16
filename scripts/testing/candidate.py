#!/usr/bin/env python3
"""用全新临时依赖运行候选业务合同。只接收本地源码、产物目录和已构建镜像。"""
from __future__ import annotations

import argparse
import hashlib
import io
import json
import os
from pathlib import Path
import re
import secrets
import shutil
import socket
import signal
import stat
import subprocess
import sys
import tarfile
import time
import threading
import traceback

LABEL = 'renai.local-test.run'
STAGES = ('billing', 'ws', 'compact', 'plugin-before', 'plugin-after', 'upstream_sync', 'sdk', 'routing')
UPGRADE_STAGES = ('upgrade-baseline', 'upgrade-management', 'upgrade-before-seconds',
                  'upgrade-prepare-p', 'upgrade-resume-p', 'upgrade-after-seconds', 'upgrade-after-p')
HOST_INTERFACE = r'[A-Za-z0-9_.-]{1,15}'
# 除本轮生成的 mock CA 证书外，宿主网卡计数文件是候选唯一允许挂载的宿主文件；它不是数据路径，只读单文件。
COUNTER_SOURCE, COUNTER_TARGET = '/proc/1/net/dev', '/host/proc/net/dev'


class CandidateError(RuntimeError):
    """只保存预定义诊断码，避免命令行、DSN 或响应正文进入报告。"""


def require(condition, code):
    if not condition:
        raise CandidateError(code)


def contract(check, *args):
    """Run an upgrade contract check and keep its diagnostic code in the report."""
    from contracts.common import RequirementError
    try:
        return check(*args)
    except RequirementError as error:
        raise CandidateError(str(error)) from None


def private_write(path: Path, value):
    require(not path.is_symlink(), 'symlink_output_rejected')
    if path.exists():
        metadata = path.lstat()
        require(stat.S_ISREG(metadata.st_mode) and metadata.st_nlink == 1, 'regular_output_required')
    payload = value if isinstance(value, bytes) else (value if isinstance(value, str) else json.dumps(value, indent=2, ensure_ascii=False)).encode()
    temporary = path.with_name(path.name + '.' + secrets.token_hex(8) + '.new')
    fd = os.open(temporary, os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW, 0o600)
    try:
        with os.fdopen(fd, 'wb') as stream:
            stream.write(payload)
            stream.flush()
            os.fsync(stream.fileno())
        os.replace(temporary, path)
    finally:
        temporary.unlink(missing_ok=True)


def execute(arguments, *, stdin=None, timeout=120, check=True, env=None):
    # 捕获内容仅留在进程内；故障报告不包含 subprocess 的原始异常或输出。
    process = subprocess.Popen(arguments, stdin=subprocess.PIPE if stdin is not None else subprocess.DEVNULL,
                               stdout=subprocess.PIPE, stderr=subprocess.PIPE, env=env, start_new_session=True)
    try:
        stdout, stderr = process.communicate(stdin, timeout=timeout)
    except BaseException:
        try:
            os.killpg(process.pid, signal.SIGKILL)
        except ProcessLookupError:
            pass
        process.communicate()
        raise
    require(not check or process.returncode == 0, 'local_command_failed')
    return subprocess.CompletedProcess(arguments, process.returncode, stdout, stderr)


def source_identity(source):
    head = execute(['git', '-C', str(source), 'rev-parse', 'HEAD']).stdout.decode().strip()
    paths = execute(['git', '-C', str(source), 'ls-files', '-z', '--cached', '--others', '--exclude-standard', '--', '.', ':(exclude)docs/developer-notes', ':(exclude)**/developer-notes/**']).stdout.split(b'\0')
    digest = hashlib.sha256()
    count = 0
    for raw in sorted(set(paths)):
        if not raw:
            continue
        path = source / os.fsdecode(raw)
        if 'developer-notes' in path.parts:
            continue
        digest.update(raw + b'\0')
        if path.is_symlink():
            digest.update(b'link\0' + os.fsencode(os.readlink(path)))
        elif path.is_file():
            digest.update(b'executable\0' if path.stat().st_mode & 0o111 else b'regular\0')
            digest.update(hashlib.sha256(path.read_bytes()).digest())
        else:
            digest.update(b'deleted\0')
        count += 1
    status = execute(['git', '-C', str(source), 'status', '--porcelain']).stdout
    return {'git_head': head, 'candidate_sha': None if status else head,
            'working_tree_sha256': digest.hexdigest(), 'files': count, 'dirty': bool(status)}


def counter_mounts(container):
    """归一化为 [type, source, target, read_only]；凡目标在 /host 下或源在 /proc 下都算，可写或错源也照实记录。"""
    found = []
    for mount in container.get('Mounts', []):
        source, target = mount.get('Source', ''), mount.get('Destination', '')
        if target.startswith('/host/') or source == '/proc' or source.startswith('/proc/'):
            found.append([mount.get('Type'), source, target, mount.get('RW') is False])
    return sorted(found)


def image_matches_source(labels, source, routing_only=False):
    """A HEAD label never overrides a recorded build-content fingerprint."""
    fingerprint = labels.get('io.ren2hub.source-sha256')
    require(isinstance(fingerprint, str) and re.fullmatch(r'[0-9a-f]{64}', fingerprint)
            and fingerprint == source['working_tree_sha256'], 'image_source_fingerprint_mismatch')
    return source['candidate_sha'] == labels.get('org.opencontainers.image.revision')


def image_files(docker, image_id, paths):
    """只从未启动的临时容器读取镜像文件；包括失败路径在内都清理该容器。"""
    require(re.fullmatch(r'sha256:[0-9a-f]{64}', image_id or ''), 'immutable_image_id_required')
    identifier = docker('create', '--network', 'none', '--read-only', '--label',
                        'renai.build-inspection=' + secrets.token_hex(12), image_id).stdout.decode().strip()
    require(re.fullmatch(r'[0-9a-f]{64}', identifier), 'invalid_image_inspection_container')
    try:
        result = {}
        for path in paths:
            archive = docker('cp', identifier + ':' + path, '-').stdout
            with tarfile.open(fileobj=io.BytesIO(archive)) as tar:
                members = tar.getmembers()
                require(len(members) == 1 and members[0].isfile()
                        and members[0].name == Path(path).name, 'invalid_image_file_archive')
                result[path] = tar.extractfile(members[0]).read()
        return result
    finally:
        docker('rm', '-f', identifier)


def inspect_build_payload(docker, image_id, artifact):
    binary_path = '/new-api' if artifact == 'ren2hub' else '/channel-simulator'
    paths = [binary_path]
    if artifact == 'ren2hub':
        paths.append('/etc/ren2hub/build-tools.json')
    files = image_files(docker, image_id, paths)
    result = {'binary': {'path': binary_path, 'sha256': hashlib.sha256(files[binary_path]).hexdigest()}}
    if artifact == 'ren2hub':
        result['tools'] = json.loads(files['/etc/ren2hub/build-tools.json'])
    return result


def validate_build_identity(manifest, image, artifact, current_source=None, payload=None):
    """两独立构建入口共享数据合同；不导入或执行模拟器源码。"""
    require(isinstance(manifest, dict) and type(manifest.get('schema_version')) is int
            and manifest['schema_version'] == 1 and manifest.get('artifact') == artifact
            and manifest.get('success') is True and manifest.get('stable') is True,
            'successful_versioned_build_identity_required')
    before, after = manifest.get('source_before'), manifest.get('source_after')
    require(isinstance(before, dict) and before == after
            and re.fullmatch(r'[0-9a-f]{64}', str(before.get('working_tree_sha256', '')))
            and type(before.get('dirty')) is bool, 'build_identity_unstable_source')
    head = before.get('git_head')
    require((isinstance(head, str) and re.fullmatch(r'[0-9a-f]{40}', head))
            or (artifact == 'channel-simulator' and head is None and before['dirty']),
            'build_identity_invalid_base_revision')
    require(manifest.get('base_revision') == head
            and 'candidate_sha' in before
            and before['candidate_sha'] == (None if before['dirty'] else head),
            'build_identity_invalid_candidate_revision')
    revision = head if artifact == 'ren2hub' else ('unborn' if head is None else head + ('-dirty' if before['dirty'] else ''))
    require(manifest.get('revision') == revision, 'build_identity_revision_mismatch')
    require(re.fullmatch(r'sha256:[0-9a-f]{64}', str(manifest.get('image_id', '')))
            and manifest['image_id'] == image['Id'], 'build_identity_image_mismatch')
    labels = image['Config'].get('Labels') or {}
    prefix = 'io.ren2hub' if artifact == 'ren2hub' else 'io.channel-simulator'
    require(labels.get(prefix + '.source-sha256') == before['working_tree_sha256']
            and labels.get('org.opencontainers.image.revision') == revision, 'build_identity_label_mismatch')
    if artifact == 'ren2hub':
        require(labels.get('org.opencontainers.image.version') == revision, 'build_identity_label_mismatch')
        require(current_source is not None and before == current_source, 'build_identity_stale_worktree')
    else:
        require(type(manifest.get('protocol_version')) is int and manifest['protocol_version'] == 1
                and labels.get(prefix + '.protocol') == '1', 'build_identity_protocol_mismatch')
    binary = manifest.get('binary')
    expected_path = '/new-api' if artifact == 'ren2hub' else '/channel-simulator'
    require(isinstance(binary, dict) and binary.get('path') == expected_path
            and re.fullmatch(r'[0-9a-f]{64}', str(binary.get('sha256', ''))), 'build_identity_binary_required')
    tools = manifest.get('tools')
    required_tools = ('go', 'docker_server', 'docker_client', 'python') + (('bun', 'node') if artifact == 'ren2hub' else ())
    require(isinstance(tools, dict) and all(isinstance(tools.get(key), str) and tools[key].strip()
            for key in required_tools), 'build_identity_tools_required')
    if payload is not None:
        require(payload['binary'] == binary, 'build_identity_binary_mismatch')
        if artifact == 'ren2hub':
            require(isinstance(payload.get('tools'), dict) and all(payload['tools'].get(key) == tools[key]
                    for key in ('go', 'bun', 'node')), 'build_identity_tools_mismatch')


def read_build_identity(path):
    require(path is not None, 'explicit_build_identity_required')
    path = Path(path)
    require(path.is_file() and not path.is_symlink() and path.stat().st_nlink == 1,
            'regular_build_identity_required')
    raw = path.read_bytes()
    require(len(raw) <= 1 << 20, 'build_identity_too_large')
    try:
        return json.loads(raw), raw
    except (ValueError, UnicodeDecodeError):
        raise CandidateError('invalid_build_identity_json') from None


def stage_accepted(stage, returncode, report):
    return (stage == 'routing' and returncode == 2 and report.get('exit_code') == 2
            and report.get('contracts_success') is True) or (returncode == 0 and report.get('success') is True)


class Candidate:
    def __init__(self, source, output, image, traffic_interface=None, previous_image=None, simulator_image=None, routing_only=False,
                 image_identity=None, simulator_identity=None):
        require(traffic_interface is None or re.fullmatch(HOST_INTERFACE, traffic_interface),
                'invalid_traffic_host_interface')
        self.traffic_interface = traffic_interface or ''
        self.source = Path(source).resolve()
        require(self.source == Path(__file__).resolve().parents[2], 'candidate_source_must_contain_executing_runner')
        self.output = Path(output).absolute()
        require(os.name == 'posix' and Path('/proc/self/ns/net').exists(), 'linux_required')
        require(self.source.is_dir() and not self.source.is_symlink(), 'source_directory_required')
        for path in (self.output, *self.output.parents):
            require(not path.is_symlink(), 'symlink_output_directory_rejected')
        self.output.mkdir(parents=True, exist_ok=True)
        require(not any(self.output.iterdir()), 'empty_candidate_output_required')
        self.private = self.output / 'candidate-private'
        require(not self.private.exists() and not self.private.is_symlink()
                and not (self.output / 'candidate.json').exists(), 'new_candidate_output_required')
        self.private.mkdir(mode=0o700)
        self.run_id = secrets.token_hex(12)
        self.prefix = 'ren2hub-candidate-local-' + self.run_id
        self.containers = {}
        self.network_id = None
        self.image = image
        self.previous_image = previous_image
        self.simulator_image = simulator_image
        self.image_identity_path = image_identity
        self.simulator_identity_path = simulator_identity
        self.routing_only = routing_only
        require(not routing_only or previous_image is None, "routing_does_not_run_upgrade")
        require(bool(simulator_image), "explicit_simulator_image_required")
        self.attachment_volume = None
        self.stages = ('routing',) if routing_only else ((*UPGRADE_STAGES, *STAGES) if previous_image else STAGES)
        self.workspace_identity = source_identity(self.source.parent)
        self.workspace_sha = self.workspace_identity['git_head']
        self.report = {'success': False, 'layer': 'routing' if routing_only else 'candidate', 'run_id': self.run_id,
                       'source': source_identity(self.source), 'workspace': self.workspace_identity, 'stages': {}, 'cleanup': {},
                       'external_requests': None, 'database_lifecycle': 'fresh_tmpfs',
                       'limits': ['本地模拟供应商和支付；不验证真实供应商、真实支付或生产部署。']}
        self.report['exit_code'] = 1

    def docker(self, *arguments, **kwargs):
        return execute(['sudo', '-n', 'docker', '--host', 'unix:///var/run/docker.sock', *arguments], **kwargs)

    def inspect(self, identifier):
        return json.loads(self.docker('inspect', identifier).stdout)[0]

    def owned(self, role):
        identifier = self.containers[role]
        item = self.inspect(identifier)
        require(item['Id'] == identifier and item['Name'] == '/' + self.prefix + '-' + role
                and item['Config'].get('Labels', {}).get(LABEL) == self.run_id,
                'candidate_container_ownership_changed')
        return item

    def env_file(self, name, values):
        require(all(re.fullmatch(r'[A-Z][A-Z0-9_]*', key) and '\n' not in value and '\r' not in value
                    for key, value in values.items()), 'invalid_candidate_environment')
        path = self.private / name
        private_write(path, ''.join(key + '=' + value + '\n' for key, value in values.items()))
        return path

    def start(self, role, image, options=(), command=()):
        name = self.prefix + '-' + role
        create = self.docker('create', '--name', name, '--label', LABEL + '=' + self.run_id,
                             '--security-opt', 'no-new-privileges:true', '--pids-limit', '256',
                             '--cpus', '2', '--memory', '1g', '--pull', 'never',
                             *options, image, *command)
        identifier = create.stdout.decode().strip()
        require(re.fullmatch(r'[0-9a-f]{64}', identifier), 'invalid_created_container_id')
        self.containers[role] = identifier
        self.owned(role)
        self.docker('start', identifier)
        return identifier

    def wait_healthy(self, role):
        deadline = time.monotonic() + 150
        while time.monotonic() < deadline:
            item = self.owned(role)
            require(item['State']['Running'], 'candidate_' + role + '_exited')
            health = item['State'].get('Health', {}).get('Status')
            if health == 'healthy':
                return
            require(health != 'unhealthy', 'candidate_' + role + '_unhealthy')
            time.sleep(1)
        raise CandidateError('candidate_' + role + '_health_timeout')

    def tls(self):
        openssl = shutil.which('openssl')
        require(bool(openssl), 'openssl_required_for_local_tls')
        key, certificate = self.private / 'server.key', self.private / 'server.crt'
        execute([openssl, 'req', '-x509', '-newkey', 'rsa:2048', '-nodes', '-days', '2',
                 '-subj', '/CN=Ren2Hub Local Contract', '-addext',
                 'subjectAltName=IP:127.0.0.1,DNS:www.newapi.ai,DNS:www.newapi.pro',
                 '-addext', 'basicConstraints=critical,CA:TRUE', '-keyout', str(key), '-out', str(certificate)])
        os.chmod(key, 0o600)
        os.chmod(certificate, 0o600)
        return certificate

    def sdk_runtimes(self):
        """在启动任何容器前确认 SDK 运行时可用，避免前六个阶段跑完才在 sdk 阶段失败。"""
        environment = {key: os.environ[key] for key in ('GATEWAY_SDK_PYTHON', 'GATEWAY_SDK_NODE', 'GATEWAY_SDK_NODE_MODULE')
                       if os.environ.get(key)}
        environment.setdefault('GATEWAY_SDK_PYTHON', str(self.source / '.test-tools/sdk-python/bin/python3'))
        environment.setdefault('GATEWAY_SDK_NODE', shutil.which('node') or '/usr/bin/node')
        environment.setdefault('GATEWAY_SDK_NODE_MODULE', str(self.source / '.test-tools/sdk-node/node_modules/openai'))
        for language in ('python', 'node'):
            executable = Path(environment['GATEWAY_SDK_' + language.upper()])
            require(executable.is_absolute() and os.access(executable, os.X_OK), 'explicit_sdk_' + language + '_runtime_required')
        # 与 sdk_node.mjs 一致：可传包目录，也可传包内入口文件。
        module = Path(environment['GATEWAY_SDK_NODE_MODULE'])
        package = module if module.is_dir() else module.parent
        require(module.is_absolute() and module.exists() and (package / 'package.json').is_file(),
                'explicit_sdk_node_module_required')
        self.sdk_environment = environment

    def verify_build_identities(self):
        require(all(re.fullmatch(r'sha256:[0-9a-f]{64}', value or '')
                    for value in (self.image, self.simulator_image)), 'routing_requires_immutable_image_ids')
        inputs = [('app', 'ren2hub', self.image, getattr(self, 'image_identity_path', None)),
                  ('simulator', 'channel-simulator', self.simulator_image, getattr(self, 'simulator_identity_path', None))]
        loaded = [(name, artifact, image_id, *read_build_identity(path)) for name, artifact, image_id, path in inputs]
        current = source_identity(self.source)
        require(current == self.report['source'], 'source_changed_before_identity_verification')
        verified = {}
        for name, artifact, image_id, manifest, raw in loaded:
            image = json.loads(self.docker('image', 'inspect', image_id).stdout)[0]
            require(image['Id'] == image_id, 'build_identity_image_mismatch')
            validate_build_identity(manifest, image, artifact, current)
            payload = inspect_build_payload(self.docker, image_id, artifact)
            validate_build_identity(manifest, image, artifact, current, payload)
            copy = self.output / (name + '-build-identity.json')
            private_write(copy, raw)
            verified[name] = {'path': str(copy), 'sha256': hashlib.sha256(raw).hexdigest(), 'manifest': manifest}
        require(source_identity(self.source) == current, 'source_changed_during_identity_verification')
        self.report['build_identities'] = verified
        return verified

    def prepare(self):
        self.sdk_environment = {}
        self.verify_build_identities()
        if not getattr(self, 'routing_only', False):
            self.sdk_runtimes()
        image = json.loads(self.docker('image', 'inspect', self.image).stdout)[0]
        labels = image['Config'].get('Labels') or {}
        revision = labels.get('org.opencontainers.image.revision', '')
        require(re.fullmatch(r'[0-9a-f]{40}', revision) and labels.get('org.opencontainers.image.version') == revision,
                'immutable_application_revision_labels_required')
        self.image_id, self.revision = image['Id'], revision
        self.report['image'] = {'reference': self.image, 'id': self.image_id, 'revision': revision}
        # 不把现有镜像的测试结果冒充未提交工作树构建结果。
        self.report['image_matches_clean_source'] = image_matches_source(
            labels, self.report['source'], getattr(self, 'routing_only', False))
        self.report['image']['working_tree_sha256'] = labels.get('io.ren2hub.source-sha256')
        self.active_image_id, self.active_revision = self.image_id, self.revision
        if getattr(self, 'previous_image', None):
            previous = json.loads(self.docker('image', 'inspect', self.previous_image).stdout)[0]
            previous_labels = previous['Config'].get('Labels') or {}
            previous_revision = previous_labels.get('org.opencontainers.image.revision', '')
            require(re.fullmatch(r'[0-9a-f]{40}', previous_revision)
                    and previous_labels.get('org.opencontainers.image.version') == previous_revision
                    and previous_revision != revision, 'distinct_immutable_previous_image_required')
            self.active_image_id, self.active_revision = previous['Id'], previous_revision
            self.report['upgrade'] = {'previous_image': {'reference': self.previous_image,
                                      'id': previous['Id'], 'revision': previous_revision}}
            self.report['upgrade']['mock_downtime_policy'] = {
                'TASK_POLL_MAX_FAILURES': 0,
                'reason': '阶段间模拟供应商计划停服，不把连接失败阈值作为本升级合同；原生任务超时仍生效。'}
        compose = self.source.parent / 'deploy/ren2hub/docker-compose.preview.yml'
        references = {}
        dependencies = {}
        for role in ('postgres', 'redis'):
            matches = re.findall(r'^    image: (' + role + r':[^\s]+@sha256:[0-9a-f]{64})$', compose.read_text(), re.M)
            require(len(matches) == 1, 'pinned_dependency_image_required_' + role)
            references[role] = matches[0]
            dependencies[role] = json.loads(self.docker('image', 'inspect', matches[0]).stdout)[0]['Id']
        self.report['dependency_images'] = {role: {'reference': references[role], 'id': dependencies[role]} for role in dependencies}
        self.report['dependency_compose_sha256'] = hashlib.sha256(compose.read_bytes()).hexdigest()
        certificate = self.tls()
        self.network = self.prefix + '-network'
        self.network_id = self.docker('network', 'create', '--internal', '--label', LABEL + '=' + self.run_id,
                                      self.network).stdout.decode().strip()
        require(re.fullmatch(r'[0-9a-f]{64}', self.network_id), 'invalid_network_id')
        network = self.inspect(self.network_id)
        require(network.get('Internal') is True and network['Labels'].get(LABEL) == self.run_id,
                'internal_network_identity_mismatch')
        passwords = {key: secrets.token_hex(32) for key in ('POSTGRES_PASSWORD', 'REDIS_PASSWORD', 'SESSION_SECRET', 'CRYPTO_SECRET')}
        pg_env = self.env_file('postgres.env', {'POSTGRES_USER': 'ren2hub', 'POSTGRES_DB': 'ren2hub_candidate',
                                              'POSTGRES_PASSWORD': passwords['POSTGRES_PASSWORD']})
        redis_env = self.env_file('redis.env', {'REDIS_PASSWORD': passwords['REDIS_PASSWORD'], 'REDISCLI_AUTH': passwords['REDIS_PASSWORD']})
        health = ('--health-interval', '2s', '--health-timeout', '3s', '--health-retries', '50')
        self.start('postgres', dependencies['postgres'], ('--network', self.network, '--network-alias', 'postgres',
                   '--env-file', str(pg_env), '--tmpfs', '/var/lib/postgresql/data:rw,size=768m',
                   '--health-cmd', 'pg_isready -U ren2hub -d ren2hub_candidate', *health))
        self.start('redis', dependencies['redis'], ('--network', self.network, '--network-alias', 'redis',
                   '--env-file', str(redis_env), '--tmpfs', '/data:rw,size=128m', '--health-cmd', 'redis-cli ping', *health),
                   ('sh', '-c', 'exec redis-server --save "" --appendonly no --requirepass "$REDIS_PASSWORD"'))
        for role in ('postgres', 'redis'):
            self.wait_healthy(role)
        # mock 持有稳定网络命名空间，app 重启不改变本地供应商地址。
        self.start('mock', dependencies['redis'], ('--network', self.network, '--network-alias', 'app',
                   '--entrypoint', '/bin/sh', '--read-only', '--tmpfs', '/data:rw,size=1m'), ('-c', 'exec sleep infinity'))
        if getattr(self, 'simulator_image', None):
            from simulator_support import prepare
            prepare(self)
        self.network_guard = self.lock_network()
        # Only this run's independently owned supplier bypasses the local
        # marketplace proxy; private ranges and unrelated hosts stay proxied.
        simulator_address = None
        if 'simulator' in self.containers:
            simulator_address = self.owned('simulator')['NetworkSettings']['Networks'][self.network]['IPAddress']
            require(simulator_address == self.network_guard['addresses']['simulator'],
                    'candidate_simulator_network_address_changed')
        from contracts.common import candidate_no_proxy
        app_env = self.env_file('app.env', {
            'SQL_DSN': 'postgresql://ren2hub:' + passwords['POSTGRES_PASSWORD'] + '@postgres:5432/ren2hub_candidate',
            'REDIS_CONN_STRING': 'redis://:' + passwords['REDIS_PASSWORD'] + '@redis:6379/0',
            'SESSION_SECRET': passwords['SESSION_SECRET'], 'CRYPTO_SECRET': passwords['CRYPTO_SECRET'],
            'LOG_SQL_DSN': '', 'SQLITE_PATH': '', 'MEMORY_CACHE_ENABLED': 'true', 'NODE_TYPE': 'master',
            # Routing configures and reads back every fixture through the API.
            # Keep rate limiting enabled without throttling this isolated matrix.
            'CRITICAL_RATE_LIMIT': '200', 'GLOBAL_API_RATE_LIMIT': '10000', 'TRUSTED_PROXIES': 'none',
            'SSL_CERT_FILE': '/etc/ren2hub/mock-ca.crt', 'TICKET_UPLOAD_DIR': '/app/ticket-attachments',
            'SESSION_COOKIE_SECURE': 'false', 'SESSION_COOKIE_TRUSTED_URL': '', 'TZ': 'UTC',
            'HTTP_PROXY': 'http://127.0.0.1:41749', 'HTTPS_PROXY': 'http://127.0.0.1:41749',
            'NO_PROXY': candidate_no_proxy(simulator_address),
            **({'TASK_POLL_MAX_FAILURES': '0'} if getattr(self, 'previous_image', None) else {}),
            **({'TRAFFIC_HOST_INTERFACE': self.traffic_interface} if self.traffic_interface else {})})
        # 默认不挂载也不设网卡名，应用使用主监听器计数；--mount 的源缺失时容器创建直接失败。
        counter = ('--mount', 'type=bind,src=' + COUNTER_SOURCE + ',dst=' + COUNTER_TARGET + ',readonly') if self.traffic_interface else ()
        storage = ('--tmpfs', '/app/ticket-attachments:rw,size=128m')
        if getattr(self, 'previous_image', None):
            self.attachment_volume = self.prefix + '-attachments'
            self.docker('volume', 'create', '--label', LABEL + '=' + self.run_id, self.attachment_volume)
            volume = json.loads(self.docker('volume', 'inspect', self.attachment_volume).stdout)[0]
            require(volume['Name'] == self.attachment_volume and volume.get('Labels', {}).get(LABEL) == self.run_id,
                    'candidate_attachment_volume_identity_mismatch')
            storage = ('--mount', 'type=volume,src=' + self.attachment_volume + ',dst=/app/ticket-attachments')
        self.app_options = ('--network', 'container:' + self.containers['mock'],
                   '--env-file', str(app_env),
                   '--mount', 'type=bind,src=' + str(certificate) + ',dst=/etc/ren2hub/mock-ca.crt,readonly', *counter,
                   *storage, '--tmpfs', '/tmp:rw,size=128m',
                   '--stop-timeout', '130', '--health-start-period', '15s',
                   '--health-cmd', 'wget -q -T 3 -O /dev/null http://127.0.0.1:3000/api/status', *health)
        self.start_app()
        self.wait_healthy('app')
        target = {role + '_container': self.prefix + '-' + role for role in self.containers if role != 'simulator'}
        target.update(database_user='ren2hub', database_name='ren2hub_candidate', database_host='postgres:5432',
                      redis_host='redis:6379', network=self.network, network_id=self.network_id, run_id=self.run_id,
                      container_ids=dict(self.containers), image_id=self.active_image_id, revision=self.active_revision, ca_path=str(certificate), network_guard=self.network_guard,
                      traffic_host_interface=self.traffic_interface)
        if getattr(self, 'attachment_volume', None):
            target['attachment_volume'] = self.attachment_volume
        if getattr(self, 'simulator_image', None):
            from simulator_support import target as simulator_target
            target['simulator'] = simulator_target(self)
        self.target = target
        if getattr(self, 'simulator_image', None):
            from simulator_support import wait_ready
            wait_ready(self)
        private_write(self.private / 'target.json', target)
        # scope/interface 由管理组从 Root 接口实测后补上；挂载取自运行中容器的 inspect。
        traffic = {'configured_interface': self.traffic_interface, 'scope': None, 'interface': None,
                   'mounts': counter_mounts(self.owned('app'))}
        self.report['isolation'] = {'network_id': self.network_id, 'internal': True,
                                    'container_ids': dict(self.containers), 'published_ports': [],
                                    'storage': 'tmpfs_and_owned_attachment_volume' if getattr(self, 'attachment_volume', None) else 'tmpfs',
                                    'network_guard': self.network_guard,
                                    'traffic': traffic}

    def start_app(self):
        # Avoid Docker's loopback DNS forwarder: every permitted hostname has
        # an exact entry, and all DNS traffic is blocked in this namespace.
        hosts = '\n'.join(address + ' ' + role for role, address in self.network_guard['addresses'].items())
        self.start('app', self.active_image_id, (*self.app_options,
                   '--entrypoint', '/bin/sh',
                   '--label', 'org.opencontainers.image.revision=' + self.active_revision,
                   '--label', 'org.opencontainers.image.version=' + self.active_revision),
                   ('-c', 'printf "%s\\n" "$1" >> /etc/hosts; shift; exec /new-api "$@"',
                    'candidate-start', hosts, '--log-dir', '/tmp/ren2hub-local-logs'))

    def upgrade_snapshot(self, include_options=False):
        """只记录本轮合成数据的指纹；轮询中的任务和日志另由行为合同验证。"""
        self.owned('postgres')
        tables = ('options', 'users', 'tokens', 'channels', 'top_ups', 'gmpay_orders', 'redemptions',
                  'admin_quota_operations', 'token_route_profiles', 'token_route_entries',
                  'tickets', 'ticket_messages', 'ticket_attachments')
        from upgrade_contract import ADDED_COLUMN_SUFFIX, option_values, split_added_column
        result, options = {}, None
        for table in tables:
            rows, added = contract(split_added_column, table, self.table_rows(table))
            result[table] = hashlib.sha256(json.dumps(rows, sort_keys=True).encode()).hexdigest()
            if added is not None:
                result[table + ADDED_COLUMN_SUFFIX] = added
            if table == 'options' and include_options:
                options = contract(option_values, rows)
        self.owned('app')
        files = self.docker('exec', self.containers['app'], 'sh', '-c',
                            'find /app/ticket-attachments -type f -exec sha256sum {} \\; | sort').stdout
        result['attachments'] = hashlib.sha256(files).hexdigest()
        return (result, options) if include_options else result

    def psql_json(self, query):
        raw = self.docker('exec', '-i', self.containers['postgres'], 'psql', '-X', '-q', '-t', '-A',
                          '-v', 'ON_ERROR_STOP=1', '-U', 'ren2hub', '-d', 'ren2hub_candidate', stdin=query.encode()).stdout
        return json.loads(raw)

    def table_rows(self, table):
        return self.psql_json('BEGIN READ ONLY; SELECT COALESCE(json_agg(value ORDER BY value::text),\'[]\'::json) '
                              'FROM (SELECT row_to_json(t) AS value FROM ' + table + ' t) s; COMMIT;')

    def topup_bonus_state(self):
        """折扣与加赠表的完整内容，只在内存比较。"""
        from upgrade_contract import BONUS_ROW_TABLES, BONUS_TABLES, LEGACY_DISCOUNT_TABLES
        self.owned('postgres')
        names = ','.join("'" + table + "'" for table in (*LEGACY_DISCOUNT_TABLES, *BONUS_TABLES))
        tables = self.psql_json('BEGIN READ ONLY; SELECT COALESCE(json_agg(t ORDER BY t),\'[]\'::json) FROM unnest(ARRAY['
                                + names + ']) t WHERE to_regclass(\'public.\' || t) IS NOT NULL; COMMIT;')
        state = {'tables': tables}
        for table in tables:
            if table in BONUS_ROW_TABLES:
                state[table] = self.table_rows(table)
        return state

    def seed_legacy_topup_discount(self):
        """折扣版本的升级源写入两版配置、两条小时用量和已用/未用两个兑换码，让首次升级迁移真实旧表，
        并核对兑换码在新增 bonus_quota 列后保持不变。"""
        from upgrade_contract import LEGACY_DISCOUNT_SEED_CONFIGS, LEGACY_DISCOUNT_SEED_USER
        state = self.topup_bonus_state()
        if not {'topup_discount_configs', 'topup_discount_usage_hourly'} <= set(state['tables']):
            return False
        first, latest = LEGACY_DISCOUNT_SEED_CONFIGS
        require('$$' not in first + latest, 'upgrade_seed_config_invalid')
        query = ('BEGIN; WITH base AS (SELECT COALESCE(MAX(version), 0) AS version FROM topup_discount_configs) '
                 'INSERT INTO topup_discount_configs (version, config, created_by, created_at) '
                 'SELECT base.version + seed.step, seed.config, 1, seed.created_at FROM base, '
                 '(VALUES (1, $$' + first + '$$, 1793289600), (2, $$' + latest + '$$, 1793293200)) AS seed(step, config, created_at); '
                 'INSERT INTO topup_discount_usage_hourly (user_id, hour_utc, quota) '
                 'SELECT ' + str(LEGACY_DISCOUNT_SEED_USER) + ', EXTRACT(EPOCH FROM date_trunc(\'hour\', now()))::bigint - seed.hours * 3600, seed.quota '
                 'FROM (VALUES (1, 1200), (25, 3400)) AS seed(hours, quota); '
                 'INSERT INTO redemptions (user_id, "key", status, name, quota, created_time, redeemed_time, used_user_id, expired_time) '
                 "VALUES (1, 'upgrade-contract-redemption-used', 3, 'upgrade contract', 500000, 1793289600, 1793293200, "
                 + str(LEGACY_DISCOUNT_SEED_USER) + ", 0), "
                 "(1, 'upgrade-contract-redemption-open', 1, 'upgrade contract', 700000, 1793289600, 0, 0, 0); COMMIT;")
        self.owned('postgres')
        self.docker('exec', '-i', self.containers['postgres'], 'psql', '-X', '-q', '-v', 'ON_ERROR_STOP=1',
                    '-U', 'ren2hub', '-d', 'ren2hub_candidate', stdin=query.encode())
        return True

    def upgrade(self):
        require(all(self.report['stages'].get(stage, {}).get('success') is True
                    for stage in UPGRADE_STAGES[:4]), 'upgrade_preparation_must_pass')
        previous_target = dict(self.target)
        previous_target['container_ids'] = dict(self.target['container_ids'])
        private_write(self.private / 'upgrade-source-target.json', previous_target)
        seeded = self.seed_legacy_topup_discount()
        before, options_before = self.upgrade_snapshot(include_options=True)
        bonus_before = self.topup_bonus_state()
        schema_before = self.schema_fingerprint()
        old = self.owned('app')
        self.docker('stop', '--time', '130', old['Id'], timeout=180)
        require(self.owned('app')['State']['ExitCode'] == 0, 'upgrade_previous_app_did_not_exit_cleanly')
        self.docker('rm', old['Id'])
        del self.containers['app']
        self.active_image_id, self.active_revision = self.image_id, self.revision
        self.start_app()
        self.wait_healthy('app')
        after, options_after = self.upgrade_snapshot(include_options=True)
        bonus_after = self.topup_bonus_state()
        self.report['upgrade'].update(data_before=before, data_after=after, data_preserved=False,
                                      schema_before=schema_before, schema_after=self.schema_fingerprint())
        from upgrade_contract import verify_options_migration, verify_persisted_data, verify_topup_bonus_migration
        added_columns = contract(verify_persisted_data, before, after)
        bonus_migration = contract(verify_topup_bonus_migration, bonus_before, bonus_after)
        migration = contract(verify_options_migration, options_before, options_after)
        self.report['upgrade'].update(data_preserved=True, sensitive_word_migration=migration,
                                     added_columns=added_columns,
                                     topup_bonus_migration={**bonus_migration, 'legacy_seeded': seeded},
                                     rollback_limit='old_image_does_not_restore_retired_sensitive_options_or_discount_config')
        self.target.update(container_ids=dict(self.containers), image_id=self.image_id, revision=self.revision)
        private_write(self.private / 'target.json', self.target)
        self.report['isolation']['container_ids'] = dict(self.containers)
        schema = self.schema_fingerprint()
        self.docker('restart', '--time', '130', self.containers['app'], timeout=180)
        self.wait_healthy('app')
        require(schema == self.schema_fingerprint() and after == self.upgrade_snapshot()
                and bonus_after == self.topup_bonus_state(), 'upgrade_repeated_start_changed_persisted_data')
        self.report['upgrade']['repeated_start_preserved_data'] = True
        self.verify_rollback_copy()

    def verify_rollback_copy(self):
        """旧镜像只启动于本轮数据库副本和独立网络命名空间。"""
        self.report['upgrade']['rollback_copy'] = {'success': False}
        candidate_before = self.upgrade_snapshot()
        postgres = self.containers['postgres']
        self.owned('postgres')
        dump = self.docker('exec', postgres, 'pg_dump', '-U', 'ren2hub', '-d', 'ren2hub_candidate',
                           '--no-owner', '--no-privileges', timeout=120).stdout
        self.docker('exec', postgres, 'createdb', '-U', 'ren2hub', 'ren2hub_rollback')
        self.docker('exec', '-i', postgres, 'psql', '-X', '-q', '-v', 'ON_ERROR_STOP=1', '-U', 'ren2hub',
                    '-d', 'ren2hub_rollback', stdin=dump, timeout=120)
        del dump
        app = self.owned('app')
        settings = dict(item.split('=', 1) for item in app['Config']['Env'])
        # Reuse the local PostgreSQL role, but never the candidate database,
        # Redis database, keys, attachment volume, or listener namespace.
        settings = {key: value for key, value in settings.items() if key in {
            'SQL_DSN', 'REDIS_CONN_STRING', 'SSL_CERT_FILE', 'HTTP_PROXY', 'HTTPS_PROXY', 'NO_PROXY'}}
        settings['SQL_DSN'] = settings['SQL_DSN'].replace('/ren2hub_candidate', '/ren2hub_rollback')
        settings['REDIS_CONN_STRING'] = settings['REDIS_CONN_STRING'].split('/0')[0] + '/15'
        settings.update(SESSION_SECRET=secrets.token_hex(32), CRYPTO_SECRET=secrets.token_hex(32),
                        LOG_SQL_DSN='', SQLITE_PATH='', TASK_POLL_MAX_FAILURES='0',
                        TICKET_UPLOAD_DIR='/app/ticket-attachments', NODE_TYPE='master', TZ='UTC')
        env = self.env_file('rollback.env', settings)
        dependency = self.report['dependency_images']['redis']['id']
        self.start('rollback_mock', dependency, ('--network', self.network, '--entrypoint', '/bin/sh',
                   '--read-only', '--tmpfs', '/data:rw,size=1m'), ('-c', 'exec sleep infinity'))
        self.namespace('rollback_mock', '/usr/bin/ip', 'route', 'flush', 'default')
        for binary in ('/usr/sbin/iptables', '/usr/sbin/ip6tables'):
            self.namespace('rollback_mock', binary, '-w', '5', '-P', 'OUTPUT', 'DROP')
            if binary.endswith('/iptables'):
                self.namespace('rollback_mock', binary, '-w', '5', '-A', 'OUTPUT',
                               '-d', '127.0.0.11/32', '-j', 'REJECT')
            for protocol in ('udp', 'tcp'):
                self.namespace('rollback_mock', binary, '-w', '5', '-A', 'OUTPUT', '-p', protocol,
                               '--dport', '53', '-j', 'REJECT')
            self.namespace('rollback_mock', binary, '-w', '5', '-A', 'OUTPUT', '-o', 'lo', '-j', 'ACCEPT')
            if binary.endswith('/iptables'):
                for role in ('postgres', 'redis'):
                    self.namespace('rollback_mock', binary, '-w', '5', '-A', 'OUTPUT', '-d',
                                   self.network_guard['addresses'][role] + '/32', '-j', 'ACCEPT')
            self.namespace('rollback_mock', binary, '-w', '5', '-A', 'OUTPUT', '-j', 'REJECT')
        hosts = '\n'.join(self.network_guard['addresses'][role] + ' ' + role for role in ('postgres', 'redis'))
        old = self.report['upgrade']['previous_image']
        self.start('rollback_app', old['id'], ('--network', 'container:' + self.containers['rollback_mock'],
                   '--entrypoint', '/bin/sh', '--env-file', str(env),
                   '--tmpfs', '/app/ticket-attachments:rw,size=128m', '--tmpfs', '/tmp:rw,size=128m',
                   '--health-cmd', 'wget -q -T 3 -O /dev/null http://127.0.0.1:3000/api/status',
                   '--health-interval', '2s', '--health-timeout', '3s', '--health-retries', '50'),
                   ('-c', 'printf "%s\\n" "$1" >> /etc/hosts; shift; exec /new-api "$@"',
                    'candidate-rollback', hosts, '--log-dir', '/tmp/ren2hub-local-logs'))
        self.wait_healthy('rollback_app')
        from upgrade_contract import INSTANCES_KEY, SENSITIVE_KEYS
        keys = sorted((*SENSITIVE_KEYS, INSTANCES_KEY))
        query = ('BEGIN READ ONLY; SELECT COALESCE(json_agg(t),\'[]\'::json) FROM '
                 '(SELECT key,value FROM options WHERE key IN (' + ','.join("'" + key + "'" for key in keys) + ')) t; COMMIT;')
        raw = self.docker('exec', '-i', postgres, 'psql', '-X', '-q', '-t', '-A', '-v', 'ON_ERROR_STOP=1',
                          '-U', 'ren2hub', '-d', 'ren2hub_rollback', stdin=query.encode()).stdout
        rows = json.loads(raw)
        require(not ({row['key'] for row in rows} & SENSITIVE_KEYS),
                'rollback_unexpectedly_restored_retired_sensitive_options')
        require(INSTANCES_KEY in {row['key'] for row in rows}, 'rollback_removed_migrated_filter')
        self.report['upgrade']['rollback_copy'] = {
            'success': False, 'old_image_id': old['id'], 'old_revision': old['revision'],
            'database': 'ren2hub_rollback', 'redis_database': 15,
            'isolated_network_namespace': True, 'retired_options_restored': False,
            'migrated_filter_remains': True,
            'rejected_packets': self.network_rejections(('rollback_mock',)),
        }
        for role in ('rollback_app', 'rollback_mock'):
            self.docker('rm', '-f', self.owned(role)['Id'])
            del self.containers[role]
        self.docker('exec', postgres, 'dropdb', '-U', 'ren2hub', 'ren2hub_rollback')
        require(candidate_before == self.upgrade_snapshot(), 'rollback_copy_changed_candidate_data')
        self.report['upgrade']['rollback_copy'].update(success=True, candidate_data_unchanged=True)

    def namespace(self, role, *command):
        item = self.owned(role)
        pid = str(item['State']['Pid'])
        namespace = execute(['sudo', '-n', 'readlink', '/proc/' + pid + '/ns/net']).stdout.decode().strip()
        require(namespace != os.readlink('/proc/self/ns/net'), 'host_namespace_mutation_rejected')
        return execute(['sudo', '-n', 'nsenter', '--target', pid, '--net', *command])

    def lock_network(self):
        """规则只写入本轮容器 netns；internal bridge 本身不能隔离宿主 INPUT。"""
        addresses = {}
        for role in ('postgres', 'redis', 'mock', *(['simulator'] if 'simulator' in self.containers else [])):
            info = self.owned(role)
            require(info['State']['Running'], 'candidate_' + role + '_exited')
            address = info['NetworkSettings']['Networks'][self.network]['IPAddress']
            require(bool(re.fullmatch(r'(?:\d{1,3}\.){3}\d{1,3}', address)),
                    'candidate_' + role + '_network_address_missing')
            addresses[role] = address
        rules = {}
        for role in addresses:
            self.namespace(role, '/usr/bin/ip', 'route', 'flush', 'default')
            for family, binary in (('ipv4', '/usr/sbin/iptables'), ('ipv6', '/usr/sbin/ip6tables')):
                self.namespace(role, binary, '-w', '5', '-P', 'OUTPUT', 'DROP')
                # Docker DNAT rewrites port 53 before filter/OUTPUT. Block its
                # loopback resolver address as well as plain DNS ports.
                if family == 'ipv4':
                    self.namespace(role, binary, '-w', '5', '-A', 'OUTPUT',
                                   '-d', '127.0.0.11/32', '-j', 'REJECT')
                for protocol in ('udp', 'tcp'):
                    self.namespace(role, binary, '-w', '5', '-A', 'OUTPUT', '-p', protocol,
                                   '--dport', '53', '-j', 'REJECT')
                self.namespace(role, binary, '-w', '5', '-A', 'OUTPUT', '-o', 'lo', '-j', 'ACCEPT')
                self.namespace(role, binary, '-w', '5', '-A', 'OUTPUT', '-m', 'conntrack',
                               '--ctstate', 'ESTABLISHED,RELATED', '-j', 'ACCEPT')
                if family == 'ipv4':
                    # Only app/test workers (the mock namespace) initiate peer
                    # connections. Simulator and datastores only reply.
                    peers = sorted(addresses.values()) if role == 'mock' else []
                    for address in peers:
                        self.namespace(role, binary, '-w', '5', '-A', 'OUTPUT', '-d', address + '/32', '-j', 'ACCEPT')
                self.namespace(role, binary, '-w', '5', '-A', 'OUTPUT', '-j', 'REJECT')
                raw = self.namespace(role, binary, '-w', '5', '-S', 'OUTPUT').stdout
                rules[role + '_' + family] = hashlib.sha256(raw).hexdigest()
        gateway = self.inspect(self.network_id)['IPAM']['Config'][0]['Gateway']
        # 只访问本轮临时监听端口，绝不探测宿主已有服务。
        with socket.socket() as listener:
            listener.bind((gateway, 0))
            listener.listen(4)
            port = listener.getsockname()[1]
            with socket.create_connection((gateway, port), timeout=2):
                connection, _ = listener.accept()
                connection.close()
            probe = ('import socket,sys; s=socket.socket(); s.settimeout(2); '
                     'sys.exit(1 if s.connect_ex((sys.argv[1],int(sys.argv[2]))) == 0 else 0)')
            for role in addresses:
                self.namespace(role, sys.executable, '-c', probe, gateway, str(port))
        counters = self.network_rejections(addresses)
        return {'addresses': addresses, 'output_rules_sha256': rules, 'host_probe_blocked': True,
                'dns_forwarding_blocked': True, 'initial_rejected_packets': counters,
                'new_peer_connections':{'mock':'owned_addresses','simulator':[],'postgres':[],'redis':[]},
                'scope': 'owned_container_network_namespaces_only'}

    def network_rejections(self, roles):
        counters = {}
        for role in roles:
            for family, binary in (('ipv4', '/usr/sbin/iptables-save'), ('ipv6', '/usr/sbin/ip6tables-save')):
                raw = self.namespace(role, binary, '-c', '-t', 'filter').stdout.decode()
                values = re.findall(r'^\[(\d+):\d+\] -A OUTPUT\b.* -j REJECT\b', raw, re.M)
                require(len(values) == (4 if family == 'ipv4' else 3), 'candidate_rejection_counters_missing')
                counters[role + '_' + family] = sum(int(value) for value in values)
        return counters

    def network_evidence(self):
        if not getattr(self, 'network_guard', None):
            return
        initial = self.network_guard['initial_rejected_packets']
        final = self.network_rejections(self.network_guard['addresses'])
        require(set(initial) == set(final) and all(final[key] >= initial[key] for key in final),
                'candidate_rejection_counters_reset')
        market = self.report['stages'].get('upstream_sync', {}).get('marketplace', {})
        self.report['external_requests'] = {
            'rejected_packets': sum(final[key] - initial[key] for key in final),
            'rejected_packets_by_namespace': {key: final[key] - initial[key] for key in final},
            'marketplace_local_reports': market.get('reports_received'),
            'marketplace_proxy_rejections': market.get('proxy_rejections'),
            'dns_forwarding_blocked': True,
            'public_destinations_allowed': False,
        }

    def schema_fingerprint(self):
        self.owned('postgres')
        query = """BEGIN READ ONLY;
        SELECT json_build_object(
          'columns', (SELECT json_agg(c ORDER BY table_name, column_name) FROM
            (SELECT table_name, column_name, data_type, character_maximum_length,
                    numeric_precision, numeric_scale, is_nullable, column_default
             FROM information_schema.columns WHERE table_schema='public') c),
          'indexes', (SELECT json_agg(i ORDER BY indexname) FROM
            (SELECT tablename,indexname,indexdef FROM pg_indexes WHERE schemaname='public') i));
        COMMIT;"""
        result = self.docker('exec', '-i', self.containers['postgres'], 'psql', '-X', '-q', '-t', '-A',
                             '-v', 'ON_ERROR_STOP=1', '-U', 'ren2hub', '-d', 'ren2hub_candidate', stdin=query.encode())
        schema = json.loads(result.stdout)
        return hashlib.sha256(json.dumps(schema, sort_keys=True).encode()).hexdigest()

    def stage(self, stage):
        require(source_identity(self.source) == self.report['source'], 'source_changed_before_http_stage')
        app = self.owned('app')
        require(app['State']['Running'] and app['Image'] == self.active_image_id, 'candidate_image_or_state_changed')
        if stage == 'plugin-after':
            before = json.loads((self.output / 'plugin-before.json').read_text())
            require(before.get('success') is True, 'plugin_before_restart_must_pass')
            schema_before = self.schema_fingerprint()
            self.docker('restart', '--time', '130', self.containers['app'], timeout=180)
            self.wait_healthy('app')
            schema_after = self.schema_fingerprint()
            self.report['restart_schema'] = {'before': schema_before, 'after': schema_after, 'unchanged': schema_before == schema_after}
            require(schema_before == schema_after, 'candidate_restart_changed_schema')
        mock = self.owned('mock')
        configuration = {'private': str(self.private), 'output': str(self.output), 'stage': stage, 'revision': self.active_revision}
        environment = {'PATH': '/usr/local/bin:/usr/bin:/bin', 'PYTHONDONTWRITEBYTECODE': '1',
                       'PYTHONPATH': str(Path(__file__).parent), 'LANG': 'C.UTF-8'}
        environment.update(self.sdk_environment)
        command = ['sudo', '-n', 'nsenter', '--target', str(mock['State']['Pid']), '--net',
                   '--setuid', str(os.getuid()), '--setgid', str(os.getgid()), 'env', '-i',
                   *[key + '=' + value for key, value in environment.items()], sys.executable, '-B', '-c',
                   'from contracts.worker import execute; execute()']
        result = execute(command, stdin=json.dumps(configuration).encode(), timeout=900, check=False)
        report_file = self.output / (stage + '.json')
        require(report_file.is_file() and not report_file.is_symlink(), 'candidate_stage_report_missing_' + stage)
        stage_report = json.loads(report_file.read_text())
        self.report['stages'][stage] = stage_report
        if stage == 'upstream_sync':
            # manifest 在 cleanup 之后写，必须趁容器还在运行时采集。
            observed = stage_report.get('traffic') or {}
            self.report['isolation']['traffic'].update(scope=observed.get('scope'), interface=observed.get('interface'),
                                                       mounts=counter_mounts(app))
        private_write(self.output / 'candidate.json', self.report)
        require(stage_accepted(stage, result.returncode, stage_report),
                'candidate_stage_failed_' + stage)

    def diagnostics(self):
        from diagnostics import run as run_diagnostics
        self.report['diagnostics'] = run_diagnostics(self.source, self.output / 'diagnostics')
        from native_restored import run as run_restored
        self.report['known_upstream_restored'] = run_restored(self.source, self.output / 'known_upstream_restored')
        from native_pricing_race import run as run_pricing_race
        self.report['known_upstream_race'] = run_pricing_race(self.source, self.output / 'known_upstream_race')
        from native_logger_race import run as run_logger_race
        self.report['known_upstream_logger_race'] = run_logger_race(self.source, self.output / 'known_upstream_logger_race')
        from ordinary_race import run as run_ordinary_race
        self.report['ordinary_race'] = run_ordinary_race(self.source, self.output / 'ordinary_race')

    def manifest(self):
        reports = {}
        for stage in self.stages:
            path = self.output / (stage + '.json')
            if path.is_file() and not path.is_symlink():
                group = {'ws': 'websocket', 'upstream_sync': 'management'}.get(stage, stage)
                reports[group] = {'path': str(path), 'sha256': hashlib.sha256(path.read_bytes()).hexdigest()}
        source_end = source_identity(self.source)
        workspace_end = source_identity(self.source.parent)
        stable = source_end == self.report['source'] and workspace_end == self.workspace_identity
        self.report['identity_verification'] = {'stable': stable, 'source_end': source_end, 'workspace_end': workspace_end}
        if not stable:
            self.report['success'] = False
            self.report['exit_code'] = 1
            self.report['error_code'] = 'source_or_workspace_changed_during_candidate'
        workspace_clean = workspace_end['candidate_sha'] == self.workspace_sha
        gitlink = execute(['git', '-C', str(self.source.parent), 'ls-tree', 'HEAD', 'Ren2Hub']).stdout.decode().split()
        linked = len(gitlink) == 4 and gitlink[:3] == ['160000', 'commit', self.report.get('image', {}).get('revision')]
        from routing_result import release_exemption, diagnostics_complete, routing_complete
        exemption = release_exemption(self.report.get('diagnostics'), self.report['stages'].get('routing'))
        exempted = exemption is not None
        legacy_clean = (self.report.get('diagnostics', {}).get('success') is True
                        and diagnostics_complete(self.report.get('diagnostics'))
                        and self.report['stages'].get('routing', {}).get('success') is True
                        and routing_complete(self.report['stages'].get('routing')))
        # 豁免只替换 routing 与诊断两处的通过要求；其余阶段仍必须逐项 success。
        stages_ok = all(self.report['stages'].get(stage, {}).get('success') is True
                        for stage in self.stages if not (exempted and stage == 'routing'))
        complete = (len(reports) == len(self.stages) and stages_ok
                    and (legacy_clean or exempted))
        from native_restored import InvalidEvidence, verify_report
        restored = self.report.get('known_upstream_restored') or {}
        restored_acceptance = None
        try:
            restored_acceptance = verify_report(restored, self.output / 'known_upstream_restored',
                                                self.source, self.report['source'])
        except (InvalidEvidence, ValueError, OSError, TypeError, KeyError, IndexError):
            complete = False
        from native_pricing_race import verify_report as verify_pricing_race
        pricing_race = self.report.get('known_upstream_race') or {}
        pricing_race_acceptance = None
        try:
            pricing_race_acceptance = verify_pricing_race(pricing_race, self.output / 'known_upstream_race',
                                                        self.source, self.report['source'])
        except (InvalidEvidence, ValueError, OSError, TypeError, KeyError, IndexError):
            complete = False
        from native_logger_race import verify_report as verify_logger_race
        logger_race = self.report.get('known_upstream_logger_race') or {}
        logger_race_acceptance = None
        try:
            logger_race_acceptance = verify_logger_race(logger_race, self.output / 'known_upstream_logger_race',
                                                      self.source, self.report['source'])
        except (InvalidEvidence, ValueError, OSError, TypeError, KeyError, IndexError):
            complete = False
        from ordinary_race import verify_report as verify_ordinary_race
        ordinary = self.report.get('ordinary_race') or {}
        ordinary_acceptance = None
        try:
            ordinary_acceptance = verify_ordinary_race(ordinary, self.output / 'ordinary_race',
                                                       self.source, self.report['source'])
        except (InvalidEvidence, ValueError, OSError, TypeError, KeyError, IndexError):
            complete = False
        deployable = bool(not getattr(self, 'routing_only', False) and complete
                          and self.report.get('exit_code') in (0, 2) and stable
                          and not self.report.get('error_code') and not self.report.get('cleanup_errors')
                          and self.report.get('image_matches_clean_source') and workspace_clean and linked)
        traffic = self.report.get('isolation', {}).get('traffic')
        manifest = {'success': deployable, 'candidate_success': self.report['success'], 'deployment_eligible': deployable,
                    'source_sha': self.report.get('image', {}).get('revision'),
                    'workspace_sha': self.workspace_sha, 'image_id': self.report.get('image', {}).get('id'),
                    'reports': reports, 'source_worktree': self.report['source'], 'identity_verification': self.report['identity_verification'],
                    'image_matches_clean_source': self.report.get('image_matches_clean_source', False),
                    'upgrade': self.report.get('upgrade'), 'simulator': self.report.get('simulator'),
                    'build_identities': self.report.get('build_identities'),
                    'diagnostics': {key:self.report.get('diagnostics', {}).get(key)
                                    for key in ('exit_code','counts','report_path','report_sha256')},
                    'upstream_exemption': exemption if exempted else None,
                    'known_upstream_restored': {key: restored.get(key) for key in
                                               ('report_path', 'report_sha256')},
                    'restored_native_acceptance': restored_acceptance,
                    'known_upstream_race': {key: pricing_race.get(key) for key in ('report_path', 'report_sha256')},
                    'pricing_race_acceptance': pricing_race_acceptance,
                    'known_upstream_logger_race': {key: logger_race.get(key) for key in ('report_path', 'report_sha256')},
                    'logger_race_acceptance': logger_race_acceptance,
                    'ordinary_race': {key: ordinary.get(key) for key in ('report_path', 'report_sha256')},
                    'ordinary_race_acceptance': ordinary_acceptance,
                    'traffic': {key: traffic[key] for key in ('scope', 'interface', 'mounts')} if traffic else None}
        private_write(self.output / 'candidate-manifest.json', manifest)
        self.report['deployment_eligible'] = deployable

    def cleanup(self):
        errors = []
        removed = []
        try:
            self.network_evidence()
        except Exception:
            errors.append('network_evidence')
        for role in reversed(self.containers):
            try:
                info = self.owned(role)
                self.docker('rm', '-f', info['Id'], timeout=60)
                removed.append(role)
            except Exception:
                errors.append('remove_' + role)
        if self.network_id:
            try:
                network = self.inspect(self.network_id)
                require(network['Id'] == self.network_id and network.get('Labels', {}).get(LABEL) == self.run_id,
                        'candidate_network_ownership_changed')
                self.docker('network', 'rm', self.network_id)
            except Exception:
                errors.append('remove_network')
        if getattr(self, 'attachment_volume', None):
            try:
                volume = json.loads(self.docker('volume', 'inspect', self.attachment_volume).stdout)[0]
                require(volume['Name'] == self.attachment_volume and volume.get('Labels', {}).get(LABEL) == self.run_id,
                        'candidate_attachment_volume_identity_mismatch')
                self.docker('volume', 'rm', self.attachment_volume)
            except Exception:
                errors.append('remove_attachment_volume')
        self.report['cleanup'] = {'success': not errors, 'removed_containers': removed, 'errors': errors}
        if errors:
            self.report['success'] = False
            self.report['exit_code'] = 1


def interrupted(_signum, _frame):
    raise CandidateError('candidate_interrupted')


def run(source: Path, output: Path, image: str, traffic_interface: str | None = None,
        previous_image: str | None = None, simulator_image: str | None = None, routing_only: bool = False,
        image_identity: Path | None = None, simulator_identity: Path | None = None) -> dict:
    previous_umask = os.umask(0o077)
    candidate = None
    old_handlers = {}
    if threading.current_thread() is threading.main_thread():
        for signum in (signal.SIGINT, signal.SIGTERM):
            old_handlers[signum] = signal.signal(signum, interrupted)
    try:
        candidate = Candidate(source, output, image, traffic_interface, previous_image, simulator_image, routing_only,
                              image_identity, simulator_identity)
        candidate.diagnostics()
        candidate.prepare()
        for stage in candidate.stages:
            if stage == 'upgrade-resume-p':
                candidate.upgrade()
            candidate.stage(stage)
        from routing_result import combined_exit_code
        code = combined_exit_code([*candidate.report['stages'].values(), candidate.report['diagnostics'],
                                   candidate.report['known_upstream_restored'], candidate.report['known_upstream_race'],
                                   candidate.report['known_upstream_logger_race'], candidate.report['ordinary_race']])
        candidate.report.update(success=code == 0, exit_code=code)
    except Exception as error:
        if candidate is None:
            raise
        candidate.report['error_code'] = str(error) if isinstance(error, CandidateError) else 'candidate_execution_failed'
        candidate.report['error_type'] = type(error).__name__
        candidate.report['failure_location'] = [
            {'file': Path(frame.filename).name, 'line': frame.lineno, 'function': frame.name}
            for frame in traceback.extract_tb(error.__traceback__)[-5:]]
    finally:
        if candidate:
            candidate.cleanup()
            candidate.manifest()
            private_write(candidate.output / 'candidate.json', candidate.report)
        for signum, handler in old_handlers.items():
            signal.signal(signum, handler)
        os.umask(previous_umask)
    return candidate.report


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--source', required=True, type=Path)
    parser.add_argument('--output', required=True, type=Path)
    parser.add_argument('--image', required=True)
    parser.add_argument('--simulator-image', required=True)
    parser.add_argument('--image-identity', required=True, type=Path)
    parser.add_argument('--simulator-identity', required=True, type=Path)
    parser.add_argument('--routing-only', action='store_true')
    parser.add_argument('--previous-image', help='普通升级合同使用的固定旧镜像；必须与新镜像修订不同')
    parser.add_argument('--traffic-host-interface', metavar='NAME',
                        help='只读挂载宿主 /proc/1/net/dev 并读取该网卡；默认使用主监听器计数')
    args = parser.parse_args()
    try:
        report = run(args.source, args.output, args.image, args.traffic_host_interface, args.previous_image, args.simulator_image, args.routing_only,
                     args.image_identity, args.simulator_identity)
    except Exception as error:
        report = {'success': False, 'error_type': type(error).__name__,
                  'error_code': str(error) if isinstance(error, CandidateError) else 'candidate_initialization_failed'}
    # success 只表示候选阶段通过；正式更新另需 deployment_eligible（干净源码、匹配镜像与 gitlink）。
    print(json.dumps({'success': report['success'], 'deployment_eligible': report.get('deployment_eligible', False),
                      'error_code': report.get('error_code'),
                      'report': str(args.output / 'candidate.json')}, ensure_ascii=False))
    return report.get('exit_code', 0 if report['success'] else 1)


if __name__ == '__main__':
    raise SystemExit(main())
