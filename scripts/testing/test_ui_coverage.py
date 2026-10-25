"""保护证据等级，禁止将引用、隐藏实例和失败用例混报为可见通过。"""
import json
import hashlib
from pathlib import Path
import subprocess
import tempfile
import unittest

from ui_coverage import apply_review, build_report, read_evidence, route_links, static_links


class UICoverageTests(unittest.TestCase):
    def setUp(self):
        self.directory = tempfile.TemporaryDirectory()
        self.addCleanup(self.directory.cleanup)
        self.root = Path(self.directory.name)

    def write(self, name, text):
        path = self.root / name
        path.parent.mkdir(parents=True, exist_ok=True)
        path.write_text(text)
        return path

    def report(self, status='passed', retry=0):
        return {'suites': [{'specs': [{'id': 'one', 'title': 'open', 'file': 'one.spec.ts',
            'tests': [{'expectedStatus': 'passed', 'status': 'expected' if status == 'passed' else 'unexpected',
                       'results': [{'status': status, 'retry': retry}]}]}]}]}

    def evidence(self, visible, mounted=None):
        return {'title': 'open', 'label': 'dialog', 'url': 'http://127.0.0.1:12345/page',
                'visible': visible, 'mounted': mounted if mounted is not None else visible}

    def test_failed_or_retried_case_does_not_become_a_passed_capture(self):
        evidence = self.write('run/artifacts/dialog.coverage.json', json.dumps(self.evidence(['src/views/Page.vue'])))
        evidence.with_name('dialog.png').write_bytes(b'png')
        for status, retry in [('failed', 0), ('passed', 1)]:
            self.write('run/playwright.json', json.dumps(self.report(status, retry)))
            captures, issues = read_evidence([self.root / 'run'])
            self.assertEqual(issues, [])
            self.assertEqual(captures[0]['test_status'], 'not-passed')

    def test_invalid_capture_does_not_disappear_silently(self):
        self.write('run/bad.coverage.json', json.dumps(self.evidence(['src/views/Page.vue'], [])))
        captures, issues = read_evidence([self.root / 'run'])
        self.assertEqual(captures, [])
        self.assertEqual(len(issues), 1)

    def test_same_title_is_not_a_unique_test_identity(self):
        self.write('run/dialog.coverage.json', json.dumps(self.evidence([])))
        report = self.report()
        report['suites'][0]['specs'].append({**report['suites'][0]['specs'][0], 'id': 'two'})
        self.write('run/playwright.json', json.dumps(report))
        captures, issues = read_evidence([self.root / 'run'])
        self.assertEqual(captures[0]['test_status'], 'unknown')
        self.assertEqual(len(issues), 1)

    def test_route_links_ignore_redirect_helpers_and_resolve_nested_paths(self):
        self.write('frontend/src/views/Page.vue', '<template>page</template>')
        text = """function helper() { return {path: '/wrong'} }
        const routes = [{path: '/settings', children: [{path: 'security/:section?',
          component: () => import('@/views/Page.vue')}]},
          {path: '/old', redirect: {path: '/new'}}]"""
        path = self.write('frontend/src/router/index.ts', text)
        routes = list(route_links(self.root / 'frontend', path, text))
        self.assertEqual(len(routes), 1)
        self.assertEqual(routes[0][1]['path'], '/settings/security/:section?')

    def test_absolute_children_and_imported_arrays_preserve_parent_policy_sources(self):
        self.write('frontend/src/views/Page.vue', '<template>page</template>')
        self.write('frontend/src/router/adminRoutes.ts', """export const adminRoutes = [
          {path: '/ops', component: () => import('@/views/Page.vue'),
           meta: {requiresRoot: true, ...currentPermissionHelper('ops')}}]""")
        self.write('frontend/src/router/index.ts', """import { adminRoutes } from './adminRoutes'
        const routes = [{path: '/console', meta: {requiresAuth: true}, children: [
          ...adminRoutes, {path: '/absolute', component: () => import('@/views/Page.vue'),
           beforeEnter: existingGuard}]}]""")
        _, _, routes = static_links(self.root)
        entries = {entry['path']: entry for entry in routes['src/views/Page.vue']}
        ops = entries['/ops']['policy_references']
        self.assertTrue(any(ref['inherited'] and 'requiresAuth' in ref['expression'] for ref in ops))
        self.assertTrue(any("currentPermissionHelper('ops')" in ref['expression'] for ref in ops))
        direct = entries['/absolute']['policy_references']
        self.assertTrue(any(ref['kind'] == 'beforeEnter' for ref in direct))
        self.assertTrue(any(ref['inherited'] and 'requiresAuth' in ref['expression'] for ref in direct))

    def test_review_role_cannot_outlive_its_fixture_source_or_replace_runtime_coverage(self):
        fixture = self.write('frontend/tests/fixture.ts', 'role: 100')
        sha = hashlib.sha256(fixture.read_bytes()).hexdigest()
        evidence = [{'id': 'capture'}]
        items = [{'path': 'src/views/Page.vue', 'history': 'modified', 'sha256': 'current',
                  'current_visible_evidence': ['capture']}]
        review = {'items': {'src/views/Page.vue': {'sha256': 'old', 'classification': ['已批准变化'],
                                                'note': '保留新的真实数据合同'}},
                  'evidence_contexts': {'capture': {'role': 'Root (100)', 'source_references': [
                      {'source': 'tests/fixture.ts', 'sha256': sha, 'line': 1}]}}}
        apply_review(self.root, items, evidence, review)
        self.assertEqual(items[0]['disposition']['source_status'], 'unverified-source')
        self.assertEqual(items[0]['observed_roles'], ['Root (100)'])
        fixture.write_text('role: 1')
        apply_review(self.root, items, evidence, review)
        self.assertEqual(items[0]['observed_roles'], [])
        self.assertEqual(evidence[0]['test_context']['source_status'], 'unverified-source')

    def test_static_import_unit_reference_and_hidden_instance_are_separate(self):
        self.write('frontend/src/views/Page.vue', '<script setup>import Dialog from "@/components/Dialog.vue"</script>')
        self.write('frontend/src/components/Dialog.vue', '<template><dialog>hidden</dialog></template>')
        self.write('frontend/src/components/Unused.vue', '<template>unused</template>')
        self.write('frontend/src/components/__tests__/Dialog.spec.ts', 'import Dialog from "../Dialog.vue"')
        self.write('frontend/src/router/index.ts', "const routes = [{path: '/page', component: () => import('@/views/Page.vue')}]")
        subprocess.run(['git', 'init', '-q', str(self.root)], check=True)
        subprocess.run(['git', '-C', str(self.root), 'add', 'frontend'], check=True)
        subprocess.run(['git', '-C', str(self.root), '-c', 'user.name=Test', '-c', 'user.email=test@example.invalid',
                        'commit', '-qm', 'test fixture'], check=True)
        history = subprocess.check_output(['git', '-C', str(self.root), 'rev-parse', 'HEAD'], text=True).strip()
        self.write('frontend/src/components/Added.vue', '<template>new</template>')
        self.write('frontend/src/components/Unused.vue', '<template>changed</template>')
        self.write('run/artifacts/dialog.coverage.json', json.dumps(self.evidence(
            ['src/views/Page.vue'], ['src/views/Page.vue', 'src/components/Dialog.vue'])))
        self.write('run/artifacts/dialog.png', 'png')
        self.write('run/playwright.json', json.dumps(self.report()))
        report = build_report(self.root, history, [self.root / 'run'])
        items = {item['path']: item for item in report['items']}
        self.assertEqual(items['src/views/Page.vue']['browser'], 'visible-passed')
        dialog = items['src/components/Dialog.vue']
        self.assertEqual(dialog['browser'], 'mounted-only')
        self.assertEqual(dialog['route_candidates'][0]['path'], '/page')
        self.assertEqual(len(dialog['unit_test_references']), 1)
        self.assertEqual(items['src/components/Unused.vue']['browser'], 'missing')
        self.assertEqual(items['src/components/Unused.vue']['history'], 'modified')
        self.assertEqual(items['src/components/Added.vue']['history'], 'added')
        self.assertEqual(report['source']['candidate_sha'], None)
        self.assertEqual(items['src/views/Page.vue']['current_browser'], 'visible-unverified-source')

        evidence = self.evidence(['src/views/Page.vue'])
        evidence['sourceHashes'] = {'src/views/Page.vue': hashlib.sha256(
            (self.root / 'frontend/src/views/Page.vue').read_bytes()).hexdigest()}
        self.write('run/artifacts/dialog.coverage.json', json.dumps(evidence))
        report = build_report(self.root, history, [self.root / 'run'])
        page = next(i for i in report['items'] if i['path'] == 'src/views/Page.vue')
        self.assertEqual(page['current_browser'], 'visible-current')
        self.write('frontend/src/views/Page.vue', '<template>new rendering</template>')
        report = build_report(self.root, history, [self.root / 'run'])
        page = next(i for i in report['items'] if i['path'] == 'src/views/Page.vue')
        self.assertEqual(page['current_browser'], 'visible-unverified-source')

        evidence['sourceHashes']['src/views/Page.vue'] = hashlib.sha256(
            (self.root / 'frontend/src/views/Page.vue').read_bytes()).hexdigest()
        evidence['screenshots'] = ['dialog.png', 'dialog-scroll.png']
        self.write('run/artifacts/dialog.coverage.json', json.dumps(evidence))
        report = build_report(self.root, history, [self.root / 'run'])
        page = next(i for i in report['items'] if i['path'] == 'src/views/Page.vue')
        self.assertEqual(page['current_browser'], 'visible-unverified-source')


if __name__ == '__main__':
    unittest.main()
