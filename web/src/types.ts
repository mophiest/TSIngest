export type StreamMode = 'listener' | 'caller'
export type RecordingStatus = 'waiting_input' | 'recording' | 'finalizing' | 'ready' | 'failed'

export interface Stream {
  id: string
  name: string
  mode: StreamMode
  host: string
  port: number
  streamId: string
  latencyMs: number
  timeoutMs: number
  hasPassphrase: boolean
  autoMp4: boolean
  createdAt: string
  updatedAt: string
  deletedAt?: string
}

export interface MediaFile {
  id: string
  recordingId: string
  kind: 'ts' | 'mp4'
  status: string
  name: string
  sizeBytes: number
  durationMs: number
  codecs: {
    video?: Array<{ codec: string; profile: string; width: number; height: number }>
    audio?: Array<{ codec: string; channels: number; sampleRate: string; language?: string }>
  }
  createdAt: string
}

export interface MP4Job {
  id: string
  recordingId: string
  status: 'queued' | 'running' | 'ready' | 'failed'
  progress: number
  error: string
  createdAt: string
}

export interface Recording {
  id: string
  streamId: string
  streamName: string
  autoMp4: boolean
  status: RecordingStatus
  endReason: string
  requestedAt: string
  startedAt?: string
  endedAt?: string
  progressMs: number
  progressSize: number
  progressBitrate: string
  lastProgressAt?: string
  error: string
  files: MediaFile[]
  mp4Job?: MP4Job
}

export interface WorkerHeartbeat {
  workerId: string
  status: string
  activeRecordings: number
  activeConversions: number
  diskTotalBytes: number
  diskFreeBytes: number
  version: string
  lastSeenAt: string
}

export interface Settings {
  mp4Concurrency: number
  softFreePercent: number
  softFreeGiB: number
  hardFreePercent: number
  hardFreeGiB: number
}

export interface Dashboard {
  streams: Stream[]
  recordings: Recording[]
  workers: WorkerHeartbeat[]
  settings: Settings
  serverTime: string
  activeCount: number
  recordingCount: number
  queuedMp4: number
  failedLast24h: number
}
