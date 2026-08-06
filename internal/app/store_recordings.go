package app

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"
)

func (s *Store) StartRecording(ctx context.Context, streamID string, maxActive int) (Recording, error) {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return Recording{}, err
	}
	defer tx.Rollback()
	// Serialize the global active-count check so concurrent API requests cannot
	// collectively pass the configured single-host limit.
	if _, err = tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(hashtext('tsingest:active-recordings'))`); err != nil {
		return Recording{}, err
	}
	var streamName string
	var autoMP4 bool
	if err = tx.QueryRowContext(ctx, `SELECT name,auto_mp4 FROM streams WHERE id=$1 AND deleted_at IS NULL FOR SHARE`, streamID).Scan(&streamName, &autoMP4); err != nil {
		return Recording{}, err
	}
	var count int
	if err = tx.QueryRowContext(ctx, `SELECT count(*) FROM recordings WHERE status IN ('waiting_input','recording','finalizing')`).Scan(&count); err != nil {
		return Recording{}, err
	}
	if count >= maxActive {
		return Recording{}, fmt.Errorf("活动录制已达到上限 %d", maxActive)
	}
	id := uuid.NewString()
	if _, err = tx.ExecContext(ctx, `INSERT INTO recordings(id,stream_id,stream_name,auto_mp4,status) VALUES($1,$2,$3,$4,'waiting_input')`, id, streamID, streamName, autoMP4); err != nil {
		return Recording{}, err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO worker_commands(kind,target_id) VALUES('start_recording',$1)`, id); err != nil {
		return Recording{}, err
	}
	if err = tx.Commit(); err != nil {
		return Recording{}, err
	}
	s.Notify(ctx)
	return s.GetRecording(ctx, id)
}

