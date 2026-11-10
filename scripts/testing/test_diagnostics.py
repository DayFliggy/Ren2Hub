"""诊断执行器的失败优先级、证据约束和原始产物合同。"""
import contextlib
import io
import json
import os
from pathlib import Path
import re
import sys
import tempfile
import unittest
from unittest.mock import patch

import diagnostics as d


def probe(kind):
    return next(p for p in d.PROBES if p.fingerprint == kind)


def equal_output(spec, expected, actual, test=None):
    name = test or spec.test
    return (f'    {spec.file}:50: \n'
            f'        Error Trace:\t/fixture/{spec.package}/{spec.file}:50\n'
            f'        Error:      \tNot equal:\n'
            f'                    \texpected: {expected}\n'
            f'                    \tactual  : {actual}\n'
            f'        Test:       \t{name}\n')


def execution(spec, text='', *, passed=False):
    nodes, leaves = d.test_nodes(spec)
    package = d.MODULE + spec.package
    events = [{'Action': 'start', 'Package': package}]
    for name in sorted(nodes, key=lambda n: (n.count('/'), n)):
        events.append({'Action': 'run', 'Package': package, 'Test': name})
    for name in leaves:
        events.append({'Action': 'output', 'Package': package, 'Test': name,
                       'Output': text.get(name, '') if isinstance(text, dict) else text})
    for name in sorted(nodes, key=lambda n: (-n.count('/'), n)):
        events.append({'Action': 'pass' if passed else 'fail', 'Package': package, 'Test': name})
    events.append({'Action': 'pass' if passed else 'fail', 'Package': package})
    return events


