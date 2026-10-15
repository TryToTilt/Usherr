package api

import "testing"

func TestStreamedPath(t *testing.T) {
	for p, want := range map[string]bool{
		`F:\Movies-Stream\Alive (2020)\Alive.mkv`:    true,
		`F:\TV Shows-Stream\Severance`:               true,
		`E:\Streamed\Movies\Coco (2017)\Coco.strm`:   true,
		`/data/streamed/tv/Elite`:                    true,
		`D:\Movies\Coco (2017)\Coco.mkv`:             false,
		`G:\TV Shows\Severance`:                      false,
		`D:\Movies\Livestream (2019)\Livestream.mkv`: false,
	} {
		if got := streamedPath(p); got != want {
			t.Fatalf("streamedPath(%q) = %v, want %v", p, got, want)
		}
	}
}
