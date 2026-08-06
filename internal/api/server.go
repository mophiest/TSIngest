package api

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"tsingest/internal/app"
	"tsingest/internal/media"
	"tsingest/internal/ui"
)

type Server struct {
	cfg   app.Config
	store *app.Store
	log   *slog.Logger
	login *loginLimiter
}

type contextKey string

const userKey contextKey = "user"

func New(cfg app.Config, store *app.Store, log *slog.Logger) http.Handler {
	s := &Server{cfg: cfg, store: store, log: log, login: newLoginLimiter()}
	r := chi.NewRouter()
	r.Use(middleware.RequestID, middleware.RealIP, middleware.Recoverer, s.requestLogger)
	r.Get("/healthz", s.health)
	r.Get("/readyz", s.ready)
	r.Get("/metrics", s.metrics)
	r.Post("/api/v1/auth/login", s.loginHandler)
	r.Group(func(protected chi.Router) {
		protected.Use(s.authenticate)
		protected.Use(s.verifyOrigin)
		protected.Post("/api/v1/auth/logout", s.logoutHandler)
		protected.Get("/api/v1/auth/me", s.meHandler)
		protected.Get("/api/v1/dashboard", s.dashboardHandler)
		protected.Get("/api/v1/events", s.eventsHandler)
		protected.Route("/api/v1/streams", func(sr chi.Router) {
			sr.Get("/", s.listStreams)
			sr.Post("/", s.createStream)
			sr.Get("/{id}", s.getStream)
			sr.Put("/{id}", s.updateStream)
			sr.Delete("/{id}", s.deleteStream)
			sr.Post("/{id}/recordings", s.startRecording)
		})
		protected.Route("/api/v1/recordings", func(rr chi.Router) {
			rr.Get("/", s.listRecordings)
			rr.Get("/{id}", s.getRecording)
			rr.Delete("/{id}", s.deleteRecording)
			rr.Post("/{id}/stop", s.stopRecording)
			rr.Post("/{id}/mp4", s.generateMP4)
			rr.Get("/{id}/files/{kind}", s.serveMediaFile)
			rr.Delete("/{id}/files/{kind}", s.deleteMediaFile)
		})
		protected.Get("/api/v1/settings", s.getSettings)
		protected.Put("/api/v1/settings", s.updateSettings)
		protected.Post("/api/v1/settings/password", s.changePassword)
	})
	r.Handle("/*", s.staticHandler())
	return r
}

func (s *Server) requestLogger(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		wrapper := middleware.NewWrapResponseWriter(w, r.ProtoMajor)
		next.ServeHTTP(wrapper, r)
		if !strings.HasPrefix(r.URL.Path, "/api/v1/events") {
			s.log.Info("http request", "method", r.Method, "path", r.URL.Path, "status", wrapper.Status(), "duration_ms", time.Since(start).Milliseconds(), "request_id", middleware.GetReqID(r.Context()))
		}
	})
}

func (s *Server) staticHandler() http.Handler {
	dist, err := fs.Sub(ui.Files, "dist")
	if err != nil {
		panic(err)
	}
	files := http.FileServer(http.FS(dist))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := strings.TrimPrefix(filepath.Clean(r.URL.Path), "/")
		if path == "." {
			path = "index.html"
		}
		if _, err := fs.Stat(dist, path); err != nil {
			r.URL.Path = "/"
			path = "index.html"
		}
		if path == "index.html" {
			w.Header().Set("Cache-Control", "no-store")
		}
		files.ServeHTTP(w, r)
	})
}

func (s *Server) authenticate(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cookie, err := r.Cookie("tsingest_session")
		if err != nil {
			writeError(w, http.StatusUnauthorized, "请先登录")
			return
		}
		user, err := s.store.SessionUser(r.Context(), app.TokenHash(cookie.Value), s.cfg.SessionIdle)
		if err != nil {
			clearSessionCookie(w, s.cfg.CookieSecure)
			writeError(w, http.StatusUnauthorized, "登录已失效")
			return
		}
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), userKey, user)))
	})
}

