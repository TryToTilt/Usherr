package api

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"
	"time"
)

func TestTVEncoderArgs(t *testing.T) {
	for kind, want := range map[string][]string{
		encCPU:   {"-c:v libx264 -preset veryfast -crf 21", "-vf format=yuv420p"},
		encNVENC: {"-c:v h264_nvenc -preset p4 -tune ll -rc vbr -cq 23", "-vf format=yuv420p"},
		encQSV:   {"-c:v h264_qsv", "-global_quality 23", "-vf format=nv12"},
		encVAAPI: {"-vaapi_device /dev/dri/", "-vf format=nv12,hwupload", "-c:v h264_vaapi -qp 23"},
	} {
		args := strings.Join(tvEncoderTestArgs(kind), " ")
		for _, w := range append(want, "-f lavfi -i testsrc2=size=1280x720:rate=30 -t 1", "-f null -") {
			if !strings.Contains(args, w) {
				t.Errorf("%s test args missing %q: %s", kind, w, args)
			}
		}
	}
	// VAAPI's device comes before the input.
	args := tvEncoderTestArgs(encVAAPI)
	if slices.Index(args, "-vaapi_device") > slices.Index(args, "-i") {
		t.Errorf("device after input: %v", args)
	}
}

func TestPickTVEncoder(t *testing.T) {
	res := []tvEncoderResult{{Kind: encNVENC}, {Kind: encQSV, Works: true}, {Kind: encVAAPI, Works: true}, {Kind: encCPU, Works: true}}
	if got := pickTVEncoder(encAuto, res); got != encQSV {
		t.Errorf("auto picked %s, want qsv", got)
	}
	if got := pickTVEncoder(encAuto, nil); got != encCPU {
		t.Errorf("nothing tested: %s", got)
	}
	if got := pickTVEncoder(encNVENC, res); got != encNVENC {
		t.Errorf("a chosen encoder is used as chosen: %s", got)
	}
}

func TestTVEncoderSelfTest(t *testing.T) {
	old := tvRunEncoderTest
	t.Cleanup(func() { tvRunEncoderTest = old; tvEncResults = nil })
	tvRunEncoderTest = func(_ context.Context, args []string) error {
		if slices.Contains(args, "h264_nvenc") {
			return errors.New("Cannot load libcuda.so.1")
		}
		if slices.Contains(args, "h264_qsv") || slices.Contains(args, "h264_vaapi") {
			return errors.New("No VA display found")
		}
		return nil
	}
	res := testTVEncoders(context.Background())
	if len(res) != 4 || res[0].Kind != encNVENC || res[0].Works || res[0].Message != "Cannot load libcuda.so.1" || !res[3].Works || res[3].Message != "works" {
		t.Fatalf("results: %+v", res)
	}
	if pickTVEncoder(encAuto, tvEncoderResults(context.Background())) != encCPU {
		t.Error("only the CPU works")
	}
}

func TestTVVideoDefaults(t *testing.T) {
	c := defaultTVVideo()
	if c.Encoder != encAuto || c.Watermark.On || c.Watermark.Size != 10 || c.Watermark.Opacity != 60 || c.Watermark.Position != "bottom-right" || c.Watermark.Show != "logo" {
		t.Errorf("defaults: %+v", c)
	}
	c = tvVideoConfig{Encoder: "bogus", Watermark: tvWatermark{Size: 90, Opacity: 3, Margin: 40, Position: "middle"}}.normalized()
	if c.Encoder != encAuto || c.Watermark.Size != 25 || c.Watermark.Opacity != 10 || c.Watermark.Margin != 10 || c.Watermark.Position != "bottom-right" {
		t.Errorf("clamped: %+v", c)
	}
}

