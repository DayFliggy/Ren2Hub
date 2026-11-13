"""升级前后配置的精确合同；配置值只在内存比较，不进入报告。"""
import json
import re

from contracts.common import require

SENSITIVE_KEYS = frozenset((
    'CheckSensitiveEnabled', 'CheckSensitiveOnPromptEnabled', 'StopOnSensitiveEnabled',
    'SensitiveWords', 'StreamCacheQueueLength',
))
INSTANCES_KEY = 'request_plugin_setting.instances'


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