func (s *Store) StopRecording(ctx context.Context, id string) (Recording, error) {
	recording, err := s.GetRecording(ctx, id)
	if err != nil {
		return Recording{}, err
	}
	if recording.Status != "waiting_input" && recording.Status != "recording" && recording.Status != "finalizing" {
		return recording, nil
	}
	var exists bool
	_ = s.DB.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM worker_commands WHERE kind='stop_recording' AND target_id=$1 AND status IN ('pending','claimed'))`, id).Scan(&exists)
	if !exists {
		_, err = s.DB.ExecContext(ctx, `INSERT INTO worker_commands(kind,target_id) VALUES('stop_recording',$1)`, id)
		if err != nil {
			return Recording{}, err
		}
		s.Notify(ctx)
	}
	return recording, nil
}

func (s *Store) QueueMP4(ctx context.Context, recordingID string) (MP4Job, error) {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return MP4Job{}, err
	}
	defer tx.Rollback()
	var status string
	if err = tx.QueryRowContext(ctx, `SELECT status FROM recordings WHERE id=$1 FOR SHARE`, recordingID).Scan(&status); err != nil {
		return MP4Job{}, err
	}
	if status != "ready" {
		return MP4Job{}, errors.New("TS尚未就绪")
	}
	var existing MP4Job
	err = tx.QueryRowContext(ctx, `SELECT id,recording_id,status,progress,error_text,created_at,started_at,ended_at FROM mp4_jobs WHERE recording_id=$1 AND status IN ('queued','running') ORDER BY created_at DESC LIMIT 1`, recordingID).
		Scan(&existing.ID, &existing.RecordingID, &existing.Status, &existing.Progress, &existing.ErrorText, &existing.CreatedAt, nullableTime(&existing.StartedAt), nullableTime(&existing.EndedAt))
	if err == nil {
		return existing, nil
	}
	if err != sql.ErrNoRows {
		return MP4Job{}, err
	}
	var readyMP4 bool
	if err = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM media_files WHERE recording_id=$1 AND kind='mp4' AND status='ready')`, recordingID).Scan(&readyMP4); err != nil {
		return MP4Job{}, err
	}
	if readyMP4 {
		return MP4Job{}, errors.New("MP4已经存在")
	}
	id := uuid.NewString()
	if _, err = tx.ExecContext(ctx, `INSERT INTO mp4_jobs(id,recording_id,status) VALUES($1,$2,'queued')`, id, recordingID); err != nil {
		return MP4Job{}, err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO worker_commands(kind,target_id) VALUES('generate_mp4',$1)`, id); err != nil {
		return MP4Job{}, err
	}
	if err = tx.Commit(); err != nil {
		return MP4Job{}, err
	}
	s.Notify(ctx)
	return s.GetMP4Job(ctx, id)
}

func (s *Store) QueueDeleteFile(ctx context.Context, recordingID, kind string) error {
	if kind != "ts" && kind != "mp4" {
		return errors.New("文件类型无效")
	}
	var status string
	if err := s.DB.QueryRowContext(ctx, `SELECT status FROM recordings WHERE id=$1`, recordingID).Scan(&status); err != nil {
		return err
	}
	if status == "waiting_input" || status == "recording" || status == "finalizing" {
		return errors.New("录制进行中，不能删除")
	}
	if kind == "ts" {
		var active int
		if err := s.DB.QueryRowContext(ctx, `SELECT count(*) FROM mp4_jobs WHERE recording_id=$1 AND status IN ('queued','running')`, recordingID).Scan(&active); err != nil {
			return err
		}
		if active > 0 {
			return errors.New("MP4处理中，不能删除TS")
		}
	}
	payload, _ := json.Marshal(map[string]string{"kind": kind})
	_, err := s.DB.ExecContext(ctx, `INSERT INTO worker_commands(kind,target_id,payload) VALUES('delete_file',$1,$2)`, recordingID, payload)
	if err == nil {
		s.Notify(ctx)
	}
	return err
}

func (s *Store) ListRecordings(ctx context.Context, streamID, status string, limit, offset int) ([]Recording, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	args := []any{}
	where := []string{"hidden_at IS NULL"}
	if streamID != "" {
		args = append(args, streamID)
		where = append(where, fmt.Sprintf("stream_id=$%d", len(args)))
	}
	if status != "" {
		args = append(args, status)
		where = append(where, fmt.Sprintf("status=$%d", len(args)))
	}
	args = append(args, limit, offset)
	query := `SELECT id,stream_id,stream_name,auto_mp4,status,end_reason,requested_at,started_at,ended_at,progress_ms,progress_size,progress_bitrate,last_progress_at,error_text FROM recordings WHERE ` + strings.Join(where, " AND ") + fmt.Sprintf(" ORDER BY created_at DESC LIMIT $%d OFFSET $%d", len(args)-1, len(args))
	rows, err := s.DB.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]Recording, 0)
	for rows.Next() {
		item, err := scanRecording(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for i := range items {
		_ = s.populateRecording(ctx, &items[i])
	}
	return items, nil
}

func (s *Store) GetRecording(ctx context.Context, id string) (Recording, error) {
	row := s.DB.QueryRowContext(ctx, `SELECT id,stream_id,stream_name,auto_mp4,status,end_reason,requested_at,started_at,ended_at,progress_ms,progress_size,progress_bitrate,last_progress_at,error_text FROM recordings WHERE id=$1 AND hidden_at IS NULL`, id)
	item, err := scanRecording(row)
	if err != nil {
		return Recording{}, err
	}
	if err = s.populateRecording(ctx, &item); err != nil {
		return Recording{}, err
	}
	return item, nil
}

func (s *Store) HideRecordingRecord(ctx context.Context, id string) error {
	var status string
	if err := s.DB.QueryRowContext(ctx, `SELECT status FROM recordings WHERE id=$1 AND hidden_at IS NULL`, id).Scan(&status); err != nil {
		return err
	}
	if status == "waiting_input" || status == "recording" || status == "finalizing" {
		return errors.New("录制进行中，不能删除记录")
	}
	var files int
	if err := s.DB.QueryRowContext(ctx, `SELECT count(*) FROM media_files WHERE recording_id=$1 AND status<>'deleted'`, id).Scan(&files); err != nil {
		return err
	}
	if status != "failed" && files > 0 {
		return errors.New("该录制已有媒体文件，请先删除文件")
	}
	if status != "failed" && files == 0 {
		var activeJobs int
		if err := s.DB.QueryRowContext(ctx, `SELECT count(*) FROM mp4_jobs WHERE recording_id=$1 AND status IN ('queued','running')`, id).Scan(&activeJobs); err != nil {
			return err
		}
		if activeJobs > 0 {
			return errors.New("MP4任务处理中，不能删除记录")
		}
	}
	result, err := s.DB.ExecContext(ctx, `UPDATE recordings SET hidden_at=now(),updated_at=now() WHERE id=$1 AND hidden_at IS NULL`, id)
	if err != nil {
		return err
	}
	if count, _ := result.RowsAffected(); count == 0 {
		return sql.ErrNoRows
	}
	return nil
}

type rowScanner interface{ Scan(...any) error }

func scanRecording(row rowScanner) (Recording, error) {
	var item Recording
	var started, ended, last sql.NullTime
	err := row.Scan(&item.ID, &item.StreamID, &item.StreamName, &item.AutoMP4, &item.Status, &item.EndReason, &item.RequestedAt, &started, &ended, &item.ProgressMS, &item.ProgressSize, &item.ProgressBitrate, &last, &item.ErrorText)
	if started.Valid {
		item.StartedAt = &started.Time
	}
	if ended.Valid {
		item.EndedAt = &ended.Time
	}
	if last.Valid {
		item.LastProgressAt = &last.Time
	}
	return item, err
}

func (s *Store) populateRecording(ctx context.Context, item *Recording) error {
	rows, err := s.DB.QueryContext(ctx, `SELECT id,recording_id,kind,status,path,size_bytes,duration_ms,codecs,created_at FROM media_files WHERE recording_id=$1 AND status<>'deleted' ORDER BY kind`, item.ID)
	if err != nil {
		return err
	}
	defer rows.Close()
	item.Files = make([]MediaFile, 0)
	for rows.Next() {
		var file MediaFile
		var codecs []byte
		if err := rows.Scan(&file.ID, &file.RecordingID, &file.Kind, &file.Status, &file.Path, &file.SizeBytes, &file.DurationMS, &codecs, &file.CreatedAt); err != nil {
			return err
		}
		file.Name = filepath.Base(file.Path)
		_ = json.Unmarshal(codecs, &file.Codecs)
		item.Files = append(item.Files, file)
	}
	var job MP4Job
	var started, ended sql.NullTime
	err = s.DB.QueryRowContext(ctx, `SELECT id,recording_id,status,progress,error_text,created_at,started_at,ended_at FROM mp4_jobs WHERE recording_id=$1 ORDER BY created_at DESC LIMIT 1`, item.ID).
		Scan(&job.ID, &job.RecordingID, &job.Status, &job.Progress, &job.ErrorText, &job.CreatedAt, &started, &ended)
	if err == nil {
		if started.Valid {
			job.StartedAt = &started.Time
		}
		if ended.Valid {
			job.EndedAt = &ended.Time
		}
		item.MP4Job = &job
	} else if err != sql.ErrNoRows {
		return err
	}
	return nil
}

func (s *Store) GetMP4Job(ctx context.Context, id string) (MP4Job, error) {
	var item MP4Job
	var started, ended sql.NullTime
	err := s.DB.QueryRowContext(ctx, `SELECT id,recording_id,status,progress,error_text,created_at,started_at,ended_at FROM mp4_jobs WHERE id=$1`, id).
		Scan(&item.ID, &item.RecordingID, &item.Status, &item.Progress, &item.ErrorText, &item.CreatedAt, &started, &ended)
	if started.Valid {
		item.StartedAt = &started.Time
	}
	if ended.Valid {
		item.EndedAt = &ended.Time
	}
	return item, err
}

func nullableTime(target **time.Time) any { return &nullTimeScanner{target: target} }

type nullTimeScanner struct{ target **time.Time }

func (n *nullTimeScanner) Scan(value any) error {
	if value == nil {
		*n.target = nil
		return nil
	}
	t, ok := value.(time.Time)
	if !ok {
		return fmt.Errorf("expected time.Time, got %T", value)
	}
	*n.target = &t
	return nil
}
