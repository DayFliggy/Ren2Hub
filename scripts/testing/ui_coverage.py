"""汇总 Vue 源码清单与实际截图证据；静态引用不计作运行覆盖。"""
import argparse
from collections import Counter, defaultdict, deque
from datetime import datetime, timezone
import hashlib
import json
from pathlib import Path
import re
import subprocess


HISTORY = '9a63062227c31ba54a6346e07ee8b4fd8802f22f'
IMPORT = re.compile(r'''(?:from\s*|import\s*\(\s*|import\s*)["']([^"']+)["']''')
ROUTE_PATH = re.compile(r'''\bpath\s*:\s*(["'`])([^\n]*?)\1''')


def git(repo, *args):
    return subprocess.check_output(['git', '-C', str(repo), *args], text=True).strip()


def digest(path):
    return hashlib.sha256(path.read_bytes()).hexdigest()


def is_test(path):
    return '__tests__' in path.parts or bool(re.search(r'\.(?:spec|test)\.', path.name))


def source_inventory(repo, history):
    src = repo / 'frontend/src'
    historical = {}
    for line in git(repo, 'ls-tree', '-r', history, '--', 'frontend/src').splitlines():
        header, name = line.split('\t', 1)
        historical[name.removeprefix('frontend/')] = header.split()[2]
    result = []
    for path in sorted(src.rglob('*.vue')):
        name = path.relative_to(repo / 'frontend').as_posix()
        kind = ('component' if path.is_relative_to(src / 'components') else
                'view' if path.is_relative_to(src / 'views') else
                'app' if path == src / 'App.vue' else 'feature-component')
        content = path.read_bytes()
        blob = hashlib.sha1(f'blob {len(content)}\0'.encode() + content).hexdigest()
        previous = historical.get(name)
        result.append({'path': name, 'kind': kind,
                       'history': 'added' if previous is None else 'same' if previous == blob else 'modified',
                       'sha256': hashlib.sha256(content).hexdigest()})
    return result


def resolve_import(frontend, source, target):
    if target.startswith('@/'):
        candidate = frontend / 'src' / target[2:]
    elif target.startswith('.'):
        candidate = source.parent / target
    else:
        return None
    for path in (candidate, candidate.with_suffix('.ts'), candidate.with_suffix('.vue'), candidate / 'index.ts'):
        if path.is_file() and path.resolve().is_relative_to(frontend.resolve()):
            return path.resolve().relative_to(frontend.resolve()).as_posix()
    return None


def object_spans(text):
    """定位路由对象的括号，跳过字符串与注释，避免关联相邻的重定向对象。"""
    stack, spans, index = [], [], 0
    while index < len(text):
        char = text[index]
        if char in "'\"`":
            quote, index = char, index + 1
            while index < len(text):
                if text[index] == '\\':
                    index += 2
                elif text[index] == quote:
                    break
                else:
                    index += 1
        elif text.startswith('//', index):
            end = text.find('\n', index)
            index = len(text) if end == -1 else end
        elif text.startswith('/*', index):
            end = text.find('*/', index + 2)
            index = len(text) if end == -1 else end + 1
        elif char == '{':
            stack.append(index)
        elif char == '}' and stack:
            spans.append((stack.pop(), index + 1))
        index += 1
    return spans


def enclosing(spans, index):
    return min((span for span in spans if span[0] < index < span[1]),
               key=lambda span: span[1] - span[0], default=None)


def route_policy_references(frontend, path, text, spans, route_span, inherited=False):
    """记录真实 meta/guard 的源码；不重建授权规则或执行 TypeScript。"""
    references = []
    for match in re.finditer(r'\b(meta|beforeEnter)\s*:\s*', text):
        if enclosing(spans, match.start()) != route_span:
            continue
        start = match.end()
        value_span = next((span for span in spans if span[0] == start), None)
        end = value_span[1] if value_span else text.find('\n', start)
        if end < 0:
            end = len(text)
        references.append({'source': path.relative_to(frontend).as_posix(),
                           'line': text[:match.start()].count('\n') + 1,
                           'kind': match.group(1), 'inherited': inherited,
                           'expression': text[start:end].strip().rstrip(',')})
    return references


