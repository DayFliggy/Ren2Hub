"""路由审计的结果合同；已知上游缺陷不能计为通过。"""
from typing import NamedTuple

STATUSES = ('pass', 'fail', 'known_upstream', 'blocked', 'not_run')

# 执行器与发布复核共用完整清单；本模块按路径独立加载，只依赖标准库。
class Probe(NamedTuple):
    package: str
    test: str
    file: str
    fingerprint: str
    leaves: tuple[str, ...] = ('',)
    tag: str = 'upstream_probe'

    @property
    def name(self):
        return self.package + '/' + self.test


# 完整测试名是合同；不能把支付、余额或其他 TestUpstream* 吸收入等待列表。
DIAGNOSTIC_PROBES = (
    Probe('middleware', 'TestUpstreamPluginOriginChannelStorageFailureIsPrivate500',
          'task_origin_upstream_probe_test.go', 'plugin_storage'),
    Probe('relay', 'TestUpstreamRemixPreservesInheritedRatiosUnderAdaptorEstimate',
          'task_upstream_probe_test.go', 'A7'),
    Probe('relay', 'TestUpstreamRemixResolvesVendorTaskID', 'task_upstream_probe_test.go', 'N1'),
    Probe('relay', 'TestUpstreamRemixAdaptorPreservesAction', 'task_upstream_probe_test.go', 'N3'),
    Probe('router', 'TestUpstreamNativeRemixRouteSendsUpstreamRemixWithOriginChannelContext',
          'video_remix_upstream_probe_test.go', 'N2'),
    Probe('router', 'TestUpstreamNativeRemixRouteLegacyTaskUsesPublicIDAndDataRatios',
          'video_remix_upstream_probe_test.go', 'N2', ('1', '55')),
    Probe('router', 'TestUpstreamNativeRemixRouteOnTaskPluginChannel',
          'video_remix_upstream_probe_test.go', 'N2'),
    Probe('router', 'TestUpstreamNativeRemixRouteChecksKeyModelLimitAgainstOriginModel',
          'video_remix_upstream_probe_test.go', 'A5'),
    Probe('router', 'TestUpstreamNativeRemixRouteRejectsOriginChannelOfAnotherPlatform',
          'video_remix_upstream_probe_test.go', 'N2', (
              'kling_task_on_a_channel_now_typed_openai',
              'sora_task_on_a_plugin_channel_rebound_to_kling')),
    Probe('router', 'TestUpstreamNativeRemixRouteRetryDoesNotCompoundInheritedRatios',
          'video_remix_upstream_probe_test.go', 'N2'),
    Probe('router', 'TestUpstreamMidjourneyContinuationOfMissingOrForeignTaskIsNotFoundFirst',
          'mj_origin_upstream_probe_test.go', 'mj_priority', tuple(
              owner + '/' + condition for owner in ('missing', 'foreign')
              for condition in ('empty_manual_list', 'all_candidates_full', 'model_not_allowed'))),
    Probe('router', 'TestUpstreamMidjourneyContinuationLookupFailureIsPrivate500',
          'mj_origin_upstream_probe_test.go', 'mj_storage'),
)


# Version 1: independent inventory of the approved HTTP matrix. Removing a
# runner case must leave a missing requirement, not silently shrink coverage.
REQUIRED_CASES = (
    'automatic_eligibility_priority_ratio', 'exact_normalized_model',
    'manual_order_disabled_empty_mapping_price', 'zero_weight_backup',
    'weight_distribution_audit', 'retry_order_error_and_refund',
    'automatic_health_manual_independence', 'cross_key_concurrency_and_release',
    'native_protocols_streams_and_conversion', 'responses_websocket',
    'prefer_strict_affinity', 'client_disconnect_settles_once_and_releases',
    'real_redis_cache_repair_preserves_reservation', 'manual_unavailable_no_auto_fallback',
    'route_version_ownership_edit_mode', 'nonretryable_no_replay', 'retry_after_cooldown',
    'automatic_breaker_admission', 'prefer_escapes_cooling_channel',
    'expensive_retry_insufficient_reserve', 'delivered_stream_not_replayed',
    'disconnect_retry', 'automatic_busy_hot_limit', 'automatic_nonfull_load_bucket',
    'endpoint_eligibility', 'preview_websocket_eligibility', 'preview_plugin_eligibility',
    'error_classification_and_privacy', 'sora_task_lifecycle', 'hailuo_task_lifecycle',
    'plugin_origin_pin_price_and_capacity', 'mj_continuation_pin_price',
    'upstream_native_remix_probe', 'native_remix_upstream_id', 'native_remix_plugin_action',
    'native_remix_inherited_billing', 'native_remix_platform',
    'native_remix_expression_usage', 'native_remix_unsupported_provider',
)
KNOWN_UPSTREAM = {
    'upstream_native_remix_probe': {
        'upstream': ['QuantumNous/new-api#4121', 'QuantumNous/new-api#7676'],
        'diagnosis': 'nil_ChannelMeta_in_ResolveOriginTask',
    },
}


