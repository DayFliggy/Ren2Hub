"""保护 affected 档位的选择规则：漏选会让回归在迭代中漏网，多选会浪费时间。"""
from pathlib import Path
import tempfile
import unittest

import affected


def package(path, directory, module='.', deps=(), go=(), tests=(), test_imports=()):
    return path, {'ImportPath': path, 'Dir': directory, 'module': module, 'Deps': list(deps),
                  'GoFiles': list(go), 'TestGoFiles': list(tests), 'TestImports': list(test_imports)}


class GoSelectionTests(unittest.TestCase):
    def setUp(self):
        self.temporary = tempfile.TemporaryDirectory()
        self.source = Path(self.temporary.name)
        for directory in ('model', 'service', 'controller', 'common'):
            (self.source / directory).mkdir()
            (self.source / directory / 'x_test.go').write_text('package x\n')
        (self.source / 'controller/x_test.go').write_text(
            'package controller\nvar fixture = filepath.Join("..", "fixtures", "native")\n')
        (self.source / 'fixtures/native').mkdir(parents=True)
        (self.source / 'fixtures/native/a.json').write_text('{}')
        self.packages = dict([
            package('m/common', 'common', go=['common.go'], tests=['x_test.go']),
            package('m/model', 'model', deps=['m/common'], go=['channel.go'], tests=['x_test.go']),
            package('m/service', 'service', deps=['m/model', 'm/common'], go=['s.go'], tests=['x_test.go']),
            package('m/controller', 'controller', deps=['m/common'], go=['c.go'], tests=['x_test.go'],
                    test_imports=['m/service']),
            package('m/router', 'router', deps=['m/service', 'm/model', 'm/common'], go=['r.go']),
        ])

    def tearDown(self):
        self.temporary.cleanup()

    def select(self, *files):
        return affected.select_go(self.source, self.packages, list(files))

    def test_production_change_reaches_reverse_deps_and_test_imports(self):
        result = self.select('model/channel.go')
        # controller links model only through its test import of service.
        self.assertEqual(result['tested'], ['m/controller', 'm/model', 'm/service'])
        self.assertEqual(result['vet'], ['m/controller', 'm/model', 'm/router', 'm/service'])

    def test_test_file_and_testdata_changes_stay_in_their_package(self):
        self.assertEqual(self.select('model/x_test.go')['tested'], ['m/model'])
        self.assertEqual(self.select('model/testdata/case.json')['tested'], ['m/model'])

    def test_deleted_production_file_counts_as_production(self):
        self.assertIn('m/router', self.select('model/removed.go')['vet'])

    def test_files_read_by_tests_select_the_reading_package(self):
        self.assertEqual(self.select('fixtures/native/a.json')['tested'], ['m/controller'])

    def test_go_mod_selects_the_whole_module(self):
        self.assertEqual(self.select('go.mod')['tested'], ['m/common', 'm/controller', 'm/model', 'm/service'])

    def test_unrelated_file_selects_nothing(self):
        self.assertEqual(self.select('README.md')['tested'], [])


class TargetedSelectionTests(GoSelectionTests):
    """targeted 由执行者点名；名称写错时必须报错，否则会把空运行当成通过。"""

    def request(self, **values):
        return {'go': [], 'db': [], 'race': [], 'vitest': [], 'visual': [], 'lint': [], 'python': False, **values}

    def targeted(self, **values):
        return affected.targeted(self.source, self.packages, self.request(**values),
                                 {'TestWalletDatabases'}, {'model', 'service'})

    def test_named_packages_keep_their_filters_and_skip_reverse_dependencies(self):
        selection = self.targeted(go=['./model/:^TestChannel', 'common'], db=['TestWalletDatabases'], race=['service'])
        self.assertEqual(selection['go']['tested'], ['m/common', 'm/model'])
        self.assertEqual(selection['go']['patterns'], {'m/model': '^TestChannel'})
        self.assertEqual(selection['go']['vet_modules'], {'.': ['m/common', 'm/model']})
        self.assertEqual(selection['databases'], {'packages': None, 'tests': ['TestWalletDatabases']})
        self.assertEqual(selection['race'], ['service'])
        self.assertEqual(selection['visual']['files'], [])

    def test_frontend_targets_become_related_tests_and_file_level_checks(self):
        for path in ('frontend/src/a.ts', 'frontend/src/a.css', 'frontend/tests/visual/wallet.spec.ts'):
            (self.source / path).parent.mkdir(parents=True, exist_ok=True)
            (self.source / path).write_text('')
        selection = self.targeted(vitest=['frontend/src/a.ts'], lint=['frontend/src/a.ts', 'frontend/src/a.css'],
                                  visual=['frontend/tests/visual/wallet.spec.ts'], python=True)
        self.assertEqual(selection['frontend'], {'vitest': ['frontend/src/a.ts'], 'typecheck': True,
                                                 'lint': ['frontend/src/a.ts'],
                                                 'format': ['frontend/src/a.css', 'frontend/src/a.ts'], 'build': False})
        self.assertEqual(selection['visual'], {'mode': 'subset', 'files': ['wallet.spec.ts']})
        self.assertTrue(selection['python'])
        self.assertIsNone(selection['databases'])

    def test_unknown_names_are_rejected(self):
        for values in ({'go': ['router']}, {'go': ['missing']}, {'go': ['model:(']}, {'go': ['model:^A', 'model:^B']},
                       {'db': ['TestUnregistered']}, {'race': ['controller']}, {'vitest': ['src/a.ts']},
                       {'lint': ['frontend/src/missing.ts']}, {'visual': ['missing.spec.ts']}):
            with self.subTest(values=values), self.assertRaises(ValueError):
                self.targeted(**values)


