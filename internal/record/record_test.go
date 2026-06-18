package record

import (
	"os"
	"path/filepath"
	"testing"
)

func TestIsMedia(t *testing.T) {
	cases := []struct {
		ct, url string
		want    bool
	}{
		{"video/mp2t", "http://up/ch1/1.ts", true},
		{"application/octet-stream", "http://up/ch1/1.ts", true}, // by extension
		{"audio/aac", "http://up/ch1/1.aac", true},
		{"image/png", "http://up/logo.png", false},
		{"application/vnd.apple.mpegurl", "http://up/ch1/mono.m3u8", false},
		{"application/xml", "http://up/epg.xml", false},
	}
	for _, c := range cases {
		if got := isMedia(c.ct, c.url); got != c.want {
			t.Errorf("isMedia(%q, %q) = %v, want %v", c.ct, c.url, got, c.want)
		}
	}
}

func TestChannelKey(t *testing.T) {
	cases := map[string]string{
		"http://2.hls.gd/ch963/452685.ts":             "ch963",
		"http://2.hls.gd/ch963/dvr-2026/06/17/x.ts":   "ch963",
		"http://up:8080/live/user/pass/12345.ts":      "live_12345", // creds excluded
		"http://up:8080/timeshift/u/p/60/2026/999.ts": "timeshift_999",
	}
	for url, want := range cases {
		if got := channelKey(url); got != want {
			t.Errorf("channelKey(%q) = %q, want %q", url, got, want)
		}
	}
}

func TestStart_NonMediaReturnsNil(t *testing.T) {
	r := newTestRecorder(t, 1)
	if e := r.Start("image/png", "http://up/logo.png"); e != nil {
		t.Error("expected nil entry for non-media")
	}
}

func TestStart_DedupesSameURL(t *testing.T) {
	r := newTestRecorder(t, 1)
	url := "http://up/ch1/1.ts"
	e1 := r.Start("video/mp2t", url)
	if e1 == nil {
		t.Fatal("first Start returned nil")
	}
	e1.Close()
	if e2 := r.Start("video/mp2t", url); e2 != nil {
		t.Error("duplicate URL should not be recorded again")
	}
}

func TestStart_WritesGroupedFile(t *testing.T) {
	r := newTestRecorder(t, 1)
	e := r.Start("video/mp2t", "http://up/ch7/100.ts")
	if e == nil {
		t.Fatal("Start returned nil")
	}
	if _, err := e.Write([]byte("payload")); err != nil {
		t.Fatal(err)
	}
	e.Close()

	matches, _ := filepath.Glob(filepath.Join(r.dir, "ch7", "*.ts"))
	if len(matches) != 1 {
		t.Fatalf("expected 1 file under ch7, got %v", matches)
	}
	data, _ := os.ReadFile(matches[0])
	if string(data) != "payload" {
		t.Errorf("file content = %q, want %q", data, "payload")
	}
}

func TestEviction_StaysUnderCap(t *testing.T) {
	// Cap of 10 bytes; write three 8-byte segments. The oldest must be deleted so
	// total on-disk stays within the cap.
	r := newTestRecorderBytes(t, 10)
	for _, name := range []string{"a", "b", "c"} {
		e := r.Start("video/mp2t", "http://up/ch1/"+name+".ts")
		if e == nil {
			t.Fatalf("Start nil for %s", name)
		}
		e.Write([]byte("01234567")) // 8 bytes
		e.Close()
	}
	if total := r.sizeOnDisk(); total > 10 {
		t.Errorf("on-disk total = %d, want <= 10 (cap)", total)
	}
}

func newTestRecorder(t *testing.T, maxGB float64) *Recorder {
	t.Helper()
	r, err := New(t.TempDir(), maxGB)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func newTestRecorderBytes(t *testing.T, maxBytes int64) *Recorder {
	t.Helper()
	r := newTestRecorder(t, 1)
	r.maxBytes = maxBytes
	return r
}
