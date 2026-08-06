package worker

import (
	"bufio"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"tsingest/internal/app"
	"tsingest/internal/media"
)

const Version = "0.1.1"

type Runner struct {
	cfg   app.Config
	store *app.Store
	log   *slog.Logger

	mu          sync.Mutex
	recordings  map[string]*recordingProcess
	conversions int
	settings    app.SystemSettings
	stopping    atomic.Bool
	wg          sync.WaitGroup
}

type recordingProcess struct {
	id         string
	mu         sync.Mutex
	cmd        *exec.Cmd
	cmdDone    chan struct{}
	stopReason string
	stopOnce   sync.Once
	stopCh     chan struct{}
	done       chan struct{}
}

func New(cfg app.Config, store *app.Store, log *slog.Logger) *Runner {
	return &Runner{cfg: cfg, store: store, log: log, recordings: make(map[string]*recordingProcess), settings: app.SystemSettings{
		MP4Concurrency:  cfg.DefaultMP4Concurrency,
		SoftFreePercent: cfg.SoftFreePercent,
		SoftFreeGiB:     cfg.SoftFreeBytes / (1024 * 1024 * 1024),
		HardFreePercent: cfg.HardFreePercent,
		HardFreeGiB:     cfg.HardFreeBytes / (1024 * 1024 * 1024),
	}}
}

func (r *Runner) Run(ctx context.Context) error {
	if err := os.MkdirAll(r.cfg.RecordingsRoot, 0o750); err != nil {
		return err
	}
	if err := r.checkCapabilities(ctx); err != nil {
		return err
	}
	if err := r.store.ResetStaleWork(ctx, r.cfg.WorkerID); err != nil {
		return err
	}
	if settings, err := r.store.GetSettings(ctx); err == nil {
		r.settings = settings
	}
	if err := r.recoverInterrupted(ctx); err != nil {
		r.log.Error("recovery scan failed", "error", err)
	}

	heartbeat := time.NewTicker(5 * time.Second)
	settingsTick := time.NewTicker(15 * time.Second)
	poll := time.NewTicker(500 * time.Millisecond)
	defer heartbeat.Stop()
	defer settingsTick.Stop()
	defer poll.Stop()
	r.sendHeartbeat(ctx)

	for {
		select {
		case <-ctx.Done():
			r.shutdown()
			return nil
		case <-heartbeat.C:
			r.sendHeartbeat(context.Background())
			r.enforceDiskGuard()
		case <-settingsTick.C:
			if settings, err := r.store.GetSettings(ctx); err == nil {
				r.mu.Lock()
				r.settings = settings
				r.mu.Unlock()
			}
		case <-poll.C:
			for i := 0; i < 10; i++ {
				cmd, err := r.store.ClaimCommand(ctx, r.cfg.WorkerID)
				if errors.Is(err, sql.ErrNoRows) {
					break
				}
				if err != nil {
					r.log.Warn("claim command failed", "error", err)
					break
				}
				if cmd.Kind == "generate_mp4" && !r.reserveConversion() {
					r.store.RequeueCommand(ctx, cmd.ID)
					break
				}
				r.wg.Add(1)
				go func(command app.WorkerCommand) {
					defer r.wg.Done()
					if command.Kind == "generate_mp4" {
						defer r.releaseConversion()
					}
					err := r.handleCommand(context.Background(), command)
					r.store.CompleteCommand(context.Background(), command.ID, err)
					if err != nil {
						r.log.Error("command failed", "kind", command.Kind, "target", command.TargetID, "error", err)
					}
				}(cmd)
			}
		}
	}
}

func (r *Runner) checkCapabilities(ctx context.Context) error {
	cmd := exec.CommandContext(ctx, r.cfg.FFmpegPath, "-hide_banner", "-protocols")
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("ffmpeg capability check: %w: %s", err, output)
	}
	if !strings.Contains(string(output), "srt") {
		return errors.New("ffmpeg build does not include SRT support")
	}
	if _, err := exec.LookPath(r.cfg.FFprobePath); err != nil {
		return fmt.Errorf("ffprobe unavailable: %w", err)
	}
	return nil
}

