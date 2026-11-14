"""logger 独立验收拒绝未知竞争、普通失败以及被改写的执行证据。"""
import copy
import json
import os
from pathlib import Path
import shutil
import tempfile
import unittest

import native_logger_race as logger
from native_restored import InvalidEvidence, digest

SOURCE = Path(__file__).resolve().parents[2]


def prepare_fixture(root):
    manifest, _ = logger.load_manifest(SOURCE)
    files = {logger.MANIFEST, 'logger/logger.go', *manifest['local_fixture_files']}
    for key in (*manifest['fixture_functions'], *manifest['local_fixture_functions'], *manifest['current_functions']):
        files.add(key.split(':')[0])
    for name in files:
        target = root / name
        target.parent.mkdir(parents=True, exist_ok=True)
        shutil.copyfile(SOURCE / name, target)
    cache = root / 'module-cache'
    for name in manifest['external_files']:
        target = cache / name
        target.parent.mkdir(parents=True, exist_ok=True)
        module_cache = Path(os.environ.get('GOMODCACHE', str(Path(os.environ.get('GOPATH', str(Path.home() / 'go'))) / 'pkg/mod')))
        shutil.copyfile(module_cache / name, target)
    return manifest


def location(root, spec):
    if spec['path'].startswith('/src/'):
        source = (root / spec['path'][5:]).read_text().splitlines()
        function = spec['function'].removeprefix(logger.PREFIX).rsplit('/', 1)[-1].split('.', 1)[1].split('.func', 1)[0]
        start = next(i for i, line in enumerate(source) if line.startswith('func ' + function + '('))
        line = next(i + 1 for i in range(start, len(source)) if source[i].strip() == spec['source_line'])
    else:
        line = spec.get('line', 200)
    return f"  {spec['function']}()\n      {spec['path']}:{line} +0x1\n"


def known_block(root, manifest, group):
    paths = manifest['paths']
    stacks = paths[group['id'] + '_access'][:1] * 2
    if group['id'] == 'stream':
        creations = [paths['stream_creation'][stack[2]['function']] for stack in stacks]
    else:
        creations = [paths['task_creation'][0]] * 2
    first = {'function': logger.LOGGER, 'path': '/src/logger/logger.go', 'source_line': logger.COUNTER_INCREMENT}
    output = 'WARNING: DATA RACE\n'
    for kind, gid, path in zip(('Read', 'Previous write'), ('7', '8'), stacks):
        output += f'{kind} at 0x00001234 by goroutine {gid}:\n' + ''.join(location(root, item) for item in [first, *path]) + '\n'
    for gid, path in zip(('7', '8'), creations):
        output += f'Goroutine {gid} (running) created at:\n' + ''.join(location(root, item) for item in path) + '\n'
    return output


def synthetic_events(root, manifest, group, with_race=False):
    package, events = logger.PREFIX + group['package'], []
    def event(action, name=None, output=None):
        item = {'Action': action, 'Package': package}
        if name is not None:
            item['Test'] = name
        if output is not None:
            item['Output'] = output
        events.append(item)
    event('start')
    failed = group['names'][0] if with_race else None
    for name in group['nodes']:
        event('run', name)
        event('output', name, '=== RUN   ' + name + '\n')
        if group['id'] == 'stream' and name in group['names']:
            event('pause', name)
            event('cont', name)
        if name == failed:
            event('output', name, '==================\n' + known_block(root, manifest, group) + '==================\n')
            event('output', name, '    testing.go:1712: race detected during execution of test\n')
        action = 'fail' if name == failed else 'pass'
        event('output', name, f'--- {action.upper()}: {name} (0.01s)\n')
        event(action, name)
    event('output', output='FAIL\n' if with_race else 'PASS\n')
    event('output', output=('FAIL' if with_race else 'ok  ') + '\t' + package + '\t0.100s\n')
    event('fail' if with_race else 'pass')
    return events


