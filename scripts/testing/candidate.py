#!/usr/bin/env python3
"""用全新临时依赖运行候选业务合同。只接收本地源码、产物目录和已构建镜像。"""
from __future__ import annotations

import argparse
import hashlib
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
import time
import threading

LABEL = 'renai.local-test.run'
STAGES = ('billing', 'ws', 'compact', 'plugin-before', 'plugin-after', 'upstream_sync', 'sdk')


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
            digest.update(hashlib.sha256(path.read_bytes()).digest())
        else:
            digest.update(b'deleted\0')
        count += 1
    status = execute(['git', '-C', str(source), 'status', '--porcelain']).stdout
    return {'git_head': head, 'candidate_sha': None if status else head,
            'working_tree_sha256': digest.hexdigest(), 'files': count, 'dirty': bool(status)}


class Candidate:
    def __init__(self, source, output, image):
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
        self.workspace_identity = source_identity(self.source.parent)
        self.workspace_sha = self.workspace_identity['git_head']
        self.report = {'success': False, 'layer': 'candidate', 'run_id': self.run_id,
                       'source': source_identity(self.source), 'workspace': self.workspace_identity, 'stages': {}, 'cleanup': {},
                       'external_requests': 0, 'database_lifecycle': 'fresh_tmpfs',
                       'limits': ['本地模拟供应商和支付；不验证真实供应商、真实支付或生产部署。']}

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
                 '-subj', '/CN=Ren2Hub Local Contract', '-addext', 'subjectAltName=IP:127.0.0.1',
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

    def prepare(self):
        self.sdk_runtimes()
        image = json.loads(self.docker('image', 'inspect', self.image).stdout)[0]
        labels = image['Config'].get('Labels') or {}
        revision = labels.get('org.opencontainers.image.revision', '')
        require(re.fullmatch(r'[0-9a-f]{40}', revision) and labels.get('org.opencontainers.image.version') == revision,
                'immutable_application_revision_labels_required')
        self.image_id, self.revision = image['Id'], revision
        self.report['image'] = {'reference': self.image, 'id': self.image_id, 'revision': revision}
        # 不把现有镜像的测试结果冒充未提交工作树构建结果。
        self.report['image_matches_clean_source'] = self.report['source']['candidate_sha'] == revision
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
        self.network_guard = self.lock_network()
        app_env = self.env_file('app.env', {
            'SQL_DSN': 'postgresql://ren2hub:' + passwords['POSTGRES_PASSWORD'] + '@postgres:5432/ren2hub_candidate',
            'REDIS_CONN_STRING': 'redis://:' + passwords['REDIS_PASSWORD'] + '@redis:6379/0',
            'SESSION_SECRET': passwords['SESSION_SECRET'], 'CRYPTO_SECRET': passwords['CRYPTO_SECRET'],
            'LOG_SQL_DSN': '', 'SQLITE_PATH': '', 'MEMORY_CACHE_ENABLED': 'true', 'NODE_TYPE': 'master',
            'CRITICAL_RATE_LIMIT': '200', 'GLOBAL_API_RATE_LIMIT': '1000', 'TRUSTED_PROXIES': 'none',
            'SSL_CERT_FILE': '/etc/ren2hub/mock-ca.crt', 'TICKET_UPLOAD_DIR': '/app/ticket-attachments',
            'SESSION_COOKIE_SECURE': 'false', 'SESSION_COOKIE_TRUSTED_URL': '', 'TZ': 'UTC'})
        self.start('app', self.image_id, ('--network', 'container:' + self.containers['mock'],
                   '--env-file', str(app_env), '--label', 'org.opencontainers.image.revision=' + revision,
                   '--label', 'org.opencontainers.image.version=' + revision,
                   '--mount', 'type=bind,src=' + str(certificate) + ',dst=/etc/ren2hub/mock-ca.crt,readonly',
                   '--tmpfs', '/app/ticket-attachments:rw,size=128m', '--tmpfs', '/tmp:rw,size=128m',
                   '--stop-timeout', '130', '--health-start-period', '15s',
                   '--health-cmd', 'wget -q -T 3 -O /dev/null http://127.0.0.1:3000/api/status', *health),
                   ('--log-dir', '/tmp/ren2hub-local-logs'))
        self.wait_healthy('app')
        target = {role + '_container': self.prefix + '-' + role for role in self.containers}
        target.update(database_user='ren2hub', database_name='ren2hub_candidate', database_host='postgres:5432',
                      redis_host='redis:6379', network=self.network, network_id=self.network_id, run_id=self.run_id,
                      container_ids=self.containers, image_id=self.image_id, revision=self.revision, ca_path=str(certificate), network_guard=self.network_guard)
        private_write(self.private / 'target.json', target)
        self.report['isolation'] = {'network_id': self.network_id, 'internal': True,
                                    'container_ids': dict(self.containers), 'published_ports': [], 'storage': 'tmpfs', 'network_guard': self.network_guard}

    def namespace(self, role, *command):
        item = self.owned(role)
        pid = str(item['State']['Pid'])
        namespace = execute(['sudo', '-n', 'readlink', '/proc/' + pid + '/ns/net']).stdout.decode().strip()
        require(namespace != os.readlink('/proc/self/ns/net'), 'host_namespace_mutation_rejected')
        return execute(['sudo', '-n', 'nsenter', '--target', pid, '--net', *command])

    def lock_network(self):
        """规则只写入本轮容器 netns；internal bridge 本身不能隔离宿主 INPUT。"""
        addresses = {}
        for role in ('postgres', 'redis', 'mock'):
            info = self.owned(role)
            addresses[role] = info['NetworkSettings']['Networks'][self.network]['IPAddress']
        rules = {}
        for role in addresses:
            self.namespace(role, '/usr/bin/ip', 'route', 'flush', 'default')
            for family, binary in (('ipv4', '/usr/sbin/iptables'), ('ipv6', '/usr/sbin/ip6tables')):
                self.namespace(role, binary, '-w', '5', '-P', 'OUTPUT', 'DROP')
                self.namespace(role, binary, '-w', '5', '-A', 'OUTPUT', '-o', 'lo', '-j', 'ACCEPT')
                if family == 'ipv4':
                    for address in sorted(addresses.values()):
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
        return {'addresses': addresses, 'output_rules_sha256': rules, 'host_probe_blocked': True,
                'scope': 'owned_container_network_namespaces_only'}

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
        private_write(self.output / 'candidate.json', self.report)
        require(result.returncode == 0 and stage_report.get('success') is True, 'candidate_stage_failed_' + stage)

    def manifest(self):
        reports = {}
        for stage in STAGES:
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
            self.report['error_code'] = 'source_or_workspace_changed_during_candidate'
        workspace_clean = workspace_end['candidate_sha'] == self.workspace_sha
        gitlink = execute(['git', '-C', str(self.source.parent), 'ls-tree', 'HEAD', 'Ren2Hub']).stdout.decode().split()
        linked = len(gitlink) == 4 and gitlink[:3] == ['160000', 'commit', self.report.get('image', {}).get('revision')]
        complete = len(reports) == len(STAGES) and all(self.report['stages'].get(stage, {}).get('success') is True for stage in STAGES)
        deployable = bool(self.report['success'] and complete and self.report.get('image_matches_clean_source') and workspace_clean and linked)
        manifest = {'success': deployable, 'candidate_success': self.report['success'], 'deployment_eligible': deployable,
                    'source_sha': self.report.get('image', {}).get('revision'),
                    'workspace_sha': self.workspace_sha, 'image_id': self.report.get('image', {}).get('id'),
                    'reports': reports, 'source_worktree': self.report['source'], 'identity_verification': self.report['identity_verification'],
                    'image_matches_clean_source': self.report.get('image_matches_clean_source', False)}
        private_write(self.output / 'candidate-manifest.json', manifest)
        self.report['deployment_eligible'] = deployable

    def cleanup(self):
        errors = []
        removed = []
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
        self.report['cleanup'] = {'success': not errors, 'removed_containers': removed, 'errors': errors}
        if errors:
            self.report['success'] = False


