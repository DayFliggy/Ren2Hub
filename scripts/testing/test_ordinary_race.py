"""普通 race 发布证据必须覆盖固定 124 次执行，且不接受任何竞争。"""
import copy
import json
import shutil
import subprocess
from pathlib import Path
import tempfile
import unittest

import ordinary_race as ordinary
from native_restored import InvalidEvidence, digest
from test_native_logger_race import prepare_fixture as logger_fixture

SOURCE = Path(__file__).resolve().parents[2]


def prepare_fixture(root):
    logger_fixture(root)
    target = root / ordinary.SELECTION
    target.write_bytes((SOURCE / ordinary.SELECTION).read_bytes())
    return ordinary.load_selection(root)


def events(group):
    package = 'github.com/QuantumNous/new-api/' + group['package']
    rows = [{'Action': 'start', 'Package': package}]
    names = list(group['names'])
    if group.get('subtests'):
        names.append(group['names'][0] + '/sqlite')
    for name in names:
        rows += [{'Action': action, 'Package': package, 'Test': name} for action in ('run', 'pass')]
    rows += [{'Action': 'output', 'Package': package, 'Output': 'PASS\n'}, {'Action': 'pass', 'Package': package}]
    return rows


def minimal_report(root, output, source=None):
    """仅构造发布验收器的合成证据，不代表实际测试结果。"""
    _, groups = ordinary.load_selection(root)
    output.mkdir(parents=True, exist_ok=True)
    image = output / 'image.json'
    image.write_text(json.dumps([{'Id': ordinary.IMAGE}]))
    identity = {'fixture': True} if source is None else source
    runtime = {'module_cache': str(root / 'module-cache'), 'build_cache': str(root / 'build-cache'), 'uid': 1000, 'gid': 1000}
    report = {'schema_version': 1, 'stage': ordinary.STAGE, 'selection_sha256': ordinary.FROZEN_SELECTION,
              'source_root': str(root.resolve()), 'source_before': identity, 'source_after': identity, 'runtime': runtime, 'image': ordinary.IMAGE,
              'image_log': image.name, 'image_log_sha256': digest(image), 'suites': [], 'error': None,
              'success': True, 'contracts_success': True, 'exit_code': 0, 'counts': {'pass': 124}}
    for group in groups:
        inventory = output / (group['id'] + '.inventory.log')
        inventory.write_text('\n'.join(group['names']) + '\n')
        log = output / (group['id'] + '.jsonl')
        log.write_text('\n'.join(json.dumps(row) for row in events(group)) + '\n')
        observed, text = ordinary.execution(log, 0, group)
        plain = output / (group['id'] + '.log')
        plain.write_text(text)
        report['suites'].append({'id': group['id'], 'inventory_command': ordinary.argv(root, runtime, group, True),
                                'inventory_returncode': 0, 'inventory_timed_out': False, 'inventory_error': None,
                                'inventory_log': inventory.name, 'inventory_log_sha256': digest(inventory),
                                'command': ordinary.argv(root, runtime, group), 'returncode': 0,
                                'timed_out': False, 'error': None, 'log': log.name, 'log_sha256': digest(log),
                                'plain_log': plain.name, 'plain_log_sha256': digest(plain), 'observed': observed})
    return report