class FrozenInputTests(unittest.TestCase):
    def test_changing_a_frozen_fixture_asks_for_its_diagnostic(self):
        with tempfile.TemporaryDirectory() as directory:
            source = Path(directory)
            (source / 'scripts/testing').mkdir(parents=True)
            (source / 'frontend/tests/visual').mkdir(parents=True)
            (source / 'scripts/testing/known_upstream_restored.json').write_text(
                '{"cases": [{"fixtures": {"service/task_missing_channel_database_test.go": "0"}}]}')
            (source / 'scripts/testing/known_upstream_logger_race.json').write_text(
                '{"local_fixture_files": {"relay/helper/stream_test.go": "0"}, "current_functions": {"logger/logger.go:LogInfo": "0"}}')
            plan = affected.plan(source, ['service/task_missing_channel_database_test.go', 'logger/logger.go',
                                          'service/unrelated_test.go'], {}, None)
            self.assertEqual(plan['not_covered'], ['known_upstream_logger_race（冻结夹具或源码改动）',
                                                   'known_upstream_restored（冻结夹具或源码改动）'])


class FrontendSelectionTests(unittest.TestCase):
    def setUp(self):
        self.temporary = tempfile.TemporaryDirectory()
        self.source = Path(self.temporary.name)
        visual = self.source / 'frontend/tests/visual'
        visual.mkdir(parents=True)
        (visual / 'fixtures.ts').write_text("import { routes } from './routes'\n")
        (visual / 'routes.ts').write_text('export const routes = []\n')
        (visual / 'wallet.spec.ts').write_text("import { test } from './fixtures'\n")
        (visual / 'home.spec.ts').write_text("import { test } from '@playwright/test'\n")
        (visual / 'orphan.spec.ts').write_text('')
        (self.source / 'frontend/src/views').mkdir(parents=True)
        (self.source / 'frontend/src/views/Wallet.vue').write_text('')
        self.impact = {'schema_version': 1, 'specs': {
            'wallet.spec.ts': ['frontend/src/views/Wallet.vue'], 'home.spec.ts': ['frontend/src/views/Home.vue']}}

    def tearDown(self):
        self.temporary.cleanup()

    def visual(self, *files, impact=None):
        return affected.select_visual(self.source, list(files), impact)

    def test_global_config_runs_every_spec(self):
        self.assertEqual(self.visual('frontend/playwright.config.ts')['mode'], 'all')

    def test_shared_helper_selects_transitive_importers(self):
        self.assertEqual(self.visual('frontend/tests/visual/routes.ts')['files'], ['wallet.spec.ts'])

    def test_impact_map_selects_loaders_and_unmapped_specs(self):
        result = self.visual('frontend/src/views/Wallet.vue', impact=self.impact)
        self.assertEqual(result['files'], ['orphan.spec.ts', 'wallet.spec.ts'])

    def test_app_change_without_impact_map_runs_everything(self):
        self.assertEqual(self.visual('frontend/src/views/Wallet.vue')['mode'], 'all')

    def test_frontend_layers_follow_changed_files(self):
        result = affected.select_frontend(self.source, ['frontend/src/views/Wallet.vue', 'model/channel.go'])
        self.assertEqual(result['vitest'], ['frontend/src/views/Wallet.vue'])
        self.assertEqual(result['lint'], ['frontend/src/views/Wallet.vue'])
        self.assertTrue(result['typecheck'] and result['build'])
        self.assertFalse(affected.select_frontend(self.source, ['model/channel.go'])['build'])
        self.assertEqual(affected.select_frontend(self.source, ['frontend/eslint.config.js'])['lint'], 'all')

    def test_build_impact_assigns_requests_by_test_window(self):
        report = {'suites': [{'specs': [
            {'file': 'visual/a.spec.ts', 'tests': [{'results': [{'startTime': '2026-11-15T00:00:00.000Z', 'duration': 1000}]}]},
            {'file': 'visual/b.spec.ts', 'tests': [{'results': [{'startTime': '2026-11-15T00:00:02.000Z', 'duration': 1000}]}]},
        ]}]}
        start = affected.iso_ms('2026-11-15T00:00:00.000Z')
        log = f'{start + 500} /src/A.vue?t=1\n{start + 2500} /src/B.vue\n{start + 2600} /@vite/client\n'
        from run import browser_specs
        impact = affected.build_impact(report, log, browser_specs(report))
        self.assertEqual(impact['specs'], {'a.spec.ts': ['frontend/src/A.vue'], 'b.spec.ts': ['frontend/src/B.vue']})
        with self.assertRaises(RuntimeError):
            affected.build_impact(report, '', browser_specs(report))


if __name__ == '__main__':
    unittest.main()
