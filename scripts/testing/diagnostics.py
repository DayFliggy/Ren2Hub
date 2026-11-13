"""执行明确登记的路由诊断；上游缺陷证据不能替代本地验收。"""
from __future__ import annotations

import argparse
from collections import Counter
from dataclasses import dataclass
import hashlib
import json
import os
from pathlib import Path
import re
import signal
import subprocess
import time

GO = 'go'
MODULE = 'github.com/QuantumNous/new-api/'
STATUSES = ('pass', 'fail', 'known_upstream', 'blocked', 'not_run')


def _verify_probe_registry():
    """探针清单与 routing_result.DIAGNOSTIC_PROBES 必须逐项一致，发布复算不能另有一份。"""
    from routing_result import DIAGNOSTIC_PROBES
    actual = tuple((probe.package, probe.test, probe.fingerprint) for probe in PROBES)
    if actual != DIAGNOSTIC_PROBES:
        raise RuntimeError('diagnostic probe registry diverged from routing_result.DIAGNOSTIC_PROBES')
BLOCKED = ('outbound_id', 'plugin_action', 'context_mapping', 'platform', 'billing', 'retry')
N2_MARKER = ('known_upstream=N2; blocker=nil_ChannelMeta_in_ResolveOriginTask; '
             'blocked=' + ','.join(BLOCKED))


