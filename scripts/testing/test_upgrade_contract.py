"""升级候选不得把有意迁移扩大成任意配置或资金变化的豁免。"""
import copy
import json
from pathlib import Path
import tempfile
import unittest
from unittest.mock import Mock

from candidate import Candidate, CandidateError, UPGRADE_STAGES
from contracts.common import RequirementError
from upgrade_contract import INSTANCES_KEY, SENSITIVE_KEYS, option_values, verify_options_migration


class UpgradeOptionsTests(unittest.TestCase):
    def setUp(self):
        self.before = {'EpayKey': 'private-test-value', 'SensitiveWords': ' Bad\n \n worse\t',
                       'CheckSensitiveEnabled': 'true', 'CheckSensitiveOnPromptEnabled': 'true',
                       'StopOnSensitiveEnabled': 'false', 'StreamCacheQueueLength': '7'}
        self.filter = {'id': 'rp_abcdefgh', 'name': '敏感词', 'plugin': 'request-filter',
                       'enabled': True, 'global': True, 'models': [],
                       'config': {'rules': [], 'keywords': 'Bad\nworse',
                                  'keywordsCaseSensitive': False, 'message': '', 'replies': False}}
        self.after = {'EpayKey': 'private-test-value', INSTANCES_KEY: json.dumps([self.filter])}

    def test_preserves_unrelated_options_and_checks_full_filter_semantics(self):
        self.assertEqual(verify_options_migration(self.before, self.after),
                         {'retired_keys': sorted(SENSITIVE_KEYS), 'filter_created': True})
        for field, value in [('enabled', False), ('enabled', 1), ('global', False), ('models', ['limited']),
                             ('plugin', 'parameter-override'), ('name', 'wrong')]:
            modified = copy.deepcopy(self.filter)
            modified[field] = value
            with self.subTest(field=field), self.assertRaises(RequirementError):
                verify_options_migration(self.before, {**self.after, INSTANCES_KEY: json.dumps([modified])})
        for field, value in [('keywords', 'Bad'), ('keywordsCaseSensitive', True), ('replies', True), ('replies', 0),
                             ('rules', [{'action': 'allow'}]), ('message', 'different')]:
            modified = copy.deepcopy(self.filter)
            modified['config'][field] = value
            with self.subTest(field=field), self.assertRaises(RequirementError):
                verify_options_migration(self.before, {**self.after, INSTANCES_KEY: json.dumps([modified])})

    def test_rejects_missing_retirement_and_unrelated_changes(self):
        for changed in ({**self.after, 'EpayKey': 'changed'}, {INSTANCES_KEY: self.after[INSTANCES_KEY]},
                        {**self.after, 'new-option': 'new'}, {**self.after, 'SensitiveWords': 'Bad'},
                        {'EpayKey': 'private-test-value'}):
            with self.subTest(changed_keys=sorted(changed)), self.assertRaises(RequirementError):
                verify_options_migration(self.before, changed)

    def test_no_legacy_keys_and_repeated_start_require_exact_stability(self):
        self.assertFalse(verify_options_migration(self.after, self.after)['filter_created'])
        for value in ({**self.after, 'EpayKey': 'changed'},
                      {**self.after, INSTANCES_KEY: json.dumps([self.filter, self.filter])}):
            with self.assertRaises(RequirementError):
                verify_options_migration(self.after, value)

    def test_empty_words_delete_only_retired_keys(self):
        for words in ('', ' \n\t', None):
            before = {**self.before, 'SensitiveWords': words}
            if words is None:
                del before['SensitiveWords']
            after = {'EpayKey': 'private-test-value'}
            self.assertFalse(verify_options_migration(before, after)['filter_created'])
            with self.assertRaises(RequirementError):
                verify_options_migration(before, {**after, INSTANCES_KEY: '[]'})

    def test_enabled_requires_both_switches_exactly_true(self):
        for first, second in [('false', 'true'), ('true', 'false'), ('TRUE', 'true'), (None, 'true')]:
            before = {**self.before, 'CheckSensitiveEnabled': first, 'CheckSensitiveOnPromptEnabled': second}
            if first is None:
                del before['CheckSensitiveEnabled']
            new = {**self.filter, 'enabled': False}
            self.assertTrue(verify_options_migration(before,
                            {**self.after, INSTANCES_KEY: json.dumps([new])})['filter_created'])
            with self.assertRaises(RequirementError):
                verify_options_migration(before, self.after)

    def test_existing_instances_survive_in_order_and_new_id_is_unique(self):
        existing = {'id': 'rp_existing', 'name': 'existing', 'plugin': 'request-filter',
                    'enabled': False, 'global': True, 'models': [], 'config': {'keywords': 'kept'}}
        before = {**self.before, INSTANCES_KEY: json.dumps([existing])}
        after = {**self.after, INSTANCES_KEY: json.dumps([existing, self.filter])}
        self.assertTrue(verify_options_migration(before, after)['filter_created'])
        for items in ([self.filter, existing], [{**existing, 'enabled': True}, self.filter],
                      [{**existing, 'enabled': 0}, self.filter],
                      [existing, {**self.filter, 'id': existing['id']}], [existing, self.filter, self.filter]):
            with self.subTest(items=items), self.assertRaises(RequirementError):
                verify_options_migration(before, {**after, INSTANCES_KEY: json.dumps(items)})

    def test_invalid_instances_are_never_accepted_as_empty(self):
        for text in ('{', '{}', '[1]'):
            with self.subTest(text=text), self.assertRaises(RequirementError):
                verify_options_migration({**self.before, INSTANCES_KEY: text}, self.after)

    def test_option_rows_cannot_silently_drop_columns_or_duplicates(self):
        rows = [{'key': 'payment', 'value': 'secret'}]
        self.assertEqual(option_values(rows), {'payment': 'secret'})
        for invalid in (rows * 2, [{'key': 'payment', 'value': 'secret', 'other': 'changed'}],
                        [{'key': 'payment', 'value': None}], {}):
            with self.assertRaises(RequirementError):
                option_values(invalid)

    def test_upgrade_runner_preserves_every_financial_and_business_hash(self):
        before = {'options': 'old-options', 'users': 'wallet', 'tokens': 'token-quota',
                  'top_ups': 'money', 'channels': 'route', 'attachments': 'files'}
        after = {**before, 'options': 'new-options'}
        for changed in ('users', 'tokens', 'top_ups', 'channels', 'attachments'):
            with self.subTest(changed=changed), tempfile.TemporaryDirectory() as directory:
                runner = self.upgrade_runner(directory)
                runner.upgrade_snapshot = Mock(side_effect=[(before, self.before),
                    ({**after, changed: 'unexpected-change'}, self.after)])
                with self.assertRaisesRegex(CandidateError, 'upgrade_changed_persisted_contract_data'):
                    runner.upgrade()
                self.assertFalse(runner.report['upgrade']['data_preserved'])
                runner.verify_rollback_copy.assert_not_called()

    def test_upgrade_runner_requires_exact_repeat_start_and_then_rollback_copy(self):
        before, after = {'options': 'old', 'users': 'wallet'}, {'options': 'new', 'users': 'wallet'}
        for repeated, valid in ((after, True), ({**after, 'options': 'new-again'}, False)):
            with self.subTest(valid=valid), tempfile.TemporaryDirectory() as directory:
                runner = self.upgrade_runner(directory)
                runner.upgrade_snapshot = Mock(side_effect=[(before, self.before), (after, self.after), repeated])
                if valid:
                    runner.upgrade()
                    self.assertTrue(runner.report['upgrade']['repeated_start_preserved_data'])
                    runner.verify_rollback_copy.assert_called_once()
                else:
                    with self.assertRaisesRegex(CandidateError, 'upgrade_repeated_start_changed_persisted_data'):
                        runner.upgrade()
                    runner.verify_rollback_copy.assert_not_called()

    @staticmethod
    def upgrade_runner(directory):
        runner = Candidate.__new__(Candidate)
        runner.private = Path(directory)
        runner.report = {'stages': {stage: {'success': True} for stage in UPGRADE_STAGES[:4]},
                         'upgrade': {}, 'isolation': {}}
        runner.containers = {'app': 'old-app', 'postgres': 'local-postgres'}
        runner.target = {'container_ids': dict(runner.containers)}
        runner.owned = Mock(return_value={'Id': 'old-app', 'State': {'ExitCode': 0}})
        runner.docker = Mock()
        runner.schema_fingerprint = Mock(return_value='stable-schema')
        runner.start_app = lambda: runner.containers.update(app='new-app')
        runner.wait_healthy = Mock()
        runner.image_id, runner.revision = 'new-image', 'new-revision'
        runner.verify_rollback_copy = Mock()
        return runner


if __name__ == '__main__':
    unittest.main()
