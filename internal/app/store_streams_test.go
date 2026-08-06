package app

import "testing"

func TestNormalizeStreamInputFromSRTURL(t *testing.T) {
	in := StreamInput{SourceURL: "srt://192.168.1.30:8890?streamid=read:27srt-h1"}
	if err := ValidateStreamInput(&in); err != nil {
		t.Fatalf("ValidateStreamInput() error = %v", err)
	}
	if in.Mode != "caller" {
		t.Fatalf("Mode = %q, want caller", in.Mode)
	}
	if in.Host != "192.168.1.30" {
		t.Fatalf("Host = %q", in.Host)
	}
	if in.Port != 8890 {
		t.Fatalf("Port = %d", in.Port)
	}
	if in.StreamID != "read:27srt-h1" {
		t.Fatalf("StreamID = %q", in.StreamID)
	}
	if in.Name != "27srt-h1" {
		t.Fatalf("Name = %q", in.Name)
	}
}

func TestNormalizeStreamInputRejectsListenerURL(t *testing.T) {
	in := StreamInput{Name: "bad", SourceURL: "srt://192.168.1.30:8890?mode=listener"}
	if err := ValidateStreamInput(&in); err == nil {
		t.Fatal("ValidateStreamInput() error = nil, want listener URL rejection")
	}
}
