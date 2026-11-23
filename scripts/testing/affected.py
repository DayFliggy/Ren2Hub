"""按当前改动选择开发期测试。结果只服务迭代，不能替代 quick、all 或候选发布证据。"""
from __future__ import annotations

import json
import re
import subprocess
from pathlib import Path, PurePosixPath

MODULES = ('.', 'relaykit', 'tokenkit')
# 这些文件改变工具链、依赖或全局配置，对应层只能整体重跑。
FRONTEND_GLOBAL = {'frontend/package.json', 'frontend/bun.lock', 'frontend/tsconfig.json',
                   'frontend/tsconfig.app.json', 'frontend/tsconfig.node.json', 'frontend/vite.config.ts'}
VITEST_GLOBAL = FRONTEND_GLOBAL | {'frontend/vitest.config.ts'}
VISUAL_GLOBAL = FRONTEND_GLOBAL | {'frontend/playwright.config.ts', 'frontend/index.html'}
LINT_GLOBAL = FRONTEND_GLOBAL | {'frontend/eslint.config.js'}
FORMAT_GLOBAL = FRONTEND_GLOBAL | {'frontend/.prettierrc.json', 'frontend/.prettierignore'}
BUILD_INPUTS = ('frontend/src/', 'frontend/public/', 'frontend/index.html', 'scripts/prepare-frontend-embed.mjs')
SCRIPT_CODE = re.compile(r'\.(ts|tsx|vue|js|mjs|cjs)$')
LITERAL = re.compile(r'"((?:[^"\\]|\\.)*)"|`([^`]*)`')
JOIN = re.compile(r'filepath\.Join\(((?:\s*"[^"]*"\s*,?)+)')
IMPACT_MAP = 'ui-impact.json'


def git(source: Path, *args: str) -> list[str]:
    raw = subprocess.check_output(['git', '-C', str(source), *args])
    return [name for name in raw.decode().split('\0') if name]


def changed_files(source: Path, base: str) -> list[str]:
    """提交差异、暂存、未暂存和未跟踪文件，均相对 Ren2Hub 根目录。"""
    names = set(git(source, 'diff', '--name-only', '-z', base, '--'))
    names.update(git(source, 'ls-files', '--others', '--exclude-standard', '-z'))
    return sorted(name for name in names if not name.startswith('docs/developer-notes/'))


def default_base(source: Path) -> str:
    """未指定时与内部 main 的分叉点比较；没有 origin/main 时只看工作区改动。"""
    try:
        return git(source, 'merge-base', 'HEAD', 'origin/main')[0].strip()
    except (subprocess.CalledProcessError, IndexError):
        return 'HEAD'
GO_FIELDS = ('ImportPath', 'Dir', 'Deps', 'Imports', 'TestImports', 'XTestImports', 'GoFiles', 'CgoFiles',
             'TestGoFiles', 'XTestGoFiles', 'OtherFiles', 'SFiles', 'EmbedFiles', 'TestEmbedFiles', 'XTestEmbedFiles')


def go_packages(source: Path, env: dict[str, str]) -> dict[str, dict]:
    """三个模块的包图；包目录相对 Ren2Hub 根目录。"""
    packages = {}
    for module in MODULES:
        raw = subprocess.run(['go', 'list', '-buildvcs=false', '-json=' + ','.join(GO_FIELDS), './...'],
                             cwd=source / module, env=env, check=True, capture_output=True, text=True).stdout
        for item in decode_stream(raw):
            directory = Path(item['Dir']).resolve().relative_to(source.resolve()).as_posix()
            packages[item['ImportPath']] = {**item, 'Dir': directory, 'module': module}
    return packages


def decode_stream(raw: str):
    decoder, index = json.JSONDecoder(), 0
    while index < len(raw):
        if raw[index].isspace():
            index += 1
            continue
        item, index = decoder.raw_decode(raw, index)
        yield item


def test_read_paths(source: Path, package: dict) -> set[str]:
    """测试源码里能解析到真实文件或目录的相对路径，例如 ../frontend/tests/fixtures/native。"""
    found = set()
    directory = source / package['Dir']
    for name in package.get('TestGoFiles', []) + package.get('XTestGoFiles', []):
        text = (directory / name).read_text(encoding='utf-8')
        candidates = [a or b for a, b in LITERAL.findall(text)]
        candidates += ['/'.join(re.findall(r'"([^"]*)"', args)) for args in JOIN.findall(text)]
        for value in candidates:
            if len(value) > 200 or re.search(r'\s', value) or ('/' not in value and not re.fullmatch(r'[\w.-]+\.\w+', value)):
                continue
            value = re.split(r'[*?\[]', value, maxsplit=1)[0]
            target = (directory / value).resolve()
            try:
                relative = target.relative_to(source.resolve()).as_posix()
            except ValueError:
                continue
            if relative not in ('', '.') and target.exists():
                found.add(relative)
    return found


