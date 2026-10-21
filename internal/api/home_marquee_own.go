package api

// Usherr: a Marquee screen's own settings are only what it keeps different
// from the shared ones — picking a look for the TV keeps that look there,
// and everything else still follows the shared settings as they change.
//
// Stored in marquee_screens.settings as {"own": {field: value, ...}}. An
// older row holds a whole copy of the settings; it is turned into the
// fields that differ from the shared settings the first time it is read.

import (
	"bytes"
	"context"
	"encoding/json"
	"sort"
)

// screenOwn is a screen's own fields, as the settings' JSON names them.
type screenOwn map[string]json.RawMessage

// settingsMap is the settings as their JSON fields.
func settingsMap(m marqueeSettings) map[string]json.RawMessage {
	b, _ := json.Marshal(m)
	var out map[string]json.RawMessage
	_ = json.Unmarshal(b, &out)
	return out
}

// diffOwn is what m keeps different from shared.
func diffOwn(shared, m marqueeSettings) screenOwn {
	a, b := settingsMap(shared.clean()), settingsMap(m.clean())
	own := screenOwn{}
	for k, v := range b {
		if !bytes.Equal(a[k], v) {
			own[k] = v
		}
	}
	return own
}

// withOwn is the shared settings with a screen's own fields on top.
func withOwn(shared marqueeSettings, own screenOwn) marqueeSettings {
	if len(own) == 0 {
		return shared
	}
	all := settingsMap(shared)
	for k, v := range own {
		all[k] = v
	}
	b, _ := json.Marshal(all)
	var m marqueeSettings
	if json.Unmarshal(b, &m) != nil {
		return shared
	}
	return m.clean()
}

// encodeOwn is what's stored for own ("" when the screen keeps nothing).
func encodeOwn(own screenOwn) string {
	if len(own) == 0 {
		return ""
	}
	b, _ := json.Marshal(map[string]screenOwn{"own": own})
	return string(b)
}

// decodeOwn reads what's stored; legacy is true for an older whole copy.
func decodeOwn(raw string) (own screenOwn, legacy bool) {
	if raw == "" {
		return nil, false
	}
	var wrapped struct {
		Own screenOwn `json:"own"`
	}
	if json.Unmarshal([]byte(raw), &wrapped) == nil && wrapped.Own != nil {
		return wrapped.Own, false
	}
	return nil, true
}

// screenOwnSettings is a screen's own fields, turning an older whole copy
// into just its differences (and saving that) on the way.
func (s *Server) screenOwnSettings(ctx context.Context, id, raw string, shared marqueeSettings) screenOwn {
	own, legacy := decodeOwn(raw)
	if !legacy {
		return own
	}
	var m marqueeSettings
	if json.Unmarshal([]byte(raw), &m) != nil {
		return nil
	}
	own = diffOwn(shared, m)
	_, _ = s.db.ExecContext(ctx, `UPDATE marquee_screens SET settings = ? WHERE id = ? AND settings = ?`, encodeOwn(own), id, raw)
	return own
}

// ownKeys lists a screen's own fields, sorted.
func ownKeys(own screenOwn) []string {
	keys := make([]string, 0, len(own))
	for k := range own {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// saveScreenOwn stores a screen's own fields.
func (s *Server) saveScreenOwn(ctx context.Context, id string, own screenOwn) (bool, error) {
	res, err := s.db.ExecContext(ctx, `UPDATE marquee_screens SET settings = ? WHERE id = ?`, encodeOwn(own), id)
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	return n > 0, nil
}
