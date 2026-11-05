package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/rdborg/mediarium/internal/auth"
	"github.com/rdborg/mediarium/internal/crypto"
	"github.com/rdborg/mediarium/internal/library"
	"github.com/rdborg/mediarium/internal/mediaservers"
	"github.com/rdborg/mediarium/internal/settings"
	"github.com/rdborg/mediarium/internal/watchlog"
)

func watchTestServer(t *testing.T) *Server {
	t.Helper()
	dir := t.TempDir()
	db, err := OpenTestDB(filepath.Join(dir, "app.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	box, err := crypto.LoadOrCreateKey(filepath.Join(dir, "secret.key"))
	if err != nil {
		t.Fatal(err)
	}
	return &Server{db: db, Settings: settings.New(db, box), MediaServers: mediaservers.NewRepo(db, box), Auth: auth.New(db),
		MovieRepo: library.NewRepo(db), mediaClient: mediaservers.NewClient("test")}
}

// fakeSessions is a media server whose answer the test changes between checks.
type fakeSessions struct {
	mu   sync.Mutex
	body string
}

func (f *fakeSessions) set(body string) {
	f.mu.Lock()
	f.body = body
	f.mu.Unlock()
}

func (f *fakeSessions) server(t *testing.T, path string) *httptest.Server {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != path {
			http.NotFound(w, r)
			return
		}
		f.mu.Lock()
		defer f.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(f.body))
	}))
	t.Cleanup(srv.Close)
	return srv
}

func jellyfinSession(posSec int, paused bool) string {
	b, _ := json.Marshal([]map[string]any{
		{"Id": "idle", "UserName": "Nobody"}, // a signed-in app that plays nothing
		{
			"Id": "s1", "UserId": "u1", "UserName": "Alice", "Client": "Jellyfin Web", "DeviceName": "Firefox", "RemoteEndPoint": "192.168.1.20",
			"NowPlayingItem": map[string]any{
				"Id": "i1", "Name": "The Matrix", "Type": "Movie", "MediaType": "Video", "ProductionYear": 1999, "RunTimeTicks": int64(6000) * 10_000_000,
				"ProviderIds": map[string]string{"Tmdb": "603"},
				"MediaStreams": []map[string]any{
					{"Type": "Video", "Codec": "HEVC", "Width": 3840, "Height": 2160},
					{"Type": "Audio", "Codec": "truehd", "Channels": 8, "Index": 1},
				},
			},
			"PlayState": map[string]any{"PositionTicks": int64(posSec) * 10_000_000, "IsPaused": paused, "PlayMethod": "Transcode", "AudioStreamIndex": 1},
			"TranscodingInfo": map[string]any{
				"VideoCodec": "h264", "AudioCodec": "aac", "Width": 1920, "Height": 1080, "Bitrate": 8_000_000,
				"IsVideoDirect": false, "IsAudioDirect": false, "TranscodeReasons": []string{"VideoCodecNotSupported", "AudioCodecNotSupported"},
			},
		},
	})
	return string(b)
}

