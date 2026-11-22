import type { ApiResponse } from './types'

export type HttpMethod = 'GET' | 'POST' | 'PUT' | 'PATCH' | 'DELETE'

export interface RequestOptions {
  params?: Record<string, unknown>
  data?: unknown
  signal?: AbortSignal
  timeoutMs?: number
  headers?: Record<string, string>
}

export interface ApiTransport {
  request<T>(
    method: HttpMethod,
    url: string,
    options?: RequestOptions
  ): Promise<ApiResponse<T>>
  getBlob?(url: string, options?: RequestOptions): Promise<Blob>
  getStream?(
    url: string,
    options?: RequestOptions
  ): Promise<ReadableStream<Uint8Array>>
}