func (r *Runner) handleCommand(ctx context.Context, command app.WorkerCommand) error {
	switch command.Kind {
	case "start_recording":
		err := r.startRecording(ctx, command.TargetID)
		if err != nil {
			reason := "process_error"
			if strings.Contains(err.Error(), "磁盘") {
				reason = "disk_guard"
			}
			_ = r.store.FinishRecording(context.Background(), command.TargetID, "failed", reason, err.Error())
		}
		return err
	case "stop_recording":
		return r.stopRecording(command.TargetID, "operator_stop")
	case "generate_mp4":
		return r.generateMP4(ctx, command.TargetID)
	case "delete_file":
		return r.deleteFile(ctx, command.TargetID, command.Payload)
	default:
		return fmt.Errorf("unknown command %q", command.Kind)
	}
}

func (r *Runner) startRecording(ctx context.Context, recordingID string) error {
	if r.stopping.Load() {
		return errors.New("worker is stopping")
	}
	if soft, _, _, _ := r.diskLevels(); soft {
		return errors.New("磁盘已达到软水位，禁止开始新录制")
	}
	recording, err := r.store.GetRecording(ctx, recordingID)
	if err != nil {
		return err
	}
	if recording.Status != "waiting_input" {
		return fmt.Errorf("录制任务状态为%s，不能启动", recording.Status)
	}
	stream, err := r.store.GetStreamSecret(ctx, recording.StreamID)
	if err != nil {
		return err
	}
	passphrase, err := app.DecryptSecret(r.cfg.EncryptionKey, stream.PassphraseEnc)
	if err != nil {
		return fmt.Errorf("decrypt SRT passphrase: %w", err)
	}
	inputURL, err := media.BuildSRTURL(stream, passphrase)
	if err != nil {
		return err
	}
	requested := recording.RequestedAt.UTC()
	directory := filepath.Join(r.cfg.RecordingsRoot, stream.ID, requested.Format("2006"), requested.Format("01"), requested.Format("02"))
	if err := os.MkdirAll(directory, 0o750); err != nil {
		return err
	}
	base := requested.Format("20060102T150405Z") + "_" + recording.ID
	partPath, err := media.SafePath(r.cfg.RecordingsRoot, filepath.Join(directory, base+".part.ts"))
	if err != nil {
		return err
	}
	if err := r.store.SetRecordingWorkPath(ctx, recordingID, partPath); err != nil {
		return err
	}

	proc := &recordingProcess{id: recordingID, stopCh: make(chan struct{}), done: make(chan struct{})}
	r.mu.Lock()
	if _, exists := r.recordings[recordingID]; exists {
		r.mu.Unlock()
		return errors.New("recording already running")
	}
	r.recordings[recordingID] = proc
	r.mu.Unlock()
	r.wg.Add(1)
	go func() {
		defer r.wg.Done()
		defer close(proc.done)
		defer func() { r.mu.Lock(); delete(r.recordings, recordingID); r.mu.Unlock() }()
		r.runRecording(proc, stream, inputURL, partPath, recording.AutoMP4)
	}()
	r.log.Info("recording worker started", "recording", recordingID, "stream", stream.Name, "input", media.RedactSRTURL(inputURL))
	return nil
}

func (r *Runner) runRecording(proc *recordingProcess, stream app.StreamSecret, inputURL, partPath string, autoMP4 bool) {
	backoff := 2 * time.Second
	for {
		proc.mu.Lock()
		stopping := proc.stopReason != ""
		proc.mu.Unlock()
		if stopping {
			r.finishWithoutMedia(proc.id, proc.reason(), "未收到媒体数据")
			return
		}
		started, stderrText, err := r.runFFmpegAttempt(proc, inputURL, partPath, time.Duration(stream.TimeoutMS)*time.Millisecond)
		if started || fileHasData(partPath) {
			reason := proc.reason()
			if reason == "" {
				reason = classifyRecordingExit(err, stderrText)
			}
			r.finalizeRecording(proc.id, partPath, reason, stderrText, autoMP4)
			return
		}
		if proc.reason() != "" {
			r.finishWithoutMedia(proc.id, proc.reason(), "未收到媒体数据")
			return
		}
		if stream.Mode != "caller" {
			r.finishWithoutMedia(proc.id, "process_error", errorMessage(err, stderrText))
			return
		}
		r.log.Warn("caller connection failed; retrying", "recording", proc.id, "after", backoff, "error", errorMessage(err, stderrText))
		timer := time.NewTimer(backoff)
		select {
		case <-timer.C:
		case <-proc.stopCh:
			timer.Stop()
			continue
		}
		if backoff < 30*time.Second {
			backoff *= 2
			if backoff > 30*time.Second {
				backoff = 30 * time.Second
			}
		}
	}
}