// A Jellyfin playback is recorded from start to end: matched to the library
// and to the account linked to the Jellyfin user, how it is transcoded and
// why, the time watched without the pauses, and finished once past 90%.
func TestWatchHistoryJellyfinPlayback(t *testing.T) {
	s := watchTestServer(t)
	ctx := context.Background()
	mv, err := s.MovieRepo.Add(library.Movie{TMDBID: 603, Title: "The Matrix", Monitored: true})
	if err != nil {
		t.Fatal(err)
	}
	alice, err := s.Auth.CreateUser("alice", "a-long-password-1")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Auth.LinkIdentity(alice.ID, auth.SourceJellyfin, "u1"); err != nil {
		t.Fatal(err)
	}
	fake := &fakeSessions{}
	jf := fake.server(t, "/Sessions")
	if _, err := s.MediaServers.Create(mediaservers.Server{Name: "Jellyfin", Kind: mediaservers.KindJellyfin, BaseURL: jf.URL, Token: "k", Enabled: true}); err != nil {
		t.Fatal(err)
	}
	t0 := time.Date(2026, 11, 4, 20, 0, 0, 0, time.UTC)
	now := t0
	s.watchLog()
	s.watch.now = func() time.Time { return now }
	step := func(at time.Duration, body string) {
		now = t0.Add(at)
		fake.set(body)
		s.watchSessionsJob(ctx)
	}
	step(0, jellyfinSession(0, false))
	step(30*time.Second, jellyfinSession(30, false))  // +30 s
	step(60*time.Second, jellyfinSession(50, true))   // paused at 50: +20 s
	step(90*time.Second, jellyfinSession(50, true))   // still paused: nothing
	step(120*time.Second, jellyfinSession(60, false)) // playing again: +10 s
	if _, tr := s.watchLog(); len(tr.Playing()) != 1 {
		t.Fatalf("playing now: %+v", tr.Playing())
	}
	step(150*time.Second, jellyfinSession(5500, false)) // skipped ahead: no more than the 30 s that passed
	step(180*time.Second, `[]`)                         // gone: it ended

	repo, tr := s.watchLog()
	if len(tr.Playing()) != 0 {
		t.Fatalf("still playing after it was gone: %+v", tr.Playing())
	}
	list, total, err := repo.List(ctx, watchlog.Filter{})
	if err != nil || total != 1 {
		t.Fatalf("history: %d rows, %v", total, err)
	}
	got := list[0]
	if got.WatchedSec != 90 {
		t.Errorf("watched %d s, want 90 (pauses left out)", got.WatchedSec)
	}
	if !got.Completed || got.Progress < 0.9 {
		t.Errorf("not finished: progress %.2f", got.Progress)
	}
	if !got.StartedAt.Equal(t0) || !got.EndedAt.Equal(t0.Add(150*time.Second)) {
		t.Errorf("times: %v to %v", got.StartedAt, got.EndedAt)
	}
	if got.UserID != alice.ID || got.UserName != "Alice" || got.LibraryID != mv.ID || got.TMDBID != 603 || got.Kind != "movie" {
		t.Errorf("matching: %+v", got)
	}
	if got.PlayMethod != "Transcode" || strings.Join(got.TranscodeReasons, ",") != "Video codec not supported,Audio codec not supported" {
		t.Errorf("transcode: %q %q", got.PlayMethod, got.TranscodeReasons)
	}
	if got.VideoIn != "hevc" || got.VideoOut != "h264" || got.AudioIn != "truehd" || got.AudioOut != "aac" || got.ResolutionIn != "4K" || got.ResolutionOut != "1080p" {
		t.Errorf("streams: %+v", got)
	}
	if got.Client != "Jellyfin Web" || got.Device != "Firefox" || got.BandwidthKbps != 8000 || got.Location != "lan" || got.Address != "" {
		t.Errorf("player: %+v", got)
	}
}

