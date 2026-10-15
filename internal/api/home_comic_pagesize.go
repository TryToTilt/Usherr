package api

// Home fork: comic pages at the size the screen needs. A phone doesn't need
// a 3,000-pixel, 3 MB scan: ?w=1600 sends a JPEG that wide (made once, kept
// in a "sized" folder beside the unpacked pages in Mediarium's cache, never
// in the library).

import (
	"image"
	"image/color"
	_ "image/gif" // pages in other formats
	"image/jpeg"
	_ "image/png"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
)

var pageSizeMu sync.Mutex // one resize at a time: they're memory-hungry

// sizedPage returns a copy of page no wider than w, or "" when the page is
// already narrow enough (or can't be read): then the original is sent.
func sizedPage(page string, w int) string {
	dir := filepath.Join(filepath.Dir(page), "sized")
	out := filepath.Join(dir, strings.TrimSuffix(filepath.Base(page), filepath.Ext(page))+".w"+strconv.Itoa(w)+".jpg")
	if st, err := os.Stat(out); err == nil && st.Size() > 0 {
		return out
	}
	pageSizeMu.Lock()
	defer pageSizeMu.Unlock()
	f, err := os.Open(page)
	if err != nil {
		return ""
	}
	src, _, err := image.Decode(f)
	f.Close()
	if err != nil || src.Bounds().Dx() <= w {
		return ""
	}
	dst := shrinkTo(src, w)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return ""
	}
	tmp := out + ".part"
	o, err := os.Create(tmp)
	if err != nil {
		return ""
	}
	err = jpeg.Encode(o, dst, &jpeg.Options{Quality: 86})
	if cerr := o.Close(); err == nil {
		err = cerr
	}
	if err != nil || os.Rename(tmp, out) != nil {
		_ = os.Remove(tmp)
		return ""
	}
	return out
}

// shrinkTo scales src down to width w by averaging the pixels each new pixel
// covers (sharp lines stay clean, unlike picking one pixel).
func shrinkTo(src image.Image, w int) *image.RGBA {
	b := src.Bounds()
	sw, sh := b.Dx(), b.Dy()
	h := max(1, sh*w/sw)
	dst := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		y0 := b.Min.Y + y*sh/h
		y1 := max(y0+1, b.Min.Y+(y+1)*sh/h)
		for x := 0; x < w; x++ {
			x0 := b.Min.X + x*sw/w
			x1 := max(x0+1, b.Min.X+(x+1)*sw/w)
			var r, g, bl, n uint64
			for sy := y0; sy < y1; sy++ {
				for sx := x0; sx < x1; sx++ {
					cr, cg, cb, _ := src.At(sx, sy).RGBA()
					r, g, bl, n = r+uint64(cr), g+uint64(cg), bl+uint64(cb), n+1
				}
			}
			dst.SetRGBA(x, y, color.RGBA{uint8(r / n >> 8), uint8(g / n >> 8), uint8(bl / n >> 8), 255})
		}
	}
	return dst
}
