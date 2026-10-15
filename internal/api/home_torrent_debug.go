package api

// Home fork: GET /api/home/torrent-debug (administrators) shows what each
// running torrent sees: whether it has its metadata yet, peers known and
// connected, and its trackers. For finding out why a torrent sits at 0%.

import (
	"encoding/binary"
	"net"
	"net/http"
	"sort"
	"time"
)

func (s *Server) handleTorrentDebug(w http.ResponseWriter, r *http.Request) {
	type row struct {
		QueueID   int64    `json:"queueId"`
		Name      string   `json:"name"`
		HasInfo   bool     `json:"hasInfo"`
		Total     int      `json:"totalPeers"`
		Active    int      `json:"activePeers"`
		Pending   int      `json:"pendingPeers"`
		HalfOpen  int      `json:"halfOpenPeers"`
		Seeders   int      `json:"connectedSeeders"`
		Completed int64    `json:"bytesCompleted"`
		Trackers  []string `json:"trackers"`
	}
	reg := &s.torrents
	reg.mu.Lock()
	var out []row
	for id, job := range reg.jobs {
		if job.t == nil {
			out = append(out, row{QueueID: id, Name: "(adding)"})
			continue
		}
		t := job.t
		st := t.Stats()
		rw := row{QueueID: id, Name: t.Name(), HasInfo: t.Info() != nil, Total: st.TotalPeers, Active: st.ActivePeers,
			Pending: st.PendingPeers, HalfOpen: st.HalfOpenPeers, Seeders: st.ConnectedSeeders, Completed: t.BytesCompleted()}
		for _, tier := range t.Metainfo().AnnounceList {
			rw.Trackers = append(rw.Trackers, tier...)
		}
		out = append(out, rw)
	}
	// Torrents the engines hold that aren't attached to a job yet (still
	// waiting for their metadata).
	for _, e := range reg.engines {
		for _, t := range e.tc.AllTorrents() {
			attached := false
			for _, job := range reg.jobs {
				if job.t == t {
					attached = true
				}
			}
			if attached {
				continue
			}
			st := t.Stats()
			rw := row{QueueID: -1, Name: t.Name(), HasInfo: t.Info() != nil, Total: st.TotalPeers, Active: st.ActivePeers,
				Pending: st.PendingPeers, HalfOpen: st.HalfOpenPeers, Seeders: st.ConnectedSeeders}
			for _, tier := range t.Metainfo().AnnounceList {
				rw.Trackers = append(rw.Trackers, tier...)
			}
			out = append(out, rw)
		}
	}
	reg.mu.Unlock()
	sort.Slice(out, func(i, j int) bool { return out[i].QueueID < out[j].QueueID })
	writeJSON(w, http.StatusOK, out)
}

// GET /api/home/vpn-udp-test: sends a UDP tracker "connect" request to
// tracker.opentrackr.org through the VPN tunnel and reports whether the
// tracker answered (home fork: diagnostics).
func (s *Server) handleVPNUDPTest(w http.ResponseWriter, r *http.Request) {
	t := s.VPNManager.Tunnel()
	if t == nil {
		writeError(w, http.StatusConflict, "The VPN isn't connected.")
		return
	}
	res := map[string]any{}
	addrs, err := t.LookupHost(r.Context(), "tracker.opentrackr.org")
	res["lookup"], res["lookupErr"] = addrs, errString(err)
	var ip net.IP
	for _, a := range addrs {
		if p := net.ParseIP(a); p != nil && p.To4() != nil {
			ip = p
			break
		}
	}
	if ip == nil {
		writeJSON(w, http.StatusOK, res)
		return
	}
	pc, err := t.ListenPacket("udp4", ":0")
	if err != nil {
		res["listenErr"] = err.Error()
		writeJSON(w, http.StatusOK, res)
		return
	}
	defer pc.Close()
	req := make([]byte, 16)
	binary.BigEndian.PutUint64(req[0:], 0x41727101980) // protocol id
	binary.BigEndian.PutUint32(req[8:], 0)             // action: connect
	binary.BigEndian.PutUint32(req[12:], 0x1234abcd)   // transaction id
	_, err = pc.WriteTo(req, &net.UDPAddr{IP: ip, Port: 1337})
	res["writeErr"] = errString(err)
	_ = pc.SetReadDeadline(time.Now().Add(8 * time.Second))
	buf := make([]byte, 64)
	n, from, err := pc.ReadFrom(buf)
	res["readErr"], res["replyBytes"] = errString(err), n
	if from != nil {
		res["from"] = from.String()
	}
	writeJSON(w, http.StatusOK, res)
}

func errString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}
