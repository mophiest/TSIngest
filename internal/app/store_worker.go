package app

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"

	"github.com/google/uuid"
)

func (s *Store) ResetStaleWork(ctx context.Context, workerID string) error {
	_, err := s.DB.ExecContext(ctx, `UPDATE worker_commands SET status='pending',worker_id='',claimed_at=NULL WHERE status='claimed' AND (worker_id=$1 OR claimed_at<now()-interval '2 minutes')`, workerID)
	return err
}

func (s *Store) ClaimCommand(ctx context.Context, workerID string) (WorkerCommand, error) {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return WorkerCommand{}, err
	}
	defer tx.Rollback()
	var cmd WorkerCommand
	err = tx.QueryRowContext(ctx, `SELECT id,kind,target_id,payload FROM worker_commands WHERE status='pending' ORDER BY id FOR UPDATE SKIP LOCKED LIMIT 1`).
		Scan(&cmd.ID, &cmd.Kind, &cmd.TargetID, &cmd.Payload)
	if err != nil {
		return WorkerCommand{}, err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE worker_commands SET status='claimed',worker_id=$2,claimed_at=now() WHERE id=$1`, cmd.ID, workerID); err != nil {
		return WorkerCommand{}, err
	}
	return cmd, tx.Commit()
}

func (s *Store) CompleteCommand(ctx context.Context, id int64, commandErr error) {
	if commandErr == nil {
		_, _ = s.DB.ExecContext(ctx, `UPDATE worker_commands SET status='done',completed_at=now() WHERE id=$1`, id)
	} else {
		_, _ = s.DB.ExecContext(ctx, `UPDATE worker_commands SET status='failed',error_text=$2,completed_at=now() WHERE id=$1`, id, commandErr.Error())
	}
}

func (s *Store) RequeueCommand(ctx context.Context, id int64) {
	_, _ = s.DB.ExecContext(ctx, `UPDATE worker_commands SET status='pending',worker_id='',claimed_at=NULL WHERE id=$1`, id)
}

func (s *Store) SetRecordingWorkPath(ctx context.Context, id, path string) error {
	_, err := s.DB.ExecContext(ctx, `UPDATE recordings SET working_path=$2,updated_at=now() WHERE id=$1`, id, path)
	return err
}

func (s *Store) SetRecordingStarted(ctx context.Context, id string) error {
	_, err := s.DB.ExecContext(ctx, `UPDATE recordings SET status='recording',started_at=COALESCE(started_at,now()),last_progress_at=now(),updated_at=now() WHERE id=$1`, id)
	return err
}

func (s *Store) UpdateRecordingProgress(ctx context.Context, id string, progressMS, size int64, bitrate string) error {
	_, err := s.DB.ExecContext(ctx, `UPDATE recordings SET progress_ms=$2,progress_size=$3,progress_bitrate=$4,last_progress_at=now(),updated_at=now() WHERE id=$1`, id, progressMS, size, bitrate)
	return err
}

func (s *Store) SetRecordingFinalizing(ctx context.Context, id, reason string) error {
	_, err := s.DB.ExecContext(ctx, `UPDATE recordings SET status='finalizing',end_reason=$2,updated_at=now() WHERE id=$1`, id, reason)
	return err
}

func (s *Store) FinishRecording(ctx context.Context, id, status, reason, errorText string) error {
	_, err := s.DB.ExecContext(ctx, `UPDATE recordings SET status=$2,end_reason=$3,error_text=$4,ended_at=now(),updated_at=now() WHERE id=$1`, id, status, reason, errorText)
	return err
}

func (s *Store) AddMediaFile(ctx context.Context, recordingID, kind, path string, size, duration int64, codecs any) error {
	data, _ := json.Marshal(codecs)
	_, err := s.DB.ExecContext(ctx, `INSERT INTO media_files(id,recording_id,kind,status,path,size_bytes,duration_ms,codecs) VALUES($1,$2,$3,'ready',$4,$5,$6,$7)
		ON CONFLICT(recording_id,kind) DO UPDATE SET status='ready',path=excluded.path,size_bytes=excluded.size_bytes,duration_ms=excluded.duration_ms,codecs=excluded.codecs,updated_at=now()`,
		uuid.NewString(), recordingID, kind, path, size, duration, data)
	return err
}

func (s *Store) MediaFileByKind(ctx context.Context, recordingID, kind string) (MediaFile, error) {
	var file MediaFile
	var codecs []byte
	err := s.DB.QueryRowContext(ctx, `SELECT id,recording_id,kind,status,path,size_bytes,duration_ms,codecs,created_at FROM media_files WHERE recording_id=$1 AND kind=$2 AND status='ready'`, recordingID, kind).
		Scan(&file.ID, &file.RecordingID, &file.Kind, &file.Status, &file.Path, &file.SizeBytes, &file.DurationMS, &codecs, &file.CreatedAt)
	if err == nil {
		_ = json.Unmarshal(codecs, &file.Codecs)
	}
	return file, err
}

func (s *Store) SetMP4Running(ctx context.Context, id, outputPath string) error {
	_, err := s.DB.ExecContext(ctx, `UPDATE mp4_jobs SET status='running',output_path=$2,progress=0,started_at=now(),updated_at=now() WHERE id=$1`, id, outputPath)
	return err
}

func (s *Store) UpdateMP4Progress(ctx context.Context, id string, progress float64) error {
	_, err := s.DB.ExecContext(ctx, `UPDATE mp4_jobs SET progress=$2,updated_at=now() WHERE id=$1`, id, progress)
	return err
}

func (s *Store) FinishMP4(ctx context.Context, id, status, errorText string) error {
	_, err := s.DB.ExecContext(ctx, `UPDATE mp4_jobs SET status=$2,error_text=$3,progress=CASE WHEN $2='ready' THEN 100 ELSE progress END,ended_at=now(),updated_at=now() WHERE id=$1`, id, status, errorText)
	return err
}

func (s *Store) DeleteMediaFileRecord(ctx context.Context, recordingID, kind string) error {
	result, err := s.DB.ExecContext(ctx, `UPDATE media_files SET status='deleted',updated_at=now() WHERE recording_id=$1 AND kind=$2 AND status='ready'`, recordingID, kind)
	if err != nil {
		return err
	}
	if count, _ := result.RowsAffected(); count == 0 {
		return sql.ErrNoRows
	}
	return nil
}

func (s *Store) ActiveRecordingsForRecovery(ctx context.Context) ([]struct{ ID, Path string }, error) {
	rows, err := s.DB.QueryContext(ctx, `
		SELECT id,working_path FROM recordings WHERE status IN ('waiting_input','recording','finalizing')
		UNION ALL
		SELECT r.id,r.working_path FROM recordings r
		WHERE r.status='failed'
		  AND r.error_text='TS中未检测到H.264视频'
		  AND r.working_path<>''
		  AND NOT EXISTS (
		    SELECT 1 FROM media_files mf
		    WHERE mf.recording_id=r.id AND mf.kind='ts' AND mf.status='ready'
		  )`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]struct{ ID, Path string }, 0)
	for rows.Next() {
		var item struct{ ID, Path string }
		if err := rows.Scan(&item.ID, &item.Path); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *Store) ActiveRecordingStream(ctx context.Context, recordingID string) (string, bool, error) {
	var streamID string
	var autoMP4 bool
	err := s.DB.QueryRowContext(ctx, `SELECT stream_id,auto_mp4 FROM recordings WHERE id=$1`, recordingID).Scan(&streamID, &autoMP4)
	return streamID, autoMP4, err
}

func (s *Store) MP4JobContext(ctx context.Context, jobID string) (MP4Job, Recording, error) {
	job, err := s.GetMP4Job(ctx, jobID)
	if err != nil {
		return MP4Job{}, Recording{}, err
	}
	rec, err := s.GetRecording(ctx, job.RecordingID)
	return job, rec, err
}

func (s *Store) Heartbeat(ctx context.Context, hb WorkerHeartbeat) error {
	_, err := s.DB.ExecContext(ctx, `INSERT INTO worker_heartbeats(worker_id,status,active_recordings,active_conversions,disk_total_bytes,disk_free_bytes,version,last_seen_at)
		VALUES($1,$2,$3,$4,$5,$6,$7,now()) ON CONFLICT(worker_id) DO UPDATE SET status=excluded.status,active_recordings=excluded.active_recordings,active_conversions=excluded.active_conversions,disk_total_bytes=excluded.disk_total_bytes,disk_free_bytes=excluded.disk_free_bytes,version=excluded.version,last_seen_at=now()`,
		hb.WorkerID, hb.Status, hb.ActiveRecordings, hb.ActiveConversions, hb.DiskTotalBytes, hb.DiskFreeBytes, hb.Version)
	return err
}

func (s *Store) QueueAutoMP4(ctx context.Context, recordingID string) error {
	_, err := s.QueueMP4(ctx, recordingID)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	return nil
}
