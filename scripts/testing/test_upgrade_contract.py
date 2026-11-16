"""升级候选不得把有意迁移扩大成任意配置或资金变化的豁免。"""
import copy
import json
from pathlib import Path
import tempfile
import unittest
from unittest.mock import Mock

from candidate import Candidate, CandidateError, UPGRADE_STAGES
from contracts.common import RequirementError
from upgrade_contract import (ADDED_COLUMN_SUFFIX, INSTANCES_KEY, LEGACY_DISCOUNT_SEED_CONFIGS, SENSITIVE_KEYS,
                              option_values, split_added_column, verify_options_migration, verify_persisted_data,
                              verify_topup_bonus_migration)

MIGRATED_BONUS_STATE = {'tables': ['topup_bonus_configs', 'topup_bonus_usage_hourly', 'topup_bonus_decisions'],
                        'topup_bonus_configs': [], 'topup_bonus_usage_hourly': [], 'topup_bonus_decisions': []}


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

    def test_upgrade_runner_requires_the_bonus_migration_and_its_repeat_stability(self):
        before, after = {'options': 'old', 'users': 'wallet'}, {'options': 'new', 'users': 'wallet'}
        legacy = {'tables': ['topup_discount_configs', 'topup_discount_usage_hourly'],
                  'topup_discount_configs': [], 'topup_discount_usage_hourly': []}
        stale = {**MIGRATED_BONUS_STATE, 'tables': [*MIGRATED_BONUS_STATE['tables'], 'topup_discounts']}
        for states, error in (([legacy, stale], 'upgrade_legacy_discount_tables_remain'),
                              ([legacy, MIGRATED_BONUS_STATE, stale], 'upgrade_repeated_start_changed_persisted_data')):
            with self.subTest(error=error), tempfile.TemporaryDirectory() as directory:
                runner = self.upgrade_runner(directory)
                runner.upgrade_snapshot = Mock(side_effect=[(before, self.before), (after, self.after), after])
                runner.topup_bonus_state = Mock(side_effect=states)
                with self.assertRaisesRegex(CandidateError, error):
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
        runner.seed_legacy_topup_discount = Mock(return_value=False)
        runner.topup_bonus_state = Mock(return_value=MIGRATED_BONUS_STATE)
        return runner


