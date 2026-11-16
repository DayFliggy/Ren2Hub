#!/usr/bin/env python3
"""Ren2Hub 本地测试入口。产物和凭据只进入本轮私有目录。"""
from __future__ import annotations

import argparse
import hashlib
import json
import os
from pathlib import Path
import re
import signal
import socket
import subprocess
import sys
import tempfile
import time

sys.dont_write_bytecode = True

SOURCE = Path(__file__).resolve().parents[2]
MODULES = ('.', 'relaykit', 'tokenkit')
# Invitation URLs render window.location.origin. A random transport port changes
# visible content between reference creation and verification. Fail if occupied.
UI_CONTRACTS_PORT = 41739
PROFILES = ('registry', 'targeted', 'affected', 'unit', 'static', 'build', 'databases', 'race', 'browser', 'ui-contracts',
            'diagnostics', 'known_upstream_restored', 'known_upstream_race', 'known_upstream_logger_race',
            'candidate', 'routing', 'quick', 'all')
# 默认按改动模块测试：targeted 运行点名的测试，affected 按差异自动选择。quick 与 all 是全量档位，
# 只在明确要求全量时运行；候选只在 all 内运行一次诊断。registry 是秒级登记检查，各档位先跑它。
COMPOSITES = {'quick': ('registry', 'unit', 'static', 'build'),
              'all': ('registry', 'unit', 'databases', 'static', 'build', 'race', 'browser', 'ui-contracts', 'candidate')}
# affected --skip 可跳过的层；registry 不可跳过。
LAYERS = ('python', 'go-vet', 'go-unit', 'databases', 'race', 'vitest', 'frontend-static', 'build', 'ui-contracts')
DEFERRED = {
    'TestVLLMStatusLive': '需要真实外部供应商；本地方案不执行',
    'TestProductionEmbeddedAssetGraph': 'build 阶段验证实际前端产物',
    'TestProductionEmbeddedModelPagesAndFiles': 'build 阶段验证实际前端产物',
}


def environment(output: Path) -> dict[str, str]:
    # Do not inherit application settings, provider credentials, DSNs or proxies.
    allowed = ('PATH', 'HOME', 'USER', 'LANG', 'LC_ALL', 'GOPATH', 'GOCACHE',
               'GOMODCACHE', 'PLAYWRIGHT_BROWSERS_PATH', 'LD_LIBRARY_PATH')
    env = {key: os.environ[key] for key in allowed if key in os.environ}
    temporary = output / 'tmp'
    temporary.mkdir(mode=0o700, exist_ok=True)
    env.update(GOWORK='off', GOTOOLCHAIN='local', GOENV='off', GOPROXY='off', GOSUMDB='off',
               GOFLAGS='-mod=readonly', CGO_ENABLED='0', GOMAXPROCS='2',
               LOG_SQL_DSN='', TMPDIR=str(temporary), PYTHONDONTWRITEBYTECODE='1', CI='1')
    return env


def identity(source: Path) -> dict:
    def git(*args: str) -> bytes:
        return subprocess.check_output(['git', '-C', str(source), *args])
    base = git('rev-parse', 'HEAD').decode().strip()
    status = git('status', '--porcelain', '-z')
    digest = hashlib.sha256()
    for name in sorted(set(git('ls-files', '--cached', '--others', '--exclude-standard', '-z',
                              '--', '.', ':(exclude)docs/developer-notes/**').split(b'\0'))):
        if not name:
            continue
        path = source / os.fsdecode(name)
        digest.update(name + b'\0')
        if path.is_symlink():
            digest.update(b'link:' + os.fsencode(os.readlink(path)))
        elif path.is_file():
            digest.update(path.read_bytes())
        else:
            digest.update(b'deleted')
    return {'base_sha': base, 'candidate_sha': None if status else base,
            'dirty': bool(status), 'source_sha256': digest.hexdigest()}


