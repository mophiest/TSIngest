package media

import (
	"os"
	"strings"
	"testing"

	"tsingest/internal/app"
)

func TestBuildAndRedactSRTURL(t *testing.T) {
	stream := app.StreamSecret{Stream: app.Stream{Mode: "listener", Port: 9000, LatencyMS: 200, TimeoutMS: 30000}}
	raw, err := BuildSRTURL(stream, "this-is-a-secret")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(raw, "srt://0.0.0.0:9000?") {
		t.Fatalf("unexpected URL: %s", raw)
	}
	if !strings.Contains(raw, "passphrase=this-is-a-secret") || !strings.Contains(raw, "pbkeylen=32") {
		t.Fatalf("encryption options missing: %s", raw)
	}
	redacted := RedactSRTURL(raw)
	if strings.Contains(redacted, "this-is-a-secret") || !strings.Contains(redacted, "REDACTED") {
		t.Fatalf("URL was not redacted: %s", redacted)
	}
}

func TestCallerSRTURL(t *testing.T) {
	stream := app.StreamSecret{Stream: app.Stream{Mode: "caller", Host: "media.example", Port: 10000, LatencyMS: 400, TimeoutMS: 15000, StreamID: "camera-1"}}
	raw, err := BuildSRTURL(stream, "")
	if err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{"srt://media.example:10000?", "latency=400000", "mode=caller", "streamid=camera-1", "timeout=15000000"} {
		if !strings.Contains(raw, expected) {
			t.Fatalf("%q missing from %s", expected, raw)
		}
	}
}

func TestCallerSRTURLKeepsStreamIDColonReadable(t *testing.T) {
	stream := app.StreamSecret{Stream: app.Stream{Mode: "caller", Host: "192.168.1.30", Port: 8890, LatencyMS: 200, TimeoutMS: 30000, StreamID: "read:27srt-h1"}}
	raw, err := BuildSRTURL(stream, "")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(raw, "streamid=read:27srt-h1") {
		t.Fatalf("streamid colon should remain readable in SRT URL: %s", raw)
	}
}

func TestMP4ArgsPreserveAndTranscodeAudio(t *testing.T) {
	probe := ProbeResult{Streams: []ProbeStream{
		{CodecType: "video", CodecName: "h264"},
		{CodecType: "audio", CodecName: "aac"},
		{CodecType: "audio", CodecName: "ac3"},
	}}
	args, err := MP4Args("input.ts", "output.part.mp4", probe)
	if err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(args, " ")
	if !strings.Contains(joined, "-map 0:a?") || !strings.Contains(joined, "-c:a:1 aac") || !strings.Contains(joined, "-movflags +faststart") {
		t.Fatalf("unexpected arguments: %s", joined)
	}
}

func TestMP4ArgsAcceptsHEVC(t *testing.T) {
	args, err := MP4Args("input.ts", "output.part.mp4", ProbeResult{Streams: []ProbeStream{
		{CodecType: "video", CodecName: "hevc"},
		{CodecType: "audio", CodecName: "ac3"},
	}})
	if err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(args, " ")
	if !strings.Contains(joined, "-c:v copy") || !strings.Contains(joined, "-tag:v hvc1") || !strings.Contains(joined, "-c:a:0 aac") {
		t.Fatalf("unexpected HEVC arguments: %s", joined)
	}
}

func TestMP4RejectsUnsupportedVideo(t *testing.T) {
	_, err := MP4Args("input.ts", "output.mp4", ProbeResult{Streams: []ProbeStream{{CodecType: "video", CodecName: "mpeg2video"}}})
	if err == nil {
		t.Fatal("unsupported video was accepted")
	}
}

func TestSafePath(t *testing.T) {
	root := t.TempDir()
	if _, err := SafePath(root, root+"/stream/file.ts"); err != nil {
		t.Fatal(err)
	}
	if _, err := SafePath(root, root+"/../outside.ts"); err == nil {
		t.Fatal("escaping path was accepted")
	}
}

func TestSafeExistingPathRejectsEscapingSymlink(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	target := outside + "/outside.ts"
	if err := os.WriteFile(target, []byte("media"), 0o600); err != nil {
		t.Fatal(err)
	}
	link := root + "/link.ts"
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	if _, err := SafeExistingPath(root, link); err == nil {
		t.Fatal("escaping symlink was accepted")
	}
}

func TestParseProgress(t *testing.T) {
	input := "out_time_us=1250000\ntotal_size=4096\nbitrate=26.2kbits/s\nspeed=1x\nprogress=continue\n"
	var got Progress
	if err := ParseProgress(strings.NewReader(input), func(value Progress) { got = value }); err != nil {
		t.Fatal(err)
	}
	if got.OutTimeMS != 1250 || got.TotalSize != 4096 || got.End {
		t.Fatalf("unexpected progress: %#v", got)
	}
}
