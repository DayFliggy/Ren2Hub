import { flushPromises, mount } from '@vue/test-utils'
import { beforeAll, describe, expect, it, vi } from 'vitest'

import UserPermissionsEditor from '../UserPermissionsEditor.vue'
import i18n, { loadMessageDomain, setLocale } from '@/i18n'

const mocks = vi.hoisted(() => ({ get: vi.fn() }))
vi.mock('@/api/client', () => ({ api: mocks }))

// The data of GET /api/authz/catalog (controller/authz.go): authz.Catalog()
// resources, cut to two, and authz.Roles(). Action JSON carries no default roles.
const catalog = {
  resources: [
    {
      resource: 'channel',
      label_key: 'Channel Management',
      actions: [
        {
          action: 'read',
          label_key: 'Read channels',
          description_key: 'View channel lists and details without secrets.',
        },
        {
          action: 'secret_view',
          label_key: 'View channel secrets',
          description_key:
            'Reserved for viewing complete channel keys after secure verification.',
        },
      ],
    },
    {
      resource: 'ticket',
      label_key: 'Ticket management',
      actions: [
        {
          action: 'manage',
          label_key: 'Manage tickets',
          description_key: 'Assign, transfer, close, and reopen tickets.',
        },
      ],
    },
  ],
  roles: [
    {
      key: 'admin',
      name: 'Admin',
      built_in: true,
      superuser: false,
      grants: {
        channel: { read: true, secret_view: false },
        ticket: { manage: true },
      },
    },
  ],
}

beforeAll(async () => {
  await loadMessageDomain('console')
  await setLocale('zh-CN')
})

describe('UserPermissionsEditor', () => {
  it('shows zh-CN labels and descriptions and edits by resource and action', async () => {
    mocks.get.mockResolvedValue(catalog)
    const wrapper = mount(UserPermissionsEditor, {
      props: { modelValue: { channel: { read: true } } },
      global: { plugins: [i18n] },
    })
    await flushPromises()
    expect(mocks.get).toHaveBeenCalledWith('/api/authz/catalog')
    const text = wrapper.text()
    for (const translated of [
      '渠道管理',
      '读取渠道',
      '查看渠道密钥',
      '预留用于在安全验证后查看完整渠道密钥。',
      '工单管理',
      '分配、转交、关闭和重新打开工单。',
    ])
      expect(text).toContain(translated)
    expect(text).not.toMatch(/channel|ticket|View channel secrets/i)

    const boxes = wrapper.findAll('input[type="checkbox"]')
    expect(
      boxes.map((box) => (box.element as HTMLInputElement).checked)
    ).toEqual([true, false, false])
    await boxes[1].setValue(true)
    expect(wrapper.emitted('update:modelValue')?.at(-1)).toEqual([
      { channel: { read: true, secret_view: true } },
    ])
    wrapper.unmount()
  })
})
