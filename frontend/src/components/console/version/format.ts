/** Release tags arrive as `v1.0.1` from the check and `1.0.1` from the
 *  rollback list; both display as `v1.0.1`. Source builds keep their text. */
export function displayVersion(value: string) {
  return /^\d/.test(value) ? `v${value}` : value
}

/** The release date in the page locale, or '' for an unreadable value. */
export function publishedDate(value: string, locale: string) {
  const time = Date.parse(value)
  if (Number.isNaN(time)) return ''
  return new Intl.DateTimeFormat(locale, { dateStyle: 'medium' }).format(time)
}
