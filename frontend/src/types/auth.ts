export interface UserInfo {
  id: number
  username: string
  display_name: string
  email: string
  role: number
  quota: number
  used_quota: number
  group: string
  admin_permissions?: string[]
}
