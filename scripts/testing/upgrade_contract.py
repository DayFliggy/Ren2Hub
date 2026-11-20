"""升级前后配置与数据的精确合同；配置值只在内存比较，不进入报告。"""
import hashlib
import json
import re

from contracts.common import require

SENSITIVE_KEYS = frozenset((
    'CheckSensitiveEnabled', 'CheckSensitiveOnPromptEnabled', 'StopOnSensitiveEnabled',
    'SensitiveWords', 'StreamCacheQueueLength',
))
INSTANCES_KEY = 'request_plugin_setting.instances'

# The top-up bonus release adds these columns with default 0 and replaces the
# discount tables; every other persisted row must survive an upgrade as is.
ADDED_COLUMNS = {'token_route_profiles': 'selected_only', 'top_ups': 'bonus_quota', 'admin_quota_operations': 'bonus_quota', 'redemptions': 'bonus_quota'}
ADDED_COLUMN_SUFFIX = '.added_column'
LEGACY_DISCOUNT_TABLES = ('topup_discount_configs', 'topup_discount_usage_hourly', 'topup_discounts',
                          'usage_discount_configs', 'usage_discount_hourly')
BONUS_TABLES = ('topup_bonus_configs', 'topup_bonus_usage_hourly', 'topup_bonus_decisions')
BONUS_ROW_TABLES = ('topup_discount_configs', 'topup_discount_usage_hourly', *BONUS_TABLES)
# Seeded into the discount release before its upgrade. Version 2 is the latest
# and grants nothing, so later stages keep their exact credits.
LEGACY_DISCOUNT_SEED_CONFIGS = tuple(json.dumps({
    'schema_version': 1, 'timezone': 'UTC', 'cap_rate': '0.05', 'fixed_rate': fixed,
    'amount': {'mode': 'off', 'tiers': []}, 'site': {'mode': 'off', 'tiers': []},
    'personal': {name: {'mode': 'off', 'tiers': []} for name in ('day', 'week', 'month')},
    'excluded_user_ids': [],
}, separators=(',', ':')) for fixed in ('0.01', '0'))
LEGACY_DISCOUNT_SEED_USER = 2147480000


def option_values(rows):
    require(isinstance(rows, list), 'upgrade_options_rows_invalid')
    result = {}
    for row in rows:
        require(isinstance(row, dict) and set(row) == {'key', 'value'}
                and isinstance(row['key'], str) and isinstance(row['value'], str)
                and row['key'] not in result, 'upgrade_options_rows_invalid')
        result[row['key']] = row['value']
    return result


def instances(value):
    try:
        result = json.loads(value) if value.strip() else []
    except (ValueError, TypeError):
        require(False, 'upgrade_request_instances_invalid')
    # The upstream nil list is serialized as an array once migration appends.
    if result is None:
        result = []
    require(isinstance(result, list) and all(isinstance(item, dict) for item in result),
            'upgrade_request_instances_invalid')
    return result


def verify_options_migration(before, after):
    """仅容许敏感词旧键删除及一个等价过滤实例追加，其他配置逐字节保持。"""
    retired = set(before) & SENSITIVE_KEYS
    if not retired:
        require(before == after, 'upgrade_changed_options_without_migration')
        return {'retired_keys': [], 'filter_created': False}
    require(not (set(after) & SENSITIVE_KEYS), 'upgrade_retired_sensitive_options_remain')
    words = '\n'.join(line.strip() for line in before.get('SensitiveWords', '').split('\n') if line.strip())
    expected = {key: value for key, value in before.items() if key not in SENSITIVE_KEYS}
    if words:
        old_instances = instances(before.get(INSTANCES_KEY, ''))
        require(INSTANCES_KEY in after, 'upgrade_sensitive_filter_missing')
        new_instances = instances(after[INSTANCES_KEY])
        require(len(new_instances) == len(old_instances) + 1
                and json.dumps(new_instances[:-1], sort_keys=True) == json.dumps(old_instances, sort_keys=True),
                'upgrade_existing_request_instances_changed')
        added = new_instances[-1]
        identifier = added.get('id')
        require(isinstance(identifier, str) and re.fullmatch(r'rp_[a-z2-7]{8}', identifier)
                and identifier not in {item.get('id') for item in old_instances},
                'upgrade_sensitive_filter_id_invalid')
        expected_instance = {
            'id': identifier, 'name': '敏感词', 'plugin': 'request-filter',
            'enabled': before.get('CheckSensitiveEnabled') == 'true'
                       and before.get('CheckSensitiveOnPromptEnabled') == 'true',
            'global': True, 'models': [],
            'config': {'rules': [], 'keywords': words, 'keywordsCaseSensitive': False,
                       'message': '', 'replies': False},
        }
        require(json.dumps(added, sort_keys=True) == json.dumps(expected_instance, sort_keys=True),
                'upgrade_sensitive_filter_behavior_changed')
        expected[INSTANCES_KEY] = after[INSTANCES_KEY]
    require(expected == after, 'upgrade_unexpected_option_change')
    return {'retired_keys': sorted(retired), 'filter_created': bool(words)}