class ClassificationTests(unittest.TestCase):
    def setUp(self):
        temporary = tempfile.TemporaryDirectory()
        self.addCleanup(temporary.cleanup)
        self.root = Path(temporary.name)
        self.log = self.root / 'raw.jsonl'

    def classify(self, spec, events, code=1, source='', **kwargs):
        self.log.write_text('\n'.join(map(json.dumps, events)) + '\n')
        return d.classify(spec, d.CommandResult(code, self.log, **kwargs), source)

    def test_n1_requires_concrete_values_and_rejects_additional_failures(self):
        spec = probe('N1')
        correct = equal_output(spec, '"video_vendor"', '"task_public"')
        result = self.classify(spec, execution(spec, correct))
        self.assertEqual(result['status'], 'known_upstream')
        for value in (
            equal_output(spec, '"video_vendor"', '"unrelated_id"'),
            'known_upstream=N1\n',
            correct + equal_output(spec, '1', '0'),
            correct + f'    {spec.file}:99: quota unexpectedly changed\n',
        ):
            with self.subTest(value=value):
                self.assertEqual(self.classify(spec, execution(spec, value))['status'], 'fail')

    def test_arbitrary_500_never_matches_the_native_panic(self):
        spec = probe('N2')
        for text in (d.N2_MARKER, equal_output(spec, '200', '500')):
            self.assertEqual(self.classify(spec, execution(spec, text))['status'], 'fail')

    def test_n2_requires_safe_panic_and_exact_source_frame(self):
        spec = probe('N2')
        source = 'package relay\nfunc ResolveOriginTask() {\nif originTask.ChannelId != info.ChannelId {\n}\n'
        text = (f'    {spec.file}:40: upstream_panic type=runtime.errorString '
                'message="runtime error: invalid memory address or nil pointer dereference" '
                'function=relay.ResolveOriginTask frame=/fixture/relay/relay_task.go:3\n'
                f'    {spec.file}:41: {d.N2_MARKER}\n')
        self.assertEqual(self.classify(spec, execution(spec, text), source=source)['status'], 'known_upstream')
        for changed in (text.replace('go:3', 'go:4'), text.replace('relay.ResolveOriginTask', 'relay.OtherFunction'),
                        text.replace('runtime.errorString', 'other.Error'),
                        text + equal_output(spec, '1', '2')):
            self.assertEqual(self.classify(spec, execution(spec, changed), source=source)['status'], 'fail')

    def test_upstream_pass_requires_review(self):
        spec = probe('N1')
        self.assertEqual(self.classify(spec, execution(spec, passed=True), code=0)['status'], 'fail')

    def test_skips_timeouts_missing_or_duplicate_results_are_failures(self):
        spec = probe('N1')
        events = execution(spec, equal_output(spec, '"video_vendor"', '"task_public"'))
        variants = []
        for index, event in enumerate(events):
            if event.get('Test') == spec.test and event['Action'] == 'fail':
                skipped = [dict(item) for item in events]
                skipped[index]['Action'] = 'skip'
                variants.extend([skipped, events[:index] + events[index+1:], events + [event]])
        variants.extend([events[:-1], events[1:]])
        for variant in variants:
            self.assertEqual(self.classify(spec, variant)['status'], 'fail')
        for options in ({'timed_out': True}, {'error': 'OSError'}):
            self.assertEqual(self.classify(spec, events, **options)['status'], 'fail')

    def test_compile_errors_and_malformed_events_are_not_upstream_evidence(self):
        spec = probe('N1')
        valid = execution(spec, equal_output(spec, '"video_vendor"', '"task_public"'))
        self.log.write_text('\n'.join(map(json.dumps, valid)) + '\n# package\nundefined: symbol\n')
        self.assertEqual(d.classify(spec, d.CommandResult(1, self.log))['status'], 'fail')
        for event in ({'Action': 'build-fail', 'ImportPath': d.MODULE + spec.package},
                      {'Action': 'skip', 'Package': d.MODULE + spec.package},
                      {'Action': 'output', 'Package': d.MODULE + spec.package, 'Output': 'panic: test timed out after 1m0s\n'},
                      {'Action': 'output', 'Package': d.MODULE + spec.package, 'Output': 'WARNING: DATA RACE\n'},
                      {'Action': 'output', 'Package': 'different', 'Output': ''},
                      {'Action': 'fail', 'Package': d.MODULE + spec.package, 'Test': 'TestUpstreamSyncStripeRetriesTransientWalletFailure'}):
            self.assertEqual(self.classify(spec, valid + [event])['status'], 'fail')

    def test_subtest_coverage_cannot_shrink_or_hide_parent_failure(self):
        spec = probe('N2')
        spec = d.Probe(spec.package, spec.test, spec.file, 'N1', ('one', 'two'))
        _, leaves = d.test_nodes(spec)
        logs = {name: equal_output(spec, '"video_vendor"', '"task_public"', name) for name in leaves}
        events = execution(spec, logs)
        self.assertEqual(self.classify(spec, events)['status'], 'known_upstream')
        self.assertEqual(self.classify(spec, [e for e in events if e.get('Test') != spec.test + '/two'])['status'], 'fail')
        extra = {'Action': 'output', 'Package': d.MODULE + spec.package, 'Test': spec.test,
                 'Output': f'    {spec.file}:99: unrelated parent failure\n'}
        self.assertEqual(self.classify(spec, events[:-1] + [extra] + events[-1:])['status'], 'fail')

    def test_billing_fingerprint_requires_both_attempts_and_exact_quota(self):
        spec = probe('A7')
        one = (equal_output(spec, 'map[string]float64{"seconds":5, "size":1.5}', 'map[string]float64{"seconds":5}')
               + equal_output(spec, '375000', '250000'))
        self.assertEqual(self.classify(spec, execution(spec, one * 2))['status'], 'known_upstream')
        for output in (one, (one * 2).replace('250000', '0')):
            self.assertEqual(self.classify(spec, execution(spec, output))['status'], 'fail')

    def test_slashes_in_go_subtest_names_do_not_create_parent_events(self):
        spec = probe('mj_priority')
        nodes, leaves = d.test_nodes(spec)
        self.assertEqual(nodes - leaves, {spec.test})
        logs = {}
        for leaf in leaves:
            status, code, message = {
                'empty_manual_list': ('503', 'model_not_found', 'No available channel for model mj_upscale (distributor) (request id: )'),
                'all_candidates_full': ('429', 'channel_concurrency_exceeded', 'concurrency limit reached (request id: )'),
                'model_not_allowed': ('403', '', 'This token has no access to model mj_upscale (request id: )'),
            }[leaf.rsplit('/', 1)[-1]]
            expected = 'map[string]interface {}{"code":4, "description":"task_not_found ", "type":"upstream_error"}'
            actual = ('map[string]interface {}{"error":map[string]interface {}{"code":' + json.dumps(code)
                      + ', "message":' + json.dumps(message) + ', "type":"new_api_error"}}')
            logs[leaf] = equal_output(spec, '400', status, leaf) + equal_output(spec, expected, actual, leaf)
        self.assertEqual(self.classify(spec, execution(spec, logs))['status'], 'known_upstream')
        removed = [e for e in execution(spec, logs) if e.get('Test') != next(iter(leaves))]
        self.assertEqual(self.classify(spec, removed)['status'], 'fail')

    def test_local_failure_always_beats_known_and_blocked(self):
        spec = probe('N1')
        local = self.classify(spec, execution(spec, 'known_upstream=N2\n'))
        self.assertEqual(local['status'], 'fail')
        known = {'name': 'n2', 'status': 'known_upstream', 'upstream': 'new-api', 'diagnosis': 'nil pointer'}
        blocked = {'name': 'later', 'status': 'blocked', 'blocked_by': 'n2'}
        self.assertEqual(d.summarize([known, blocked])['exit_code'], 2)
        self.assertEqual(d.summarize([known, blocked, local])['exit_code'], 1)
        self.assertEqual(d.summarize([local, blocked, known])['exit_code'], 1)
        self.assertEqual(d.summarize([{'name': 'local', 'status': 'pass'}])['exit_code'], 0)
        self.assertEqual(d.summarize([blocked])['exit_code'], 1)
        self.assertEqual(d.summarize([known, {'name': 'missing', 'status': 'not_run'}])['exit_code'], 1)


