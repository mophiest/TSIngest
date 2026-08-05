package media

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/url"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"tsingest/internal/app"
)

type ProbeStream struct {
	Index      int               `json:"index"`
	CodecName  string            `json:"codec_name"`
	CodecType  string            `json:"codec_type"`
	Profile    string            `json:"profile"`
	Width      int               `json:"width"`
	Height     int               `json:"height"`
	Channels   int               `json:"channels"`
	SampleRate string            `json:"sample_rate"`
	Tags       map[string]string `json:"tags"`
}

type probeFormat struct {
	Duration string `json:"duration"`
	Size     string `json:"size"`
}

type ProbeResult struct {
	Streams []ProbeStream `json:"streams"`
	Format  probeFormat   `json:"format"`
}

func Probe(ctx context.Context, ffprobePath, input string) (ProbeResult, error) {
	cmd := exec.CommandContext(ctx, ffprobePath, "-v", "error", "-show_streams", "-show_format", "-of", "json", input)
	output, err := cmd.Output()
	if err != nil {
		if exit, ok := err.(*exec.ExitError); ok {
			return ProbeResult{}, fmt.Errorf("ffprobe: %s", strings.TrimSpace(string(exit.Stderr)))
		}
		return ProbeResult{}, err
	}
	var result ProbeResult
	if err := json.Unmarshal(output, &result); err != nil {
		return ProbeResult{}, err
	}
	if len(result.Streams) == 0 {
		return ProbeResult{}, errors.New("文件中没有可识别的媒体流")
	}
	return result, nil
}

func (p ProbeResult) DurationMS() int64 {
	seconds, _ := strconv.ParseFloat(p.Format.Duration, 64)
	return int64(seconds * 1000)
}

func (p ProbeResult) SizeBytes() int64 {
	size, _ := strconv.ParseInt(p.Format.Size, 10, 64)
	return size
}

func (p ProbeResult) HasH264Video() bool {
	for _, stream := range p.Streams {
		if stream.CodecType == "video" {
			return stream.CodecName == "h264"
		}
	}
	return false
}

func (p ProbeResult) HasAudio() bool {
	for _, stream := range p.Streams {
		if stream.CodecType == "audio" {
			return true
		}
	}
	return false
}

func (p ProbeResult) CodecSummary() map[string]any {
	videos := make([]map[string]any, 0)
	audios := make([]map[string]any, 0)
	for _, stream := range p.Streams {
		switch stream.CodecType {
		case "video":
			videos = append(videos, map[string]any{"codec": stream.CodecName, "profile": stream.Profile, "width": stream.Width, "height": stream.Height})
		case "audio":
			audios = append(audios, map[string]any{"codec": stream.CodecName, "channels": stream.Channels, "sampleRate": stream.SampleRate, "language": stream.Tags["language"]})
		}
	}
	return map[string]any{"video": videos, "audio": audios}
}

func BuildSRTURL(stream app.StreamSecret, passphrase string) (string, error) {
	values := url.Values{}
	values.Set("mode", stream.Mode)
	values.Set("transtype", "live")
	values.Set("latency", strconv.Itoa(stream.LatencyMS*1000))
	values.Set("timeout", strconv.Itoa(stream.TimeoutMS*1000))
	if stream.StreamID != "" {
		values.Set("streamid", stream.StreamID)
	}
	if passphrase != "" {
		values.Set("passphrase", passphrase)
		values.Set("pbkeylen", "32")
		values.Set("enforced_encryption", "1")
	}
	host := stream.Host
	if stream.Mode == "listener" {
		host = "0.0.0.0"
	}
	if host == "" {
		return "", errors.New("SRT host is empty")
	}
	return (&url.URL{Scheme: "srt", Host: net.JoinHostPort(host, strconv.Itoa(stream.Port)), RawQuery: values.Encode()}).String(), nil
}

func RedactSRTURL(raw string) string {
	parsed, err := url.Parse(raw)
	if err != nil {
		return "srt://[invalid]"
	}
	values := parsed.Query()
	if values.Has("passphrase") {
		values.Set("passphrase", "REDACTED")
	}
	parsed.RawQuery = values.Encode()
	return parsed.String()
}

func MP4Args(input, output string, probe ProbeResult) ([]string, error) {
	if !probe.HasH264Video() {
		return nil, errors.New("TS视频编码不是H.264，第一版不支持视频转码")
	}
	args := []string{"-hide_banner", "-nostdin", "-y", "-i", input, "-map", "0:v:0", "-map", "0:a?", "-c:v", "copy", "-c:a", "copy"}
	audioIndex := 0
	for _, stream := range probe.Streams {
		if stream.CodecType != "audio" {
			continue
		}
		if stream.CodecName != "aac" {
			args = append(args, fmt.Sprintf("-c:a:%d", audioIndex), "aac", fmt.Sprintf("-b:a:%d", audioIndex), "192k")
		}
		audioIndex++
	}
	args = append(args, "-movflags", "+faststart", "-progress", "pipe:1", "-stats_period", "1", output)
	return args, nil
}

// SafeExistingPath resolves symlinks before checking containment. Use it for
// reads and deletes; SafePath remains suitable for output files not yet created.
func SafeExistingPath(root, candidate string) (string, error) {
	realRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		return "", err
	}
	realCandidate, err := filepath.EvalSymlinks(candidate)
	if err != nil {
		return "", err
	}
	return SafePath(realRoot, realCandidate)
}

type Progress struct {
	OutTimeMS int64
	TotalSize int64
	Bitrate   string
	Speed     string
	End       bool
}

func ParseProgress(reader io.Reader, onProgress func(Progress)) error {
	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 64*1024), 1024*1024)
	values := map[string]string{}
	for scanner.Scan() {
		line := scanner.Text()
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		values[key] = value
		if key != "progress" {
			continue
		}
		var outUS int64
		if raw := values["out_time_us"]; raw != "" {
			outUS, _ = strconv.ParseInt(raw, 10, 64)
		} else if raw := values["out_time_ms"]; raw != "" {
			// FFmpeg historically named this field out_time_ms while reporting microseconds.
			outUS, _ = strconv.ParseInt(raw, 10, 64)
		}
		size, _ := strconv.ParseInt(values["total_size"], 10, 64)
		onProgress(Progress{OutTimeMS: outUS / 1000, TotalSize: size, Bitrate: values["bitrate"], Speed: values["speed"], End: value == "end"})
		values = map[string]string{}
	}
	return scanner.Err()
}

func SafePath(root, candidate string) (string, error) {
	rootAbs, err := filepath.Abs(root)
	if err != nil {
		return "", err
	}
	candidateAbs, err := filepath.Abs(candidate)
	if err != nil {
		return "", err
	}
	rel, err := filepath.Rel(rootAbs, candidateAbs)
	if err != nil {
		return "", err
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(rel) {
		return "", errors.New("path escapes recordings root")
	}
	return candidateAbs, nil
}

func FinalPath(partPath, kind string) string {
	suffix := ".part." + kind
	if strings.HasSuffix(partPath, suffix) {
		return strings.TrimSuffix(partPath, suffix) + "." + kind
	}
	return strings.TrimSuffix(partPath, filepath.Ext(partPath)) + "." + kind
}

func ProbeWithTimeout(ffprobePath, input string, timeout time.Duration) (ProbeResult, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	return Probe(ctx, ffprobePath, input)
}
