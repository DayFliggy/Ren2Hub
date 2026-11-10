"""版本化构建证据以实际镜像内容及当前工作树为准；全部使用本地夹具。"""
from copy import deepcopy
import hashlib
import io
import json
from pathlib import Path
import subprocess
import tarfile
import tempfile
from types import SimpleNamespace
import unittest
from unittest.mock import Mock, patch

import build_routing_image
import candidate


APP_ID, SIM_ID = 'sha256:'+'a'*64, 'sha256:'+'b'*64
HEAD = 'c'*40
BUILD_TOOLS = {'go':'go1.26.10', 'bun':'1.4.0', 'node':'v24.10.0'}


def identity(artifact='ren2hub', source=None):
    source = source or {'git_head':HEAD, 'candidate_sha':None, 'dirty':True,
                        'working_tree_sha256':'d'*64, 'files':1}
    app = artifact == 'ren2hub'
    revision = HEAD if app else HEAD+'-dirty'
    path, binary = ('/new-api', b'app executable') if app else ('/channel-simulator', b'simulator executable')
    manifest = {'schema_version':1, 'artifact':artifact, 'success':True, 'stable':True,
                'image_id':APP_ID if app else SIM_ID, 'revision':revision, 'base_revision':HEAD,
                'source_before':deepcopy(source), 'source_after':deepcopy(source),
                'binary':{'path':path, 'sha256':hashlib.sha256(binary).hexdigest()},
                'tools':{'docker_client':'28.0.4', 'docker_server':'28.0.4', 'python':'3.13.5',
                         **(BUILD_TOOLS if app else {'go':'go1.26.10'})}}
    labels = {'org.opencontainers.image.revision':revision,
              ('io.ren2hub' if app else 'io.channel-simulator')+'.source-sha256':source['working_tree_sha256']}
    files = {path:binary}
    if app:
        labels['org.opencontainers.image.version'] = revision
        files['/etc/ren2hub/build-tools.json'] = json.dumps(BUILD_TOOLS).encode()
    else:
        manifest['protocol_version'] = 1
        labels['io.channel-simulator.protocol'] = '1'
    image = {'Id':manifest['image_id'], 'Config':{'Labels':labels}}
    return manifest, image, files


def archive(path, data):
    output = io.BytesIO()
    with tarfile.open(fileobj=output, mode='w') as stream:
        member = tarfile.TarInfo(Path(path).name)
        member.size = len(data)
        stream.addfile(member, io.BytesIO(data))
    return output.getvalue()


class DockerFixture:
    """只支持校验所需操作；任何启动容器或 HTTP 调用都会使测试失败。"""
    def __init__(self, entries):
        self.entries = {image['Id']:(image,files) for _,image,files in entries}
        self.created, self.removed, self.containers, self.calls = [], [], {}, []
        self.copy_fault = False
        self.remove_fault = False

    def __call__(self, *args, **kwargs):
        self.calls.append(args)
        if args[:2] == ('image','inspect'):
            return SimpleNamespace(stdout=json.dumps([self.entries[args[2]][0]]).encode())
        if args[0] == 'create':
            assert '--read-only' in args and args[1:3] == ('--network','none')
            name = format(len(self.created)+1, '064x')
            self.created.append(name)
            self.containers[name] = args[-1]
            return SimpleNamespace(stdout=name.encode())
        if args[0] == 'cp':
            name, path = args[1].split(':',1)
            return SimpleNamespace(stdout=b'broken archive' if self.copy_fault else
                                   archive(path, self.entries[self.containers[name]][1][path]))
        if args[0] == 'rm':
            self.removed.append(args[-1])
            if self.remove_fault: raise candidate.CandidateError('fixture_cleanup_failed')
            del self.containers[args[-1]]
            return SimpleNamespace(stdout=b'')
        if args[0] == 'version': return SimpleNamespace(stdout=b'28.0.4\n')
        if args[0] == 'tag': return SimpleNamespace(stdout=b'')
        raise AssertionError('unexpected operation: '+repr(args))


