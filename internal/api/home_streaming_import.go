package api

// Usherr: a one-time import from StreamNZB, for someone who has it and wants
// to use Usherr's own streaming engine instead (or next to it). StreamNZB's
// config.json holds its indexers, its Usenet providers and its filter
// profiles. The person is asked first: the preview lists what is there and
// what Usherr already has (names and addresses only, never logins), and only
// what is ticked is copied. Logins are copied into Usherr's own encrypted
// settings and never sent back to the page.

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/rdborg/mediarium/internal/download"
	"github.com/rdborg/mediarium/internal/indexers"
)

const maxSNZBConfig = 2 << 20

type snzbConfig struct {
	Indexers []struct {
		Name    string `json:"name"`
		URL     string `json:"url"`
		APIKey  string `json:"api_key"`
		APIPath string `json:"api_path"`
		Type    string `json:"type"`
		Enabled *bool  `json:"enabled"`
	} `json:"indexers"`
	Providers []struct {
		Name        string `json:"name"`
		Host        string `json:"host"`
		Port        int    `json:"port"`
		Username    string `json:"username"`
		Password    string `json:"password"`
		Connections int    `json:"connections"`
		UseSSL      bool   `json:"use_ssl"`
		Priority    int    `json:"priority"`
		Enabled     *bool  `json:"enabled"`
		Backup      bool   `json:"backup"`
	} `json:"providers"`
	FilterProfiles []struct {
		Name   string `json:"name"`
		Preset string `json:"preset"`
		Rules  []struct {
			When    string `json:"when"`
			Action  string `json:"action"`
			Enabled *bool  `json:"enabled"`
		} `json:"rules"`
	} `json:"filter_profiles"`
}

// loadSNZBConfig reads StreamNZB's settings from what the page sent, or from
// the config.json mapped into Usherr (the way the admin token is found).
func loadSNZBConfig(sent string) (snzbConfig, error) {
	var raw []byte
	if strings.TrimSpace(sent) != "" {
		if len(sent) > maxSNZBConfig {
			return snzbConfig{}, errors.New("That file is too big to be StreamNZB's config.json.")
		}
		raw = []byte(sent)
	} else {
		b, err := os.ReadFile(streamNZBConfig)
		if err != nil {
			return snzbConfig{}, errors.New("StreamNZB's settings weren't found. Choose its config.json, or map it into Usherr at /streamnzb/config.json (read only).")
		}
		raw = b
	}
	var c snzbConfig
	if err := json.Unmarshal(raw, &c); err != nil {
		return snzbConfig{}, errors.New("That isn't StreamNZB's config.json: it couldn't be read.")
	}
	if len(c.Indexers) == 0 && len(c.Providers) == 0 && len(c.FilterProfiles) == 0 {
		return snzbConfig{}, errors.New("That file has no indexers, providers or filter profiles to import.")
	}
	return c, nil
}

// snzbIndexerURL is the address an indexer is added to Usherr with: an
// aggregator (Prowlarr, NZBHydra) has the indexer's own path after its
// address, like http://host:9696/1/api.
func snzbIndexerURL(base, apiPath string) string {
	base = strings.TrimRight(strings.TrimSpace(base), "/")
	ap := strings.Trim(strings.TrimSpace(apiPath), "/")
	if ap == "" || ap == "api" {
		return base
	}
	return base + "/" + ap
}

var viaProwlarr = regexp.MustCompile(`(?i)^prowlarr[- ]|[ -]*\(via prowlarr\)$|[ -]*\(via nzbhydra2?\)$`)

// indexerKey is how two indexers are told to be the same: by name (without
// "Prowlarr-" and "(via Prowlarr)"), or by the path an aggregator gives it.
func indexerKey(name string) string {
	return strings.ToLower(strings.TrimSpace(viaProwlarr.ReplaceAllString(strings.TrimSpace(name), "")))
}

func pathKey(addr string) string {
	u, err := url.Parse(addr)
	if err != nil || u.Host == "" {
		return ""
	}
	p := strings.Trim(u.Path, "/")
	if p == "" || p == "api" {
		return ""
	}
	return strings.ToLower(u.Port() + "/" + p)
}

// ---- what the filter profiles say ----

