"""保护统一入口的证据判定与环境隔离，避免退出 0 冒充完整测试。"""
import json
from pathlib import Path
import tempfile
import unittest
from unittest.mock import patch

from run import environment, go_result


class RunnerContractTests(unittest.TestCase):
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