@dataclass(frozen=True)
class Probe:
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
PROBES = (
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

_verify_probe_registry()


@dataclass(frozen=True)
class CommandResult:
    returncode: int | None
    log: Path
    timed_out: bool = False
    error: str | None = None


def command(argv, root, log, env, timeout):
    """保留原始输出，不把非零退出当异常吞掉，也不向终端转发日志。"""
    with os.fdopen(os.open(log, os.O_WRONLY | os.O_CREAT | os.O_EXCL, 0o600), 'w') as stream:
        try:
            process = subprocess.Popen(argv, cwd=root, env=env, stdout=stream,
                                       stderr=subprocess.STDOUT, start_new_session=True)
        except OSError as error:
            return CommandResult(None, log, error=type(error).__name__)
        try:
            return CommandResult(process.wait(timeout=timeout), log)
        except BaseException as error:
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
            if isinstance(error, subprocess.TimeoutExpired):
                return CommandResult(process.returncode, log, timed_out=True)
            if isinstance(error, KeyboardInterrupt):
                return CommandResult(process.returncode, log, error='interrupted')
            raise


def environment(output, supplied=None):
    from run import environment as runner_environment
    env = runner_environment(output)
    allowed = ('PATH', 'HOME', 'USER', 'LANG', 'LC_ALL', 'GOPATH', 'GOCACHE',
               'GOMODCACHE', 'LD_LIBRARY_PATH')
    if supplied is not None:
        for key in allowed:
            if key in supplied:
                env[key] = supplied[key]
    env.update(GOWORK='off', GOTOOLCHAIN='local', GOMAXPROCS='2')
    return env


def test_command(probe, go=GO):
    return [go, 'test', '-p=2', '-count=1', '-timeout=60s', '-json',
            '-tags=' + probe.tag, '-run', '^' + re.escape(probe.test) + '$', './' + probe.package]


def test_nodes(probe):
    leaves = {probe.test + ('/' + suffix if suffix else '') for suffix in probe.leaves}
    # t.Run("missing/model_not_allowed") 发出一个子测试，不能虚构中间父节点。
    return leaves | {probe.test}, leaves


def parse_execution(probe, result):
    """确认完整执行集合，再让行为指纹解释失败；运行故障一律先失败。"""
    if result.timed_out:
        return None, 'command_timeout'
    if result.error:
        return None, 'command_error'
    if result.returncode not in (0, 1):
        return None, 'unexpected_process_exit'
    nodes, leaves = test_nodes(probe)
    starts, ends, outputs = Counter(), {}, {node: '' for node in nodes}
    package_starts, package_ends = 0, []
    raw = result.log.read_text(errors='replace')
    for line in raw.splitlines():
        if not line.strip():
            continue
        try:
            event = json.loads(line)
        except (ValueError, TypeError):
            return None, 'non_json_output_or_build_failure'
        if not isinstance(event, dict) or event.get('Package') != MODULE + probe.package:
            return None, 'unexpected_package_or_build_event'
        action, test = event.get('Action'), event.get('Test')
        if action not in ('start', 'run', 'pause', 'cont', 'output', 'pass', 'fail', 'skip'):
            return None, 'unknown_go_event'
        text = event.get('Output', '')
        if not isinstance(text, str):
            return None, 'invalid_output_event'
        if re.search(r'(?m)^(?:panic:|fatal error:|WARNING: DATA RACE)|test timed out|\[build failed\]', text):
            return None, 'runtime_or_build_failure'
        if action == 'skip':
            return None, 'skipped_test'
        if test:
            if test not in nodes:
                return None, 'unexpected_test'
            if action == 'run':
                starts[test] += 1
            if action in ('pass', 'fail'):
                ends.setdefault(test, []).append(action)
            if action == 'output':
                outputs[test] += text
        else:
            package_starts += action == 'start'
            if action in ('pass', 'fail'):
                package_ends.append(action)
    if package_starts != 1 or len(package_ends) != 1:
        return None, 'missing_package_result'
    if set(ends) != nodes or any(starts[node] != 1 or len(ends[node]) != 1 for node in nodes):
        return None, 'missing_or_duplicate_test_result'
    failed = {node for node in nodes if ends[node] == ['fail']}
    if bool(failed) != (package_ends == ['fail']) or bool(failed) != (result.returncode == 1):
        return None, 'inconsistent_exit_and_results'
    return {'nodes': nodes, 'leaves': leaves, 'failed': failed, 'outputs': outputs}, None


ASSERTION = re.compile(r'^\s*Error Trace:\s*(.*?)^\s*Error:\s*(.*?)^\s*Test:\s*([^\n]+)', re.M | re.S)
LOCATION = re.compile(r'^[ \t]+\w+_test\.go:\d+:[ \t]*([^\n]*)', re.M)


def assertions(text, probe, test):
    found = list(ASSERTION.finditer(text))
    if len(found) != len(re.findall(r'^\s*Error:', text, re.M)):
        return None
    if any(probe.file + ':' not in match[1] or match[3].strip() != test for match in found):
        return None
    # Unknown direct t.Error/t.Fatal output must not hide behind a recognized assertion.
    if any(match[1].strip() for match in LOCATION.finditer(text)):
        return None
    return [match[2].strip() for match in found]


def equal_pair(block):
    expected = re.search(r'^\s*expected:\s*(.*?)\s*$', block, re.M)
    actual = re.search(r'^\s*actual\s*:\s*(.*?)\s*$', block, re.M)
    if not block.startswith('Not equal:') or not expected or not actual:
        return None
    return expected[1], actual[1]


def matches_fingerprint(probe, test, text, origin_source):
    if probe.fingerprint == 'N2':
        from routing_result import origin_panic_frame
        panic = re.findall(
            r'upstream_panic type=runtime\.errorString '
            r'message="runtime error: invalid memory address or nil pointer dereference" '
            r'function=relay\.ResolveOriginTask frame=([^\s]+)', text)
        notes = [m[1].strip() for m in LOCATION.finditer(text)]
        if (len(panic) != 1 or text.count(N2_MARKER) != 1 or len(notes) != 2
                or not notes[0].startswith('upstream_panic ') or notes[1] != N2_MARKER
                or re.search(r'^\s*Error:', text, re.M)):
            return False
        return origin_panic_frame((panic[0] + '\n').encode(), origin_source) is not None
    blocks = assertions(text, probe, test)
    if not blocks:
        return False
    pairs = [equal_pair(block) for block in blocks]
    if probe.fingerprint == 'N1':
        return pairs == [('"video_vendor"', '"task_public"')]
    if probe.fingerprint == 'N3':
        return pairs == [('"remix"', '"image_to_video"')]
    if probe.fingerprint == 'A7':
        return pairs == [
            ('map[string]float64{"seconds":5, "size":1.5}', 'map[string]float64{"seconds":5}'),
            ('375000', '250000')] * 2
    if probe.fingerprint == 'A5':
        return pairs == [('200', '403')] and 'This token has no access to model  (request id: )' in text
    if probe.fingerprint == 'plugin_storage':
        return pairs == [('500', '400')]
    if probe.fingerprint == 'mj_storage':
        body = '{"code":4,"description":"task_not_found ","type":"upstream_error"}'
        return (len(blocks) == 2 and pairs[0] == ('500', '400')
                and blocks[1] == json.dumps(body) + ' does not contain "get_origin_task_failed"'
                and body in text)
    if probe.fingerprint == 'mj_priority':
        status, code, message = {
            'empty_manual_list': ('503', 'model_not_found', 'No available channel for model mj_upscale (distributor) (request id: )'),
            'all_candidates_full': ('429', 'channel_concurrency_exceeded', 'concurrency limit reached (request id: )'),
            'model_not_allowed': ('403', '', 'This token has no access to model mj_upscale (request id: )'),
        }[test.rsplit('/', 1)[-1]]
        expected = 'map[string]interface {}{"code":4, "description":"task_not_found ", "type":"upstream_error"}'
        actual = ('map[string]interface {}{"error":map[string]interface {}{"code":' + json.dumps(code)
                  + ', "message":' + json.dumps(message) + ', "type":"new_api_error"}}')
        return pairs == [('400', status), (expected, actual)]
    return False


def classify(probe, result, origin_source=''):
    base = {'name': probe.name, 'tag': probe.tag, 'package': probe.package, 'test': probe.test}
    execution, error = parse_execution(probe, result)
    if error:
        return {**base, 'status': 'fail', 'reason': error}
    if execution['failed'] != execution['nodes']:
        return {**base, 'status': 'fail', 'reason': 'unexpected_pass_review_waiting_list'}
    for parent in execution['nodes'] - execution['leaves']:
        output = execution['outputs'][parent]
        if LOCATION.search(output) or re.search(r'^\s*Error:', output, re.M):
            return {**base, 'status': 'fail', 'reason': 'unknown_parent_failure'}
    for leaf in sorted(execution['leaves']):
        if not matches_fingerprint(probe, leaf, execution['outputs'][leaf], origin_source):
            return {**base, 'status': 'fail', 'reason': 'unknown_failure_fingerprint', 'unmatched_test': leaf}
    return {**base, 'status': 'known_upstream', 'upstream': 'QuantumNous/new-api',
            'diagnosis': probe.fingerprint, 'evidence_tests': sorted(execution['leaves'])}


def summarize(cases, preflight=()):
    """逐项判定汇总；完整性复算在 routing_result.diagnostics_complete，两边的退出码规则一致。"""
    names = [case['name'] for case in cases]
    known = {case['name'] for case in cases if case.get('status') == 'known_upstream'}
    invalid = not cases or len(names) != len(set(names))
    for case in [*cases, *preflight]:
        status = case.get('status')
        invalid |= status not in STATUSES or status in ('fail', 'not_run')
        invalid |= status == 'known_upstream' and (not case.get('diagnosis') or not case.get('upstream'))
        invalid |= status == 'blocked' and case.get('blocked_by') not in known
    code = 1 if invalid else (2 if known else 0)
    return {'success': code == 0, 'contracts_success': code != 1, 'exit_code': code,
            'counts': {status: sum(case.get('status') == status for case in cases) for status in STATUSES}}


def sha256(path):
    return hashlib.sha256(path.read_bytes()).hexdigest()


def run(root: Path, output: Path, env: dict[str, str] | None = None, *, go: str = GO) -> dict:
    """返回结构化报告，不因诊断为红而抛异常；output 必须是本轮专属目录。"""
    root, output = Path(root).resolve(), Path(output).absolute()
    if output.is_symlink() or any(parent.is_symlink() for parent in output.parents):
        raise ValueError('diagnostic output must not contain symlinks')
    output.mkdir(mode=0o700, parents=True, exist_ok=True)
    if any(path.name != 'tmp' for path in output.iterdir()):
        raise ValueError('diagnostic output already contains evidence')
    output.chmod(0o700)
    safe_env = environment(output, env)
    cases, preflight, artifacts = [], [], {}
    source_files = sorted({p.package + '/' + p.file for p in PROBES} | {'relay/relay_task.go'})
    sources = {name: sha256(root / name) if (root / name).is_file() else None for name in source_files}
    origin = root / 'relay/relay_task.go'
    origin_source = origin.read_text() if origin.is_file() else ''
    started = time.time()
    version_result = command([go, 'version'], root, output / 'go-version.log', safe_env, 60)
    version = version_result.log.read_text().strip()
    tool_ready = (version_result.returncode == 0 and not version_result.error and not version_result.timed_out
                  and re.fullmatch(r'go version go1\.26\.10 \S+', version) is not None)
    artifacts[version_result.log.name] = sha256(version_result.log)
    preflight.append({'name':'go_toolchain','status':'pass' if tool_ready else 'fail',
                      'reason':'go1.26.10' if tool_ready else 'go1.26.10_required',
                      'log':version_result.log.name, 'sha256':artifacts[version_result.log.name]})
    readiness = {}
    for tag, package in dict.fromkeys((p.tag, p.package) for p in PROBES):
        name = 'vet-' + tag + '-' + package
        if not tool_ready:
            readiness[(tag, package)] = False
            preflight.append({'name':name,'status':'not_run','reason':'go_toolchain_failed'})
            continue
        result = command([go, 'vet', '-p=2', '-tags=' + tag, './' + package],
                         root, output / (name + '.log'), safe_env, 300)
        passed = result.returncode == 0 and not result.error and not result.timed_out
        readiness[(tag, package)] = passed
        artifacts[result.log.name] = sha256(result.log)
        preflight.append({'name': name, 'status': 'pass' if passed else 'fail',
                          'reason': 'vet_pass' if passed else 'vet_failed_or_incomplete',
                          'returncode': result.returncode, 'timed_out': result.timed_out,
                          'log': result.log.name, 'sha256': artifacts[result.log.name]})
    for probe in PROBES:
        if not readiness[(probe.tag, probe.package)]:
            cases.append({'name': probe.name, 'status': 'not_run', 'reason': 'tagged_vet_failed'})
            continue
        result = command(test_command(probe, go), root,
                         output / (probe.package + '-' + probe.test + '.jsonl'), safe_env, 120)
        case = classify(probe, result, origin_source)
        artifacts[result.log.name] = sha256(result.log)
        case.update(log=result.log.name, sha256=artifacts[result.log.name], returncode=result.returncode)
        cases.append(case)
        if case['status'] == 'known_upstream' and probe.fingerprint == 'N2':
            cases.extend({'name': probe.name + '#' + assertion, 'status': 'blocked',
                          'blocked_by': probe.name, 'reason': 'origin_resolution_panic'} for assertion in BLOCKED)
    source_end = {name: sha256(root / name) if (root / name).is_file() else None for name in source_files}
    if sources != source_end:
        preflight.append({'name': 'probe_source_integrity', 'status': 'fail', 'reason': 'source_changed_during_run'})
    report = {'schema_version': 1, **summarize(cases, preflight), 'cases': cases, 'preflight': preflight,
              'tools':{'go':version if tool_ready else None},
              'source_sha256': sources, 'source_end_sha256': source_end,
              'artifacts': artifacts, 'started_at': started, 'finished_at': time.time()}
    path = output / 'report.json'
    with os.fdopen(os.open(path, os.O_WRONLY | os.O_CREAT | os.O_EXCL, 0o600), 'w') as stream:
        json.dump(report, stream, ensure_ascii=False, indent=2)
        stream.write('\n')
    # 报告哈希置于返回值和独立索引，避免自引用哈希。
    digest = sha256(path)
    with os.fdopen(os.open(output / 'report.sha256', os.O_WRONLY | os.O_CREAT | os.O_EXCL, 0o600), 'w') as stream:
        stream.write(digest + '  report.json\n')
    return {**report, 'report_path': str(path), 'report_sha256': digest}


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--root', type=Path, default=Path(__file__).resolve().parents[2])
    parser.add_argument('--output', type=Path, required=True)
    args = parser.parse_args()
    report = run(args.root, args.output)
    print(json.dumps({key: report[key] for key in ('exit_code', 'counts', 'report_path', 'report_sha256')}, ensure_ascii=False))
    return report['exit_code']


if __name__ == '__main__':
    raise SystemExit(main())
