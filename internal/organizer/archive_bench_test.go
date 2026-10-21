package organizer_test

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/rdborg/mediarium/internal/organizer"
)

// benchSet writes a stored multi-volume RAR set of mb megabytes (in volumes
// of about 20 MB) into dir and returns the first volume.
func benchSet(tb testing.TB, dir string, mb int) string {
	tb.Helper()
	data := make([]byte, mb<<20)
	for i := range data {
		data[i] = byte(i * 31)
	}
	names := make([]string, max(2, mb/20))
	for i := range names {
		names[i] = fmt.Sprintf("movie.part%03d.rar", i+1)
	}
	rarSplit(tb, dir, names, true, rarEntry{name: "movie.mkv", data: data})
	return filepath.Join(dir, names[0])
}

// BenchmarkExtractRARStored times unpacking a stored multi-volume RAR set.
// MEDIARIUM_BENCH_MB sets the size (default 200) and MEDIARIUM_BENCH_DIR where
// the set and the output go (default a temp folder), so the same benchmark can
// be pointed at a slow file system.
func BenchmarkExtractRARStored(b *testing.B) {
	mb := 200
	if v, err := strconv.Atoi(os.Getenv("MEDIARIUM_BENCH_MB")); err == nil && v > 0 {
		mb = v
	}
	root := b.TempDir()
	if d := os.Getenv("MEDIARIUM_BENCH_DIR"); d != "" {
		root = d
		b.Cleanup(func() { os.RemoveAll(d) })
	}
	first := benchSet(b, root, mb)
	b.SetBytes(int64(mb) << 20)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		dest := filepath.Join(root, fmt.Sprintf("out%d", i))
		if err := organizer.NewExtractor().Extract(first, dest); err != nil {
			b.Fatal(err)
		}
		b.StopTimer()
		os.RemoveAll(dest)
		b.StartTimer()
	}
}
