"""按源码差异选择隔离候选合同。"""

import re

# 可单独选择的候选部分及其阶段，顺序即执行顺序。routing 与冻结诊断只属于完整候选或各自的独立档位。
PART_STAGES = {
    'billing': ('billing',),
    'websocket': ('ws',),
    'compact': ('compact',),
    'plugin': ('plugin-before', 'plugin-after'),
    'management': ('upstream_sync',),
    'sdk': ('sdk',),
    'routing-workload': ('routing-workload',),
}
PARTS = tuple(PART_STAGES)
# plugin 阶段复用 billing 初始化的实例凭据。
IMPLIED = {'plugin': ('billing',)}
# 不进入服务端程序、也不改变镜像运行时的路径：前端（嵌入的静态资源）、文档、脚本、仓库元数据、
# 根目录的 Markdown 和 Go 测试文件。只改这些时，新版本启动后读取、迁移和改写已有数据的方式不变。
# testdata/ 和包目录里的其他文件不在其中：go:embed 会把整棵目录嵌入程序（plugins/tasks 的每个子目录
# 都在启动时注册为插件），目录名不能说明文件不参与启动。
STARTUP_NEUTRAL = re.compile(r'(?:frontend|docs|scripts|\.github|\.agents)/.*|[^/]*\.md|.*_test\.go')


# Shared trigger for the development runner and the scoped release verifier.
ROUTING_WORKLOAD_PATHS = re.compile(
    r'(?:service/(?:route_[^/]+|channel_select|channel_affinity|token_route|concurrency|relay_error|usage_helpr)'
    r'|controller/(?:relay|relay_route_selection|routing_lifecycle)|middleware/(?:distributor|task_plugin)'
    r'|relay/responses_websocket|relay/channel/(?:openai/(?:relay-openai|relay_responses(?:_compact)?|'
    r'chat_via_responses|responses_via_chat)|claude/(?:relay-claude|relay_responses)|gemini/(?:relay-gemini(?:-native)?|'
    r'relay_responses)))(?:_test)?\.go|scripts/testing/contracts/routing(?:_[^/]+)?\.py'
)
ROUTING_WORKLOAD_TESTS = (
    'TestAutomaticRoutingWorkload', 'TestRouteQualityWindowAndRecovery', 'TestRouteRankCapacityPriceAndUnknownSeats',
    'TestRouteCacheQualityComparableSessions', 'TestRouteCacheStatefulWorkload', 'TestRouteCacheExplorationBudget',
)


def routing_workload_inputs(files):
    return sorted(path for path in files if ROUTING_WORKLOAD_PATHS.fullmatch(path))


def required_parts(files) -> list[str]:
    """服务端改动触发新库启动与管理合同，路由改动另加负载测试。"""
    files = list(files)
    required = ['management'] if any(not STARTUP_NEUTRAL.fullmatch(path) for path in files) else []
    if routing_workload_inputs(files):
        required.append('routing-workload')
    return required


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

    auto 表示按与基线提交的差异补上最低要求；boot 表示不选任何阶段，只核对镜像身份并在全新实例启动。
    """
    names = [name.strip() for name in value.split(',') if name.strip()]
    if names == ['full']:
        return None
    unknown = sorted(set(names) - set(PARTS) - {'auto', 'boot'})
    if not names or unknown:
        raise ValueError('invalid_candidate_scope: ' + (', '.join(unknown) or value))
    return names
