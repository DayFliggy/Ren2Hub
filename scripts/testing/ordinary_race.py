"""普通 race 的固定选择、执行和独立证据验收；任何竞争都阻断。"""
from __future__ import annotations

import argparse
from collections import Counter
import json
import os
from pathlib import Path
import re
import subprocess
import time

import native_logger_race as logger
from native_restored import InvalidEvidence, digest, regular_path, require, verified_race_source_root

STAGE = 'ordinary_race'
SELECTION = 'scripts/testing/race_selection.json'
FROZEN_SELECTION = '7afa686929a83c071986538c8e14d733d009544fb0f053e7e229db60348c18f5'
IMAGE = logger.IMAGE
DOCKER = logger.DOCKER


def load_selection(root):
    path = regular_path(root, SELECTION)
    require(digest(path) == FROZEN_SELECTION, 'ordinary_race_selection_changed')
    selection = json.loads(path.read_text())
    manifest, _ = logger.load_manifest(root)
    delegated = {group['package']: group['names'] for group in manifest['groups']}
    require(selection.get('schema_version') == 1 and selection.get('delegated') == delegated
            and selection.get('delegated_stage') == logger.STAGE, 'ordinary_race_delegation_changed')
    groups, removed = [], []
    for group in selection['groups']:
        names = group['names']
        require(names and len(names) == len(set(names)) and all(re.fullmatch(r'Test\w+', name) for name in names),
                'ordinary_race_invalid_names')
        excluded = set(delegated.get(group['package'], []))
        names = [name for name in group['names'] if name not in excluded]
        removed.extend((group['package'], name) for name in group['names'] if name in excluded)
        require(names, 'ordinary_race_empty_group')
        pattern = '^(' + '|'.join(names) + ')$'
        if group.get('subtests'):
            require(group['id'] == 'auth-cache-integrity-sqlite' and group['subtests'] == 'sqlite',
                    'ordinary_race_unknown_subtest_scope')
            pattern += '/^sqlite$'
        groups.append({**group, 'names': names, 'pattern': pattern})
    expected = sorted((package, name) for package, names in delegated.items() for name in names)
    require(sorted(removed) == expected and len(groups) == 11
            and sum(len(group['names']) for group in selection['groups']) == 143
            and sum(len(group['names']) for group in groups) == 124, 'ordinary_race_incomplete_selection')
    return selection, groups


def argv(root, runtime, group, inventory=False):
    command = logger.argv(root, runtime, group)
    command[-1] = group['pattern']
    if inventory:
        command = [value for value in command if value not in ('-race', '-json', '-count=1', '-timeout=60s')]
        command[command.index('-run')] = '-list'
        command[-1] = '^(' + '|'.join(group['names']) + ')$'
    return command


def execution(log, returncode, group):
    require(returncode == 0, 'ordinary_race_nonzero_exit')
    starts, passes, package_starts, package_passes = Counter(), Counter(), 0, 0
    output = []
    for line in log.read_text().splitlines():
        require(bool(line.strip()), 'ordinary_race_empty_log_line')
        event = json.loads(line)
        require(isinstance(event, dict) and event.get('Package') == logger.PREFIX + group['package'],
                'ordinary_race_unexpected_package')
        action, name, text = event.get('Action'), event.get('Test'), event.get('Output', '')
        require(action in ('start', 'run', 'output', 'pause', 'cont', 'pass'), 'ordinary_race_failure_or_skip')
        require(isinstance(text, str), 'ordinary_race_invalid_output')
        if name:
            require(name.split('/', 1)[0] in group['names'], 'ordinary_race_unexpected_test')
            if action == 'run':
                starts[name] += 1
            if action == 'pass':
                passes[name] += 1
        else:
            package_starts += action == 'start'
            package_passes += action == 'pass'
        if action == 'output':
            output.append(text)
    plain = ''.join(output)
    require(not re.search(r'DATA RACE|panic:|fatal error:|test timed out|\[build failed\]|\(cached\)', plain),
            'ordinary_race_competition_or_runtime_failure')
    require(starts and starts == passes and all(count == 1 for count in starts.values())
            and package_starts == 1 and package_passes == 1
            and sorted(name for name in starts if '/' not in name) == sorted(group['names']),
            'ordinary_race_missing_or_duplicate_nodes')
    if group.get('subtests'):
        require(all(name == group['names'][0] or name.startswith(group['names'][0] + '/sqlite') for name in starts)
                and group['names'][0] + '/sqlite' in starts, 'ordinary_race_subtest_scope_changed')
    return {'nodes': sorted(starts), 'top_level_passed': len(group['names']), 'passed_nodes': len(starts),
            'skipped': 0, 'race_reports': 0}, plain


