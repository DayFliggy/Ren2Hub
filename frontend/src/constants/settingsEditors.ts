export interface RecordField {
  key: string
  kind: 'text' | 'number' | 'boolean' | 'url' | 'datetime' | 'select' | 'json'
  required?: boolean
  min?: number
  maxLength?: number
  choices?: readonly string[]
  defaultValue?: unknown
}

export interface RecordEditorSchema {
  fields: readonly RecordField[]
  limit?: number
  uniqueKey?: string
}

// Wire field names are preserved; editing one field never drops provider extensions.
export const SETTINGS_RECORD_EDITORS: Record<string, RecordEditorSchema> = {
  'console_setting.announcements': {
    limit: 100,
    fields: [
      { key: 'content', kind: 'text', required: true, maxLength: 500 },
      { key: 'publishDate', kind: 'datetime', required: true },
      {
        key: 'type',
        kind: 'select',
        choices: ['default', 'ongoing', 'success', 'warning', 'error'],
        defaultValue: 'default',
      },
      { key: 'extra', kind: 'text', maxLength: 100 },
    ],
  },
  'console_setting.api_info': {
    limit: 50,
    fields: [
      { key: 'url', kind: 'url', required: true, maxLength: 500 },
      { key: 'route', kind: 'text', required: true, maxLength: 100 },
      { key: 'description', kind: 'text', required: true, maxLength: 200 },
      {
        key: 'color',
        kind: 'select',
        required: true,
        choices: [
          'blue',
          'green',
          'cyan',
          'purple',
          'pink',
          'red',
          'orange',
          'amber',
          'yellow',
          'lime',
          'light-green',
          'teal',
          'light-blue',
          'indigo',
          'violet',
          'grey',
          'slate',
        ],
        defaultValue: 'blue',
      },
    ],
  },
  'console_setting.faq': {
    limit: 100,
    fields: [
      { key: 'question', kind: 'text', required: true, maxLength: 200 },
      { key: 'answer', kind: 'text', required: true, maxLength: 1000 },
    ],
  },
  'console_setting.uptime_kuma_groups': {
    limit: 20,
    uniqueKey: 'categoryName',
    fields: [
      { key: 'categoryName', kind: 'text', required: true, maxLength: 50 },
      { key: 'url', kind: 'url', required: true, maxLength: 500 },
      { key: 'slug', kind: 'text', required: true, maxLength: 100 },
      { key: 'description', kind: 'text', maxLength: 200 },
    ],
  },
  PayMethods: {
    uniqueKey: 'type',
    fields: [
      { key: 'name', kind: 'text', required: true },
      { key: 'type', kind: 'text', required: true },
      { key: 'icon', kind: 'text' },
      { key: 'color', kind: 'text' },
      { key: 'min_topup', kind: 'text' },
    ],
  },
  CreemProducts: {
    uniqueKey: 'productId',
    fields: [
      { key: 'name', kind: 'text', required: true },
      { key: 'productId', kind: 'text', required: true },
      { key: 'price', kind: 'number', min: 0.01, required: true },
      { key: 'quota', kind: 'number', min: 1, required: true },
      {
        key: 'currency',
        kind: 'select',
        choices: ['USD', 'EUR'],
        defaultValue: 'USD',
      },
    ],
  },
  'channel_affinity_setting.rules': {
    uniqueKey: 'name',
    fields: [
      { key: 'name', kind: 'text', required: true },
      { key: 'model_regex', kind: 'json', defaultValue: [] },
      { key: 'path_regex', kind: 'json', defaultValue: [] },
      { key: 'user_agent_include', kind: 'json', defaultValue: [] },
      {
        key: 'key_sources',
        kind: 'json',
        required: true,
        defaultValue: [{ type: 'gjson', path: 'prompt_cache_key' }],
      },
      { key: 'value_regex', kind: 'text' },
      {
        key: 'ttl_seconds',
        kind: 'number',
        required: true,
        min: 1,
        defaultValue: 3600,
      },
      { key: 'skip_retry_on_failure', kind: 'boolean', defaultValue: false },
      { key: 'include_using_group', kind: 'boolean', defaultValue: true },
      { key: 'include_model_name', kind: 'boolean', defaultValue: true },
      { key: 'include_rule_name', kind: 'boolean', defaultValue: true },
      { key: 'param_override_template', kind: 'json', defaultValue: {} },
    ],
  },
}
