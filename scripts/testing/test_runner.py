"""保护统一入口的证据判定与环境隔离，避免退出 0 冒充完整测试。"""
import contextlib
import errno
import io
import json
from pathlib import Path
import socket
import sys
import tempfile
from types import SimpleNamespace
import unittest
from unittest.mock import patch

import candidate
from contracts import gateway
from contracts.common import RequirementError, acceptance_target, require_candidate_mounts
import run
from run import environment, go_result, playwright_cases, playwright_result, ui_contracts

COUNTER = ['bind', '/proc/1/net/dev', '/host/proc/net/dev', True]
COUNTER_MOUNT = {'Type': 'bind', 'Source': '/proc/1/net/dev', 'Destination': '/host/proc/net/dev', 'RW': False}
CA_MOUNT = {'Type': 'bind', 'Source': '/run/candidate/server.crt', 'Destination': '/etc/ren2hub/mock-ca.crt', 'RW': False}


class RunnerContractTests(unittest.TestCase):
    def test_ui_runs_keep_origin_stable_after_recent_server_shutdown(self):
        with tempfile.TemporaryDirectory() as directory, socket.socket() as available:
            root = Path(directory)
            suite = root / 'frontend/tests/visual'
            suite.mkdir(parents=True)
            (suite / 'one.spec.ts').write_text('')
            case = {'id': 'one', 'file': 'one.spec.ts', 'tests': [{
                'expectedStatus': 'passed', 'status': 'expected',
                'results': [{'status': 'passed', 'retry': 0}]}]}
            report = {'suites': [{'specs': [case]}], 'stats': {'expected': 1}}
            available.bind(('127.0.0.1', 0))
            port = available.getsockname()[1]
            available.close()
            observed = []
            attachments = []

            def execute(command, cwd, output, name, env, timeout):
                observed.append(env['PLAYWRIGHT_PORT'])
                attachments.append(command[command.index('--output') + 1])
                if '--list' not in command:
                    Path(env['PLAYWRIGHT_JSON_OUTPUT_FILE']).write_text(json.dumps(report))
                return json.dumps(report)

            with patch.dict('os.environ', {}, clear=True), \
                    patch('run.UI_CONTRACTS_PORT', port), patch('run.execute', side_effect=execute):
                self.assertTrue(ui_contracts(root, root, {})['success'])
                # Close the server side first, leaving a real TIME_WAIT socket.
                # A completed visual run must not block the next fixed-origin run.
                with socket.socket() as server, socket.socket() as client:
                    server.setsockopt(socket.SOL_SOCKET, socket.SO_REUSEADDR, 1)
                    server.bind(('127.0.0.1', port))
                    server.listen(1)
                    client.settimeout(2)
                    client.connect(('127.0.0.1', port))
                    connection, _ = server.accept()
                    connection.close()
                    self.assertEqual(client.recv(1), b'')
                self.assertTrue(ui_contracts(root, root, {})['success'])
            self.assertEqual(observed, [str(port)] * 4)
            self.assertEqual(attachments, [str(root / 'artifacts')] * 4)

    def test_occupied_visual_port_fails_without_random_port_fallback(self):
        with tempfile.TemporaryDirectory() as directory, socket.socket() as occupied:
            root = Path(directory)
            suite = root / 'frontend/tests/visual'
            suite.mkdir(parents=True)
            (suite / 'one.spec.ts').write_text('')
            occupied.setsockopt(socket.SOL_SOCKET, socket.SO_REUSEADDR, 1)
            occupied.bind(('127.0.0.1', 0))
            occupied.listen(1)
            with patch.dict('os.environ', {}, clear=True), \
                    patch('run.UI_CONTRACTS_PORT', occupied.getsockname()[1]), \
                    patch('run.execute') as execute:
                with self.assertRaises(OSError) as failure:
                    ui_contracts(root, root, {})
                self.assertEqual(failure.exception.errno, errno.EADDRINUSE)
                execute.assert_not_called()

    def test_unified_ui_fails_when_visual_suite_is_missing(self):
        with tempfile.TemporaryDirectory() as directory, patch('run.execute') as execute:
            with self.assertRaisesRegex(RuntimeError, 'no browser specs'):
                ui_contracts(Path(directory), Path(directory), {})
            execute.assert_not_called()

    def test_browser_inventory_rejects_missing_or_unregistered_suites(self):
        report = {'suites': [{'specs': [{'id': 'one', 'file': 'one.spec.ts',
                    'tests': [{'projectName': ''}]}]}]}
        self.assertEqual(playwright_cases(report, {'one.spec.ts'}), {'one'})
        for files in ({'one.spec.ts', 'missing.spec.ts'}, {'different.spec.ts'}):
            with self.subTest(files=files), self.assertRaises(RuntimeError):
                playwright_cases(report, files)

    def test_browser_results_require_each_discovered_case_once_without_retries(self):
        case = {'id': 'one', 'file': 'one.spec.ts', 'tests': [{
            'expectedStatus': 'passed', 'status': 'expected',
            'results': [{'status': 'passed', 'retry': 0}]}]}
        report = {'suites': [{'specs': [case]}], 'stats': {'expected': 1}}
        self.assertEqual(playwright_result(report, {'one'}), 1)
        with self.assertRaises(RuntimeError):
            playwright_result(report, {'one', 'missing'})
        for results in ([], [{'status': 'skipped'}],
                        [{'status': 'passed', 'retry': 1}],
                        [{'status': 'failed'}, {'status': 'passed'}]):
            case['tests'][0]['results'] = results
            with self.subTest(results=results), self.assertRaises(RuntimeError):
                playwright_result(report, {'one'})

    def test_go_rejects_empty_skipped_missing_and_failed_events(self):
        events = [[], [{'Action': 'skip', 'Test': 'TestMoney'}],
                  [{'Action': 'pass', 'Test': 'TestDifferent'}],
                  [{'Action': 'fail', 'Test': 'TestMoney'}]]
        for value in events:
            with self.subTest(events=value), self.assertRaises(RuntimeError):
                go_result('\n'.join(map(json.dumps, value)), ['TestMoney'])

    def test_go_requires_parent_pass_even_when_subtest_passes(self):
        with self.assertRaises(RuntimeError):
            go_result(json.dumps({'Action': 'pass', 'Test': 'TestMoney/sqlite'}), ['TestMoney'])

    def test_go_accepts_executed_test(self):
        result = go_result(json.dumps({'Action': 'pass', 'Test': 'TestMoney'}), ['TestMoney'])
        self.assertEqual(result['top_level_passed'], 1)

    def test_environment_drops_database_credentials_proxies_and_go_flags(self):
        with tempfile.TemporaryDirectory() as directory, patch.dict('os.environ', {
            'SQL_DSN': 'production', 'TEST_MYSQL_DSN': 'production', 'HTTP_PROXY': 'private',
            'OPENAI_API_KEY': 'private', 'GOFLAGS': '-overlay=unsafe', 'SESSION_SECRET': 'private',
        }):
            actual = environment(Path(directory))
        self.assertTrue(all(key not in actual for key in (
            'SQL_DSN', 'TEST_MYSQL_DSN', 'HTTP_PROXY', 'OPENAI_API_KEY', 'SESSION_SECRET')))
        self.assertEqual(actual['GOFLAGS'], '-mod=readonly')
        self.assertEqual(actual['GOENV'], 'off')
        self.assertEqual(actual['GOWORK'], 'off')

    def test_candidate_rejects_missing_sdk_runtime_before_any_container(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            python = root / 'python3'
            python.write_text('#!/bin/sh\n')
            python.chmod(0o700)
            module = root / 'openai'
            module.mkdir()
            runner = candidate.Candidate.__new__(candidate.Candidate)
            runner.source = root
            runner.docker = lambda *argv, **kwargs: self.fail('docker called before SDK preflight')
            cases = (
                ({'GATEWAY_SDK_PYTHON': str(root / 'missing')}, 'explicit_sdk_python_runtime_required'),
                ({'GATEWAY_SDK_PYTHON': str(python), 'GATEWAY_SDK_NODE': 'node'}, 'explicit_sdk_node_runtime_required'),
                ({'GATEWAY_SDK_PYTHON': str(python), 'GATEWAY_SDK_NODE': str(python),
                  'GATEWAY_SDK_NODE_MODULE': str(module)}, 'explicit_sdk_node_module_required'),
            )
            for env, code in cases:
                with self.subTest(code=code), patch.dict('os.environ', env, clear=True), \
                        self.assertRaisesRegex(candidate.CandidateError, code):
                    runner.prepare()
            (module / 'package.json').write_text('{}')
            env = {'GATEWAY_SDK_PYTHON': str(python), 'GATEWAY_SDK_NODE': str(python), 'GATEWAY_SDK_NODE_MODULE': str(module)}
            with patch.dict('os.environ', env, clear=True):
                runner.sdk_runtimes()
            self.assertEqual(runner.sdk_environment, env)
            (module / 'index.mjs').write_text('')
            env['GATEWAY_SDK_NODE_MODULE'] = str(module / 'index.mjs')
            with patch.dict('os.environ', env, clear=True):
                runner.sdk_runtimes()


class HostTrafficCandidateTests(unittest.TestCase):
    def prepared(self, root, interface):
        """用记录参数的假 docker 跑一次 prepare；app 的 Mounts 由实际传入的 --mount 推导。"""
        (root / 'deploy/ren2hub').mkdir(parents=True, exist_ok=True)
        (root / 'deploy/ren2hub/docker-compose.preview.yml').write_text(
            '    image: postgres:15@sha256:' + '1' * 64 + '\n    image: redis:7@sha256:' + '2' * 64 + '\n')
        (root / 'private').mkdir(mode=0o700, exist_ok=True)
        runner = candidate.Candidate.__new__(candidate.Candidate)
        runner.source, runner.private, runner.run_id = root / 'Ren2Hub', root / 'private', 'f' * 24
        runner.prefix = 'ren2hub-candidate-local-' + runner.run_id
        runner.containers, runner.image, runner.traffic_interface = {}, 'ren2hub-test:local', interface or ''
        runner.report = {'source': {'candidate_sha': 'a' * 40}}
        started = {}
        labels = {'org.opencontainers.image.revision': 'a' * 40, 'org.opencontainers.image.version': 'a' * 40}

        def docker(*argv, **kwargs):
            if argv[:2] == ('image', 'inspect'):
                return SimpleNamespace(stdout=json.dumps([{'Id': argv[2], 'Config': {'Labels': labels}}]).encode())
            self.assertEqual(argv[:2], ('network', 'create'))
            return SimpleNamespace(stdout=b'e' * 64 + b'\n')

        def start(role, image, options=(), command=()):
            started[role] = list(options)
            runner.containers[role] = str(len(started)) * 64

        def owned(role):
            options = started[role]
            mounts = []
            for spec in (options[index + 1] for index, value in enumerate(options) if value == '--mount'):
                fields = dict(part.partition('=')[::2] for part in spec.split(','))
                mounts.append({'Type': fields['type'], 'Source': fields['src'], 'Destination': fields['dst'],
                               'RW': 'readonly' not in fields})
            return {'Mounts': mounts}

        runner.docker, runner.start, runner.owned = docker, start, owned
        runner.sdk_runtimes, runner.wait_healthy = lambda: None, lambda role: None
        runner.tls, runner.lock_network = lambda: root / 'private/server.crt', lambda: {'addresses': {}}
        runner.inspect = lambda identifier: {'Internal': True, 'Labels': {candidate.LABEL: runner.run_id}}
        runner.prepare()
        env = dict(line.split('=', 1) for line in (root / 'private/app.env').read_text().splitlines())
        with patch.dict('os.environ', {'REN2HUB_ACCEPTANCE_TARGET': str(root / 'private/target.json')}):
            target = acceptance_target()
        return started['app'], env, target, runner.report['isolation']['traffic']

    def test_host_interface_adds_only_one_readonly_counter_mount_and_env(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            host, host_env, host_target, host_report = self.prepared(root, 'eth0')
            listener, listener_env, listener_target, listener_report = self.prepared(root, None)
        counter = ['--mount', 'type=bind,src=/proc/1/net/dev,dst=/host/proc/net/dev,readonly']
        start = host.index(counter[1]) - 1
        self.assertEqual(host[start:start + 2], counter)
        self.assertEqual(host[:start] + host[start + 2:], listener)
        self.assertEqual(set(host_env) - set(listener_env), {'TRAFFIC_HOST_INTERFACE'})
        self.assertEqual(set(listener_env) - set(host_env), set())
        self.assertEqual(host_env['TRAFFIC_HOST_INTERFACE'], 'eth0')
        self.assertNotIn('TRAFFIC_HOST_NET_DEV', host_env)
        self.assertEqual((host_target['traffic_host_interface'], listener_target['traffic_host_interface']), ('eth0', ''))
        self.assertEqual(host_report, {'configured_interface': 'eth0', 'scope': None, 'interface': None, 'mounts': [COUNTER]})
        self.assertEqual(listener_report, {'configured_interface': '', 'scope': None, 'interface': None, 'mounts': []})

    def test_invalid_interface_is_rejected_before_any_output(self):
        with tempfile.TemporaryDirectory() as directory:
            for name in ('', 'eth0;id', 'a' * 16, 'eth 0', 'eth0/1', 'eth0\n'):
                with self.subTest(name=name), self.assertRaisesRegex(candidate.CandidateError, 'invalid_traffic_host_interface'):
                    candidate.Candidate(Path(directory), Path(directory) / 'out', 'image', name)
            self.assertFalse((Path(directory) / 'out').exists())

    def test_run_passes_traffic_interface_to_candidate(self):
        def execute(command, cwd, output, name, env, timeout=300):
            return {'go': 'go version go1.26.10 linux/amd64', 'bun': '1.4.0', 'node': 'v24.0.0'}[command[0]]
        for extra, expected in ((['--traffic-host-interface', 'eth0'], 'eth0'), ([], None)):
            with self.subTest(expected=expected), tempfile.TemporaryDirectory() as directory, \
                    patch.object(sys, 'argv', ['run.py', 'candidate', '--image', 'ren2hub-test:x', '--output-parent', directory, *extra]), \
                    patch('run.identity', return_value={'source_sha256': 'fixed'}), patch('run.execute', side_effect=execute), \
                    patch('run.signal.signal'), patch('run.os.umask'), contextlib.redirect_stdout(io.StringIO()), \
                    patch('candidate.run', return_value={'success': True}) as runner:
                self.assertEqual(run.main(), 0)
                source, stage, image, interface = runner.call_args.args
                self.assertEqual((source, stage.name, image, interface), (run.SOURCE, 'candidate', 'ren2hub-test:x', expected))

    def test_run_rejects_invalid_interface_before_creating_evidence(self):
        with tempfile.TemporaryDirectory() as directory, patch('run.signal.signal'), \
                contextlib.redirect_stderr(io.StringIO()), patch('candidate.run') as runner:
            for name in ('', 'eth0;id', 'a' * 16):
                with self.subTest(name=name), self.assertRaises(SystemExit), patch.object(sys, 'argv', [
                        'run.py', 'candidate', '--image', 'x', '--output-parent', directory, '--traffic-host-interface', name]):
                    run.main()
            self.assertEqual(list(Path(directory).iterdir()), [])
            runner.assert_not_called()

    def test_isolation_accepts_counter_mount_only_when_configured(self):
        host, listener = {'ca_path': CA_MOUNT['Source'], 'traffic_host_interface': 'eth0'}, {'ca_path': CA_MOUNT['Source'], 'traffic_host_interface': ''}
        tmpfs = {'Type': 'tmpfs', 'Source': '', 'Destination': '/tmp', 'RW': True}
        require_candidate_mounts('app', {'Mounts': [CA_MOUNT, COUNTER_MOUNT, tmpfs]}, host)
        require_candidate_mounts('app', {'Mounts': [CA_MOUNT, tmpfs]}, listener)
        rejected = (
            ('listener_has_counter', [CA_MOUNT, COUNTER_MOUNT], listener, 'candidate_unexpected_bind_mount'),
            ('wrong_source', [CA_MOUNT, {**COUNTER_MOUNT, 'Source': '/proc/self/net/dev'}], host, 'candidate_unexpected_bind_mount'),
            ('whole_proc_net', [CA_MOUNT, {**COUNTER_MOUNT, 'Source': '/proc/1/net', 'Destination': '/host/proc/net'}], host, 'candidate_unexpected_bind_mount'),
            ('writable', [CA_MOUNT, {**COUNTER_MOUNT, 'RW': True}], host, 'candidate_unexpected_bind_mount'),
            ('extra_bind', [CA_MOUNT, COUNTER_MOUNT, {**CA_MOUNT, 'Destination': '/data'}], host, 'candidate_unexpected_bind_mount'),
            ('second_counter', [CA_MOUNT, COUNTER_MOUNT, COUNTER_MOUNT], host, 'candidate_traffic_counter_mount_mismatch'),
            ('missing_counter', [CA_MOUNT], host, 'candidate_traffic_counter_mount_mismatch'),
            ('writable_ca', [{**CA_MOUNT, 'RW': True}, COUNTER_MOUNT], host, 'candidate_unexpected_bind_mount'),
        )
        for name, mounts, target, code in rejected:
            with self.subTest(name), self.assertRaisesRegex(RequirementError, code):
                require_candidate_mounts('app', {'Mounts': mounts}, target)
        # Datastores and the mock keep the tmpfs-only rule in either mode.
        with self.assertRaisesRegex(RequirementError, 'candidate_persistent_storage_rejected'):
            require_candidate_mounts('postgres', {'Mounts': [COUNTER_MOUNT]}, host)

    def test_manifest_traffic_comes_from_management_report_and_pre_cleanup_inspect(self):
        writable = {**COUNTER_MOUNT, 'RW': True}
        cases = (('host_interface', 'eth0', [CA_MOUNT, COUNTER_MOUNT], [COUNTER]),
                 ('application_listener', '', [CA_MOUNT], []),
                 ('host_interface', 'eth0', [CA_MOUNT, writable], [COUNTER[:3] + [False]]))
        for scope, interface, mounts, expected in cases:
            with self.subTest(scope=scope, mounts=expected), tempfile.TemporaryDirectory() as directory:
                root = Path(directory)
                runner = candidate.Candidate.__new__(candidate.Candidate)
                runner.output, runner.private, runner.revision = root, root / 'private', 'a' * 40
                runner.image_id, runner.sdk_environment, runner.containers = 'sha256:image', {}, {'app': 'a' * 64}
                identity = {'git_head': 'b' * 40, 'candidate_sha': 'b' * 40}
                runner.source, runner.workspace_identity, runner.workspace_sha = root / 'Ren2Hub', identity, 'b' * 40
                runner.report = {'success': True, 'stages': {}, 'source': identity, 'image': {'revision': 'a' * 40, 'id': 'sha256:image'},
                                 'isolation': {'traffic': {'configured_interface': interface, 'scope': None, 'interface': None, 'mounts': []}}}
                app = {'State': {'Running': True, 'Pid': 1}, 'Image': 'sha256:image', 'Mounts': mounts}
                runner.owned = lambda role, app=app: app

                def execute(command, **kwargs):
                    if command[0] == 'git':
                        return SimpleNamespace(stdout=('160000 commit ' + 'a' * 40 + '\tRen2Hub\n').encode())
                    report = {'success': True, 'group': 'management', 'checks': ['traffic_source_matches_configuration'],
                              'traffic': {'scope': scope, 'interface': interface}}
                    (root / 'upstream_sync.json').write_text(json.dumps(report))
                    return SimpleNamespace(returncode=0)

                with patch('candidate.execute', side_effect=execute), patch('candidate.source_identity', return_value=identity):
                    runner.stage('upstream_sync')
                    runner.owned = lambda role: self.fail('manifest must not inspect after cleanup')
                    runner.manifest()
                manifest = json.loads((root / 'candidate-manifest.json').read_text())
                self.assertEqual(manifest['traffic'], {'scope': scope, 'interface': interface, 'mounts': expected})
                self.assertEqual(runner.report['isolation']['traffic'],
                                 {'configured_interface': interface, 'scope': scope, 'interface': interface, 'mounts': expected})

    def test_management_waits_for_rates_then_requires_configured_source(self):
        def stats(scope='host_interface', interface='eth0', inbound=0.5, outbound=0, reason='', stale=False):
            return {'observability': {'stale': stale, 'traffic': {'scope': scope, 'inbound_mbps': inbound,
                    'outbound_mbps': outbound, 'interface': interface, 'reason': reason}}}

        def check(configured, responses, timeout=45):
            experiment = gateway.Experiment.__new__(gateway.Experiment)
            experiment.checks, experiment.traffic = [], None
            experiment.root = SimpleNamespace(api=lambda method, path: responses.pop(0))
            with patch('contracts.gateway.acceptance_target', return_value={'traffic_host_interface': configured}), \
                    patch('contracts.gateway.time.sleep'):
                experiment.traffic_source(timeout)
            return experiment

        experiment = check('eth0', [stats(inbound=None, outbound=None), stats()])
        self.assertEqual((experiment.traffic, experiment.checks),
                         ({'scope': 'host_interface', 'interface': 'eth0'}, ['traffic_source_matches_configuration']))
        # Host mode: 0 is a valid reading, the candidate's requests never touch the host interface.
        self.assertEqual(check('eth0', [stats(inbound=0, outbound=0)]).traffic, {'scope': 'host_interface', 'interface': 'eth0'})
        # Listener mode: the stage's own requests cross the main listener, so it waits for both directions above 0.
        listener = check('', [stats('application_listener', '', 0, 0), stats('application_listener', '', 0.01, 0.02)])
        self.assertEqual(listener.traffic, {'scope': 'application_listener', 'interface': ''})
        with self.assertRaisesRegex(RequirementError, 'listener_traffic_not_counted'):
            check('', [stats('application_listener', '', 0.5, 0)], timeout=0)
        rejected = (
            ('', [stats()], 'traffic_source_mismatch'),
            ('eth1', [stats()], 'traffic_source_mismatch'),
            ('eth0', [stats(inbound=None, outbound=None, reason='host_counters_unreadable')], 'traffic_reason_reported'),
            ('eth0', [stats(inbound=-1)], 'traffic_rate_invalid'),
            ('', [stats('application_listener', '', -1, 1)], 'traffic_rate_invalid'),
            ('eth0', [stats(inbound=float('inf'))], 'traffic_rate_invalid'),
            ('eth0', [stats(stale=True)], 'traffic_observation_stale'),
        )
        for configured, responses, code in rejected:
            with self.subTest(code=code, configured=configured), self.assertRaisesRegex(RequirementError, code):
                check(configured, responses)
        with self.assertRaisesRegex(RequirementError, 'traffic_rates_unavailable'):
            check('eth0', [stats(inbound=None, outbound=None)], timeout=0)


if __name__ == '__main__':
    unittest.main()
