export interface LoginSessionLabels {
  unknown: string
  password: string
  twoFactor: string
  passkey: string
  wechat: string
  telegram: string
  oauth: string
}

export function sessionDevice(
  userAgent: string,
  unknownDevice: string,
  browserLabel: string,
  maxTouchPoints = 0
): string {
  if (!userAgent) return unknownDevice
  let browser = browserLabel
  if (userAgent.includes('Edg/')) browser = 'Edge'
  else if (userAgent.includes('Chrome/')) browser = 'Chrome'
  else if (userAgent.includes('Firefox/')) browser = 'Firefox'
  else if (userAgent.includes('Safari/')) browser = 'Safari'

  const isIPad =
    userAgent.includes('iPad') ||
    (userAgent.includes('Macintosh') && maxTouchPoints > 1)
  let system = ''
  if (userAgent.includes('iPhone') || isIPad) system = 'iOS'
  else if (userAgent.includes('Android')) system = 'Android'
  else if (userAgent.includes('Windows')) system = 'Windows'
  else if (userAgent.includes('Mac OS')) system = 'macOS'
  else if (userAgent.includes('Linux')) system = 'Linux'
  return system ? `${browser} · ${system}` : browser
}

export function loginMethodLabel(
  method: string,
  labels: LoginSessionLabels
): string {
  const normalized = method.trim().toLowerCase()
  switch (normalized) {
    case 'password':
      return labels.password
    case '2fa':
      return labels.twoFactor
    case 'passkey':
      return labels.passkey
    case 'wechat':
      return labels.wechat
    case 'telegram':
      return labels.telegram
    case 'oauth':
      return labels.oauth
    case 'unknown':
    case '':
      return labels.unknown
    default:
      break
  }
  if (!normalized.startsWith('oauth:')) return method
  const provider = normalized.slice('oauth:'.length)
  const providerNames: Record<string, string> = {
    discord: 'Discord',
    github: 'GitHub',
    linuxdo: 'LinuxDO',
    oidc: 'OIDC',
  }
  return `${labels.oauth} · ${providerNames[provider] || provider}`
}
