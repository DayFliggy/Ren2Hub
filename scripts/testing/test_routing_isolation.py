import hashlib
import json
from pathlib import Path
import tempfile
import unittest
from unittest.mock import patch

import candidate
from contracts.common import RequirementError, acceptance_target, atomic_private, require_candidate_mounts
from contracts.client import APIError, Client


class APIResponseSafetyTests(unittest.TestCase):
    def test_non_json_rate_limit_keeps_status_without_response_contents(self):
        client = Client('http://127.0.0.1:3000')
        with patch.object(client, 'request', return_value=(429, b'private-response-marker')):
            with self.assertRaises(APIError) as caught:
                client.api('GET', '/api/channel/?key=private-query-marker')
        self.assertIn('status=429 non_json_response', str(caught.exception))
        self.assertNotIn('private-', str(caught.exception))

    def test_wrong_json_shape_is_an_api_failure(self):
        client = Client('http://127.0.0.1:3000')
        with patch.object(client, 'request', return_value=(200, b'[]')):
            with self.assertRaises(APIError):
                client.api('GET', '/api/channel/')


class SimulatorIsolationTests(unittest.TestCase):
    def test_known_only_routing_is_observed_but_not_a_success(self):
        known = {'success':False,'exit_code':2,'contracts_success':True}
        self.assertTrue(candidate.stage_accepted('routing', 2, known))
        self.assertFalse(candidate.stage_accepted('billing', 2, known))
        self.assertFalse(candidate.stage_accepted('routing', 2, {**known,'contracts_success':False}))
        self.assertFalse(candidate.stage_accepted('routing', 1, known))

    def test_dirty_build_cannot_impersonate_a_clean_revision(self):
        source = {'candidate_sha':'a'*40, 'working_tree_sha256':'b'*64}
        labels = {'org.opencontainers.image.revision':'a'*40, 'io.ren2hub.source-sha256':'c'*64}
        for routing_only in (False, True):
            with self.subTest(routing_only=routing_only), self.assertRaisesRegex(
                    candidate.CandidateError, 'image_source_fingerprint_mismatch'):
                candidate.image_matches_source(labels, source, routing_only)
        labels['io.ren2hub.source-sha256'] = 'b'*64
        self.assertTrue(candidate.image_matches_source(labels, source))
        self.assertFalse(candidate.image_matches_source(labels, {**source,'candidate_sha':None}))

    def test_routing_requires_content_identity_even_for_a_clean_head(self):
        source = {'candidate_sha':'a'*40, 'working_tree_sha256':'b'*64}
        labels = {'org.opencontainers.image.revision':'a'*40}
        for routing_only in (False, True):
            with self.assertRaisesRegex(candidate.CandidateError, 'image_source_fingerprint_mismatch'):
                candidate.image_matches_source(labels, source, routing_only)
        for fingerprint in ('', 'invalid', None):
            with self.subTest(fingerprint=fingerprint), self.assertRaises(candidate.CandidateError):
                candidate.image_matches_source({**labels,'io.ren2hub.source-sha256':fingerprint}, source, True)

    def test_simulator_accepts_only_one_readonly_generated_config_mount(self):
        target = {'ca_path': '/test/server.crt', 'traffic_host_interface': '',
                  'simulator': {'configuration_path': '/test/simulator.json'}}
        mount = {'Type': 'bind', 'Source': '/test/simulator.json',
                 'Destination': '/simulator.json', 'RW': False}
        require_candidate_mounts('simulator', {'Mounts': [mount]}, target)
        for mounts in ([], [mount, mount], [{**mount, 'RW': True}],
                       [{**mount, 'Source': '/etc/passwd'}], [{**mount, 'Destination': '/other'}]):
            with self.subTest(mounts=mounts), self.assertRaises(RequirementError):
                require_candidate_mounts('simulator', {'Mounts': mounts}, target)

    def test_simulator_config_path_and_content_are_bound_to_run(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            config = root / 'simulator.json'
            atomic_private(config, '{}')
            prefix = 'ren2hub-candidate-local-' + 'f' * 24
            target = {role + '_container': prefix + '-' + role for role in ('app','postgres','redis','mock')}
            target.update(database_user='ren2hub', database_name='ren2hub_candidate', database_host='postgres:5432',
                          redis_host='redis:6379', run_id='f'*24, network=prefix+'-network', network_id='a'*64,
                          container_ids={role:'b'*64 for role in ('app','postgres','redis','mock','simulator')},
                          image_id='sha256:'+'c'*64, revision='d'*40, ca_path=str(root/'server.crt'),
                          network_guard={}, traffic_host_interface='', simulator={
                              'container':prefix+'-simulator','configuration_path':str(config),
                              'configuration_sha256':hashlib.sha256(config.read_bytes()).hexdigest()})
            manifest = root/'target.json'
            atomic_private(manifest, json.dumps(target))
            with patch.dict('os.environ', {'REN2HUB_ACCEPTANCE_TARGET':str(manifest)}):
                self.assertEqual(acceptance_target(), target)
                atomic_private(config, '{"changed":true}')
                with self.assertRaisesRegex(RequirementError, 'simulator_configuration_changed'):
                    acceptance_target()
                target['simulator']['configuration_path']='/etc/passwd'
                atomic_private(manifest,json.dumps(target))
                with self.assertRaisesRegex(RequirementError, 'foreign_simulator_configuration'):
                    acceptance_target()

    def test_routing_rejects_mutable_image_references_before_docker(self):
        for app, simulator in [('app:latest','sha256:'+'a'*64), ('sha256:'+'a'*64,'sim:latest')]:
            runner=candidate.Candidate.__new__(candidate.Candidate)
            runner.routing_only=True
            runner.image,runner.simulator_image=app,simulator
            runner.docker=lambda *_:self.fail('docker called before immutable ID validation')
            with self.assertRaisesRegex(candidate.CandidateError,'routing_requires_immutable_image_ids'):
                runner.prepare()