def minimal_report(root, output, with_race=False):
    """供发布校验器的隔离单元夹具使用；这不代表真实测试证据。"""
    manifest, checksum = logger.load_manifest(root)
    output.mkdir(parents=True, exist_ok=True)
    image = output / 'image.json'
    image.write_text(json.dumps([{'Id': logger.IMAGE}]))
    runtime = {'module_cache': str(root / 'module-cache'), 'build_cache': str(root / 'build-cache'), 'uid': 1000, 'gid': 1000}
    report = {'schema_version': 1, 'stage': logger.STAGE, 'manifest_sha256': checksum, 'image': logger.IMAGE,
              'source_root': str(root.resolve()), 'source_before': {'fixture': True}, 'source_after': {'fixture': True}, 'runtime': runtime,
              'image_log': image.name, 'image_log_sha256': digest(image), 'suites': [], 'error': None,
              'success': not with_race, 'contracts_success': True, 'exit_code': 2 if with_race else 0,
              'counts': {logger.STAGE: 19} if with_race else {'pass': 19}}
    for group in manifest['groups']:
        path = output / (group['id'] + '.jsonl')
        path.write_text('\n'.join(json.dumps(row) for row in synthetic_events(root, manifest, group, with_race)) + '\n')
        observed, text = logger.execution(path, int(with_race), root, manifest, group)
        plain = output / (group['id'] + '.log')
        plain.write_text(text)
        report['suites'].append({'id': group['id'], 'command': logger.argv(root, runtime, group),
                                'returncode': int(with_race), 'timed_out': False, 'error': None,
                                'log': path.name, 'log_sha256': digest(path), 'plain_log': plain.name,
                                'plain_log_sha256': digest(plain), 'observed': observed})
    return report