func TestTVMarkGraph(t *testing.T) {
	w := tvWatermark{On: true, Show: "logo", Position: "bottom-right", Size: 10, Opacity: 60, Margin: 3}
	g := tvMarkGraph(tvMark{image: "/c/logo.png", width: 1920, w: w}, "format=yuv420p")
	for _, want := range []string{"[1:v]scale=192:-2", "colorchannelmixer=aa=0.60", "[0:v:0][wm]overlay=x=main_w-overlay_w-57:y=main_h-overlay_h-57", ",format=yuv420p[v]"} {
		if !strings.Contains(g, want) {
			t.Errorf("graph missing %q: %s", want, g)
		}
	}
	w.Position, w.Size, w.Opacity, w.Margin = "top-left", 25, 100, 0
	g = tvMarkGraph(tvMark{image: "/c/logo.png", width: 1280, w: w}, "")
	if !strings.Contains(g, "scale=320:-2") || !strings.Contains(g, "aa=1.00") || !strings.Contains(g, "overlay=x=0:y=0:format=auto[v]") {
		t.Errorf("top-left: %s", g)
	}
	w.Position = "top-right"
	g = tvMarkGraph(tvMark{textFile: "/tmp/t.txt", font: "/usr/share/fonts/DejaVuSans-Bold.ttf", width: 1920, w: w}, "format=nv12,hwupload")
	for _, want := range []string{"[0:v:0]drawtext=", "textfile=/tmp/t.txt", "fontsize=120", "fontcolor=white@1.00", "x=w-text_w-0:y=0", ",format=nv12,hwupload[v]"} {
		if !strings.Contains(g, want) {
			t.Errorf("text graph missing %q: %s", want, g)
		}
	}
}

func TestTVTunerArgsWatermark(t *testing.T) {
	// Off: the picture is copied, as before.
	plain := strings.Join(tvTunerArgs("/m/a.mkv", 0, time.Minute, 0, 0, 0, nil, encNVENC), " ")
	if !strings.Contains(plain, "-map 0:v:0") || !strings.Contains(plain, "-c:v copy") || strings.Contains(plain, "nvenc") || strings.Contains(plain, "filter_complex") {
		t.Errorf("no watermark copies: %s", plain)
	}
	m := &tvMark{image: "/c/logo.png", width: 1920, w: tvWatermark{Position: "bottom-left", Size: 10, Opacity: 60, Margin: 2}}
	args := tvTunerArgs("/m/a.mkv", 90*time.Second, 10*time.Minute, 3600*time.Second, 10*time.Second, 1, m, encNVENC)
	j := strings.Join(args, " ")
	for _, want := range []string{"-noaccurate_seek -ss 90.000", "-readrate_initial_burst 10.000 -i /m/a.mkv -i /c/logo.png -t 600.000", "-map [v] -map 0:a:1?", "-c:v h264_nvenc", "-c:a aac", "-output_ts_offset 3600.000", "-f mpegts pipe:1"} {
		if !strings.Contains(j, want) {
			t.Errorf("args missing %q: %s", want, j)
		}
	}
	if strings.Contains(j, "-c:v copy") {
		t.Error("a watermark needs encoding")
	}
	// The CPU fallback is the same with libx264.
	cpu := strings.Join(tvTunerArgs("/m/a.mkv", 0, time.Minute, 0, 0, 0, m, encCPU), " ")
	if !strings.Contains(cpu, "-c:v libx264") || strings.Contains(cpu, "nvenc") {
		t.Errorf("cpu: %s", cpu)
	}
	// VAAPI's device goes before the inputs.
	va := tvTunerArgs("/m/a.mkv", 0, time.Minute, 0, 0, 0, m, encVAAPI)
	if i := slices.Index(va, "-vaapi_device"); i < 0 || i > slices.Index(va, "-i") {
		t.Errorf("vaapi: %v", va)
	}
}

func TestTVBrowserArgsEncoder(t *testing.T) {
	args := tvBrowserArgsWith("http://x/a.avi", 0, false, encNVENC)
	if !has(args, "-c:v", "h264_nvenc") || !has(args, "-vf", "format=yuv420p") {
		t.Errorf("nvenc: %v", args)
	}
	args = tvBrowserArgsWith("http://x/a.avi", 0, true, encVAAPI)
	if !has(args, "-c:v", "copy") || slices.Contains(args, "-vaapi_device") {
		t.Errorf("copy needs no encoder: %v", args)
	}
}

func TestTVPreviewArgs(t *testing.T) {
	m := tvMark{image: "/c/w.png", width: 1280, w: tvWatermark{Position: "top-right", Size: 10, Opacity: 60}}
	j := strings.Join(tvPreviewArgs("", 0, m), " ")
	for _, want := range []string{"-f lavfi -i color=", "-i /c/w.png", "-frames:v 1", "-c:v mjpeg", "pipe:1"} {
		if !strings.Contains(j, want) {
			t.Errorf("preview missing %q: %s", want, j)
		}
	}
	if j := strings.Join(tvPreviewArgs("/m/a.mkv", time.Minute, m), " "); !strings.Contains(j, "-ss 60.000 -i /m/a.mkv") {
		t.Errorf("preview seeks: %s", j)
	}
}