// A Plex playback: the user, the app, away from home, and the reason worked
// out from what Plex converts. The address is kept only when switched on.
func TestWatchHistoryPlexPlayback(t *testing.T) {
	s := watchTestServer(t)
	ctx := context.Background()
	if err := s.Settings.Set(settings.KeyWatchKeepAddresses, "1", false); err != nil {
		t.Fatal(err)
	}
	fake := &fakeSessions{}
	px := fake.server(t, "/status/sessions")
	if _, err := s.MediaServers.Create(mediaservers.Server{Name: "Plex", Kind: mediaservers.KindPlex, BaseURL: px.URL, Token: "tok", Enabled: true}); err != nil {
		t.Fatal(err)
	}
	session := func(offsetMS int) string {
		b, _ := json.Marshal(map[string]any{"MediaContainer": map[string]any{"Metadata": []map[string]any{{
			"type": "movie", "title": "Arrival", "year": 2016, "ratingKey": "100", "sessionKey": "7", "duration": 6_960_000, "viewOffset": offsetMS,
			"Guid":             []map[string]any{{"id": "tmdb://329865"}},
			"User":             map[string]any{"id": 1, "title": "robin"},
			"Player":           map[string]any{"title": "Living Room TV", "product": "Plex for Android (TV)", "platform": "Android", "state": "playing", "address": "203.0.113.5", "local": false},
			"Session":          map[string]any{"id": "abc", "bandwidth": 12000, "location": "wan"},
			"TranscodeSession": map[string]any{"videoDecision": "copy", "audioDecision": "transcode", "audioCodec": "aac", "sourceAudioCodec": "truehd"},
			"Media":            []map[string]any{{"videoCodec": "hevc", "audioCodec": "truehd", "Part": []map[string]any{{"Stream": []map[string]any{{"streamType": 1, "codec": "hevc", "width": 1920, "height": 800}}}}}},
		}}}})
		return string(b)
	}
	t0 := time.Date(2026, 11, 4, 21, 0, 0, 0, time.UTC)
	now := t0
	s.watchLog()
	s.watch.now = func() time.Time { return now }
	fake.set(session(0))
	s.watchSessionsJob(ctx)
	now = t0.Add(30 * time.Second)
	fake.set(session(30_000))
	s.watchSessionsJob(ctx)
	now = t0.Add(60 * time.Second)
	fake.set(`{"MediaContainer":{"size":0}}`)
	s.watchSessionsJob(ctx)

	repo, _ := s.watchLog()
	list, _, err := repo.List(ctx, watchlog.Filter{})
	if err != nil || len(list) != 1 {
		t.Fatalf("history: %+v %v", list, err)
	}
	got := list[0]
	if got.UserName != "robin" || got.ServerUserID != "1" || got.Client != "Plex for Android (TV) (Android)" || got.Device != "Living Room TV" {
		t.Errorf("who: %+v", got)
	}
	if got.PlayMethod != "Transcode" || strings.Join(got.TranscodeReasons, ",") != "Audio converted" || got.AudioIn != "truehd" || got.AudioOut != "aac" || got.VideoOut != "hevc" {
		t.Errorf("how: %+v", got)
	}
	if got.Location != "remote" || got.Address != "203.0.113.5" || got.BandwidthKbps != 12000 || got.WatchedSec != 30 || got.TMDBID != 329865 {
		t.Errorf("where: %+v", got)
	}
	if got.EndedAt.IsZero() || got.Completed {
		t.Errorf("end: %+v", got)
	}
	if n, err := repo.ClearAddresses(ctx); err != nil || n != 1 {
		t.Fatalf("cleared %d, %v", n, err)
	}
}

func watchRequest(t *testing.T, s *Server, h http.HandlerFunc, u *auth.User, target string) map[string]any {
	t.Helper()
	r := httptest.NewRequest(http.MethodGet, target, nil)
	r = r.WithContext(auth.WithUser(r.Context(), u))
	w := httptest.NewRecorder()
	h(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("%s: %d %s", target, w.Code, w.Body.String())
	}
	var out map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	return out
}

