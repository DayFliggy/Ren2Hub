"""单独验收固定上游 logger 计数竞争；业务失败和其他竞争仍阻断。"""
from __future__ import annotations

import argparse
from collections import Counter
import hashlib
import json
import os
from pathlib import Path
import re
import subprocess
import time

from native_restored import InvalidEvidence, digest, regular_path, require, verified_race_source_root

STAGE = 'known_upstream_logger_race'
MANIFEST = 'scripts/testing/known_upstream_logger_race.json'
FROZEN_MANIFEST = 'aa94d4439e30325d7fb404cb40f182b7e065c0da16fe6ab7ae29f6f4f6092669'
IMAGE = 'sha256:0d494ea9b5863337de0dfb4f938062bf0261a919016eb162d2be26381dbadafe'
DOCKER = ['sudo', '-n', 'docker', '--host', 'unix:///var/run/docker.sock']
PREFIX = 'github.com/QuantumNous/new-api/'
LOGGER = PREFIX + 'logger.logHelper'
COUNTER_INCREMENT = "logCount++ // we don't need accurate count, so no lock here"
COUNTER_THRESHOLD = 'if logCount > maxLogCount && !setupLogWorking {'
RACE_PATTERN = r'^==================\n(WARNING: DATA RACE\n.*?)^==================\n'


def function_body(path, name):
    matches = re.findall(r'^func ' + re.escape(name) + r'\([^\n]*\{.*?^}', path.read_text(), re.M | re.S)
    require(len(matches) == 1, 'logger_function_missing_or_ambiguous')
    return matches[0]


def load_manifest(root):
    path = regular_path(root, MANIFEST)
    require(digest(path) == FROZEN_MANIFEST, 'logger_frozen_manifest_changed')
    data = json.loads(path.read_text())
    require(digest(regular_path(root, 'logger/logger.go')) == data['logger_sha256'], 'logger_source_changed')
    for inventory in ('fixture_functions', 'local_fixture_functions', 'current_functions'):
        for key, expected in data[inventory].items():
            name, function = key.split(':')
            actual = hashlib.sha256(function_body(regular_path(root, name), function).encode()).hexdigest()
            require(actual == expected, 'logger_test_or_function_changed')
    for name, expected in data['local_fixture_files'].items():
        require(digest(regular_path(root, name)) == expected, 'logger_fixture_file_changed')
    return data, FROZEN_MANIFEST


def frames(text):
    lines = text.strip('\n').splitlines()
    require(lines and len(lines) % 2 == 0, 'invalid_logger_stack')
    result = []
    for offset in range(0, len(lines), 2):
        function = re.fullmatch(r'  (\S+)\(\)', lines[offset])
        location = re.fullmatch(r'\s+(\S+\.go):([0-9]+)(?: \+0x[0-9a-f]+)?', lines[offset + 1])
        require(function is not None and location is not None, 'invalid_logger_frame')
        result.append({'function': function[1], 'path': location[1], 'line': int(location[2])})
    return result


def matches(actual, expected, root):
    if len(actual) != len(expected):
        return False
    for got, want in zip(actual, expected):
        if got['function'] != want['function'] or got['path'] != want['path']:
            return False
        if want.get('line') is not None and got['line'] != want['line']:
            return False
        if got['path'].startswith('/src/'):
            source = regular_path(root, got['path'][5:]).read_text().splitlines()
            if not 0 < got['line'] <= len(source) or source[got['line'] - 1].strip() != want['source_line']:
                return False
            # A matching line in another function is not a matching call site.
            declaration = next((line for line in reversed(source[:got['line']]) if line.startswith('func ')), '')
            function = want['function'].removeprefix(PREFIX).rsplit('/', 1)[-1].split('.', 1)[1].split('.func', 1)[0]
            if not declaration.startswith('func ' + function + '('):
                return False
    return True


