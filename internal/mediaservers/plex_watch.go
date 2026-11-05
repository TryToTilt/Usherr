package mediaservers

// Usherr: the watch history's part of a Plex session (who, which app, how it
// plays). The rest of a session is read in plex_sessions.go.

import (
	"encoding/json"
	"strings"
)

// plexLooseID is an id Plex writes as a number or as text.
type plexLooseID string

func (p *plexLooseID) UnmarshalJSON(b []byte) error {
	var s string
	if json.Unmarshal(b, &s) == nil {
		*p = plexLooseID(s)
		return nil
	}
	var n json.Number
	if json.Unmarshal(b, &n) == nil {
		*p = plexLooseID(n.String())
	}
	return nil
}

// plexSessionInfo fills in SessionInfo for a Plex session. Plex gives no
// reasons for a transcode, so they are worked out from what it converts.
func plexSessionInfo(it plexSession) SessionInfo {
	si := SessionInfo{ID: it.SessionKey, ItemID: it.RatingKey, UserID: string(it.User.ID), UserName: it.User.Title,
		PositionSec: float64(it.ViewOffset) / 1000, RuntimeSec: float64(it.Duration) / 1000,
		BandwidthKbps: it.Session.Bandwidth, Address: it.Player.Address, Audio: it.Type == "track"}
	if si.ID == "" {
		si.ID = it.Session.ID
	}
	si.Client = it.Player.Product
	if it.Player.Platform != "" && it.Player.Platform != it.Player.Product {
		si.Client = strings.TrimSpace(si.Client + " (" + it.Player.Platform + ")")
	}
	switch {
	case it.Session.Location == "lan":
		si.Location = "lan"
	case it.Session.Location == "wan":
		si.Location = "remote"
	case it.Player.Local != nil && *it.Player.Local:
		si.Location = "lan"
	case it.Player.Local != nil:
		si.Location = "remote"
	default:
		si.Location = LocationOf(it.Player.Address)
	}
	if len(it.Media) > 0 {
		si.VideoIn, si.AudioIn = strings.ToLower(it.Media[0].VideoCodec), strings.ToLower(it.Media[0].AudioCodec)
		if si.BandwidthKbps == 0 {
			si.BandwidthKbps = it.Media[0].Bitrate
		}
	}
	tc := it.TranscodeSession
	if tc != nil && tc.SourceVideoCodec != "" {
		si.VideoIn = strings.ToLower(tc.SourceVideoCodec)
	}
	if tc != nil && tc.SourceAudioCodec != "" {
		si.AudioIn = strings.ToLower(tc.SourceAudioCodec)
	}
	si.VideoOut, si.AudioOut = si.VideoIn, si.AudioIn
	if tc == nil {
		return si
	}
	if tc.VideoDecision == "transcode" {
		if tc.VideoCodec != "" {
			si.VideoOut = strings.ToLower(tc.VideoCodec)
		}
		si.ResolutionOut = ResolutionLabel(tc.Width, tc.Height)
		si.TranscodeReasons = append(si.TranscodeReasons, "Video converted")
	}
	if tc.AudioDecision == "transcode" {
		if tc.AudioCodec != "" {
			si.AudioOut = strings.ToLower(tc.AudioCodec)
		}
		si.TranscodeReasons = append(si.TranscodeReasons, "Audio converted")
	}
	if len(si.TranscodeReasons) == 0 {
		si.TranscodeReasons = []string{"Container changed"}
	}
	return si
}