def execute(command: list[str], cwd: Path, output: Path, name: str,
            env: dict[str, str], timeout: int = 300) -> str:
    log = output / (name + '.log')
    with log.open('w', encoding='utf-8') as stream:
        process = subprocess.Popen(command, cwd=cwd, env=env, stdout=stream,
                                   stderr=subprocess.STDOUT, start_new_session=True)
        try:
            code = process.wait(timeout=timeout)
        except BaseException:
            try:
                os.killpg(process.pid, signal.SIGTERM)
            except ProcessLookupError:
                pass
            try:
                process.wait(timeout=5)
            except subprocess.TimeoutExpired:
                try:
                    os.killpg(process.pid, signal.SIGKILL)
                except ProcessLookupError:
                    pass
                process.wait()
            raise
    if code:
        raise RuntimeError(f'{name} 退出 {code}；日志：{log}')
    return log.read_text()


def go_result(text: str, required: list[str]) -> dict:
    passed, skipped, failed = set(), set(), set()
    for line in text.splitlines():
        if not line.startswith('{'):
            continue
        event = json.loads(line)
        test = event.get('Test')
        if not test:
            continue
        action = event.get('Action')
        if action == 'pass':
            passed.add(test)
        elif action == 'skip':
            skipped.add(test)
        elif action == 'fail':
            failed.add(test)
    missing = set(required) - passed
    if missing or skipped or failed or not passed:
        raise RuntimeError(f'Go 执行不完整：missing={sorted(missing)}, skip={sorted(skipped)}, fail={sorted(failed)}')
    return {'passed': len(passed), 'top_level_passed': sum('/' not in name for name in passed), 'skipped': 0}


def select_tests(package: str, found: list[str], deferred: dict[str, str], pattern: str | None) -> tuple[list[str], list[str]]:
    """返回（本层运行的测试，转交其他层的测试）；正则筛掉全部可运行测试时报错，不把空运行算通过。"""
    matched = [test for test in found if not pattern or re.search(pattern, test)]
    selected = [test for test in matched if test not in deferred]
    handed_over = [test for test in matched if test in deferred]
    if pattern and not selected:
        raise RuntimeError(f'{package} 的筛选 {pattern} ' + (
            '只匹配到转交其他层的测试：' + ', '.join(handed_over) if handed_over else '没有匹配任何测试'))
    return selected, handed_over


def go_unit(source: Path, output: Path, env: dict[str, str], only: set[str] | None = None,
            patterns: dict[str, str] | None = None) -> dict:
    """only 限定包；patterns 再按顶层测试名正则筛选（targeted 使用）。"""
    from database import REQUIRED_TESTS
    deferred = dict(DEFERRED)
    deferred.update({name: 'databases 阶段在全新三数据库执行' for name in REQUIRED_TESTS})
    results, excluded = [], []
    for module in MODULES:
        cwd = source / module
        label = 'root' if module == '.' else module
        # Package discovery needs no VCS stamp. Git-file worktrees can sit
        # beneath unrelated .git directories; source identity is recorded above.
        packages = execute(['go', 'list', '-buildvcs=false', '-f', '{{if or .TestGoFiles .XTestGoFiles}}{{.ImportPath}}|{{.Dir}}{{end}}', './...'],
                           cwd, output, label + '-packages', env).splitlines()
        for index, entry in enumerate(filter(None, packages)):
            package, directory = entry.split('|', 1)
            if only is not None and package not in only:
                continue
            name = f'{label}-{index}'
            binary = output / (name + '.test')
            execute(['go', 'test', '-c', '-p=2', '-o', str(binary), package], cwd, output,
                    name + '-compile', env, 600)
            inventory = execute([str(binary), '-test.list=.'], Path(directory), output,
                                name + '-inventory', env, 60)
            found = [line for line in inventory.splitlines() if re.fullmatch(r'(Test|Example|Fuzz)\w+', line)]
            if not found:
                raise RuntimeError('声明了测试源码但发现清单为空: ' + package)
            selected, handed_over = select_tests(package, found, deferred, (patterns or {}).get(package))
            excluded.extend({'package': package, 'test': test, 'reason': deferred[test]} for test in handed_over)
            binary_hash = hashlib.sha256(binary.read_bytes()).hexdigest()
            for batch_index in range(0, len(selected), 20):
                batch = selected[batch_index:batch_index + 20]
                expression = '^(' + '|'.join(map(re.escape, batch)) + ')$'
                log = execute(['go', 'tool', 'test2json', '-t', '-p', package, str(binary),
                               '-test.v=test2json', '-test.timeout=60s', '-test.count=1', '-test.run=' + expression],
                              Path(directory), output, f'{name}-batch-{batch_index // 20}', env, 65)
                results.append({'package': package, 'inventory_count': len(found),
                                'binary_sha256': binary_hash, **go_result(log, batch)})
            binary.unlink()
    if not results and only is None:
        raise RuntimeError('Go 测试清单为空')
    return {'success': True, 'batches': results, 'deferred': excluded}


