"""按改动模块确定候选范围。候选与工作区发布校验器加载同一份规则，最低要求只在这里定义。"""
from __future__ import annotations

import re

# 可单独选择的候选部分及其阶段，顺序即执行顺序。routing 与冻结诊断只属于完整候选或各自的独立档位。
# 阶段名与 candidate.py 的 STAGES/UPGRADE_STAGES 一致（test_release_scope.py 核对）；candidate.py 会被
# 发布校验器按文件单独加载，不能在模块顶层导入本文件。
PART_STAGES = {
    'upgrade': ('upgrade-baseline', 'upgrade-management', 'upgrade-before-seconds',
                'upgrade-prepare-p', 'upgrade-resume-p', 'upgrade-after-seconds', 'upgrade-after-p'),
    'billing': ('billing',),
    'websocket': ('ws',),
    'compact': ('compact',),
    'plugin': ('plugin-before', 'plugin-after'),
    'management': ('upstream_sync',),
    'sdk': ('sdk',),
}
PARTS = tuple(PART_STAGES)
# plugin 阶段复用 billing 初始化的实例凭据；升级演练在 management 阶段回读旧版写入的工单附件。
IMPLIED = {'plugin': ('billing',), 'upgrade': ('management',)}
# 这些路径决定已有数据在新版本启动时如何被读取或迁移，改动后必须从现行正式镜像做升级演练。
UPGRADE_PATHS = re.compile(r'model/(?!.*_test\.go$)[^/]+\.go|main\.go|go\.mod|go\.sum|Dockerfile')


def required_parts(files) -> list[str]:
    """部署候选的最低范围；其余部分按改动模块由执行者追加，不在这里强制。"""
    return ['upgrade'] if any(UPGRADE_PATHS.fullmatch(path) for path in files) else []


def expand(parts) -> list[str]:
    """补上依赖部分并按固定顺序返回；未知名称直接拒绝。"""
    unknown = sorted(set(parts) - set(PARTS))
    if unknown:
        raise ValueError('unknown_candidate_scope_parts: ' + ', '.join(unknown))
    selected = set(parts)
    for part in parts:
        selected.update(IMPLIED.get(part, ()))
    return [part for part in PARTS if part in selected]


def stages(parts) -> tuple[str, ...]:
    return tuple(stage for part in expand(parts) for stage in PART_STAGES[part])


def parse(value: str) -> list[str] | None:
    """--scope 的取值：full 为完整候选（None）；其余为逗号分隔的名称。

    auto 表示按与旧镜像修订的差异补上最低要求；boot 表示不选任何阶段，只核对镜像身份并在全新实例启动。
    """
    names = [name.strip() for name in value.split(',') if name.strip()]
    if names == ['full']:
        return None
    unknown = sorted(set(names) - set(PARTS) - {'auto', 'boot'})
    if not names or unknown:
        raise ValueError('invalid_candidate_scope: ' + (', '.join(unknown) or value))
    return names
