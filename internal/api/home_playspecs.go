package api

// Usherr: what a file or stream Usherr plays really is — its picture (size,
// shape, HDR or Dolby Vision) and its sound (Dolby Atmos, DTS:X, TrueHD...,
// and channels) — for the Marquee's badges. Read once per file or stream with
// ffprobe, in the background (a stream's first read can take a while), and
// remembered. A picture stored with black bars (a widescreen film in a 16:9
// frame) is measured a few seconds in, so the shape is the film's own.

import (
	"context"
	"encoding/json"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/rdborg/mediarium/internal/mediaservers"
)

type playSpecs struct {
	resolution, hdr, sound, channels, aspect string
}

type playSpecsEntry struct {
	done  bool
	specs playSpecs
	at    time.Time
}

var (
	playSpecsMu    sync.Mutex
	playSpecsCache = map[string]*playSpecsEntry{} // file path or stream address -> what it is
	playSpecsSlot  = make(chan struct{}, 1)       // one look at a time
)

// playSpecsFor is what source is, once it has been looked at; the first ask
// starts the look in the background and answers false.
func playSpecsFor(source string) (playSpecs, bool) {
	if source == "" {
		return playSpecs{}, false
	}
	playSpecsMu.Lock()
	e, ok := playSpecsCache[source]
	if !ok {
		e = &playSpecsEntry{at: time.Now()}
		playSpecsCache[source] = e
		for k, v := range playSpecsCache { // keep the list small
			if time.Since(v.at) > 12*time.Hour {
				delete(playSpecsCache, k)
			}
		}
	}
	done, specs := e.done, e.specs
	playSpecsMu.Unlock()
	if !ok {
		go func() {
			playSpecsSlot <- struct{}{}
			defer func() { <-playSpecsSlot }()
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
			defer cancel()
			sp := probePlaySpecs(ctx, source)
			playSpecsMu.Lock()
			e.done, e.specs = true, sp
			playSpecsMu.Unlock()
		}()
	}
	return specs, done
}

// probePlaySpecs reads source's picture and its first sound track.
func probePlaySpecs(ctx context.Context, source string) playSpecs {
	var sp playSpecs
	out, err := exec.CommandContext(ctx, "ffprobe", "-v", "error", "-rw_timeout", "30000000",
		"-show_entries", "format=duration:stream=codec_type,codec_name,profile,width,height,sample_aspect_ratio,channels,color_transfer:stream_tags=title:stream_side_data=side_data_type",
		"-of", "json", source).Output()
	if err != nil {
		return sp
	}
	var res struct {
		Format struct {
			Duration string `json:"duration"`
		} `json:"format"`
		Streams []struct {
			Type     string            `json:"codec_type"`
			Codec    string            `json:"codec_name"`
			Profile  string            `json:"profile"`
			Width    int               `json:"width"`
			Height   int               `json:"height"`
			SAR      string            `json:"sample_aspect_ratio"`
			Channels int               `json:"channels"`
			Transfer string            `json:"color_transfer"`
			Tags     map[string]string `json:"tags"`
			Side     []struct {
				Type string `json:"side_data_type"`
			} `json:"side_data_list"`
		} `json:"streams"`
	}
	if json.Unmarshal(out, &res) != nil {
		return sp
	}
	video, sound := -1, -1
	for i, st := range res.Streams {
		switch {
		case st.Type == "video" && video < 0 && st.Codec != "mjpeg" && st.Codec != "png":
			video = i
		case st.Type == "audio" && sound < 0:
			sound = i
		}
	}
	if video >= 0 {
		v := res.Streams[video]
		sp.resolution = mediaservers.ResolutionLabel(v.Width, v.Height)
		for _, sd := range v.Side {
			if strings.Contains(strings.ToUpper(sd.Type), "DOVI") {
				sp.hdr = "Dolby Vision"
			}
		}
		if sp.hdr == "" {
			switch v.Transfer {
			case "smpte2084":
				sp.hdr = "HDR10"
			case "arib-std-b67":
				sp.hdr = "HLG"
			}
		}
		w, h := float64(v.Width)*sarOf(v.SAR), float64(v.Height)
		// A 16:9 (or narrower) frame may hold a wider film between black
		// bars: look a few seconds in.
		if w > 0 && h > 0 && w/h < 1.82 {
			dur, _ := strconv.ParseFloat(res.Format.Duration, 64)
			if cw, ch, ok := cropOf(ctx, source, dur); ok && float64(ch) < 0.93*float64(v.Height) {
				w, h = float64(cw)*sarOf(v.SAR), float64(ch)
			}
		}
		sp.aspect = mediaservers.AspectLabel(w, h)
	}
	if sound >= 0 {
		a := res.Streams[sound]
		sp.sound, sp.channels = mediaservers.SoundLabel(a.Codec, a.Profile+" "+a.Tags["title"], a.Channels)
	}
	return sp
}