def race_block(block, root, manifest, group):
    require(block.startswith('WARNING: DATA RACE\n'), 'invalid_logger_race_block')
    accesses = list(re.finditer(r'^(?:Previous )?(Read|Write|read|write) at (0x[0-9a-f]+) by goroutine ([0-9]+):\n', block, re.M))
    require(len(accesses) == 2 and accesses[0][2] == accesses[1][2]
            and accesses[0][3] != accesses[1][3]
            and any(item[1].lower() == 'write' for item in accesses), 'invalid_logger_access_pair')
    bindings, observed = {}, []
    for index, access in enumerate(accesses):
        end = accesses[index + 1].start() if index == 0 else len(block)
        stack = frames(block[access.end():end].split('\nGoroutine ', 1)[0])
        require(stack[0]['function'] == LOGGER and stack[0]['path'] == '/src/logger/logger.go', 'unknown_logger_race')
        source = regular_path(root, 'logger/logger.go').read_text().splitlines()
        line = stack[0]['line']
        require(0 < line <= len(source), 'invalid_logger_access_line')
        text = source[line - 1].strip()
        require(text == COUNTER_INCREMENT or (text == COUNTER_THRESHOLD and access[1].lower() == 'read'),
                'unknown_logger_counter_access')
        paths = manifest['paths'][group['id'] + '_access']
        selected = [path for path in paths if matches(stack[1:], path, root)]
        require(len(selected) == 1, 'unknown_logger_access_stack')
        if group['id'] == 'stream':
            test_function = stack[3]['function']
            bindings[access[3]] = [manifest['paths']['stream_creation'][test_function]]
        else:
            bindings[access[3]] = manifest['paths']['task_creation']
        observed.append({'kind': access[1].lower(), 'source_line': text,
                         'stack': selected[0]})
    creations = list(re.finditer(r'^Goroutine ([0-9]+) \((?:running|finished)\) created at:\n', block, re.M))
    require(len(creations) == 2 and {item[1] for item in creations} == set(bindings),
            'missing_or_unknown_logger_creation_goroutine')
    creation_paths = []
    for index, created in enumerate(creations):
        end = creations[index + 1].start() if index == 0 else len(block)
        stack = frames(block[created.end():end])
        selected = [path for path in bindings[created[1]] if matches(stack, path, root)]
        require(len(selected) == 1, 'unknown_logger_creation_stack')
        creation_paths.append(selected[0])
    if group['id'] == 'task':
        creators = [next(frame['function'].split('.func', 1)[0] for frame in path
                         if frame['function'].startswith(PREFIX + 'service.Test')) for path in creation_paths]
        require(len(set(creators)) == 1, 'logger_task_creation_tests_differ')
    return {'accesses': observed, 'creations': creation_paths}


def allowed_output(line, group):
    package = PREFIX + group['package']
    names = '|'.join(re.escape(name) for name in group['nodes'])
    common = [r'=== (?:RUN   |PAUSE |CONT  )(' + names + ')',
              r'\s*--- (?:PASS|FAIL): (' + names + r') \([0-9.]+s\)',
              r'\s+testing\.go:[0-9]+: race detected during execution of test',
              r'(?:PASS|FAIL)', r'(?:ok  |FAIL)\t' + re.escape(package) + r'\t[0-9.]+s']
    timestamp = r'\[(?:INFO|ERR)\] [0-9]{4}/[0-9]{2}/[0-9]{2} - [0-9:]{8} \| SYSTEM \| '
    if group['id'] == 'stream':
        common.append(timestamp + r'stream ended: reason=(?:done|eof|handler_stop)(?: end_error="(?:fatal|stop at 10|fatal at 50)")?(?: soft_errors=[0-9]+)?(?:, received=[0-9]+)? ')
    else:
        common.append(timestamp + r'Channel #(?:201|202|251|252|301|302) (?:pending video tasks: [12]|failed to update video async tasks: context deadline exceeded) ')
    return any(re.fullmatch(pattern, line) for pattern in common)


def execution(log, returncode, root, manifest, group):
    require(returncode in (0, 1), 'logger_runtime_exit')
    starts, ends, pauses, continues, reasons = Counter(), {}, Counter(), Counter(), Counter()
    package_starts, package_ends, output = 0, [], []
    for line in log.read_text().splitlines():
        require(bool(line.strip()), 'empty_logger_log_line')
        event = json.loads(line)
        require(isinstance(event, dict) and event.get('Package') == PREFIX + group['package'],
                'unexpected_logger_package_or_build_failure')
        action, name, text = event.get('Action'), event.get('Test'), event.get('Output', '')
        require(action in ('start', 'run', 'output', 'pause', 'cont', 'pass', 'fail'), 'unexpected_logger_action_or_skip')
        require(isinstance(text, str) and not re.search(r'(?m)^(?:panic:|fatal error:)|test timed out|\[build failed\]|\(cached\)', text),
                'logger_runtime_or_build_failure')
        if name:
            require(name in group['nodes'], 'unexpected_logger_test')
            if action == 'run':
                starts[name] += 1
            if action in ('pass', 'fail'):
                ends.setdefault(name, []).append(action)
            if action == 'pause':
                pauses[name] += 1
            if action == 'cont':
                continues[name] += 1
            if re.fullmatch(r'\s+testing\.go:[0-9]+: race detected during execution of test\n', text):
                reasons[name] += 1
        else:
            package_starts += action == 'start'
            if action in ('pass', 'fail'):
                package_ends.append(action)
        if action == 'output':
            output.append(text)
    expected = Counter({name: 1 for name in group['nodes']})
    require(starts == expected and set(ends) == set(expected)
            and all(len(actions) == 1 for actions in ends.values()) and package_starts == 1,
            'missing_or_duplicate_logger_test')
    parallel = Counter({name: 1 for name in group['names']}) if group['id'] == 'stream' else Counter()
    require(pauses == parallel and continues == parallel, 'logger_test_parallelism_changed')
    plain = ''.join(output)
    blocks = re.findall(RACE_PATTERN, plain, re.M | re.S)
    require(len(blocks) == plain.count('WARNING: DATA RACE'), 'unparsed_logger_race')
    races = [race_block(block, root, manifest, group) for block in blocks]
    remaining = re.sub(RACE_PATTERN, '', plain, flags=re.M | re.S)
    require(all(allowed_output(line, group) for line in remaining.splitlines()), 'unexpected_logger_output_or_assertion')
    failures = [name for name, actions in ends.items() if actions == ['fail']]
    require(all(reasons[name] <= 1 for name in reasons), 'duplicate_logger_failure_reason')
    require(all(any(reasons[child] for child in ends if child == name or child.startswith(name + '/')) for name in failures),
            'non_race_logger_test_failure')
    require(all(ends[name] == ['fail'] for name in reasons), 'inconsistent_logger_failure_reason')
    if races:
        require(returncode == 1 and package_ends == ['fail'] and failures and reasons, 'logger_race_exit_mismatch')
    else:
        require(returncode == 0 and package_ends == ['pass'] and not failures and not reasons, 'logger_clean_exit_mismatch')
    return {'nodes': {name: actions[0] for name, actions in sorted(ends.items())}, 'race_reports': len(races),
            'races': races, 'business_assertions_failed': False}, plain