class BuildIdentityTests(unittest.TestCase):
    def setUp(self):
        self.app, self.sim = identity(), identity('channel-simulator')

    def validate(self, entry=None, source=None, payload=None):
        manifest, image, _ = entry or self.app
        candidate.validate_build_identity(manifest, image, manifest['artifact'],
                                          source or self.app[0]['source_before'], payload)

    def test_current_tree_fingerprint_includes_executable_bit_even_when_already_dirty(self):
        with tempfile.TemporaryDirectory() as directory:
            root=Path(directory)
            script=root/'script.sh'
            script.write_text('#!/bin/sh\nexit 0\n')
            def git(args,**kwargs):
                output=HEAD.encode() if 'rev-parse' in args else b'script.sh\0' if 'ls-files' in args else b' M script.sh\n'
                return SimpleNamespace(stdout=output)
            with patch('candidate.execute',side_effect=git):
                script.chmod(0o644)
                before=candidate.source_identity(root)
                script.chmod(0o755)
                after=candidate.source_identity(root)
            self.assertNotEqual(before['working_tree_sha256'],after['working_tree_sha256'])
            self.assertIsNone(before['candidate_sha'])
            self.assertIsNone(after['candidate_sha'])

    def test_dirty_and_unborn_builds_keep_candidate_sha_null(self):
        self.validate()
        manifest,image,files = self.sim
        for key in ('source_before','source_after'):
            manifest[key]['git_head'] = None
        manifest.update(revision='unborn',base_revision=None)
        image['Config']['Labels']['org.opencontainers.image.revision'] = 'unborn'
        self.validate(self.sim)
        bad = deepcopy(self.app)
        for key in ('source_before','source_after'): bad[0][key]['candidate_sha']=HEAD
        with self.assertRaisesRegex(candidate.CandidateError,'invalid_candidate_revision'): self.validate(bad)

    def test_rejects_wrong_image_label_and_stale_manifest(self):
        mutations = [
            (lambda m,i:m.update(schema_version=0), 'versioned_build_identity'),
            (lambda m,i:m.update(success=False), 'versioned_build_identity'),
            (lambda m,i:m.update(image_id=SIM_ID), 'image_mismatch'),
            (lambda m,i:m['source_after'].update(working_tree_sha256='e'*64), 'unstable_source'),
            (lambda m,i:i['Config']['Labels'].update({'io.ren2hub.source-sha256':'e'*64}), 'label_mismatch'),
            (lambda m,i:m.update(base_revision='f'*40), 'invalid_candidate_revision'),
            (lambda m,i:m['tools'].update(node=''), 'tools_required')]
        for mutation,code in mutations:
            with self.subTest(code=code):
                entry = deepcopy(self.app)
                mutation(*entry[:2])
                with self.assertRaisesRegex(candidate.CandidateError,code): self.validate(entry)
        with self.assertRaisesRegex(candidate.CandidateError,'stale_worktree'):
            self.validate(source={**self.app[0]['source_before'],'working_tree_sha256':'e'*64})

    def test_actual_binary_and_build_stage_tools_must_match_manifest(self):
        for artifact, path in [('ren2hub','/new-api'),('channel-simulator','/channel-simulator')]:
            entry = identity(artifact)
            entry[2][path] = b'other executable with identical labels'
            docker = DockerFixture([entry])
            payload = candidate.inspect_build_payload(docker, entry[0]['image_id'], artifact)
            with self.assertRaisesRegex(candidate.CandidateError,'binary_mismatch'):
                self.validate(entry, payload=payload)
            self.assertEqual(docker.created,docker.removed)
        docker = DockerFixture([self.app])
        self.app[2]['/etc/ren2hub/build-tools.json'] = json.dumps({**BUILD_TOOLS,'node':'v22.0.0'}).encode()
        payload = candidate.inspect_build_payload(docker,APP_ID,'ren2hub')
        with self.assertRaisesRegex(candidate.CandidateError,'tools_mismatch'): self.validate(payload=payload)

    def test_extract_failure_cleans_container_and_cleanup_failure_cannot_pass(self):
        for fault in ('copy_fault','remove_fault'):
            with self.subTest(fault=fault):
                docker = DockerFixture([self.app])
                setattr(docker,fault,True)
                with self.assertRaises((tarfile.ReadError,candidate.CandidateError)):
                    candidate.inspect_build_payload(docker,APP_ID,'ren2hub')
                self.assertEqual(docker.created,docker.removed)

    def runner(self, root, docker):
        runner = candidate.Candidate.__new__(candidate.Candidate)
        runner.source,runner.output = root,root
        runner.image,runner.simulator_image = APP_ID,SIM_ID
        runner.report = {'source':deepcopy(self.app[0]['source_before'])}
        runner.image_identity_path,runner.simulator_identity_path = root/'app-input.json',root/'sim-input.json'
        for path,entry in ((runner.image_identity_path,self.app),(runner.simulator_identity_path,self.sim)):
            path.write_text(json.dumps(entry[0],indent=4)+'\n')
        runner.docker = docker
        return runner

    def test_candidate_copies_exact_manifests_and_hashes_before_any_started_container(self):
        with tempfile.TemporaryDirectory() as directory:
            docker = DockerFixture([self.app,self.sim])
            runner = self.runner(Path(directory),docker)
            with patch('candidate.source_identity',return_value=runner.report['source']):
                result = runner.verify_build_identities()
            for name,input_path in [('app',runner.image_identity_path),('simulator',runner.simulator_identity_path)]:
                raw = input_path.read_bytes()
                self.assertEqual(Path(result[name]['path']).read_bytes(),raw)
                self.assertEqual(result[name]['sha256'],hashlib.sha256(raw).hexdigest())
            self.assertEqual(docker.created,docker.removed)
            self.assertEqual(len(docker.created),2)

    def test_missing_manifest_has_no_label_only_fallback(self):
        with tempfile.TemporaryDirectory() as directory:
            docker = DockerFixture([self.app,self.sim])
            runner = self.runner(Path(directory),docker)
            runner.simulator_identity_path.unlink()
            with self.assertRaisesRegex(candidate.CandidateError,'regular_build_identity'):
                runner.verify_build_identities()
            self.assertEqual(docker.calls,[])

    def test_changed_source_or_wrong_binary_prevents_candidate_http(self):
        for fault in ('stale','during_inspection','binary'):
            with self.subTest(fault=fault), tempfile.TemporaryDirectory() as directory:
                docker = DockerFixture([self.app,self.sim])
                runner = self.runner(Path(directory),docker)
                source = deepcopy(runner.report['source'])
                changed = {**source,'working_tree_sha256':'e'*64}
                if fault=='stale': runner.report['source']=changed
                if fault=='binary': self.app[2]['/new-api']=b'wrong executable'
                runner.sdk_runtimes = lambda:self.fail('candidate advanced past rejected identity')
                with patch('candidate.source_identity',side_effect=[source,changed] if fault=='during_inspection' else None,
                           return_value=source), self.assertRaises(candidate.CandidateError):
                    runner.prepare()
                self.assertEqual(docker.created,docker.removed)

    def test_source_edit_after_preparation_rejected_before_http_stage(self):
        runner = candidate.Candidate.__new__(candidate.Candidate)
        runner.source = Path('/fixture/source')
        runner.report = {'source':self.app[0]['source_before']}
        with patch('candidate.source_identity',return_value={**runner.report['source'],'working_tree_sha256':'e'*64}), \
                self.assertRaisesRegex(candidate.CandidateError,'source_changed_before_http_stage'):
            runner.stage('routing')

    def test_rejected_image_cleans_up_and_failure_outranks_known_upstream(self):
        with tempfile.TemporaryDirectory() as directory:
            docker=DockerFixture([self.app,self.sim])
            self.app[2]['/new-api']=b'wrong binary despite matching labels'
            runner=self.runner(Path(directory),docker)
            runner.report.update(success=False,exit_code=1,stages={})
            runner.diagnostics=lambda:runner.report.update(diagnostics={'exit_code':2,'success':False})
            runner.containers,runner.network_id={},None
            runner.manifest=lambda:None
            runner.stage=Mock(side_effect=AssertionError('HTTP must not run'))
            with patch('candidate.Candidate',return_value=runner), \
                    patch('candidate.source_identity',return_value=runner.report['source']):
                report=candidate.run(runner.source,runner.output,APP_ID,simulator_image=SIM_ID)
            self.assertEqual(report['exit_code'],1)
            self.assertEqual(report['error_code'],'build_identity_binary_mismatch')
            runner.stage.assert_not_called()
            self.assertEqual(docker.created,docker.removed)
            self.assertTrue(report['cleanup']['success'])
            self.assertEqual(json.loads((runner.output/'candidate.json').read_text())['exit_code'],1)