func (r *Runner) runFFmpegAttempt(proc *recordingProcess, inputURL, partPath string, stallTimeout time.Duration) (bool, string, error) {
	args := []string{"-hide_banner", "-nostdin", "-y", "-loglevel", "warning", "-i", inputURL, "-map", "0", "-c", "copy", "-f", "mpegts", "-progress", "pipe:1", "-stats_period", "1", partPath}
	cmd := exec.Command(r.cfg.FFmpegPath, args...)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return false, "", err
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return false, "", err
	}
	if err = cmd.Start(); err != nil {
		return false, "", err
	}
	proc.mu.Lock()
	proc.cmd = cmd
	cmdDone := make(chan struct{})
	proc.cmdDone = cmdDone
	stopRequested := proc.stopReason != ""
	proc.mu.Unlock()
	if stopRequested {
		signalProcess(cmd, cmdDone)
	}

	if stallTimeout < 5*time.Second {
		stallTimeout = 5 * time.Second
	}
	var mediaSeen atomic.Bool
	var statusPersisted atomic.Bool
	var lastAdvanceNS atomic.Int64
	var lastStderrMu sync.Mutex
	lastLines := make([]string, 0, 20)
	progressDone := make(chan struct{})
	go func() {
		defer close(progressDone)
		var previousSize, previousTime int64
		positiveSamples := 0
		_ = media.ParseProgress(stdout, func(progress media.Progress) {
			if mediaProgressAdvanced(progress, previousSize, previousTime) {
				mediaSeen.Store(true)
				lastAdvanceNS.Store(time.Now().UnixNano())
				positiveSamples++
			}
			if progress.TotalSize > previousSize {
				previousSize = progress.TotalSize
			}
			if progress.OutTimeMS > previousTime {
				previousTime = progress.OutTimeMS
			}
			if positiveSamples >= 2 && !statusPersisted.Load() {
				ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
				err := r.store.SetRecordingStarted(ctx, proc.id)
				cancel()
				if err == nil {
					statusPersisted.Store(true)
				} else {
					r.log.Warn("recording status update deferred", "recording", proc.id, "error", err)
				}
			}
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			err := r.store.UpdateRecordingProgress(ctx, proc.id, progress.OutTimeMS, progress.TotalSize, progress.Bitrate)
			cancel()
			if err != nil {
				r.log.Warn("recording progress update deferred", "recording", proc.id, "error", err)
			}
		})
	}()
	watchdogDone := make(chan struct{})
	watchdogStop := make(chan struct{})
	go func() {
		defer close(watchdogDone)
		ticker := time.NewTicker(time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				last := lastAdvanceNS.Load()
				if mediaSeen.Load() && last > 0 && time.Since(time.Unix(0, last)) >= stallTimeout {
					r.log.Warn("recording media progress stalled", "recording", proc.id, "timeout", stallTimeout)
					proc.stop("source_disconnect")
					return
				}
			case <-watchdogStop:
				return
			case <-proc.stopCh:
				return
			}
		}
	}()
	stderrDone := make(chan struct{})
	go func() {
		defer close(stderrDone)
		scanner := bufio.NewScanner(stderr)
		for scanner.Scan() {
			line := strings.TrimSpace(scanner.Text())
			if line == "" {
				continue
			}
			lastStderrMu.Lock()
			if len(lastLines) == 20 {
				copy(lastLines, lastLines[1:])
				lastLines = lastLines[:19]
			}
			lastLines = append(lastLines, line)
			lastStderrMu.Unlock()
		}
	}()
	err = cmd.Wait()
	close(cmdDone)
	close(watchdogStop)
	<-watchdogDone
	<-progressDone
	<-stderrDone
	proc.mu.Lock()
	proc.cmd = nil
	proc.cmdDone = nil
	proc.mu.Unlock()
	lastStderrMu.Lock()
	stderrText := strings.Join(lastLines, "\n")
	lastStderrMu.Unlock()
	return mediaSeen.Load(), stderrText, err
}

func (r *Runner) stopRecording(id, reason string) error {
	r.mu.Lock()
	proc := r.recordings[id]
	r.mu.Unlock()
	if proc == nil {
		recording, err := r.store.GetRecording(context.Background(), id)
		if err != nil {
			return err
		}
		if recording.Status == "ready" || recording.Status == "failed" {
			return nil
		}
		return errors.New("活动录制进程不存在")
	}
	proc.stop(reason)
	return nil
}

