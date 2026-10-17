package api

// Usherr: Live TV as an HDHomeRun tuner, for Plex's Live TV & DVR (and
// anything else that finds tuners that way). Plex is given
//
//	<base>/api/tvfeed/{key}/hdhr
//
// as the tuner's address and reads discover.json, lineup.json and
// lineup_status.json under it; each channel plays from the same live stream
// the M3U lists. The guide is the same XMLTV file.

import (
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"net/http"
	"strings"
)

func (s *Server) registerTVHDHRRoutes(public *http.ServeMux) {
	public.HandleFunc("GET /api/tvfeed/{key}/hdhr/discover.json", s.handleHDHRDiscover)
	public.HandleFunc("GET /api/tvfeed/{key}/hdhr/lineup.json", s.handleHDHRLineup)
	public.HandleFunc("GET /api/tvfeed/{key}/hdhr/lineup_status.json", s.handleHDHRStatus)
	public.HandleFunc("GET /api/tvfeed/{key}/hdhr/device.xml", s.handleHDHRDevice)
}

func (s *Server) hdhrBase(r *http.Request) string {
	return strings.TrimRight(tvFeedBase(r), "/") + "/api/tvfeed/" + r.PathValue("key")
}

// hdhrID is a device id that stays the same for this server's key.
func (s *Server) hdhrID() string {
	sum := sha1.Sum([]byte("usherr-tv:" + s.tvFeedKey()))
	return strings.ToUpper(hex.EncodeToString(sum[:4]))
}

func (s *Server) handleHDHRDiscover(w http.ResponseWriter, r *http.Request) {
	if !s.tvFeedOK(r) {
		http.NotFound(w, r)
		return
	}
	base := s.hdhrBase(r) + "/hdhr"
	writeJSON(w, http.StatusOK, map[string]any{
		"FriendlyName": "Usherr TV", "Manufacturer": "Usherr", "ModelNumber": "HDTC-2US", "FirmwareName": "hdhomeruntc_atsc",
		"FirmwareVersion": "20240101", "DeviceID": s.hdhrID(), "DeviceAuth": "usherr", "TunerCount": maxTVStreams,
		"BaseURL": base, "LineupURL": base + "/lineup.json",
	})
}

func (s *Server) handleHDHRStatus(w http.ResponseWriter, r *http.Request) {
	if !s.tvFeedOK(r) {
		http.NotFound(w, r)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ScanInProgress": 0, "ScanPossible": 1, "Source": "Cable", "SourceList": []string{"Cable"}})
}

func (s *Server) handleHDHRLineup(w http.ResponseWriter, r *http.Request) {
	if !s.tvFeedOK(r) {
		http.NotFound(w, r)
		return
	}
	chans, err := s.tvRepo().Channels()
	if err != nil {
		http.Error(w, "unavailable", http.StatusInternalServerError)
		return
	}
	base := s.hdhrBase(r)
	type entry struct {
		GuideNumber string `json:"GuideNumber"`
		GuideName   string `json:"GuideName"`
		URL         string `json:"URL"`
	}
	out := []entry{}
	for _, c := range chans {
		if !c.Enabled {
			continue
		}
		out = append(out, entry{GuideNumber: fmt.Sprint(c.Number), GuideName: c.Name, URL: fmt.Sprintf("%s/stream/%d.ts", base, c.Number)})
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) handleHDHRDevice(w http.ResponseWriter, r *http.Request) {
	if !s.tvFeedOK(r) {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "application/xml")
	_, _ = fmt.Fprintf(w, `<?xml version="1.0" encoding="UTF-8"?>
<root xmlns="urn:schemas-upnp-org:device-1-0"><specVersion><major>1</major><minor>0</minor></specVersion>
<URLBase>%s/hdhr</URLBase><device><deviceType>urn:schemas-upnp-org:device:MediaServer:1</deviceType>
<friendlyName>Usherr TV</friendlyName><manufacturer>Usherr</manufacturer><modelName>HDTC-2US</modelName><modelNumber>HDTC-2US</modelNumber>
<serialNumber>%s</serialNumber><UDN>uuid:%s</UDN></device></root>`, s.hdhrBase(r), s.hdhrID(), s.hdhrID())
}