def evaluate(cases, required, cleanup_errors=()):
    names = [case.get('name') for case in cases]
    invalid = len(names) != len(set(names)) or set(names) != set(required)
    known = {case['name'] for case in cases if case.get('status') == 'known_upstream'
             and KNOWN_UPSTREAM.get(case.get('name')) == {
                 'upstream': case.get('upstream'), 'diagnosis': case.get('diagnosis')}}
    for case in cases:
        status = case.get('status')
        invalid |= status not in STATUSES or status in ('fail', 'not_run')
        invalid |= status == 'known_upstream' and case.get('name') not in known
        invalid |= status == 'blocked' and case.get('blocked_by') not in known
    counts = {status: sum(case.get('status') == status for case in cases) for status in STATUSES}
    code = 1 if invalid or cleanup_errors or not cases else (2 if known else 0)
    return {'success': code == 0, 'contracts_success': code != 1, 'exit_code': code,
            'counts': counts, 'missing_cases': sorted(set(required) - set(names))}


# 2026-11-13 用户批准的发布豁免，只覆盖已精确确认的上游缺陷。
# 该批准集合独立于执行清单：新增探针不能自动扩大豁免范围。
# 发布校验器按已验证的 source_sha 加载本函数复核。
RELEASE_EXEMPT_DIAGNOSTICS = (
    ('middleware/TestUpstreamPluginOriginChannelStorageFailureIsPrivate500', 'plugin_storage'),
    ('relay/TestUpstreamRemixPreservesInheritedRatiosUnderAdaptorEstimate', 'A7'),
    ('relay/TestUpstreamRemixResolvesVendorTaskID', 'N1'),
    ('relay/TestUpstreamRemixAdaptorPreservesAction', 'N3'),
    ('router/TestUpstreamNativeRemixRouteSendsUpstreamRemixWithOriginChannelContext', 'N2'),
    ('router/TestUpstreamNativeRemixRouteLegacyTaskUsesPublicIDAndDataRatios', 'N2'),
    ('router/TestUpstreamNativeRemixRouteOnTaskPluginChannel', 'N2'),
    ('router/TestUpstreamNativeRemixRouteChecksKeyModelLimitAgainstOriginModel', 'A5'),
    ('router/TestUpstreamNativeRemixRouteRejectsOriginChannelOfAnotherPlatform', 'N2'),
    ('router/TestUpstreamNativeRemixRouteRetryDoesNotCompoundInheritedRatios', 'N2'),
    ('router/TestUpstreamMidjourneyContinuationOfMissingOrForeignTaskIsNotFoundFirst', 'mj_priority'),
    ('router/TestUpstreamMidjourneyContinuationLookupFailureIsPrivate500', 'mj_storage'),
)
RELEASE_EXEMPT_BLOCKED = ('outbound_id', 'plugin_action', 'context_mapping', 'platform', 'billing', 'retry')
RELEASE_EXEMPT_ROUTING_BLOCKED = (
    'native_remix_upstream_id', 'native_remix_plugin_action', 'native_remix_inherited_billing',
    'native_remix_platform', 'native_remix_expression_usage', 'native_remix_unsupported_provider',
)


# 诊断 preflight 的固定项：工具链与每个探针的 vet。运行中源码变化才会追加完整性项。
DIAGNOSTIC_PREFLIGHT = ('go_toolchain',) + tuple(
    'vet-' + tag + '-' + package for tag, package in dict.fromkeys(
        (probe.tag, probe.package) for probe in DIAGNOSTIC_PROBES))


