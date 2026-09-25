import { flushPromises, mount } from '@vue/test-utils'
import { beforeAll, describe, expect, it, vi } from 'vitest'
import SecurityProofDialog from '../SecurityProofDialog.vue'
import { securityApi, type SecurityProof } from '@/api/security'
import { publicApi } from '@/api/public'
import { api } from '@/api/client'
import * as passkey from '@/utils/passkey'
import i18n, { loadMessageDomain, setLocale } from '@/i18n'

beforeAll(async () => {
  await loadMessageDomain('console')
  await setLocale('en')
})

describe('security proof request lifecycle', () => {
  it('does not submit when every verification method is unavailable', async () => {
    vi.spyOn(publicApi, 'status').mockResolvedValue({})
    vi.spyOn(securityApi, 'requirements').mockResolvedValue({
      scope: 'access_token.generate',
      methods: [{ method: 'passkey', available: false, reason: 'disabled' }],
      oauth_providers: [],
      password_encryption_enabled: false,
    })
    const verify = vi.spyOn(securityApi, 'verify')
    const wrapper = mount(SecurityProofDialog, {
      global: {
        plugins: [i18n],
        stubs: {
          ConsoleModal: {
            props: ['open'],
            template: '<div v-if="open"><slot /></div>',
          },
        },
      },
    })
    const pending = wrapper.vm
      .request({ scope: 'access_token.generate' })
      .catch((cause: Error) => cause)
    await flushPromises()
    expect(
      wrapper.get('button[type=submit]').attributes('disabled')
    ).toBeDefined()
    await wrapper.get('form').trigger('submit')
    expect(verify).not.toHaveBeenCalled()
    wrapper.unmount()
    expect(await pending).toMatchObject({ name: 'AbortError' })
  })
  it.each(['wrong scope', 'expired'] as const)(
    'rejects a %s proof and permits verification retry',
    async (condition) => {
      vi.spyOn(publicApi, 'status').mockResolvedValue({})
      vi.spyOn(securityApi, 'requirements').mockResolvedValue({
        scope: 'access_token.generate',
        methods: [{ method: '2fa', available: true }],
        oauth_providers: [],
        password_encryption_enabled: false,
      })
      const proof: SecurityProof = {
        scope: 'access_token.generate',
        method: '2fa',
        proof_token: 'current-proof',
        expires_at: Date.now() / 1000 + 60,
      }
      vi.spyOn(securityApi, 'verify')
        .mockResolvedValueOnce({
          ...proof,
          ...(condition === 'expired'
            ? { expires_at: 1 }
            : { scope: 'access_token.revoke' }),
        })
        .mockResolvedValueOnce(proof)
      const wrapper = mount(SecurityProofDialog, {
        global: {
          plugins: [i18n],
          stubs: {
            ConsoleModal: {
              props: ['open'],
              template: '<div v-if="open"><slot /></div>',
            },
          },
        },
      })
      const pending = wrapper.vm.request({ scope: 'access_token.generate' })
      await flushPromises()
      await wrapper.get('input').setValue('123456')
      await wrapper.get('form').trigger('submit')
      await flushPromises()
      expect(wrapper.find('[role=alert]').exists()).toBe(true)
      expect(
        wrapper.get('button[type=submit]').attributes('disabled')
      ).toBeUndefined()
      await wrapper.get('form').trigger('submit')
      await expect(pending).resolves.toBe('current-proof')
      wrapper.unmount()
    }
  )
  it('does not open a credential prompt after its security request was disposed', async () => {
    vi.spyOn(publicApi, 'status').mockResolvedValue({})
    vi.spyOn(securityApi, 'requirements').mockResolvedValue({
      scope: 'access_token.generate',
      methods: [{ method: 'passkey', available: true }],
      oauth_providers: [],
      password_encryption_enabled: false,
    })
    let resolve!: (value: unknown) => void
    const post = vi.spyOn(api, 'post').mockReturnValue(
      new Promise((done) => {
        resolve = done
      })
    )
    const assertion = vi.spyOn(passkey, 'assertPasskey')
    const wrapper = mount(SecurityProofDialog, {
      global: {
        plugins: [i18n],
        stubs: {
          ConsoleModal: {
            props: ['open'],
            template: '<div v-if="open"><slot /></div>',
          },
        },
      },
    })
    const proof = wrapper.vm
      .request({ scope: 'access_token.generate' })
      .catch((cause: Error) => cause)
    await flushPromises()
    await wrapper.get('form').trigger('submit')
    wrapper.unmount()
    resolve({
      flow_token: 'old-flow',
      options: { publicKey: { challenge: 'YQ' } },
    })
    await flushPromises()
    expect(await proof).toMatchObject({ name: 'AbortError' })
    expect(assertion).not.toHaveBeenCalled()
    expect(post).toHaveBeenCalledTimes(1)
  })
  it('isolates pending verification from a replacement security operation', async () => {
    vi.spyOn(publicApi, 'status').mockResolvedValue({})
    vi.spyOn(securityApi, 'requirements').mockImplementation(async (scope) => ({
      scope,
      methods: [{ method: '2fa', available: true }],
      oauth_providers: [],
      password_encryption_enabled: false,
    }))
    let rejectFirst!: (error: Error) => void
    let resolveSecond!: (proof: SecurityProof) => void
    vi.spyOn(securityApi, 'verify')
      .mockReturnValueOnce(
        new Promise((_, reject) => {
          rejectFirst = reject
        })
      )
      .mockReturnValueOnce(
        new Promise((resolve) => {
          resolveSecond = resolve
        })
      )
    const wrapper = mount(SecurityProofDialog, {
      global: {
        plugins: [i18n],
        stubs: {
          ConsoleModal: {
            props: ['open'],
            template: '<div v-if="open"><slot /></div>',
          },
        },
      },
    })
    const first = wrapper.vm
      .request({ scope: 'access_token.generate' })
      .catch((cause: Error) => cause)
    await flushPromises()
    await wrapper.get('input').setValue('123456')
    await wrapper.get('form').trigger('submit')
    const second = wrapper.vm.request({ scope: 'access_token.revoke' })
    await flushPromises()
    expect(await first).toMatchObject({ name: 'AbortError' })
    await wrapper.get('input').setValue('654321')
    expect(
      wrapper.get('button[type=submit]').attributes('disabled')
    ).toBeUndefined()
    await wrapper.get('form').trigger('submit')
    rejectFirst(new Error('Old request failed'))
    await flushPromises()
    expect(wrapper.find('[role=alert]').exists()).toBe(false)
    expect(
      wrapper.get('button[type=submit]').attributes('disabled')
    ).toBeDefined()
    resolveSecond({
      scope: 'access_token.revoke',
      method: '2fa',
      proof_token: 'current-proof',
      expires_at: Date.now() / 1000 + 60,
    })
    await flushPromises()
    await expect(second).resolves.toBe('current-proof')
    wrapper.unmount()
  })
})