func (p *recordingProcess) reason() string { p.mu.Lock(); defer p.mu.Unlock(); return p.stopReason }

func (p *recordingProcess) stop(reason string) {
	p.mu.Lock()
	if p.stopReason == "" {
		p.stopReason = reason
	}
	cmd := p.cmd
	cmdDone := p.cmdDone
	p.mu.Unlock()
	p.stopOnce.Do(func() { close(p.stopCh) })
	if cmd == nil || cmd.Process == nil {
		return
	}
	signalProcess(cmd, cmdDone)
}

func signalProcess(cmd *exec.Cmd, done <-chan struct{}) {
	if cmd == nil || cmd.Process == nil {
		return
	}
	pid := cmd.Process.Pid
	_ = syscall.Kill(-pid, syscall.SIGINT)
	go func() {
		select {
		case <-done:
			return
		case <-time.After(15 * time.Second):
			_ = syscall.Kill(-pid, syscall.SIGTERM)
		}
		select {
		case <-done:
			return
		case <-time.After(5 * time.Second):
			_ = syscall.Kill(-pid, syscall.SIGKILL)
		}
	}()
}

func (r *Runner) finalizeRecording(id, partPath, reason, stderrText string, autoMP4 bool) {
	ctx := context.Background()
	_ = r.storeWithRetry("set recording finalizing", func(callCtx context.Context) error {
		return r.store.SetRecordingFinalizing(callCtx, id, reason)
	})
	probe, err := media.ProbeWithTimeout(r.cfg.FFprobePath, partPath, 30*time.Second)
	info, statErr := os.Stat(partPath)
	size := probe.SizeBytes()
	if statErr == nil {
		size = info.Size()
	}
	if err != nil || probe.DurationMS() <= 0 || size < 188 {
		message := errorMessage(err, stderrText)
		if err == nil {
			switch {
			case probe.DurationMS() <= 0:
				message = "TS时长无效"
			default:
				message = "TS文件没有有效媒体数据"
			}
		}
		_ = r.storeWithRetry("mark recording failed", func(callCtx context.Context) error {
			return r.store.FinishRecording(callCtx, id, "failed", reason, message)
		})
		return
	}
	finalPath := media.FinalPath(partPath, "ts")
	if err := os.Rename(partPath, finalPath); err != nil {
		_ = r.storeWithRetry("mark recording rename failure", func(callCtx context.Context) error {
			return r.store.FinishRecording(callCtx, id, "failed", reason, "完成TS重命名失败: "+err.Error())
		})
		return
	}
	info, _ = os.Stat(finalPath)
	if info != nil {
		size = info.Size()
	}
	if err := r.storeWithRetry("save TS metadata", func(callCtx context.Context) error {
		return r.store.AddMediaFile(callCtx, id, "ts", finalPath, size, probe.DurationMS(), probe.CodecSummary())
	}); err != nil {
		_ = r.storeWithRetry("mark recording metadata failure", func(callCtx context.Context) error {
			return r.store.FinishRecording(callCtx, id, "failed", reason, "保存TS元数据失败: "+err.Error())
		})
		return
	}
	_ = r.storeWithRetry("mark recording ready", func(callCtx context.Context) error {
		return r.store.FinishRecording(callCtx, id, "ready", reason, "")
	})
	if autoMP4 {
		if err := r.store.QueueAutoMP4(ctx, id); err != nil {
			r.log.Error("auto MP4 queue failed", "recording", id, "error", err)
		}
	}
}

func (r *Runner) finishWithoutMedia(id, reason, message string) {
	_ = r.storeWithRetry("mark recording without media", func(ctx context.Context) error {
		return r.store.FinishRecording(ctx, id, "failed", reason, message)
	})
}