def diagnostics_complete(diagnostics):
    """复算诊断报告的完整性，不信任报告自带的汇总字段。

    按集合核对而不是按顺序：真实报告把 blocked 项紧跟在对应探针后面，
    与清单顺序不同。preflight、状态与 blocked 关联逐项核验。
    """
    probes = DIAGNOSTIC_PROBES
    if not isinstance(diagnostics, dict) or not probes:
        return False
    cases = diagnostics.get('cases')
    preflight = diagnostics.get('preflight')
    if not isinstance(cases, list) or not isinstance(preflight, list) or not preflight:
        return False
    if not all(isinstance(item, dict) for item in (*cases, *preflight)):
        return False
    by_name = {}
    for case in cases:
        name = case.get('name')
        if not isinstance(name, str) or name in by_name:
            return False
        by_name[name] = case
    probe_names = {probe.name for probe in probes}
    if not probe_names <= set(by_name):
        return False
    # blocked 项只在探针为 known_upstream 且指纹为 N2 时产生；全绿报告没有 blocked 项。
    known = set()
    for probe in probes:
        case = by_name[probe.name]
        status = case.get('status')
        if status not in ('pass', 'known_upstream'):
            return False
        if status == 'known_upstream':
            if case.get('upstream') != 'QuantumNous/new-api' or case.get('diagnosis') != probe.fingerprint:
                return False
            known.add(probe.name)
    blocked_names = {probe.name + '#' + assertion for probe in probes
                     if probe.name in known and probe.fingerprint == 'N2'
                     for assertion in RELEASE_EXEMPT_BLOCKED}
    if set(by_name) != probe_names | blocked_names:
        return False
    for name in blocked_names:
        case = by_name[name]
        parent = name.split('#', 1)[0]
        if case.get('status') != 'blocked' or case.get('blocked_by') != parent or parent not in known:
            return False
        if case.get('reason') != 'origin_resolution_panic':
            return False
    preflight_names = [item.get('name') for item in preflight]
    # 名字必须是字符串；列表或对象进集合会抛 TypeError，发布路径上应拒绝而不是异常。
    if not all(isinstance(name, str) for name in preflight_names) or len(preflight_names) != len(set(preflight_names)):
        return False
    if not set(DIAGNOSTIC_PREFLIGHT) <= set(preflight_names):
        return False
    if any(item.get('status') not in STATUSES or not isinstance(item.get('name'), str) for item in preflight):
        return False
    counts = {status: sum(case.get('status') == status for case in cases) for status in STATUSES}
    # preflight 只有 pass 是健康状态；blocked 与 known_upstream 在这里没有定义的含义。
    invalid = counts['fail'] or counts['not_run'] or any(item.get('status') != 'pass' for item in preflight)
    code = 1 if invalid else (2 if counts['known_upstream'] else 0)
    return (diagnostics.get('exit_code') == code and diagnostics.get('success') is (code == 0)
            and diagnostics.get('contracts_success') is (code != 1) and diagnostics.get('counts') == counts)


def release_exemption(diagnostics, routing):
    """未通过项与登记的上游缺陷完全一致时返回记录；否则返回 None。

    豁免不把缺陷计为通过。调用方必须另行确认其余阶段全部通过。
    """
    if not diagnostics_complete(diagnostics) or not routing_complete(routing):
        return None
    if diagnostics.get('exit_code') != 2 or diagnostics.get('contracts_success') is not True:
        return None
    if routing.get('exit_code') != 2 or routing.get('contracts_success') is not True:
        return None
    if routing.get('cleanup_errors') or routing.get('missing_cases'):
        return None
    if any(item.get('status') != 'pass' for item in diagnostics.get('preflight', [])):
        return None
    known, blocked = {}, {}
    for case in diagnostics.get('cases', []):
        status = case.get('status')
        if status == 'known_upstream':
            if case.get('upstream') != 'QuantumNous/new-api':
                return None
            known[case.get('name')] = case.get('diagnosis')
        elif status == 'blocked':
            if case.get('reason') != 'origin_resolution_panic':
                return None
            blocked.setdefault(case.get('blocked_by'), set()).add(case.get('name'))
        else:
            return None
    if known != dict(RELEASE_EXEMPT_DIAGNOSTICS):
        return None
    n2 = [name for name, diagnosis in RELEASE_EXEMPT_DIAGNOSTICS if diagnosis == 'N2']
    if set(blocked) != set(n2) or any(
            blocked[name] != {name + '#' + assertion for assertion in RELEASE_EXEMPT_BLOCKED} for name in n2):
        return None
    routed, routed_blocked = {}, set()
    for case in routing.get('cases', []):
        status = case.get('status')
        if status == 'pass':
            continue
        if status == 'known_upstream':
            routed[case.get('name')] = {'upstream': case.get('upstream'), 'diagnosis': case.get('diagnosis')}
        elif status == 'blocked':
            if case.get('blocked_by') not in KNOWN_UPSTREAM:
                return None
            routed_blocked.add(case.get('name'))
        else:
            return None
    if routed != KNOWN_UPSTREAM or routed_blocked != set(RELEASE_EXEMPT_ROUTING_BLOCKED):
        return None
    return {'diagnostics': [list(item) for item in RELEASE_EXEMPT_DIAGNOSTICS],
            'routing': KNOWN_UPSTREAM['upstream_native_remix_probe']['diagnosis']}


