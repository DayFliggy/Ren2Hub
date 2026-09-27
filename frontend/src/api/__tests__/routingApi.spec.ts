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
  max_ratio: '1',
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

const requestRequirements = {
  format: 'openai',
  model: 'gpt-5',
  stream: false,
  operation: 'text',
  native_required: false,
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
  it('preserves eighteen-place ceilings and rejects invalid or floating point ratios', () => {
    const response = profileResponse()
    const withRatio = (max_ratio: unknown) => ({
      ...response,
      groups: [{ ...response.groups[0], policy: { ...policy, max_ratio } }],
    })
    expect(
      parseRouteProfileView(withRatio('1.000000000000000001'), '/routing')
        .groups[0]?.policy.max_ratio
    ).toBe('1.000000000000000001')
    for (const invalid of [
      1,
      '0',
      '-1',
      '1000.000000000000000001',
      '0.0000000000000000001',
      'NaN',
      '1e3',
    ])
      expect(() =>
        parseRouteProfileView(withRatio(invalid), '/routing')
      ).toThrow()
  })
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
    const floatingPointRatio = {
      ...profileResponse(),
      groups: [
        {
          ...profileResponse().groups[0],
          policy: { ...policy, max_ratio: 1 },
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
      floatingPointRatio,
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
          diagnostics: [],
        },
      ],
      candidate_channel_ids: [],
      selection_mode: 'ordered',
      filter_reason_counts: { runtime_recheck_required: 1 },
      has_mixed: false,
      runtime_recheck_required: true,
      runtime_recheck_reasons: ['price_qualification'],
      live_selection: false,
      request_requirements: requestRequirements,
      conversion_policy: 'safe',
    })

    expect(parsed.live_selection).toBe(false)
    expect(parsed.entries[0].filter_reason).toBe('runtime_recheck_required')
    expect(parsed.entries[0].health.state).toBe('closed')
  })

  it('accepts automatic previews without profile or entry identities', () => {
    const parsed = parseRoutePreview({
      token_id: 11,
      mode: 'auto_lab',
      preview_id: 'snapshot',
      generated_at: 1789700000000,
      profile_id: 0,
      profile_version: 0,
      request_model: 'gpt-5',
      normalized_model: 'gpt-5',
      path: '/v1/chat/completions',
      endpoint_type: 'openai',
      entries: [202, 101].map((id) => ({
        entry_id: 0,
        channel_id: id,
        position: 0,
        priority: 2,
        weight: 10,
        request_model: 'gpt-5',
        actual_model: 'gpt-5',
        lab_slug: 'openai',
        snapshot_version: 1,
        catalog_version: 'fixture',
        capability_state: 'eligible',
        conversion_plan: {
          from: 'openai',
          to: 'openai',
          native: true,
          policy: 'safe',
          stream_supported: true,
        },
        diagnostics: [],
        health: {
          state: 'closed',
          failure_count: 0,
          cooldown_until: 0,
          health_epoch: 1,
          last_latency_ms: 0,
          first_token_latency_ms: 0,
          updated_at: 0,
        },
      })),
      candidate_channel_ids: [202, 101],
      preferred_channel_id: 202,
      selection_mode: 'weighted',
      filter_reason_counts: {},
      has_mixed: false,
      runtime_recheck_required: true,
      runtime_recheck_reasons: ['admission_capacity'],
      live_selection: false,
      request_requirements: requestRequirements,
      conversion_policy: 'safe',
    })
    expect(parsed.profile_id).toBe(0)
    expect(parsed.mode).toBe('auto_lab')
    expect(parsed.candidate_channel_ids).toEqual([202, 101])
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
          diagnostics: [],
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
      request_requirements: requestRequirements,
      conversion_policy: 'safe',
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

  it('preserves conversion losses and rejects executable plans that violate token policy', () => {
    const diagnostic = {
      code: 'serial_tools_not_portable',
      path: 'parallel_tool_calls',
      message: 'Gemini conversion cannot enforce serial tool execution',
      severity: 'error',
      from: 'openai',
      to: 'gemini',
    }
    const response = {
      profile_id: 1,
      profile_version: 3,
      request_model: 'gpt-5',
      normalized_model: 'gpt-5',
      path: '/v1/chat/completions',
      endpoint_type: 'openai',
      request_requirements: {
        ...requestRequirements,
        features: ['serial_tools'],
      },
      conversion_policy: 'safe',
      entries: [
        {
          entry_id: 9,
          channel_id: 101,
          position: 0,
          weight: 100,
          request_model: 'gpt-5',
          actual_model: 'gemini-model',
          lab_slug: 'google',
          snapshot_version: 2,
          catalog_version: 'catalog-2',
          capability_state: 'eligible',
          health: {
            state: 'closed',
            failure_count: 0,
            cooldown_until: 0,
            health_epoch: 1,
            last_latency_ms: 0,
            first_token_latency_ms: 0,
            updated_at: 0,
          },
          filter_reason: 'conversion_unavailable',
          diagnostics: [diagnostic],
          conversion_plan: {
            from: 'openai',
            to: 'gemini',
            native: false,
            policy: 'safe',
            stream_supported: true,
            request_converter: 'openai_to_gemini',
            response_converter: 'gemini_to_openai',
            steps: [
              { converter: 'openai_to_gemini', from: 'openai', to: 'gemini' },
            ],
            diagnostics: [diagnostic],
          },
        },
      ],
      candidate_channel_ids: [] as number[],
      selection_mode: 'ordered',
      filter_reason_counts: { conversion_unavailable: 1 },
      has_mixed: false,
      runtime_recheck_required: true,
      runtime_recheck_reasons: ['quota_qualification'],
      live_selection: false,
    }
    const parsed = parseRoutePreview(response)
    expect(parsed.entries[0].diagnostics[0]).toEqual(diagnostic)
    expect(parsed.request_requirements.features).toEqual(['serial_tools'])
    expect(parsed.entries[0].conversion_plan?.steps).toHaveLength(1)

    const projection = structuredClone(response)
    Object.assign(projection.entries[0].conversion_plan, {
      request_projection: 'coze_text',
    })
    expect(
      parseRoutePreview(projection).entries[0].conversion_plan
        ?.request_projection
    ).toBe('coze_text')

    const bypass = structuredClone(response)
    bypass.entries[0].filter_reason = ''
    bypass.candidate_channel_ids = [101]
    expect(() => parseRoutePreview(bypass)).toThrow('Invalid API response')

    const policyMismatch = structuredClone(response)
    policyMismatch.entries[0].conversion_plan.policy = 'allow'
    expect(() => parseRoutePreview(policyMismatch)).toThrow(
      'Invalid API response'
    )

    const unknownSeverity = structuredClone(response)
    unknownSeverity.entries[0].diagnostics[0].severity = 'silent'
    expect(() => parseRoutePreview(unknownSeverity)).toThrow(
      'Invalid API response'
    )

    bypass.conversion_policy = 'allow'
    bypass.entries[0].conversion_plan.policy = 'allow'
    expect(parseRoutePreview(bypass).candidate_channel_ids).toEqual([101])
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
