package comics

import (
	"image"
	"image/color"
	"image/draw"
	"testing"
)

// drawnPage draws dark panels with white gutters on a white page.
func drawnPage(w, h int, boxes ...image.Rectangle) image.Image {
	img := image.NewGray(image.Rect(0, 0, w, h))
	draw.Draw(img, img.Bounds(), image.NewUniform(color.White), image.Point{}, draw.Src)
	for _, b := range boxes {
		draw.Draw(img, b, image.NewUniform(color.Gray{Y: 40}), image.Point{}, draw.Src)
	}
	return img
}

func TestSpreadOrderReadsOnePageThenTheOther(t *testing.T) {
	// A row across both pages, cut as a wide picture, reads right, left, then the
	// next row. A reader wants all of the right page first.
	img := drawnPage(2000, 1400,
		image.Rect(1050, 50, 1950, 650), image.Rect(50, 50, 950, 650),
		image.Rect(1050, 750, 1950, 1350), image.Rect(50, 750, 950, 1350))
	got := SpreadOrder(DetectPanels(img, PanelOptions{RightToLeft: true}), true)
	if len(got) != 4 {
		t.Fatalf("%d panels: %+v", len(got), got)
	}
	if got[0].X < 0.5 || got[1].X < 0.5 || got[2].X > 0.5 || got[3].X > 0.5 {
		t.Errorf("not the right page first: %+v", got)
	}
	if got[0].Y > got[1].Y || got[2].Y > got[3].Y {
		t.Errorf("each page isn't read top to bottom: %+v", got)
	}
	got = SpreadOrder(DetectPanels(img, PanelOptions{}), false)
	if got[0].X > 0.5 || got[1].X > 0.5 || got[2].X < 0.5 || got[3].X < 0.5 {
		t.Errorf("left to right, not the left page first: %+v", got)
	}
}

func TestSpreadOrderLeavesAPanelAcrossTheFold(t *testing.T) {
	in := []Panel{{X: 0.1, Y: 0, W: 0.8, H: 0.5}, {X: 0.6, Y: 0.6, W: 0.3, H: 0.3}, {X: 0.1, Y: 0.6, W: 0.3, H: 0.3}}
	got := SpreadOrder(in, true)
	for i := range in {
		if got[i] != in[i] {
			t.Fatalf("order changed: %+v", got)
		}
	}
}

func TestSpreadWithRowsThatDontLineUp(t *testing.T) {
	img := drawnPage(2000, 1400,
		image.Rect(1050, 50, 1950, 1350),
		image.Rect(50, 50, 950, 650), image.Rect(50, 750, 950, 1350))
	got := SpreadOrder(DetectPanels(img, PanelOptions{RightToLeft: true}), true)
	if len(got) != 3 || got[0].X < 0.5 || got[1].Y > got[2].Y {
		t.Fatalf("%+v", got)
	}
}

func TestDetectStripTopToBottom(t *testing.T) {
	img := drawnPage(800, 6000,
		image.Rect(0, 100, 800, 900), image.Rect(0, 1100, 800, 2000), image.Rect(0, 2200, 800, 5900))
	got := DetectStrip(img)
	if len(got) < 4 {
		t.Fatalf("%d views: %+v", len(got), got)
	}
	for i := 1; i < len(got); i++ {
		if got[i].Y <= got[i-1].Y {
			t.Errorf("not top to bottom: %+v", got)
		}
	}
	// The long last panel is seen whole, a screen at a time, and nothing is too tall.
	last := got[len(got)-1]
	if last.Y+last.H < 0.97 {
		t.Errorf("the end isn't reached: %+v", last)
	}
	for _, p := range got {
		if p.W != 1 || p.H*7.5 > stripTallest+0.2 {
			t.Errorf("too tall a view: %+v", p)
		}
	}
}

func TestDetectStripWithNoGuttersGoesAScreenAtATime(t *testing.T) {
	if got := DetectStrip(drawnPage(800, 4000, image.Rect(0, 8, 800, 3992))); len(got) < 2 {
		t.Errorf("%+v", got)
	}
	if got := DetectStrip(drawnPage(800, 1200)); got != nil {
		t.Errorf("a normal page isn't a strip: %+v", got)
	}
}
