package app

import (
	"context"
	"database/sql"
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	_ "github.com/jackc/pgx/v5/stdlib"
)

//go:embed migrations/*.sql
var migrationFiles embed.FS

type Store struct {
	DB  *sql.DB
	Log *slog.Logger
}

func OpenStore(ctx context.Context, cfg Config, log *slog.Logger) (*Store, error) {
	db, err := sql.Open("pgx", cfg.DatabaseURL)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(30)
	db.SetMaxIdleConns(10)
	db.SetConnMaxLifetime(30 * time.Minute)
	deadline := time.Now().Add(60 * time.Second)
	for {
		if err = db.PingContext(ctx); err == nil {
			break
		}
		if time.Now().After(deadline) {
			db.Close()
			return nil, fmt.Errorf("database unavailable: %w", err)
		}
		select {
		case <-ctx.Done():
			db.Close()
			return nil, ctx.Err()
		case <-time.After(time.Second):
		}
	}
	store := &Store{DB: db, Log: log}
	if err := store.Migrate(ctx); err != nil {
		db.Close()
		return nil, err
	}
	return store, nil
}

func (s *Store) Migrate(ctx context.Context) error {
	if _, err := s.DB.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS schema_migrations (version text PRIMARY KEY, applied_at timestamptz NOT NULL DEFAULT now())`); err != nil {
		return err
	}
	entries, err := fs.ReadDir(migrationFiles, "migrations")
	if err != nil {
		return err
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name() < entries[j].Name() })
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".sql") {
			continue
		}
		var exists bool
		if err := s.DB.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM schema_migrations WHERE version=$1)`, entry.Name()).Scan(&exists); err != nil {
			return err
		}
		if exists {
			continue
		}
		body, err := migrationFiles.ReadFile("migrations/" + entry.Name())
		if err != nil {
			return err
		}
		tx, err := s.DB.BeginTx(ctx, nil)
		if err != nil {
			return err
		}
		if _, err = tx.ExecContext(ctx, string(body)); err != nil {
			tx.Rollback()
			return fmt.Errorf("migration %s: %w", entry.Name(), err)
		}
		if _, err = tx.ExecContext(ctx, `INSERT INTO schema_migrations(version) VALUES($1)`, entry.Name()); err != nil {
			tx.Rollback()
			return err
		}
		if err = tx.Commit(); err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) BootstrapAdmin(ctx context.Context, username, password string) error {
	var count int
	if err := s.DB.QueryRowContext(ctx, `SELECT count(*) FROM users`).Scan(&count); err != nil {
		return err
	}
	if count > 0 {
		return nil
	}
	hash, err := HashPassword(password)
	if err != nil {
		return err
	}
	_, err = s.DB.ExecContext(ctx, `INSERT INTO users(id,username,password_hash) VALUES($1,$2,$3)`, uuid.NewString(), username, hash)
	return err
}

func (s *Store) UserByUsername(ctx context.Context, username string) (User, error) {
	var user User
	err := s.DB.QueryRowContext(ctx, `SELECT id,username,password_hash FROM users WHERE lower(username)=lower($1)`, username).
		Scan(&user.ID, &user.Username, &user.PasswordHash)
	return user, err
}

func (s *Store) CreateSession(ctx context.Context, userID, tokenHash, ip, userAgent string, maxAge time.Duration) error {
	_, err := s.DB.ExecContext(ctx, `INSERT INTO sessions(token_hash,user_id,expires_at,ip,user_agent) VALUES($1,$2,now()+$3::interval,$4,$5)`,
		tokenHash, userID, fmt.Sprintf("%d seconds", int(maxAge.Seconds())), ip, userAgent)
	return err
}

func (s *Store) SessionUser(ctx context.Context, tokenHash string, idle time.Duration) (User, error) {
	var user User
	err := s.DB.QueryRowContext(ctx, `
		SELECT u.id,u.username,u.password_hash
		FROM sessions s JOIN users u ON u.id=s.user_id
		WHERE s.token_hash=$1 AND s.expires_at>now() AND s.last_seen_at>now()-$2::interval`,
		tokenHash, fmt.Sprintf("%d seconds", int(idle.Seconds()))).Scan(&user.ID, &user.Username, &user.PasswordHash)
	if err != nil {
		return user, err
	}
	_, _ = s.DB.ExecContext(ctx, `UPDATE sessions SET last_seen_at=now() WHERE token_hash=$1`, tokenHash)
	return user, nil
}

func (s *Store) DeleteSession(ctx context.Context, tokenHash string) error {
	_, err := s.DB.ExecContext(ctx, `DELETE FROM sessions WHERE token_hash=$1`, tokenHash)
	return err
}

func (s *Store) ChangePassword(ctx context.Context, userID, current, next string) error {
	var encoded string
	if err := s.DB.QueryRowContext(ctx, `SELECT password_hash FROM users WHERE id=$1`, userID).Scan(&encoded); err != nil {
		return err
	}
	if !VerifyPassword(encoded, current) {
		return errors.New("current password is incorrect")
	}
	hash, err := HashPassword(next)
	if err != nil {
		return err
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, `UPDATE users SET password_hash=$2,updated_at=now() WHERE id=$1`, userID, hash); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `DELETE FROM sessions WHERE user_id=$1`, userID); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) Audit(ctx context.Context, userID, action, targetType, targetID string, details any) {
	if userID == "" {
		_, _ = s.DB.ExecContext(ctx, `INSERT INTO audit_events(action,target_type,target_id,details) VALUES($1,$2,$3,$4)`, action, targetType, targetID, details)
		return
	}
	_, _ = s.DB.ExecContext(ctx, `INSERT INTO audit_events(user_id,action,target_type,target_id,details) VALUES($1,$2,$3,$4,$5)`, userID, action, targetType, targetID, details)
}

func (s *Store) Notify(ctx context.Context) {
	_, _ = s.DB.ExecContext(ctx, `SELECT pg_notify('tsingest_commands','wake')`)
}