func (r *Runner) generateMP4(ctx context.Context, jobID string) error {
	job, recording, err := r.store.MP4JobContext(ctx, jobID)
	if err != nil {
		return err
	}
	tsFile, err := r.store.MediaFileByKind(ctx, recording.ID, "ts")
	if err != nil {
		_ = r.store.FinishMP4(ctx, jobID, "failed", "TS文件不存在")
		return err
	}
	_, _, free, hardReserve := r.diskLevels()
	needed := uint64(float64(tsFile.SizeBytes)*1.1) + hardReserve
	if free < needed {
		err = errors.New("磁盘空间不足，无法生成MP4")
		_ = r.store.FinishMP4(ctx, jobID, "failed", err.Error())
		return err
	}
	probe, err := media.Probe(ctx, r.cfg.FFprobePath, tsFile.Path)
	if err != nil {
		_ = r.store.FinishMP4(ctx, jobID, "failed", err.Error())
		return err
	}
	partPath := strings.TrimSuffix(tsFile.Path, ".ts") + ".part.mp4"
	partPath, err = media.SafePath(r.cfg.RecordingsRoot, partPath)
	if err != nil {
		return err
	}
	args, err := media.MP4Args(tsFile.Path, partPath, probe)
	if err != nil {
		_ = r.store.FinishMP4(ctx, jobID, "failed", err.Error())
		return err
	}
	if err = r.store.SetMP4Running(ctx, job.ID, partPath); err != nil {
		return err
	}
	cmd := exec.Command(r.cfg.FFmpegPath, args...)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	var stderr strings.Builder
	cmd.Stderr = &limitedWriter{target: &stderr, limit: 64 * 1024}
	if err = cmd.Start(); err != nil {
		_ = r.store.FinishMP4(ctx, jobID, "failed", err.Error())
		return err
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		_ = media.ParseProgress(stdout, func(progress media.Progress) {
			percent := float64(0)
			if tsFile.DurationMS > 0 {
				percent = float64(progress.OutTimeMS) * 100 / float64(tsFile.DurationMS)
			}
			if percent > 99.9 {
				percent = 99.9
			}
			_ = r.store.UpdateMP4Progress(context.Background(), jobID, percent)
		})
	}()
	err = cmd.Wait()
	<-done
	if err != nil {
		_ = r.store.FinishMP4(ctx, jobID, "failed", strings.TrimSpace(stderr.String()))
		return err
	}
	outProbe, err := media.Probe(ctx, r.cfg.FFprobePath, partPath)
	if err != nil || !outProbe.HasH264Video() || !outProbe.HasAudio() || outProbe.DurationMS() <= 0 {
		if err == nil {
			err = errors.New("生成的MP4未通过H.264、音轨和时长校验")
		}
		_ = r.store.FinishMP4(ctx, jobID, "failed", err.Error())
		return err
	}
	finalPath := media.FinalPath(partPath, "mp4")
	if err = os.Rename(partPath, finalPath); err != nil {
		_ = r.store.FinishMP4(ctx, jobID, "failed", err.Error())
		return err
	}
	info, _ := os.Stat(finalPath)
	size := outProbe.SizeBytes()
	if info != nil {
		size = info.Size()
	}
	if err = r.store.AddMediaFile(ctx, recording.ID, "mp4", finalPath, size, outProbe.DurationMS(), outProbe.CodecSummary()); err != nil {
		_ = r.store.FinishMP4(ctx, jobID, "failed", err.Error())
		return err
	}
	return r.store.FinishMP4(ctx, jobID, "ready", "")
}

type limitedWriter struct {
	target *strings.Builder
	limit  int
}

func (w *limitedWriter) Write(data []byte) (int, error) {
	if w.target.Len()+len(data) <= w.limit {
		_, _ = w.target.Write(data)
	}
	return len(data), nil
}

func (r *Runner) deleteFile(ctx context.Context, recordingID string, payload []byte) error {
	var request struct {
		Kind string `json:"kind"`
	}
	if err := json.Unmarshal(payload, &request); err != nil {
		return err
	}
	file, err := r.store.MediaFileByKind(ctx, recordingID, request.Kind)
	if err != nil {
		return err
	}
	path, err := media.SafeExistingPath(r.cfg.RecordingsRoot, file.Path)
	if err != nil {
		return err
	}
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return err
	}
	return r.store.DeleteMediaFileRecord(ctx, recordingID, request.Kind)
}

func (r *Runner) recoverInterrupted(ctx context.Context) error {
	items, err := r.store.ActiveRecordingsForRecovery(ctx)
	if err != nil {
		return err
	}
	for _, item := range items {
		if !fileHasData(item.Path) {
			_ = r.store.FinishRecording(ctx, item.ID, "failed", "worker_shutdown", "Worker重启前未收到媒体数据")
			continue
		}
		recording, _ := r.store.GetRecording(ctx, item.ID)
		r.finalizeRecording(item.ID, item.Path, "worker_shutdown", "Worker重启后恢复", recording.AutoMP4)
	}
	return nil
}

func (r *Runner) reserveConversion() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.conversions >= r.settings.MP4Concurrency {
		return false
	}
	r.conversions++
	return true
}
func (r *Runner) releaseConversion() {
	r.mu.Lock()
	if r.conversions > 0 {
		r.conversions--
	}
	r.mu.Unlock()
}