def frontend_unit(source: Path, output: Path, env: dict[str, str]) -> dict:
    report = output / 'vitest.json'
    execute(['bun', 'run', 'test:run', '--reporter=json', '--outputFile=' + str(report)],
            source / 'frontend', output, 'vitest', env, 900)
    data = json.loads(report.read_text())
    count = data.get('numPassedTests', 0)
    if not data.get('success') or count <= 0 or data.get('numTotalTests') != count:
        raise RuntimeError('Vitest 零用例、失败或跳过，详见 vitest.json')
    return {'success': True, 'passed': count, 'files': len(data.get('testResults', []))}


def static(source: Path, output: Path, env: dict[str, str]) -> dict:
    for task in ('typecheck', 'lint', 'format:check'):
        execute(['bun', 'run', task], source / 'frontend', output, task.replace(':', '-'), env, 300)
    for module in MODULES:
        execute(['go', 'vet', '-p=2', './...'], source / module, output,
                'vet-' + ('root' if module == '.' else module), env, 300)
    return {'success': True}


def build(source: Path, output: Path, env: dict[str, str]) -> dict:
    execute(['bun', 'run', 'build'], source / 'frontend', output, 'frontend-build', env, 600)
    index = source / 'frontend/embed-dist/index.html'
    original = index.read_bytes() if index.exists() else None
    try:
        execute(['node', 'scripts/prepare-frontend-embed.mjs'], source, output, 'embed', env)
        tests = ['TestProductionEmbeddedAssetGraph', 'TestProductionEmbeddedModelPagesAndFiles']
        raw = execute(['go', 'test', '-json', '-timeout=60s', '-count=1', '-p=2',
                       '-run', '^(' + '|'.join(tests) + ')$', '.'], source, output, 'embedded-assets', env)
        embedded = go_result(raw, tests)
        binary = output / 'ren2hub'
        execute(['go', 'build', '-buildvcs=false', '-p=2', '-o', str(binary), '.'],
                source, output, 'backend-build', env, 600)
        for module in MODULES[1:]:
            execute(['go', 'build', '-p=2', './...'], source / module, output, 'build-' + module, env)
        return {'success': True, 'binary': str(binary),
                'binary_sha256': hashlib.sha256(binary.read_bytes()).hexdigest(), 'embedded': embedded}
    finally:
        if original is not None:
            index.write_bytes(original)
        else:
            index.unlink(missing_ok=True)


def browser_specs(data: dict):
    for suite in data.get('suites', []):
        yield from suite.get('specs', [])
        yield from browser_specs(suite)


def playwright_cases(data: dict, required_files: set[str]) -> set[str]:
    specs = list(browser_specs(data))
    found_files = {Path(spec['file']).name for spec in specs}
    ids = [spec['id'] for spec in specs]
    if not ids or len(ids) != len(set(ids)) or found_files != required_files:
        raise RuntimeError(f'浏览器发现清单不完整：missing={sorted(required_files - found_files)}, '
                           f'unregistered={sorted(found_files - required_files)}')
    return set(ids)


def playwright_result(data: dict, expected_ids: set[str]) -> int:
    specs = list(browser_specs(data))
    actual = [spec['id'] for spec in specs]
    if set(actual) != expected_ids or len(actual) != len(expected_ids):
        raise RuntimeError('浏览器执行集合与发现清单不一致')
    for spec in specs:
        tests = spec.get('tests', [])
        if len(tests) != 1:
            raise RuntimeError(f'浏览器用例缺失或重复：{spec["id"]}')
        test = tests[0]
        results = test.get('results', [])
        if (test.get('expectedStatus') != 'passed' or test.get('status') != 'expected'
                or len(results) != 1 or results[0].get('status') != 'passed'
                or results[0].get('retry', 0) != 0):
            raise RuntimeError(f'浏览器用例失败、跳过或重试：{spec["id"]}')
    stats = data.get('stats', {})
    if stats.get('expected') != len(expected_ids) or any(stats.get(k, 0) for k in ('skipped', 'unexpected', 'flaky')):
        raise RuntimeError('浏览器报告统计与实际执行不一致')
    return len(expected_ids)


