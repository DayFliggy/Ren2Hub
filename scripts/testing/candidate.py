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
DEPENDENCY_IMAGES = {
    'postgres': 'postgres:15-bookworm@sha256:e8db9bd3e9e1751eb639fb17be53cc6d1b62a322adf75b99e791767a7a16ce69',
    'redis': 'redis:7-bookworm@sha256:e9b2e45ecd47fbb69b877cf8d045d5cccaaaed52524b6e098b4abe8212994f73',
}
HOST_INTERFACE = r'[A-Za-z0-9_.-]{1,15}'
# 除本轮生成的 mock CA 证书外，宿主网卡计数文件是候选唯一允许挂载的宿主文件；它不是数据路径，只读单文件。
COUNTER_SOURCE, COUNTER_TARGET = '/proc/1/net/dev', '/host/proc/net/dev'


class CandidateError(RuntimeError):
    """只保存预定义诊断码，避免命令行、DSN 或响应正文进入报告。"""


def require(condition, code):
    if not condition:
        raise CandidateError(code)


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
    # 只读核对：不让 status 刷新索引，否则以 root 运行的发布校验器会把索引改成 root 所有。
    status = execute(['git', '-C', str(source), '--no-optional-locks', 'status', '--porcelain']).stdout
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
    def __init__(self, source, output, image, traffic_interface=None, base_revision=None, simulator_image=None, routing_only=False,
                 image_identity=None, simulator_identity=None, scope=None):
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
        self.base_revision = base_revision
        self.simulator_image = simulator_image
        self.image_identity_path = image_identity
        self.simulator_identity_path = simulator_identity
        self.routing_only = routing_only
        # scope 为 None 时是完整候选；否则只运行按改动模块选出的部分，阶段在 prepare 读到基线提交后确定。
        self.scope = scope
        require(not routing_only or scope is None, 'routing_has_no_scope')
        require(bool(simulator_image), "explicit_simulator_image_required")
        self.attachment_volume = None
        self.stages = ('routing',) if routing_only else () if scope is not None else STAGES
        self.report = {'success': False, 'layer': 'routing' if routing_only else 'candidate', 'run_id': self.run_id,
                       'source': source_identity(self.source), 'stages': {}, 'cleanup': {},
                       'external_requests': None, 'database_lifecycle': 'fresh_tmpfs',
                       'limits': ['本地模拟供应商和支付；不验证真实供应商、真实支付或生产部署。']}
        self.report['exit_code'] = 1
        if scope is not None:
            # 在确定阶段之前失败时，manifest 仍标明这是范围候选，不会被读成完整候选。
            self.report['scope'] = {'requested': sorted(scope)}

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

    def select_scope(self):
        """按请求和与基线提交的差异确定本轮验收范围。"""
        from release_scope import expand, required_parts, stages
        requested = set(self.scope)
        base, required = None, None
        if self.base_revision:
            base = self.base_revision
            require(re.fullmatch(r'[0-9a-f]{40}', base), 'complete_base_revision_required')
            # --no-renames：重命名按删除加新增列出，移走的文件不会被目标路径掩盖。
            changed = execute(['git', '-C', str(self.source), 'diff', '--name-only', '--no-renames', '-z',
                               base, 'HEAD', '--'], check=False)
            require(changed.returncode == 0, 'previous_revision_missing_in_source')
            required = required_parts(name for name in changed.stdout.decode().split('\0') if name)
        require('auto' not in requested or required is not None, 'auto_scope_requires_base_revision')
        parts = expand((requested - {'auto', 'boot'}) | set(required if 'auto' in requested else ()))
        require(required is None or set(required) <= set(parts), 'candidate_scope_missing_required_parts')
        self.stages = stages(parts)
        self.report['scope'] = {'requested': sorted(requested), 'parts': parts, 'required': required,
                                'base_revision': base}

    def prepare(self):
        self.sdk_environment = {}
        self.verify_build_identities()
        scoped = getattr(self, 'scope', None) is not None
        if scoped:
            self.select_scope()
        if not getattr(self, 'routing_only', False) and (not scoped or 'sdk' in self.stages):
            self.sdk_runtimes()
        image = json.loads(self.docker('image', 'inspect', self.image).stdout)[0]
        labels = image['Config'].get('Labels') or {}
        revision = labels.get('org.opencontainers.image.revision', '')
        require(re.fullmatch(r'[0-9a-f]{40}', revision),
                'immutable_application_revision_labels_required')
        self.image_id, self.revision = image['Id'], revision
        self.report['image'] = {'reference': self.image, 'id': self.image_id, 'revision': revision}
        # 不把现有镜像的测试结果冒充未提交工作树构建结果。
        self.report['image_matches_clean_source'] = image_matches_source(
            labels, self.report['source'], getattr(self, 'routing_only', False))
        self.report['image']['working_tree_sha256'] = labels.get('io.ren2hub.source-sha256')
        dependencies = {}
        for role, reference in DEPENDENCY_IMAGES.items():
            dependencies[role] = json.loads(self.docker('image', 'inspect', reference).stdout)[0]['Id']
        self.report['dependency_images'] = {
            role: {'reference': DEPENDENCY_IMAGES[role], 'id': image_id} for role, image_id in dependencies.items()
        }
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
            **({'TRAFFIC_HOST_INTERFACE': self.traffic_interface} if self.traffic_interface else {})})
        # 默认不挂载也不设网卡名，应用使用主监听器计数；--mount 的源缺失时容器创建直接失败。
        counter = ('--mount', 'type=bind,src=' + COUNTER_SOURCE + ',dst=' + COUNTER_TARGET + ',readonly') if self.traffic_interface else ()
        # Ticket uploads require 5 GiB free after the write, so attachments use a run-owned volume on host disk.
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
                      container_ids=dict(self.containers), image_id=self.image_id, revision=self.revision, ca_path=str(certificate), network_guard=self.network_guard,
                      traffic_host_interface=self.traffic_interface, attachment_volume=self.attachment_volume)
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
                                    'storage': 'tmpfs_and_owned_attachment_volume',
                                    'network_guard': self.network_guard,
                                    'traffic': traffic}

    def start_app(self):
        # Avoid Docker's loopback DNS forwarder: every permitted hostname has
        # an exact entry, and all DNS traffic is blocked in this namespace.
        hosts = '\n'.join(address + ' ' + role for role, address in self.network_guard['addresses'].items())
        self.start('app', self.image_id, (*self.app_options,
                   '--entrypoint', '/bin/sh',
                   '--label', 'org.opencontainers.image.revision=' + self.revision),
                   ('-c', 'printf "%s\\n" "$1" >> /etc/hosts; shift; exec /new-api "$@"',
                    'candidate-start', hosts, '--log-dir', '/tmp/ren2hub-local-logs'))

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
        if stage == 'routing-workload':
            from run import environment, routing_workload
            logs = self.output / 'routing-workload'
            logs.mkdir(mode=0o700)
            result = routing_workload(self.source, logs, environment(logs))
            self.report['stages'][stage] = result
            private_write(self.output / (stage + '.json'), result)
            return
        require(source_identity(self.source) == self.report['source'], 'source_changed_before_http_stage')
        app = self.owned('app')
        require(app['State']['Running'] and app['Image'] == self.image_id, 'candidate_image_or_state_changed')
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
        configuration = {'private': str(self.private), 'output': str(self.output), 'stage': stage, 'revision': self.revision}
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
        stable = source_end == self.report['source']
        self.report['identity_verification'] = {'stable': stable, 'source_end': source_end}
        if not stable:
            self.report['success'] = False
            self.report['exit_code'] = 1
            self.report['error_code'] = 'source_changed_during_candidate'
        if getattr(self, 'scope', None) is not None:
            return self.scoped_manifest(reports, stable)
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
                          and self.report.get('image_matches_clean_source'))
        traffic = self.report.get('isolation', {}).get('traffic')
        manifest = {'success': deployable, 'candidate_success': self.report['success'], 'deployment_eligible': deployable,
                    'source_sha': self.report.get('image', {}).get('revision'),
                    'image_id': self.report.get('image', {}).get('id'),
                    'reports': reports, 'source_worktree': self.report['source'], 'identity_verification': self.report['identity_verification'],
                    'image_matches_clean_source': self.report.get('image_matches_clean_source', False),
                    'simulator': self.report.get('simulator'),
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

    def scoped_manifest(self, reports, stable):
        """按模块选择的候选：所选阶段全部通过才可部署；未运行的部分不出现在 manifest，也不计为通过。"""
        scope = self.report.get('scope') or {}
        complete = isinstance(scope.get('parts'), list) and len(reports) == len(self.stages) and all(
            self.report['stages'].get(stage, {}).get('success') is True for stage in self.stages)
        deployable = bool(complete and self.report.get('exit_code') == 0 and stable
                          and not self.report.get('error_code') and self.report.get('cleanup', {}).get('success') is True
                          and self.report.get('image_matches_clean_source'))
        traffic = self.report.get('isolation', {}).get('traffic')
        manifest = {'success': deployable, 'candidate_success': self.report['success'], 'deployment_eligible': deployable,
                    'scope': scope, 'source_sha': self.report.get('image', {}).get('revision'),
                    'image_id': self.report.get('image', {}).get('id'),
                    'reports': reports, 'source_worktree': self.report['source'],
                    'identity_verification': self.report['identity_verification'],
                    'image_matches_clean_source': self.report.get('image_matches_clean_source', False),
                    'simulator': self.report.get('simulator'),
                    'build_identities': self.report.get('build_identities'),
                    # scope/interface 是 management 阶段的实测值，未选它时为 None；
                    # configured_interface 是候选容器实际使用的配置，发布校验器用它对照正式 compose。
                    'traffic': {key: traffic[key] for key in ('scope', 'interface', 'mounts', 'configured_interface')}
                    if traffic else None}
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
        base_revision: str | None = None, simulator_image: str | None = None, routing_only: bool = False,
        image_identity: Path | None = None, simulator_identity: Path | None = None,
        scope: list[str] | None = None) -> dict:
    previous_umask = os.umask(0o077)
    candidate = None
    old_handlers = {}
    if threading.current_thread() is threading.main_thread():
        for signum in (signal.SIGINT, signal.SIGTERM):
            old_handlers[signum] = signal.signal(signum, interrupted)
    try:
        candidate = Candidate(source, output, image, traffic_interface, base_revision, simulator_image, routing_only,
                              image_identity, simulator_identity, scope)
        # 冻结诊断与 race 是源码级检查，只随完整候选运行；按模块选择时使用各自的独立档位。
        if scope is None:
            candidate.diagnostics()
        candidate.prepare()
        for stage in candidate.stages:
            candidate.stage(stage)
        from routing_result import combined_exit_code
        results = list(candidate.report['stages'].values())
        if scope is None:
            results += [candidate.report[name] for name in (
                'diagnostics', 'known_upstream_restored', 'known_upstream_race',
                'known_upstream_logger_race', 'ordinary_race')]
        # 只选 boot 时没有阶段报告；镜像身份核对与全新实例启动成功即为通过。
        code = combined_exit_code(results) if results or scope is None else 0
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
    parser.add_argument('--base', dest='base_revision', help='auto 范围的基线提交（完整 SHA）')
    parser.add_argument('--traffic-host-interface', metavar='NAME',
                        help='只读挂载宿主 /proc/1/net/dev 并读取该网卡；默认使用主监听器计数')
    parser.add_argument('--scope', required=True,
                        help='auto[,部分]、boot 或 full（完整候选，只在用户明确要求全量时使用）；部分名见 release_scope.py')
    args = parser.parse_args()
    try:
        from release_scope import parse
        report = run(args.source, args.output, args.image, args.traffic_host_interface, args.base_revision, args.simulator_image, args.routing_only,
                     args.image_identity, args.simulator_identity, parse(args.scope))
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
