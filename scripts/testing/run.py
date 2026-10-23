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
PROFILES = ('unit', 'static', 'build', 'databases', 'race', 'browser', 'ui-contracts', 'candidate', 'all')
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
    return {'passed': len(passed), 'top_level_passed': len(required), 'skipped': 0}


def go_unit(source: Path, output: Path, env: dict[str, str]) -> dict:
    from database import REQUIRED_TESTS
    deferred = dict(DEFERRED)
    deferred.update({name: 'databases 阶段在全新三数据库执行' for name in REQUIRED_TESTS})
    results, excluded = [], []
    for module in MODULES:
        cwd = source / module
        label = 'root' if module == '.' else module
        packages = execute(['go', 'list', '-f', '{{if or .TestGoFiles .XTestGoFiles}}{{.ImportPath}}|{{.Dir}}{{end}}', './...'],
                           cwd, output, label + '-packages', env).splitlines()
        for index, entry in enumerate(filter(None, packages)):
            package, directory = entry.split('|', 1)
            name = f'{label}-{index}'
            binary = output / (name + '.test')
            execute(['go', 'test', '-c', '-p=2', '-o', str(binary), package], cwd, output,
                    name + '-compile', env, 600)
            inventory = execute([str(binary), '-test.list=.'], Path(directory), output,
                                name + '-inventory', env, 60)
            found = [line for line in inventory.splitlines() if re.fullmatch(r'(Test|Example|Fuzz)\w+', line)]
            selected = [test for test in found if test not in deferred]
            excluded.extend({'package': package, 'test': test, 'reason': deferred[test]}
                            for test in found if test in deferred)
            if not found:
                raise RuntimeError('声明了测试源码但发现清单为空: ' + package)
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
    if not results:
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


def ui_contracts(source: Path, output: Path, env: dict[str, str]) -> dict:
    env = {**env, 'REN2HUB_UI_OUTPUT': str(output)}
    with socket.socket() as reservation:
        reservation.bind(('127.0.0.1', 0))
        env['REN2HUB_UI_PORT'] = str(reservation.getsockname()[1])
    libraries = os.environ.get('REN2HUB_BROWSER_LIB_DIR')
    if libraries:
        env['LD_LIBRARY_PATH'] = libraries
    execute(['bun', 'x', '--no-install', 'playwright', 'test', '--config', 'tests/contracts/playwright.config.ts'],
            source / 'frontend', output, 'ui-contracts', env, 600)
    data = json.loads((output / 'playwright.json').read_text())
    stats = data.get('stats', {})
    if stats.get('expected') != 32 or any(stats.get(key, 0) for key in ('skipped', 'unexpected', 'flaky')):
        raise RuntimeError('UI 契约必须32项通过，零跳过/重试/失败')
    return {'success': True, 'passed': stats['expected'], 'kind': 'mock-api-ui-contracts'}


def race(source: Path, output: Path, env: dict[str, str]) -> dict:
    # A pinned container supplies gcc/CGO even on minimal production hosts.
    probe = subprocess.run(['docker', 'info'], stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
    docker = ['docker'] if probe.returncode == 0 else ['sudo', '-n', 'docker']
    execute(docker + ['image', 'inspect', 'golang:1.26.9-bookworm'], source, output, 'race-image', env)
    modcache = execute(['go', 'env', 'GOMODCACHE'], source, output, 'race-modcache', env).strip()
    cache = Path(execute(['go', 'env', 'GOCACHE'], source, output, 'race-cache', env).strip())
    cache.mkdir(parents=True, exist_ok=True)
    results = []
    suites = {
        './model': '^Test(Polling|AffiliateConcurrentAdminAndTransferOperations|RedeemConcurrentSingleSuccess)',
        './service': '^Test.*(Concurrency|Admission)',
        './controller': '^TestResponsesWebSocket(ReusesConnectionAndSettlesEachRequest|DisconnectSettlesDeliveredOutputOnce)$',
    }
    for index, (package, pattern) in enumerate(suites.items()):
        command = docker + ['run', '--rm', '--network', 'none', '--user', f'{os.getuid()}:{os.getgid()}',
                            '-v', f'{source}:/src:ro', '-v', f'{modcache}:/go/pkg/mod:ro',
                            '-v', f'{cache}:/cache', '-w', '/src', '-e', 'GOCACHE=/cache',
                            '-e', 'GOWORK=off', '-e', 'GOTOOLCHAIN=local', '-e', 'GOMAXPROCS=2',
                            '-e', 'CGO_ENABLED=1', '-e', 'GOPROXY=off', '-e', 'GOSUMDB=off',
                            'golang:1.26.9-bookworm', 'go', 'test', '-buildvcs=false', '-race', '-json',
                            '-timeout=60s', '-count=1', '-p=2', '-run', pattern, package]
        raw = execute(command, source, output, f'race-{index}', env, 600)
        results.append(go_result(raw, []))
    return {'success': True, 'suites': results}


def main() -> int:
    def interrupt(signum, frame):
        raise KeyboardInterrupt('收到终止信号')
    signal.signal(signal.SIGTERM, interrupt)
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('profile', choices=PROFILES)
    parser.add_argument('--output-parent', type=Path, default=SOURCE / '.local-tests')
    parser.add_argument('--image', help='candidate/all 使用已发布的不可变镜像；无默认生产目标')
    args = parser.parse_args()
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
              'profile': args.profile, 'success': False, 'stages': {}, 'started_at': time.time()}
    print(f'证据目录：{output}', flush=True)
    try:
        tools = {}
        for command, key in [(['go', 'version'], 'go'), (['bun', '--version'], 'bun'),
                             (['node', '--version'], 'node')]:
            tools[key] = execute(command, SOURCE, output, 'version-' + key, env).strip()
        report['tools'] = tools
        if not re.search(r'\bgo1\.26\.9\b', tools['go']) or tools['bun'] != '1.4.0':
            raise RuntimeError('要求 Go 1.26.9 与 Bun 1.4.0；不会自动下载或切换工具链')
        profiles = ('unit', 'databases', 'static', 'build', 'race', 'browser', 'ui-contracts', 'candidate') if args.profile == 'all' else (args.profile,)
        for profile in profiles:
            stage = output / profile
            stage.mkdir(mode=0o700)
            print(f'执行 {profile}', flush=True)
            if profile == 'unit':
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
                if not args.image:
                    raise RuntimeError('candidate/all 必须显式传入 --image，未运行候选不能算通过')
                from candidate import run
                result = run(SOURCE, stage, args.image)
            report['stages'][profile] = result
            if not result.get('success'):
                raise RuntimeError(f'{profile} 未通过，查看分层报告')
        if identity(SOURCE)['source_sha256'] != report['source']['source_sha256']:
            raise RuntimeError('运行期间源码改变；证据不可用于发布')
        report['success'] = True
    except (Exception, KeyboardInterrupt) as error:
        report['error'] = str(error) or type(error).__name__
        print(report['error'], file=sys.stderr)
    finally:
        report['finished_at'] = time.time()
        report['artifacts'] = {str(p.relative_to(output)): hashlib.sha256(p.read_bytes()).hexdigest()
                               for p in output.rglob('*') if p.is_file() and p.suffix in ('.log', '.json', '.jsonl')
                               and 'private' not in str(p.relative_to(output))}
        (output / 'report.json').write_text(json.dumps(report, ensure_ascii=False, indent=2) + '\n')
    return 0 if report['success'] else 1


if __name__ == '__main__':
    sys.exit(main())
