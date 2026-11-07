package download

import (
	"context"
	"fmt"
	"io"
	"math/rand/v2"
	"runtime"
	"sync"
	"testing"
	"time"
)

// Usherr: how the read path behaves against a fake news server that answers
// each article as slowly as a real provider does at its median (236 ms, from a
// month of provider latency seen at home). No real Usenet is used. Run them
// once each:
//
//	go test ./internal/download -run '^$' -bench 'BenchmarkStream' -benchtime=1x
//
// They report what a viewer feels: MB/s when a player reads as fast as it can
// (a remux, or Plex analysing), how long the first bytes take after a jump,
// how long a hung connection holds a read up, and how much memory stays in use.

const (
	benchArticle = 716800 // what most posters use
	benchLatency = 236 * time.Millisecond
)

// benchFile is a video of n articles on a server that takes latency to answer.
type benchFile struct {
	nzb  *NZB
	size int64
}

func benchBodies(n int) ([][]byte, int64) {
	r := rand.New(rand.NewPCG(1, 2))
	data := make([]byte, n*benchArticle)
	for i := range data {
		data[i] = byte(r.Uint32())
	}
	copy(data, []byte{0x1a, 0x45, 0xdf, 0xa3}) // starts as a Matroska file does
	out := make([][]byte, n)
	for i := range out {
		out[i] = buildMultipartYenc("bench.mkv", data, i*benchArticle+1, (i+1)*benchArticle)
	}
	return out, int64(len(data))
}

func benchFiles(bodies [][]byte, size int64, count int) (map[string][]byte, []benchFile) {
	all := map[string][]byte{}
	files := make([]benchFile, count)
	for f := range files {
		nf := NZBFile{Subject: fmt.Sprintf(`[%d/%d] "bench%d.mkv" yEnc (1/%d)`, f+1, count, f, len(bodies))}
		for i, b := range bodies {
			id := fmt.Sprintf("f%d-a%d@bench", f, i)
			all[id] = b
			nf.Segments = append(nf.Segments, NZBSegment{Number: i + 1, Bytes: int64(len(b)), MessageID: id})
		}
		files[f] = benchFile{nzb: &NZB{Files: []NZBFile{nf}}, size: size}
	}
	return all, files
}

func benchServer(b *testing.B, bodies map[string][]byte) []ClientConfig {
	b.Helper()
	srv := newFakeNNTPServerTB(b, bodies)
	srv.latency.Store(int64(benchLatency))
	return []ClientConfig{{Host: srv.addr, Port: srv.port, Connections: 80}}
}

func BenchmarkStreamSequential(b *testing.B) {
	bodies, size := benchBodies(300)
	all, files := benchFiles(bodies, size, 1)
	servers := benchServer(b, all)
	for b.Loop() {
		f, err := OpenStream(context.Background(), servers, files[0].nzb)
		if err != nil {
			b.Fatal(err)
		}
		start := time.Now()
		n, err := io.Copy(io.Discard, io.NewSectionReader(f, 0, f.Size))
		if err != nil {
			b.Fatal(err)
		}
		b.ReportMetric(float64(n)/(1<<20)/time.Since(start).Seconds(), "MB/s")
		b.ReportMetric(0, "ns/op")
	}
}

func BenchmarkStreamSeek(b *testing.B) {
	bodies, size := benchBodies(300)
	all, files := benchFiles(bodies, size, 1)
	servers := benchServer(b, all)
	for b.Loop() {
		f, err := OpenStream(context.Background(), servers, files[0].nzb)
		if err != nil {
			b.Fatal(err)
		}
		// Play the start a while, so read-ahead is under way, then jump.
		buf := make([]byte, 256<<10)
		for off := int64(0); off < 8*benchArticle; off += int64(len(buf)) {
			if _, err := f.ReadAt(buf, off); err != nil {
				b.Fatal(err)
			}
		}
		var worst, sum time.Duration
		const jumps = 5
		for j := range jumps {
			start := time.Now()
			if _, err := f.ReadAt(buf[:64<<10], int64(100+j*40)*benchArticle); err != nil {
				b.Fatal(err)
			}
			took := time.Since(start)
			sum += took
			worst = max(worst, took)
		}
		b.ReportMetric(float64(sum.Milliseconds())/jumps, "ms/seek")
		b.ReportMetric(float64(worst.Milliseconds()), "ms/worst-seek")
		b.ReportMetric(0, "ns/op")
	}
}

// One connection hangs on an article, the way a stuck socket does: nothing
// comes, and nothing says so. How long is the reader held up?
func BenchmarkStreamHungConnection(b *testing.B) {
	bodies, size := benchBodies(40)
	all, files := benchFiles(bodies, size, 1)
	srv := newFakeNNTPServerTB(b, all)
	srv.latency.Store(int64(benchLatency))
	servers := []ClientConfig{{Host: srv.addr, Port: srv.port, Connections: 80}}
	for b.Loop() {
		srv.hangRequests("f0-a30@bench", 1)
		f, err := OpenStream(context.Background(), servers, files[0].nzb)
		if err != nil {
			b.Fatal(err)
		}
		buf := make([]byte, 256<<10)
		var worst time.Duration
		for off := int64(0); off < f.Size; off += int64(len(buf)) {
			start := time.Now()
			n, err := f.ReadAt(buf, off)
			if err != nil && err != io.EOF {
				b.Logf("read failed after %v: %v", time.Since(start), err)
			}
			if n == 0 {
				off -= int64(len(buf)) // read it again
			}
			worst = max(worst, time.Since(start))
		}
		b.ReportMetric(worst.Seconds(), "s/worst-read")
		b.ReportMetric(0, "ns/op")
	}
}

// Eight streams read at once and then held, as sessions are, for hours.
func BenchmarkStreamMemory(b *testing.B) {
	bodies, size := benchBodies(300)
	all, files := benchFiles(bodies, size, 8)
	servers := benchServer(b, all)
	for b.Loop() {
		opened := make([]*StreamFile, len(files))
		var wg sync.WaitGroup
		for i := range files {
			wg.Add(1)
			go func(i int) {
				defer wg.Done()
				f, err := OpenStream(context.Background(), servers, files[i].nzb)
				if err != nil {
					b.Error(err)
					return
				}
				opened[i] = f
				if _, err := io.Copy(io.Discard, io.NewSectionReader(f, 0, f.Size)); err != nil {
					b.Error(err)
				}
			}(i)
		}
		wg.Wait()
		runtime.GC()
		var m runtime.MemStats
		runtime.ReadMemStats(&m)
		b.ReportMetric(float64(m.HeapAlloc-uint64(len(bodies)*benchArticle*14/10))/(1<<20), "MB-held")
		runtime.KeepAlive(opened)
		b.ReportMetric(0, "ns/op")
	}
}