class TopUpBonusUpgradeTests(unittest.TestCase):
    def test_added_column_must_start_at_zero_and_then_stay_as_data(self):
        old = [{'id': 1, 'money': 7}, {'id': 2, 'money': 9}]
        rows, absent = split_added_column('top_ups', old)
        self.assertEqual((rows, absent), (old, 'absent'))
        new_rows, state = split_added_column('top_ups', [{**row, 'bonus_quota': 0} for row in old])
        self.assertEqual(new_rows, old)
        self.assertEqual(state['non_default'], 0)
        key = 'top_ups' + ADDED_COLUMN_SUFFIX
        self.assertEqual(verify_persisted_data({'top_ups': 'rows', key: absent}, {'top_ups': 'rows', key: state}),
                         ['top_ups.bonus_quota'])
        _, granted = split_added_column('top_ups', [{**old[0], 'bonus_quota': 5}, {**old[1], 'bonus_quota': 0}])
        with self.assertRaisesRegex(RequirementError, 'upgrade_added_column_not_default'):
            verify_persisted_data({key: absent}, {key: granted})
        self.assertEqual(verify_persisted_data({key: granted}, {key: granted}), [], 'later upgrades keep the bonus')
        with self.assertRaisesRegex(RequirementError, 'upgrade_changed_persisted_contract_data'):
            verify_persisted_data({key: granted}, {key: state})
        with self.assertRaisesRegex(RequirementError, 'upgrade_added_column_rows_invalid'):
            split_added_column('top_ups', [old[0], {**old[1], 'bonus_quota': 0}])
        self.assertEqual(split_added_column('users', old), (old, None))
        # Codes redeemed before the upgrade keep their face value and gain no bonus.
        self.assertEqual(split_added_column('redemptions', old)[1], 'absent')

    def test_first_upgrade_copies_the_latest_config_and_adds_hourly_usage(self):
        first, latest = LEGACY_DISCOUNT_SEED_CONFIGS
        before = {'tables': ['topup_discount_configs', 'topup_discount_usage_hourly', 'topup_discounts'],
                  'topup_discount_configs': [{'version': 3, 'config': first, 'created_by': 1, 'created_at': 10},
                                             {'version': 4, 'config': latest, 'created_by': 2, 'created_at': 20}],
                  'topup_discount_usage_hourly': [{'user_id': 7, 'hour_utc': 3600, 'quota': 12},
                                                  {'user_id': 7, 'hour_utc': 7200, 'quota': 30}]}
        after = {'tables': ['topup_bonus_configs', 'topup_bonus_usage_hourly', 'topup_bonus_decisions'],
                 'topup_bonus_configs': [{'version': 1, 'config': latest, 'created_by': 2, 'created_at': 20}],
                 'topup_bonus_usage_hourly': [{'user_id': 7, 'hour_utc': 7200, 'quota': 30},
                                              {'user_id': 7, 'hour_utc': 3600, 'quota': 12}],
                 'topup_bonus_decisions': []}
        self.assertEqual(verify_topup_bonus_migration(before, after),
                         {'legacy_tables_dropped': ['topup_discount_configs', 'topup_discount_usage_hourly', 'topup_discounts'],
                          'config_copied': True, 'legacy_usage_rows_moved': 2})
        changes = {
            'upgrade_bonus_config_changed': {**after, 'topup_bonus_configs': [{**after['topup_bonus_configs'][0], 'config': first}]},
            'upgrade_bonus_usage_changed': {**after, 'topup_bonus_usage_hourly': after['topup_bonus_usage_hourly'][:1]},
            'upgrade_bonus_tables_missing': {**after, 'tables': ['topup_bonus_configs', 'topup_bonus_usage_hourly']},
            'upgrade_bonus_decisions_changed': {**after, 'topup_bonus_decisions': [{'id': 1, 'source': 'topup'}]},
        }
        for error, changed in changes.items():
            with self.subTest(error=error), self.assertRaisesRegex(RequirementError, error):
                verify_topup_bonus_migration(before, changed)

    def test_an_existing_bonus_table_wins_and_rolled_back_usage_adds_up(self):
        bonus = [{'version': 2, 'config': 'bonus', 'created_by': 1, 'created_at': 5}]
        before = {'tables': ['topup_bonus_configs', 'topup_bonus_usage_hourly', 'topup_discount_configs', 'topup_discount_usage_hourly'],
                  'topup_bonus_configs': bonus, 'topup_discount_configs': [{'version': 1, 'config': 'discount', 'created_by': 1, 'created_at': 9}],
                  'topup_bonus_usage_hourly': [{'user_id': 7, 'hour_utc': 3600, 'quota': 12}],
                  'topup_discount_usage_hourly': [{'user_id': 7, 'hour_utc': 3600, 'quota': 3}]}
        decisions = [{'id': 4, 'source': 'redemption'}]
        before['topup_bonus_decisions'] = decisions
        after = {'tables': ['topup_bonus_configs', 'topup_bonus_usage_hourly', 'topup_bonus_decisions'],
                 'topup_bonus_configs': bonus, 'topup_bonus_decisions': decisions,
                 'topup_bonus_usage_hourly': [{'user_id': 7, 'hour_utc': 3600, 'quota': 15}]}
        self.assertFalse(verify_topup_bonus_migration(before, after)['config_copied'])
        with self.assertRaisesRegex(RequirementError, 'upgrade_bonus_decisions_changed'):
            verify_topup_bonus_migration(before, {**after, 'topup_bonus_decisions': []})
        self.assertEqual(verify_topup_bonus_migration(MIGRATED_BONUS_STATE, MIGRATED_BONUS_STATE),
                         {'legacy_tables_dropped': [], 'config_copied': False, 'legacy_usage_rows_moved': 0})


if __name__ == '__main__':
    unittest.main()