def split_added_column(table, rows):
    """去掉新增列后的行用于逐表比较；新增列只能整行出现或整表缺失。"""
    column = ADDED_COLUMNS.get(table)
    if column is None:
        return rows, None
    if not rows:
        return rows, 'empty'
    present = {column in row for row in rows}
    require(len(present) == 1, 'upgrade_added_column_rows_invalid')
    if present == {False}:
        return rows, 'absent'
    primary_key = 'token_id' if table == 'token_route_profiles' else 'id'
    values = sorted([row[primary_key], row[column]] for row in rows)
    stripped = [{key: value for key, value in row.items() if key != column} for row in rows]
    return stripped, {'non_default': sum(1 for _, value in values if value != 0),
                      'sha256': hashlib.sha256(json.dumps(values).encode()).hexdigest()}


def verify_persisted_data(before, after):
    """除配置外逐表一致；新增列首次出现时每行都是 0，之后按数据保持。"""
    def stable(snapshot):
        return {key: value for key, value in snapshot.items()
                if key != 'options' and not key.endswith(ADDED_COLUMN_SUFFIX)}
    require(stable(before) == stable(after), 'upgrade_changed_persisted_contract_data')
    added = []
    for table, column in ADDED_COLUMNS.items():
        old, new = before.get(table + ADDED_COLUMN_SUFFIX), after.get(table + ADDED_COLUMN_SUFFIX)
        if old == 'absent':
            require(isinstance(new, dict) and new['non_default'] == 0, 'upgrade_added_column_not_default')
            added.append(table + '.' + column)
            continue
        require(old == new, 'upgrade_changed_persisted_contract_data')
    return added


def _usage(rows):
    return sorted((row['user_id'], row['hour_utc'], row['quota']) for row in rows)


def verify_topup_bonus_migration(before, after):
    """首次升级把最新折扣版本原样复制为加赠版本 1，逐小时用量相加迁移，并删除全部折扣表；
    已有加赠版本时以加赠表为准。"""
    require(not set(after['tables']) & set(LEGACY_DISCOUNT_TABLES), 'upgrade_legacy_discount_tables_remain')
    require(set(BONUS_TABLES) <= set(after['tables']), 'upgrade_bonus_tables_missing')
    usage = {}
    for row in [*before.get('topup_bonus_usage_hourly', []), *before.get('topup_discount_usage_hourly', [])]:
        key = (row['user_id'], row['hour_utc'])
        usage[key] = usage.get(key, 0) + row['quota']
    require(_usage(after['topup_bonus_usage_hourly']) == sorted((*key, quota) for key, quota in usage.items()),
            'upgrade_bonus_usage_changed')
    configs = before.get('topup_bonus_configs', [])
    legacy = before.get('topup_discount_configs', [])
    copied = not configs and bool(legacy)
    if copied:
        configs = [{**max(legacy, key=lambda row: row['version']), 'version': 1}]
    def ordered(rows):
        return sorted(json.dumps(row, sort_keys=True) for row in rows)
    require(ordered(after['topup_bonus_configs']) == ordered(configs), 'upgrade_bonus_config_changed')
    # Startup never writes decisions: a new table starts empty and existing records stay.
    require(after['topup_bonus_decisions'] == before.get('topup_bonus_decisions', []), 'upgrade_bonus_decisions_changed')
    return {'legacy_tables_dropped': sorted(set(before['tables']) & set(LEGACY_DISCOUNT_TABLES)),
            'config_copied': copied, 'legacy_usage_rows_moved': len(before.get('topup_discount_usage_hourly', []))}