def argv(root, runtime, group):
    for key in ('module_cache', 'build_cache'):
        value = runtime.get(key)
        require(isinstance(value, str) and Path(value).is_absolute() and not re.search(r'[:,\n]', value), 'invalid_logger_cache_path')
    require(isinstance(runtime.get('uid'), int) and runtime['uid'] >= 0
            and isinstance(runtime.get('gid'), int) and runtime['gid'] >= 0, 'invalid_logger_uid')
    require(not re.search(r'[:,\n]', str(root)), 'invalid_logger_source_path')
    command = DOCKER + ['run', '--rm', '--pull=never', '--network', 'none', '--user', f"{runtime['uid']}:{runtime['gid']}",
                       '--cpus', '2', '-v', str(root) + ':/src:ro', '-v', runtime['module_cache'] + ':/go/pkg/mod:ro',
                       '-v', runtime['build_cache'] + ':/cache', '-w', '/src']
    for value in ('GOCACHE=/cache', 'GOWORK=off', 'GOTOOLCHAIN=local', 'GOENV=off', 'GOMAXPROCS=2',
                  'CGO_ENABLED=1', 'GOPROXY=off', 'GOSUMDB=off', 'GOFLAGS=-mod=readonly', 'CI=1', 'LOG_SQL_DSN='):
        command += ['-e', value]
    return command + [IMAGE, 'go', 'test', '-buildvcs=false', '-race', '-json', '-count=1', '-timeout=60s',
                      '-p=2', './' + group['package'], '-run', '^(' + '|'.join(group['names']) + ')$']


def verify_report(report, output, root, expected_source=None):
    execution_root = verified_race_source_root(report, root, expected_source)
    manifest, checksum = load_manifest(root)
    require(report.get('schema_version') == 1 and report.get('stage') == STAGE
            and report.get('manifest_sha256') == checksum, 'logger_manifest_mismatch')
    require(isinstance(report.get('source_before'), dict) and report['source_before'] == report.get('source_after'), 'logger_source_changed')
    if expected_source is not None:
        require(report['source_before'] == expected_source, 'logger_source_identity_mismatch')
    require(report.get('image') == IMAGE and report.get('error') is None, 'logger_execution_failed')
    runtime = report.get('runtime', {})
    image = regular_path(output, report.get('image_log'))
    require(digest(image) == report.get('image_log_sha256') and json.loads(image.read_text())[0]['Id'] == IMAGE,
            'logger_image_identity_mismatch')
    for name, expected in manifest['external_files'].items():
        require(digest(regular_path(Path(runtime['module_cache']), name)) == expected, 'logger_module_source_changed')
    suites = report.get('suites')
    require(isinstance(suites, list) and len(suites) == len(manifest['groups']), 'logger_suite_inventory_mismatch')
    races = 0
    for row, group in zip(suites, manifest['groups']):
        require(row.get('id') == group['id'] and row.get('command') == argv(execution_root, runtime, group)
                and row.get('timed_out') is False and row.get('error') is None, 'logger_command_changed_or_failed')
        log = regular_path(output, row.get('log'))
        require(digest(log) == row.get('log_sha256'), 'logger_log_hash_mismatch')
        observed, plain = execution(log, row.get('returncode'), root, manifest, group)
        raw = regular_path(output, row.get('plain_log'))
        require(digest(raw) == row.get('plain_log_sha256') and raw.read_bytes() == plain.encode('utf-8'), 'logger_plain_log_mismatch')
        require(row.get('observed') == observed, 'logger_observed_mismatch')
        races += observed['race_reports']
    require(report.get('success') is (races == 0) and report.get('contracts_success') is True
            and report.get('exit_code') == (2 if races else 0)
            and report.get('counts') == ({STAGE: 19} if races else {'pass': 19}), 'logger_summary_mismatch')
    return {'stage': STAGE, 'manifest_sha256': checksum, 'cases': [name for group in manifest['groups'] for name in group['names']],
            'race_reports': races, 'clean': races == 0}


