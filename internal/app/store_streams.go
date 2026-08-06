package app

import (
	"context"
	"database/sql"
	"errors"
	"net"
	"net/url"
	"strconv"
	"strings"

	"github.com/google/uuid"
)

type StreamInput struct {
	Name            string `json:"name"`
	Mode            string `json:"mode"`
	Host            string `json:"host"`
	Port            int    `json:"port"`
	StreamID        string `json:"streamId"`
	LatencyMS       int    `json:"latencyMs"`
	TimeoutMS       int    `json:"timeoutMs"`
	Passphrase      string `json:"passphrase"`
	ClearPassphrase bool   `json:"clearPassphrase"`
	AutoMP4         bool   `json:"autoMp4"`
	SourceURL       string `json:"sourceUrl"`
}

func ValidateStreamInput(in *StreamInput) error {
	if err := NormalizeStreamInput(in); err != nil {
		return err
	}
	in.Name = strings.TrimSpace(in.Name)
	in.Mode = strings.ToLower(strings.TrimSpace(in.Mode))
	in.Host = strings.TrimSpace(in.Host)
	in.StreamID = strings.TrimSpace(in.StreamID)
	if in.Name == "" || len([]rune(in.Name)) > 80 {
		return errors.New("名称不能为空且不能超过80个字符")
	}
	if in.Mode != "listener" && in.Mode != "caller" {
		return errors.New("连接模式必须是 listener 或 caller")
	}
	if in.Mode == "listener" && (in.Port < 9000 || in.Port > 9099) {
		return errors.New("Listener端口必须在9000–9099之间")
	}
	if in.Mode == "caller" && in.Host == "" {
		return errors.New("Caller模式必须填写上游主机")
	}
	if in.Port < 1 || in.Port > 65535 {
		return errors.New("端口无效")
	}
	if in.LatencyMS == 0 {
		in.LatencyMS = 200
	}
	if in.LatencyMS < 20 || in.LatencyMS > 8000 {
		return errors.New("SRT延迟必须在20–8000毫秒之间")
	}
	if in.TimeoutMS == 0 {
		in.TimeoutMS = 30000
	}
	if in.TimeoutMS < 5000 || in.TimeoutMS > 300000 {
		return errors.New("无数据超时必须在5–300秒之间")
	}
	if in.Passphrase != "" && (len(in.Passphrase) < 10 || len(in.Passphrase) > 79) {
		return errors.New("SRT口令长度必须为10–79个字符")
	}
	return nil
}

func NormalizeStreamInput(in *StreamInput) error {
	raw := strings.TrimSpace(in.SourceURL)
	if raw == "" {
		return nil
	}
	parsed, err := url.Parse(raw)
	if err != nil || !strings.EqualFold(parsed.Scheme, "srt") {
		return errors.New("SRT URL格式无效")
	}
	host := strings.TrimSpace(parsed.Hostname())
	portText := strings.TrimSpace(parsed.Port())
	if host == "" || portText == "" {
		return errors.New("SRT URL必须包含主机和端口")
	}
	port, err := strconv.Atoi(portText)
	if err != nil || port < 1 || port > 65535 {
		return errors.New("SRT URL端口无效")
	}
	query := parsed.Query()
	mode := strings.ToLower(firstQuery(query, "mode"))
	if mode == "" || mode == "caller" {
		mode = "caller"
	} else if mode == "listener" {
		return errors.New("直填上游SRT URL请使用可连接的Caller地址，不支持导入listener地址")
	} else {
		return errors.New("SRT URL mode 只支持 caller 或留空")
	}
	streamID := firstQuery(query, "streamid", "streamId")
	in.Mode = mode
	in.Host = host
	in.Port = port
	in.StreamID = streamID
	if strings.TrimSpace(in.Name) == "" {
		in.Name = StreamNameFromSRT(streamID, host, port)
	}
	return nil
}

func firstQuery(values url.Values, names ...string) string {
	for _, name := range names {
		if value := strings.TrimSpace(values.Get(name)); value != "" {
			return value
		}
	}
	for key, list := range values {
		for _, name := range names {
			if strings.EqualFold(key, name) && len(list) > 0 {
				return strings.TrimSpace(list[0])
			}
		}
	}
	return ""
}

func StreamNameFromSRT(streamID, host string, port int) string {
	streamID = strings.TrimSpace(streamID)
	if streamID != "" {
		parts := strings.Split(streamID, ":")
		for i := len(parts) - 1; i >= 0; i-- {
			if value := strings.TrimSpace(parts[i]); value != "" {
				return value
			}
		}
	}
	if net.ParseIP(host) != nil {
		return host + "-" + strconv.Itoa(port)
	}
	return strings.TrimSpace(host) + "-" + strconv.Itoa(port)
}

