"""候选 runner 的内部子进程。输入由 runner 经 stdin 传递，不提供独立参数入口。"""
import json
from pathlib import Path
import sys
from types import SimpleNamespace

from .common import RequirementError, atomic_private, configure_target, require


def execute():
    raw = sys.stdin.buffer.read(16385)
    require(len(raw) <= 16384, 'candidate_worker_input_too_large')
    configuration = json.loads(raw)
    private, output = Path(configuration['private']), Path(configuration['output'])
    stage = configuration['stage']
    from candidate import STAGES
    require(stage in STAGES, 'unknown_candidate_contract')
    configure_target(private / 'target.json')
    credentials = private / 'credentials.json'
    args = SimpleNamespace(revision=configuration['revision'], report=output / (stage + '.json'),
                           credentials=credentials if credentials.exists() else None,
                           credentials_output=credentials, mock_tls_cert=private / 'server.crt',
                           mock_tls_key=private / 'server.key', base='http://127.0.0.1:3000',
                           state=private / 'plugin-state.json',
                           phase='after-restart' if stage == 'plugin-after' else 'before-restart',
                           snapshot=True)
    try:
        if stage == 'routing':
            from .routing import run_routing
            report = run_routing(args)
        elif stage.startswith('plugin-'):
            from .plugin import run_plugin
            report = run_plugin(args)
        else:
            from .gateway import run_group
            report = run_group({'ws': 'websocket', 'upstream_sync': 'management'}.get(stage, stage), args)
    except Exception as error:
        report = {'success': False, 'error_type': type(error).__name__,
                  'error_code': str(error) if isinstance(error, RequirementError) else 'candidate_worker_failed'}
        atomic_private(args.report, json.dumps(report, indent=2))
    raise SystemExit(report.get('exit_code', 0 if report['success'] else 1))
