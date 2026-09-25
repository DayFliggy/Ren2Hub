/** Backend business envelope: every /api response uses this shape. */
export interface ApiResponse<T> {
  success: boolean
  message?: string
  data: T
  total?: number
}

/**
 * Distinguishes transport-level HTTP errors from HTTP 200 responses
 * carrying { success: false } (integration contract §4 / §7).
 */
export class ApiError extends Error {
  readonly status?: number
  readonly code?: string
  /** true when the server answered 200 with success:false */
  readonly business: boolean
  /** Structured business-error details; callers must validate before use. */
  readonly data?: unknown

  constructor(
    message: string,
    options?: {
      status?: number
      code?: string
      business?: boolean
      cause?: unknown
      data?: unknown
    }
  ) {
    super(message)
    this.name = 'ApiError'
    this.status = options?.status
    this.code = options?.code
    this.business = options?.business ?? false
    this.data = options?.data
    if (options?.cause !== undefined) this.cause = options.cause
  }
}

export interface PageResult<T> {
  items: T[]
  total: number
  page: number
  pageSize: number
}

export interface TokenSecretResponse {
  key: string
}