PRODUCTION_KEYS = ('GoFiles', 'CgoFiles', 'OtherFiles', 'SFiles', 'EmbedFiles')
TEST_KEYS = ('TestGoFiles', 'XTestGoFiles', 'TestEmbedFiles', 'XTestEmbedFiles')


def owned_files(package: dict, keys: tuple[str, ...]) -> set[str]:
    prefix = '' if package['Dir'] == '.' else package['Dir'] + '/'
    return {prefix + name for key in keys for name in package.get(key) or []}


def under(path: str, root: str) -> bool:
    return path == root or path.startswith(root.rstrip('/') + '/')


def changed_packages(packages: dict[str, dict], files: list[str]) -> tuple[dict[str, list[str]], dict[str, list[str]]]:
    """返回（生产改动，仅测试改动）。生产改动会传给反向依赖；测试文件、testdata 只影响本包测试。"""
    production: dict[str, list[str]] = {}
    test_only: dict[str, list[str]] = {}
    for path in files:
        for name, package in packages.items():
            module = PurePosixPath(package['module'])
            prefix = '' if package['Dir'] == '.' else package['Dir'] + '/'
            # 删除或受构建标签排除的 .go 文件不在 go list 文件表中，按所在目录归属。
            loose_go = path.endswith('.go') and PurePosixPath(path).parent.as_posix() == package['Dir']
            if path in (str(module / 'go.mod'), str(module / 'go.sum')) or path in owned_files(package, PRODUCTION_KEYS) \
                    or (loose_go and not path.endswith('_test.go')):
                production.setdefault(name, []).append(path)
            elif path in owned_files(package, TEST_KEYS) or loose_go or path.startswith(prefix + 'testdata/'):
                test_only.setdefault(name, []).append(path)
    return production, test_only


def test_closure(package: dict, packages: dict[str, dict]) -> set[str]:
    """测试二进制会链接的本仓库包：自身、生产依赖、测试导入及其依赖。"""
    closure = {package['ImportPath'], *(package.get('Deps') or [])}
    for name in (package.get('TestImports') or []) + (package.get('XTestImports') or []):
        closure.add(name)
        closure.update(packages.get(name, {}).get('Deps') or [])
    return closure


def summary(prefix: str, values: list[str]) -> str:
    return prefix + ', '.join(values[:3]) + (' …' if len(values) > 3 else '')


def select_go(source: Path, packages: dict[str, dict], files: list[str]) -> dict:
    """返回需 vet 的包及需运行测试的包（单测、数据库矩阵、race 共用），附选择原因。"""
    production, test_only = changed_packages(packages, files)
    vet, tested, reasons, read = set(test_only), set(), {}, set()
    for name, package in packages.items():
        has_tests = bool(package.get('TestGoFiles') or package.get('XTestGoFiles'))
        # vet 检查生产代码会被改动波及的包；测试闭包另含测试导入。
        if {name, *(package.get('Deps') or [])} & production.keys():
            vet.add(name)
        if not has_tests:
            continue
        hits = sorted(test_closure(package, packages) & production.keys())
        roots = test_read_paths(source, package)
        reads = sorted(path for path in files if any(under(path, root) for root in roots))
        read.update(reads)
        if hits:
            reasons[name] = summary('links changed ', hits)
        elif name in test_only:
            reasons[name] = summary('test files ', test_only[name])
        elif reads:
            reasons[name] = summary('test reads ', reads)
        else:
            continue
        tested.add(name)
        vet.add(name)
    return {'changed': sorted(production.keys() | test_only.keys()), 'vet': sorted(vet), 'tested': sorted(tested),
            'tested_dirs': sorted({packages[name]['Dir'] for name in tested}),
            'vet_modules': {module: sorted(name for name in vet if packages[name]['module'] == module)
                            for module in MODULES if any(packages[name]['module'] == module for name in vet)},
            'reasons': {name: reasons[name] for name in sorted(tested)},
            'owned_files': sorted({path for paths in (*production.values(), *test_only.values()) for path in paths} | read)}