func (r *Runner) sendHeartbeat(ctx context.Context) {
	total, free, _ := diskStats(r.cfg.RecordingsRoot)
	r.mu.Lock()
	active, conversions := len(r.recordings), r.conversions
	r.mu.Unlock()
	status := "ready"
	if r.stopping.Load() {
		status = "stopping"
	}
	_ = r.store.Heartbeat(ctx, app.WorkerHeartbeat{WorkerID: r.cfg.WorkerID, Status: status, ActiveRecordings: active, ActiveConversions: conversions, DiskTotalBytes: int64(total), DiskFreeBytes: int64(free), Version: Version})
}

func (r *Runner) diskLevels() (soft, hard bool, free, hardReserve uint64) {
	total, free, _ := diskStats(r.cfg.RecordingsRoot)
	r.mu.Lock()
	settings := r.settings
	r.mu.Unlock()
	percent := float64(100)
	if total > 0 {
		percent = float64(free) * 100 / float64(total)
	}
	softBytes := settings.SoftFreeGiB * 1024 * 1024 * 1024
	hardBytes := settings.HardFreeGiB * 1024 * 1024 * 1024
	return percent <= settings.SoftFreePercent || free <= softBytes, percent <= settings.HardFreePercent || free <= hardBytes, free, hardBytes
}

func (r *Runner) enforceDiskGuard() {
	_, hard, _, _ := r.diskLevels()
	if !hard {
		return
	}
	r.mu.Lock()
	items := make([]*recordingProcess, 0, len(r.recordings))
	for _, p := range r.recordings {
		items = append(items, p)
	}
	r.mu.Unlock()
	for _, proc := range items {
		proc.stop("disk_guard")
	}
}

func (r *Runner) shutdown() {
	if !r.stopping.CompareAndSwap(false, true) {
		return
	}
	r.mu.Lock()
	items := make([]*recordingProcess, 0, len(r.recordings))
	for _, p := range r.recordings {
		items = append(items, p)
	}
	r.mu.Unlock()
	for _, proc := range items {
		proc.stop("worker_shutdown")
	}
	done := make(chan struct{})
	go func() { r.wg.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(25 * time.Second):
	}
	r.sendHeartbeat(context.Background())
}

func diskStats(path string) (total, free uint64, err error) {
	var stats syscall.Statfs_t
	if err = syscall.Statfs(path, &stats); err != nil {
		return 0, 0, err
	}
	return stats.Blocks * uint64(stats.Bsize), stats.Bavail * uint64(stats.Bsize), nil
}

func fileHasData(path string) bool { info, err := os.Stat(path); return err == nil && info.Size() > 0 }
func mediaProgressAdvanced(progress media.Progress, previousSize, previousTime int64) bool {
	return progress.TotalSize > 0 && (progress.TotalSize > previousSize || progress.OutTimeMS > previousTime)
}

func classifyRecordingExit(err error, stderr string) string {
	message := strings.ToLower(stderr)
	for _, marker := range []string{"no space left", "permission denied", "read-only file system", "error writing", "av_interleaved_write_frame"} {
		if strings.Contains(message, marker) {
			if marker == "no space left" {
				return "disk_guard"
			}
			return "process_error"
		}
	}
	for _, marker := range []string{"connection was broken", "connection reset", "connection timed out", "i/o timeout", "network is unreachable", "connection refused", "end of file"} {
		if strings.Contains(message, marker) {
			return "source_disconnect"
		}
	}
	if err != nil {
		return "process_error"
	}
	return "source_disconnect"
}

func (r *Runner) storeWithRetry(operation string, call func(context.Context) error) error {
	var err error
	for attempt := 1; attempt <= 3; attempt++ {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		err = call(ctx)
		cancel()
		if err == nil {
			return nil
		}
		if attempt < 3 {
			time.Sleep(time.Duration(attempt) * 500 * time.Millisecond)
		}
	}
	r.log.Error("database state update failed", "operation", operation, "error", err)
	return err
}

func errorMessage(err error, stderr string) string {
	stderr = strings.TrimSpace(stderr)
	if stderr != "" {
		if len(stderr) > 2000 {
			return stderr[len(stderr)-2000:]
		}
		return stderr
	}
	if err != nil {
		return err.Error()
	}
	return "FFmpeg未产生媒体数据"
}
