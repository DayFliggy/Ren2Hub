"""固定 b388 价格缓存竞争的独立诊断；不能用于放行其他 race。"""
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

STAGE = 'known_upstream_race'
MANIFEST = 'scripts/testing/known_upstream_race.json'
UPSTREAM = 'b388140acfd80369ec6243b16869daff1b4051f4'
BASELINE = '2382fbdd55a6d76d73d8e5b314c7765c98cd8da6'
IMAGE = 'sha256:0d494ea9b5863337de0dfb4f938062bf0261a919016eb162d2be26381dbadafe'
PACKAGE = 'github.com/QuantumNous/new-api/model'
TEST = 'TestRestoredNativeControlPricingConcurrentRefresh'
FIXTURE = 'model/native_pricing_race_test.go'
ACCESS_LINES = {
    'GetPricing': ['if time.Since(lastGetPricingTime) > time.Minute*1 || len(pricingMap) == 0 {', 'return pricingMap'],
    'InvalidatePricingCache': ['pricingMap = nil', 'lastGetPricingTime = time.Time{}'],
    'updatePricing': ['pricingMap = make([]Pricing, 0)', 'pricingMap = append(pricingMap, pricing)', 'lastGetPricingTime = time.Now()'],
}
CREATION_PATHS = [
    [PACKAGE + '.' + TEST, 'testing.tRunner', 'testing.(*T).Run.gowrap1'],
    ['testing.(*T).Run', 'testing.runTests.func1', 'testing.tRunner', 'testing.runTests',
     'testing.(*M).Run', PACKAGE + '.TestMain', 'main.main'],
]
CREATION_SITES = {
    'test_entry': {'path': 'model/task_cas_test.go', 'source_line': 'os.Exit(m.Run())'},
    'reader_spawn': {'path': FIXTURE, 'source_line': 'go func() {'},
}
DOCKER = ['sudo', '-n', 'docker', '--host', 'unix:///var/run/docker.sock']


def function_body(path, name):
    matches = re.findall(r'^func ' + re.escape(name) + r'\([^\n]*\{.*?^}', path.read_text(), re.M | re.S)
    require(len(matches) == 1, 'native_function_missing_or_ambiguous')
    return matches[0]


def load_manifest(root):
    path = regular_path(root, MANIFEST)
    data = json.loads(path.read_text())
    require(data.get('schema_version') == 1 and data.get('stage') == STAGE
            and data.get('upstream') == UPSTREAM and data.get('baseline') == BASELINE
            and data.get('image') == IMAGE and data.get('package') == './model'
            and data.get('test') == TEST and data.get('access_lines') == ACCESS_LINES
            and data.get('creation_paths') == CREATION_PATHS and data.get('creation_sites') == CREATION_SITES,
            'invalid_frozen_race_manifest')
    require(data.get('fixtures') == {FIXTURE: digest(regular_path(root, FIXTURE))}, 'race_fixture_changed')
    fixture = regular_path(root, FIXTURE).read_text()
    require(fixture.startswith('//go:build known_upstream_race\n')
            and re.findall(r'^func (Test\w+)\(', fixture, re.M) == [TEST], 'unexpected_race_fixture_test')
    entry_key = CREATION_SITES['test_entry']['path'] + ':TestMain'
    entry_functions = data.get('local_fixture_functions')
    require(isinstance(entry_functions, dict) and set(entry_functions) == {entry_key},
            'invalid_test_entry_function_inventory')
    entry_hash = entry_functions[entry_key]
    require(isinstance(entry_hash, str) and re.fullmatch(r'[0-9a-f]{64}', entry_hash)
            and hashlib.sha256(function_body(regular_path(root, CREATION_SITES['test_entry']['path']),
                                               'TestMain').encode()).hexdigest() == entry_hash,
            'race_test_entry_function_changed')
    bodies = data.get('native_functions')
    require(isinstance(bodies, dict) and set(bodies) == {'GetPricing', 'InvalidatePricingCache', 'RefreshPricing'},
            'invalid_native_function_inventory')
    source = regular_path(root, 'model/pricing.go')
    for name, expected in bodies.items():
        require(isinstance(expected, str) and re.fullmatch(r'[0-9a-f]{64}', expected)
                and hashlib.sha256(function_body(regular_path(root, 'model/pricing_refresh.go') if name == 'RefreshPricing' else source, name).encode()).hexdigest() == expected,
                'restored_pricing_function_changed')
    preflight = data.get('preflight', {})
    require(preflight.get('baseline_exit') == 0 and preflight.get('native_exit') == 1
            and all(re.fullmatch(r'[0-9a-f]{64}', preflight.get(k, '')) for k in
                    ('baseline_log_sha256', 'native_log_sha256')),
            'missing_race_preflight')
    for name in ('InvalidatePricingCache', 'updatePricing'):
        assignments = re.findall(r'^\s*((?:pricingMap|lastGetPricingTime)\s*=\s*[^\n]+)', function_body(source, name), re.M)
        require(assignments == ACCESS_LINES[name], 'restored_pricing_assignments_changed')
    return data, digest(path)