def select_frontend(source: Path, files: list[str]) -> dict:
    """Vitest 用导入图（vitest related）；lint/prettier 只查改动文件；全局配置改动则整层运行。"""
    front = [path for path in files if path.startswith('frontend/') and not path.startswith('frontend/embed-dist/')]
    present = [path for path in front if (source / path).is_file()]
    code = [path for path in present if SCRIPT_CODE.search(path)]
    return {
        'vitest': 'all' if VITEST_GLOBAL & set(front) else [path for path in present if path.startswith(('frontend/src/', 'frontend/tests/'))],
        'typecheck': any(SCRIPT_CODE.search(path) or path in FRONTEND_GLOBAL for path in front),
        'lint': 'all' if LINT_GLOBAL & set(front) else code,
        'format': 'all' if FORMAT_GLOBAL & set(front) else present,
        'build': any(path in FRONTEND_GLOBAL or path.startswith(BUILD_INPUTS) for path in files),
    }


def helper_dependents(source: Path, changed: str) -> set[str]:
    """frontend/tests 下辅助模块的反向导入闭包中的视觉 spec 文件名。"""
    tests = source / 'frontend/tests'
    imports: dict[str, set[str]] = {}
    for path in tests.rglob('*.ts'):
        relative = path.relative_to(source).as_posix()
        targets = set()
        for spec in re.findall(r"""from\s+['"](\.{1,2}/[^'"]+)['"]""", path.read_text(encoding='utf-8')):
            target = (path.parent / spec).resolve()
            for candidate in (target, target.with_name(target.name + '.ts'), target / 'index.ts'):
                if candidate.is_file():
                    targets.add(candidate.relative_to(source.resolve()).as_posix())
        imports[relative] = targets
    reached, frontier = {changed}, [changed]
    while frontier:
        current = frontier.pop()
        for importer, targets in imports.items():
            if current in targets and importer not in reached:
                reached.add(importer)
                frontier.append(importer)
    return {PurePosixPath(path).name for path in reached
            if path.startswith('frontend/tests/visual/') and path.endswith('.spec.ts')}


def select_visual(source: Path, files: list[str], impact: dict | None) -> dict:
    """视觉 spec 通过 URL 加载页面，导入图无法关联 src；用此前运行记录的影响图选择。

    影响图按检出各一份，由点名或整层运行逐步建立。没有影响记录的 spec 和关联不到任何 spec 的改动
    只报告，不自动运行：它们需要按行为点名，自动全选会把一次缓存缺失变成整层运行。
    """
    specs = {path.name for path in (source / 'frontend/tests/visual').glob('*.spec.ts')}
    global_hits = sorted(VISUAL_GLOBAL & set(files))
    if global_hits:
        return {'mode': 'all', 'files': sorted(specs), 'reasons': {'*': 'global: ' + ', '.join(global_hits)},
                'unlinked': [], 'unrecorded_specs': []}
    selected: dict[str, str] = {}
    unlinked: list[str] = []
    for path in files:
        name = PurePosixPath(path).name
        if path.startswith('frontend/tests/visual/') and name in specs:
            selected.setdefault(name, 'spec changed')
        elif path.startswith('frontend/tests/') and not path.startswith('frontend/tests/local/'):
            # 删除的辅助文件找不到依赖方，和没有依赖方的文件一样交给计划处置。
            dependents = helper_dependents(source, path) if (source / path).is_file() else set()
            for spec in dependents:
                selected.setdefault(spec, 'imports ' + path)
            if not dependents:
                # 例如 fixtures/native/*.json：native-core-fixture.ts 用 readFileSync 读取，导入闭包看不到。
                unlinked.append(path)
    # 影响图里已删除 spec 的记录不算覆盖。
    mapped = {spec: modules for spec, modules in (impact or {}).get('specs', {}).items() if spec in specs}
    recorded = {module for modules in mapped.values() for module in modules}
    # Vitest 用例和 __tests__ 下的辅助文件不被页面加载，由 Vitest 层负责。
    app_changes = [path for path in files if path.startswith(('frontend/src/', 'frontend/public/'))
                   and '/__tests__/' not in path and not re.search(r'\.(spec|test)\.ts$', path)]
    for spec in sorted(mapped):
        hits = sorted(set(app_changes) & set(mapped[spec]))
        if hits:
            selected.setdefault(spec, 'loads ' + ', '.join(hits[:3]) + (' …' if len(hits) > 3 else ''))
    unlinked += [path for path in app_changes if path not in recorded]
    mode = 'all' if specs and set(selected) == specs else 'subset'
    return {'mode': mode, 'files': sorted(selected), 'reasons': dict(sorted(selected.items())),
            'unlinked': sorted(unlinked), 'unrecorded_specs': sorted(specs - mapped.keys()) if app_changes else []}