def interrupted(_signum, _frame):
    raise CandidateError('candidate_interrupted')


def run(source: Path, output: Path, image: str) -> dict:
    previous_umask = os.umask(0o077)
    candidate = None
    old_handlers = {}
    if threading.current_thread() is threading.main_thread():
        for signum in (signal.SIGINT, signal.SIGTERM):
            old_handlers[signum] = signal.signal(signum, interrupted)
    try:
        candidate = Candidate(source, output, image)
        candidate.prepare()
        for stage in STAGES:
            candidate.stage(stage)
        candidate.report['success'] = True
    except Exception as error:
        if candidate is None:
            raise
        candidate.report['error_code'] = str(error) if isinstance(error, CandidateError) else 'candidate_execution_failed'
        candidate.report['error_type'] = type(error).__name__
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
    args = parser.parse_args()
    try:
        report = run(args.source, args.output, args.image)
    except Exception as error:
        report = {'success': False, 'error_type': type(error).__name__,
                  'error_code': str(error) if isinstance(error, CandidateError) else 'candidate_initialization_failed'}
    # success 只表示候选阶段通过；正式更新另需 deployment_eligible（干净源码、匹配镜像与 gitlink）。
    print(json.dumps({'success': report['success'], 'deployment_eligible': report.get('deployment_eligible', False),
                      'error_code': report.get('error_code'),
                      'report': str(args.output / 'candidate.json')}, ensure_ascii=False))
    return 0 if report['success'] else 1


if __name__ == '__main__':
    raise SystemExit(main())