class RoutingImageBuilderTests(unittest.TestCase):
    def fixture(self, root, fault=None):
        source,output = root/'source',root/'output'
        source.mkdir(); output.mkdir()
        (source/'Dockerfile').write_text('FROM scratch\n')
        def source_identity(_):
            return {'git_head':HEAD, 'candidate_sha':None, 'dirty':True, 'files':1,
                    'working_tree_sha256':hashlib.sha256((source/'Dockerfile').read_bytes()).hexdigest()}
        entry=identity(source=source_identity(source))
        docker=DockerFixture([entry])
        def execute(args,**kwargs):
            if args[0]=='git': return SimpleNamespace(stdout=b'Dockerfile\0')
            if args[5]=='tag' and fault=='source_after_tag':
                (source/'Dockerfile').write_text('FROM changed\n')
            return docker(*args[5:],**kwargs)
        def build(args,**kwargs):
            self.assertNotIn('--tag',args)
            self.assertEqual((output/'source/Dockerfile').read_text(),'FROM scratch\n')
            if fault=='source': (source/'Dockerfile').write_text('FROM changed\n')
            if fault=='tools': entry[2]['/etc/ren2hub/build-tools.json']=json.dumps({**BUILD_TOOLS,'bun':'9.9.9'}).encode()
            return subprocess.CompletedProcess(args,0,APP_ID.encode()+b'\n')
        return source,output,source_identity,execute,build,docker

    def test_success_persists_actual_versions_and_binary_not_host_versions(self):
        with tempfile.TemporaryDirectory() as directory:
            source,output,identity_fn,execute,build,docker=self.fixture(Path(directory))
            with patch('build_routing_image.source_identity',side_effect=identity_fn), \
                    patch('build_routing_image.execute',side_effect=execute), \
                    patch('build_routing_image.subprocess.run',side_effect=build):
                result=build_routing_image.build(source,output)
            self.assertEqual(json.loads((output/'identity.json').read_text()),result)
            self.assertEqual(result['source_before'],result['source_after'])
            self.assertIsNone(result['source_before']['candidate_sha'])
            self.assertEqual({key:result['tools'][key] for key in BUILD_TOOLS},BUILD_TOOLS)
            self.assertEqual(result['binary']['sha256'],hashlib.sha256(b'app executable').hexdigest())
            self.assertEqual(docker.created,docker.removed)
            self.assertTrue(json.loads((output/'build-report.json').read_text())['success'])

    def test_mutation_and_unexpected_build_tool_never_publish_success_manifest(self):
        for fault in ('source','tools','source_after_tag'):
            with self.subTest(fault=fault), tempfile.TemporaryDirectory() as directory:
                source,output,identity_fn,execute,build,docker=self.fixture(Path(directory),fault)
                with patch('build_routing_image.source_identity',side_effect=identity_fn), \
                        patch('build_routing_image.execute',side_effect=execute), \
                        patch('build_routing_image.subprocess.run',side_effect=build), \
                        self.assertRaises(candidate.CandidateError):
                    build_routing_image.build(source,output)
                self.assertFalse((output/'identity.json').exists())
                self.assertFalse(json.loads((output/'build-report.json').read_text())['success'])
                if fault!='source_after_tag': self.assertFalse(any(args[0]=='tag' for args in docker.calls))
                self.assertEqual(docker.created,docker.removed)
