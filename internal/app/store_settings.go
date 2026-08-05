package app

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"
)

func (s *Store) GetSettings(ctx context.Context) (SystemSettings, error) {
	settings := SystemSettings{MP4Concurrency: 2, SoftFreePercent: 10, SoftFreeGiB: 100, HardFreePercent: 5, HardFreeGiB: 20}
	rows, err := s.DB.QueryContext(ctx, `SELECT key,value FROM settings`)
	if err != nil {
		return settings, err
	}
	defer rows.Close()
	for rows.Next() {
		var key string
		var raw []byte
		if err := rows.Scan(&key, &raw); err != nil {
			return settings, err
		}
		switch key {
		case "mp4_concurrency":
			_ = json.Unmarshal(raw, &settings.MP4Concurrency)
		case "soft_free_percent":
			_ = json.Unmarshal(raw, &settings.SoftFreePercent)
		case "soft_free_gib":
			_ = json.Unmarshal(raw, &settings.SoftFreeGiB)
		case "hard_free_percent":
			_ = json.Unmarshal(raw, &settings.HardFreePercent)
		case "hard_free_gib":
			_ = json.Unmarshal(raw, &settings.HardFreeGiB)
		}
	}
	return settings, rows.Err()
}

func ValidateSettings(settings SystemSettings) error {
	if settings.MP4Concurrency < 1 || settings.MP4Concurrency > 8 {
		return fmt.Errorf("MP4并发必须在1–8之间")
	}
	if settings.SoftFreePercent <= settings.HardFreePercent || settings.SoftFreePercent > 50 {
		return fmt.Errorf("软水位百分比必须大于硬水位且不超过50")
	}
	if settings.HardFreePercent < 1 || settings.HardFreePercent > 20 {
		return fmt.Errorf("硬水位百分比必须在1–20之间")
	}
	if settings.SoftFreeGiB <= settings.HardFreeGiB {
		return fmt.Errorf("软水位容量必须大于硬水位")
	}
	return nil
}

func (s *Store) SaveSettings(ctx context.Context, settings SystemSettings) error {
	if err := ValidateSettings(settings); err != nil {
		return err
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	values := map[string]any{
		"mp4_concurrency":   settings.MP4Concurrency,
		"soft_free_percent": settings.SoftFreePercent,
		"soft_free_gib":     settings.SoftFreeGiB,
		"hard_free_percent": settings.HardFreePercent,
		"hard_free_gib":     settings.HardFreeGiB,
	}
	for key, value := range values {
		data, _ := json.Marshal(value)
		if _, err := tx.ExecContext(ctx, `INSERT INTO settings(key,value,updated_at) VALUES($1,$2,now()) ON CONFLICT(key) DO UPDATE SET value=excluded.value,updated_at=now()`, key, data); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (s *Store) ListWorkers(ctx context.Context) ([]WorkerHeartbeat, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT worker_id,status,active_recordings,active_conversions,disk_total_bytes,disk_free_bytes,version,last_seen_at FROM worker_heartbeats ORDER BY worker_id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]WorkerHeartbeat, 0)
	for rows.Next() {
		var item WorkerHeartbeat
		if err := rows.Scan(&item.WorkerID, &item.Status, &item.ActiveRecordings, &item.ActiveConversions, &item.DiskTotalBytes, &item.DiskFreeBytes, &item.Version, &item.LastSeenAt); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *Store) Dashboard(ctx context.Context) (DashboardSnapshot, error) {
	streams, err := s.ListStreams(ctx)
	if err != nil {
		return DashboardSnapshot{}, err
	}
	recordings, err := s.ListRecordings(ctx, "", "", 50, 0)
	if err != nil {
		return DashboardSnapshot{}, err
	}
	workers, err := s.ListWorkers(ctx)
	if err != nil {
		return DashboardSnapshot{}, err
	}
	settings, err := s.GetSettings(ctx)
	if err != nil {
		return DashboardSnapshot{}, err
	}
	var active, recording, queued, failed int
	_ = s.DB.QueryRowContext(ctx, `SELECT count(*) FROM recordings WHERE status IN ('waiting_input','recording','finalizing')`).Scan(&active)
	_ = s.DB.QueryRowContext(ctx, `SELECT count(*) FROM recordings WHERE status='recording'`).Scan(&recording)
	_ = s.DB.QueryRowContext(ctx, `SELECT count(*) FROM mp4_jobs WHERE status='queued'`).Scan(&queued)
	_ = s.DB.QueryRowContext(ctx, `SELECT count(*) FROM recordings WHERE status='failed' AND updated_at>now()-interval '24 hours'`).Scan(&failed)
	return DashboardSnapshot{Streams: streams, Recordings: recordings, Workers: workers, Settings: settings, ServerTime: time.Now().UTC(), ActiveCount: active, RecordingCount: recording, QueuedMP4: queued, FailedLast24h: failed}, nil
}

func (s *Store) LatestWorker(ctx context.Context) (WorkerHeartbeat, error) {
	var item WorkerHeartbeat
	err := s.DB.QueryRowContext(ctx, `SELECT worker_id,status,active_recordings,active_conversions,disk_total_bytes,disk_free_bytes,version,last_seen_at FROM worker_heartbeats ORDER BY last_seen_at DESC LIMIT 1`).
		Scan(&item.WorkerID, &item.Status, &item.ActiveRecordings, &item.ActiveConversions, &item.DiskTotalBytes, &item.DiskFreeBytes, &item.Version, &item.LastSeenAt)
	return item, err
}

func (s *Store) Ready(ctx context.Context) error {
	var one int
	return s.DB.QueryRowContext(ctx, `SELECT 1`).Scan(&one)
}

func IsNotFound(err error) bool { return err == sql.ErrNoRows }
