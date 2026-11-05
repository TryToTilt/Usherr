// Package watchlog keeps the watch history (Usherr): one row per playback on
// Plex, Jellyfin or Emby, recorded from what the servers say is playing, and
// the numbers worked out from it (hours per day, per person, top titles, how
// things play). It does the job of Streamystats or Tautulli.
//
// A Tracker is told, every half minute or so, what one server is playing. A
// playback that is new gets a row; one that is still there has its time and
// progress brought up to date; one that is gone has ended. The time watched
// counts only the stretches it was playing, so a pause adds nothing.
package watchlog

import (
	"strings"
	"time"
)

// Completed is how far through a playback counts as watched to the end.
const Completed = 0.9

// maxStep is the most one check can add to the time watched, so a gap (Usherr
// restarting, a server that didn't answer for a while) is not counted whole.
const maxStep = 150 * time.Second

// Session is one playback.
type Session struct {
	ID               int64     `json:"id"`
	ServerID         int64     `json:"serverId"`
	ServerName       string    `json:"server"`
	ServerKind       string    `json:"serverKind"`
	Key              string    `json:"-"` // the server's session and item ids
	ServerUserID     string    `json:"-"`
	UserName         string    `json:"user"`
	UserID           int64     `json:"userId,omitempty"` // the Usherr account, 0 when not matched
	Kind             string    `json:"kind"`             // movie, episode or other
	Title            string    `json:"title"`
	Series           string    `json:"series,omitempty"`
	Season           int       `json:"season,omitempty"`
	Episode          int       `json:"episode,omitempty"`
	Year             int       `json:"year,omitempty"`
	TMDBID           int       `json:"tmdbId,omitempty"`    // the film's or the show's
	LibraryID        int64     `json:"libraryId,omitempty"` // the Usherr movie or show
	Client           string    `json:"client,omitempty"`
	Device           string    `json:"device,omitempty"`
	PlayMethod       string    `json:"playMethod,omitempty"` // DirectPlay, DirectStream or Transcode
	TranscodeReasons []string  `json:"transcodeReasons,omitempty"`
	VideoIn          string    `json:"videoIn,omitempty"`
	VideoOut         string    `json:"videoOut,omitempty"`
	AudioIn          string    `json:"audioIn,omitempty"`
	AudioOut         string    `json:"audioOut,omitempty"`
	ResolutionIn     string    `json:"resolutionIn,omitempty"`
	ResolutionOut    string    `json:"resolutionOut,omitempty"`
	BandwidthKbps    int       `json:"bandwidthKbps,omitempty"`
	Location         string    `json:"location,omitempty"` // lan or remote
	Address          string    `json:"address,omitempty"`
	StartedAt        time.Time `json:"startedAt"`
	EndedAt          time.Time `json:"endedAt,omitzero"` // zero while playing
	LastSeen         time.Time `json:"-"`
	WatchedSec       int       `json:"watchedSec"`
	Progress         float64   `json:"progress"` // 0..1, the furthest it got
	RuntimeSec       int       `json:"runtimeSec,omitempty"`
	Paused           bool      `json:"paused"`
	Completed        bool      `json:"completed"`
}

// Seen is one playback as a server describes it right now.
type Seen struct {
	Key              string // stable for one playback of one item on one server
	ServerUserID     string
	UserName         string
	Kind             string
	Title            string
	Series           string
	Season, Episode  int
	Year             int
	TMDBID           int
	LibraryID        int64
	UserID           int64
	Client, Device   string
	PlayMethod       string
	TranscodeReasons []string
	VideoIn          string
	VideoOut         string
	AudioIn          string
	AudioOut         string
	ResolutionIn     string
	ResolutionOut    string
	BandwidthKbps    int
	Location         string
	Address          string
	PositionSec      float64
	RuntimeSec       float64
	Paused           bool
}

// PlayMethodName is "direct", "stream" or "transcode" for a play method as a
// server writes it.
func PlayMethodName(m string) string {
	switch strings.ToLower(strings.ReplaceAll(m, " ", "")) {
	case "":
		return ""
	case "directplay":
		return "direct"
	case "directstream", "copy":
		return "stream"
	}
	return "transcode"
}
