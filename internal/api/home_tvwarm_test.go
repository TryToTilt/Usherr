package api

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestTVStreamSourceWithin(t *testing.T) {
	s := &Server{}
	defer func() { tvLookupForTest = nil }()

	// A quick lookup is answered at once.
	tvLookupForTest = func(ctx context.Context, path string) (tvStreamSrc, error) {
		return tvStreamSrc{url: "http://x/" + path}, nil
	}
	src, pending, err := s.tvStreamSourceWithin(context.Background(), "stream:1:1:1", time.Second)
	if err != nil || pending || src.url != "http://x/stream:1:1:1" {
		t.Fatalf("quick lookup: %+v pending=%v err=%v", src, pending, err)
	}

	// A failure is passed on, not hidden as "pending".
	boom := errors.New("no release")
	tvLookupForTest = func(context.Context, string) (tvStreamSrc, error) { return tvStreamSrc{}, boom }
	if _, pending, err = s.tvStreamSourceWithin(context.Background(), "stream:2:1:1", time.Second); pending || !errors.Is(err, boom) {
		t.Fatalf("failed lookup: pending=%v err=%v", pending, err)
	}

	// A slow lookup says "pending" without being cancelled, and everyone who
	// asks meanwhile shares the one lookup; its answer is there afterwards.
	var calls atomic.Int32
	release := make(chan struct{})
	tvLookupForTest = func(context.Context, string) (tvStreamSrc, error) {
		calls.Add(1)
		<-release
		return tvStreamSrc{url: "http://slow"}, nil
	}
	for i := 0; i < 3; i++ {
		if _, pending, err = s.tvStreamSourceWithin(context.Background(), "stream:3:1:1", 30*time.Millisecond); !pending || err != nil {
			t.Fatalf("slow lookup %d: pending=%v err=%v", i, pending, err)
		}
	}
	if n := calls.Load(); n != 1 {
		t.Fatalf("%d lookups ran for one program", n)
	}
	done := make(chan tvStreamSrc, 1)
	go func() {
		src, _, _ := s.tvStreamSourceWithin(context.Background(), "stream:3:1:1", 5*time.Second)
		done <- src
	}()
	time.Sleep(20 * time.Millisecond)
	close(release)
	select {
	case got := <-done:
		if got.url != "http://slow" {
			t.Fatalf("the shared lookup answered %+v", got)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("a waiter never got the answer")
	}

	// Someone who leaves doesn't stop the lookup.
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	block := make(chan struct{})
	tvLookupForTest = func(context.Context, string) (tvStreamSrc, error) { <-block; return tvStreamSrc{}, nil }
	if _, pending, _ = s.tvStreamSourceWithin(ctx, "stream:4:1:1", time.Minute); !pending {
		t.Fatal("a gone viewer was not told to ask again")
	}
	close(block)
}

func TestReplayTokensGrowWithSlots(t *testing.T) {
	if replayTokens(0) != 1500 || replayTokens(5) != 1500 {
		t.Errorf("small: %d %d", replayTokens(0), replayTokens(5))
	}
	// A day of half hours with a backup for each (96 entries) ran past 4000.
	if got := replayTokens(96); got < 8000 || got > 12000 {
		t.Errorf("96 slots: %d", got)
	}
	if replayTokens(1000) != 12000 {
		t.Errorf("cap: %d", replayTokens(1000))
	}
}

// fakeFFprobe puts a script called ffprobe first on PATH.
func fakeFFprobe(t *testing.T, script string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("needs a shell")
	}
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "ffprobe"), []byte("#!/bin/sh\n"+script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
}

func TestProbeStreamTwiceLooksAgain(t *testing.T) {
	flag := filepath.Join(t.TempDir(), "first-done")
	// Fails the first time, as a stream just opened from Usenet sometimes does.
	fakeFFprobe(t, `if [ ! -f "`+flag+`" ]; then touch "`+flag+`"; echo "Error while reading: end of file" >&2; exit 1; fi
echo codec_name=h264
echo duration=1500.5
echo format_name=matroska,webm
`)
	p, err := probeStreamTwice(context.Background(), "http://x/video.mkv")
	if err != nil || p.secs != 1500.5 || p.video != "h264" || p.format != "matroska,webm" {
		t.Fatalf("second look: %+v %v", p, err)
	}
}

func TestProbeStreamSaysWhyItFailed(t *testing.T) {
	fakeFFprobe(t, `echo "http://x/video.mkv: Server returned 404 Not Found" >&2; exit 1`)
	_, err := probeStreamWithin(context.Background(), "http://x/video.mkv", 5*time.Second)
	if err == nil || !strings.Contains(err.Error(), "404 Not Found") {
		t.Fatalf("the reason was lost: %v", err)
	}
}

func TestProbeStreamGivesUpWhenItStalls(t *testing.T) {
	fakeFFprobe(t, `exec sleep 30`)
	start := time.Now()
	_, err := probeStreamWithin(context.Background(), "http://x/video.mkv", 300*time.Millisecond)
	if err == nil || !strings.Contains(err.Error(), "didn't answer") || time.Since(start) > 5*time.Second {
		t.Fatalf("a stalled probe: %v after %v", err, time.Since(start))
	}
}
