package api

// Usherr: Service checks (Settings > System > Service checks). Usherr asks
// the addresses an administrator listed (Jellyfin, a download client,
// Prowlarr, anything with a web address) every so often, shows how each is
// doing, and sends a "down" and a "back" message through the notification
// targets that chose the "Service down" and "Service back" events. It does
// the job of a separate watchdog container (internal/servicecheck).

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/rdborg/mediarium/internal/mediaservers"
	"github.com/rdborg/mediarium/internal/notify"
	"github.com/rdborg/mediarium/internal/servicecheck"
	"github.com/rdborg/mediarium/internal/settings"
)

// serviceChecksEvery is how often the job looks for services that are due;
// each service has its own interval (30 seconds at the least).
const serviceChecksEvery = 15 * time.Second

// serviceCheckRecent is how many of the newest results the page shows as a strip.
const serviceCheckRecent = 40

// serviceWatch holds the runner of the checks, made on first use.
type serviceWatch struct {
	once   sync.Once
	runner *servicecheck.Runner
}

// serviceChecks is the runner of this server's checks.
func (s *Server) serviceChecks() *servicecheck.Runner {
	s.serviceWatch.once.Do(func() {
		s.serviceWatch.runner = &servicecheck.Runner{Repo: servicecheck.NewRepo(s.db), Notify: serviceNotifier{s}}
	})
	return s.serviceWatch.runner
}

// serviceNotifier turns a service going down or coming back into a message.
type serviceNotifier struct{ s *Server }

func (n serviceNotifier) Down(c servicecheck.Check, at time.Time) {
	n.s.sendNotification(notify.ComposeServiceDown(c.Name, c.Fails, c.FailSince, c.LastError, n.s.publicLinks(), at))
}

func (n serviceNotifier) Back(c servicecheck.Check, outage time.Duration, at time.Time) {
	n.s.sendNotification(notify.ComposeServiceUp(c.Name, outage, n.s.publicLinks(), at))
}

// serviceChecksJob checks the services that are due (run every 15 seconds).
func (s *Server) serviceChecksJob(ctx context.Context) {
	s.serviceChecks().RunDue(ctx)
}

func (s *Server) registerServiceCheckRoutes(admin routeGroup) {
	admin.HandleFunc("GET /api/service-checks", s.handleListServiceChecks)
	admin.HandleFunc("GET /api/service-checks/suggestions", s.handleServiceCheckSuggestions)
	admin.HandleFunc("POST /api/service-checks", s.handleSaveServiceCheck)
	admin.HandleFunc("POST /api/service-checks/try", s.handleTryServiceCheck)
	admin.HandleFunc("PUT /api/service-checks/{id}", s.handleSaveServiceCheck)
	admin.HandleFunc("DELETE /api/service-checks/{id}", s.handleDeleteServiceCheck)
	admin.HandleFunc("POST /api/service-checks/{id}/check", s.handleCheckServiceNow)
}

type serviceCheckBody struct {
	Name        string `json:"name"`
	URL         string `json:"url"`
	Expect      string `json:"expect"`
	Keyword     string `json:"keyword"`
	IntervalSec int    `json:"intervalSec"`
	FailAfter   int    `json:"failAfter"`
	Enabled     *bool  `json:"enabled,omitempty"`
}

func (b serviceCheckBody) check() servicecheck.Check {
	return servicecheck.Check{
		Name: b.Name, URL: b.URL, Expect: b.Expect, Keyword: b.Keyword,
		Interval: time.Duration(b.IntervalSec) * time.Second, FailAfter: b.FailAfter,
		Enabled: b.Enabled == nil || *b.Enabled,
	}
}

type serviceUptimeJSON struct {
	Checks  int     `json:"checks"`
	Percent float64 `json:"percent"`
	AvgMs   int     `json:"avgMs"`
}

type serviceResultJSON struct {
	At    time.Time `json:"at"`
	OK    bool      `json:"ok"`
	Ms    int       `json:"ms"`
	Code  int       `json:"code"`
	Error string    `json:"error,omitempty"`
}