def build_impact(playwright: dict, request_log: str, specs) -> dict:
    """按测试时间窗把 Vite 返回的 /src 模块归到 spec；ui-contracts 固定单 worker 串行执行。"""
    requests = []
    for line in request_log.splitlines():
        stamp, _, url = line.partition(' ')
        path = url.split('?', 1)[0]
        if stamp.isdigit() and path.startswith(('/src/', '/public/')):
            requests.append((int(stamp), 'frontend' + path))
    requests.sort()
    mapped: dict[str, set[str]] = {}
    for spec in specs:
        name = PurePosixPath(spec['file']).name
        bucket = mapped.setdefault(name, set())
        for test in spec.get('tests', []):
            for result in test.get('results', []):
                start = iso_ms(result['startTime'])
                end = start + int(result.get('duration', 0))
                bucket.update(path for stamp, path in requests if start <= stamp <= end)
    if not requests or not any(mapped.values()):
        raise RuntimeError('视觉影响图为空：Vite 请求日志未记录 /src 模块')
    return {'schema_version': 1, 'specs': {name: sorted(paths) for name, paths in sorted(mapped.items())}}


def iso_ms(value: str) -> int:
    from datetime import datetime
    return int(datetime.fromisoformat(value.replace('Z', '+00:00')).timestamp() * 1000)


def load_impact(path: Path) -> dict | None:
    if not path.is_file():
        return None
    data = json.loads(path.read_text())
    if data.get('schema_version') != 1 or not isinstance(data.get('specs'), dict):
        raise RuntimeError(f'视觉影响图格式无效：{path}')
    return data


# 工作区的候选镜像构建器复用 candidate.py 的源码与二进制身份校验。
WORKSPACE_GATE = '工作区 infra（候选镜像构建合同）'
# 改动这些路径时 affected 不覆盖对应门槛，报告提示需要单独运行的档位。
EXTERNAL_LAYERS = (
    ('scripts/testing/contracts/', 'routing 或 candidate（候选合同）'),
    ('scripts/testing/candidate.py', 'routing 或 candidate（候选合同）'),
    ('scripts/testing/browser.py', 'browser'),
    ('frontend/tests/local/', 'browser'),
    ('scripts/testing/diagnostics.py', 'diagnostics'),
    ('scripts/testing/native_', 'known_upstream_*'),
    ('scripts/testing/known_upstream_', 'known_upstream_*'),
    ('scripts/testing/fixtures/known_upstream_', 'known_upstream_*'),
    ('docs/upstream/NATIVE_B388_', 'known_upstream_restored'),
    ('Dockerfile', 'candidate'),
    ('scripts/testing/candidate.py', WORKSPACE_GATE),
)


def frozen_inputs(source: Path) -> dict[str, str]:
    """冻结诊断绑定的源码与夹具文件 → 需单独重跑的档位。改这些文件后哈希要更新，诊断也要重跑。"""
    testing, inputs = source / 'scripts/testing', {}
    for manifest, layer, keys in (
            ('known_upstream_restored.json', 'known_upstream_restored', ()),
            ('known_upstream_race.json', 'known_upstream_race', ('fixtures', 'local_fixture_functions')),
            ('known_upstream_logger_race.json', 'known_upstream_logger_race',
             ('local_fixture_files', 'fixture_functions', 'local_fixture_functions', 'current_functions'))):
        path = testing / manifest
        if not path.is_file():
            continue
        data = json.loads(path.read_text())
        names = [name for case in data.get('cases', []) for name in case['fixtures']]
        names += [key.split(':', 1)[0] for field in keys for key in data.get(field, {})]
        inputs.update({name: layer for name in names})
    return inputs


PYTHON_EXTRA = ('scripts/refresh-core-source-delta.py',)  # 由 test_registry.py 覆盖


def is_documentation(path: str) -> bool:
    # docs/ 下的 JSON 等文件可能被程序读取（例如来源清单），不算文档。
    return path.endswith('.md')


