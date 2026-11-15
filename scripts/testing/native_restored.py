"""本批恢复原生行为的独立诊断；允许集合只能来自改动前冻结的清单。"""
from __future__ import annotations

import argparse
from collections import Counter
import hashlib
import json
from pathlib import Path
import re
import time

STAGE = 'known_upstream_restored'
MANIFEST = 'scripts/testing/known_upstream_restored.json'
UPSTREAM = 'b388140acfd80369ec6243b16869daff1b4051f4'
BASELINE = '2382fbdd55a6d76d73d8e5b314c7765c98cd8da6'
ASSERTION = re.compile(r'^\s*Error Trace:\s*(.*?)^\s*Error:\s*(.*?)^\s*Test:\s*([^\n]+)', re.M | re.S)
LOCATION = re.compile(r'^[ \t]*[\w.-]+_test\.go:\d+:[ \t]*(\S[^\n]*)', re.M)


class InvalidEvidence(ValueError):
    pass


def require(condition, code):
    if not condition:
        raise InvalidEvidence(code)


def digest(path):
    return hashlib.sha256(path.read_bytes()).hexdigest()


def regular_path(root, relative):
    require(isinstance(relative, str) and relative and not Path(relative).is_absolute(), 'relative_path_required')
    path = root / relative
    require('..' not in Path(relative).parts and path.is_file() and not path.is_symlink()
            and path.resolve().is_relative_to(root.resolve()), 'invalid_evidence_path')
    return path



def verified_race_source_root(report, root, expected_source=None):
    """跨检出仅接受内容身份相同且仍可复算的原执行树。"""
    value = report.get('source_root')
    require(isinstance(value, str) and value and Path(value).is_absolute(), 'race_execution_source_root_missing')
    try:
        execution_root = Path(value).resolve(strict=True)
        require(str(execution_root) == value and execution_root.is_dir(), 'race_execution_source_root_not_canonical')
        current_root = root.resolve(strict=True)
        if execution_root != current_root:
            require(expected_source is not None and report.get('source_before') == expected_source
                    and report.get('source_after') == expected_source, 'race_cross_checkout_expected_identity_required')
            candidate_path = regular_path(current_root, 'scripts/testing/candidate.py')
            namespace = {'__name__': '_ren2hub_race_source_identity', '__file__': str(candidate_path)}
            # Load the existing implementation without writing __pycache__ in
            # either checkout during this read-only verification.
            exec(compile(candidate_path.read_bytes(), str(candidate_path), 'exec'), namespace)
            identify = namespace['source_identity']
            require(identify(current_root) == expected_source and identify(execution_root) == expected_source,
                    'race_execution_tree_drifted')
        return execution_root
    except InvalidEvidence:
        raise
    except Exception:
        raise InvalidEvidence('race_execution_source_identity_failed') from None


def load_manifest(root):
    path = regular_path(root, MANIFEST)
    manifest = json.loads(path.read_text())
    require(manifest.get('schema_version') == 1 and manifest.get('stage') == STAGE
            and manifest.get('upstream') == UPSTREAM and manifest.get('baseline') == BASELINE,
            'invalid_frozen_manifest')
    cases = manifest.get('cases')
    require(isinstance(cases, list) and cases, 'empty_diagnostic_inventory')
    seen = set()
    for case in cases:
        ident, test = case.get('id'), case.get('test')
        require(isinstance(ident, str) and ident not in seen and isinstance(test, str)
                and re.fullmatch(r'Test\w+', test), 'invalid_or_duplicate_probe')
        seen.add(ident)
        require(case.get('module') in ('.', 'relaykit', 'tokenkit')
                and re.fullmatch(r'\.|[A-Za-z0-9_/-]+', case.get('package', ''))
                and '..' not in Path(case['package']).parts, 'invalid_probe_package')
        fixtures = case.get('fixtures')
        require(isinstance(fixtures, dict) and fixtures, 'missing_probe_fixtures')
        for name, checksum in fixtures.items():
            require(digest(regular_path(root, name)) == checksum, 'frozen_fixture_changed')
        expected = case.get('expected')
        require(isinstance(expected, dict) and test in expected.get('nodes', {})
                and set(expected['nodes'].values()) <= {'pass', 'fail'}
                and all(name == test or name.startswith(test + '/') for name in expected['nodes']),
                'invalid_expected_nodes')
        require('fail' in expected['nodes'].values(), 'diagnostic_requires_native_difference')
        evidence = case.get('preflight', {})
        require(evidence.get('baseline_sha') == BASELINE and evidence.get('upstream_sha') == UPSTREAM
                and all(re.fullmatch(r'[a-f0-9]{64}', evidence.get(key, '')) for key in
                        ('baseline_log_sha256', 'upstream_log_sha256')),
                'missing_prechange_comparison')
    return manifest, digest(path)


