package worker

import (
	"errors"
	"testing"

	"tsingest/internal/media"
)

func TestMediaProgressAdvanced(t *testing.T) {
	tests := []struct {
		name                     string
		progress                 media.Progress
		previousSize, previousMS int64
		want                     bool
	}{
		{name: "zero output is not media", progress: media.Progress{}, want: false},
		{name: "first bytes are media", progress: media.Progress{TotalSize: 188}, want: true},
		{name: "unchanged snapshot is not progress", progress: media.Progress{TotalSize: 188, OutTimeMS: 40}, previousSize: 188, previousMS: 40, want: false},
		{name: "size growth is progress", progress: media.Progress{TotalSize: 376, OutTimeMS: 40}, previousSize: 188, previousMS: 40, want: true},
		{name: "time growth is progress", progress: media.Progress{TotalSize: 188, OutTimeMS: 80}, previousSize: 188, previousMS: 40, want: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := mediaProgressAdvanced(test.progress, test.previousSize, test.previousMS); got != test.want {
				t.Fatalf("mediaProgressAdvanced() = %v, want %v", got, test.want)
			}
		})
	}
}

func TestClassifyRecordingExit(t *testing.T) {
	tests := []struct {
		name, stderr, want string
		err                error
	}{
		{name: "clean eof", want: "source_disconnect"},
		{name: "network reset", err: errors.New("exit status 1"), stderr: "Connection reset by peer", want: "source_disconnect"},
		{name: "disk full", err: errors.New("exit status 1"), stderr: "No space left on device", want: "disk_guard"},
		{name: "muxer failure", err: errors.New("exit status 1"), stderr: "av_interleaved_write_frame(): I/O error", want: "process_error"},
		{name: "unknown crash", err: errors.New("signal: aborted"), want: "process_error"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := classifyRecordingExit(test.err, test.stderr); got != test.want {
				t.Fatalf("classifyRecordingExit() = %q, want %q", got, test.want)
			}
		})
	}
}