def run(root, output, supplied_env=None):
    from candidate import source_identity
    from diagnostics import command, environment
    root, output = root.resolve(), output.resolve()
    output.mkdir(parents=True, mode=0o700, exist_ok=False)
    manifest, checksum = load_manifest(root)
    source = source_identity(root)
    env, started = environment(output, supplied_env), time.time()
    caches = subprocess.run(['go', 'env', 'GOMODCACHE', 'GOCACHE'], cwd=root, env=env, check=True,
                            stdout=subprocess.PIPE, stderr=subprocess.PIPE, text=True).stdout.splitlines()
    require(len(caches) == 2 and Path(caches[0]).is_dir(), 'logger_caches_unavailable')
    Path(caches[1]).mkdir(parents=True, exist_ok=True)
    runtime = {'module_cache': caches[0], 'build_cache': caches[1], 'uid': os.getuid(), 'gid': os.getgid()}
    image_log = output / 'image.json'
    inspected = command(DOCKER + ['image', 'inspect', IMAGE], root, image_log, env, 30)
    require(inspected.returncode == 0 and not inspected.timed_out and inspected.error is None
            and json.loads(image_log.read_text())[0]['Id'] == IMAGE, 'logger_image_unavailable')
    report = {'schema_version': 1, 'stage': STAGE, 'manifest_sha256': checksum, 'source_root': str(root), 'source_before': source,
              'runtime': runtime, 'image': IMAGE, 'image_log': image_log.name, 'image_log_sha256': digest(image_log),
              'suites': [], 'success': False, 'contracts_success': False, 'exit_code': 1,
              'counts': {'fail': 1}, 'error': None, 'started_at': started}
    try:
        races = 0
        for group in manifest['groups']:
            log, plain = output / (group['id'] + '.jsonl'), output / (group['id'] + '.log')
            invocation = argv(root, runtime, group)
            result = command(invocation, root, log, env, 600)
            row = {'id': group['id'], 'command': invocation, 'returncode': result.returncode,
                   'timed_out': result.timed_out, 'error': result.error, 'log': log.name, 'log_sha256': digest(log)}
            report['suites'].append(row)
            try:
                text = ''.join(json.loads(line).get('Output', '') for line in log.read_text().splitlines())
            except (ValueError, TypeError, AttributeError):
                text = log.read_text()
            plain.write_text(text)
            row.update(plain_log=plain.name, plain_log_sha256=digest(plain))
            require(not result.timed_out and result.error is None, 'logger_command_failed')
            observed, _ = execution(log, result.returncode, root, manifest, group)
            row.update(observed=observed)
            races += observed['race_reports']
        report['source_after'] = source_identity(root)
        require(source == report['source_after'], 'logger_source_changed')
        report.update(success=races == 0, contracts_success=True, exit_code=2 if races else 0,
                      counts={STAGE: 19} if races else {'pass': 19})
        verify_report(report, output, root, source)
    except (InvalidEvidence, ValueError, TypeError, KeyError, IndexError) as error:
        report.update(success=False, contracts_success=False, exit_code=1, counts={'fail': 1},
                      error=str(error) if isinstance(error, InvalidEvidence) else 'invalid_logger_evidence')
    report.setdefault('source_after', source_identity(root))
    report['finished_at'] = time.time()
    path = output / 'report.json'
    with path.open('x') as stream:
        json.dump(report, stream, ensure_ascii=False, indent=2)
        stream.write('\n')
    return {**report, 'report_path': str(path), 'report_sha256': digest(path)}


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--root', type=Path, default=Path(__file__).resolve().parents[2])
    parser.add_argument('--output', type=Path, required=True)
    args = parser.parse_args()
    report = run(args.root, args.output)
    print(json.dumps({key: report[key] for key in ('exit_code', 'counts', 'report_path')}, ensure_ascii=False))
    return report['exit_code']


if __name__ == '__main__':
    raise SystemExit(main())