def race_block(block, root):
    """保留函数身份，只忽略地址、偏移、goroutine 编号和源码行号。"""
    require(block.startswith('WARNING: DATA RACE\n'), 'invalid_race_block')
    accesses = list(re.finditer(r'^(?:Previous )?(Read|Write|read|write) at 0x[0-9a-f]+ by goroutine ([0-9]+):\n', block, re.M))
    require(len(accesses) == 2, 'unexpected_race_access_count')
    observed, expected_creations = {}, {}
    for index, match in enumerate(accesses):
        end = accesses[index + 1].start() if index + 1 < len(accesses) else len(block)
        stack = block[match.end():end].split('\nGoroutine ', 1)[0].strip('\n')
        lines = stack.splitlines()
        require(len(lines) % 2 == 0 and lines, 'invalid_race_stack')
        names = []
        for i in range(0, len(lines), 2):
            require(re.fullmatch(r'  (\S+)\(\)', lines[i]) is not None
                    and re.fullmatch(r'\s+\S+\.go:[0-9]+(?: \+0x[0-9a-f]+)?', lines[i + 1]) is not None,
                    'invalid_race_frame')
            names.append(lines[i].strip()[:-2])
        kind = match[1].lower()
        function = names[0].removeprefix(PACKAGE + '.')
        require(kind not in observed and names[0].startswith(PACKAGE + '.')
                and function in ({'GetPricing'} if kind == 'read' else {'InvalidatePricingCache', 'updatePricing'}),
                'unknown_pricing_race_pair')
        location = re.fullmatch(r'\s+/src/model/pricing\.go:([0-9]+)(?: \+0x[0-9a-f]+)?', lines[1])
        require(location is not None, 'unexpected_pricing_race_location')
        source_lines = regular_path(root, 'model/pricing.go').read_text().splitlines()
        lineno = int(location[1])
        require(0 < lineno <= len(source_lines), 'invalid_race_source_line')
        source_line = source_lines[lineno - 1].strip()
        body = function_body(regular_path(root, 'model/pricing.go'), function)
        declaration = next((i for i in range(lineno - 1, -1, -1) if source_lines[i].startswith('func ')), -1)
        require(declaration >= 0 and source_lines[declaration].startswith('func ' + function + '(')
                and source_line in ACCESS_LINES[function] and source_line in body,
                'unapproved_pricing_race_source_line')
        if kind == 'read' and source_line.startswith('if '):
            require(lineno == declaration + 2, 'locked_inner_read_is_not_approved_race')
        short = [name.removeprefix(PACKAGE + '.') for name in names if name.startswith(PACKAGE + '.')]
        paths = ([['GetPricing', TEST + '.func1']] if kind == 'read' else
                 [['InvalidatePricingCache', TEST]] if function == 'InvalidatePricingCache' else
                 [['updatePricing', 'RefreshPricing', TEST], ['updatePricing', 'GetPricing', TEST + '.func1']])
        require(short in paths, 'unknown_pricing_race_stack')
        reader = short[-1] == TEST + '.func1'
        suffix = [] if reader else ['testing.tRunner', 'testing.(*T).Run.gowrap1']
        require(names == [PACKAGE + '.' + part for part in short] + suffix, 'unknown_pricing_race_access_frames')
        goroutine = match[2]
        require(goroutine not in expected_creations, 'race_uses_same_goroutine')
        expected_creations[goroutine] = 0 if reader else 1
        observed[kind] = {'function': names[0], 'source_line': source_line, 'path': short}
    require(set(observed) == {'read', 'write'}, 'missing_pricing_race_pair')
    creations = list(re.finditer(r'^Goroutine ([0-9]+) \((?:running|finished)\) created at:\n', block, re.M))
    require(len(creations) == 2 and {m[1] for m in creations} == set(expected_creations),
            'missing_or_additional_race_creation_stack')
    for index, created in enumerate(creations):
        end = creations[index + 1].start() if index + 1 < len(creations) else len(block)
        lines = block[created.end():end].strip('\n').splitlines()
        require(lines and len(lines) % 2 == 0, 'invalid_race_creation_stack')
        names = []
        for i in range(0, len(lines), 2):
            match = re.fullmatch(r'  (\S+)\(\)', lines[i])
            require(match is not None and re.fullmatch(r'\s+\S+\.go:[0-9]+(?: \+0x[0-9a-f]+)?', lines[i + 1]) is not None,
                    'invalid_race_creation_frame')
            names.append(match[1])
        category = expected_creations[created[1]]
        require(names == CREATION_PATHS[category], 'unknown_pricing_race_creation_path')
        site = CREATION_SITES['reader_spawn' if category == 0 else 'test_entry']
        frame_index = 0 if category == 0 else names.index(PACKAGE + '.TestMain')
        location = re.fullmatch(r'\s+/src/' + re.escape(site['path']) + r':([0-9]+)(?: \+0x[0-9a-f]+)?', lines[frame_index * 2 + 1])
        require(location is not None, 'unexpected_race_creation_location')
        source_lines = regular_path(root, site['path']).read_text().splitlines()
        lineno = int(location[1])
        require(0 < lineno <= len(source_lines) and source_lines[lineno - 1].strip() == site['source_line'],
                'unapproved_race_creation_source_line')
    return observed


