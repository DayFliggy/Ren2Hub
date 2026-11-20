"""候选 runner 的内部子进程。输入由 runner 经 stdin 传递，不提供独立参数入口。"""
import json
from pathlib import Path
import sys
from types import SimpleNamespace

from .common import RequirementError, acceptance_target, atomic_private, configure_target, require


def execute():
    raw = sys.stdin.buffer.read(16385)
    require(len(raw) <= 16384, 'candidate_worker_input_too_large')
    configuration = json.loads(raw)
    private, output = Path(configuration['private']), Path(configuration['output'])
    stage = configuration['stage']
    from candidate import STAGES, UPGRADE_STAGES
    require(stage in (*STAGES, *UPGRADE_STAGES),
            'unknown_candidate_contract')
    configure_target(private / 'target.json')
    credentials = private / 'credentials.json'
    upgrade_plugin = stage.startswith(('upgrade-before-', 'upgrade-prepare-', 'upgrade-resume-', 'upgrade-after-'))
    quantity = stage.rsplit('-', 1)[-1] if upgrade_plugin else 'seconds'
    after = stage == 'plugin-after' or stage.startswith('upgrade-after-')
    attachment_phase = 'capture' if stage == 'upgrade-management' else (
        'verify' if stage == 'upstream_sync' and 'attachment_volume' in acceptance_target() else None)
    args = SimpleNamespace(revision=configuration['revision'], report=output / (stage + '.json'),
                           credentials=credentials if credentials.exists() else None,
                           credentials_output=credentials, mock_tls_cert=private / 'server.crt',
                           mock_tls_key=private / 'server.key', base='http://127.0.0.1:3000',
                           state=private / ('upgrade-plugin-' + quantity + '.json' if upgrade_plugin else 'plugin-state.json'),
                           phase='after-restart' if after else 'before-restart',
                           snapshot=True, usage_quantity=quantity,
                           upgrade_source=stage in ('upgrade-baseline', 'upgrade-management'),
                           resume_state=stage == 'upgrade-resume-p', prepare_only=stage == 'upgrade-prepare-p',
                           upgrade_attachment_phase=attachment_phase,
                           upgrade_attachment_state=private / 'upgrade-attachment.json' if attachment_phase else None,
                           previous_target=private / 'upgrade-source-target.json'
                           if (upgrade_plugin and (after or stage == 'upgrade-resume-p')) or attachment_phase == 'verify' else None)
    try:
        if stage == 'routing':
            from .routing import run_routing
            report = run_routing(args)
        elif stage.startswith('plugin-') or upgrade_plugin:
            from .plugin import run_plugin
            report = run_plugin(args)
        else:
            from .gateway import run_group
            report = run_group({'ws': 'websocket', 'upstream_sync': 'management',
                                'upgrade-baseline': 'billing', 'upgrade-management': 'management'}.get(stage, stage), args)
    except Exception as error:
        report = {'success': False, 'error_type': type(error).__name__,
                  'error_code': str(error) if isinstance(error, RequirementError) else 'candidate_worker_failed'}
        atomic_private(args.report, json.dumps(report, indent=2))
    raise SystemExit(report.get('exit_code', 0 if report['success'] else 1))