class OrdinaryRaceTests(unittest.TestCase):
    def setUp(self):
        self.temporary = tempfile.TemporaryDirectory()
        self.addCleanup(self.temporary.cleanup)
        self.root = Path(self.temporary.name)
        self.selection, self.groups = prepare_fixture(self.root)
        self.output = self.root / 'out'
        self.output.mkdir()

    def test_complete_report_and_exact_concurrent_commands(self):
        report = minimal_report(self.root, self.output)
        verified = ordinary.verify_report(report, self.output, self.root, {'fixture': True})
        self.assertEqual(verified['test_count'], 124)
        self.assertEqual(len(verified['groups']), 11)
        self.assertEqual(len(self.groups[4]['names']), 10)
        self.assertEqual(sum(len(group['names']) for group in self.groups), 124)
        for group, row in zip(self.groups, report['suites']):
            self.assertIn('-timeout=60s', row['command'])
            self.assertIn('-p=2', row['command'])
            self.assertIn('GOMAXPROCS=2', row['command'])
            self.assertIn(ordinary.IMAGE, row['command'])
            self.assertNotIn('-parallel=1', row['command'])
            self.assertEqual(row['command'][row['command'].index('--network') + 1], 'none')
            self.assertFalse(set(group['names']) & set(self.selection['delegated'].get(group['package'], [])))

    def test_plain_log_preserves_crlf_bytes_exactly(self):
        report = minimal_report(self.root, self.output)
        row = report['suites'][0]
        log = self.output / row['log']
        rows = [json.loads(line) for line in log.read_text().splitlines()]
        rows.insert(-1, {'Action': 'output', 'Package': 'github.com/QuantumNous/new-api/model',
                         'Output': '\r\nGORM test diagnostic\n'})
        log.write_text('\n'.join(json.dumps(event) for event in rows) + '\n')
        row['observed'], plain = ordinary.execution(log, 0, self.groups[0])
        raw = self.output / row['plain_log']
        raw.write_bytes(plain.encode('utf-8'))
        row.update(log_sha256=digest(log), plain_log_sha256=digest(raw))
        ordinary.verify_report(report, self.output, self.root)
        raw.write_bytes(plain.replace('\r\n', '\n').encode('utf-8'))
        row['plain_log_sha256'] = digest(raw)
        with self.assertRaisesRegex(InvalidEvidence, 'ordinary_race_plain_log_mismatch'):
            ordinary.verify_report(report, self.output, self.root)

    def test_execution_root_must_be_present_absolute_and_canonical(self):
        report = minimal_report(self.root, self.output)
        for value in (None, '', '.', str(self.root / 'missing'), str(self.root) + '/.'):
            altered = copy.deepcopy(report)
            altered['source_root'] = value
            with self.subTest(value=value), self.assertRaises(InvalidEvidence):
                ordinary.verify_report(altered, self.output, self.root)
        altered = copy.deepcopy(report)
        altered['source_root'] = str(self.output)
        with self.assertRaises(InvalidEvidence):
            ordinary.verify_report(altered, self.output, self.root, report['source_before'])

    def test_two_real_worktrees_require_matching_original_and_published_identity(self):
        from candidate import source_identity
        from native_restored import verified_race_source_root
        execution, published = self.root / 'execution', self.root / 'published'
        (execution / 'scripts/testing').mkdir(parents=True)
        shutil.copyfile(SOURCE / 'scripts/testing/candidate.py', execution / 'scripts/testing/candidate.py')
        payload = execution / 'payload.txt'
        payload.write_text('source fixture\n')
        def git(*args):
            subprocess.run(['git', *args], check=True, stdout=subprocess.PIPE, stderr=subprocess.PIPE)
        git('init', '-q', str(execution))
        git('-C', str(execution), 'add', '--all')
        git('-C', str(execution), '-c', 'user.name=Test Fixture', '-c', 'user.email=test@example.invalid',
            'commit', '--no-gpg-sign', '-qm', 'isolated source identity fixture')
        git('-C', str(execution), 'worktree', 'add', '--detach', str(published), 'HEAD')
        identity = source_identity(published)
        report = {'source_root': str(execution.resolve()), 'source_before': identity, 'source_after': identity}
        self.assertEqual(verified_race_source_root(report, published, identity), execution)
        self.assertEqual(source_identity(execution), identity)
        payload.write_text('changed source\n')
        with self.assertRaisesRegex(InvalidEvidence, 'race_execution_tree_drifted'):
            verified_race_source_root(report, published, identity)
        payload.write_text('source fixture\n')
        bad = {**report, 'source_root': str(self.output)}
        with self.assertRaises(InvalidEvidence):
            verified_race_source_root(bad, published, identity)
        self.assertEqual(source_identity(execution), source_identity(published))

    def test_selection_is_frozen_even_if_counts_are_unchanged(self):
        path = self.root / ordinary.SELECTION
        path.write_text(path.read_text().replace('TestRedeemConcurrentSingleSuccess', 'TestOtherConcurrentSingleSuccess'))
        with self.assertRaises(InvalidEvidence):
            ordinary.load_selection(self.root)

    def test_missing_group_command_source_and_summary_are_rejected(self):
        report = minimal_report(self.root, self.output)
        mutations = [lambda r: r['suites'].pop(),
                     lambda r: r['suites'][0]['command'].append('-parallel=1'),
                     lambda r: r['suites'][0]['inventory_command'].append('-run=^$'),
                     lambda r: r['suites'][0].__setitem__('timed_out', True),
                     lambda r: r['suites'][0].__setitem__('returncode', 1),
                     lambda r: r.__setitem__('selection_sha256', '0' * 64),
                     lambda r: r.__setitem__('source_after', {}),
                     lambda r: r.__setitem__('counts', {'pass': 128}),
                     lambda r: r.__setitem__('exit_code', 2)]
        for mutation in mutations:
            altered = copy.deepcopy(report)
            mutation(altered)
            with self.subTest(mutation=mutation), self.assertRaises(InvalidEvidence):
                ordinary.verify_report(altered, self.output, self.root)
        with self.assertRaises(InvalidEvidence):
            ordinary.verify_report(report, self.output, self.root, {'other': True})

    def test_inventory_and_raw_hashes_are_verified(self):
        for name in ('log', 'plain_log', 'inventory_log'):
            report = minimal_report(self.root, self.output)
            path = self.output / report['suites'][0][name]
            path.write_text('')
            with self.subTest(name=name), self.assertRaises(InvalidEvidence):
                ordinary.verify_report(report, self.output, self.root)
        report = minimal_report(self.root, self.output)
        path = self.output / report['suites'][0]['inventory_log']
        path.write_text('TestMadeUp\n')
        report['suites'][0]['inventory_log_sha256'] = digest(path)
        with self.assertRaises(InvalidEvidence):
            ordinary.verify_report(report, self.output, self.root)

    def test_unknown_race_skip_assertion_and_empty_or_missing_nodes_block(self):
        group = self.groups[0]
        package = 'github.com/QuantumNous/new-api/' + group['package']
        changes = [lambda rows: rows.clear(),
                   lambda rows: rows.pop(1),
                   lambda rows: rows.append({'Action': 'output', 'Package': package, 'Output': 'WARNING: DATA RACE\n'}),
                   lambda rows: rows.append({'Action': 'fail', 'Package': package, 'Test': group['names'][0]}),
                   lambda rows: rows.append({'Action': 'skip', 'Package': package, 'Test': group['names'][0]}),
                   lambda rows: rows.append({'Action': 'output', 'Package': package, 'Output': 'panic: failure\n'}),
                   lambda rows: rows.append({'Action': 'output', 'Package': package, 'Output': '(cached)\n'}),
                   lambda rows: rows.append({'Action': 'run', 'Package': package, 'Test': 'TestUnexpected'})]
        for mutation in changes:
            rows = events(group)
            mutation(rows)
            path = self.output / 'changed.jsonl'
            path.write_text('\n'.join(json.dumps(row) for row in rows))
            with self.subTest(mutation=mutation), self.assertRaises(InvalidEvidence):
                ordinary.execution(path, 0, group)


if __name__ == '__main__':
    unittest.main()