func (s *Server) verifyOrigin(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet || r.Method == http.MethodHead || r.Method == http.MethodOptions {
			next.ServeHTTP(w, r)
			return
		}
		origin := r.Header.Get("Origin")
		if origin == "" {
			next.ServeHTTP(w, r)
			return
		}
		parsed, err := url.Parse(origin)
		if err != nil {
			writeError(w, http.StatusForbidden, "请求来源无效")
			return
		}
		allowedHost := r.Host
		if s.cfg.PublicURL != "" {
			if public, err := url.Parse(s.cfg.PublicURL); err == nil {
				allowedHost = public.Host
			}
		}
		if !strings.EqualFold(parsed.Host, allowedHost) {
			writeError(w, http.StatusForbidden, "请求来源不受信任")
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) loginHandler(w http.ResponseWriter, r *http.Request) {
	ip, _, _ := net.SplitHostPort(r.RemoteAddr)
	if ip == "" {
		ip = r.RemoteAddr
	}
	if !s.login.Allow(ip) {
		writeError(w, http.StatusTooManyRequests, "登录尝试过于频繁，请稍后再试")
		return
	}
	var input struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := decodeJSON(r, &input); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	user, err := s.store.UserByUsername(r.Context(), input.Username)
	if err != nil || !app.VerifyPassword(user.PasswordHash, input.Password) {
		s.login.Fail(ip)
		writeError(w, http.StatusUnauthorized, "用户名或密码错误")
		return
	}
	plain, hash, err := app.RandomToken()
	if err != nil {
		writeError(w, 500, "无法创建会话")
		return
	}
	if err = s.store.CreateSession(r.Context(), user.ID, hash, ip, r.UserAgent(), s.cfg.SessionMax); err != nil {
		writeError(w, 500, "无法保存会话")
		return
	}
	http.SetCookie(w, &http.Cookie{Name: "tsingest_session", Value: plain, Path: "/", HttpOnly: true, Secure: s.cfg.CookieSecure, SameSite: http.SameSiteStrictMode, MaxAge: int(s.cfg.SessionMax.Seconds())})
	s.login.Success(ip)
	s.store.Audit(r.Context(), user.ID, "login", "user", user.ID, map[string]string{"ip": ip})
	writeJSON(w, 200, map[string]any{"id": user.ID, "username": user.Username})
}

func (s *Server) logoutHandler(w http.ResponseWriter, r *http.Request) {
	if cookie, err := r.Cookie("tsingest_session"); err == nil {
		_ = s.store.DeleteSession(r.Context(), app.TokenHash(cookie.Value))
	}
	clearSessionCookie(w, s.cfg.CookieSecure)
	writeJSON(w, 200, map[string]bool{"ok": true})
}
func (s *Server) meHandler(w http.ResponseWriter, r *http.Request) {
	u := currentUser(r)
	writeJSON(w, 200, map[string]string{"id": u.ID, "username": u.Username})
}

func (s *Server) listStreams(w http.ResponseWriter, r *http.Request) {
	items, err := s.store.ListStreams(r.Context())
	respond(w, items, err)
}
func (s *Server) getStream(w http.ResponseWriter, r *http.Request) {
	item, err := s.store.GetStream(r.Context(), chi.URLParam(r, "id"))
	respond(w, item, err)
}
func (s *Server) createStream(w http.ResponseWriter, r *http.Request) {
	var input app.StreamInput
	if err := decodeJSON(r, &input); err != nil {
		writeError(w, 400, err.Error())
		return
	}
	if err := app.ValidateStreamInput(&input); err != nil {
		writeError(w, 400, err.Error())
		return
	}
	encrypted, err := app.EncryptSecret(s.cfg.EncryptionKey, input.Passphrase)
	if err != nil {
		writeError(w, 500, "无法加密SRT口令")
		return
	}
	item, err := s.store.CreateStream(r.Context(), input, encrypted)
	if err != nil {
		writeDBError(w, err)
		return
	}
	u := currentUser(r)
	s.store.Audit(r.Context(), u.ID, "stream.create", "stream", item.ID, map[string]any{"name": item.Name, "mode": item.Mode, "port": item.Port})
	writeJSON(w, 201, item)
}
func (s *Server) updateStream(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	var input app.StreamInput
	if err := decodeJSON(r, &input); err != nil {
		writeError(w, 400, err.Error())
		return
	}
	if err := app.ValidateStreamInput(&input); err != nil {
		writeError(w, 400, err.Error())
		return
	}
	var encrypted *string
	if input.ClearPassphrase || input.Passphrase != "" {
		value, err := app.EncryptSecret(s.cfg.EncryptionKey, input.Passphrase)
		if err != nil {
			writeError(w, 500, "无法加密SRT口令")
			return
		}
		encrypted = &value
	}
	item, err := s.store.UpdateStream(r.Context(), id, input, encrypted)
	if err != nil {
		writeDBError(w, err)
		return
	}
	u := currentUser(r)
	s.store.Audit(r.Context(), u.ID, "stream.update", "stream", id, map[string]any{"name": item.Name})
	writeJSON(w, 200, item)
}
func (s *Server) deleteStream(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if err := s.store.DeleteStream(r.Context(), id); err != nil {
		writeDBError(w, err)
		return
	}
	u := currentUser(r)
	s.store.Audit(r.Context(), u.ID, "stream.delete", "stream", id, map[string]any{})
	w.WriteHeader(204)
}
func (s *Server) startRecording(w http.ResponseWriter, r *http.Request) {
	item, err := s.store.StartRecording(r.Context(), chi.URLParam(r, "id"), s.cfg.MaxActiveRecordings)
	if err != nil {
		writeDBError(w, err)
		return
	}
	u := currentUser(r)
	s.store.Audit(r.Context(), u.ID, "recording.start", "recording", item.ID, map[string]any{"streamId": item.StreamID})
	writeJSON(w, 202, item)
}
func (s *Server) stopRecording(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	item, err := s.store.StopRecording(r.Context(), id)
	if err != nil {
		writeDBError(w, err)
		return
	}
	u := currentUser(r)
	s.store.Audit(r.Context(), u.ID, "recording.stop", "recording", id, map[string]any{})
	writeJSON(w, 202, item)
}
func (s *Server) listRecordings(w http.ResponseWriter, r *http.Request) {
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	offset, _ := strconv.Atoi(r.URL.Query().Get("offset"))
	items, err := s.store.ListRecordings(r.Context(), r.URL.Query().Get("streamId"), r.URL.Query().Get("status"), limit, offset)
	respond(w, items, err)
}
func (s *Server) getRecording(w http.ResponseWriter, r *http.Request) {
	item, err := s.store.GetRecording(r.Context(), chi.URLParam(r, "id"))
	respond(w, item, err)
}
func (s *Server) deleteRecording(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if err := s.store.HideFailedRecording(r.Context(), id); err != nil {
		writeDBError(w, err)
		return
	}
	u := currentUser(r)
	s.store.Audit(r.Context(), u.ID, "recording.alert.clear", "recording", id, map[string]any{})
	w.WriteHeader(204)
}
func (s *Server) generateMP4(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	item, err := s.store.QueueMP4(r.Context(), id)
	if err != nil {
		writeDBError(w, err)
		return
	}
	u := currentUser(r)
	s.store.Audit(r.Context(), u.ID, "mp4.queue", "recording", id, map[string]any{})
	writeJSON(w, 202, item)
}

func (s *Server) serveMediaFile(w http.ResponseWriter, r *http.Request) {
	id, kind := chi.URLParam(r, "id"), chi.URLParam(r, "kind")
	file, err := s.store.MediaFileByKind(r.Context(), id, kind)
	if err != nil {
		writeDBError(w, err)
		return
	}
	path, err := media.SafeExistingPath(s.cfg.RecordingsRoot, file.Path)
	if err != nil {
		writeError(w, 403, "文件路径无效")
		return
	}
	handle, err := os.Open(path)
	if err != nil {
		writeError(w, 404, "文件不存在")
		return
	}
	defer handle.Close()
	info, err := handle.Stat()
	if err != nil {
		writeError(w, 500, "无法读取文件")
		return
	}
	if r.URL.Query().Get("download") == "1" || kind == "ts" {
		w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", filepath.Base(path)))
	}
	if kind == "mp4" {
		w.Header().Set("Content-Type", "video/mp4")
	} else {
		w.Header().Set("Content-Type", "video/mp2t")
	}
	http.ServeContent(w, r, filepath.Base(path), info.ModTime(), handle)
}
func (s *Server) deleteMediaFile(w http.ResponseWriter, r *http.Request) {
	if r.URL.Query().Get("confirm") != "DELETE" {
		writeError(w, 400, "需要明确确认删除")
		return
	}
	id, kind := chi.URLParam(r, "id"), chi.URLParam(r, "kind")
	if err := s.store.QueueDeleteFile(r.Context(), id, kind); err != nil {
		writeDBError(w, err)
		return
	}
	u := currentUser(r)
	s.store.Audit(r.Context(), u.ID, "file.delete", "recording", id, map[string]string{"kind": kind})
	writeJSON(w, 202, map[string]bool{"queued": true})
}

func (s *Server) dashboardHandler(w http.ResponseWriter, r *http.Request) {
	item, err := s.store.Dashboard(r.Context())
	respond(w, item, err)
}
func (s *Server) getSettings(w http.ResponseWriter, r *http.Request) {
	item, err := s.store.GetSettings(r.Context())
	respond(w, item, err)
}
func (s *Server) updateSettings(w http.ResponseWriter, r *http.Request) {
	var input app.SystemSettings
	if err := decodeJSON(r, &input); err != nil {
		writeError(w, 400, err.Error())
		return
	}
	if err := s.store.SaveSettings(r.Context(), input); err != nil {
		writeError(w, 400, err.Error())
		return
	}
	u := currentUser(r)
	s.store.Audit(r.Context(), u.ID, "settings.update", "system", "", input)
	writeJSON(w, 200, input)
}
func (s *Server) changePassword(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Current string `json:"current"`
		Next    string `json:"next"`
	}
	if err := decodeJSON(r, &input); err != nil {
		writeError(w, 400, err.Error())
		return
	}
	if len(input.Next) < 12 {
		writeError(w, 400, "新密码至少需要12个字符")
		return
	}
	u := currentUser(r)
	if err := s.store.ChangePassword(r.Context(), u.ID, input.Current, input.Next); err != nil {
		writeError(w, 400, err.Error())
		return
	}
	clearSessionCookie(w, s.cfg.CookieSecure)
	writeJSON(w, 200, map[string]bool{"ok": true})
}

