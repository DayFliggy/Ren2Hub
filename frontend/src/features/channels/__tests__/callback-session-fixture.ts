import type { AuthBundle } from '@/types/auth'

export function callbackSession(
  sid = 'callback-session',
  userId = 1
): AuthBundle {
  return {
    access_token: 'test-access',
    token_type: 'Bearer',
    access_expires_at: 2_000_000_000,
    session: {
      sid,
      current: true,
      login_method: 'password',
      ip: '127.0.0.1',
      user_agent: 'vitest',
      created_at: 1,
      last_active_at: 2,
      expires_at: 2_000_000_000,
    },
    user: {
      id: userId,
      username: 'callback-test',
      display_name: 'Callback Test',
      email: '',
      role: 100,
      status: 1,
      quota: 0,
      used_quota: 0,
      request_count: 0,
    },
  }
}