// A member sees only their own playbacks and numbers until the administrator
// lets members see everyone's; an administrator always sees everyone's.
func TestWatchHistoryMemberSeesOnlyOwn(t *testing.T) {
	s := watchTestServer(t)
	ctx := context.Background()
	repo, _ := s.watchLog()
	now := time.Now()
	for _, row := range []watchlog.Session{
		{UserName: "Alice", UserID: 2, Kind: "movie", Title: "Her film", WatchedSec: 3600, Address: "10.0.0.5"},
		{UserName: "Bob", UserID: 3, Kind: "movie", Title: "His film", WatchedSec: 1800},
		{UserName: "Bob", UserID: 3, Kind: "episode", Series: "A show", Title: "Pilot", WatchedSec: 1800},
	} {
		row.StartedAt, row.EndedAt, row.LastSeen = now.Add(-time.Hour), now, now
		if err := repo.Insert(ctx, &row); err != nil {
			t.Fatal(err)
		}
	}
	alice := &auth.User{ID: 2, Username: "alice"}
	admin := &auth.User{ID: 1, Username: "admin", IsAdmin: true}

	h := watchRequest(t, s, s.handleWatchHistory, alice, "/api/watch/history")
	items := h["items"].([]any)
	if h["total"].(float64) != 1 || len(items) != 1 || items[0].(map[string]any)["title"] != "Her film" || h["everyone"] != false {
		t.Fatalf("a member's history: %v", h)
	}
	if _, ok := items[0].(map[string]any)["address"]; ok {
		t.Fatalf("a member was shown an address: %v", items[0])
	}
	st := watchRequest(t, s, s.handleWatchStats, alice, "/api/watch/stats?days=7")["stats"].(map[string]any)
	if st["plays"].(float64) != 1 || len(st["users"].([]any)) != 0 {
		t.Fatalf("a member's stats: %v", st)
	}
	// Asking for someone else by name changes nothing.
	if h := watchRequest(t, s, s.handleWatchHistory, alice, "/api/watch/history?user=Bob"); h["total"].(float64) != 1 {
		t.Fatalf("a member reached someone else's history: %v", h)
	}

	if h := watchRequest(t, s, s.handleWatchHistory, admin, "/api/watch/history"); h["total"].(float64) != 3 {
		t.Fatalf("the administrator's history: %v", h)
	}
	if h := watchRequest(t, s, s.handleWatchHistory, admin, "/api/watch/history?user=Bob&q=show"); h["total"].(float64) != 1 {
		t.Fatalf("filtered: %v", h)
	}
	if err := s.Settings.Set(settings.KeyWatchMembersSeeAll, "1", false); err != nil {
		t.Fatal(err)
	}
	if h := watchRequest(t, s, s.handleWatchHistory, alice, "/api/watch/history"); h["total"].(float64) != 3 || h["everyone"] != true {
		t.Fatalf("members see everyone's: %v", h)
	}
	st = watchRequest(t, s, s.handleWatchStats, alice, "/api/watch/stats?days=30")["stats"].(map[string]any)
	if st["plays"].(float64) != 3 || st["hours"].(float64) != 2 || len(st["users"].([]any)) != 2 {
		t.Fatalf("everyone's stats: %v", st)
	}
}

// Playbacks older than the retention setting go in the daily prune; 0 keeps
// them for ever.
func TestWatchHistoryRetentionPrune(t *testing.T) {
	s := watchTestServer(t)
	ctx := context.Background()
	repo, _ := s.watchLog()
	now := time.Date(2026, 11, 4, 12, 0, 0, 0, time.UTC)
	s.watch.now = func() time.Time { return now }
	add := func(daysAgo int) {
		at := now.AddDate(0, 0, -daysAgo)
		row := watchlog.Session{UserName: "Alice", Kind: "movie", Title: "Old", StartedAt: at, EndedAt: at.Add(time.Hour), LastSeen: at.Add(time.Hour)}
		if err := repo.Insert(ctx, &row); err != nil {
			t.Fatal(err)
		}
	}
	count := func() int {
		_, n, err := repo.List(ctx, watchlog.Filter{})
		if err != nil {
			t.Fatal(err)
		}
		return n
	}
	add(400)
	add(10)
	s.watchPruneJob(ctx) // 365 days by default
	if n := count(); n != 1 {
		t.Fatalf("%d left, want 1", n)
	}
	if err := s.Settings.Set(settings.KeyWatchRetentionDays, "0", false); err != nil {
		t.Fatal(err)
	}
	add(4000)
	s.watchPruneJob(ctx)
	if n := count(); n != 2 {
		t.Fatalf("%d left with keep-for-ever, want 2", n)
	}
	if err := s.Settings.Set(settings.KeyWatchRetentionDays, "5", false); err != nil {
		t.Fatal(err)
	}
	s.watchPruneJob(ctx)
	if n := count(); n != 0 {
		t.Fatalf("%d left with 5 days, want 0", n)
	}
}
