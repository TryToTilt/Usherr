package kosync

import (
	"bytes"
	"fmt"
	"io"
	"testing"
)

// pattern is the test file of the kosync-conformance vectors (spec section 8.6, CC0).
func pattern(n int) []byte {
	b := make([]byte, n)
	for i := range b {
		b[i] = byte((i*31 + (i >> 8)) % 256)
	}
	return b
}

func TestPartialMD5GoldenVectors(t *testing.T) {
	for _, tc := range []struct {
		size int
		want string
	}{
		{500, "21f0df72cce9bc7da8dae2512ee5feed"},
		{1024, "f4cd1641040a17288bb6104d9e66bdb5"},
		{1025, "170a888db01c00d8fc3e5d8d84de5838"},
		{2048, "d0e59a0c7b893c3b8d6a9bddbd64e631"},
		{3000, "d0e59a0c7b893c3b8d6a9bddbd64e631"},
		{200000, "1784b150454ef4cf6780ceb94af0386f"},
		{1050000, "0fca5c751677ecbc2dcfe80ea75ace32"},
	} {
		got, err := PartialMD5(bytes.NewReader(pattern(tc.size)))
		if err != nil {
			t.Fatal(err)
		}
		if got != tc.want {
			t.Errorf("P(%d) = %s, want %s", tc.size, got, tc.want)
		}
	}
}

// sparse is the vectors' big file: zeros, with each offset written as 16 digits at itself.
type sparse struct{ size int64 }

func (s sparse) ReadAt(p []byte, off int64) (int, error) {
	if off >= s.size {
		return 0, io.EOF
	}
	n := int64(len(p))
	short := false
	if off+n > s.size {
		n = s.size - off
		short = true
	}
	clear(p[:n])
	for _, o := range partialOffsets {
		marker := fmt.Sprintf("%016d", o)
		for i := range marker {
			if at := o + int64(i); at >= off && at < off+n {
				p[at-off] = marker[i]
			}
		}
	}
	if short {
		return int(n), io.EOF
	}
	return int(n), nil
}

func TestPartialMD5ReadsAllTwelveSamples(t *testing.T) {
	got, err := PartialMD5(sparse{size: 1073742848})
	if err != nil {
		t.Fatal(err)
	}
	if want := "266ff27c24919f4cec96f90f9b3231ca"; got != want {
		t.Errorf("got %s, want %s", got, want)
	}
}

func TestSpineIndexOfCFI(t *testing.T) {
	for cfi, want := range map[string]int{
		"epubcfi(/6/2!/4/2/1:0)":         0,
		"epubcfi(/6/14!/4/2/6/1:120)":    6,
		"epubcfi(/6/8[chapter3]!/4/2/2)": 3,
		"epubcfi(/6/8[ch]!/4,/2/1:0,/2)": 3,
	} {
		if got, ok := SpineIndexOfCFI(cfi); !ok || got != want {
			t.Errorf("%s = %d %v, want %d", cfi, got, ok, want)
		}
	}
	for _, cfi := range []string{"", "page:4", "epubcfi(/6/3!/4)", "epubcfi(/6/0!/4)", "epubcfi(/4/2!/4)"} {
		if _, ok := SpineIndexOfCFI(cfi); ok {
			t.Errorf("%q should say nothing", cfi)
		}
	}
}

func TestSpineIndexOfPercent(t *testing.T) {
	sizes := []int64{100, 300, 600}
	for pct, want := range map[float64]int{0: 0, 9.9: 0, 10: 1, 39: 1, 40: 2, 99: 2, 100: 2} {
		if got, ok := SpineIndexOfPercent(pct, sizes); !ok || got != want {
			t.Errorf("%v%% = %d %v, want %d", pct, got, ok, want)
		}
	}
	if _, ok := SpineIndexOfPercent(50, nil); ok {
		t.Error("no items should say nothing")
	}
	if got := ChapterXPointer(0); got != "/body/DocFragment[1]/body" {
		t.Errorf("got %s", got)
	}
}