def ui_contracts(source: Path, output: Path, env: dict[str, str], only: list[str] | None = None) -> dict:
    # tests/visual asserts behaviour and layout against mocked native APIs; it has
    # no reviewed pixel references, so this layer does not claim pixel equality.
    suite = source / 'frontend/tests/visual'
    files = {path.name for path in suite.glob('*.spec.ts')}
    if not files:
        raise RuntimeError('tests/visual contains no browser specs')
    if only is not None:
        if not set(only) <= files:
            raise RuntimeError(f'选择了不存在的浏览器 spec：{sorted(set(only) - files)}')
        files = set(only)
    env = {key: value for key, value in env.items() if key != 'REN2HUB_LOCAL_SESSION'}
    with socket.socket() as reservation:
        # Permit the previous server's TIME_WAIT connections, never a live listener.
        reservation.setsockopt(socket.SOL_SOCKET, socket.SO_REUSEADDR, 1)
        reservation.bind(('127.0.0.1', UI_CONTRACTS_PORT))
        env['PLAYWRIGHT_PORT'] = str(UI_CONTRACTS_PORT)
    origin = f'http://127.0.0.1:{UI_CONTRACTS_PORT}'
    (output / 'visual-origin.json').write_text(json.dumps({'origin': origin}, indent=2))
    libraries = os.environ.get('REN2HUB_BROWSER_LIB_DIR')
    if libraries:
        env['LD_LIBRARY_PATH'] = libraries
    # Playwright empties outputDir before each run; keep attachments with this run's evidence.
    command = ['bun', 'x', '--no-install', 'playwright', 'test', '--config', 'playwright.config.ts',
               '--output', str(output / 'artifacts')]
    if only is not None:
        command += ['tests/visual/' + name for name in sorted(files)]
    requests = output / 'vite-requests.log'
    env['REN2HUB_VISUAL_REQUEST_LOG'] = str(requests)
    discovered = json.loads(execute(command + ['--list', '--reporter=json'],
                                   source / 'frontend', output, 'ui-inventory', env, 120))
    expected_ids = playwright_cases(discovered, files)
    # Match the discovered suite size. The per-case Playwright deadline still
    # catches hangs; a fixed one-hour process limit truncates the full matrix.
    deadline = max(3600, len(expected_ids) * 45 + 300)
    report = output / 'playwright.json'
    run_env = {**env, 'PLAYWRIGHT_JSON_OUTPUT_FILE': str(report)}
    execute(command + ['--reporter=list,json'], source / 'frontend', output, 'ui-contracts', run_env, deadline)
    data = json.loads(report.read_text())
    passed = playwright_result(data, expected_ids)
    record_impact(source, data, requests.read_text() if requests.exists() else '', only is not None)
    return {'success': True, 'passed': passed, 'files': sorted(files),
            'kind': 'mock-api-ui-behaviour', 'origin': origin, 'pixel_comparison': False}


def record_impact(source: Path, data: dict, requests: str, partial: bool) -> None:
    """通过的运行更新 spec→/src 模块影响图；局部运行只刷新已执行的 spec。"""
    from affected import IMPACT_MAP, build_impact, load_impact
    path = source / '.local-tests' / IMPACT_MAP
    current = load_impact(path)
    if partial and current is None:
        return
    fresh = build_impact(data, requests, browser_specs(data))
    if partial:
        fresh = {**current, 'specs': {**current['specs'], **fresh['specs']}}
    path.parent.mkdir(mode=0o700, exist_ok=True)
    temporary = path.with_suffix('.tmp')
    temporary.write_text(json.dumps(fresh, ensure_ascii=False, indent=1) + '\n')
    temporary.replace(path)