type serviceChangeJSON struct {
	At     time.Time `json:"at"`
	State  string    `json:"state"`
	Detail string    `json:"detail"`
}

type serviceCheckJSON struct {
	ID          int64               `json:"id"`
	Name        string              `json:"name"`
	URL         string              `json:"url"`
	Expect      string              `json:"expect"`
	Keyword     string              `json:"keyword"`
	IntervalSec int                 `json:"intervalSec"`
	FailAfter   int                 `json:"failAfter"`
	Enabled     bool                `json:"enabled"`
	State       string              `json:"state"`
	Fails       int                 `json:"fails"`
	FailSince   *time.Time          `json:"failSince,omitempty"`
	Changed     *time.Time          `json:"changed,omitempty"`
	Checked     *time.Time          `json:"checked,omitempty"`
	LastMs      int                 `json:"lastMs"`
	LastCode    int                 `json:"lastCode"`
	LastError   string              `json:"lastError,omitempty"`
	Uptime24h   serviceUptimeJSON   `json:"uptime24h"`
	Uptime7d    serviceUptimeJSON   `json:"uptime7d"`
	Recent      []serviceResultJSON `json:"recent"`
	Changes     []serviceChangeJSON `json:"changes"`
}

func timeOrNil(t time.Time) *time.Time {
	if t.IsZero() {
		return nil
	}
	return &t
}

func (s *Server) serviceCheckJSON(ctx context.Context, c servicecheck.Check, now time.Time) serviceCheckJSON {
	repo := s.serviceChecks().Repo
	out := serviceCheckJSON{
		ID: c.ID, Name: c.Name, URL: c.URL, Expect: c.Expect, Keyword: c.Keyword,
		IntervalSec: int(c.Interval / time.Second), FailAfter: c.FailAfter, Enabled: c.Enabled,
		State: c.State, Fails: c.Fails, FailSince: timeOrNil(c.FailSince), Changed: timeOrNil(c.Changed), Checked: timeOrNil(c.Checked),
		LastMs: c.LastMs, LastCode: c.LastCode, LastError: c.LastError,
		Recent: []serviceResultJSON{}, Changes: []serviceChangeJSON{},
	}
	if u, err := repo.UptimeSince(ctx, c.ID, now.Add(-24*time.Hour)); err == nil {
		out.Uptime24h = serviceUptimeJSON(u)
	}
	if u, err := repo.UptimeSince(ctx, c.ID, now.Add(-7*24*time.Hour)); err == nil {
		out.Uptime7d = serviceUptimeJSON(u)
	}
	if list, err := repo.Recent(ctx, c.ID, serviceCheckRecent); err == nil {
		for _, r := range list {
			out.Recent = append(out.Recent, serviceResultJSON(r))
		}
	}
	if list, err := repo.Changes(ctx, c.ID, 10); err == nil {
		for _, x := range list {
			out.Changes = append(out.Changes, serviceChangeJSON(x))
		}
	}
	return out
}

// serviceAlertTargets is how many enabled notification targets hear about a
// service going down.
func (s *Server) serviceAlertTargets() int {
	targets, err := s.NotifyRepo.List()
	if err != nil {
		return 0
	}
	n := 0
	for _, t := range targets {
		if t.Enabled && t.Subscribed(notify.EventServiceDown) {
			n++
		}
	}
	return n
}

// GET /api/service-checks: every service with its state and history, and
// how many notification targets hear about outages.
func (s *Server) handleListServiceChecks(w http.ResponseWriter, r *http.Request) {
	list, err := s.serviceChecks().Repo.List(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "The service checks could not be read. Try again.")
		return
	}
	now := time.Now()
	out := make([]serviceCheckJSON, 0, len(list))
	for _, c := range list {
		out = append(out, s.serviceCheckJSON(r.Context(), c, now))
	}
	writeJSON(w, http.StatusOK, map[string]any{"checks": out, "alertTargets": s.serviceAlertTargets()})
}