def route_links(frontend, path, text):
    aliases = dict(re.findall(r'''import\s+(\w+)\s+from\s+["']([^"']+\.vue)["']''', text))
    spans = object_spans(text)
    paths = {enclosing(spans, m.start()): m for m in ROUTE_PATH.finditer(text)}
    component_pattern = r'''\bcomponent\s*:\s*(?:\(\)\s*=>\s*import\(["']([^"']+)["']\)|(\w+))'''
    for match in re.finditer(component_pattern, text):
        span = enclosing(spans, match.start())
        route = paths.get(span)
        if route is None:
            continue
        target = resolve_import(frontend, path, match.group(1) or aliases.get(match.group(2), ''))
        if not target:
            continue
        route_path = route.group(2)
        parents = sorted((s for s in paths if s and span and s[0] < span[0] and s[1] > span[1]),
                         key=lambda s: s[1] - s[0])
        for parent in parents:
            if route_path.startswith('/'):
                break
            route_path = paths[parent].group(2).rstrip('/') + '/' + route_path
        policies = route_policy_references(frontend, path, text, spans, span)
        for parent in parents:
            policies.extend(route_policy_references(frontend, path, text, spans, parent, True))
        yield target, {'path': route_path, 'source': path.relative_to(frontend).as_posix(),
                       'line': text[:route.start()].count('\n') + 1,
                       'dynamic': '${' in route_path, 'policy_references': policies}


def static_links(repo):
    frontend = repo / 'frontend'
    importers = defaultdict(set)
    tests = defaultdict(set)
    routes = defaultdict(list)
    for path in sorted((frontend / 'src').rglob('*')):
        if not path.is_file() or path.suffix not in ('.ts', '.tsx', '.vue', '.js'):
            continue
        text = path.read_text()
        name = path.relative_to(frontend).as_posix()
        for match in IMPORT.finditer(text):
            target = resolve_import(frontend, path, match.group(1))
            if target:
                (tests if is_test(path) else importers)[target].add(name)
        if path.parent != frontend / 'src/router':
            continue
        for target, route in route_links(frontend, path, text):
            routes[target].append(route)
    # Imported route arrays inherit their actual mounting record's meta. The
    # reference remains source text, including permission-helper spreads.
    for path in sorted((frontend / 'src/router').glob('*.ts')):
        text = path.read_text()
        spans = object_spans(text)
        path_spans = {enclosing(spans, match.start()) for match in ROUTE_PATH.finditer(text)}
        for match in re.finditer(r'''import\s*\{\s*(\w+)\s*\}\s*from\s*["']([^"']+)["']''', text):
            target = resolve_import(frontend, path, match.group(2))
            if not target:
                continue
            inherited = []
            for spread in re.finditer(r'\.\.\.' + re.escape(match.group(1)) + r'\b', text):
                for span in path_spans:
                    if span and span[0] < spread.start() < span[1]:
                        inherited.extend(route_policy_references(frontend, path, text, spans, span, True))
            for candidates in routes.values():
                for route in candidates:
                    if route['source'] == target:
                        route['policy_references'].extend(inherited)
    return importers, tests, routes


def reachable_views(name, importers, routes):
    """反向静态可达性只用于找场景，不能证明条件分支已经渲染。"""
    queue, visited, found = deque([name]), set(), []
    while queue:
        current = queue.popleft()
        if current in visited:
            continue
        visited.add(current)
        if current == 'src/App.vue':
            found.append({'path': '<app-lifecycle>', 'source': current, 'line': 1,
                          'dynamic': True, 'component': current})
            continue
        found.extend({**route, 'component': current} for route in routes.get(current, []))
        if current in routes:
            continue
        queue.extend(sorted(importers.get(current, [])))
    unique = {(r['source'], r['line'], r['path'], r['component']): r for r in found}
    return sorted(unique.values(), key=lambda item: (item['path'], item['component']))


def specs(report):
    for spec in report.get('specs', []):
        yield spec
    for suite in report.get('suites', []):
        yield from specs(suite)


def case_status(spec):
    tests = spec.get('tests', [])
    if not tests:
        return 'unknown'
    clean = all(t.get('expectedStatus') == 'passed' and t.get('status') == 'expected'
                and len(t.get('results', [])) == 1
                and t['results'][0].get('status') == 'passed'
                and t['results'][0].get('retry', 0) == 0 for t in tests)
    return 'passed' if clean else 'not-passed'