def execution(log, package, test, returncode):
    """先校验真实执行节点，再提取全部失败断言；不从输出生成允许集合。"""
    require(returncode in (0, 1), 'invalid_command_exit')
    starts, ends, outputs = Counter(), {}, {}
    package_starts, package_ends = 0, []
    for line in log.read_text().splitlines():
        require(bool(line.strip()), 'empty_non_json_line')
        event = json.loads(line)
        require(isinstance(event, dict) and event.get('Package') == package, 'unexpected_package_or_build_failure')
        action, name, output = event.get('Action'), event.get('Test'), event.get('Output', '')
        require(action in ('start', 'run', 'pause', 'cont', 'output', 'pass', 'fail'), 'skip_or_unknown_event')
        require(isinstance(output, str), 'invalid_output')
        require(not re.search(r'(?m)^(?:panic:|fatal error:|WARNING: DATA RACE)|test timed out|\[build failed\]|\(cached\)', output),
                'runtime_failure_or_cached_result')
        if name:
            require(isinstance(name, str) and (name == test or name.startswith(test + '/')), 'unexpected_test')
            if action == 'run':
                starts[name] += 1
            if action in ('pass', 'fail'):
                ends.setdefault(name, []).append(action)
            if action == 'output':
                outputs[name] = outputs.get(name, '') + output
        else:
            package_starts += action == 'start'
            if action in ('pass', 'fail'):
                package_ends.append(action)
    require(package_starts == 1 and len(package_ends) == 1 and test in ends
            and set(starts) == set(ends) and all(starts[n] == 1 and len(v) == 1 for n, v in ends.items()),
            'missing_or_duplicate_test_result')
    failed = any(v == ['fail'] for v in ends.values())
    require(failed == (package_ends == ['fail']) == (returncode == 1), 'inconsistent_exit')
    assertions, messages = {}, {}
    for name, output in outputs.items():
        matches = list(ASSERTION.finditer(output))
        require(len(matches) == len(re.findall(r'^\s*Error:', output, re.M)), 'unparsed_assertion')
        require(all(m[3].strip() == name for m in matches), 'assertion_test_mismatch')
        if matches:
            assertions[name] = [' '.join(m[2].split()) for m in matches]
        # t.Error/t.Fatal/t.Log 的源码行也逐条绑定，不能藏进已知 testify 失败中。
        direct = [' '.join(m[1].split()) for m in LOCATION.finditer(output)]
        direct.extend('testify: ' + ' '.join(message.split()) for message in
                      re.findall(r'^[ \t]*Messages:[ \t]*([^\n]*)', output, re.M))
        if direct:
            messages[name] = direct
    return {'nodes': {n: v[0] for n, v in sorted(ends.items())},
            'assertions': assertions, 'messages': messages}


def package_name(root, case):
    module_file = regular_path(root, ('' if case['module'] == '.' else case['module'] + '/') + 'go.mod')
    module = re.search(r'^module\s+(\S+)', module_file.read_text(), re.M)
    require(module is not None, 'module_name_missing')
    return module[1] + ('' if case['package'] == '.' else '/' + case['package'])


def binary_key(case):
    return case['module'] + ':' + case['package']


def binary_path(output, case):
    name = binary_key(case).replace('/', '_').replace(':', '__').replace('.', 'root')
    return Path(output).resolve() / 'bin' / (name + '.test')


def compile_command(case, output):
    return ['go', 'test', '-c', '-buildvcs=false', '-p=2', '-tags=' + STAGE, '-o', str(binary_path(output, case)),
            '.' if case['package'] == '.' else './' + case['package']]


def invocation(case, output, package):
    """同包探针共用一次编译；每个探针仍是独立进程、独立日志和 60 秒上限。"""
    return ['go', 'tool', 'test2json', '-t', '-p', package, str(binary_path(output, case)),
            '-test.v=test2json', '-test.timeout=60s', '-test.count=1', '-test.run=^' + case['test'] + '$']


def compile_binaries(manifest, root, output, env, command):
    binaries = {}
    for case in manifest['cases']:
        key = binary_key(case)
        if key in binaries:
            continue
        path = binary_path(output, case)
        path.parent.mkdir(mode=0o700, exist_ok=True)
        log = output / ('compile-' + path.stem + '.log')
        argv = compile_command(case, output)
        result = command(argv, root / case['module'], log, env, 600)
        entry = {'command': argv, 'log': log.name, 'log_sha256': digest(log), 'returncode': result.returncode,
                 'timed_out': result.timed_out, 'error': result.error, 'sha256': None}
        if result.returncode == 0 and not result.timed_out and result.error is None and path.is_file():
            entry['sha256'] = digest(path)
        binaries[key] = entry
    return binaries


