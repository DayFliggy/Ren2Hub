"""保护按改动模块确定候选范围的规则：隔离实例按源码差异选择管理与路由合同。"""
import json
from pathlib import Path
import tempfile
from types import SimpleNamespace
import unittest
from unittest.mock import patch

import candidate
import release_scope


class ScopeRuleTests(unittest.TestCase):
    def test_parts_cover_exactly_the_candidate_stages_except_routing(self):
        base = tuple(stage for part in release_scope.PARTS if part != 'routing-workload'
                     for stage in release_scope.PART_STAGES[part])
        self.assertEqual((*base, 'routing'), candidate.STAGES)

    def test_routing_change_requires_named_source_workload(self):
        self.assertEqual(release_scope.required_parts(['service/route_breaker.go']), ['management', 'routing-workload'])
        self.assertEqual(release_scope.required_parts(['service/route_cache_quality_test.go']), ['routing-workload'])
        self.assertEqual(release_scope.required_parts(['docs/routing/UNIFIED_ROUTING.md']), [])
        self.assertEqual(release_scope.stages(['routing-workload']), ('routing-workload',))

    def test_source_workload_requires_every_named_case_and_nonempty_execution(self):
        import run
        with tempfile.TemporaryDirectory() as tmp:
            path = Path(tmp)
            expected = release_scope.ROUTING_WORKLOAD_TESTS
            with patch('run.go_unit', return_value={'success': True, 'batches': []}) as unit:
                with self.assertRaisesRegex(RuntimeError, '路由负载用例没有执行'):
                    run.routing_workload(path, path, {})
                package = run.ROUTING_SERVICE_PACKAGE
                pattern = unit.call_args.args[4][package]
                with self.assertRaisesRegex(RuntimeError, expected[-1]):
                    run.select_tests(package, list(expected[:-1]), {}, pattern, True)

    def test_candidate_records_source_workload_as_a_scoped_report(self):
        with tempfile.TemporaryDirectory() as tmp:
            runner = object.__new__(candidate.Candidate)
            runner.source = Path(tmp)
            runner.output = Path(tmp)
            runner.report = {'stages': {}}
            expected = {'success': True, 'tests': ['TestAutomaticRoutingWorkload'], 'batches': [{'passed': 1}]}
            with patch('run.routing_workload', return_value=expected):
                runner.stage('routing-workload')
            self.assertEqual(runner.report['stages']['routing-workload'], expected)
            self.assertEqual(json.loads((Path(tmp) / 'routing-workload.json').read_text()), expected)

    def test_server_changes_select_management_on_fresh_database(self):
        # 启动路径上读写已有数据的代码：迁移、已存配置解析及其共享解码、内置角色策略重置、
        # OAuth 提供方注册、已有插件编译。它们不在同一个包里，规则不能依赖逐个列举。
        for path in ('model/topup_bonus_migration.go', 'model/main.go', 'model/option.go',
                     'setting/retired_business_options.go', 'setting/ratio_setting/model_ratio.go',
                     'service/authz/seed.go', 'service/authz/adapter.go', 'types/rw_map.go', 'common/json.go',
                     'oauth/registry.go', 'controller/task_plugin.go', 'relay/channel/openai/adaptor.go',
                     'relaykit/dto/a.go', 'tokenkit/image.go', 'main.go', 'go.mod', 'go.sum', 'relaykit/go.mod',
                     'Dockerfile', '.dockerignore', 'i18n/locales/zh-CN.yaml', 'VERSION', 'a_new_top_level_file',
                     # go:embed 嵌入整棵目录：plugins/tasks 下任何子目录的 plugin.js 都会在启动时注册。
                     'plugins/tasks/testdata/plugin.js', 'plugins/tasks/sora/plugin.js', 'plugins/tasks/sora/README.md',
                     'relaykit/relayconvert/testdata/a.json', 'testdata/b.json', 'relaykit/README.md'):
            self.assertEqual(release_scope.required_parts([path]), ['management'], path)
        for path in ('frontend/src/main.ts', 'frontend/package.json', 'frontend/tests/visual/a.spec.ts',
                     'frontend/README.md', 'docs/billing/TOPUP_BONUS.md', 'AGENTS.md', 'README.zh_CN.md',
                     'scripts/testing/run.py', 'scripts/prepare-frontend-embed.mjs', '.github/workflows/ci.yml',
                     '.agents/skills/a/SKILL.md', 'model/topup_test.go', 'main_test.go',
                     'relaykit/relayconvert/internal/oai_chat/convert_test.go'):
            self.assertEqual(release_scope.required_parts([path]), [], path)
        self.assertEqual(release_scope.required_parts([]), [])
        self.assertEqual(release_scope.required_parts(['docs/a.md', 'frontend/src/a.vue', 'service/b.go']), ['management'])
        # 名称相近，但仍应运行管理合同。
        for path in ('frontendx/a.go', 'docs.go', 'scripts.go', 'model/a_test.go.bak', 'service/testdata.go'):
            self.assertEqual(release_scope.required_parts([path]), ['management'], path)
        with self.assertRaises(ValueError):
            release_scope.parse('upgrade')

    def test_expand_adds_dependencies_in_execution_order(self):
        self.assertEqual(release_scope.expand(['plugin']), ['billing', 'plugin'])
        self.assertEqual(release_scope.stages([]), ())
        with self.assertRaises(ValueError):
            release_scope.expand(['routing'])

    def test_parse_distinguishes_full_from_selected_parts(self):
        self.assertIsNone(release_scope.parse('full'))
        self.assertEqual(release_scope.parse('auto, billing'), ['auto', 'billing'])
        self.assertEqual(release_scope.parse('boot'), ['boot'])
        for value in ('', 'full,billing', 'routing', 'diagnostics'):
            with self.assertRaises(ValueError):
                release_scope.parse(value)