def execution(log, returncode, root):
    require(returncode == 1, 'race_must_reproduce_exit_one')
    starts, ends, package_starts, package_ends = Counter(), {}, 0, []
    outputs = []
    for line in log.read_text().splitlines():
        require(bool(line.strip()), 'empty_race_log_line')
        event = json.loads(line)
        require(isinstance(event, dict) and event.get('Package') == PACKAGE, 'unexpected_race_package_or_build_failure')
        action, name, output = event.get('Action'), event.get('Test'), event.get('Output', '')
        require(action in ('start', 'run', 'output', 'fail'), 'unexpected_race_action_or_skip')
        require(isinstance(output, str) and not re.search(r'(?m)^(?:panic:|fatal error:)|test timed out|\[build failed\]|\(cached\)', output),
                'race_runtime_or_build_failure')
        if name:
            require(name == TEST, 'unexpected_race_test')
            if action == 'run':
                starts[name] += 1
            if action == 'fail':
                ends.setdefault(name, []).append(action)
        else:
            package_starts += action == 'start'
            if action == 'fail':
                package_ends.append(action)
        if action == 'output':
            outputs.append(output)
    require(starts == {TEST: 1} and ends == {TEST: ['fail']}
            and package_starts == 1 and package_ends == ['fail'], 'missing_or_duplicate_race_result')
    text = ''.join(outputs)
    blocks = re.findall(r'^==================\n(WARNING: DATA RACE\n.*?)^==================\n', text, re.M | re.S)
    require(blocks and len(blocks) == text.count('WARNING: DATA RACE'), 'race_not_reproduced_or_unparsed')
    pairs = [race_block(block, root) for block in blocks]
    remaining = re.sub(r'^==================\nWARNING: DATA RACE\n.*?^==================\n', '', text, flags=re.M | re.S)
    allowed = [r'=== RUN   ' + TEST,
               r'\s+testing\.go:[0-9]+: race detected during execution of test',
               r'--- FAIL: ' + TEST + r' \([0-9.]+s\)', 'FAIL',
               r'FAIL\t' + re.escape(PACKAGE) + r'\t[0-9.]+s']
    require(all(any(re.fullmatch(pattern, line) for pattern in allowed) for line in remaining.splitlines()),
            'additional_race_failure_or_output')
    require(remaining.count('race detected during execution of test') == 1, 'missing_or_duplicate_race_failure_reason')
    return {'nodes': {TEST: 'fail'}, 'race_reports': len(pairs), 'pairs': pairs,
            'failure_reason': 'race detected during execution of test'}


def argv(root, runtime):
    for key in ('module_cache', 'build_cache'):
        value = runtime.get(key)
        require(isinstance(value, str) and Path(value).is_absolute() and not re.search(r'[:,\n]', value), 'invalid_race_cache_path')
    require(isinstance(runtime.get('uid'), int) and runtime['uid'] >= 0
            and isinstance(runtime.get('gid'), int) and runtime['gid'] >= 0, 'invalid_race_uid')
    require(not re.search(r'[:,\n]', str(root)), 'invalid_race_source_path')
    command = DOCKER + ['run', '--rm', '--pull=never', '--network', 'none', '--user', f"{runtime['uid']}:{runtime['gid']}",
                       '-v', str(root) + ':/src:ro', '-v', runtime['module_cache'] + ':/go/pkg/mod:ro',
                       '-v', runtime['build_cache'] + ':/cache', '-w', '/src']
    for value in ('GOCACHE=/cache', 'GOWORK=off', 'GOTOOLCHAIN=local', 'GOENV=off', 'GOMAXPROCS=2',
                  'CGO_ENABLED=1', 'GOPROXY=off', 'GOSUMDB=off', 'GOFLAGS=-mod=readonly', 'CI=1', 'LOG_SQL_DSN='):
        command += ['-e', value]
    return command + [IMAGE, 'go', 'test', '-buildvcs=false', '-race', '-json', '-count=1', '-timeout=60s',
                      '-p=2', '-tags=' + STAGE, './model', '-run', '^' + TEST + '$']


