"""保护诊断验收边界：未知失败、缺失、skip、假汇总和日志篡改必须阻断。"""
import copy
import json
from pathlib import Path
import tempfile
import unittest

import native_restored as rules


class RestoredEvidenceTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name)
        self.log = self.root / 'probe.jsonl'
        self.package, self.test = 'example.test/core', 'TestInvariant'
        self.events = [
            {'Action': 'start'},
            {'Action': 'run', 'Test': self.test},
            {'Action': 'output', 'Test': self.test, 'Output': '    invariant_test.go:12: invariant violated\n'},
            {'Action': 'fail', 'Test': self.test},
            {'Action': 'fail'},
        ]
        for event in self.events:
            event['Package'] = self.package

    def write(self, events=None):
        self.log.write_text('\n'.join(json.dumps(e) for e in (self.events if events is None else events)) + '\n')

    def parse(self):
        return rules.execution(self.log, self.package, self.test, 1)

    def test_known_failure_retains_exact_message(self):
        self.write()
        self.assertEqual(self.parse(), {'nodes': {self.test: 'fail'}, 'assertions': {},
                                       'messages': {self.test: ['invariant violated']}})

    def test_unknown_additional_assertion_changes_fingerprint(self):
        self.write()
        expected = self.parse()
        self.events.insert(3, {'Action': 'output', 'Package': self.package, 'Test': self.test,
                               'Output': '    invariant_test.go:13: another failure\n'})
        self.write()
        self.assertNotEqual(self.parse(), expected)

    def test_missing_duplicate_skip_and_unexpected_nodes_rejected(self):
        variants = [self.events[:-1], self.events + [self.events[-1]],
                    [dict(e, Action='skip') if e.get('Action') == 'fail' else e for e in self.events],
                    [dict(e, Test='TestUnexpected') if e.get('Test') else e for e in self.events]]
        for variant in variants:
            with self.subTest(events=variant):
                self.write(variant)
                with self.assertRaises(rules.InvalidEvidence):
                    self.parse()

    def test_compile_timeout_panic_race_and_cached_output_rejected(self):
        for text in ('[build failed]', 'test timed out after 1m0s', 'panic: test',
                     'WARNING: DATA RACE', 'ok example (cached)'):
            with self.subTest(text=text):
                events = copy.deepcopy(self.events)
                events[2]['Output'] = text
                self.write(events)
                with self.assertRaises(rules.InvalidEvidence):
                    self.parse()

    def test_testify_fingerprint_excludes_location_but_keeps_exact_difference(self):
        self.events[2]['Output'] = ('    invariant_test.go:12:\n'
                                   '        Error Trace: /tmp/invariant_test.go:12\n'
                                   '        Error: Not equal:\n'
                                   '               expected: 1\n'
                                   '               actual  : 2\n'
                                   '        Test: TestInvariant\n')
        self.write()
        self.assertEqual(self.parse()['assertions'], {self.test: ['Not equal: expected: 1 actual : 2']})
        self.assertEqual(self.parse()['messages'], {})

    def fixture_report(self):
        self.write()
        (self.root / 'go.mod').write_text('module example.test/core\n')
        fixture = self.root / 'probe_test.go'
        fixture.write_text('// synthetic fixture\n')
        manifest = self.root / rules.MANIFEST
        manifest.parent.mkdir(parents=True)
        source = {'candidate_sha': 'a' * 40, 'working_tree_sha256': 'b' * 64}
        case = {'id': 'invariant', 'test': self.test, 'module': '.', 'package': '.',
                'fixtures': {'probe_test.go': rules.digest(fixture)}, 'expected': self.parse(),
                'preflight': {'baseline_sha': rules.BASELINE, 'upstream_sha': rules.UPSTREAM,
                              'baseline_log_sha256': 'c' * 64, 'upstream_log_sha256': 'd' * 64}}
        manifest.write_text(json.dumps({'schema_version': 1, 'stage': rules.STAGE,
                                       'baseline': rules.BASELINE, 'upstream': rules.UPSTREAM, 'cases': [case]}))
        compile_log = self.root / 'compile.log'
        compile_log.write_text('')
        built = {'command': rules.compile_command(case, self.root), 'log': compile_log.name,
                 'log_sha256': rules.digest(compile_log), 'returncode': 0, 'timed_out': False,
                 'error': None, 'sha256': 'e' * 64}
        row = {'id': 'invariant', 'status': rules.STAGE, 'timed_out': False, 'error': None,
               'command': rules.invocation(case, self.root, self.package), 'binary_sha256': built['sha256'],
               'log': self.log.name, 'log_sha256': rules.digest(self.log), 'returncode': 1, 'observed': self.parse()}
        report = {'schema_version': 1, 'stage': rules.STAGE, 'manifest_sha256': rules.digest(manifest),
                  'source_before': source, 'source_after': source, 'cases': [row], 'success': False,
                  'binaries': {rules.binary_key(case): built},
                  'contracts_success': True, 'exit_code': 2, 'counts': {rules.STAGE: 1}}
        return report, source

    def test_generic_assertion_keeps_each_credential_marker(self):
        self.events[2]['Output'] = ('    invariant_test.go:12:\n'
                                   '        Error Trace: /tmp/invariant_test.go:12\n'
                                   '        Error: Should be false\n'
                                   '        Test: TestInvariant\n'
                                   '        Messages: credential marker: signature\n')
        self.write()
        expected = self.parse()
        self.assertEqual(expected['messages'], {self.test: ['testify: credential marker: signature']})
        self.events[2]['Output'] = self.events[2]['Output'].replace('marker: signature', 'marker: body')
        self.write()
        self.assertNotEqual(self.parse(), expected)

    def test_reverification_rejects_forged_summary_source_or_missing_case(self):
        report, source = self.fixture_report()
        self.assertEqual(rules.verify_report(report, self.root, self.root, source)['cases'], ['invariant'])
        for change in ({'success': True}, {'cases': []}, {'source_after': {}}, {'counts': {rules.STAGE: 2}}):
            with self.subTest(change=change), self.assertRaises(rules.InvalidEvidence):
                rules.verify_report({**report, **change}, self.root, self.root, source)

    def test_reverification_rejects_modified_log_and_fixture(self):
        report, source = self.fixture_report()
        self.log.write_text(self.log.read_text().replace('invariant violated', 'another failure'))
        with self.assertRaises(rules.InvalidEvidence):
            rules.verify_report(report, self.root, self.root, source)
        self.write()
        (self.root / 'probe_test.go').write_text('// changed\n')
        with self.assertRaises(rules.InvalidEvidence):
            rules.verify_report(report, self.root, self.root, source)

    def test_reverification_rejects_changed_or_missing_command(self):
        report, source = self.fixture_report()
        for command in (None, ['go', 'test', '-run', 'Other'],
                        [arg.replace('-test.count=1', '-test.count=0') for arg in report['cases'][0]['command']]):
            changed = copy.deepcopy(report)
            changed['cases'][0]['command'] = command
            with self.subTest(command=command), self.assertRaisesRegex(rules.InvalidEvidence, 'command_mismatch'):
                rules.verify_report(changed, self.root, self.root, source)

    def test_reverification_binds_each_probe_to_its_compiled_binary(self):
        report, source = self.fixture_report()
        built = report['binaries']['.:.']
        changes = [('binaries', {}), ('returncode', 1), ('sha256', None),
                   ('command', built['command'][:-1] + ['./other']), ('binary_sha256', 'f' * 64)]
        for field, value in changes:
            changed = copy.deepcopy(report)
            target = changed['cases'][0] if field == 'binary_sha256' else changed['binaries']['.:.']
            if field == 'binaries':
                changed['binaries'] = value
            else:
                target[field] = value
            with self.subTest(field=field), self.assertRaisesRegex(rules.InvalidEvidence, 'binary_mismatch'):
                rules.verify_report(changed, self.root, self.root, source)

    def test_run_compiles_each_package_once_and_runs_each_probe_separately(self):
        calls = []
        def command(argv, cwd, log, env, timeout):
            calls.append((argv, cwd, timeout))
            if argv[:3] == ['go', 'test', '-c']:
                Path(argv[argv.index('-o') + 1]).write_bytes(b'binary')
            log.write_text('')
            return type('Result', (), {'returncode': 0, 'timed_out': False, 'error': None})()
        cases = [{'id': name, 'module': '.', 'package': 'pkg', 'test': name} for name in ('TestA', 'TestB')]
        output = self.root / 'out'
        output.mkdir()
        binaries = rules.compile_binaries({'cases': cases}, self.root, output, {}, command)
        self.assertEqual(len(calls), 1)
        self.assertRegex(binaries['.:pkg']['sha256'], '^[a-f0-9]{64}$')
        commands = [rules.invocation(case, output, 'example.test/core/pkg') for case in cases]
        self.assertEqual(commands[0][-1], '-test.run=^TestA$')
        self.assertIn('-test.timeout=60s', commands[1])
        self.assertEqual(commands[0][6], commands[1][6])


if __name__ == '__main__':
    unittest.main()