func (s *Server) eventsHandler(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, 500, "服务器不支持事件流")
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("X-Accel-Buffering", "no")
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	for {
		snapshot, err := s.store.Dashboard(r.Context())
		if err == nil {
			data, _ := json.Marshal(snapshot)
			_, _ = fmt.Fprintf(w, "event: snapshot\ndata: %s\n\n", data)
			flusher.Flush()
		}
		select {
		case <-r.Context().Done():
			return
		case <-ticker.C:
		}
	}
}

func (s *Server) health(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, 200, map[string]string{"status": "ok"})
}
func (s *Server) ready(w http.ResponseWriter, r *http.Request) {
	if err := s.store.Ready(r.Context()); err != nil {
		writeError(w, 503, "数据库未就绪")
		return
	}
	writeJSON(w, 200, map[string]string{"status": "ready"})
}
func (s *Server) metrics(w http.ResponseWriter, r *http.Request) {
	snapshot, err := s.store.Dashboard(r.Context())
	if err != nil {
		writeError(w, 503, "metrics unavailable")
		return
	}
	w.Header().Set("Content-Type", "text/plain; version=0.0.4")
	fmt.Fprintf(w, "tsingest_active_recordings %d\ntsingest_recordings_writing %d\ntsingest_mp4_queued %d\ntsingest_failures_24h %d\n", snapshot.ActiveCount, snapshot.RecordingCount, snapshot.QueuedMP4, snapshot.FailedLast24h)
	for _, worker := range snapshot.Workers {
		fmt.Fprintf(w, "tsingest_worker_disk_free_bytes{worker=%q} %d\n", worker.WorkerID, worker.DiskFreeBytes)
	}
}

