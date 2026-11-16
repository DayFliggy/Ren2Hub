"""保护统一入口的证据判定与环境隔离，避免退出 0 冒充完整测试。"""
import contextlib
import errno
import hashlib
import io
import json
import re
from pathlib import Path
import socket
import sys
import tempfile
from types import SimpleNamespace
import unittest
from unittest.mock import Mock, patch

import candidate
import browser
from contracts import gateway, worker
from contracts.common import RequirementError, acceptance_target, require_candidate_mounts
import run
from run import environment, go_result, playwright_cases, playwright_result, ui_contracts

COUNTER = ['bind', '/proc/1/net/dev', '/host/proc/net/dev', True]
COUNTER_MOUNT = {'Type': 'bind', 'Source': '/proc/1/net/dev', 'Destination': '/host/proc/net/dev', 'RW': False}
CA_MOUNT = {'Type': 'bind', 'Source': '/run/candidate/server.crt', 'Destination': '/etc/ren2hub/mock-ca.crt', 'RW': False}


class RunnerContractTests(unittest.TestCase):
    def test_browser_fixture_changes_only_critical_limit_window(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            process = Mock()
            process.poll.return_value = 1
            with patch.dict('os.environ', {'CRITICAL_RATE_LIMIT': '9999',
                                          'CRITICAL_RATE_LIMIT_ENABLE': 'false',
                                          'CRITICAL_RATE_LIMIT_DURATION': '1200'}), \
                    patch('browser.subprocess.Popen', return_value=process) as popen:
                result = browser.exercise(root, root, root / 'backend', 'desktop-light')
            self.assertFalse(result['success'])
            settings = popen.call_args.kwargs['env']
            self.assertEqual(settings['CRITICAL_RATE_LIMIT_DURATION'], '1')
            self.assertNotIn('CRITICAL_RATE_LIMIT', settings)
            self.assertNotIn('CRITICAL_RATE_LIMIT_ENABLE', settings)
            self.assertEqual(settings['SQLITE_PATH'], str(root / 'desktop-light-private/app.db'))

    def test_credential_race_group_is_independent_and_requires_all_ten_cases(self):
        # The standalone module owns both orchestration and release verification;
        # this profile must not maintain a second test-selection implementation.
        source, output, env = Path('/source'), Path('/output'), {'fixture': 'true'}
        result = {'success': True, 'contracts_success': True, 'exit_code': 0, 'counts': {'pass': 129}}
        with patch('ordinary_race.run', return_value=result) as execute:
            self.assertIs(run.race(source, output, env), result)
        execute.assert_called_once_with(source, output / 'ordinary-race', env)
        with patch('ordinary_race.load_selection', return_value=({}, [])) as selection:
            self.assertEqual(run.race_selection(source), ({}, []))
        selection.assert_called_once_with(source)

    def test_network_evidence_reports_real_counters_and_rejects_resets(self):
        runner = candidate.Candidate.__new__(candidate.Candidate)
        runner.network_guard = {'addresses': {'mock': '172.19.0.2'},
                                'initial_rejected_packets': {'mock_ipv4': 2, 'mock_ipv6': 0}}
        runner.report = {'stages': {'upstream_sync': {'marketplace': {
            'reports_received': 1, 'proxy_rejections': 0}}}}
        runner.network_rejections = lambda roles: {'mock_ipv4': 7, 'mock_ipv6': 1}
        runner.network_evidence()
        self.assertEqual(runner.report['external_requests']['rejected_packets'], 6)
        self.assertEqual(runner.report['external_requests']['marketplace_local_reports'], 1)
        self.assertFalse(runner.report['external_requests']['public_destinations_allowed'])
        runner.network_rejections = lambda roles: {'mock_ipv4': 1, 'mock_ipv6': 0}
        with self.assertRaisesRegex(candidate.CandidateError, 'candidate_rejection_counters_reset'):
            runner.network_evidence()

    def test_network_rejections_require_every_dns_and_default_reject_rule(self):
        runner = candidate.Candidate.__new__(candidate.Candidate)
        def namespace(role, binary, *args):
            rules = ['[2:80] -A OUTPUT -p udp --dport 53 -j REJECT --reject-with icmp-port-unreachable',
                     '[3:120] -A OUTPUT -p tcp --dport 53 -j REJECT --reject-with icmp-port-unreachable',
                     '[5:200] -A OUTPUT -j REJECT --reject-with icmp-port-unreachable']
            if binary.endswith('/iptables-save'):
                rules.append('[7:280] -A OUTPUT -d 127.0.0.11/32 -j REJECT --reject-with icmp-port-unreachable')
            return SimpleNamespace(stdout='\n'.join(rules).encode())
        runner.namespace = namespace
        self.assertEqual(runner.network_rejections(('mock',)), {'mock_ipv4': 17, 'mock_ipv6': 10})
        runner.namespace = lambda *args: SimpleNamespace(stdout=b'')
        with self.assertRaisesRegex(candidate.CandidateError, 'candidate_rejection_counters_missing'):
            runner.network_rejections(('mock',))

    def test_all_runs_other_layers_and_diagnoses_once_without_hiding_failure(self):
        for diagnostic_code in (1, 2):
            with self.subTest(diagnostic_code=diagnostic_code), tempfile.TemporaryDirectory() as directory:
                output = Path(directory)
                observed = []

                def passed(name, **fields):
                    def check(*args, **kwargs):
                        observed.append(name)
                        return {'success': True, **fields}
                    return check

                diagnostic = {'success': False, 'exit_code': diagnostic_code,
                              'cases': [{'name': 'A4', 'status': 'fail'}] if diagnostic_code == 1 else
                                       [{'name': 'N2', 'status': 'known_upstream'}]}

                def create_candidate(source, stage_output, *args):
                    observed.append('candidate')
                    runner = SimpleNamespace(source=source, output=stage_output, stages=('routing',),
                                             report={'success': False, 'stages': {}})
                    runner.diagnostics = lambda: candidate.Candidate.diagnostics(runner)
                    runner.prepare = lambda: observed.append('prepare')
                    runner.stage = lambda name: runner.report['stages'].update(
                        {name: {'success': False, 'contracts_success': True, 'exit_code': 2}})
                    runner.cleanup = lambda: observed.append('cleanup')
                    runner.manifest = lambda: runner.report.update(deployment_eligible=False)
                    return runner

                argv = ['run.py', 'all', '--output-parent', directory,
                        '--image', 'sha256:' + 'a' * 64, '--simulator-image', 'sha256:' + 'b' * 64,
                        '--image-identity', 'app-identity.json', '--simulator-identity', 'sim-identity.json']
                with contextlib.ExitStack() as stack:
                    for target, replacement in (
                            ('run.go_unit', passed('unit-go')), ('run.frontend_unit', passed('unit-vue')),
                            ('database.run', passed('databases')), ('run.static', passed('static')),
                            ('run.build', passed('build', binary=str(output / 'app'))),
                            ('run.race', passed('race')), ('browser.run', passed('browser')),
                            ('run.ui_contracts', passed('ui-contracts'))):
                        stack.enter_context(patch(target, side_effect=replacement))
                    stack.enter_context(patch.object(sys, 'argv', argv))
                    stack.enter_context(patch('run.identity', return_value={'source_sha256': 'stable'}))
                    stack.enter_context(patch('run.environment', return_value={}))
                    stack.enter_context(patch('run.execute', side_effect=['go version go1.26.10 linux/amd64', '1.4.0', 'v24.0.0']))
                    stack.enter_context(patch('run.os.umask', return_value=0o022))
                    stack.enter_context(patch('run.signal.signal'))
                    stack.enter_context(patch('candidate.Candidate', side_effect=create_candidate,
                                               diagnostics=candidate.Candidate.diagnostics))
                    probe = stack.enter_context(patch('diagnostics.run', return_value=diagnostic))
                    for module in ('native_restored', 'native_pricing_race', 'native_logger_race'):
                        stack.enter_context(patch(module + '.run', return_value={
                            'success': False, 'contracts_success': True, 'exit_code': 2}))
                    stack.enter_context(patch('ordinary_race.run', return_value={'success': True, 'exit_code': 0}))
                    stack.enter_context(patch('registry.run', side_effect=passed('registry')))
                    stack.enter_context(contextlib.redirect_stdout(io.StringIO()))
                    stack.enter_context(contextlib.redirect_stderr(io.StringIO()))
                    code = run.main()
                self.assertEqual(observed, ['registry', 'unit-go', 'unit-vue', 'databases', 'static', 'build',
                                            'race', 'browser', 'ui-contracts', 'candidate', 'prepare', 'cleanup'])
                probe.assert_called_once()
                report = json.loads(next(output.glob('run-*/report.json')).read_text())
                self.assertEqual(list(report['stages']), ['registry', 'unit', 'databases', 'static', 'build',
                                                         'race', 'browser', 'ui-contracts', 'candidate'])
                self.assertEqual(report['stages']['candidate']['diagnostics'], diagnostic)
                self.assertEqual(report['stages']['candidate']['stages']['routing']['exit_code'], 2)
                self.assertFalse(report['stages']['candidate']['deployment_eligible'])
                self.assertEqual(code, diagnostic_code)
                self.assertEqual(report['exit_code'], diagnostic_code)

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
                    patch('run.UI_CONTRACTS_PORT', port), patch('run.execute', side_effect=execute), \
                    patch('run.record_impact'):
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

    def test_targeted_filter_never_passes_on_an_empty_selection(self):
        found, deferred = ['TestTopUpBonusTiers', 'TestTopUpBonusDatabaseMatrix', 'TestWallet'], {
            'TestTopUpBonusDatabaseMatrix': 'databases 阶段在全新三数据库执行'}
        self.assertEqual(run.select_tests('m/model', found, deferred, None),
                         (['TestTopUpBonusTiers', 'TestWallet'], ['TestTopUpBonusDatabaseMatrix']))
        self.assertEqual(run.select_tests('m/model', found, deferred, '^TestTopUpBonus'),
                         (['TestTopUpBonusTiers'], ['TestTopUpBonusDatabaseMatrix']))
        with self.assertRaisesRegex(RuntimeError, '没有匹配任何测试'):
            run.select_tests('m/model', found, deferred, 'TestMissing')
        # 只匹配到矩阵测试时提示它属于另一层，而不是报告 0 项通过。
        with self.assertRaisesRegex(RuntimeError, '转交其他层的测试：TestTopUpBonusDatabaseMatrix'):
            run.select_tests('m/model', found, deferred, 'DatabaseMatrix$')

    def test_selectors_are_rejected_outside_their_profile(self):
        for argv in (['targeted'], ['affected', '--go', 'model'], ['quick', '--skip', 'race'],
                     ['all', '--scope', 'auto'], ['candidate', '--scope', 'routing'], ['affected', '--skip', 'registry']):
            with self.subTest(argv=argv), tempfile.TemporaryDirectory() as directory, \
                    patch.object(sys, 'argv', ['run.py', *argv, '--output-parent', directory]), \
                    patch('run.signal.signal'), contextlib.redirect_stderr(io.StringIO()), self.assertRaises(SystemExit):
                run.main()
            self.assertEqual(list(Path(directory).iterdir()) if Path(directory).exists() else [], [])

    def test_affected_and_targeted_share_one_executor_and_report_skipped_layers(self):
        selection = {'python': True, 'go': {'vet_modules': {'.': ['m/model']}, 'tested': ['m/model'],
                                            'patterns': {'m/model': '^TestTopUp'}},
                     'databases': {'packages': None, 'tests': ['TestWalletDatabases']}, 'race': ['model'],
                     'frontend': {'vitest': ['frontend/src/a.ts'], 'typecheck': False, 'lint': [], 'format': [], 'build': False},
                     'visual': {'mode': 'subset', 'files': ['wallet.spec.ts']}}
        calls = {}

        def record(name):
            def call(*args, **kwargs):
                calls[name] = args
                return {'success': True}
            return call

        with tempfile.TemporaryDirectory() as directory, contextlib.ExitStack() as stack:
            for target, name in (('run.python_unit', 'python'), ('run.go_vet', 'go-vet'), ('run.go_unit', 'go-unit'),
                                 ('database.run', 'databases'), ('ordinary_race.run', 'race'),
                                 ('run.frontend_related', 'vitest'), ('run.ui_contracts', 'ui-contracts')):
                stack.enter_context(patch(target, side_effect=record(name)))
            stack.enter_context(contextlib.redirect_stdout(io.StringIO()))
            stages, skipped = run.run_selection(run.SOURCE, Path(directory), {}, selection, 'targeted', ('race',))
        self.assertEqual(list(stages), ['python', 'go-vet', 'go-unit', 'databases', 'vitest', 'ui-contracts'])
        self.assertEqual(skipped, ['race'])
        self.assertEqual(calls['go-unit'][3:], ({'m/model'}, {'m/model': '^TestTopUp'}))
        self.assertEqual(calls['databases'][2:], (None, {'TestWalletDatabases'}))
        self.assertEqual(calls['ui-contracts'][3], ['wallet.spec.ts'])

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
                    runner.sdk_runtimes()
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
    def prepared(self, root, interface, simulator_address=None, guard_address=None):
        """用记录参数的假 docker 跑一次 prepare；app 的 Mounts 由实际传入的 --mount 推导。"""
        (root / 'deploy/ren2hub').mkdir(parents=True, exist_ok=True)
        (root / 'deploy/ren2hub/docker-compose.preview.yml').write_text(
            '    image: postgres:15@sha256:' + '1' * 64 + '\n    image: redis:7@sha256:' + '2' * 64 + '\n')
        (root / 'private').mkdir(mode=0o700, exist_ok=True)
        runner = candidate.Candidate.__new__(candidate.Candidate)
        runner.source, runner.private, runner.run_id = root / 'Ren2Hub', root / 'private', 'f' * 24
        runner.prefix = 'ren2hub-candidate-local-' + runner.run_id
        runner.containers, runner.image, runner.traffic_interface = {}, 'ren2hub-test:local', interface or ''
        runner.previous_image = None
        runner.simulator_image = 'simulator-image' if simulator_address else None
        runner.report = {'source': {'candidate_sha': 'a' * 40, 'working_tree_sha256': 'b' * 64}}
        runner.verify_build_identities = lambda: None
        started = {}
        labels = {'org.opencontainers.image.revision': 'a' * 40, 'org.opencontainers.image.version': 'a' * 40,
                  'io.ren2hub.source-sha256': 'b' * 64}

        def docker(*argv, **kwargs):
            if argv[:2] == ('image', 'inspect'):
                return SimpleNamespace(stdout=json.dumps([{'Id': argv[2], 'Config': {'Labels': labels}}]).encode())
            self.assertEqual(argv[:2], ('network', 'create'))
            return SimpleNamespace(stdout=b'e' * 64 + b'\n')

        def start(role, image, options=(), command=()):
            started[role] = list(options)
            runner.containers[role] = str(len(started)) * 64

        def owned(role):
            if role == 'simulator':
                return {'NetworkSettings': {'Networks': {runner.network: {'IPAddress': simulator_address}}}}
            options = started[role]
            mounts = []
            for spec in (options[index + 1] for index, value in enumerate(options) if value == '--mount'):
                fields = dict(part.partition('=')[::2] for part in spec.split(','))
                mounts.append({'Type': fields['type'], 'Source': fields['src'], 'Destination': fields['dst'],
                               'RW': 'readonly' not in fields})
            return {'Mounts': mounts}

        runner.docker, runner.start, runner.owned = docker, start, owned
        runner.sdk_runtimes, runner.wait_healthy = lambda: None, lambda role: None
        runner.tls = lambda: root / 'private/server.crt'
        runner.lock_network = lambda: {'addresses': {'simulator': guard_address or simulator_address} if simulator_address else {}}
        runner.inspect = lambda identifier: {'Internal': True, 'Labels': {candidate.LABEL: runner.run_id}}
        def prepare_simulator(runner):
            runner.start('simulator', 'simulator-image')
            candidate.private_write(root / 'private/simulator.json', {})
        def simulator_target(runner):
            return {'container': runner.prefix + '-simulator', 'address': simulator_address,
                    'configuration_path': str(root / 'private/simulator.json'),
                    'configuration_sha256': hashlib.sha256((root / 'private/simulator.json').read_bytes()).hexdigest()}
        with patch('simulator_support.prepare', side_effect=prepare_simulator), \
                patch('simulator_support.target', side_effect=simulator_target), \
                patch('simulator_support.wait_ready'):
            runner.prepare()
        env = dict(line.split('=', 1) for line in (root / 'private/app.env').read_text().splitlines())
        with patch.dict('os.environ', {'REN2HUB_ACCEPTANCE_TARGET': str(root / 'private/target.json')}):
            target = acceptance_target()
        return started['app'], env, target, runner.report['isolation']['traffic']

    def test_simulator_proxy_bypass_uses_only_the_verified_owned_address(self):
        with tempfile.TemporaryDirectory() as directory:
            _, env, target, _ = self.prepared(Path(directory), None, '192.168.176.5')
            self.assertEqual(env['NO_PROXY'], 'localhost,127.0.0.1,postgres,redis,192.168.176.5')
            self.assertEqual(target['simulator']['address'], '192.168.176.5')
            self.assertEqual(env['HTTP_PROXY'], 'http://127.0.0.1:41749')
            self.assertEqual(env['HTTPS_PROXY'], env['HTTP_PROXY'])
        with tempfile.TemporaryDirectory() as directory:
            with self.assertRaisesRegex(candidate.CandidateError, 'candidate_simulator_network_address_changed'):
                self.prepared(Path(directory), None, '192.168.176.5', '192.168.176.6')

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
        for extra, expected, expected_scope in ((['--traffic-host-interface', 'eth0'], 'eth0', None), ([], None, None),
                                                (['--scope', 'auto,billing'], None, ['auto', 'billing'])):
            with self.subTest(expected=expected, scope=expected_scope), tempfile.TemporaryDirectory() as directory, \
                    patch.object(sys, 'argv', ['run.py', 'candidate', '--image', 'ren2hub-test:x', '--simulator-image', 'channel-simulator:x', '--image-identity', 'app-identity.json', '--simulator-identity', 'sim-identity.json', '--output-parent', directory, *extra]), \
                    patch('run.identity', return_value={'source_sha256': 'fixed'}), patch('run.execute', side_effect=execute), \
                    patch('run.signal.signal'), patch('run.os.umask'), contextlib.redirect_stdout(io.StringIO()), \
                    patch('candidate.run', return_value={'success': True}) as runner:
                self.assertEqual(run.main(), 0)
                source, stage, image, interface, previous, simulator, routing_only, app_identity, sim_identity, scope = runner.call_args.args
                self.assertEqual(scope, expected_scope)
                self.assertEqual((source, stage.name, image, interface), (run.SOURCE, 'candidate', 'ren2hub-test:x', expected))
                self.assertIsNone(previous)
                self.assertEqual(simulator, "channel-simulator:x")
                self.assertFalse(routing_only)
                self.assertEqual((app_identity, sim_identity), (Path('app-identity.json'), Path('sim-identity.json')))

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
                runner.active_image_id, runner.active_revision = runner.image_id, runner.revision
                runner.stages = candidate.STAGES
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


class UpgradeCandidateTests(unittest.TestCase):
    def ticket_detail(self):
        # User-facing ticketDTO deliberately omits the database owner ID.
        return {'ticket': {'id': 12, 'title': 'Candidate ticket', 'category': 'api',
                          'priority': 'normal', 'status': 'open', 'reply_count': 1,
                          'message_count': 1, 'last_reply_role': 'user', 'created': 1, 'updated': 1},
                'messages': [{'id': 21, 'role': 'user', 'content': 'Local acceptance request',
                              'images': ['/api/tickets/attachments/31'], 'created': 1}]}

    def attachment_fixture(self, directory):
        from contracts.extensions import PNG
        current = {'revision': 'b' * 40, 'image_id': 'new-image', 'run_id': 'same-run',
                   'attachment_volume': 'owned-volume', 'container_ids': {'app': 'new-app', 'postgres': 'same-db'}}
        previous = {**current, 'revision': 'a' * 40, 'image_id': 'old-image',
                    'container_ids': {**current['container_ids'], 'app': 'old-app'}}
        state = {'format': 1, 'acceptance_target': previous, 'ticket_id': 12,
                 'attachment_path': '/api/tickets/attachments/31', 'sha256': gateway.hashlib.sha256(PNG).hexdigest(),
                 'owner': {'id': 8, 'username': 'old-owner', 'password': 'owner-secret'},
                 'foreign': {'id': 9, 'username': 'old-foreign', 'password': 'foreign-secret'}}
        args = SimpleNamespace(revision=current['revision'], upgrade_attachment_phase='verify',
                               upgrade_attachment_state=Path(directory) / 'attachment.json',
                               previous_target=Path(directory) / 'previous-target.json')
        gateway.atomic_private(args.previous_target, json.dumps(previous))
        gateway.atomic_private(args.upgrade_attachment_state, json.dumps(state))
        return args, current, state

    def test_old_attachment_capture_keeps_credentials_private_and_reports_no_secrets(self):
        from contracts.extensions import PNG
        with tempfile.TemporaryDirectory() as directory:
            experiment = gateway.Experiment.__new__(gateway.Experiment)
            experiment.args = SimpleNamespace(upgrade_attachment_state=Path(directory) / 'attachment.json')
            experiment.user_id, experiment.user_name, experiment.user_password = 8, 'old-owner', 'owner-secret'
            experiment.other_id, experiment.other_name, experiment.other_password = 9, 'old-foreign', 'foreign-secret'
            experiment.checks = []
            experiment.rows = Mock(return_value=[{'id': 12}])
            experiment.user = SimpleNamespace(api=Mock(return_value=self.ticket_detail()),
                                              request=Mock(return_value=(200, PNG)))
            with patch('contracts.gateway.acceptance_target', return_value={'run_id': 'same-run'}):
                experiment.capture_upgrade_attachment()
            path = experiment.args.upgrade_attachment_state
            self.assertEqual(path.stat().st_mode & 0o777, 0o600)
            saved = json.loads(path.read_text())
            self.assertEqual(saved['owner']['password'], 'owner-secret')
            self.assertEqual(saved['foreign']['password'], 'foreign-secret')
            self.assertEqual(saved['sha256'], gateway.hashlib.sha256(PNG).hexdigest())
            self.assertEqual(saved['attachment_path'], '/api/tickets/attachments/31')
            self.assertEqual(experiment.checks, ['old_ticket_attachment_saved_for_upgrade'])
            with self.assertRaisesRegex(RequirementError, 'new_upgrade_attachment_state_required'):
                experiment.capture_upgrade_attachment()

    def test_upgrade_attachment_rejects_missing_foreign_or_external_state_before_login(self):
        for problem, code in (('missing', 'upgrade_attachment_state_required'),
                              ('foreign', 'upgrade_attachment_target_mismatch'),
                              ('wrong_database', 'upgrade_attachment_target_mismatch'),
                              ('external_route', 'upgrade_attachment_record_invalid')):
            with self.subTest(problem=problem), tempfile.TemporaryDirectory() as directory:
                args, current, state = self.attachment_fixture(directory)
                if problem == 'missing':
                    args.upgrade_attachment_state.unlink()
                else:
                    if problem == 'foreign':
                        state['acceptance_target']['run_id'] = 'foreign-run'
                    elif problem == 'wrong_database':
                        state['acceptance_target']['container_ids']['postgres'] = 'foreign-db'
                        gateway.atomic_private(args.previous_target, json.dumps(state['acceptance_target']))
                    else:
                        state['attachment_path'] = 'https://example.com/attachment'
                    gateway.atomic_private(args.upgrade_attachment_state, json.dumps(state))
                with patch('contracts.gateway.verify_candidate_database'), \
                        patch('contracts.gateway.acceptance_target', return_value=current), \
                        patch('contracts.gateway.Client') as client, self.assertRaisesRegex(RequirementError, code):
                    gateway.Experiment(args)
                client.assert_not_called()

    def test_upgrade_reads_original_attachment_and_rejects_changed_content_or_owner_boundary(self):
        from contracts.extensions import PNG
        rejected = (200, json.dumps({'success': False, 'code': 'NOT_FOUND'}).encode())
        for problem, code in ((None, None), ('content', 'upgrade_attachment_content_changed'),
                              ('identity', 'upgrade_attachment_account_identity_changed'),
                              ('foreign', 'upgrade_attachment_cross_user_read')):
            with self.subTest(problem=problem), tempfile.TemporaryDirectory() as directory:
                args, current, _ = self.attachment_fixture(directory)
                with patch('contracts.gateway.acceptance_target', return_value=current):
                    saved = gateway.upgrade_attachment_state(args)
                experiment = gateway.Experiment.__new__(gateway.Experiment)
                experiment.saved_attachment, experiment.base = saved, 'http://127.0.0.1:3000'
                experiment.upgrade_clients, experiment.checks = [], []
                experiment.rows = Mock(return_value=[{'user_id': 8}])
                owner = SimpleNamespace(bearer='old-owner-session', login=Mock(return_value={'id': 18 if problem == 'identity' else 8}),
                    api=Mock(return_value=self.ticket_detail()),
                    request=Mock(return_value=(200, b'changed' if problem == 'content' else PNG)))
                foreign = SimpleNamespace(bearer='old-foreign-session', login=Mock(return_value={'id': 9}),
                    request=Mock(side_effect=[rejected, (200, b'{"success":true}') if problem == 'foreign' else rejected]))
                with patch('contracts.gateway.Client', side_effect=[owner, foreign]):
                    if code:
                        with self.assertRaisesRegex(RequirementError, code):
                            experiment.verify_upgrade_attachment()
                    else:
                        experiment.verify_upgrade_attachment()
                        owner.login.assert_called_once_with('old-owner', 'owner-secret')
                        foreign.login.assert_called_once_with('old-foreign', 'foreign-secret')
                        owner.request.assert_called_once_with('GET', '/api/tickets/attachments/31')
                        self.assertEqual([call.args for call in foreign.request.call_args_list],
                                         [('GET', '/api/tickets/12'), ('GET', '/api/tickets/attachments/31')])
                        self.assertEqual(experiment.checks, ['old_ticket_attachment_content_and_owner_boundary_survive_upgrade'])
                self.assertEqual(experiment.upgrade_clients, [owner, foreign])
                experiment.tokens = []
                experiment.root = experiment.user = experiment.other = SimpleNamespace(bearer='')
                with patch('contracts.gateway.logout_verified') as logout:
                    self.assertEqual(experiment.cleanup(), [])
                self.assertEqual([call.args[0] for call in logout.call_args_list], [owner, foreign])

    def test_upgrade_attachment_rejects_missing_ticket_or_changed_database_owner(self):
        for rows, code in (([], 'upgrade_attachment_ticket_missing'),
                            ([{'user_id': 9}], 'upgrade_attachment_owner_changed')):
            with self.subTest(rows=rows), tempfile.TemporaryDirectory() as directory:
                args, current, _ = self.attachment_fixture(directory)
                with patch('contracts.gateway.acceptance_target', return_value=current):
                    state = gateway.upgrade_attachment_state(args)
                experiment = gateway.Experiment.__new__(gateway.Experiment)
                experiment.saved_attachment = state
                with patch('contracts.gateway.candidate_query', return_value=rows) as query, \
                        patch('contracts.gateway.Client') as client, self.assertRaisesRegex(RequirementError, code):
                    experiment.verify_upgrade_attachment()
                query.assert_called_once_with('SELECT user_id FROM tickets WHERE id=12 AND deleted_at IS NULL')
                client.assert_not_called()

    def test_worker_requires_upgrade_attachment_state_without_affecting_plugin_resume(self):
        for stage, upgrade, phase in (('upgrade-management', True, 'capture'),
                                       ('upstream_sync', True, 'verify'),
                                       ('upstream_sync', False, None),
                                       ('upgrade-resume-p', True, None)):
            with self.subTest(stage=stage, upgrade=upgrade), tempfile.TemporaryDirectory() as directory:
                configuration = {'private': directory, 'output': directory, 'stage': stage, 'revision': 'b' * 40}
                target = {'attachment_volume': 'owned-volume'} if upgrade else {}
                with patch('contracts.worker.sys.stdin', SimpleNamespace(buffer=io.BytesIO(json.dumps(configuration).encode()))), \
                        patch('contracts.worker.configure_target'), \
                        patch('contracts.worker.acceptance_target', return_value=target), \
                        patch('contracts.gateway.run_group', return_value={'success': True}) as group, \
                        patch('contracts.plugin.run_plugin', return_value={'success': True}) as plugin, \
                        self.assertRaises(SystemExit) as exited:
                    worker.execute()
                self.assertEqual(exited.exception.code, 0)
                args = plugin.call_args.args[0] if stage == 'upgrade-resume-p' else group.call_args.args[1]
                self.assertEqual(args.upgrade_attachment_phase, phase)
                self.assertEqual(args.upgrade_attachment_state,
                                 Path(directory) / 'upgrade-attachment.json' if phase else None)
                expected_previous = Path(directory) / 'upgrade-source-target.json' if phase == 'verify' or stage == 'upgrade-resume-p' else None
                self.assertEqual(args.previous_target, expected_previous)
                if stage == 'upgrade-resume-p':
                    self.assertTrue(args.resume_state)
                    self.assertEqual(args.state.name, 'upgrade-plugin-p.json')

    def test_upgrade_accepts_only_its_one_attachment_volume(self):
        target = {'ca_path': CA_MOUNT['Source'], 'traffic_host_interface': '',
                  'attachment_volume': 'ren2hub-candidate-local-test-attachments'}
        volume = {'Type': 'volume', 'Name': target['attachment_volume'],
                  'Destination': '/app/ticket-attachments', 'RW': True}
        require_candidate_mounts('app', {'Mounts': [CA_MOUNT, volume]}, target)
        for mounts in ([CA_MOUNT], [volume, volume], [{**volume, 'Name': 'production'}],
                       [{**volume, 'Destination': '/app'}], [{**volume, 'RW': False}]):
            with self.subTest(mounts=mounts), self.assertRaises(RequirementError):
                require_candidate_mounts('app', {'Mounts': mounts}, target)
        for role in ('postgres', 'redis', 'mock'):
            with self.subTest(role=role), self.assertRaises(RequirementError):
                require_candidate_mounts(role, {'Mounts': [volume]}, target)

    def test_failed_or_missing_preparation_never_stops_old_application(self):
        for missing in candidate.UPGRADE_STAGES[:4]:
            runner = candidate.Candidate.__new__(candidate.Candidate)
            runner.report = {'stages': {stage: {'success': stage != missing}
                                       for stage in candidate.UPGRADE_STAGES[:4]}}
            runner.docker = lambda *args, **kwargs: self.fail('Docker mutated before preparation passed')
            with self.subTest(stage=missing), self.assertRaisesRegex(candidate.CandidateError, 'upgrade_preparation_must_pass'):
                runner.upgrade()

    def test_preparing_state_is_resumed_by_new_application(self):
        observed = []
        with tempfile.TemporaryDirectory() as directory:
            runner = SimpleNamespace(output=Path(directory),
                                     stages=(*candidate.UPGRADE_STAGES, *candidate.STAGES),
                                     report={'success': False, 'stages': {}}, revision='old')
            runner.prepare = lambda: None
            runner.diagnostics = lambda: runner.report.update(
                diagnostics={'success':True,'exit_code':0},
                known_upstream_restored={'success':True,'exit_code':0},
                known_upstream_race={'success':True,'exit_code':0},
                known_upstream_logger_race={'success':True,'exit_code':0},
                ordinary_race={'success':True,'exit_code':0})
            runner.cleanup = lambda: None
            runner.manifest = lambda: None

            def upgrade():
                self.assertEqual([stage for stage, _ in observed], list(candidate.UPGRADE_STAGES[:4]))
                runner.revision = 'new'

            runner.upgrade = upgrade
            def stage(name):
                observed.append((name, runner.revision))
                runner.report['stages'][name] = {'success': True}
            runner.stage = stage
            with patch('candidate.Candidate', return_value=runner):
                report = candidate.run(Path(directory), Path(directory), 'new-image', previous_image='old-image')
            self.assertTrue(report['success'])
            self.assertEqual(dict(observed)['upgrade-prepare-p'], 'old')
            self.assertEqual(dict(observed)['upgrade-resume-p'], 'new')
            self.assertTrue(all(revision == 'new' for stage, revision in observed[4:]))



class ReleaseExemptionManifestTests(unittest.TestCase):
    """完整候选只有在未通过项恰好是登记的上游缺陷时才可发布。"""

    def manifest_for(self, diagnostics, routing, *, exit_code=2, billing_success=True, evidence_error=None, logger_error=None, ordinary_error=None):
        from test_routing_result import ReleaseExemptionTests
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            runner = candidate.Candidate.__new__(candidate.Candidate)
            runner.output = root
            runner.routing_only = False
            runner.stages = candidate.STAGES
            identity = {'git_head': 'b' * 40, 'candidate_sha': 'b' * 40}
            runner.source = root / 'Ren2Hub'
            runner.workspace_identity, runner.workspace_sha = identity, 'b' * 40
            runner.report = {'success': exit_code == 0, 'exit_code': exit_code, 'stages': {},
                             'source': identity, 'image': {'revision': 'a' * 40, 'id': 'sha256:image'},
                             'image_matches_clean_source': True, 'diagnostics': diagnostics,
                             'isolation': {'traffic': {'scope': 'host_interface', 'interface': 'eth0', 'mounts': []}}}
            for stage in candidate.STAGES:
                report = routing if stage == 'routing' else {'success': billing_success, 'group': stage}
                (root / (stage + '.json')).write_text(json.dumps(report))
                runner.report['stages'][stage] = report
            with patch('candidate.execute', return_value=SimpleNamespace(
                    stdout=('160000 commit ' + 'a' * 40 + '\tRen2Hub\n').encode())), \
                    patch('candidate.source_identity', return_value=identity), \
                    patch('native_restored.verify_report', return_value={'verified': True}, side_effect=evidence_error), \
                    patch('native_pricing_race.verify_report', return_value={'verified': True}), \
                    patch('native_logger_race.verify_report', return_value={'verified': True}, side_effect=logger_error), \
                    patch('ordinary_race.verify_report', return_value={'verified': True}, side_effect=ordinary_error):
                runner.manifest()
            return json.loads((root / 'candidate-manifest.json').read_text())

    def test_exact_upstream_set_is_deployable_and_deviations_are_not(self):
        from test_routing_result import ReleaseExemptionTests
        diagnostics, routing = ReleaseExemptionTests().reports()
        approved = self.manifest_for(diagnostics, routing)
        self.assertTrue(approved['deployment_eligible'])
        self.assertTrue(approved['success'])
        self.assertFalse(approved['candidate_success'])
        self.assertIsNotNone(approved['upstream_exemption'])
        failed_billing = self.manifest_for(diagnostics, routing, billing_success=False)
        self.assertFalse(failed_billing['deployment_eligible'])
        from diagnostics import PROBES, summarize
        from routing_result import REQUIRED_CASES, evaluate
        cases = [{'name': probe.name, 'status': 'pass'} for probe in PROBES]
        clean_diagnostics = {'preflight': diagnostics['preflight'], 'cases': cases,
                             **summarize(cases, diagnostics['preflight'])}
        clean_cases = [{'name': name, 'status': 'pass'} for name in REQUIRED_CASES]
        clean_routing = {'cases': clean_cases, 'cleanup_errors': [], **evaluate(clean_cases, REQUIRED_CASES)}
        # 旧诊断全绿而新独立诊断仍有精确已知差异，因此总exit仍为2。
        clean = self.manifest_for(clean_diagnostics, clean_routing, exit_code=2)
        self.assertTrue(clean['deployment_eligible'])
        self.assertFalse(clean['candidate_success'])
        self.assertIsNone(clean['upstream_exemption'])
        incomplete = self.manifest_for({'success': True, 'exit_code': 0, 'cases': []},
                                       {'success': True, 'exit_code': 0, 'cases': []})
        self.assertFalse(incomplete['deployment_eligible'])
        for error in (ValueError('unknown evidence'), IndexError('malformed evidence')):
            invalid = self.manifest_for(clean_diagnostics, clean_routing, evidence_error=error)
            self.assertFalse(invalid['deployment_eligible'])
            invalid_logger = self.manifest_for(clean_diagnostics, clean_routing, logger_error=error)
            self.assertFalse(invalid_logger['deployment_eligible'])
            invalid_ordinary = self.manifest_for(clean_diagnostics, clean_routing, ordinary_error=error)
            self.assertFalse(invalid_ordinary['deployment_eligible'])


if __name__ == '__main__':
    unittest.main()
