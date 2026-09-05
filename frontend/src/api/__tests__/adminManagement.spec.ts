import { describe, expect, it } from 'vitest'
import {
  parseDeploymentContainer,
  parseDeploymentPrice,
  parseModelMetadata,
  parsePrefillGroup,
  parseSyncPreview,
  parseSystemTask,
} from '../adminManagement'

describe('admin management contracts', () => {
  it('parses model metadata and preserves enriched arrays', () => {
    expect(
      parseModelMetadata({
        id: 1,
        model_name: 'gpt-test',
        vendor_id: 2,
        status: 1,
        sync_official: 1,
        name_rule: 0,
        bound_channels: [{ name: 'primary', type: 1 }],
        enable_groups: ['fast'],
        matched_models: [],
      }).model_name
    ).toBe('gpt-test')
  })
  it('normalizes JSON prefill items', () => {
    expect(
      parsePrefillGroup({
        id: 1,
        name: 'fast',
        type: 'model',
        items: '["a","b"]',
      }).items
    ).toEqual(['a', 'b'])
  })
  it('rejects malformed sync fields and deployment prices', () => {
    expect(() => parseSyncPreview({ missing: [1], conflicts: [] })).toThrow()
    expect(() =>
      parseDeploymentPrice({
        estimated_cost: -1,
        currency: 'usdc',
        estimation_valid: true,
        price_breakdown: { hourly_rate: 1 },
      })
    ).toThrow()
  })
  it('parses deployment event and task state payloads', () => {
    expect(
      parseDeploymentContainer({
        container_id: 'c',
        device_id: 'd',
        status: 'running',
        hardware: 'gpu',
        uptime_percent: 99,
        gpus_per_container: 1,
        public_url: '',
        events: [],
      }).status
    ).toBe('running')
    expect(
      parseSystemTask({
        task_id: 't',
        type: 'log_cleanup',
        status: 'running',
        created_at: 1,
        updated_at: 2,
        state: { progress: 20, processed: 2, total: 10 },
      }).progress
    ).toBe(20)
  })
})
