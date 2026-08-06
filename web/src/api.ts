import type { Dashboard, Recording, Settings, Stream } from './types'

export class ApiError extends Error {
  status: number
  constructor(message: string, status: number) { super(message); this.status = status }
}

async function request<T>(path: string, options: RequestInit = {}): Promise<T> {
  const headers = new Headers(options.headers)
  if (options.body) headers.set('Content-Type', 'application/json')
  const response = await fetch(path, { ...options, headers, credentials: 'same-origin' })
  if (!response.ok) {
    let message = `请求失败 (${response.status})`
    try { message = (await response.json()).error || message } catch { /* ignore */ }
    throw new ApiError(message, response.status)
  }
  if (response.status === 204) return undefined as T
  return response.json()
}

export const api = {
  me: () => request<{id: string; username: string}>('/api/v1/auth/me'),
  login: (username: string, password: string) => request<{id: string; username: string}>('/api/v1/auth/login', { method: 'POST', body: JSON.stringify({ username, password }) }),
  logout: () => request('/api/v1/auth/logout', { method: 'POST' }),
  dashboard: () => request<Dashboard>('/api/v1/dashboard'),
  streams: () => request<Stream[]>('/api/v1/streams/'),
  createStream: (input: StreamForm) => request<Stream>('/api/v1/streams/', { method: 'POST', body: JSON.stringify(input) }),
  updateStream: (id: string, input: StreamForm) => request<Stream>(`/api/v1/streams/${id}`, { method: 'PUT', body: JSON.stringify(input) }),
  deleteStream: (id: string) => request(`/api/v1/streams/${id}`, { method: 'DELETE' }),
  startRecording: (streamId: string) => request<Recording>(`/api/v1/streams/${streamId}/recordings`, { method: 'POST' }),
  stopRecording: (id: string) => request<Recording>(`/api/v1/recordings/${id}/stop`, { method: 'POST' }),
  deleteRecording: (id: string) => request(`/api/v1/recordings/${id}`, { method: 'DELETE' }),
  generateMp4: (id: string) => request(`/api/v1/recordings/${id}/mp4`, { method: 'POST' }),
  deleteFile: (id: string, kind: 'ts' | 'mp4') => request(`/api/v1/recordings/${id}/files/${kind}?confirm=DELETE`, { method: 'DELETE' }),
  saveSettings: (settings: Settings) => request<Settings>('/api/v1/settings', { method: 'PUT', body: JSON.stringify(settings) }),
  changePassword: (current: string, next: string) => request('/api/v1/settings/password', { method: 'POST', body: JSON.stringify({ current, next }) }),
}

export interface StreamForm {
  name: string
  mode: 'listener' | 'caller'
  host: string
  port: number
  streamId: string
  latencyMs: number
  timeoutMs: number
  passphrase: string
  clearPassphrase: boolean
  autoMp4: boolean
  sourceUrl?: string
}
