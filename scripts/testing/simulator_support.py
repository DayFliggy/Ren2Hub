"""独立供应商模拟器的候选生命周期；无网关业务规则。"""
import hashlib
import json
import os
import secrets
import time

# Model modifiers are a gateway feature. Providers receive the base model.
MODELS = ['route-test', 'mapped-test', 'claude-sonnet-4-5',
          'gemini-2.5-flash', 'sora-2', 'MiniMax-Hailuo-02', 'mj_imagine', 'mj_change', 'mj_action', 'mj_video', 'routing-origin']
CHANNELS = [('a', 'openai'), ('b', 'openai'), ('c', 'openai'), ('claude', 'anthropic'),
            ('gemini', 'gemini'), ('sora', 'sora'), ('hailuo', 'hailuo'), ('mj', 'mj'), ('origin', 'sora')]


def prepare(runner):
    from candidate import private_write, require
    evidence = runner.report.get('build_identities', {}).get('simulator')
    require(bool(evidence), 'verified_simulator_build_identity_required')
    manifest = evidence['manifest']
    require(manifest['image_id'] == runner.simulator_image, 'build_identity_image_mismatch')
    config = {'control_listen': ':19090', 'control_key': secrets.token_urlsafe(32), 'channels': []}
    for index, (name, protocol) in enumerate(CHANNELS):
        config['channels'].append({'id': name, 'listen': ':' + str(18081 + index), 'protocol': protocol,
            'models': MODELS, 'keys': [{'alias': name + '-primary', 'value': secrets.token_urlsafe(32)}]})
    config_path = runner.private / 'simulator.json'
    private_write(config_path, config)
    runner.start('simulator', manifest['image_id'], ('--network', runner.network, '--network-alias', 'simulator',
        '--user', str(os.getuid()) + ':' + str(os.getgid()), '--read-only', '--tmpfs', '/run:rw,size=8m',
        '--mount', 'type=bind,src=' + str(config_path) + ',dst=/simulator.json,readonly'), ('--config', '/simulator.json'))
    runner.simulator_config = config
    runner.simulator_identity = {'reference': runner.simulator_image, 'id': manifest['image_id'],
        'revision': manifest['revision'], 'build_identity_sha256': evidence['sha256'],
        'working_tree_sha256': manifest['source_before']['working_tree_sha256'], 'protocol_version': 1,
        'configuration_sha256': hashlib.sha256(config_path.read_bytes()).hexdigest()}
    runner.report['simulator'] = runner.simulator_identity


def target(runner):
    address = runner.owned('simulator')['NetworkSettings']['Networks'][runner.network]['IPAddress']
    return {'container': runner.prefix + '-simulator', 'image_id': runner.simulator_identity['id'],
            'address': address, 'control_port': 19090, 'protocol_version': 1,
            'configuration_path': str(runner.private / 'simulator.json'),
            'configuration_sha256': runner.simulator_identity['configuration_sha256']}


def wait_ready(runner):
    from candidate import require
    address = runner.target['simulator']['address']
    script = ('import urllib.request; o=urllib.request.build_opener(urllib.request.ProxyHandler({})); '
              'r=o.open("http://' + address + ':19090/healthz",timeout=2); assert r.status==200')
    deadline = time.monotonic() + 15
    while time.monotonic() < deadline:
        require(runner.owned('simulator')['State']['Running'], 'simulator_exited')
        try:
            runner.namespace('mock', '/usr/bin/python3', '-c', script)
            return
        except Exception:
            time.sleep(.1)
    require(False, 'simulator_health_timeout')
