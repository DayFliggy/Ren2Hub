import { describe, expect, it } from 'vitest'

import {
  parseEligibleRouteChannel,
  parseRoutePreview,
  parseRouteProfileView,
} from '@/api/routingApi'

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
  it('accepts snapshotless channels as unresolved without weakening state validation', () => {
    const endpoint = '/api/routing/eligible-channels'
    const parsed = parseEligibleRouteChannel(
      {
        id: 101,
        name: 'Snapshotless channel',
        type: 1,
        status: 1,
        request_models: [],
        priority: 10,
        weight: 1,
        snapshot_version: 0,
        catalog_version: '',
        capability_state: 'unresolved',
        filter_reason: 'snapshot_unavailable',
      },
      endpoint
    )

    expect(parsed.capability_state).toBe('unresolved')
    expect(parsed.filter_reason).toBe('snapshot_unavailable')
    expect(() =>
      parseEligibleRouteChannel({ ...parsed, capability_state: '' }, endpoint)
    ).toThrow()
  })
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

  it('requires a persisted policy for every profile group', () => {
    const source = profileResponse()
    const withoutPolicy = {
      ...source,
      groups: source.groups.map(({ policy: _policy, ...group }) => group),
    }

    expect(() =>
      parseRouteProfileView(withoutPolicy, '/api/routing/profiles')
    ).toThrow('Invalid API response')
  })

  it('accepts profile entries ordered by persisted id when positions tie', () => {
    const source = profileResponse()
    const entries = [
      { ...entry, id: 9, channel_id: 202, position: 0 },
      { ...entry, id: 10, channel_id: 101, position: 0 },
    ]

    expect(
      parseRouteProfileView(
        {
          ...source,
          groups: [{ ...source.groups[0], entries }],
        },
        '/api/routing/profiles'
      ).groups[0].entries.map((item) => item.channel_id)
    ).toEqual([202, 101])
  })

  it('rejects numeric strings and unknown retry modes', () => {
    const numericString = {
      ...profileResponse(),
      profile: { ...profileResponse().profile, id: '1' },
    }
    const numericStringRatio = {
      ...profileResponse(),
      groups: [
        {
          ...profileResponse().groups[0],
          policy: { ...policy, max_ratio: '1' },
        },
      ],
    }
    const unknownRetryMode = {
      ...profileResponse(),
      groups: [
        {
          ...profileResponse().groups[0],
          policy: { ...policy, retry_mode: 'future_mode' },
        },
      ],
    }

    for (const invalid of [
      numericString,
      numericStringRatio,
      unknownRetryMode,
    ]) {
      expect(() =>
        parseRouteProfileView(invalid, '/api/routing/profiles')
      ).toThrow('Invalid API response')
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
          health: {
            state: 'closed',
            failure_count: 0,
            cooldown_until: 0,
            health_epoch: 1,
            last_latency_ms: 12,
            first_token_latency_ms: 4,
            updated_at: 2,
          },
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
    expect(parsed.entries[0].health.state).toBe('closed')
  })

  it('rejects unknown capability and health states', () => {
    const response = {
      profile_id: 1,
      profile_version: 3,
      request_model: 'gpt-5',
      normalized_model: 'gpt-5',
      path: '/v1/chat/completions',
      endpoint_type: 'openai',
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
          capability_state: 'future_state',
          health: {
            state: 'closed',
            failure_count: 0,
            cooldown_until: 0,
            health_epoch: 1,
            last_latency_ms: 0,
            first_token_latency_ms: 0,
            updated_at: 0,
          },
        },
      ],
      candidate_channel_ids: [],
      selection_mode: 'ordered',
      filter_reason_counts: {},
      has_mixed: false,
      runtime_recheck_required: true,
      runtime_recheck_reasons: [],
      live_selection: false,
    }

    const unknownHealth = structuredClone(response)
    unknownHealth.entries[0].capability_state = 'eligible'
    unknownHealth.entries[0].health.state = 'future_health'

    for (const invalid of [response, unknownHealth]) {
      try {
        parseRoutePreview(invalid)
        throw new Error('expected invalid response')
      } catch (error) {
        expect(error).toMatchObject({ code: 'INVALID_RESPONSE' })
      }
    }
  })

  it('rejects string numbers and unknown retry modes', () => {
    const invalidPolicy = {
      ...profileResponse(),
      groups: [
        {
          ...profileResponse().groups[0],
          policy: { ...policy, max_ratio: '1', retry_mode: 'future' },
        },
      ],
    }

    expect(() =>
      parseRouteProfileView(invalidPolicy, '/api/routing/profiles')
    ).toThrowError()
  })

  it('rejects malformed aggregate ownership, uniqueness, and ordering', () => {
    const secondGroup = {
      group: {
        id: 5,
        profile_id: 1,
        name: 'Secondary',
        kind: 'manual',
        enabled: true,
        position: 1,
      },
      entries: [{ ...entry, id: 10, group_id: 5, channel_id: 102 }],
      policy: { ...policy, group_id: 5 },
    }
    const invalidResponses = [
      {
        ...profileResponse(),
        groups: [
          {
            ...profileResponse().groups[0],
            group: { ...profileResponse().groups[0].group, profile_id: 2 },
          },
        ],
      },
      {
        ...profileResponse(),
        groups: [
          {
            ...profileResponse().groups[0],
            entries: [{ ...entry, group_id: 5 }],
          },
        ],
      },
      {
        ...profileResponse(),
        groups: [
          {
            ...profileResponse().groups[0],
            policy: { ...policy, group_id: 5 },
          },
        ],
      },
      {
        profile: { ...profileResponse().profile, active_group_id: 5 },
        groups: [profileResponse().groups[0]],
      },
      {
        ...profileResponse(),
        groups: [
          profileResponse().groups[0],
          { ...secondGroup, group: { ...secondGroup.group, id: 4 } },
        ],
      },
      {
        ...profileResponse(),
        groups: [
          profileResponse().groups[0],
          {
            ...secondGroup,
            entries: [{ ...entry, id: 9, group_id: 5, channel_id: 102 }],
          },
        ],
      },
      {
        ...profileResponse(),
        groups: [
          profileResponse().groups[0],
          { ...secondGroup, entries: [{ ...entry, id: 10, group_id: 5 }] },
        ],
      },
      {
        ...profileResponse(),
        groups: [
          {
            ...profileResponse().groups[0],
            policy: { ...policy, load_balance: true },
            entries: [
              { ...entry, id: 10, channel_id: 102, position: 0, weight: 10 },
              { ...entry, id: 9, channel_id: 101, position: 0, weight: 100 },
            ],
          },
        ],
      },
    ]

    for (const invalid of invalidResponses) {
      expect(() =>
        parseRouteProfileView(invalid, '/api/routing/profiles')
      ).toThrowError()
    }
  })
})