def read_evidence(roots):
    result, issues, reports = [], [], {}
    for root in roots:
        for path in sorted(root.rglob('*.coverage.json')):
            if not path.is_file():
                continue
            try:
                data = json.loads(path.read_text())
                if not isinstance(data.get('visible'), list) or not isinstance(data.get('mounted'), list):
                    raise ValueError('visible/mounted 必须为数组')
                if any(not isinstance(p, str) for p in data['visible'] + data['mounted']):
                    raise ValueError('组件路径必须为字符串')
                if not set(data['visible']).issubset(data['mounted']):
                    raise ValueError('visible 必须属于 mounted')
                hashes = data.get('sourceHashes', {})
                if not isinstance(hashes, dict) or any(not isinstance(value, str) or not re.fullmatch(r'[0-9a-f]{64}', value)
                                                       for value in hashes.values()):
                    raise ValueError('sourceHashes 必须为路径到 SHA256 的映射')
            except (OSError, ValueError, TypeError) as error:
                issues.append({'file': str(path), 'error': str(error)})
                continue
            report_path = next((p / 'playwright.json' for p in path.parents
                                if p.is_relative_to(root) and (p / 'playwright.json').is_file()), None)
            status, match_file = 'unknown', None
            if report_path:
                if report_path not in reports:
                    try:
                        reports[report_path] = list(specs(json.loads(report_path.read_text())))
                    except (OSError, ValueError) as error:
                        issues.append({'file': str(report_path), 'error': str(error)})
                        reports[report_path] = []
                candidates = [s for s in reports[report_path]
                              if (s.get('id') == data['testId'] if data.get('testId') else s.get('title') == data.get('title'))]
                if len(candidates) == 1:
                    status, match_file = case_status(candidates[0]), candidates[0].get('file')
                else:
                    issues.append({'file': str(path), 'error': f'匹配到 {len(candidates)} 个测试，状态保持 unknown'})
            screenshot_names = data.get('screenshots', [path.name.removesuffix('.coverage.json') + '.png'])
            if (not isinstance(screenshot_names, list) or not screenshot_names
                    or any(not isinstance(name, str) or Path(name).name != name for name in screenshot_names)):
                issues.append({'file': str(path), 'error': 'screenshots 必须为同目录的非空文件名数组'})
                continue
            screenshots = [{'file': str(path.with_name(name).resolve()),
                            'sha256': digest(path.with_name(name)) if path.with_name(name).is_file() else None}
                           for name in screenshot_names]
            png = path.with_name(screenshot_names[0])
            result.append({**data, 'id': hashlib.sha256(str(path.resolve()).encode()).hexdigest()[:16],
                           'file': str(path.resolve()), 'test_file': match_file, 'test_status': status,
                           'screenshot': str(png.resolve()) if png.is_file() else None,
                           'screenshot_sha256': digest(png) if png.is_file() else None,
                           'screenshots': screenshots,
                           'source_hashes': data.get('sourceHashes', {}),
                           'source_identity': data.get('sourceIdentity')})
    unique = {entry['file']: entry for entry in result}
    return list(unique.values()), issues


def apply_review(repo, items, evidence, review):
    """人工差异判断与角色来源独立输入；缺少或过时的判断不自动通过。"""
    context_by_id = review.get('evidence_contexts', {})
    for entry in evidence:
        context = context_by_id.get(entry['id'])
        if context:
            references = context.get('source_references', [])
            fresh = bool(references) and all(
                (repo / 'frontend' / ref['source']).is_file()
                and digest(repo / 'frontend' / ref['source']) == ref.get('sha256')
                for ref in references)
            entry['test_context'] = {**context, 'source_status': 'current' if fresh else 'unverified-source'}
        else:
            entry['test_context'] = {'role': 'unreviewed', 'basis': '未从测试夹具确认，不能按 URL 猜测身份'}
    indexed = {entry['id']: entry for entry in evidence}
    for item in items:
        disposition = review.get('items', {}).get(item['path'])
        if disposition:
            item['disposition'] = {**disposition,
                                   'source_status': 'current' if disposition.get('sha256') == item['sha256'] else 'unverified-source'}
        elif item['history'] == 'same':
            item['disposition'] = {'classification': ['无源差异'], 'source_status': 'current',
                                   'note': 'SFC 与历史内容相同；共享样式、父结构和数据仍可能改变视觉。'}
        else:
            item['disposition'] = {'classification': ['待验证'], 'source_status': 'unreviewed',
                                   'note': '尚未附逐文件差异判断，不能接受整页变化。'}
        item['observed_roles'] = sorted({str(indexed[key]['test_context']['role'])
                                        for key in item['current_visible_evidence']
                                        if indexed[key]['test_context'].get('source_status') == 'current'})
        item['authorization_result'] = '可见覆盖不代表角色授权通过；须引用独立权限测试。'


