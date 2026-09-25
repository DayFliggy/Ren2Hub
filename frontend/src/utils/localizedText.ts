export function localizedText(
  value: string | Record<string, string> | undefined,
  locale: string
): string {
  if (typeof value === 'string') return value
  if (!value) return ''
  const entries = Object.entries(value).filter(
    ([, text]) => typeof text === 'string' && text.trim()
  )
  const texts = new Map(
    entries.map(([key, text]) => [key.toLowerCase().replaceAll('_', '-'), text])
  )
  const language = locale.toLowerCase().replaceAll('_', '-')
  for (const key of [language, language.split('-')[0]!, 'en']) {
    const result = texts.get(key)
    if (result) return result
  }
  const first = [...texts.keys()].sort()[0]
  return first ? texts.get(first)! : ''
}