class LoggerRaceTests(unittest.TestCase):
    def setUp(self):
        self.temporary = tempfile.TemporaryDirectory()
        self.addCleanup(self.temporary.cleanup)
        self.root = Path(self.temporary.name)
        self.manifest = prepare_fixture(self.root)
        self.output = self.root / 'out'
        self.output.mkdir()

    def test_clean_and_known_race_have_distinct_results(self):
        for with_race in (False, True):
            report = minimal_report(self.root, self.output, with_race)
            result = logger.verify_report(report, self.output, self.root, {'fixture': True})
            self.assertEqual(result['race_reports'], 2 if with_race else 0)
            self.assertEqual(result['clean'], not with_race)
            self.assertEqual(len(result['cases']), 19)

    def test_frozen_manifest_and_source_hashes_are_required(self):
        for name in (logger.MANIFEST, 'logger/logger.go', 'service/task_polling_test.go', 'relay/helper/stream_scanner.go'):
            path = self.root / name
            original = path.read_bytes()
            with self.subTest(name=name):
                path.write_bytes(original.replace(b'logger.LogInfo(c, common.LogText("stream ended:', b'logger.LogWarn(c, common.LogText("stream ended:', 1) if name == 'relay/helper/stream_scanner.go' else original + b'\n// changed\n')
                with self.assertRaises(InvalidEvidence):
                    logger.load_manifest(self.root)
                path.write_bytes(original)

    def test_body_change_with_original_line_still_present_is_rejected(self):
        path = self.root / 'service/task_billing_test.go'
        path.write_text(path.read_text().replace('func TestMain(m *testing.M) {', 'func TestMain(m *testing.M) {\n\tprintln("changed")'))
        with self.assertRaises(InvalidEvidence):
            logger.load_manifest(self.root)

    def test_command_and_report_identity_are_not_trusted(self):
        report = minimal_report(self.root, self.output, True)
        mutations = [lambda r: r['suites'][0]['command'].append('-parallel=1'),
                     lambda r: r['suites'][0]['command'].__setitem__(r['suites'][0]['command'].index('none'), 'host'),
                     lambda r: r.__setitem__('manifest_sha256', '0' * 64),
                     lambda r: r.__setitem__('source_after', {}),
                     lambda r: r.__setitem__('success', True),
                     lambda r: r['suites'][0].__setitem__('timed_out', True),
                     lambda r: r['suites'][0].__setitem__('returncode', 0),
                     lambda r: r['suites'].pop()]
        for mutation in mutations:
            altered = copy.deepcopy(report)
            mutation(altered)
            with self.subTest(mutation=mutation), self.assertRaises(InvalidEvidence):
                logger.verify_report(altered, self.output, self.root)
        with self.assertRaises(InvalidEvidence):
            logger.verify_report(report, self.output, self.root, {'different': True})

    def test_access_line_stack_and_goroutine_binding_are_exact(self):
        group = self.manifest['groups'][0]
        block = known_block(self.root, self.manifest, group)
        changes = [lambda b: b.replace('logHelper()', 'LogWarn()', 1),
                   lambda b: b.replace('logger.go:114 ', 'logger.go:116 ', 1),
                   lambda b: b.replace('Previous write', 'Previous read'),
                   lambda b: b.replace('Goroutine 8 ', 'Goroutine 9 '),
                   lambda b: b.replace('Goroutine 8 ', 'Goroutine 7 '),
                   lambda b: b.replace('testing.runTests.func1()', 'unknown.creator()'),
                   lambda b: b.replace('testing.tRunner()', 'unknown.access()', 1),
                   lambda b: b.replace('testing/testing.go:2036 ', 'testing/testing.go:2037 ', 1),
                   lambda b: b.replace('Previous write at 0x00001234', 'Previous write at 0x00005678'),
                   lambda b: b + '  unknown.tail()\n      /tmp/x.go:1 +0x1\n']
        logger.race_block(block, self.root, self.manifest, group)
        for mutation in changes:
            with self.subTest(mutation=mutation), self.assertRaises(InvalidEvidence):
                logger.race_block(mutation(block), self.root, self.manifest, group)

    def test_nested_done_creation_requires_observed_leading_run_frame(self):
        manifest = copy.deepcopy(self.manifest)
        function = logger.PREFIX + 'relay/helper.TestStreamScannerHandler_DoneStopsScanner.func1'
        selected = next(path for path in manifest['paths']['stream_access'] if path[2]['function'] == function)
        manifest['paths']['stream_access'].insert(0, selected)
        group = self.manifest['groups'][0]
        block = known_block(self.root, manifest, group)
        logger.race_block(block, self.root, self.manifest, group)
        first = location(self.root, self.manifest['paths']['stream_creation'][function][0])
        with self.assertRaisesRegex(InvalidEvidence, 'unknown_logger_creation_stack'):
            logger.race_block(block.replace(first, '', 1), self.root, self.manifest, group)

    def test_task_goroutines_must_share_the_same_test_creator(self):
        group = self.manifest['groups'][1]
        block = known_block(self.root, self.manifest, group)
        prefix = block.split('Goroutine 8 (running) created at:', 1)[0]
        mixed = prefix + 'Goroutine 8 (running) created at:\n' + ''.join(
            location(self.root, frame) for frame in self.manifest['paths']['task_creation'][2]) + '\n'
        with self.assertRaisesRegex(InvalidEvidence, 'logger_task_creation_tests_differ'):
            logger.race_block(mixed, self.root, self.manifest, group)
        same = block.split('Goroutine 7 (running) created at:', 1)[0]
        for gid in ('7', '8'):
            same += f'Goroutine {gid} (running) created at:\n' + ''.join(
                location(self.root, frame) for frame in self.manifest['paths']['task_creation'][1]) + '\n'
        logger.race_block(same, self.root, self.manifest, group)

    def test_threshold_read_is_allowed_but_threshold_write_is_not(self):
        group = self.manifest['groups'][0]
        block = known_block(self.root, self.manifest, group)
        logger.race_block(block.replace('logger.go:114 ', 'logger.go:115 ', 1), self.root, self.manifest, group)
        with self.assertRaises(InvalidEvidence):
            logger.race_block(block.replace('logger.go:114 ', 'logger.go:115 '), self.root, self.manifest, group)

    def test_test_nodes_skips_and_normal_assertions_cannot_be_hidden(self):
        group = self.manifest['groups'][0]
        base = synthetic_events(self.root, self.manifest, group, True)
        mutations = [lambda rows: rows.clear(),
                     lambda rows: rows.pop(1),
                     lambda rows: rows.insert(1, {'Action': 'skip', 'Package': logger.PREFIX + group['package'], 'Test': group['names'][0]}),
                     lambda rows: rows.insert(1, {'Action': 'output', 'Package': logger.PREFIX + group['package'], 'Test': group['names'][0], 'Output': '    test.go:10: wrong amount\n'}),
                     lambda rows: rows.insert(1, {'Action': 'output', 'Package': logger.PREFIX + group['package'], 'Output': 'panic: hidden\n'}),
                     lambda rows: rows.insert(1, {'Action': 'output', 'Package': logger.PREFIX + group['package'], 'Output': '(cached)\n'}),
                     lambda rows: rows.__setitem__(3, {'Action': 'output', 'Package': logger.PREFIX + group['package'], 'Output': 'PASS\n'})]
        for mutation in mutations:
            rows = copy.deepcopy(base)
            mutation(rows)
            path = self.output / 'altered.jsonl'
            path.write_text('\n'.join(json.dumps(row) for row in rows))
            with self.subTest(mutation=mutation), self.assertRaises((InvalidEvidence, ValueError)):
                logger.execution(path, 1, self.root, self.manifest, group)

    def test_original_module_source_and_raw_log_are_verified(self):
        report = minimal_report(self.root, self.output, True)
        row = report['suites'][0]
        (self.output / row['log']).write_text('')
        with self.assertRaises(InvalidEvidence):
            logger.verify_report(report, self.output, self.root)
        report = minimal_report(self.root, self.output, True)
        path = self.root / 'module-cache' / next(iter(self.manifest['external_files']))
        path.write_text(path.read_text() + '\n//changed\n')
        with self.assertRaises(InvalidEvidence):
            logger.verify_report(report, self.output, self.root)


if __name__ == '__main__':
    unittest.main()