// POST /api/service-checks adds a service; PUT /api/service-checks/{id}
// changes one.
func (s *Server) handleSaveServiceCheck(w http.ResponseWriter, r *http.Request) {
	var body serviceCheckBody
	if err := decodeJSON(r, &body); err != nil {
		writeError(w, http.StatusBadRequest, "Couldn't read that request. Reload the page and try again.")
		return
	}
	c, err := servicecheck.Normalize(body.check())
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	repo := s.serviceChecks().Repo
	ctx := r.Context()
	if idText := r.PathValue("id"); idText != "" {
		id, perr := strconv.ParseInt(idText, 10, 64)
		if perr != nil {
			writeError(w, http.StatusNotFound, "That service check doesn't exist.")
			return
		}
		c.ID = id
		c, err = repo.Update(ctx, c)
	} else {
		if n, cerr := repo.Count(ctx); cerr == nil && n >= servicecheck.MaxChecks {
			writeError(w, http.StatusBadRequest, "That is a lot of services: the most is 50.")
			return
		}
		c, err = repo.Create(ctx, c, time.Now())
	}
	switch {
	case errors.Is(err, servicecheck.ErrNotFound):
		writeError(w, http.StatusNotFound, "That service check doesn't exist.")
		return
	case err != nil:
		writeError(w, http.StatusInternalServerError, "The service check could not be saved. Try again.")
		return
	}
	status := http.StatusOK
	if r.Method == http.MethodPost {
		status = http.StatusCreated
	}
	writeJSON(w, status, s.serviceCheckJSON(ctx, c, time.Now()))
}

// DELETE /api/service-checks/{id} removes a service and its history.
func (s *Server) handleDeleteServiceCheck(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err == nil {
		err = s.serviceChecks().Repo.Delete(r.Context(), id)
	}
	switch {
	case errors.Is(err, servicecheck.ErrNotFound), err != nil && id == 0:
		writeError(w, http.StatusNotFound, "That service check doesn't exist.")
	case err != nil:
		writeError(w, http.StatusInternalServerError, "The service check could not be removed. Try again.")
	default:
		w.WriteHeader(http.StatusNoContent)
	}
}

// POST /api/service-checks/{id}/check checks one service now. The result
// counts like any other check.
func (s *Server) handleCheckServiceNow(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusNotFound, "That service check doesn't exist.")
		return
	}
	c, err := s.serviceChecks().CheckNow(r.Context(), id)
	switch {
	case errors.Is(err, servicecheck.ErrNotFound):
		writeError(w, http.StatusNotFound, "That service check doesn't exist.")
		return
	case err != nil:
		writeError(w, http.StatusInternalServerError, "The check could not be saved. Try again.")
		return
	}
	writeJSON(w, http.StatusOK, s.serviceCheckJSON(r.Context(), c, time.Now()))
}

// POST /api/service-checks/try asks an address once, before it is saved.
func (s *Server) handleTryServiceCheck(w http.ResponseWriter, r *http.Request) {
	var body serviceCheckBody
	if err := decodeJSON(r, &body); err != nil {
		writeError(w, http.StatusBadRequest, "Couldn't read that request. Reload the page and try again.")
		return
	}
	c := body.check()
	if strings.TrimSpace(c.Name) == "" {
		c.Name = "Try"
	}
	c, err := servicecheck.Normalize(c)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, serviceResultJSON(s.serviceChecks().Try(r.Context(), c)))
}

type serviceSuggestion struct {
	Name   string `json:"name"`
	URL    string `json:"url"`
	Expect string `json:"expect"`
	From   string `json:"from"` // where Usherr knows it from, in plain words
}