func (s *Store) ListStreams(ctx context.Context) ([]Stream, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT id,name,mode,host,port,stream_id,latency_ms,timeout_ms,has_passphrase,auto_mp4,created_at,updated_at,deleted_at FROM streams WHERE deleted_at IS NULL ORDER BY lower(name)`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]Stream, 0)
	for rows.Next() {
		var item Stream
		if err := rows.Scan(&item.ID, &item.Name, &item.Mode, &item.Host, &item.Port, &item.StreamID, &item.LatencyMS, &item.TimeoutMS, &item.HasPassphrase, &item.AutoMP4, &item.CreatedAt, &item.UpdatedAt, nullableTime(&item.DeletedAt)); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *Store) GetStream(ctx context.Context, id string) (Stream, error) {
	var item Stream
	err := s.DB.QueryRowContext(ctx, `SELECT id,name,mode,host,port,stream_id,latency_ms,timeout_ms,has_passphrase,auto_mp4,created_at,updated_at,deleted_at FROM streams WHERE id=$1 AND deleted_at IS NULL`, id).
		Scan(&item.ID, &item.Name, &item.Mode, &item.Host, &item.Port, &item.StreamID, &item.LatencyMS, &item.TimeoutMS, &item.HasPassphrase, &item.AutoMP4, &item.CreatedAt, &item.UpdatedAt, nullableTime(&item.DeletedAt))
	return item, err
}

func (s *Store) GetStreamSecret(ctx context.Context, id string) (StreamSecret, error) {
	var item StreamSecret
	err := s.DB.QueryRowContext(ctx, `SELECT id,name,mode,host,port,stream_id,latency_ms,timeout_ms,has_passphrase,auto_mp4,created_at,updated_at,deleted_at,passphrase_enc FROM streams WHERE id=$1 AND deleted_at IS NULL`, id).
		Scan(&item.ID, &item.Name, &item.Mode, &item.Host, &item.Port, &item.StreamID, &item.LatencyMS, &item.TimeoutMS, &item.HasPassphrase, &item.AutoMP4, &item.CreatedAt, &item.UpdatedAt, nullableTime(&item.DeletedAt), &item.PassphraseEnc)
	return item, err
}

func (s *Store) CreateStream(ctx context.Context, in StreamInput, encrypted string) (Stream, error) {
	id := uuid.NewString()
	hasPass := in.Passphrase != ""
	_, err := s.DB.ExecContext(ctx, `INSERT INTO streams(id,name,mode,host,port,stream_id,latency_ms,timeout_ms,passphrase_enc,has_passphrase,auto_mp4) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)`,
		id, in.Name, in.Mode, in.Host, in.Port, in.StreamID, in.LatencyMS, in.TimeoutMS, encrypted, hasPass, in.AutoMP4)
	if err != nil {
		return Stream{}, err
	}
	return s.GetStream(ctx, id)
}

func (s *Store) UpdateStream(ctx context.Context, id string, in StreamInput, encrypted *string) (Stream, error) {
	result, err := s.DB.ExecContext(ctx, `UPDATE streams SET name=$2,mode=$3,host=$4,port=$5,stream_id=$6,latency_ms=$7,timeout_ms=$8,auto_mp4=$9,updated_at=now() WHERE id=$1 AND deleted_at IS NULL`,
		id, in.Name, in.Mode, in.Host, in.Port, in.StreamID, in.LatencyMS, in.TimeoutMS, in.AutoMP4)
	if err != nil {
		return Stream{}, err
	}
	if encrypted != nil {
		_, err = s.DB.ExecContext(ctx, `UPDATE streams SET passphrase_enc=$2,has_passphrase=$3,updated_at=now() WHERE id=$1 AND deleted_at IS NULL`, id, *encrypted, in.Passphrase != "")
		if err != nil {
			return Stream{}, err
		}
	}
	if count, _ := result.RowsAffected(); count == 0 {
		return Stream{}, sql.ErrNoRows
	}
	return s.GetStream(ctx, id)
}

func (s *Store) DeleteStream(ctx context.Context, id string) error {
	var active int
	if err := s.DB.QueryRowContext(ctx, `SELECT count(*) FROM recordings WHERE stream_id=$1 AND status IN ('waiting_input','recording','finalizing')`, id).Scan(&active); err != nil {
		return err
	}
	if active > 0 {
		return errors.New("该流正在录制，不能删除")
	}
	result, err := s.DB.ExecContext(ctx, `UPDATE streams SET deleted_at=now(),updated_at=now() WHERE id=$1 AND deleted_at IS NULL`, id)
	if err != nil {
		return err
	}
	if count, _ := result.RowsAffected(); count == 0 {
		return sql.ErrNoRows
	}
	return nil
}