class ExecutionTests(unittest.TestCase):
    def test_exact_whitelist_excludes_payments_and_rejects_suffix_matches(self):
        payment_names = ('TestUpstreamSyncStripeRetriesTransientWalletFailure',
                         'TestUpstreamSyncWaffoAcknowledgesOnlyPermanentFailures',
                         'TestUpstreamSyncPancakeAcknowledgesPermanentVerifiedFailure')
        for spec in d.PROBES:
            argv = d.test_command(spec)
            expression = argv[argv.index('-run') + 1]
            self.assertEqual(expression, '^' + spec.test + '$')
            self.assertIn('-timeout=60s', argv)
            self.assertIn('-count=1', argv)
            self.assertTrue(re.search(expression, spec.test))
            for name in payment_names + (spec.test + 'Extra',):
                self.assertFalse(re.search(expression, name))

    def test_environment_rejects_credentials_and_user_go_overrides(self):
        secrets = {'SQL_DSN': 'production', 'REDIS_CONN_STRING': 'private', 'OPENAI_API_KEY': 'private',
                   'TEST_MYSQL_DSN': 'private', 'HTTP_PROXY': 'private', 'SESSION_SECRET': 'private',
                   'GOFLAGS': '-overlay=unsafe', 'GOWORK': '/unsafe', 'GOMAXPROCS': '99'}
        with tempfile.TemporaryDirectory() as directory, patch.dict(os.environ, secrets):
            actual = d.environment(Path(directory), secrets)
        for key in ('SQL_DSN', 'REDIS_CONN_STRING', 'OPENAI_API_KEY', 'TEST_MYSQL_DSN', 'HTTP_PROXY', 'SESSION_SECRET'):
            self.assertNotIn(key, actual)
        self.assertEqual(actual['GOFLAGS'], '-mod=readonly')
        self.assertEqual(actual['GOWORK'], 'off')
        self.assertEqual(actual['GOMAXPROCS'], '2')

    def test_command_keeps_raw_nonzero_and_kills_timed_out_child(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            output = io.StringIO()
            with contextlib.redirect_stdout(output):
                result = d.command([sys.executable, '-c', 'print("fixture output"); raise SystemExit(1)'],
                                   root, root / 'one.log', os.environ.copy(), 5)
            self.assertEqual(result.returncode, 1)
            self.assertEqual(result.log.read_text(), 'fixture output\n')
            self.assertEqual(output.getvalue(), '')
            result = d.command([sys.executable, '-c', 'import time; time.sleep(30)'],
                               root, root / 'timeout.log', os.environ.copy(), 0.05)
            self.assertTrue(result.timed_out)
            self.assertNotEqual(result.returncode, 0)

    def run_fixture(self, root, specs, vet_fail=False):
        commands = []

        def execute(argv, cwd, path, env, timeout):
            commands.append(argv)
            if argv[1] == 'version':
                path.write_text('go version go1.26.10 linux/amd64\n')
                return d.CommandResult(0, path)
            if argv[1] == 'vet':
                path.write_text('compile error fixture\n' if vet_fail else '')
                return d.CommandResult(1 if vet_fail else 0, path)
            spec = next(s for s in specs if argv[argv.index('-run') + 1] == '^' + s.test + '$')
            text = equal_output(spec, '"video_vendor"', '"task_public"') if spec.fingerprint == 'N1' else 'local error\n'
            path.write_text('\n'.join(map(json.dumps, execution(spec, text))) + '\n')
            return d.CommandResult(1, path)

        with patch.object(d, 'PROBES', specs), patch.object(d, 'command', side_effect=execute):
            report = d.run(root, root / 'evidence', {})
        return report, commands

    def test_run_vets_before_exact_tests_and_preserves_report_and_raw_hashes(self):
        with tempfile.TemporaryDirectory() as directory:
            report, commands = self.run_fixture(Path(directory), (probe('N1'), probe('plugin_storage')))
            self.assertEqual([argv[1] for argv in commands], ['version', 'vet', 'vet', 'test', 'test'])
            self.assertEqual(report['exit_code'], 1)
            path = Path(report['report_path'])
            self.assertEqual(d.sha256(path), report['report_sha256'])
            self.assertEqual(path.with_name('report.sha256').read_text(), report['report_sha256'] + '  report.json\n')
            for relative, digest in report['artifacts'].items():
                self.assertEqual(d.sha256(path.parent / relative), digest)
            with self.assertRaisesRegex(ValueError, 'already contains evidence'):
                d.run(Path(directory), path.parent)

    def test_vet_compile_failure_marks_probe_not_run_and_overall_fail(self):
        with tempfile.TemporaryDirectory() as directory:
            report, commands = self.run_fixture(Path(directory), (probe('N1'),), vet_fail=True)
            self.assertEqual([argv[1] for argv in commands], ['version', 'vet'])
            self.assertEqual(report['exit_code'], 1)
            self.assertEqual(report['preflight'][1]['status'], 'fail')
            self.assertEqual(report['cases'][0]['status'], 'not_run')


if __name__ == '__main__':
    unittest.main()