var (
	sizeAlone = regexp.MustCompile(`^\s*sizeGB\s*>\s*(\d+(?:\.\d+)?)\s*$`)
	sizeNot4K = regexp.MustCompile(`^\s*sizeGB\s*>\s*(\d+(?:\.\d+)?)\s+and\s+resolution\s*!=\s*"2160p"\s*$`)
)

func presetQuality(p string) string {
	switch strings.ToLower(strings.TrimSpace(p)) {
	case "4k", "2160p", "uhd":
		return "4K"
	case "1080p":
		return "1080p"
	case "720p":
		return "720p"
	}
	return ""
}

// profilesFromSNZB makes Usherr's device qualities from StreamNZB's filter
// profiles: for each quality, the profile built for it (the first, when
// several are) and the smallest size its rules turn away.
func profilesFromSNZB(c snzbConfig) []streamProfile {
	var out []streamProfile
	seen := map[string]bool{}
	for _, fp := range c.FilterProfiles {
		q := presetQuality(fp.Preset)
		if q == "" || seen[q] {
			continue
		}
		seen[q] = true
		maxGB := 0.0
		for _, r := range fp.Rules {
			if r.Enabled != nil && !*r.Enabled || !strings.EqualFold(r.Action, "reject") {
				continue
			}
			if m := sizeAlone.FindStringSubmatch(r.When); m != nil {
				if n, _ := strconv.ParseFloat(m[1], 64); n > 0 && (maxGB == 0 || n < maxGB) {
					maxGB = n
				}
			} else if m := sizeNot4K.FindStringSubmatch(r.When); m != nil && q != "4K" {
				if n, _ := strconv.ParseFloat(m[1], 64); n > 0 && (maxGB == 0 || n < maxGB) {
					maxGB = n
				}
			}
		}
		if maxGB == 0 {
			continue // nothing it says about size: the defaults stay
		}
		out = append(out, streamProfile{Quality: q, MaxSizeGB: maxGB, Exclude: "av1"})
	}
	return out
}

// ---- the preview and the import ----

type importItem struct {
	Name     string `json:"name"`
	Detail   string `json:"detail"`
	Existing bool   `json:"existing"` // Usherr has it already
}

type importPreview struct {
	Providers []importItem `json:"providers"`
	Indexers  []importItem `json:"indexers"`
	Profiles  []importItem `json:"profiles"`
}

func (s *Server) snzbPreview(c snzbConfig) importPreview {
	var out importPreview
	have, _ := s.ClientRepo.ListAll()
	for _, p := range c.Providers {
		it := importItem{Name: p.Name, Detail: fmt.Sprintf("%s:%d · %d connections", p.Host, p.Port, p.Connections)}
		if it.Name == "" {
			it.Name = p.Host
		}
		for _, h := range have {
			if strings.EqualFold(h.Config.Host, p.Host) && h.Config.Username == p.Username {
				it.Existing = true
			}
		}
		out.Providers = append(out.Providers, it)
	}
	haveIdx, _ := s.IndexerRepo.List()
	for _, ix := range c.Indexers {
		addr := snzbIndexerURL(ix.URL, ix.APIPath)
		it := importItem{Name: ix.Name, Detail: addr}
		for _, h := range haveIdx {
			if indexerKey(h.Name) == indexerKey(ix.Name) || (pathKey(addr) != "" && pathKey(addr) == pathKey(h.BaseURL)) {
				it.Existing = true
			}
		}
		out.Indexers = append(out.Indexers, it)
	}
	current := s.streamProfiles()
	for _, p := range profilesFromSNZB(c) {
		it := importItem{Name: p.Quality, Detail: fmt.Sprintf("up to %g GB", p.MaxSizeGB)}
		for _, cur := range current {
			if cur.Quality == p.Quality && cur.MaxSizeGB == p.MaxSizeGB {
				it.Existing = true
			}
		}
		out.Profiles = append(out.Profiles, it)
	}
	return out
}

type snzbImportRequest struct {
	Config    string   `json:"config"` // StreamNZB's config.json, when it isn't mapped in
	Providers []string `json:"providers"`
	Indexers  []string `json:"indexers"`
	Profiles  bool     `json:"profiles"`
}