def verify_report(report, output, root, expected_source=None):
    execution_root = verified_race_source_root(report, root, expected_source)
    _, checksum = load_manifest(root)
    require(report.get('schema_version') == 1 and report.get('stage') == STAGE
            and report.get('manifest_sha256') == checksum, 'race_manifest_mismatch')
    require(isinstance(report.get('source_before'), dict) and report['source_before'] == report.get('source_after'), 'race_source_changed')
    if expected_source is not None:
        require(report['source_before'] == expected_source, 'race_source_identity_mismatch')
    require(report.get('image') == IMAGE and report.get('timed_out') is False and report.get('error') is None,
            'race_execution_failed')
    require(report.get('command') == argv(execution_root, report.get('runtime', {})), 'race_command_changed')
    identity = regular_path(output, report.get('image_log'))
    require(digest(identity) == report.get('image_log_sha256')
            and json.loads(identity.read_text())[0]['Id'] == IMAGE, 'race_image_identity_mismatch')
    log = regular_path(output, report.get('log'))
    require(digest(log) == report.get('log_sha256'), 'race_log_hash_mismatch')
    observed = execution(log, report.get('returncode'), root)
    require(report.get('observed') == observed, 'race_observed_mismatch')
    require(report.get('success') is False and report.get('contracts_success') is True
            and report.get('exit_code') == 2 and report.get('counts') == {STAGE: 1}, 'race_summary_mismatch')
    return {'stage': STAGE, 'manifest_sha256': checksum, 'cases': [TEST], 'race_reports': observed['race_reports']}


def run(root, output, supplied_env=None):
    from candidate import source_identity
    from diagnostics import command, environment
    root, output = root.resolve(), output.resolve()
    output.mkdir(parents=True, mode=0o700, exist_ok=False)
    _, checksum = load_manifest(root)
    source = source_identity(root)
    env, started = environment(output, supplied_env), time.time()
    caches = subprocess.run(['go', 'env', 'GOMODCACHE', 'GOCACHE'], cwd=root, env=env, check=True,
                            stdout=subprocess.PIPE, stderr=subprocess.PIPE, text=True).stdout.splitlines()
    require(len(caches) == 2 and Path(caches[0]).is_dir(), 'race_caches_unavailable')
    Path(caches[1]).mkdir(parents=True, exist_ok=True)
    runtime = {'module_cache': caches[0], 'build_cache': caches[1], 'uid': os.getuid(), 'gid': os.getgid()}
    image_log, log = output / 'image.json', output / 'race.jsonl'
    inspected = command(DOCKER + ['image', 'inspect', IMAGE], root, image_log, env, 30)
    require(inspected.returncode == 0 and not inspected.timed_out and inspected.error is None
            and json.loads(image_log.read_text())[0]['Id'] == IMAGE, 'race_image_unavailable')
    invocation = argv(root, runtime)
    result = command(invocation, root, log, env, 600)
    report = {'schema_version': 1, 'stage': STAGE, 'manifest_sha256': checksum,
              'source_root': str(root), 'source_before': source, 'source_after': source_identity(root), 'runtime': runtime,
              'image': IMAGE, 'image_log': image_log.name, 'image_log_sha256': digest(image_log),
              'command': invocation, 'log': log.name, 'log_sha256': digest(log), 'returncode': result.returncode,
              'timed_out': result.timed_out, 'error': result.error, 'success': False, 'contracts_success': False,
              'exit_code': 1, 'counts': {'fail': 1}, 'started_at': started, 'finished_at': time.time()}
    try:
        require(not result.timed_out and result.error is None, 'race_command_failed')
        report['observed'] = execution(log, result.returncode, root)
        require(report['source_before'] == report['source_after'], 'race_source_changed')
        report.update(contracts_success=True, exit_code=2, counts={STAGE: 1})
        verify_report(report, output, root, source)
    except (InvalidEvidence, ValueError, TypeError, KeyError, IndexError) as error:
        report.update(contracts_success=False, exit_code=1, counts={'fail': 1},
                      error=str(error) if isinstance(error, InvalidEvidence) else 'invalid_race_evidence')
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