def race_selection(source: Path) -> tuple[dict, list[dict]]:
    from ordinary_race import load_selection
    return load_selection(source)


def race(source: Path, output: Path, env: dict[str, str]) -> dict:
    from ordinary_race import run
    return run(source, output / 'ordinary-race', env)


def frontend_related(source: Path, output: Path, env: dict[str, str], files: list[str]) -> dict:
    """vitest related 按导入图只跑受改动影响的用例；没有相关用例时如实记录 0。"""
    report = output / 'vitest.json'
    relative = [str(Path(path).relative_to('frontend')) for path in files]
    execute(['bun', 'x', '--no-install', 'vitest', 'related', '--run', '--passWithNoTests', '--reporter=json',
             '--outputFile=' + str(report), *relative], source / 'frontend', output, 'vitest', env, 900)
    data = json.loads(report.read_text())
    count = data.get('numPassedTests', 0)
    if not data.get('success') or data.get('numTotalTests') != count:
        raise RuntimeError('Vitest 失败或跳过，详见 vitest.json')
    return {'success': True, 'passed': count, 'files': len(data.get('testResults', [])), 'related_to': relative}


def frontend_checks(source: Path, output: Path, env: dict[str, str], selection: dict) -> dict:
    """改动文件级 lint/prettier；全局配置改动时回到整层脚本。"""
    frontend, done = source / 'frontend', []
    if selection['typecheck']:
        execute(['bun', 'run', 'typecheck'], frontend, output, 'typecheck', env, 300)
        done.append('typecheck')
    for key, script, command in (
            ('lint', 'lint', ['bun', 'x', '--no-install', 'eslint', '--max-warnings', '0', '--no-warn-ignored']),
            ('format', 'format:check', ['bun', 'x', '--no-install', 'prettier', '--check', '--ignore-unknown'])):
        chosen = selection[key]
        if chosen == 'all':
            execute(['bun', 'run', script], frontend, output, key, env, 300)
        elif chosen:
            execute(command + [str(Path(path).relative_to('frontend')) for path in chosen], frontend, output, key, env, 300)
        else:
            continue
        done.append(key)
    return {'success': True, 'ran': done}


def python_unit(source: Path, output: Path, env: dict[str, str]) -> dict:
    execute([sys.executable, '-B', '-m', 'unittest', 'discover', '-s', 'scripts/testing', '-p', 'test_*.py'],
            source, output, 'unittest', env, 300)
    return {'success': True}


def go_vet(source: Path, output: Path, env: dict[str, str], modules: dict[str, list[str]]) -> dict:
    for module, packages in modules.items():
        execute(['go', 'vet', '-p=2', *packages], source / module, output,
                'vet-' + ('root' if module == '.' else module), env, 300)
    return {'success': True, 'packages': sum(len(names) for names in modules.values())}


def registry_stage(source: Path) -> dict:
    """迭代档位先核对登记；冻结清单已偏离时后面的选择和执行都不可信。"""
    import registry
    print('执行 registry', flush=True)
    result = registry.run(source, iteration=True)
    if not result['success']:
        raise RuntimeError('registry 未通过：登记与工作树不一致，见上方清单')
    return result


