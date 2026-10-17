export interface UserInfo {
  has_password?: boolean
  id: number
  username: string
  display_name: string
  email: string
  role: number
  status: number
  quota: number
  used_quota: number
  aff_count?: number
  aff_quota?: number
  aff_history_quota?: number
  aff_code?: string
  request_count: number
  group?: string
  github_id?: string
  discord_id?: string
  oidc_id?: string
  wechat_id?: string
  telegram_id?: string
  linux_do_id?: string
  permissions?: {
    admin_permissions?: Record<string, Record<string, boolean>>
    sidebar_settings?: boolean
    sidebar_modules?: Record<string, unknown>
  }
  setting?: string
}

export type UserProfilePatch = Partial<Pick<UserInfo, 'display_name' | 'email'>>

export interface LoginSession {
  sid: string
  current: boolean
  login_method: string
  ip: string
  user_agent: string
  created_at: number
  last_active_at: number
  expires_at: number
}

export interface LoginSessionRevokeResult {
  revoked_sid: string
  current: boolean
}

export interface LoginSessionBulkRevokeResult {
  revoked_count: number
}

export interface AuthTokenRotation {
  access_token: string
  token_type: 'Bearer'
  access_expires_at: number
  session: LoginSession
}

export interface AuthBundle extends AuthTokenRotation {
  user: UserInfo
}

export interface TwoFactorChallenge {
  require_verification: true
  methods: Array<{ method: string; available: boolean; reason?: string }>
  flow_token: string
  expires_at: number
}

export type LoginResponse = AuthBundle | TwoFactorChallenge