// GET /api/service-checks/suggestions: the services Usherr already knows
// about (media servers, download clients, Prowlarr, FlareSolverr, add-ons),
// with the address that tells best whether each is running. Ones already on
// the list are left out.
func (s *Server) handleServiceCheckSuggestions(w http.ResponseWriter, r *http.Request) {
	have := map[string]bool{}
	if list, err := s.serviceChecks().Repo.List(r.Context()); err == nil {
		for _, c := range list {
			have[strings.ToLower(strings.TrimRight(c.URL, "/"))] = true
		}
	}
	out := []serviceSuggestion{}
	add := func(x serviceSuggestion) {
		key := strings.ToLower(strings.TrimRight(x.URL, "/"))
		if x.URL == "" || have[key] {
			return
		}
		have[key] = true
		out = append(out, x)
	}
	if s.MediaServers != nil {
		if list, err := s.MediaServers.List(); err == nil {
			for _, m := range list {
				base := strings.TrimRight(strings.TrimSpace(m.BaseURL), "/")
				if base == "" {
					continue
				}
				switch m.Kind {
				case mediaservers.KindJellyfin:
					add(serviceSuggestion{m.Name, base + "/health", servicecheck.Expect200, "Media servers"})
				case mediaservers.KindPlex:
					add(serviceSuggestion{m.Name, base + "/identity", servicecheck.Expect200, "Media servers"})
				case mediaservers.KindEmby:
					add(serviceSuggestion{m.Name, base + "/System/Ping", servicecheck.ExpectOK, "Media servers"})
				}
			}
		}
	}
	get := func(key string) string {
		v, _ := s.Settings.Get(key)
		return strings.TrimRight(strings.TrimSpace(v), "/")
	}
	if kind, addr := get(settings.KeyDownloadClientKind), get(settings.KeyDownloadClientURL); kind != "" && addr != "" {
		add(serviceSuggestion{clientName(kind), addr + "/", servicecheck.ExpectOK, "Torrents"})
	}
	if kind, addr := get(settings.KeyUsenetClientKind), get(settings.KeyUsenetClientURL); kind != "" && addr != "" {
		// NZBGet asks for a login on its front page: any answer means it runs.
		expect := servicecheck.ExpectOK
		if kind == "nzbget" {
			expect = servicecheck.ExpectAny
		}
		add(serviceSuggestion{clientName(kind), addr + "/", expect, "Usenet"})
	}
	if addr := get(settings.KeyFlareSolverrURL); addr != "" {
		add(serviceSuggestion{"FlareSolverr", addr + "/", servicecheck.Expect200, "Indexers & Search"})
	}
	if s.IndexerRepo != nil {
		if list, err := s.IndexerRepo.List(); err == nil {
			for _, ix := range list {
				if origin, ok := prowlarrOrigin(ix.BaseURL); ok {
					add(serviceSuggestion{"Prowlarr", origin + "/ping", servicecheck.Expect200, "Indexers & Search"})
				}
			}
		}
	}
	for _, a := range s.appLinks() {
		if a.URL != "" {
			add(serviceSuggestion{a.Name, strings.TrimSpace(a.URL), servicecheck.ExpectOK, "Add-ons"})
		}
	}
	writeJSON(w, http.StatusOK, out)
}

// clientName is how a download client kind is written.
func clientName(kind string) string {
	switch kind {
	case "qbittorrent":
		return "qBittorrent"
	case "rdtclient":
		return "RDT-Client"
	case "decypharr":
		return "Decypharr"
	case "sabnzbd":
		return "SABnzbd"
	case "nzbget":
		return "NZBGet"
	}
	return kind
}

// prowlarrOrigin is Prowlarr's own address when an indexer goes through it
// (its address names Prowlarr or uses its usual port, 9696).
func prowlarrOrigin(raw string) (string, bool) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") {
		return "", false
	}
	origin := u.Scheme + "://" + u.Host
	// Prowlarr behind a path (http://host/prowlarr/1/api) keeps the path.
	if i := strings.Index(strings.ToLower(u.Path), "/prowlarr/"); i >= 0 {
		return origin + u.Path[:i+len("/prowlarr")], true
	}
	if !strings.Contains(strings.ToLower(u.Host), "prowlarr") && u.Port() != "9696" {
		return "", false
	}
	return origin, true
}
