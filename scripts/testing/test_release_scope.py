"""保护按改动模块确定候选范围的规则：漏掉升级演练会让迁移问题直接进入正式库。"""
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
        self.assertEqual(release_scope.PART_STAGES['upgrade'], candidate.UPGRADE_STAGES)
        base = tuple(stage for part in release_scope.PARTS if part != 'upgrade'
                     for stage in release_scope.PART_STAGES[part])
        self.assertEqual((*base, 'routing'), candidate.STAGES)

    def test_anything_that_reaches_the_server_binary_requires_the_upgrade_rehearsal(self):
        # 启动路径上读写已有数据的代码：迁移、已存配置解析及其共享解码、内置角色策略重置、
        # OAuth 提供方注册、已有插件编译。它们不在同一个包里，规则不能依赖逐个列举。
        for path in ('model/topup_bonus_migration.go', 'model/main.go', 'model/option.go',
                     'setting/retired_business_options.go', 'setting/ratio_setting/model_ratio.go',
                     'service/authz/seed.go', 'service/authz/adapter.go', 'types/rw_map.go', 'common/json.go',
                     'oauth/registry.go', 'controller/task_plugin.go', 'relay/channel/openai/adaptor.go',
                     'relaykit/dto/a.go', 'tokenkit/image.go', 'main.go', 'go.mod', 'go.sum', 'relaykit/go.mod',
                     'Dockerfile', '.dockerignore', 'i18n/locales/zh-CN.yaml', 'VERSION', 'a_new_top_level_file'):
            self.assertEqual(release_scope.required_parts([path]), ['upgrade'], path)
        for path in ('frontend/src/main.ts', 'frontend/package.json', 'frontend/tests/visual/a.spec.ts',
                     'docs/billing/TOPUP_BONUS.md', 'AGENTS.md', 'relaykit/README.md', 'scripts/testing/run.py',
                     'scripts/prepare-frontend-embed.mjs', '.github/workflows/ci.yml', '.agents/skills/a/SKILL.md',
                     'model/topup_test.go', 'main_test.go', 'relaykit/relayconvert/testdata/a.json',
                     'testdata/b.json'):
            self.assertEqual(release_scope.required_parts([path]), [], path)
        self.assertEqual(release_scope.required_parts([]), [])
        self.assertEqual(release_scope.required_parts(['docs/a.md', 'frontend/src/a.vue', 'service/b.go']), ['upgrade'])
        # 名称相近但不属于免演练的路径。
        for path in ('frontendx/a.go', 'docs.go', 'scripts.go', 'model/a_test.go.bak', 'service/testdata.go'):
            self.assertEqual(release_scope.required_parts([path]), ['upgrade'], path)

    def test_expand_adds_dependencies_in_execution_order(self):
        self.assertEqual(release_scope.expand(['plugin']), ['billing', 'plugin'])
        self.assertEqual(release_scope.expand(['sdk', 'upgrade']), ['upgrade', 'management', 'sdk'])
        self.assertEqual(release_scope.stages([]), ())
        self.assertEqual(release_scope.stages(['upgrade']), (*candidate.UPGRADE_STAGES, 'upstream_sync'))
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
    def runner(self, scope, previous_image, changed):
        runner = candidate.Candidate.__new__(candidate.Candidate)
        runner.scope, runner.previous_image, runner.report = scope, previous_image, {}
        runner.source = Path('/source')
        labels = {'org.opencontainers.image.revision': 'c' * 40}
        runner.docker = lambda *argv, **kwargs: SimpleNamespace(
            stdout=json.dumps([{'Config': {'Labels': labels}}]).encode())
        self.diff = patch('candidate.execute', return_value=SimpleNamespace(
            returncode=0, stdout='\0'.join(changed).encode()))
        return runner

    def select(self, scope, previous_image, changed=()):
        runner = self.runner(scope, previous_image, changed)
        with self.diff as execute:
            runner.select_scope()
        if previous_image:
            self.assertEqual(execute.call_args.args[0][3:], ['diff', '--name-only', '-z', 'c' * 40, 'HEAD', '--'])
        return runner

    def test_auto_runs_the_upgrade_only_when_persistence_changed(self):
        runner = self.select(['auto'], 'ren2hub-test:old', ['model/topup.go', 'frontend/src/a.vue'])
        self.assertEqual(runner.stages, (*candidate.UPGRADE_STAGES, 'upstream_sync'))
        self.assertEqual(runner.previous_image, 'ren2hub-test:old')
        self.assertEqual(runner.report['scope'], {'requested': ['auto'], 'parts': ['upgrade', 'management'],
                                                  'required': ['upgrade'], 'base_revision': 'c' * 40})
        runner = self.select(['auto', 'billing'], 'ren2hub-test:old', ['frontend/src/a.vue', 'docs/a.md'])
        self.assertEqual(runner.stages, ('billing',))
        # 不做升级演练时不启动旧镜像，但仍记录用来求差异的修订。
        self.assertIsNone(runner.previous_image)
        self.assertEqual(runner.report['scope'], {'requested': ['auto', 'billing'], 'parts': ['billing'],
                                                  'required': [], 'base_revision': 'c' * 40})

    def test_boot_selects_no_stage(self):
        runner = self.select(['boot'], None)
        self.assertEqual(runner.stages, ())
        self.assertEqual(runner.report['scope'], {'requested': ['boot'], 'parts': [], 'required': None,
                                                  'base_revision': None})

    def test_scope_cannot_drop_a_required_part_or_guess_without_a_base(self):
        for scope, previous, changed, code in (
                (['billing'], 'ren2hub-test:old', ['model/topup.go'], 'candidate_scope_missing_required_parts'),
                (['auto'], None, [], 'auto_scope_requires_previous_image'),
                (['upgrade'], None, [], 'upgrade_scope_requires_previous_image')):
            runner = self.runner(scope, previous, changed)
            with self.subTest(code=code), self.diff, self.assertRaisesRegex(candidate.CandidateError, code):
                runner.select_scope()

    def manifest(self, stable=True, published=True, **overrides):
        with tempfile.TemporaryDirectory() as directory:
            runner = candidate.Candidate.__new__(candidate.Candidate)
            runner.output, runner.workspace_sha, runner.stages = Path(directory), 'd' * 40, ('billing',)
            runner.report = {'success': True, 'exit_code': 0, 'source': {'candidate_sha': 'a' * 40},
                             'image': {'revision': 'a' * 40, 'id': 'sha256:' + 'e' * 64},
                             'stages': {'billing': {'success': True}}, 'cleanup': {'success': True},
                             'identity_verification': {'stable': True}, 'image_matches_clean_source': True,
                             'isolation': {'traffic': {'configured_interface': 'eth0', 'scope': None, 'interface': None,
                                                       'mounts': [['bind', '/proc/1/net/dev', '/host/proc/net/dev', True]]}},
                             'scope': {'parts': ['billing'], 'required': [], 'base_revision': None}, **overrides}
            reports = {'billing': {'path': str(runner.output / 'billing.json'), 'sha256': 'f' * 64}}
            runner.scoped_manifest(reports, stable, published)
            return json.loads((runner.output / 'candidate-manifest.json').read_text())

    def test_scoped_manifest_is_deployable_only_when_every_selected_stage_passed(self):
        manifest = self.manifest()
        self.assertTrue(manifest['deployment_eligible'] and manifest['success'])
        self.assertEqual(manifest['scope']['parts'], ['billing'])
        # 未运行的诊断和 routing 不出现在 manifest 中，不能被读成通过。
        for key in ('diagnostics', 'known_upstream_restored', 'ordinary_race', 'upstream_exemption'):
            self.assertNotIn(key, manifest)
        # 没有 management 实测时 scope/interface 为空，但候选容器的网卡配置仍写入，供发布校验器对照正式 compose。
        self.assertEqual(manifest['traffic'], {'scope': None, 'interface': None, 'configured_interface': 'eth0',
                                               'mounts': [['bind', '/proc/1/net/dev', '/host/proc/net/dev', True]]})
        # 运行期间源码变化，或提交、gitlink 与已发布状态不符，都不可部署。
        self.assertFalse(self.manifest(stable=False)['deployment_eligible'])
        self.assertFalse(self.manifest(published=False)['deployment_eligible'])
        for overrides in ({'stages': {'billing': {'success': False}}}, {'exit_code': 1}, {'error_code': 'x'},
                          {'cleanup': {'success': False}}, {'image_matches_clean_source': False}):
            self.assertFalse(self.manifest(**overrides)['deployment_eligible'], overrides)
        # 在确定阶段之前就失败的范围候选不可部署，manifest 仍带 scope，不会被当成完整候选校验。
        early = self.manifest(scope={'requested': ['auto']}, stages={}, exit_code=1)
        self.assertFalse(early['deployment_eligible'])
        self.assertEqual(early['scope'], {'requested': ['auto']})


if __name__ == '__main__':
    unittest.main()