def routing_complete(routing):
    """复算路由报告的完整性，不信任报告自带的汇总字段。

    按集合核对：真实执行顺序与 REQUIRED_CASES 的登记顺序不同，顺序不是合同。
    """
    if not isinstance(routing, dict):
        return False
    cases = routing.get('cases')
    if not isinstance(cases, list) or not all(isinstance(case, dict) for case in cases):
        return False
    names = [case.get('name') for case in cases]
    # 名字必须是字符串；列表或对象进集合会抛 TypeError，发布路径上应拒绝而不是异常。
    if not all(isinstance(name, str) for name in names) or len(names) != len(set(names)) or set(names) != set(REQUIRED_CASES):
        return False
    recomputed = evaluate(cases, REQUIRED_CASES, routing.get('cleanup_errors') or ())
    return all(routing.get(key) == recomputed[key]
               for key in ('success', 'contracts_success', 'exit_code', 'counts', 'missing_cases'))


def combined_exit_code(results):
    """Local/incomplete failure (1) outranks the known-only status (2)."""
    if not results or any(row.get('exit_code', 0 if row.get('success') else 1) not in (0,2)
                          or (row.get('exit_code', 0) == 0 and row.get('success') is not True)
                          for row in results):
        return 1
    return 2 if any(row.get('exit_code') == 2 for row in results) else 0


def weight_tail_probability(hits, samples, probability):
    """Exact two-sided binomial tail; a diagnostic, not a fixed-hit contract."""
    import math
    if not 0 <= hits <= samples or samples <= 0 or not 0 < probability < 1:
        raise ValueError('invalid_weight_sample')
    masses = [math.comb(samples, k) * probability**k * (1-probability)**(samples-k)
              for k in range(samples+1)]
    return min(1.0, sum(value for value in masses if value <= masses[hits] * (1+1e-12)))


def websocket_request_identity(rows):
    """Read the ID from a single isolated create's new native financial logs."""
    import re
    ids = {row.get('request_id') for row in rows}
    if len(ids) != 1:
        raise ValueError('ws_settlement_identity_ambiguous')
    identifier = ids.pop()
    if not isinstance(identifier, str) or not re.fullmatch(r'[A-Za-z0-9_-]+-ws-0', identifier):
        raise ValueError('ws_settlement_identity_invalid')
    return identifier


def has_private_error_field(value):
    if isinstance(value, dict):
        return any(str(key).lower() in ('base_url','key','channel_id','channel_name','api_key')
                   or has_private_error_field(item) for key,item in value.items())
    if isinstance(value, list):
        return any(has_private_error_field(item) for item in value)
    return False


def origin_panic_frame(trace, source):
    """Identify the exact origin metadata dereference in verified build source."""
    import re
    inside = False
    expected = []
    for number, line in enumerate(source.splitlines(), 1):
        if line.startswith('func '):
            inside = line.startswith('func ResolveOriginTask(')
        if inside and line.strip() == 'if originTask.ChannelId != info.ChannelId {':
            expected.append(number)
    if len(expected) != 1:
        return None
    number = expected[0]
    if re.search(rb'/relay/relay_task\.go:' + str(number).encode() + rb'(?=\s)', trace):
        return {'file':'relay/relay_task.go','line':number,'function':'ResolveOriginTask',
                'statement':'if originTask.ChannelId != info.ChannelId {'}
    return None
