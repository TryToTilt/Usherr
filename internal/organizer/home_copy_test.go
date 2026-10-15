package organizer

import (
	"bytes"
	"strings"
	"syscall"
	"testing"
)

// flakyWriter fails with ENOMEM a few times before accepting writes, like a
// Docker Desktop Windows folder mount under memory pressure.
type flakyWriter struct {
	bytes.Buffer
	fails int
}

func (f *flakyWriter) Write(p []byte) (int, error) {
	if f.fails > 0 {
		f.fails--
		return 0, syscall.ENOMEM
	}
	return f.Buffer.Write(p)
}

func TestCopyChunksRetriesOutOfMemoryWrites(t *testing.T) {
	src := strings.Repeat("x", 300<<10)
	w := &flakyWriter{fails: 2}
	if err := copyChunks(w, strings.NewReader(src)); err != nil {
		t.Fatalf("copy should get past temporary ENOMEM: %v", err)
	}
	if w.Len() != len(src) {
		t.Fatalf("copied %d bytes, want %d", w.Len(), len(src))
	}
}