def verify_report(report, output, root, expected_source=None):
    """候选及发布各自重读冻结清单、fixture 和原始日志，不信任汇总结论。"""
    manifest, checksum = load_manifest(root)
    require(report.get('schema_version') == 1 and report.get('stage') == STAGE
            and report.get('manifest_sha256') == checksum, 'report_manifest_mismatch')
    require(report.get('source_before') == report.get('source_after')
            and isinstance(report.get('source_before'), dict), 'source_changed_during_diagnostic')
    if expected_source is not None:
        require(report['source_before'] == expected_source, 'diagnostic_source_identity_mismatch')
    rows = report.get('cases')
    require(isinstance(rows, list) and len(rows) == len(manifest['cases'])
            and all(isinstance(row, dict) for row in rows), 'missing_diagnostic_case')
    by_id = {row.get('id'): row for row in rows}
    require(len(by_id) == len(rows) and set(by_id) == {case['id'] for case in manifest['cases']},
            'extra_or_duplicate_diagnostic_case')
    for case in manifest['cases']:
        row = by_id[case['id']]
        require(row.get('status') == STAGE and row.get('timed_out') is False
                and row.get('error') is None, 'diagnostic_execution_failed')
        require(row.get('command') == invocation(case, output, package_name(root, case)), 'diagnostic_command_mismatch')
        built = (report.get('binaries') or {}).get(binary_key(case)) or {}
        require(built.get('command') == compile_command(case, output) and built.get('returncode') == 0
                and built.get('timed_out') is False and built.get('error') is None
                and digest(regular_path(output, built.get('log'))) == built.get('log_sha256')
                and re.fullmatch(r'[a-f0-9]{64}', built.get('sha256') or '')
                and row.get('binary_sha256') == built['sha256'], 'diagnostic_binary_mismatch')
        log = regular_path(output, row.get('log'))
        require(digest(log) == row.get('log_sha256'), 'diagnostic_log_hash_mismatch')
        actual = execution(log, package_name(root, case), case['test'], row.get('returncode'))
        require(actual == case['expected'] and row.get('observed') == actual, 'diagnostic_fingerprint_mismatch')
    require(report.get('success') is False and report.get('contracts_success') is True
            and report.get('exit_code') == 2 and report.get('counts') == {STAGE: len(rows)},
            'diagnostic_summary_mismatch')
    return {'stage': STAGE, 'manifest_sha256': checksum, 'cases': [case['id'] for case in manifest['cases']]}


def run(root, output, supplied_env=None):
    from candidate import source_identity
    from diagnostics import command, environment
    root, output = root.resolve(), output.resolve()
    output.mkdir(parents=True, mode=0o700, exist_ok=False)
    manifest, checksum = load_manifest(root)
    source = source_identity(root)
    env = environment(output, supplied_env)
    started, rows = time.time(), []
    binaries = compile_binaries(manifest, root, output, env, command)
    for index, case in enumerate(manifest['cases']):
        built = binaries[binary_key(case)]
        row = {'id': case['id'], 'status': 'fail', 'binary_sha256': built['sha256']}
        rows.append(row)
        if built['sha256'] is None:
            row['error'] = 'compile_failed'
            continue
        log = output / f'{index:03d}.jsonl'
        argv = invocation(case, output, package_name(root, case))
        directory = root / case['module'] / ('' if case['package'] == '.' else case['package'])
        result = command(argv, directory, log, env, 90)
        row.update({'command': argv, 'log': log.name, 'log_sha256': digest(log),
                    'returncode': result.returncode, 'timed_out': result.timed_out, 'error': result.error})
        try:
            require(not result.timed_out and result.error is None, 'command_failed')
            row['observed'] = execution(log, package_name(root, case), case['test'], result.returncode)
            require(row['observed'] == case['expected'], 'diagnostic_fingerprint_mismatch')
            row['status'] = STAGE
        except (InvalidEvidence, ValueError, TypeError, KeyError) as error:
            row['error'] = str(error) if isinstance(error, InvalidEvidence) else 'invalid_test_output'
    for key, built in binaries.items():
        if built['sha256'] is not None:
            binary_path(output, next(c for c in manifest['cases'] if binary_key(c) == key)).unlink()
    end = source_identity(root)
    accepted = source == end and all(row['status'] == STAGE for row in rows)
    report = {'schema_version': 1, 'stage': STAGE, 'success': False, 'contracts_success': accepted,
              'race_instrumented': False,
              'exit_code': 2 if accepted else 1, 'manifest_sha256': checksum,
              'source_before': source, 'source_after': end, 'binaries': binaries, 'cases': rows,
              'counts': dict(Counter(row['status'] for row in rows)), 'started_at': started, 'finished_at': time.time()}
    if accepted:
        verify_report(report, output, root, source)
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
