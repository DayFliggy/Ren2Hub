// Mirrors upstream lib/plugin-icon.ts. The text-avatar palette maps to
// Ren2Hub's soft/text token pairs, which already hold in light and dark.

export type PluginIconDescriptor =
  | { kind: 'lobe'; name: string }
  | { kind: 'image'; src: string }
  | { kind: 'text'; label: string; colorSeed: string }

/** Where the gateway serves a plugin's sidecar logo. */
export function pluginIconUrl(key: string, version?: string): string {
  const base = `/api/plugin/task/${encodeURIComponent(key)}/icon`
  return version ? `${base}?version=${encodeURIComponent(version)}` : base
}

export type PluginIconInput = {
  icon?: string
  key: string
  name?: string
  /** True when the gateway holds a sidecar logo for this plugin. */
  hasIcon?: boolean
  /** Direct image source (object URL or data URI). */
  iconSrc?: string
}

/**
 * Priority: shipped image logo, then `meta.icon` (a LobeHub name or the
 * `text` / `text:<label>` scheme), then a text avatar from the name. Data URIs
 * and remote URLs in `meta.icon` are ignored; logos ship as sidecar files.
 */
export function resolvePluginIcon(
  input: PluginIconInput
): PluginIconDescriptor {
  if (input.iconSrc) return { kind: 'image', src: input.iconSrc }
  const icon = input.icon?.trim()
  if (icon) {
    if (icon === 'text' || icon.startsWith('text:')) {
      const explicit = icon.startsWith('text:') ? icon.slice(5).trim() : ''
      return {
        kind: 'text',
        label: explicit ? [...explicit].slice(0, 4).join('') : textLabel(input),
        colorSeed: input.key,
      }
    }
    if (!icon.startsWith('data:') && !icon.includes('://'))
      return { kind: 'lobe', name: icon }
  }
  return { kind: 'text', label: textLabel(input), colorSeed: input.key }
}

function textLabel(input: PluginIconInput): string {
  const source = input.name?.trim() || input.key.trim()
  return [...source].slice(0, 2).join('').toUpperCase()
}

const TEXT_AVATAR_TONES = [
  'accent',
  'success',
  'info',
  'warning',
  'danger',
] as const

/** Deterministic: the same key always renders the same tone. */
export function textAvatarTone(
  colorSeed: string
): (typeof TEXT_AVATAR_TONES)[number] {
  let hash = 0
  for (let i = 0; i < colorSeed.length; i++) {
    hash = (hash * 31 + colorSeed.charCodeAt(i)) | 0
  }
  return TEXT_AVATAR_TONES[Math.abs(hash) % TEXT_AVATAR_TONES.length]!
}
