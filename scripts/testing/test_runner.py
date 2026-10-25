"""保护统一入口的证据判定与环境隔离，避免退出 0 冒充完整测试。"""
import errno
import json
from pathlib import Path
import socket
import tempfile
import unittest
from unittest.mock import patch

from run import environment, go_result, playwright_cases, playwright_result, visual_baseline, ui_contracts


class RunnerContractTests(unittest.TestCase):
    def test_ui_runs_keep_origin_stable_after_recent_server_shutdown(self):
        with tempfile.TemporaryDirectory() as directory, socket.socket() as available:
            root = Path(directory)
            (root / 'reference.png').write_bytes(b'reviewed reference')
            manifest = root / 'frontend/tests/contracts/suite-manifest.json'
            manifest.parent.mkdir(parents=True)
            manifest.write_text(json.dumps({'files': ['one.spec.ts']}))
            case = {'id': 'one', 'file': 'one.spec.ts', 'tests': [{
                'expectedStatus': 'passed', 'status': 'expected',
                'results': [{'status': 'passed', 'retry': 0}]}]}
            report = {'suites': [{'specs': [case]}], 'stats': {'expected': 1}}
            available.bind(('127.0.0.1', 0))
            port = available.getsockname()[1]
            available.close()
            observed = []

            def execute(command, cwd, output, name, env, timeout):
                observed.append(env['REN2HUB_UI_PORT'])
                if '--list' not in command:
                    (output / 'playwright.json').write_text(json.dumps(report))
                return json.dumps(report)

            with patch.dict('os.environ', {'REN2HUB_UI_BASELINE': str(root)}, clear=True), \
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

    def test_occupied_visual_port_fails_without_random_port_fallback(self):
        with tempfile.TemporaryDirectory() as directory, socket.socket() as occupied:
            root = Path(directory)
            (root / 'reference.png').write_bytes(b'reviewed reference')
            occupied.setsockopt(socket.SOL_SOCKET, socket.SO_REUSEADDR, 1)
            occupied.bind(('127.0.0.1', 0))
            occupied.listen(1)
            with patch.dict('os.environ', {'REN2HUB_UI_BASELINE': str(root)}, clear=True), \
                    patch('run.UI_CONTRACTS_PORT', occupied.getsockname()[1]), \
                    patch('run.execute') as execute:
                with self.assertRaises(OSError) as failure:
                    ui_contracts(root, root, {})
                self.assertEqual(failure.exception.errno, errno.EADDRINUSE)
                execute.assert_not_called()

    def test_unified_ui_cannot_silently_omit_visual_comparison(self):
        with tempfile.TemporaryDirectory() as directory, patch.dict('os.environ', {}, clear=True), \
                patch('run.execute') as execute:
            with self.assertRaisesRegex(RuntimeError, 'REN2HUB_UI_BASELINE'):
                ui_contracts(Path(directory), Path(directory), {})
            execute.assert_not_called()

    def test_visual_references_reject_empty_and_detect_replacement_or_removal(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            with self.assertRaises(RuntimeError):
                visual_baseline(root)
            reference = root / 'page.png'
            reference.write_bytes(b'first reference')
            original = visual_baseline(root)
            reference.write_bytes(b'replaced reference')
            self.assertNotEqual(visual_baseline(root)['sha256'], original['sha256'])
            reference.unlink()
            with self.assertRaises(RuntimeError):
                visual_baseline(root)

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


if __name__ == '__main__':
    unittest.main()