def plan(source: Path, files: list[str], packages: dict[str, dict], impact: dict | None) -> dict:
    go = select_go(source, packages, files)
    frozen = frozen_inputs(source)
    visual = select_visual(source, files, impact)
    # runner 的代码和数据（清单、夹具、合同）都在 scripts/testing/，由 Python 单测和 registry 覆盖。
    python = [path for path in files if path.startswith('scripts/testing/') or path in PYTHON_EXTRA]
    external = [(path, layer) for path in files for prefix, layer in EXTERNAL_LAYERS if path.startswith(prefix)]
    notes = {layer for _, layer in external} | {frozen[path] + '（冻结夹具或源码改动）' for path in files if path in frozen}
    if visual['unlinked']:
        notes.add(f'视觉：{len(visual["unlinked"])} 个前端改动没有关联到任何 spec，需按行为点名')
    if visual['unrecorded_specs']:
        notes.add(f'视觉：{len(visual["unrecorded_specs"])} 个 spec 没有影响记录，未自动选中')
    covered = set(go['owned_files']) | set(python) | {path for path, _ in external} | frozen.keys()
    unclassified = sorted(path for path in files if path not in covered and not path.startswith('frontend/')
                          and not is_documentation(path))
    if unclassified:
        notes.add(f'未触发任何层：{len(unclassified)} 个文件')
    from release_scope import routing_workload_inputs
    return {'files': files, 'go': go, 'routing_workload_inputs': routing_workload_inputs(files),
            'frontend': select_frontend(source, files), 'visual': visual,
            'python': bool(python), 'unclassified': unclassified, 'not_covered': sorted(notes)}


def targeted(source: Path, packages: dict[str, dict], request: dict, database_tests: set[str],
             race_packages: set[str]) -> dict:
    """把点名的测试整理成与 plan() 相同形状的选择；名称不存在时报错，不静默缩小范围。"""
    by_directory = {package['Dir']: name for name, package in packages.items()
                    if package.get('TestGoFiles') or package.get('XTestGoFiles')}
    tested, patterns = [], {}
    for value in request['go']:
        directory, _, pattern = value.partition(':')
        directory = directory.removeprefix('./').rstrip('/') or '.'
        if directory not in by_directory:
            raise ValueError(f'--go {directory}：不是含测试的 Go 包目录')
        name = by_directory[directory]
        if name not in tested:
            tested.append(name)
        if pattern:
            try:
                re.compile(pattern)
            except re.error as error:
                raise ValueError(f'--go {value}：正则无效（{error}）') from None
            if name in patterns:
                raise ValueError(f'--go {directory}：同一个包只能给一个正则，用 | 合并')
            patterns[name] = pattern
    for flag, values, known in (('--db', request['db'], database_tests), ('--race', request['race'], race_packages)):
        unknown = sorted(set(values) - known)
        if unknown:
            raise ValueError(f'{flag} 未登记：{", ".join(unknown)}；可选：{", ".join(sorted(known))}')
    for flag in ('vitest', 'lint'):
        missing = [path for path in request[flag]
                   if not path.startswith('frontend/') or not (source / path).is_file()]
        if missing:
            raise ValueError(f'--{flag} 需要相对 Ren2Hub 根目录、位于 frontend/ 下的现有文件：{", ".join(missing)}')
    specs = sorted({PurePosixPath(value).name for value in request['visual']})
    missing = [name for name in specs if not (source / 'frontend/tests/visual' / name).is_file()]
    if missing:
        raise ValueError('--visual 不存在：' + ', '.join(missing))
    lint = sorted(set(request['lint']))
    return {
        'go': {'tested': sorted(tested), 'tested_dirs': sorted(packages[name]['Dir'] for name in tested),
               'vet_modules': {module: sorted(name for name in tested if packages[name]['module'] == module)
                               for module in MODULES if any(packages[name]['module'] == module for name in tested)},
               'patterns': patterns},
        'databases': {'packages': None, 'tests': sorted(set(request['db']))} if request['db'] else None,
        'race': sorted(set(request['race'])),
        'frontend': {'vitest': sorted(set(request['vitest'])), 'typecheck': bool(lint),
                     'lint': [path for path in lint if SCRIPT_CODE.search(path)], 'format': lint, 'build': False},
        'visual': {'mode': 'subset', 'files': specs},
        'python': bool(request['python']), 'not_covered': [], 'named': True}
