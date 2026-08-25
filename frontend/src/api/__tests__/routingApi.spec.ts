import { describe, expect, it } from 'vitest'

import { parseRoutePreview, parseRouteProfileView } from '@/api/routingApi'

const policy = {
  id: 8,
  group_id: 4,
  load_balance: false,
  max_ratio: 1,
  retry_mode: 'next_channel',
  max_same_resource_attempts: 0,
  max_failover_attempts: 1,
  sticky: false,
}

const entry = {
  id: 9,
  group_id: 4,
  channel_id: 101,
  source: 'platform',
  enabled: true,
  position: 0,
  weight: 100,
}

function profileResponse() {
  return {
    profile: {
      id: 1,
      user_id: 7,
      token_id: 11,
      mode: 'manual',
      active_group_id: 4,
      version: 3,
      status: 1,
      created_at: 1,
      updated_at: 2,
    },
    groups: [
      {
        group: {
          id: 4,
          profile_id: 1,
          name: 'Primary',
          kind: 'manual',
          enabled: true,
          position: 0,
        },
        entries: [entry],
        policy,
      },
    ],
  }
}

describe('routing API contracts', () => {
  it('parses the backend aggregate group shape without exposing sensitive fields', () => {
    const parsed = parseRouteProfileView(
      { ...profileResponse(), key: 'must-not-leak' },
      '/api/routing/profiles'
    )

    expect(parsed.groups[0]).toMatchObject({
      id: 4,
      name: 'Primary',
      entries: [{ channel_id: 101 }],
      policy: { group_id: 4 },
    })
    expect(parsed).not.toHaveProperty('key')
  })

  it('rejects non-boolean routing flags instead of coercing them', () => {
    try {
      parseRouteProfileView(
        {
          ...profileResponse(),
          groups: [
            {
              ...profileResponse().groups[0],
              policy: { ...policy, load_balance: 'false' },
            },
          ],
        },
        '/api/routing/profiles'
      )
      throw new Error('expected invalid response')
    } catch (error) {
      expect(error).toMatchObject({ code: 'INVALID_RESPONSE' })
    }
  })

  it('requires preview to remain non-live and preserves filter details', () => {
    const parsed = parseRoutePreview({
      profile_id: 1,
      profile_version: 3,
      request_model: 'gpt-5',
      normalized_model: 'gpt-5',
      path: '/v1/chat/completions',
      endpoint_type: 'openai',
      active_group: profileResponse().groups[0].group,
      policy,
      entries: [
        {
          entry_id: 9,
          channel_id: 101,
          position: 0,
          weight: 100,
          request_model: 'gpt-5',
          actual_model: 'gpt-5',
          lab_slug: 'gpt',
          snapshot_version: 2,
          catalog_version: 'catalog-2',
          capability_state: 'eligible',
          filter_reason: 'runtime_recheck_required',
        },
      ],
      candidate_channel_ids: [],
      selection_mode: 'ordered',
      filter_reason_counts: { runtime_recheck_required: 1 },
      has_mixed: false,
      runtime_recheck_required: true,
      runtime_recheck_reasons: ['price_qualification'],
      live_selection: false,
    })

    expect(parsed.live_selection).toBe(false)
    expect(parsed.entries[0].filter_reason).toBe('runtime_recheck_required')
  })
})
