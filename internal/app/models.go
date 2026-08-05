package app

import "time"

type User struct {
	ID           string
	Username     string
	PasswordHash string
}

type Stream struct {
	ID            string    `json:"id"`
	Name          string    `json:"name"`
	Mode          string    `json:"mode"`
	Host          string    `json:"host"`
	Port          int       `json:"port"`
	StreamID      string    `json:"streamId"`
	LatencyMS     int       `json:"latencyMs"`
	TimeoutMS     int       `json:"timeoutMs"`
	HasPassphrase bool      `json:"hasPassphrase"`
	AutoMP4       bool      `json:"autoMp4"`
	CreatedAt     time.Time `json:"createdAt"`
	UpdatedAt     time.Time `json:"updatedAt"`
}

type StreamSecret struct {
	Stream
	PassphraseEnc string
}

type MediaFile struct {
	ID          string         `json:"id"`
	RecordingID string         `json:"recordingId"`
	Kind        string         `json:"kind"`
	Status      string         `json:"status"`
	Path        string         `json:"-"`
	Name        string         `json:"name"`
	SizeBytes   int64          `json:"sizeBytes"`
	DurationMS  int64          `json:"durationMs"`
	Codecs      map[string]any `json:"codecs"`
	CreatedAt   time.Time      `json:"createdAt"`
}

type MP4Job struct {
	ID          string     `json:"id"`
	RecordingID string     `json:"recordingId"`
	Status      string     `json:"status"`
	Progress    float64    `json:"progress"`
	ErrorText   string     `json:"error"`
	CreatedAt   time.Time  `json:"createdAt"`
	StartedAt   *time.Time `json:"startedAt,omitempty"`
	EndedAt     *time.Time `json:"endedAt,omitempty"`
}

type Recording struct {
	ID              string      `json:"id"`
	StreamID        string      `json:"streamId"`
	StreamName      string      `json:"streamName"`
	AutoMP4         bool        `json:"autoMp4"`
	Status          string      `json:"status"`
	EndReason       string      `json:"endReason"`
	RequestedAt     time.Time   `json:"requestedAt"`
	StartedAt       *time.Time  `json:"startedAt,omitempty"`
	EndedAt         *time.Time  `json:"endedAt,omitempty"`
	ProgressMS      int64       `json:"progressMs"`
	ProgressSize    int64       `json:"progressSize"`
	ProgressBitrate string      `json:"progressBitrate"`
	LastProgressAt  *time.Time  `json:"lastProgressAt,omitempty"`
	ErrorText       string      `json:"error"`
	Files           []MediaFile `json:"files"`
	MP4Job          *MP4Job     `json:"mp4Job,omitempty"`
}

type WorkerCommand struct {
	ID       int64
	Kind     string
	TargetID string
	Payload  []byte
}

type WorkerHeartbeat struct {
	WorkerID          string    `json:"workerId"`
	Status            string    `json:"status"`
	ActiveRecordings  int       `json:"activeRecordings"`
	ActiveConversions int       `json:"activeConversions"`
	DiskTotalBytes    int64     `json:"diskTotalBytes"`
	DiskFreeBytes     int64     `json:"diskFreeBytes"`
	Version           string    `json:"version"`
	LastSeenAt        time.Time `json:"lastSeenAt"`
}

type SystemSettings struct {
	MP4Concurrency  int     `json:"mp4Concurrency"`
	SoftFreePercent float64 `json:"softFreePercent"`
	SoftFreeGiB     uint64  `json:"softFreeGiB"`
	HardFreePercent float64 `json:"hardFreePercent"`
	HardFreeGiB     uint64  `json:"hardFreeGiB"`
}

type DashboardSnapshot struct {
	Streams        []Stream          `json:"streams"`
	Recordings     []Recording       `json:"recordings"`
	Workers        []WorkerHeartbeat `json:"workers"`
	Settings       SystemSettings    `json:"settings"`
	ServerTime     time.Time         `json:"serverTime"`
	ActiveCount    int               `json:"activeCount"`
	RecordingCount int               `json:"recordingCount"`
	QueuedMP4      int               `json:"queuedMp4"`
	FailedLast24h  int               `json:"failedLast24h"`
}