def verify_report(report, output, root, expected_source=None):
    execution_root = verified_race_source_root(report, root, expected_source)
    _, groups = load_selection(root)
    require(report.get('schema_version') == 1 and report.get('stage') == STAGE
            and report.get('selection_sha256') == FROZEN_SELECTION, 'ordinary_race_selection_mismatch')
    require(isinstance(report.get('source_before'), dict) and report['source_before'] == report.get('source_after'),
            'ordinary_race_source_changed')
    if expected_source is not None:
        require(report['source_before'] == expected_source, 'ordinary_race_source_identity_mismatch')
    require(report.get('image') == IMAGE and report.get('error') is None, 'ordinary_race_execution_failed')
    image = regular_path(output, report.get('image_log'))
    require(digest(image) == report.get('image_log_sha256') and json.loads(image.read_text())[0]['Id'] == IMAGE,
            'ordinary_race_image_mismatch')
    suites, runtime = report.get('suites'), report.get('runtime', {})
    require(isinstance(suites, list) and len(suites) == len(groups), 'ordinary_race_group_count_mismatch')
    for row, group in zip(suites, groups):
        require(row.get('id') == group['id'] and row.get('command') == argv(execution_root, runtime, group)
                and row.get('inventory_command') == argv(execution_root, runtime, group, True)
                and row.get('timed_out') is False and row.get('error') is None
                and row.get('inventory_timed_out') is False and row.get('inventory_error') is None
                and row.get('inventory_returncode') == 0, 'ordinary_race_command_changed_or_failed')
        inventory = regular_path(output, row.get('inventory_log'))
        require(digest(inventory) == row.get('inventory_log_sha256'), 'ordinary_race_inventory_hash_mismatch')
        listed = [line for line in inventory.read_text().splitlines() if re.fullmatch(r'Test\w+', line)]
        require(sorted(listed) == sorted(group['names']), 'ordinary_race_actual_inventory_mismatch')
        log = regular_path(output, row.get('log'))
        require(digest(log) == row.get('log_sha256'), 'ordinary_race_log_hash_mismatch')
        observed, plain = execution(log, row.get('returncode'), group)
        raw = regular_path(output, row.get('plain_log'))
        require(digest(raw) == row.get('plain_log_sha256') and raw.read_bytes() == plain.encode('utf-8'), 'ordinary_race_plain_log_mismatch')
        require(row.get('observed') == observed, 'ordinary_race_observed_mismatch')
    require(report.get('success') is True and report.get('contracts_success') is True
            and report.get('exit_code') == 0 and report.get('counts') == {'pass': 124}, 'ordinary_race_summary_mismatch')
    return {'stage': STAGE, 'selection_sha256': FROZEN_SELECTION,
            'groups': [group['id'] for group in groups], 'test_count': 124}


def run(root, output, supplied_env=None, packages=None):
    """packages 为 None 时运行并验收固定完整选择；否则只运行所列包的组，结果不可作为发布证据。"""
    from candidate import source_identity
    from diagnostics import command, environment
    root, output = root.resolve(), output.resolve()
    output.mkdir(parents=True, mode=0o700, exist_ok=False)
    _, groups = load_selection(root)
    if packages is not None:
        groups = [group for group in groups if group['package'] in packages]
    source = source_identity(root)
    env, started = environment(output, supplied_env), time.time()
    caches = subprocess.run(['go', 'env', 'GOMODCACHE', 'GOCACHE'], cwd=root, env=env, check=True,
                            stdout=subprocess.PIPE, stderr=subprocess.PIPE, text=True).stdout.splitlines()
    require(len(caches) == 2 and Path(caches[0]).is_dir(), 'ordinary_race_caches_unavailable')
    Path(caches[1]).mkdir(parents=True, exist_ok=True)
    runtime = {'module_cache': caches[0], 'build_cache': caches[1], 'uid': os.getuid(), 'gid': os.getgid()}
    image = output / 'image.json'
    inspected = command(DOCKER + ['image', 'inspect', IMAGE], root, image, env, 30)
    require(inspected.returncode == 0 and not inspected.timed_out and inspected.error is None
            and json.loads(image.read_text())[0]['Id'] == IMAGE, 'ordinary_race_image_unavailable')
    report = {'schema_version': 1, 'stage': STAGE, 'selection_sha256': FROZEN_SELECTION,
              'source_root': str(root), 'source_before': source, 'runtime': runtime, 'image': IMAGE, 'image_log': image.name,
              'image_log_sha256': digest(image), 'suites': [], 'success': False, 'contracts_success': False,
              'exit_code': 1, 'counts': {'fail': 1}, 'error': None, 'started_at': started}
    try:
        for group in groups:
            inventory, log, plain = [output / (group['id'] + suffix) for suffix in ('.inventory.log', '.jsonl', '.log')]
            discovery = argv(root, runtime, group, True)
            listed = command(discovery, root, inventory, env, 600)
            row = {'id': group['id'], 'inventory_command': discovery, 'inventory_returncode': listed.returncode,
                   'inventory_timed_out': listed.timed_out, 'inventory_error': listed.error,
                   'inventory_log': inventory.name, 'inventory_log_sha256': digest(inventory)}
            report['suites'].append(row)
            require(listed.returncode == 0 and not listed.timed_out and listed.error is None
                    and sorted(line for line in inventory.read_text().splitlines() if re.fullmatch(r'Test\w+', line))
                    == sorted(group['names']), 'ordinary_race_actual_inventory_failed')
            invocation = argv(root, runtime, group)
            result = command(invocation, root, log, env, 600)
            row.update(command=invocation, returncode=result.returncode, timed_out=result.timed_out,
                       error=result.error, log=log.name, log_sha256=digest(log))
            try:
                text = ''.join(json.loads(line).get('Output', '') for line in log.read_text().splitlines())
            except (ValueError, TypeError, AttributeError):
                text = log.read_text()
            plain.write_text(text)
            row.update(plain_log=plain.name, plain_log_sha256=digest(plain))
            require(not result.timed_out and result.error is None, 'ordinary_race_command_failed')
            row['observed'], _ = execution(log, result.returncode, group)
        report['source_after'] = source_identity(root)
        require(source == report['source_after'], 'ordinary_race_source_changed')
        report.update(success=True, contracts_success=True, exit_code=0,
                      counts={'pass': sum(len(group['names']) for group in groups)})
        if packages is None:
            verify_report(report, output, root, source)
        else:
            report['partial_groups'] = [group['id'] for group in groups]
    except (InvalidEvidence, ValueError, TypeError, KeyError, IndexError) as error:
        report.update(success=False, contracts_success=False, exit_code=1, counts={'fail': 1},
                      error=str(error) if isinstance(error, InvalidEvidence) else 'invalid_ordinary_race_evidence')
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
