"""登记检查：冻结清单、数据库矩阵和 race 选择是否仍与工作树一致。

只读源码，不编译也不启动容器，数秒完成。改名或修改测试后先跑这一层，避免在数据库矩阵、
race 或候选运行到一半才发现登记没有同步。逐项列出偏离的文件和修正办法。
"""
from __future__ import annotations

import hashlib
import json
from pathlib import Path
import re
import subprocess
import sys

STAGE = 'registry'
DELTA = 'scripts/refresh-core-source-delta.py'


def restored_fixtures(root: Path) -> list[str]:
    """known_upstream_restored 各项绑定的夹具文件哈希；列出具体文件，而不是只报清单失效。"""
    import native_restored
    manifest = json.loads((root / native_restored.MANIFEST).read_text())
    stale = {}
    for case in manifest['cases']:
        for name, expected in case['fixtures'].items():
            path = root / name
            actual = native_restored.digest(path) if path.is_file() else None
            if actual != expected:
                stale[name] = '文件缺失' if actual is None else '内容已改变'
    if stale:
        return [f'{name}：{reason}' for name, reason in sorted(stale.items())]
    try:
        native_restored.load_manifest(root)
    except native_restored.InvalidEvidence as error:
        return [f'{native_restored.MANIFEST}：{error}']
    return []


def logger_race(root: Path) -> list[str]:
    import native_logger_race as logger
    from native_restored import InvalidEvidence
    data = json.loads((root / logger.MANIFEST).read_text())
    stale = []
    for name, expected in {'logger/logger.go': data['logger_sha256'], **data['local_fixture_files']}.items():
        path = root / name
        if not path.is_file() or hashlib.sha256(path.read_bytes()).hexdigest() != expected:
            stale.append(f'{name}：内容已改变或缺失')
    if stale:
        return stale
    try:
        logger.load_manifest(root)
    except InvalidEvidence as error:
        return [f'{logger.MANIFEST}：{error}']
    return []


def pricing_race(root: Path) -> list[str]:
    import native_pricing_race as pricing
    from native_restored import InvalidEvidence
    try:
        pricing.load_manifest(root)
    except InvalidEvidence as error:
        return [f'{pricing.MANIFEST}：{error}']
    return []


def race_selection(root: Path) -> list[str]:
    """固定选择的哈希、委托关系，以及每个被选测试仍在所属包的测试源码中。

    这是源码文本扫描，用来尽早发现改名和删除；race 层自己用 `go test -list` 核对实际清单，以它为准。
    """
    import ordinary_race
    from native_restored import InvalidEvidence
    try:
        selection, _ = ordinary_race.load_selection(root)
    except InvalidEvidence as error:
        return [f'{ordinary_race.SELECTION}：{error}']
    missing = []
    for group in selection['groups']:
        declared = set()
        for path in (root / group['package']).glob('*_test.go'):
            declared.update(re.findall(r'^func\s+(Test\w+)\s*\(', path.read_text(encoding='utf-8'), re.M))
        missing.extend(f'{group["package"]}/{name}：测试已不存在（组 {group["id"]}）'
                       for name in group['names'] if name not in declared)
    return missing


def database_registration(root: Path) -> list[str]:
    import database
    inventory = database.database_inventory(root, database.database_test_files(root))
    problems = [f'{item["package"]}/{item["test"]}：读取数据库 DSN 但未登记'
                for item in inventory['unclassified']]
    problems += [f'{name}：已登记但测试不存在' for name in inventory['missing_required']]
    problems += [f'{name}：已登记为原生恢复诊断但测试不存在' for name in inventory['missing_restored_diagnostics']]
    return problems


def source_delta(root: Path) -> list[str]:
    """核心来源清单的本地哈希；范围规则只在刷新脚本里定义，这里调用它的只读模式。"""
    result = subprocess.run([sys.executable, '-B', DELTA, '--check-local'], cwd=root,
                            capture_output=True, text=True)
    if result.returncode == 0:
        return []
    stale = [line for line in result.stderr.splitlines() if line.strip()]
    return stale or [f'{DELTA} --check-local 退出 {result.returncode}']


CHECKS = (
    ('restored_fixtures', restored_fixtures,
     '夹具确需改变时，更新 known_upstream_restored.json 中该文件的全部哈希，再运行 run.py known_upstream_restored 确认冻结断言照原样复现'),
    ('logger_race', logger_race,
     '日志计数竞争的冻结对象已改变；按 LOCAL_TESTING.md 重新取得对照后更新清单，再运行 run.py known_upstream_logger_race'),
    ('pricing_race', pricing_race,
     '定价竞争的冻结对象已改变；更新清单后运行 run.py known_upstream_race'),
    ('race_selection', race_selection,
     '在 race_selection.json 同步改名或删除，并更新 ordinary_race.py 的 FROZEN_SELECTION'),
    ('database_registration', database_registration,
     '在 scripts/testing/database.py 的矩阵清单登记新测试，或删除已不存在的登记'),
    ('source_delta', source_delta,
     '运行 scripts/refresh-core-source-delta.py --upstream-repo <NewAPI 参考仓库> 刷新 docs/upstream/CORE_SOURCE_DELTA.json'),
)
# 迭代中反复改 Go 文件时，来源清单在提交前刷新一次即可；这一项在迭代档位只提示，不阻断。
COMMIT_ONLY = ('source_delta',)


def run(root: Path, iteration: bool = False) -> dict:
    """iteration 为 True 时用于 affected/targeted：COMMIT_ONLY 的偏离记为提交前待办。"""
    root = root.resolve()
    checks, pending, failed = {}, [], False
    for name, check, fix in CHECKS:
        problems = check(root)
        deferred = iteration and name in COMMIT_ONLY
        checks[name] = {'problems': problems, 'fix': fix if problems else None, 'blocking': bool(problems) and not deferred}
        if not problems:
            continue
        label = '提交前待办' if deferred else '登记不一致'
        print(f'{label} [{name}]：{fix}', flush=True)
        for problem in problems[:20]:
            print('  ' + problem, flush=True)
        if len(problems) > 20:
            print(f'  …共 {len(problems)} 项', flush=True)
        if deferred:
            pending.append(name)
        else:
            failed = True
    return {'success': not failed, 'checks': checks, 'pending_before_commit': pending}
