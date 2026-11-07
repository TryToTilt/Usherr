package api

// Usherr: where manga comes from. Official editions and fan scans are searched on
// your indexers. Nyaa is an indexer too, added here when its switch is turned on,
// so nobody needs the indexer page for it. The one added here is stored switched
// off: everything else in Usherr searches only indexers that are on, so films and
// TV never ask it, and Manga asks it by itself. A Nyaa you added yourself is used
// as it is, and is left alone.

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/rdborg/mediarium/internal/indexers"
	"github.com/rdborg/mediarium/internal/settings"
)

const (
	keyMangaNyaaIndexer = "manga.nyaa_indexer" // the id of the Nyaa that Manga added
	nyaaDefinitionID    = "nyaasi"             // in the shared list of sites
	nyaaEnglishCategory = "3_1"                // its "Literature - English-translated"
)

func isNyaaInstance(inst indexers.Instance) bool {
	return strings.Contains(strings.ToLower(inst.DefinitionID), "nyaa") || strings.Contains(strings.ToLower(inst.Name), "nyaa")
}

// mangaOwnNyaa is the id of the Nyaa that Manga added, or 0.
func (s *Server) mangaOwnNyaa() int64 {
	v, _ := s.Settings.Get(keyMangaNyaaIndexer)
	id, _ := strconv.ParseInt(v, 10, 64)
	return id
}

// mangaInstances are the indexers to search for manga: the usual ones, and apart
// from them Nyaa when it's allowed. Nyaa files everything under Books as a whole,
// which mangaCategories covers.
func (s *Server) mangaInstances(withNyaa bool) (usual, nyaa []indexers.Instance, err error) {
	list, err := s.searchableIndexers()
	if err != nil {
		return nil, nil, err
	}
	own := s.mangaOwnNyaa()
	for _, inst := range list {
		if !isNyaaInstance(inst) {
			usual = append(usual, inst)
			continue
		}
		if !withNyaa {
			continue
		}
		if inst.ID == own {
			inst.Enabled = true // stored off so only Manga asks it
		}
		nyaa = append(nyaa, inst)
	}
	return usual, nyaa, nil
}

// ensureMangaNyaa finds a Nyaa among the indexers, or adds one from the shared
// list of sites. Nothing asks Nyaa itself here: only the list is read.
func (s *Server) ensureMangaNyaa(ctx context.Context) error {
	list, err := s.IndexerRepo.List()
	if err != nil {
		return err
	}
	for _, inst := range list {
		if isNyaaInstance(inst) {
			return nil
		}
	}
	sum, err := s.Definitions.Summary(ctx, nyaaDefinitionID)
	switch {
	case errors.Is(err, indexers.ErrDefinitionNotFound):
		return errors.New("The list of sites Usherr uses doesn't have Nyaa right now, so it couldn't be added.")
	case err != nil:
		return errors.New("Usherr couldn't download its list of sites, which Nyaa comes from. Check this server can reach github.com, and try again.")
	case !sum.Supported:
		return fmt.Errorf("Nyaa's entry in the list of sites uses something Usherr can't run yet (%s).", sum.Problem)
	case len(sum.Links) == 0:
		return errors.New("Nyaa's entry in the list of sites has no address.")
	}
	// English translations only, when the entry still offers that.
	values, err := cardigannSettings(sum, map[string]any{"cat-id": nyaaEnglishCategory})
	if err != nil {
		values = map[string]string{}
	}
	created, err := s.IndexerRepo.Create(indexers.Instance{
		Name: "Nyaa", Kind: indexers.KindCardigann, DefinitionID: sum.ID, BaseURL: sum.Links[0], Protocol: indexers.ProtocolTorrent,
		Enabled: false, Settings: values, MinSeeders: 1,
	}, sum.IsSecret)
	if err != nil {
		return err
	}
	return s.Settings.Set(keyMangaNyaaIndexer, strconv.FormatInt(created.ID, 10), false)
}

// mangaNyaaState is what the Nyaa card says about this server.
type mangaNyaaState struct {
	Indexer          string `json:"indexer,omitempty"`  // the Nyaa in use, by its name
	Own              bool   `json:"own"`                // Manga added it
	TorrentsOn       bool   `json:"torrentsOn"`         // torrents have somewhere to go
	Client           string `json:"client"`             // builtin, debrid or external
	VPNRequired      bool   `json:"vpnRequired"`        // the kill switch is on
	VPNConnected     bool   `json:"vpnConnected"`       //
	IndexersThroughV bool   `json:"indexersThroughVpn"` // searches go through the VPN
	SeedRatio        string `json:"seedRatio,omitempty"`
	SeedHours        string `json:"seedHours,omitempty"`
}

func (s *Server) mangaNyaaNow() mangaNyaaState {
	st := mangaNyaaState{TorrentsOn: s.torrentReleasesOn(), Client: "builtin", VPNRequired: s.vpnRequiredForTorrents(), VPNConnected: s.VPNManager.Status().Connected, IndexersThroughV: s.vpnForIndexers()}
	switch {
	case s.debridOn():
		st.Client = "debrid"
	case s.downloadClientOn():
		st.Client = "external"
	}
	st.SeedRatio, _ = s.Settings.Get(settings.KeyTorrentSeedRatioLimit)
	st.SeedHours, _ = s.Settings.Get(settings.KeyTorrentSeedTimeLimitH)
	own := s.mangaOwnNyaa()
	if list, err := s.IndexerRepo.List(); err == nil {
		for _, inst := range list {
			if isNyaaInstance(inst) {
				st.Indexer, st.Own = inst.Name, inst.ID == own
				break
			}
		}
	}
	return st
}
