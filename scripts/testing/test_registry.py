"""保护登记检查：它要在数秒内指出哪个冻结文件或登记项偏离，而不是等候选运行到诊断阶段才失败。"""
import contextlib
import hashlib
import io
import json
from pathlib import Path
import shutil
import subprocess
import sys
import tempfile
import unittest
from unittest.mock import patch

import registry

SOURCE = Path(__file__).resolve().parents[2]


def sha(text):
    return hashlib.sha256(text.encode()).hexdigest()


class RegistryTests(unittest.TestCase):
    def test_restored_fixture_drift_names_the_file(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            (root / 'scripts/testing').mkdir(parents=True)
            (root / 'service').mkdir()
            (root / 'service/kept_test.go').write_text('package service\n')
            (root / 'service/renamed_test.go').write_text('package service // type renamed\n')
            cases = [{'fixtures': {'service/kept_test.go': sha('package service\n'),
                                   'service/renamed_test.go': sha('package service\n')}},
                     {'fixtures': {'service/renamed_test.go': sha('package service\n'),
                                   'service/deleted_test.go': sha('x')}}]
            (root / 'scripts/testing/known_upstream_restored.json').write_text(json.dumps({'cases': cases}))
            self.assertEqual(registry.restored_fixtures(root), [
                'service/deleted_test.go：文件缺失', 'service/renamed_test.go：内容已改变'])

    def test_race_selection_reports_tests_that_no_longer_exist(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            (root / 'model').mkdir()
            (root / 'model/cache_test.go').write_text(
                'package model\n\nfunc TestKept(t *testing.T) {\n}\n\nfunc  TestSpaced (\n\tt *testing.T) {\n}\n')
            selection = {'groups': [{'id': 'cache', 'package': 'model',
                                     'names': ['TestKept', 'TestSpaced', 'TestRenamed']}]}
            with patch('ordinary_race.load_selection', return_value=(selection, [])):
                self.assertEqual(registry.race_selection(root), ['model/TestRenamed：测试已不存在（组 cache）'])

    def test_unregistered_database_tests_are_listed(self):
        inventory = {'unclassified': [{'package': 'model', 'test': 'TestNewDatabases'}],
                     'missing_required': ['TestGone'], 'missing_restored_diagnostics': []}
        with patch('database.database_inventory', return_value=inventory), \
                patch('database.database_test_files', return_value=[]):
            self.assertEqual(registry.database_registration(SOURCE), [
                'model/TestNewDatabases：读取数据库 DSN 但未登记', 'TestGone：已登记但测试不存在'])

    def test_source_delta_is_a_commit_time_item_during_iteration(self):
        checks = (('restored_fixtures', lambda root: [], 'fix-a'), ('source_delta', lambda root: ['model/a.go'], 'fix-b'))
        with patch.object(registry, 'CHECKS', checks), contextlib.redirect_stdout(io.StringIO()) as output:
            iterating = registry.run(SOURCE, iteration=True)
            committing = registry.run(SOURCE)
        self.assertTrue(iterating['success'])
        self.assertEqual(iterating['pending_before_commit'], ['source_delta'])
        self.assertFalse(committing['success'])
        self.assertTrue(committing['checks']['source_delta']['blocking'])
        self.assertIn('model/a.go', output.getvalue())

    def test_a_blocking_check_fails_in_both_modes(self):
        checks = (('race_selection', lambda root: ['model/TestGone'], 'fix'),)
        with patch.object(registry, 'CHECKS', checks), contextlib.redirect_stdout(io.StringIO()):
            self.assertFalse(registry.run(SOURCE, iteration=True)['success'])
            self.assertFalse(registry.run(SOURCE)['success'])


class SourceDeltaCheckTests(unittest.TestCase):
    """刷新脚本的 --check-local：不读取上游，只回答工作树是否偏离清单。"""

    def check(self, root):
        result = subprocess.run([sys.executable, '-B', 'scripts/refresh-core-source-delta.py', '--check-local'],
                                cwd=root, capture_output=True, text=True)
        return result.returncode, result.stderr.split()

    def test_reports_changed_deleted_and_unlisted_files_without_writing(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            (root / 'scripts').mkdir()
            (root / 'docs/upstream').mkdir(parents=True)
            (root / 'model').mkdir()
            shutil.copy(SOURCE / registry.DELTA, root / registry.DELTA)
            (root / 'model/kept.go').write_text('package model\n')
            (root / 'model/changed.go').write_text('package model\n')
            rows = [{'path': 'model/kept.go', 'status': 'extension', 'local_sha256': sha('package model\n')},
                    {'path': 'model/changed.go', 'status': 'extension', 'local_sha256': sha('package model\n')},
                    {'path': 'model/removed.go', 'status': 'removed', 'local_sha256': None}]
            manifest = root / 'docs/upstream/CORE_SOURCE_DELTA.json'
            manifest.write_text(json.dumps({'files': rows}))
            subprocess.run(['git', 'init', '-q'], cwd=root, check=True)
            before = manifest.read_bytes()
            self.assertEqual(self.check(root), (0, []))
            (root / 'model/changed.go').write_text('package model // edited\n')
            (root / 'model/new_test.go').write_text('package model\n')
            (root / 'model/kept.go').unlink()
            self.assertEqual(self.check(root), (1, ['model/changed.go', 'model/kept.go', 'model/new_test.go']))
            self.assertEqual(manifest.read_bytes(), before)
            self.assertEqual(registry.source_delta(root), ['model/changed.go', 'model/kept.go', 'model/new_test.go'])


if __name__ == '__main__':
    unittest.main()