def build_report(repo, history, roots, review=None):
    items = source_inventory(repo, history)
    importers, unit_tests, routes = static_links(repo)
    evidence, issues = read_evidence(roots)
    for item in items:
        name = item['path']
        visible = [entry for entry in evidence if name in entry['visible']]
        mounted = [entry for entry in evidence if name in entry['mounted']]
        passed = [entry for entry in visible if entry['test_status'] == 'passed' and entry['screenshot']]
        current = [entry for entry in passed if entry['source_hashes'].get(name) == item['sha256']
                   and all(image['sha256'] for image in entry['screenshots'])]
        item.update({'browser': 'visible-passed' if passed else 'visible-unverified' if visible else 'mounted-only' if mounted else 'missing',
                     'current_browser': 'visible-current' if current else 'visible-unverified-source' if passed else 'missing',
                     'current_visible_evidence': [entry['id'] for entry in current],
                     'visible_evidence': [entry['id'] for entry in visible],
                     'mounted_evidence': [entry['id'] for entry in mounted],
                     'unit_test_references': sorted(unit_tests.get(name, [])),
                     'direct_importers': sorted(importers.get(name, [])),
                     'route_candidates': reachable_views(name, importers, routes),
                     'observed_variants': [json.loads(value) for value in sorted({json.dumps(
                         {'theme': entry.get('theme'), 'locale': entry.get('locale'), 'viewport': entry.get('viewport')}, sort_keys=True)
                         for entry in current})]})
        item['next_scene'] = ('covered-state-needs-matrix-review' if passed else
                              'open-interaction' if re.search(r'Modal|Drawer|Dialog|Menu|Popover|Tooltip|Sheet', Path(name).stem) else
                              'route-state' if item['route_candidates'] else 'no-static-route')
    apply_review(repo, items, evidence, review or {})
    fingerprint = hashlib.sha256(json.dumps([(i['path'], i['sha256']) for i in items]).encode()).hexdigest()
    return {'schema_version': 2, 'generated_at': datetime.now(timezone.utc).isoformat(),
            'source': {'repo': str(repo), 'base_sha': git(repo, 'rev-parse', 'HEAD'),
                       'history_sha': git(repo, 'rev-parse', history), 'candidate_sha': None,
                       'vue_content_sha256': fingerprint},
            'evidence_roots': [str(root) for root in roots],
            'summary': {'files': len(items), 'original_components_and_views': sum(i['kind'] in ('component', 'view') for i in items),
                        'kinds': dict(Counter(i['kind'] for i in items)),
                        'history': dict(Counter(i['history'] for i in items)),
                        'browser': dict(Counter(i['browser'] for i in items)),
                        'current_browser': dict(Counter(i['current_browser'] for i in items)),
                        'evidence': len(evidence), 'evidence_issues': len(issues)},
            'limitations': ['visible 只表示截图时存在可见 DOM，不证明全部内部状态、权限或矩阵已通过。',
                           'mounted-only 不计为可见覆盖；静态导入、路由和单测引用不计为浏览器覆盖。',
                           '静态路由候选含动态模板；实际路由以截图 URL 和运行时路由表为准。',
                           'current_browser 仅校验 captureUI 记录的逐 Vue 文件摘要；不代替测试时完整资源与配置身份。无摘要的旧证据保持源码未确认。',
                           '单测这里只记录直接引用关系，不读取为单测通过结论。',
                           '角色来自经人工核对的夹具源码，不是浏览器身份遥测；Root 可见不代表普通用户授权通过。',
                           'route policy_references 仅引用当前授权源码，不求值、不复制权限规则；跨文件或动态表达式仍由原路由守卫解释。',
                           '截图创建与像素比较是不同结果；本清单只统计实际可见截图，不将创建基线记为像素比较通过。'],
            'review_sources': (review or {}).get('sources', []),
            'issues': issues, 'items': items, 'evidence': evidence}


