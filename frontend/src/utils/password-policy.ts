export const ACCOUNT_PASSWORD_MIN_LENGTH = 8
export const ACCOUNT_PASSWORD_MAX_LENGTH = 128
export function countPasswordCharacters(password: string): number {
  return [...password].length
}
export function isAccountPasswordLengthValid(password: string): boolean {
  const length = countPasswordCharacters(password)
  return (
    length >= ACCOUNT_PASSWORD_MIN_LENGTH &&
    length <= ACCOUNT_PASSWORD_MAX_LENGTH
  )
}