// sarOf reads a sample aspect ratio ("1:1", "4:3"); 1 when it says nothing.
func sarOf(s string) float64 {
	a, b, ok := strings.Cut(s, ":")
	x, _ := strconv.ParseFloat(a, 64)
	y, _ := strconv.ParseFloat(b, 64)
	if !ok || x <= 0 || y <= 0 {
		return 1
	}
	return x / y
}

var cropLine = regexp.MustCompile(`crop=(\d+):(\d+):\d+:\d+`)

// cropOf finds the picture inside black bars, from three seconds about a
// third of the way in (the opening titles can be darker than the film).
func cropOf(ctx context.Context, source string, duration float64) (w, h int, ok bool) {
	at := 0.0
	if duration > 120 {
		at = duration * 0.3
	}
	ctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	args := []string{"-hide_banner", "-nostdin", "-rw_timeout", "30000000"}
	if at > 0 {
		args = append(args, "-ss", strconv.FormatFloat(at, 'f', 1, 64))
	}
	args = append(args, "-i", source, "-t", "3", "-map", "0:v:0", "-vf", "cropdetect=limit=0.1:round=2:reset=0", "-an", "-sn", "-f", "null", "-")
	out, _ := exec.CommandContext(ctx, "ffmpeg", args...).CombinedOutput()
	m := cropLine.FindAllStringSubmatch(string(out), -1)
	if len(m) == 0 {
		return 0, 0, false
	}
	last := m[len(m)-1]
	w, _ = strconv.Atoi(last[1])
	h, _ = strconv.Atoi(last[2])
	return w, h, w > 0 && h > 0
}

// librarySpecs is what a library file is, from the details Mediarium reads
// from its headers in the background (media_info.go), so no look of Usherr's
// own is needed. needShape is true when the frame could hold a wider film
// between black bars: only a look at the picture can tell its real shape.
func (s *Server) librarySpecs(path string) (sp playSpecs, needShape, ok bool) {
	info := s.movieFileInfo(path)
	if info == nil {
		return sp, false, false
	}
	if v := info.Video; v != nil {
		sp.resolution = mediaservers.ResolutionLabel(v.Width, v.Height)
		if v.HDR != "SDR" {
			sp.hdr = v.HDR
		}
		sp.aspect = mediaservers.AspectLabel(float64(v.Width), float64(v.Height))
		needShape = v.Height > 0 && float64(v.Width)/float64(v.Height) < 1.82
	}
	if len(info.Audio) > 0 {
		a := info.Audio[0]
		for _, x := range info.Audio {
			if x.Default {
				a = x
				break
			}
		}
		about := a.Codec + " " + a.Title
		if a.Atmos {
			about += " atmos"
		}
		sp.sound, sp.channels = mediaservers.SoundLabel(mediaInfoCodec(a.Codec), about, a.Channels)
	}
	return sp, needShape, true
}

// mediaInfoCodec turns the media info's codec names ("E-AC-3", "DTS-HD MA")
// into ffprobe's, which SoundLabel reads.
func mediaInfoCodec(c string) string {
	switch strings.ToUpper(c) {
	case "E-AC-3":
		return "eac3"
	case "AC-3":
		return "ac3"
	case "TRUEHD":
		return "truehd"
	case "FLAC":
		return "flac"
	}
	if strings.HasPrefix(strings.ToUpper(c), "DTS") {
		return "dts"
	}
	return strings.ToLower(c)
}

// releaseSound names the sound from a release's name ("…TrueHD.7.1.Atmos…"),
// for a stream not yet looked at.
var releaseChannels = regexp.MustCompile(`(?i)(?:^|[^0-9])([1-8])[. ]([01])(?:[^0-9]|$)`)

func releaseSound(release string) (sound, chans string) {
	l := strings.ToLower(release)
	switch {
	case strings.Contains(l, "atmos"):
		sound = "Dolby Atmos"
	case strings.Contains(l, "dts-x") || strings.Contains(l, "dts.x") || strings.Contains(l, "dts:x"):
		sound = "DTS:X"
	case strings.Contains(l, "truehd"):
		sound = "TrueHD"
	case strings.Contains(l, "dts-hd") || strings.Contains(l, "dts.hd"):
		sound = "DTS-HD MA"
	case strings.Contains(l, "ddp") || strings.Contains(l, "dd+") || strings.Contains(l, "eac3"):
		sound = "Dolby Digital+"
	case strings.Contains(l, ".dd5") || strings.Contains(l, ".dd.") || strings.Contains(l, "ac3"):
		sound = "Dolby Digital"
	case strings.Contains(l, "dts"):
		sound = "DTS"
	}
	if m := releaseChannels.FindStringSubmatch(release); m != nil {
		switch m[1] + "." + m[2] {
		case "7.1":
			chans = "7.1"
		case "5.1":
			chans = "5.1"
		case "2.0":
			chans = "Stereo"
		}
	}
	return sound, chans
}