def markdown(report):
    summary = report['summary']
    lines = ['# Vue 页面与组件覆盖清单', '', f"生成时间：{report['generated_at']}", '',
             f"历史：`{report['source']['history_sha']}`；当前 base：`{report['source']['base_sha']}`；candidate_sha：`null`。", '',
             f"共 {summary['files']} 个 Vue 文件；历史差异 {json.dumps(summary['history'], ensure_ascii=False)}；浏览器状态 {json.dumps(summary['browser'], ensure_ascii=False)}。", '',
             *[f'- {text}' for text in report['limitations']], '',
             '| 文件 | 历史状态 | 浏览器／当前源码状态 | 场景入口候选 | 权限源码引用 | 已证实夹具角色 | 差异处置 |',
             '| --- | --- | --- | --- | --- | --- | --- |']
    for item in report['items']:
        paths = sorted({r['path'] for r in item['route_candidates']})
        disposition = item['disposition']
        note = disposition['note'].replace('|', '\\|')
        policies = sorted({f"{ref['source']}:{ref['line']}" for route in item['route_candidates']
                           for ref in route.get('policy_references', [])})
        policies.extend(f"{ref['source']}:{ref['line']}" for ref in disposition.get('component_guard_references', []))
        policy_text = '、'.join(f'`{value}`' for value in policies[:4])
        if len(policies) > 4:
            policy_text += f'；完整 {len(policies)} 处见本行对应 JSON'
        lines.append(f"| `{item['path']}` | {item['history']} | {item['browser']} / {item['current_browser']} | {'、'.join(paths) or '未解析到静态路由'} | {policy_text or '仅应用生命周期／无产品入口；详见 JSON'} | {'、'.join(item['observed_roles']) or '未确认'} | {'、'.join(disposition['classification'])}；{note}（{disposition['source_status']}） |")
    lines.extend(['', '## 实际浏览器证据', '', '| 证据 | 用例状态 | URL | 截图 |', '| --- | --- | --- | --- |'])
    for entry in report['evidence']:
        title = str(entry.get('title', '')).replace('|', '\\|')
        lines.append(f"| `{entry['id']}` {title} / {entry.get('label', '')} | {entry['test_status']} | {entry.get('url', '')} | {entry['screenshot'] or '缺少 PNG'} |")
    if report['issues']:
        lines.extend(['', '## 证据问题', ''])
        lines.extend(f"- `{item['file']}`：{item['error']}" for item in report['issues'])
    return '\n'.join(lines) + '\n'


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--repo', type=Path, default=Path(__file__).resolve().parents[2])
    parser.add_argument('--history', default=HISTORY)
    parser.add_argument('--evidence', type=Path, action='append', required=True,
                        help='只传当前测试产物目录，可重复；不要混入历史或修复前截图。')
    parser.add_argument('--output', type=Path, required=True, help='输出文件前缀，生成 .json 和 .md')
    parser.add_argument('--review', type=Path, help='逐组件差异处置和测试角色来源 JSON；缺失或摘要不匹配保持未确认')
    parser.add_argument('--require-visible', action='store_true', help='任何 Vue 文件缺少源码摘要匹配、通过用例的可见截图即失败')
    args = parser.parse_args()
    roots = [root.resolve() for root in args.evidence]
    if any(not root.is_dir() for root in roots):
        parser.error('所有证据目录必须存在')
    review = json.loads(args.review.read_text()) if args.review else None
    report = build_report(args.repo.resolve(), args.history, roots, review)
    args.output.parent.mkdir(parents=True, exist_ok=True)
    args.output.with_suffix('.json').write_text(json.dumps(report, indent=2, ensure_ascii=False) + '\n')
    args.output.with_suffix('.md').write_text(markdown(report))
    print(json.dumps(report['summary'], ensure_ascii=False))
    if report['issues'] or (args.require_visible and any(i['current_browser'] != 'visible-current' for i in report['items'])):
        raise SystemExit(1)


if __name__ == '__main__':
    main()
