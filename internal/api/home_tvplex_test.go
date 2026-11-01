package api

import (
	"context"
	"encoding/json"
	"fmt"
	"html"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

// fakeDVRPlex records Plex's Live TV setup calls.
type fakeDVRPlex struct {
	mu    sync.Mutex
	calls []string
	dvrs  []map[string]any
}

func (f *fakeDVRPlex) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	w.Header().Set("Content-Type", "application/json")
	send := func(c map[string]any) { _ = json.NewEncoder(w).Encode(map[string]any{"MediaContainer": c}) }
	q := r.URL.Query()
	switch {
	case r.Method == http.MethodGet && r.URL.Path == "/livetv/dvrs":
		// In XML, as Plex may answer.
		w.Header().Set("Content-Type", "application/xml")
		var b strings.Builder
		b.WriteString(`<MediaContainer size="1">`)
		for _, d := range f.dvrs {
			fmt.Fprintf(&b, `<Dvr key="%s" lineup="%s">`, d["key"], html.EscapeString(fmt.Sprint(d["lineup"])))
			for _, dev := range d["Device"].([]map[string]any) {
				fmt.Fprintf(&b, `<Device uuid="%s" uri="%s"/>`, dev["uuid"], dev["uri"])
			}
			b.WriteString(`</Dvr>`)
		}
		b.WriteString(`</MediaContainer>`)
		_, _ = w.Write([]byte(b.String()))
	case r.Method == http.MethodPost && r.URL.Path == "/media/grabbers/devices":
		f.calls = append(f.calls, "tuner "+q.Get("uri"))
		send(map[string]any{"Device": []map[string]any{{"uuid": "device://tv.plex.grabbers.hdhomerun/ABCD1234"}}})
	case r.Method == http.MethodPost && r.URL.Path == "/livetv/dvrs":
		f.calls = append(f.calls, "dvr "+q.Get("device")+" "+q.Get("lineup"))
		f.dvrs = append(f.dvrs, map[string]any{"key": "7", "lineup": q.Get("lineup"), "Device": []map[string]any{{"uuid": q.Get("device"), "uri": "http://192.168.1.20:8264"}}})
		// Nothing back: Usherr looks the DVR up.
	case r.Method == http.MethodGet && r.URL.Path == "/media/grabbers/devices":
		send(map[string]any{"Device": []map[string]any{{"key": "/media/grabbers/devices/1", "uuid": "device://tv.plex.grabbers.hdhomerun/ABCD1234"}}})
	case r.Method == http.MethodGet && r.URL.Path == "/media/grabbers/devices/1/channels":
		send(map[string]any{"DeviceChannel": []map[string]any{{"identifier": "1", "name": "Sitcoms"}, {"identifier": "2", "name": "Cartoons"}, {"identifier": "9", "name": "Not in the guide"}}})
	case r.Method == http.MethodGet && r.URL.Path == "/livetv/epg/lineupchannels":
		// Inside a Lineup, as Plex gives them.
		send(map[string]any{"Lineup": []map[string]any{{"Channel": []map[string]any{{"identifier": "1", "channelVcn": "1", "key": "k1"}, {"identifier": "2", "channelVcn": "2", "key": "k2"}}}}})
	case r.Method == http.MethodPut && r.URL.Path == "/media/grabbers/devices/1/channelmap":
		f.calls = append(f.calls, "map "+q.Get("channelsEnabled")+" "+q.Get("channelMapping[1]")+"/"+q.Get("channelMappingByKey[2]"))
	case r.Method == http.MethodDelete && r.URL.Path == "/livetv/dvrs/7":
		f.calls = append(f.calls, "delete 7")
		f.dvrs = nil
	case r.Method == http.MethodPost && r.URL.Path == "/livetv/dvrs/7/reloadGuide":
		f.calls = append(f.calls, "guide 7")
	default:
		http.NotFound(w, r)
	}
}

// Add to Plex: the tuner at the root of Usherr's address, a DVR of it with
// Usherr's guide, the guide read; pressed again, only the guide is read.
// The root tuner addresses answer only once it's been pressed.
func TestTVAddToPlex(t *testing.T) {
	f := &fakeDVRPlex{}
	s, servers := prerollServer(t, &fakePrerollPlex{})
	ps := httptest.NewServer(f)
	defer ps.Close()
	plex := servers[0]
	plex.BaseURL = ps.URL
	if _, err := s.MediaServers.Update(plex); err != nil {
		t.Fatal(err)
	}
	_ = s.Settings.Set(keyHomeAddress, "http://192.168.1.20:8264", false)

	root := http.NewServeMux()
	s.registerTVPlexRoutes(root)
	ask := func() int {
		w := httptest.NewRecorder()
		root.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "http://192.168.1.20:8264/discover.json", nil))
		return w.Code
	}
	if code := ask(); code != http.StatusNotFound {
		t.Fatalf("before Add to Plex the tuner answered %d", code)
	}

	press := func() string {
		w := httptest.NewRecorder()
		s.handleTVPlex(w, httptest.NewRequest(http.MethodPost, "/api/tv/plex", nil).WithContext(context.Background()))
		if w.Code != http.StatusOK {
			t.Fatalf("Add to Plex: %d %s", w.Code, w.Body)
		}
		return w.Body.String()
	}
	press()
	key := s.tvFeedKey()
	want := []string{
		"tuner http://192.168.1.20:8264",
		"dvr device://tv.plex.grabbers.hdhomerun/ABCD1234 lineup://tv.plex.providers.epg.xmltv/http://192.168.1.20:8264/api/tvfeed/" + key + "/plex.xml#Usherr TV",
		"map 1,2 1/k2",
		"guide 7",
	}
	if strings.Join(f.calls, "\n") != strings.Join(want, "\n") {
		t.Fatalf("calls:\n%s\nwant:\n%s", strings.Join(f.calls, "\n"), strings.Join(want, "\n"))
	}
	if code := ask(); code != http.StatusOK {
		t.Fatalf("after Add to Plex the tuner answered %d", code)
	}

	f.calls = nil
	if body := press(); !strings.Contains(body, "already") || strings.Join(f.calls, ",") != "map 1,2 1/k2,guide 7" {
		t.Fatalf("pressed again: %v %s", f.calls, body)
	}

	// A DVR of Usherr's tuner made with an earlier guide: made again with
	// this one, on the same tuner (no second tuner).
	f.dvrs[0]["lineup"] = "lineup://tv.plex.providers.epg.xmltv/http://192.168.1.20:8264/api/tvfeed/" + key + "/xmltv.xml#Usherr TV"
	f.calls = nil
	press()
	if got := strings.Join(f.calls, ","); got != "delete 7,dvr device://tv.plex.grabbers.hdhomerun/ABCD1234 "+want[1][len("dvr device://tv.plex.grabbers.hdhomerun/ABCD1234 "):]+",map 1,2 1/k2,guide 7" {
		t.Fatalf("an earlier guide: %s", got)
	}
}