func currentUser(r *http.Request) app.User {
	user, _ := r.Context().Value(userKey).(app.User)
	return user
}
func respond(w http.ResponseWriter, value any, err error) {
	if err != nil {
		writeDBError(w, err)
		return
	}
	writeJSON(w, 200, value)
}
func decodeJSON(r *http.Request, target any) error {
	decoder := json.NewDecoder(io.LimitReader(r.Body, 1024*1024))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return fmt.Errorf("请求内容无效: %w", err)
	}
	return nil
}
func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}
func writeDBError(w http.ResponseWriter, err error) {
	if errors.Is(err, sql.ErrNoRows) {
		writeError(w, 404, "记录不存在")
		return
	}
	message := err.Error()
	if strings.Contains(message, "duplicate key") {
		writeError(w, 409, "名称、端口或活动任务发生冲突")
		return
	}
	if strings.Contains(message, "violates foreign key") {
		writeError(w, 409, "仍有录制记录引用该流，不能删除")
		return
	}
	if strings.Contains(message, "正在") || strings.Contains(message, "尚未") || strings.Contains(message, "已经") || strings.Contains(message, "上限") || strings.Contains(message, "空间") || strings.Contains(message, "不能") {
		writeError(w, 409, message)
		return
	}
	writeError(w, 500, "服务器处理请求失败")
}
func clearSessionCookie(w http.ResponseWriter, secure bool) {
	http.SetCookie(w, &http.Cookie{Name: "tsingest_session", Value: "", Path: "/", HttpOnly: true, Secure: secure, SameSite: http.SameSiteStrictMode, MaxAge: -1})
}

type loginLimiter struct {
	mu      sync.Mutex
	entries map[string]*loginEntry
}
type loginEntry struct {
	failures     int
	window       time.Time
	blockedUntil time.Time
}

func newLoginLimiter() *loginLimiter { return &loginLimiter{entries: map[string]*loginEntry{}} }
func (l *loginLimiter) Allow(ip string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	entry := l.entries[ip]
	return entry == nil || time.Now().After(entry.blockedUntil)
}
func (l *loginLimiter) Fail(ip string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := time.Now()
	entry := l.entries[ip]
	if entry == nil || now.Sub(entry.window) > time.Minute {
		entry = &loginEntry{window: now}
		l.entries[ip] = entry
	}
	entry.failures++
	if entry.failures >= 5 {
		entry.blockedUntil = now.Add(5 * time.Minute)
	}
}
func (l *loginLimiter) Success(ip string) { l.mu.Lock(); delete(l.entries, ip); l.mu.Unlock() }