// POST /api/home/streaming/import/preview {config?}: what StreamNZB has and
// what Usherr has of it already. Nothing is changed and no login is sent.
func (s *Server) handleSNZBImportPreview(w http.ResponseWriter, r *http.Request) {
	var req snzbImportRequest
	if err := decodeJSON(r, &req); err != nil && !errors.Is(err, io.EOF) {
		writeError(w, http.StatusBadRequest, "Couldn't read that request. Reload the page and try again.")
		return
	}
	c, err := loadSNZBConfig(req.Config)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, s.snzbPreview(c))
}

// POST /api/home/streaming/import {config?, providers, indexers, profiles}:
// copies what was ticked.
func (s *Server) handleSNZBImport(w http.ResponseWriter, r *http.Request) {
	var req snzbImportRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "Couldn't read that request. Reload the page and try again.")
		return
	}
	c, err := loadSNZBConfig(req.Config)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	pre := s.snzbPreview(c)
	wantP, wantI := map[string]bool{}, map[string]bool{}
	for _, n := range req.Providers {
		wantP[n] = true
	}
	for _, n := range req.Indexers {
		wantI[n] = true
	}
	done, skipped := []string{}, []string{}

	// Providers: the primary ones first, then the backups, in StreamNZB's order.
	order := make([]int, len(c.Providers))
	for i := range order {
		order[i] = i
	}
	sort.SliceStable(order, func(a, b int) bool {
		pa, pb := c.Providers[order[a]], c.Providers[order[b]]
		if pa.Backup != pb.Backup {
			return !pa.Backup
		}
		return pa.Priority < pb.Priority
	})
	have, _ := s.ClientRepo.ListAll()
	rank := len(have)
	for _, i := range order {
		p := c.Providers[i]
		name := p.Name
		if name == "" {
			name = p.Host
		}
		if !wantP[name] {
			continue
		}
		if pre.Providers[i].Existing {
			skipped = append(skipped, name+" (already in Usherr)")
			continue
		}
		payload := usenetServerPayload{Name: name, Host: p.Host, Port: p.Port, UseSSL: p.UseSSL, Username: p.Username, Password: p.Password, Connections: p.Connections, Priority: rank, Enabled: p.Enabled == nil || *p.Enabled}
		sc, msg := payload.validate(nil)
		if msg != "" {
			skipped = append(skipped, name+": "+msg)
			continue
		}
		created, err := s.ClientRepo.Create(sc)
		if err != nil {
			skipped = append(skipped, name+": "+err.Error())
			continue
		}
		if !sc.Enabled {
			created.Enabled = false
			_ = s.ClientRepo.Update(created)
		}
		download.ConfigureServer(created.Config)
		rank++
		done = append(done, name)
	}

	// Indexers.
	for i, ix := range c.Indexers {
		if !wantI[ix.Name] {
			continue
		}
		if pre.Indexers[i].Existing {
			skipped = append(skipped, ix.Name+" (already in Usherr)")
			continue
		}
		addr := snzbIndexerURL(ix.URL, ix.APIPath)
		name := strings.TrimSpace(ix.Name)
		key := strings.TrimSpace(ix.APIKey)
		if msg := firstProblem(checkIndexerName(name), checkIndexerAddress(addr), checkAPIKey(key)); msg != "" {
			skipped = append(skipped, name+": "+msg)
			continue
		}
		if _, err := s.IndexerRepo.Create(indexers.Instance{Name: name, BaseURL: addr, APIKey: key, Protocol: indexers.ProtocolUsenet, Enabled: ix.Enabled == nil || *ix.Enabled}, nil); err != nil {
			skipped = append(skipped, name+": "+err.Error())
			continue
		}
		done = append(done, name)
	}

	// Device qualities.
	if req.Profiles {
		fresh := profilesFromSNZB(c)
		if len(fresh) > 0 {
			merged := s.streamProfiles()
			for i, cur := range merged {
				for _, p := range fresh {
					if p.Quality == cur.Quality {
						merged[i].MaxSizeGB = p.MaxSizeGB
					}
				}
			}
			b, _ := json.Marshal(merged)
			if err := s.Settings.Set(keyStreamProfiles, string(b), false); err != nil {
				skipped = append(skipped, "device qualities: "+err.Error())
			} else {
				done = append(done, "device qualities")
			}
		}
	}
	slog.Info("streaming: imported from StreamNZB", "done", len(done), "skipped", len(skipped))
	writeJSON(w, http.StatusOK, map[string]any{"imported": done, "skipped": skipped})
}