def run_selection(source: Path, output: Path, env: dict[str, str], selection: dict, label: str,
                  skip: tuple[str, ...] = ()) -> tuple[dict, list[str]]:
    """按选择逐层执行；affected 与 targeted 共用。返回各层结果和被 --skip 跳过的层。"""
    go, frontend, visual, databases = selection['go'], selection['frontend'], selection['visual'], selection['databases']
    stages, skipped = {}, []

    def stage(name, function):
        if name in skip:
            skipped.append(name)
            print(f'{label} 跳过 {name}（--skip）', flush=True)
            return
        directory = output / name
        directory.mkdir(mode=0o700)
        print(f'执行 {label}/{name}', flush=True)
        started = time.monotonic()
        stages[name] = function(directory)
        stages[name]['elapsed_seconds'] = round(time.monotonic() - started, 1)
        if not stages[name].get('success'):
            raise RuntimeError(f'{label}/{name} 未通过，查看 {directory}')

    if selection['python']:
        stage('python', lambda d: python_unit(source, d, env))
    if go['vet_modules']:
        stage('go-vet', lambda d: go_vet(source, d, env, go['vet_modules']))
    if go['tested']:
        stage('go-unit', lambda d: go_unit(source, d, env, set(go['tested']), go.get('patterns')))
    if databases:
        from database import run as database_matrix
        packages, tests = databases['packages'], databases['tests']
        stage('databases', lambda d: database_matrix(source, d / 'matrix', None if packages is None else set(packages),
                                                    None if tests is None else set(tests)))
    if selection['race']:
        from ordinary_race import run as ordinary_race
        stage('race', lambda d: ordinary_race(source, d / 'ordinary-race', env, set(selection['race'])))
    if frontend['vitest'] == 'all':
        stage('vitest', lambda d: frontend_unit(source, d, env))
    elif frontend['vitest']:
        stage('vitest', lambda d: frontend_related(source, d, env, frontend['vitest']))
    if frontend['typecheck'] or frontend['lint'] or frontend['format']:
        stage('frontend-static', lambda d: frontend_checks(source, d, env, frontend))
    if frontend['build']:
        stage('build', lambda d: build(source, d, env))
    if visual['files']:
        stage('ui-contracts', lambda d: ui_contracts(source, d, env, None if visual['mode'] == 'all' else visual['files']))
    return stages, skipped


def affected(source: Path, output: Path, env: dict[str, str], base: str | None, skip: tuple[str, ...] = ()) -> dict:
    """只运行与 base 以来改动相关的层；结果用于迭代，不是发布证据。"""
    import affected as selector
    from database import PACKAGES as DATABASE_PACKAGES
    registry = registry_stage(source)
    base = base or selector.default_base(source)
    files = selector.changed_files(source, base)
    impact = selector.load_impact(source / '.local-tests' / selector.IMPACT_MAP)
    selection = selector.plan(source, files, selector.go_packages(source, env), impact)
    tested = set(selection['go']['tested_dirs'])
    packages = sorted(tested & DATABASE_PACKAGES)
    selection.update(base=base, databases={'packages': packages, 'tests': None} if packages else None,
                     race=sorted(tested & {group['package'] for group in race_selection(source)[1]}))
    (output / 'selection.json').write_text(json.dumps(selection, ensure_ascii=False, indent=2) + '\n')
    stages, skipped = run_selection(source, output, env, selection, 'affected', skip)
    for note in selection['not_covered']:
        print('affected 未覆盖：' + note, flush=True)
    return {'success': True, 'release_evidence': False, 'base': base, 'changed_files': len(files),
            'stages': {'registry': registry, **stages}, 'skipped_layers': skipped,
            'not_covered': selection['not_covered']}


def targeted(source: Path, output: Path, env: dict[str, str], request: dict) -> dict:
    """只运行点名的测试；选择由执行者按改动模块决定并写入报告，不是发布证据。"""
    import affected as selector
    from database import REQUIRED_TESTS
    registry = registry_stage(source)
    packages = selector.go_packages(source, env) if request['go'] else {}
    selection = selector.targeted(source, packages, request, set(REQUIRED_TESTS),
                                  {group['package'] for group in race_selection(source)[1]})
    (output / 'selection.json').write_text(json.dumps(selection, ensure_ascii=False, indent=2) + '\n')
    stages, _ = run_selection(source, output, env, selection, 'targeted')
    return {'success': True, 'release_evidence': False, 'request': request, 'stages': {'registry': registry, **stages}}