class ScopedCandidateTests(unittest.TestCase):
    def runner(self, scope, base_revision, changed):
        runner = candidate.Candidate.__new__(candidate.Candidate)
        runner.scope, runner.base_revision, runner.report = scope, base_revision, {}
        runner.source = Path('/source')
        self.diff = patch('candidate.execute', return_value=SimpleNamespace(
            returncode=0, stdout='\0'.join(changed).encode()))
        return runner

    def select(self, scope, base_revision, changed=()):
        runner = self.runner(scope, base_revision, changed)
        with self.diff as execute:
            runner.select_scope()
        if base_revision:
            self.assertEqual(execute.call_args.args[0][3:],
                             ['diff', '--name-only', '--no-renames', '-z', 'c' * 40, 'HEAD', '--'])
        return runner

    def test_auto_selects_fresh_instance_stages_from_source_diff(self):
        runner = self.select(['auto'], 'c' * 40, ['model/topup.go', 'frontend/src/a.vue'])
        self.assertEqual(runner.stages, ('upstream_sync',))
        self.assertEqual(runner.report['scope']['required'], ['management'])
        runner = self.select(['auto', 'billing'], 'c' * 40, ['frontend/src/a.vue', 'docs/a.md'])
        self.assertEqual(runner.stages, ('billing',))
        self.assertEqual(runner.report['scope'], {'requested': ['auto', 'billing'], 'parts': ['billing'],
                                                  'required': [], 'base_revision': 'c' * 40})

    def test_boot_selects_no_stage(self):
        runner = self.select(['boot'], None)
        self.assertEqual(runner.stages, ())
        self.assertEqual(runner.report['scope'], {'requested': ['boot'], 'parts': [], 'required': None,
                                                  'base_revision': None})

    def test_scope_cannot_drop_a_required_part_or_guess_without_a_base(self):
        for scope, previous, changed, code in (
                (['billing'], 'c' * 40, ['model/topup.go'], 'candidate_scope_missing_required_parts'),
                (['auto'], None, [], 'auto_scope_requires_base_revision')):
            runner = self.runner(scope, previous, changed)
            with self.subTest(code=code), self.diff, self.assertRaisesRegex(candidate.CandidateError, code):
                runner.select_scope()

    def manifest(self, stable=True, **overrides):
        with tempfile.TemporaryDirectory() as directory:
            runner = candidate.Candidate.__new__(candidate.Candidate)
            runner.output, runner.stages = Path(directory), ('billing',)
            runner.report = {'success': True, 'exit_code': 0, 'source': {'candidate_sha': 'a' * 40},
                             'image': {'revision': 'a' * 40, 'id': 'sha256:' + 'e' * 64},
                             'stages': {'billing': {'success': True}}, 'cleanup': {'success': True},
                             'identity_verification': {'stable': True}, 'image_matches_clean_source': True,
                             'isolation': {'traffic': {'configured_interface': 'eth0', 'scope': None, 'interface': None,
                                                       'mounts': [['bind', '/proc/1/net/dev', '/host/proc/net/dev', True]]}},
                             'scope': {'parts': ['billing'], 'required': [], 'base_revision': None}, **overrides}
            reports = {'billing': {'path': str(runner.output / 'billing.json'), 'sha256': 'f' * 64}}
            runner.scoped_manifest(reports, stable)
            return json.loads((runner.output / 'candidate-manifest.json').read_text())

    def test_scoped_manifest_verifies_selected_evidence(self):
        manifest = self.manifest()
        self.assertTrue(manifest['evidence_verified'] and manifest['success'])
        self.assertEqual(manifest['scope']['parts'], ['billing'])
        # 未运行的诊断和 routing 不出现在 manifest 中，不能被读成通过。
        for key in ('diagnostics', 'known_upstream_restored', 'ordinary_race', 'upstream_exemption'):
            self.assertNotIn(key, manifest)
        # 没有 management 实测时 scope/interface 为空，但候选容器的网卡配置仍写入。
        self.assertEqual(manifest['traffic'], {'scope': None, 'interface': None, 'configured_interface': 'eth0',
                                               'mounts': [['bind', '/proc/1/net/dev', '/host/proc/net/dev', True]]})
        # 运行期间源码变化会使候选证据失效。
        self.assertFalse(self.manifest(stable=False)['evidence_verified'])
        for overrides in ({'stages': {'billing': {'success': False}}}, {'exit_code': 1}, {'error_code': 'x'},
                          {'cleanup': {'success': False}}, {'image_matches_clean_source': False}):
            rejected = self.manifest(**overrides)
            self.assertFalse(rejected['evidence_verified'], overrides)
            self.assertFalse(rejected['success'], overrides)
        # 在确定阶段之前失败，证据不完整，manifest 仍带 scope，不会被当成完整候选校验。
        early = self.manifest(scope={'requested': ['auto']}, stages={}, exit_code=1)
        self.assertFalse(early['evidence_verified'])
        self.assertEqual(early['scope'], {'requested': ['auto']})


if __name__ == '__main__':
    unittest.main()
