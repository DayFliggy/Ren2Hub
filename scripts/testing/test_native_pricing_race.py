"""价格 race 必须复现且仅包含已冻结函数对；未知失败永远阻断。"""
import copy
import hashlib
import json
from pathlib import Path
import tempfile
import unittest

import native_pricing_race as rules


class PricingRaceEvidenceTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name)
        self.log = self.root / 'race.jsonl'
        pricing = self.root / 'model/pricing.go'
        pricing.parent.mkdir()
        lines = ['package model'] + [''] * 82 + ['func GetPricing() []Pricing {',
                 ' if time.Since(lastGetPricingTime) > time.Minute*1 || len(pricingMap) == 0 {',
                 ' }', ' return pricingMap', '}']
        lines += [''] * (100 - len(lines))
        lines += ['func InvalidatePricingCache() {', ' pricingMap = nil', '',
                  ' lastGetPricingTime = time.Time{}', '}',
                  'func updatePricing() {', ' pricingMap = make([]Pricing, 0)',
                  ' pricingMap = append(pricingMap, pricing)', ' lastGetPricingTime = time.Now()', '}']
        pricing.write_text('\n'.join(lines) + '\n')
        fixture = self.root / rules.FIXTURE
        fixture.write_text('//go:build known_upstream_race\n\npackage model\nfunc ' + rules.TEST + '(t *testing.T) {\n' + '\n' * 21 + ' go func() {\n }()\n}\n')
        entry = self.root / 'model/task_cas_test.go'
        entry.write_text('package model\n' + '\n' * 64 + 'func TestMain(m *testing.M) {\n os.Exit(m.Run())\n}\n')

        self.block = ('==================\nWARNING: DATA RACE\n'
                      'Read at 0x001234 by goroutine 17:\n'
                      f'  {rules.PACKAGE}.GetPricing()\n      /src/model/pricing.go:85 +0x4c\n'
                      f'  {rules.PACKAGE}.{rules.TEST}.func1()\n      /src/model/native_pricing_race_test.go:29 +0x86\n\n'
                      'Previous write at 0x001234 by goroutine 13:\n'
                      f'  {rules.PACKAGE}.InvalidatePricingCache()\n      /src/model/pricing.go:104 +0xec\n'
                      f'  {rules.PACKAGE}.{rules.TEST}()\n      /src/model/native_pricing_race_test.go:34 +0x60b\n'
                      '  testing.tRunner()\n      /usr/local/go/src/testing/testing.go:2036 +0x21c\n'
                      '  testing.(*T).Run.gowrap1()\n      /usr/local/go/src/testing/testing.go:2101 +0x38\n\n'
                      'Goroutine 17 (running) created at:\n'
                      f'  {rules.PACKAGE}.{rules.TEST}()\n      /src/model/native_pricing_race_test.go:26 +0x54a\n'
                      '  testing.tRunner()\n      /usr/local/go/src/testing/testing.go:2036 +0x21c\n'
                      '  testing.(*T).Run.gowrap1()\n      /usr/local/go/src/testing/testing.go:2101 +0x38\n\n'
                      'Goroutine 13 (running) created at:\n'
                      '  testing.(*T).Run()\n      /usr/local/go/src/testing/testing.go:2101 +0xb12\n'
                      '  testing.runTests.func1()\n      /usr/local/go/src/testing/testing.go:2585 +0x84\n'
                      '  testing.tRunner()\n      /usr/local/go/src/testing/testing.go:2036 +0x21c\n'
                      '  testing.runTests()\n      /usr/local/go/src/testing/testing.go:2583 +0x9e9\n'
                      '  testing.(*M).Run()\n      /usr/local/go/src/testing/testing.go:2443 +0xf4b\n'
                      f'  {rules.PACKAGE}.TestMain()\n      /src/model/task_cas_test.go:67 +0x170b\n'
                      '  main.main()\n      _testmain.go:522 +0x171\n'
                      '==================\n')
        self.events = [
            {'Action': 'start'}, {'Action': 'run', 'Test': rules.TEST},
            {'Action': 'output', 'Test': rules.TEST, 'Output': '=== RUN   ' + rules.TEST + '\n'},
            {'Action': 'output', 'Test': rules.TEST, 'Output': self.block},
            {'Action': 'output', 'Test': rules.TEST, 'Output': '    testing.go:1712: race detected during execution of test\n'},
            {'Action': 'output', 'Test': rules.TEST, 'Output': '--- FAIL: ' + rules.TEST + ' (0.18s)\n'},
            {'Action': 'fail', 'Test': rules.TEST},
            {'Action': 'output', 'Output': 'FAIL\nFAIL\t' + rules.PACKAGE + '\t0.667s\n'},
            {'Action': 'fail'},
        ]
        for event in self.events:
            event['Package'] = rules.PACKAGE

    def write(self, events=None):
        self.log.write_text('\n'.join(json.dumps(e) for e in (events if events is not None else self.events)) + '\n')

    def test_only_fixed_read_write_pair_is_accepted(self):
        self.write()
        observed = rules.execution(self.log, 1, self.root)
        self.assertEqual(observed['pairs'][0]['read']['function'], rules.PACKAGE + '.GetPricing')
        self.assertEqual(observed['pairs'][0]['write']['function'], rules.PACKAGE + '.InvalidatePricingCache')
        self.assertEqual(observed['race_reports'], 1)

    def test_line_address_offset_and_goroutine_are_not_identity(self):
        self.events[3]['Output'] = self.block.replace('0x001234', '0xabcdef').replace('goroutine 17', 'goroutine 799').replace('Goroutine 17', 'Goroutine 799').replace('+0x4c', '+0x99')
        self.write()
        self.assertEqual(rules.execution(self.log, 1, self.root)['race_reports'], 1)

    def test_other_pair_or_missing_test_frame_is_rejected(self):
        for block in (self.block.replace('.InvalidatePricingCache()', '.updatePricing()'),
                      self.block.replace('.InvalidatePricingCache()', '.RefreshPricing()'),
                      self.block.replace('.GetPricing()', '.GetVendors()'),
                      self.block.replace(rules.TEST, 'TestOther')):
            with self.subTest(block=block):
                events = copy.deepcopy(self.events)
                events[3]['Output'] = block
                self.write(events)
                with self.assertRaises(rules.InvalidEvidence):
                    rules.execution(self.log, 1, self.root)

    def test_no_reproduction_pass_skip_duplicate_and_extra_test_are_rejected(self):
        variants = [self.events[:3] + self.events[4:], self.events + [self.events[-1]],
                    [dict(e, Action='skip') if e.get('Action') == 'fail' else e for e in self.events],
                    [dict(e, Test='TestOther') if e.get('Test') else e for e in self.events]]
        for events in variants:
            with self.subTest(events=events):
                self.write(events)
                with self.assertRaises(rules.InvalidEvidence):
                    rules.execution(self.log, 1, self.root)
        self.write()
        with self.assertRaises(rules.InvalidEvidence):
            rules.execution(self.log, 0, self.root)

    def test_panic_assertion_timeout_build_cached_and_additional_output_are_rejected(self):
        for text in ('panic: broken', 'fatal error: deadlock', 'test timed out after 1m',
                     '[build failed]', 'ok (cached)', '    fixture_test.go:9: unexpected invariant failure',
                     'another unrecognized failure'):
            with self.subTest(text=text):
                events = copy.deepcopy(self.events)
                events.insert(4, {'Action': 'output', 'Test': rules.TEST, 'Package': rules.PACKAGE, 'Output': text + '\n'})
                self.write(events)
                with self.assertRaises(rules.InvalidEvidence):
                    rules.execution(self.log, 1, self.root)

    def test_approved_update_path_and_source_line_are_accepted(self):
        self.events[3]['Output'] = self.block.replace('.InvalidatePricingCache()', '.updatePricing()').replace(
            '/src/model/pricing.go:104 +0xec', '/src/model/pricing.go:107 +0xec\n  ' + rules.PACKAGE + '.RefreshPricing()\n      /src/model/pricing_refresh.go:13 +0xe4')
        self.write()
        self.assertEqual(rules.execution(self.log, 1, self.root)['pairs'][0]['write']['path'],
                         ['updatePricing', 'RefreshPricing', rules.TEST])

    def test_same_function_unapproved_line_and_inner_locked_read_are_rejected(self):
        pricing = self.root / 'model/pricing.go'
        original = pricing.read_text()
        pricing.write_text(original.replace(' lastGetPricingTime = time.Time{}', ' vendorsList = nil'))
        self.write()
        with self.assertRaises(rules.InvalidEvidence):
            rules.execution(self.log, 1, self.root)
        pricing.write_text(original)
        self.events[3]['Output'] = self.block.replace('pricing.go:85 ', 'pricing.go:86 ')
        self.write()
        with self.assertRaises(rules.InvalidEvidence):
            rules.execution(self.log, 1, self.root)

    def test_unknown_nonmodel_creation_function_is_rejected(self):
        self.events[3]['Output'] = self.block.replace('  testing.runTests.func1()', '  arbitrary.evil()')
        self.write()
        with self.assertRaises(rules.InvalidEvidence):
            rules.execution(self.log, 1, self.root)

    def test_refresh_body_and_unobserved_assignment_changes_are_rejected(self):
        report, source = self.manifest_report()
        refresh = self.root / 'model/pricing_refresh.go'
        original = refresh.read_text()
        refresh.write_text(original.replace('updatePricing()', 'otherOperation()'))
        with self.assertRaises(rules.InvalidEvidence):
            rules.verify_report(report, self.root, self.root, source)
        refresh.write_text(original)
        pricing = self.root / 'model/pricing.go'
        pricing.write_text(pricing.read_text().replace('pricingMap = append(pricingMap, pricing)', 'pricingMap = nil'))
        with self.assertRaises(rules.InvalidEvidence):
            rules.verify_report(report, self.root, self.root, source)

    def test_testmain_body_change_is_rejected_even_with_original_exit_line(self):
        report, source = self.manifest_report()
        entry = self.root / 'model/task_cas_test.go'
        entry.write_text(entry.read_text().replace(' os.Exit(m.Run())',
                                                   ' unexpectedSetup()\n os.Exit(m.Run())'))
        self.assertIn('os.Exit(m.Run())', entry.read_text())
        with self.assertRaisesRegex(rules.InvalidEvidence, 'race_test_entry_function_changed'):
            rules.verify_report(report, self.root, self.root, source)

    def test_creation_goroutine_binding_and_source_sites_are_exact(self):
        variants = [self.block.replace('Goroutine 17 (running)', 'Goroutine 13 (running)').replace('Goroutine 13 (running) created at:\n  testing', 'Goroutine 17 (running) created at:\n  testing'),
                    self.block.replace('task_cas_test.go:67', 'task_cas_test.go:66'),
                    self.block.replace('native_pricing_race_test.go:26', 'native_pricing_race_test.go:27')]
        for block in variants:
            with self.subTest(block=block):
                events = copy.deepcopy(self.events)
                events[3]['Output'] = block
                self.write(events)
                with self.assertRaises(rules.InvalidEvidence):
                    rules.execution(self.log, 1, self.root)

    def manifest_report(self):
        self.write()
        fixture = self.root / rules.FIXTURE
        fixture.parent.mkdir(parents=True, exist_ok=True)
        pricing = self.root / 'model/pricing.go'
        refresh = self.root / 'model/pricing_refresh.go'
        refresh.write_text('package model\nfunc RefreshPricing() error {\n updatePricing()\n return nil\n}\n')
        manifest = self.root / rules.MANIFEST
        manifest.parent.mkdir(parents=True)
        data = {'schema_version': 1, 'stage': rules.STAGE, 'upstream': rules.UPSTREAM,
                'baseline': rules.BASELINE, 'image': rules.IMAGE, 'package': './model',
                'test': rules.TEST, 'access_lines': rules.ACCESS_LINES, 'creation_paths': rules.CREATION_PATHS, 'creation_sites': rules.CREATION_SITES, 'fixtures': {rules.FIXTURE: rules.digest(fixture)},
                'local_fixture_functions': {'model/task_cas_test.go:TestMain': hashlib.sha256(rules.function_body(self.root / 'model/task_cas_test.go', 'TestMain').encode()).hexdigest()},
                'native_functions': {n: hashlib.sha256(rules.function_body(refresh if n == 'RefreshPricing' else pricing, n).encode()).hexdigest()
                                     for n in ('GetPricing', 'InvalidatePricingCache', 'RefreshPricing')},
                'preflight': {'baseline_exit': 0, 'native_exit': 1,
                              'baseline_log_sha256': 'a' * 64, 'native_log_sha256': 'b' * 64}}
        manifest.write_text(json.dumps(data))
        image = self.root / 'image.json'
        image.write_text(json.dumps([{'Id': rules.IMAGE}]))
        runtime = {'module_cache': '/tmp/module-cache', 'build_cache': '/tmp/build-cache', 'uid': 1000, 'gid': 1000}
        source = {'candidate_sha': 'c' * 40, 'working_tree_sha256': 'd' * 64}
        return {'schema_version': 1, 'stage': rules.STAGE, 'manifest_sha256': rules.digest(manifest),
                'source_root': str(self.root.resolve()), 'source_before': source, 'source_after': source, 'runtime': runtime,
                'image': rules.IMAGE, 'image_log': image.name, 'image_log_sha256': rules.digest(image),
                'command': rules.argv(self.root, runtime), 'log': self.log.name,
                'log_sha256': rules.digest(self.log), 'returncode': 1, 'timed_out': False, 'error': None,
                'observed': rules.execution(self.log, 1, self.root), 'success': False, 'contracts_success': True,
                'exit_code': 2, 'counts': {rules.STAGE: 1}}, source

    def test_reverification_rejects_forged_source_command_summary_and_log(self):
        report, source = self.manifest_report()
        self.assertEqual(rules.verify_report(report, self.root, self.root, source)['race_reports'], 1)
        for change in ({'success': True}, {'source_after': {}}, {'command': []}, {'returncode': 0}, {'counts': {'fail': 0}}, {'log_sha256': '0' * 64}):
            with self.subTest(change=change), self.assertRaises(rules.InvalidEvidence):
                rules.verify_report({**report, **change}, self.root, self.root, source)

    def test_reverification_rejects_fixture_and_native_body_changes(self):
        report, source = self.manifest_report()
        fixture = self.root / rules.FIXTURE
        original = fixture.read_text()
        fixture.write_text(original + '// changed\n')
        with self.assertRaises(rules.InvalidEvidence):
            rules.verify_report(report, self.root, self.root, source)
        fixture.write_text(original)
        (self.root / 'model/pricing.go').write_text('package model\n')
        with self.assertRaises(rules.InvalidEvidence):
            rules.verify_report(report, self.root, self.root, source)


if __name__ == '__main__':
    unittest.main()