def main() -> int:
    def interrupt(signum, frame):
        raise KeyboardInterrupt('收到终止信号')
    signal.signal(signal.SIGTERM, interrupt)
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('profile', choices=PROFILES)
    parser.add_argument('--base', help='affected 的比较起点；默认 HEAD 与 origin/main 的分叉点')
    parser.add_argument('--skip', nargs='+', action='extend', default=[], choices=LAYERS, metavar='LAYER',
                        help='affected 跳过所列层（' + '、'.join(LAYERS) + '），跳过项写入报告')
    many = {'nargs': '+', 'action': 'extend', 'default': []}
    parser.add_argument('--go', metavar='DIR[:REGEX]', **many,
                        help='targeted：运行该 Go 包目录的单测与 vet，可用顶层测试名正则筛选，例如 model:^TestTopUpBonus')
    parser.add_argument('--db', metavar='TEST', **many, help='targeted：在三种数据库运行已登记的矩阵测试')
    parser.add_argument('--race', metavar='DIR', **many, help='targeted：运行该包目录在固定 race 选择中的组')
    parser.add_argument('--vitest', metavar='PATH', **many,
                        help='targeted：frontend/ 下的 spec 或源码文件，按导入图运行相关 Vitest 用例')
    parser.add_argument('--visual', metavar='SPEC', **many, help='targeted：frontend/tests/visual 下的 spec 文件名')
    parser.add_argument('--lint', metavar='PATH', **many,
                        help='targeted：对所列 frontend/ 文件运行 ESLint 与 Prettier，并整体 typecheck')
    parser.add_argument('--python', action='store_true', help='targeted：运行 scripts/testing 的 Python 单测')
    parser.add_argument('--scope', default='full',
                        help='candidate：full 为完整候选；否则为逗号分隔的部分名，可含 auto 或 boot，见 release_scope.py')
    parser.add_argument('--output-parent', type=Path, default=SOURCE / '.local-tests')
    parser.add_argument('--image', help='candidate/all 使用已构建的不可变镜像；无默认生产目标')
    parser.add_argument('--simulator-image', help='candidate/routing/all 使用的独立供应商模拟器镜像')
    parser.add_argument('--image-identity', type=Path, help='应用成功构建的版本化 identity.json')
    parser.add_argument('--simulator-identity', type=Path, help='独立模拟器成功构建的版本化 identity.json')
    parser.add_argument('--previous-image', help='candidate/all 普通升级使用的固定旧镜像')
    parser.add_argument('--traffic-host-interface', metavar='NAME',
                        help='candidate/all 只读挂载宿主 /proc/1/net/dev 并读取该网卡；默认使用主监听器计数')
    args = parser.parse_args()
    request = {key: getattr(args, key) for key in ('go', 'db', 'race', 'vitest', 'visual', 'lint', 'python')}
    if any(request.values()) != (args.profile == 'targeted'):
        parser.error('--go/--db/--race/--vitest/--visual/--lint/--python 只用于 targeted，且至少给出一项')
    if args.skip and args.profile != 'affected':
        parser.error('--skip 只用于 affected')
    if args.scope != 'full' and args.profile != 'candidate':
        parser.error('--scope 只用于 candidate；all 始终运行完整候选')
    from release_scope import parse as parse_scope
    try:
        scope = parse_scope(args.scope)
    except ValueError as error:
        parser.error(str(error))
    if args.traffic_host_interface is not None:
        from candidate import HOST_INTERFACE
        if not re.fullmatch(HOST_INTERFACE, args.traffic_host_interface):
            parser.error('--traffic-host-interface 只允许 1-15 个字母、数字、_ . -')
    args.output_parent = args.output_parent.absolute()
    if args.output_parent.is_symlink() or any(p.is_symlink() for p in args.output_parent.parents):
        parser.error('产物目录及父级不可为符号链接')
    args.output_parent.mkdir(parents=True, exist_ok=True, mode=0o700)
    if args.output_parent.stat().st_uid != os.getuid():
        parser.error('产物目录必须属于当前用户')
    output = Path(tempfile.mkdtemp(prefix='run-', dir=args.output_parent)).resolve()
    os.umask(0o077)
    env = environment(output)
    report = {'schema_version': 1, 'run_id': output.name, 'source': identity(SOURCE),
              'profile': args.profile, 'success': False, 'exit_code': 1, 'stages': {}, 'started_at': time.time()}
    print(f'证据目录：{output}', flush=True)
    try:
        tools = {}
        tool_commands = ([] if args.profile == 'registry' else
                         [(['go', 'version'], 'go')] if args.profile in ('routing','diagnostics','known_upstream_restored','known_upstream_race','known_upstream_logger_race')
                         else [(['go', 'version'], 'go'), (['bun', '--version'], 'bun'), (['node', '--version'], 'node')])
        for command, key in tool_commands:
            tools[key] = execute(command, SOURCE, output, 'version-' + key, env).strip()
        report['tools'] = tools
        if tools and (not re.search(r'\bgo1\.26\.10\b', tools['go']) or ('bun' in tools and tools['bun'] != '1.4.0')):
            raise RuntimeError('要求 Go 1.26.10 与 Bun 1.4.0；不会自动下载或切换工具链')
        profiles = COMPOSITES.get(args.profile, (args.profile,))
        for profile in profiles:
            stage = output / profile
            if profile not in ('known_upstream_restored', 'known_upstream_race', 'known_upstream_logger_race'):
                stage.mkdir(mode=0o700)
            print(f'执行 {profile}', flush=True)
            if profile == 'registry':
                from registry import run
                result = run(SOURCE)
            elif profile == 'targeted':
                result = targeted(SOURCE, stage, env, request)
            elif profile == 'diagnostics':
                from diagnostics import run
                result = run(SOURCE, stage, env)
            elif profile == 'known_upstream_restored':
                from native_restored import run
                result = run(SOURCE, stage, env)
            elif profile == 'known_upstream_race':
                from native_pricing_race import run
                result = run(SOURCE, stage, env)
            elif profile == 'known_upstream_logger_race':
                from native_logger_race import run
                result = run(SOURCE, stage, env)
            elif profile == 'affected':
                result = affected(SOURCE, stage, env, args.base, tuple(args.skip))
            elif profile == 'unit':
                result = {'go': go_unit(SOURCE, stage, env), 'vue': frontend_unit(SOURCE, stage, env), 'success': True}
            elif profile == 'static':
                result = static(SOURCE, stage, env)
            elif profile == 'build':
                result = build(SOURCE, stage, env)
            elif profile == 'databases':
                from database import run
                result = run(SOURCE, stage)
            elif profile == 'race':
                result = race(SOURCE, stage, env)
            elif profile == 'ui-contracts':
                result = ui_contracts(SOURCE, stage, env)
            elif profile == 'browser':
                from browser import run
                built = report['stages'].get('build')
                if not built:
                    build_dir = output / 'build'
                    build_dir.mkdir(mode=0o700)
                    built = build(SOURCE, build_dir, env)
                    report['stages']['build'] = built
                result = run(SOURCE, stage, Path(built['binary']))
            else:
                if not all((args.image, args.simulator_image, args.image_identity, args.simulator_identity)):
                    raise RuntimeError('candidate/routing/all 必须显式传入 --image、--simulator-image、--image-identity 和 --simulator-identity')
                from candidate import run
                result = run(SOURCE, stage, args.image, args.traffic_host_interface, args.previous_image, args.simulator_image, profile == 'routing',
                             args.image_identity, args.simulator_identity, scope if args.profile == 'candidate' else None)
            report['stages'][profile] = result
            if not result.get('success') and result.get('exit_code') != 2:
                raise RuntimeError(f'{profile} 未通过，查看分层报告')
        if identity(SOURCE)['source_sha256'] != report['source']['source_sha256']:
            raise RuntimeError('运行期间源码改变；证据不可用于发布')
        from routing_result import combined_exit_code
        report['exit_code'] = combined_exit_code(list(report['stages'].values()))
        report['success'] = report['exit_code'] == 0
    except (Exception, KeyboardInterrupt) as error:
        report['error'] = str(error) or type(error).__name__
        print(report['error'], file=sys.stderr)
    finally:
        report['source_end'] = identity(SOURCE)
        if report['source_end']['source_sha256'] != report['source']['source_sha256']:
            report.update(success=False, exit_code=1, error='运行期间源码改变；证据不可用于发布')
        report['finished_at'] = time.time()
        report['artifacts'] = {str(p.relative_to(output)): hashlib.sha256(p.read_bytes()).hexdigest()
                               for p in output.rglob('*') if p.is_file() and p.suffix in ('.log', '.json', '.jsonl')
                               and 'private' not in str(p.relative_to(output))}
        (output / 'report.json').write_text(json.dumps(report, ensure_ascii=False, indent=2) + '\n')
    return report['exit_code']


if __name__ == '__main__':
    sys.exit(main())
