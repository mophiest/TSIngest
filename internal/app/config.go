package app

import (
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	Role                  string
	ListenAddr            string
	DatabaseURL           string
	RecordingsRoot        string
	AdminUsername         string
	AdminPassword         string
	EncryptionKey         []byte
	PublicURL             string
	CookieSecure          bool
	WorkerID              string
	FFmpegPath            string
	FFprobePath           string
	MaxActiveRecordings   int
	DefaultMP4Concurrency int
	SoftFreePercent       float64
	SoftFreeBytes         uint64
	HardFreePercent       float64
	HardFreeBytes         uint64
	SessionIdle           time.Duration
	SessionMax            time.Duration
}

func LoadConfig() (Config, error) {
	host, _ := os.Hostname()
	cfg := Config{
		Role:                  env("TSINGEST_ROLE", "web"),
		ListenAddr:            env("TSINGEST_LISTEN_ADDR", ":8080"),
		DatabaseURL:           env("TSINGEST_DATABASE_URL", "postgres://tsingest:tsingest@postgres:5432/tsingest?sslmode=disable"),
		RecordingsRoot:        env("TSINGEST_RECORDINGS_ROOT", "/data/recordings"),
		AdminUsername:         env("TSINGEST_ADMIN_USERNAME", "admin"),
		AdminPassword:         os.Getenv("TSINGEST_ADMIN_PASSWORD"),
		PublicURL:             strings.TrimRight(os.Getenv("TSINGEST_PUBLIC_URL"), "/"),
		CookieSecure:          envBool("TSINGEST_COOKIE_SECURE", false),
		WorkerID:              env("TSINGEST_WORKER_ID", host),
		FFmpegPath:            env("TSINGEST_FFMPEG", "ffmpeg"),
		FFprobePath:           env("TSINGEST_FFPROBE", "ffprobe"),
		MaxActiveRecordings:   envInt("TSINGEST_MAX_ACTIVE_RECORDINGS", 64),
		DefaultMP4Concurrency: envInt("TSINGEST_MP4_CONCURRENCY", 2),
		SoftFreePercent:       envFloat("TSINGEST_SOFT_FREE_PERCENT", 10),
		SoftFreeBytes:         envGiB("TSINGEST_SOFT_FREE_GIB", 100),
		HardFreePercent:       envFloat("TSINGEST_HARD_FREE_PERCENT", 5),
		HardFreeBytes:         envGiB("TSINGEST_HARD_FREE_GIB", 20),
		SessionIdle:           24 * time.Hour,
		SessionMax:            7 * 24 * time.Hour,
	}
	if cfg.Role != "web" && cfg.Role != "worker" {
		return Config{}, fmt.Errorf("unsupported TSINGEST_ROLE %q", cfg.Role)
	}
	if cfg.AdminPassword == "" && cfg.Role == "web" {
		return Config{}, errors.New("TSINGEST_ADMIN_PASSWORD is required")
	}
	keyText := os.Getenv("TSINGEST_ENCRYPTION_KEY")
	if keyText == "" {
		return Config{}, errors.New("TSINGEST_ENCRYPTION_KEY is required (base64 encoded 32 bytes)")
	}
	key, err := base64.StdEncoding.DecodeString(keyText)
	if err != nil || len(key) != 32 {
		return Config{}, errors.New("TSINGEST_ENCRYPTION_KEY must be base64 encoded 32 bytes")
	}
	cfg.EncryptionKey = key
	return cfg, nil
}

func env(name, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(name)); value != "" {
		return value
	}
	return fallback
}

func envInt(name string, fallback int) int {
	value, err := strconv.Atoi(os.Getenv(name))
	if err != nil || value <= 0 {
		return fallback
	}
	return value
}

func envFloat(name string, fallback float64) float64 {
	value, err := strconv.ParseFloat(os.Getenv(name), 64)
	if err != nil || value < 0 || value > 100 {
		return fallback
	}
	return value
}

func envBool(name string, fallback bool) bool {
	value := os.Getenv(name)
	if value == "" {
		return fallback
	}
	parsed, err := strconv.ParseBool(value)
	if err != nil {
		return fallback
	}
	return parsed
}

func envGiB(name string, fallback uint64) uint64 {
	value, err := strconv.ParseUint(os.Getenv(name), 10, 64)
	if err != nil {
		value = fallback
	}
	return value * 1024 * 1024 * 1024
}
