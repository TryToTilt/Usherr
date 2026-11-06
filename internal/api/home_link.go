package api

// Usherr: linked screenings, step 1. Linked Usherrs: this Usherr's identity,
// one-time codes that link it with a friend's Usherr, and the list of links,
// all through the Usherr Link Relay (internal/link). Off until the switch
// "Link with other Usherrs" is on; only then does Usherr connect to a relay.
// Screenings, playback, chat and rewards over the link come in later steps,
// as message types added to the node's registry.

import (
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"log"
	"math"
	"net"
	"net/http"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"

	"github.com/rdborg/mediarium/internal/brand"
	"github.com/rdborg/mediarium/internal/link"
	"github.com/rdborg/mediarium/internal/link/screening"
	"github.com/rdborg/mediarium/internal/netguard"
)

const (
	keyLink          = "home.link"         // JSON linkSettings
	linkIdentityFile = "link-identity.key" // next to secret.key, 0600; not in the database or its backups
	linkMaxInvites   = 5                   // codes waiting at once
)

type linkSettings struct {
	On    bool   `json:"on"`
	Relay string `json:"relay"` // "" for the default relay
	Name  string `json:"name"`  // "" for the default name
}

func (s *Server) linkSettings() linkSettings {
	var l linkSettings
	if raw, _ := s.Settings.Get(keyLink); raw != "" {
		_ = json.Unmarshal([]byte(raw), &l)
	}
	if r, err := link.CleanRelay(l.Relay); err != nil || r == link.DefaultRelay {
		l.Relay = ""
	} else {
		l.Relay = r
	}
	return l
}

// linkState is the running link node; nil while the switch is off.
type linkState struct {
	mu   sync.Mutex
	node *link.Node
	id   *link.Identity
	// dial replaces how relays are reached (tests).
	dial func(relay string) link.DialFunc
	// joinWait is how long a join waits for the other Usherr's answer.
	joinWait time.Duration
	// scr is linked screenings' share (home_link_screenings.go).
	scr linkScreeningState
}

// linkIdentity is this Usherr's identity, made the first time it's needed.
func (s *Server) linkIdentity() (*link.Identity, error) {
	l := &s.linkS
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.id != nil {
		return l.id, nil
	}
	if s.cfg.ConfigDir == "" {
		return nil, errors.New("no settings folder")
	}
	id, err := link.LoadOrCreateIdentity(filepath.Join(s.cfg.ConfigDir, linkIdentityFile))
	if err != nil {
		return nil, err
	}
	l.id = id
	return id, nil
}

// StartLink connects to the relay when "Link with other Usherrs" is on. The
// returned function disconnects. Linking isn't automation, so the paused
// mode doesn't stop it.
func (s *Server) StartLink() (stop func()) {
	s.restartLink()
	return s.stopLink
}

func (s *Server) stopLink() {
	s.stopScreeningJob()
	l := &s.linkS
	l.mu.Lock()
	n := l.node
	l.node = nil
	l.mu.Unlock()
	if n != nil {
		n.Close()
	}
}

func (s *Server) restartLink() {
	s.stopLink()
	set := s.linkSettings()
	if !set.On {
		return
	}
	id, err := s.linkIdentity()
	if err != nil {
		log.Printf("link: this Usherr's identity can't be read or made: %v", err)
		return
	}
	s.linkTidy()
	n := link.NewNode(link.NodeOptions{
		Identity: id,
		Store:    linkStore{s},
		Relay:    s.linkRelay(),
		Name:     s.linkName,
		Version:  s.version,
		Dial:     s.linkDial,
		Logf:     log.Printf,
		GroupKey: s.linkGroupKey, // step 2: screening keys
	})
	if err := screening.Register(n.Registry(), linkBook{s}); err != nil {
		log.Printf("link: the screening messages couldn't be set up: %v", err)
	}
	if err := screening.RegisterRoom(n.Registry(), linkBook{s}, linkBook{s}); err != nil { // step 3: synced playback
		log.Printf("link: the playback messages couldn't be set up: %v", err)
	}
	l := &s.linkS
	l.mu.Lock()
	l.node = n
	l.mu.Unlock()
	n.Start(context.Background())
	s.startScreeningJob()
}

// linkNode is the running node, or nil when linking is off.
func (s *Server) linkNode() *link.Node {
	l := &s.linkS
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.node
}

func (s *Server) linkRelay() string {
	if r := s.linkSettings().Relay; r != "" {
		return r
	}
	return link.DefaultRelay
}

// linkDial reaches the shared relay on the public internet only; a relay of
// your own may be on the home network.
func (s *Server) linkDial(relay string) link.DialFunc {
	s.linkS.mu.Lock()
	d := s.linkS.dial
	s.linkS.mu.Unlock()
	if d != nil {
		return d(relay)
	}
	if relay != link.DefaultRelay {
		return link.DefaultDial
	}
	return func(ctx context.Context, url string) (*websocket.Conn, error) {
		d := websocket.Dialer{
			HandshakeTimeout: 15 * time.Second,
			NetDialContext:   (&net.Dialer{Timeout: 15 * time.Second, Control: netguard.ControlPublic}).DialContext,
		}
		c, _, err := d.DialContext(ctx, url, nil)
		return c, err
	}
}

// linkName is the name linked Usherrs see.
func (s *Server) linkName() string {
	if n := s.linkSettings().Name; n != "" {
		return link.CleanName(n)
	}
	return s.linkDefaultName()
}

// linkDefaultName is "<first administrator's first name>'s Usherr".
func (s *Server) linkDefaultName() string {
	var first string
	if s.db != nil {
		_ = s.db.QueryRow(`SELECT first_name FROM users WHERE is_admin = 1 ORDER BY id LIMIT 1`).Scan(&first)
	}
	if first = strings.TrimSpace(first); first != "" {
		return link.CleanName(first + "’s " + brand.Name)
	}
	return brand.Name
}

// linkTidy forgets invites that ended more than a week ago, and marks joins
// nobody answered in two days.
func (s *Server) linkTidy() {
	now := time.Now().UnixMilli()
	_, _ = s.db.Exec(`UPDATE link_invites SET used = 1, state = 'expired' WHERE used = 0 AND role = 'join' AND created_at < ?`, now-2*link.InviteLife.Milliseconds())
	_, _ = s.db.Exec(`DELETE FROM link_invites WHERE (used = 1 OR expires_at < ?) AND created_at < ?`, now, now-7*24*time.Hour.Milliseconds())
}

// linkStore keeps links and invites in the database (link.Store).
type linkStore struct{ s *Server }

const linkPeerCols = `their_id, their_x, name, version, relay, last_seen`

func scanLinkPeer(sc interface{ Scan(...any) error }) (link.Peer, error) {
	var p link.Peer
	var x string
	var seen int64
	if err := sc.Scan(&p.ID, &x, &p.Name, &p.Version, &p.Relay, &seen); err != nil {
		return p, err
	}
	p.XPub, _ = base64.RawURLEncoding.DecodeString(x)
	if seen > 0 {
		p.LastSeen = time.UnixMilli(seen)
	}
	return p, nil
}

func (st linkStore) Peer(ctx context.Context, id string) (link.Peer, bool, error) {
	if id == "" {
		return link.Peer{}, false, nil
	}
	p, err := scanLinkPeer(st.s.db.QueryRowContext(ctx, `SELECT `+linkPeerCols+` FROM link_peers WHERE their_id = ? AND status = 'linked'`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return link.Peer{}, false, nil
	}
	return p, err == nil && len(p.XPub) == 32, err
}

func (st linkStore) Peers(ctx context.Context) ([]link.Peer, error) {
	rows, err := st.s.db.QueryContext(ctx, `SELECT `+linkPeerCols+` FROM link_peers WHERE status = 'linked' AND their_id <> ''`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []link.Peer
	for rows.Next() {
		p, err := scanLinkPeer(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

func (st linkStore) SavePeer(ctx context.Context, p link.Peer) error {
	now := time.Now().UnixMilli()
	seen := int64(0)
	if !p.LastSeen.IsZero() {
		seen = p.LastSeen.UnixMilli()
	}
	_, err := st.s.db.ExecContext(ctx, `INSERT INTO link_peers (their_id, their_x, name, version, relay, status, created_at, last_seen)
		VALUES (?, ?, ?, ?, ?, 'linked', ?, ?)
		ON CONFLICT (their_id) WHERE their_id <> '' DO UPDATE SET
			their_x = excluded.their_x, name = excluded.name, version = excluded.version, relay = excluded.relay,
			status = 'linked', unlinked_by = '', last_seen = MAX(last_seen, excluded.last_seen)`,
		p.ID, base64.RawURLEncoding.EncodeToString(p.XPub), p.Name, p.Version, p.Relay, now, seen)
	return err
}

func (st linkStore) ForgetPeer(ctx context.Context, id, by string) error {
	row, linked := st.s.peerRow(ctx, id)
	_, err := st.s.db.ExecContext(ctx, `UPDATE link_peers SET status = 'unlinked', unlinked_by = ?, their_id = '', their_x = '' WHERE their_id = ?`, by, id)
	if err == nil && linked {
		st.s.screeningsForgetPeer(ctx, row) // step 2: its screenings go, on both sides
	}
	return err
}

func (st linkStore) SeenPeer(ctx context.Context, id string, at time.Time) error {
	_, err := st.s.db.ExecContext(ctx, `UPDATE link_peers SET last_seen = ? WHERE their_id = ?`, at.UnixMilli(), id)
	return err
}

const linkInviteCols = `role, box, secret, relay, created_at, expires_at, used, state`

func (st linkStore) scanInvite(sc interface{ Scan(...any) error }) (link.Invite, error) {
	var in link.Invite
	var secret string
	var created, expires int64
	if err := sc.Scan(&in.Role, &in.Box, &secret, &in.Relay, &created, &expires, &in.Used, &in.State); err != nil {
		return in, err
	}
	in.Created, in.Expires = time.UnixMilli(created), time.UnixMilli(expires)
	if st.s.box == nil {
		return in, errors.New("no server key")
	}
	plain, err := st.s.box.Decrypt(secret)
	if err != nil {
		return in, err
	}
	in.Secret, err = base64.StdEncoding.DecodeString(plain)
	return in, err
}

func (st linkStore) Invites(ctx context.Context) ([]link.Invite, error) {
	rows, err := st.s.db.QueryContext(ctx, `SELECT `+linkInviteCols+` FROM link_invites WHERE used = 0`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []link.Invite
	for rows.Next() {
		in, err := st.scanInvite(rows)
		if err != nil {
			continue // a secret.key that doesn't belong to this database: the code can't be used
		}
		out = append(out, in)
	}
	return out, rows.Err()
}

func (st linkStore) Invite(ctx context.Context, box string) (link.Invite, bool, error) {
	in, err := st.scanInvite(st.s.db.QueryRowContext(ctx, `SELECT `+linkInviteCols+` FROM link_invites WHERE box = ?`, box))
	if errors.Is(err, sql.ErrNoRows) {
		return link.Invite{}, false, nil
	}
	return in, err == nil, err
}

func (st linkStore) SaveInvite(ctx context.Context, in link.Invite) error {
	if st.s.box == nil {
		return errors.New("no server key")
	}
	secret, err := st.s.box.Encrypt(base64.StdEncoding.EncodeToString(in.Secret))
	if err != nil {
		return err
	}
	_, err = st.s.db.ExecContext(ctx, `INSERT INTO link_invites (role, box, secret, relay, created_at, expires_at, used, state)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT (box) DO UPDATE SET role = excluded.role, secret = excluded.secret, relay = excluded.relay,
			created_at = excluded.created_at, expires_at = excluded.expires_at, used = excluded.used, state = excluded.state`,
		in.Role, in.Box, secret, in.Relay, in.Created.UnixMilli(), in.Expires.UnixMilli(), in.Used, in.State)
	return err
}

func (st linkStore) FinishInvite(ctx context.Context, box, state string) (bool, error) {
	res, err := st.s.db.ExecContext(ctx, `UPDATE link_invites SET used = 1, state = ? WHERE box = ? AND used = 0`, state, box)
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	return n == 1, nil
}

// What the page shows.

type linkInviteOut struct {
	ID        int64  `json:"id"`
	Code      string `json:"code,omitempty"`
	CreatedAt int64  `json:"createdAt"`
	ExpiresAt int64  `json:"expiresAt"`
}

type linkPeerOut struct {
	ID          int64  `json:"id"`
	Name        string `json:"name"`      // the nickname, or the name it gives itself
	TheirName   string `json:"theirName"` // the name it gives itself
	Nickname    string `json:"nickname"`
	Fingerprint string `json:"fingerprint,omitempty"`
	Relay       string `json:"relay"`
	Status      string `json:"status"`               // linked, unlinked
	UnlinkedBy  string `json:"unlinkedBy,omitempty"` // me, them
	Version     string `json:"version,omitempty"`
	CreatedAt   int64  `json:"createdAt"`
	LastSeen    int64  `json:"lastSeen"`
}

func (s *Server) linkPeersOut(ctx context.Context) []linkPeerOut {
	out := []linkPeerOut{}
	rows, err := s.db.QueryContext(ctx, `SELECT id, their_id, name, nickname, relay, status, unlinked_by, version, created_at, last_seen
		FROM link_peers ORDER BY status = 'unlinked', COALESCE(NULLIF(nickname, ''), name) COLLATE NOCASE, id`)
	if err != nil {
		return out
	}
	defer rows.Close()
	for rows.Next() {
		var p linkPeerOut
		var their string
		if rows.Scan(&p.ID, &their, &p.TheirName, &p.Nickname, &p.Relay, &p.Status, &p.UnlinkedBy, &p.Version, &p.CreatedAt, &p.LastSeen) != nil {
			continue
		}
		p.Fingerprint = link.FingerprintOf(their)
		p.Name = p.Nickname
		if p.Name == "" {
			p.Name = p.TheirName
		}
		out = append(out, p)
	}
	return out
}

type linkStatusOut struct {
	link.Status
	ClockWarning bool `json:"clockWarning"`
}

func (s *Server) linkAnswer(ctx context.Context) map[string]any {
	set := s.linkSettings()
	fp, me := "", ""
	if id, err := s.linkIdentity(); err == nil {
		fp, me = id.Fingerprint(), id.ID()
	}
	st := linkStatusOut{Status: link.Status{State: "off"}}
	invites, joins := []linkInviteOut{}, []linkInviteOut{}
	n := s.linkNode()
	if n != nil {
		st.Status = n.Status()
		st.ClockWarning = st.State == "connected" && math.Abs(float64(st.SkewMs)) > float64(link.ClockWarning.Milliseconds())
	}
	now := time.Now()
	rows, err := s.db.QueryContext(ctx, `SELECT id, role, box, created_at, expires_at FROM link_invites WHERE used = 0 ORDER BY created_at DESC`)
	if err == nil {
		defer rows.Close()
		for rows.Next() {
			var o linkInviteOut
			var role, box string
			if rows.Scan(&o.ID, &role, &box, &o.CreatedAt, &o.ExpiresAt) != nil {
				continue
			}
			switch role {
			case "host":
				if o.ExpiresAt <= now.UnixMilli() {
					continue
				}
				if in, ok, _ := (linkStore{s}).Invite(ctx, box); ok && n != nil {
					o.Code = n.CodeOf(in)
				} else if ok {
					o.Code = link.EncodeCode(in.Secret, link.RelayInCode(in.Relay, link.DefaultRelay))
				}
				invites = append(invites, o)
			case "join":
				joins = append(joins, o)
			}
		}
	}
	return map[string]any{
		"settings":     set,
		"name":         s.linkName(),
		"defaultName":  s.linkDefaultName(),
		"relay":        s.linkRelay(),
		"defaultRelay": link.DefaultRelay,
		"id":           me,
		"fingerprint":  fp,
		"status":       st,
		"invites":      invites,
		"joins":        joins,
	}
}

// GET /api/link (admin): this Usherr's identity, its relay and the
// connection, and its codes waiting.
func (s *Server) handleGetLink(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.linkAnswer(r.Context()))
}

// PUT /api/link/settings (admin): the switch, the relay, this Usherr's name.
func (s *Server) handlePutLinkSettings(w http.ResponseWriter, r *http.Request) {
	var body linkSettings
	if err := decodeJSON(r, &body); err != nil {
		writeError(w, http.StatusBadRequest, "That request couldn't be read.")
		return
	}
	body.Relay = strings.TrimSpace(body.Relay)
	if body.Relay != "" {
		relay, err := link.CleanRelay(body.Relay)
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		if relay == link.DefaultRelay {
			relay = ""
		}
		body.Relay = relay
	}
	body.Name = strings.TrimSpace(body.Name)
	if body.Name != "" {
		body.Name = link.CleanName(body.Name)
	}
	before := s.linkSettings()
	b, _ := json.Marshal(body)
	if err := s.Settings.Set(keyLink, string(b), false); err != nil {
		writeError(w, http.StatusInternalServerError, "The settings couldn't be saved.")
		return
	}
	switch {
	case body.On != before.On || body.Relay != before.Relay || (body.On && s.linkNode() == nil):
		s.restartLink()
		if n := s.linkNode(); n != nil {
			n.WaitConnected(r.Context(), 5*time.Second)
		}
	case body.Name != before.Name:
		if n := s.linkNode(); n != nil {
			ctx, cancel := context.WithTimeout(context.WithoutCancel(r.Context()), 10*time.Second)
			n.Renamed(ctx)
			cancel()
		}
	}
	writeJSON(w, http.StatusOK, s.linkAnswer(r.Context()))
}

const linkOffText = "Switch on Link with other Usherrs first."

// POST /api/link/invites (admin): a new one-time code.
func (s *Server) handleCreateLinkInvite(w http.ResponseWriter, r *http.Request) {
	n := s.linkNode()
	if n == nil {
		writeError(w, http.StatusConflict, linkOffText)
		return
	}
	var waiting int
	_ = s.db.QueryRowContext(r.Context(), `SELECT COUNT(*) FROM link_invites WHERE role = 'host' AND used = 0 AND expires_at > ?`, time.Now().UnixMilli()).Scan(&waiting)
	if waiting >= linkMaxInvites {
		writeError(w, http.StatusConflict, "Five codes are waiting already. Cancel one first.")
		return
	}
	in, code, err := n.CreateInvite(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "The code couldn't be made.")
		return
	}
	var id int64
	_ = s.db.QueryRowContext(r.Context(), `SELECT id FROM link_invites WHERE box = ?`, in.Box).Scan(&id)
	writeJSON(w, http.StatusCreated, linkInviteOut{ID: id, Code: code, CreatedAt: in.Created.UnixMilli(), ExpiresAt: in.Expires.UnixMilli()})
}

// DELETE /api/link/invites/{id} (admin): cancel a code, or stop waiting
// for a friend's Usherr to answer.
func (s *Server) handleDeleteLinkInvite(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r)
	if !ok {
		writeError(w, http.StatusBadRequest, "That isn't a code here.")
		return
	}
	var box string
	if err := s.db.QueryRowContext(r.Context(), `SELECT box FROM link_invites WHERE id = ? AND used = 0`, id).Scan(&box); err != nil {
		writeError(w, http.StatusNotFound, "That code isn't waiting anymore.")
		return
	}
	if n := s.linkNode(); n != nil {
		_ = n.CancelInvite(r.Context(), box)
	} else {
		_, _ = (linkStore{s}).FinishInvite(r.Context(), box, "cancelled")
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// POST /api/link/join {"code"} (admin): link with a friend's Usherr.
func (s *Server) handleLinkJoin(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Code string `json:"code"`
	}
	if err := decodeJSON(r, &body); err != nil || len(body.Code) > 400 {
		writeError(w, http.StatusBadRequest, "That request couldn't be read.")
		return
	}
	n := s.linkNode()
	if n == nil {
		writeError(w, http.StatusConflict, linkOffText)
		return
	}
	wait := s.linkS.joinWait
	if wait <= 0 {
		wait = 10 * time.Second
	}
	res, err := n.Join(r.Context(), body.Code, wait)
	var refused link.RefusedError
	switch {
	case err == nil:
	case errors.Is(err, link.ErrNotACode), errors.Is(err, link.ErrCodeTypo), errors.Is(err, link.ErrCodeRelay), errors.Is(err, link.ErrOwnCode):
		writeError(w, http.StatusBadRequest, err.Error())
		return
	case errors.Is(err, link.ErrCodeGone), errors.As(err, &refused):
		writeError(w, http.StatusGone, err.Error())
		return
	case errors.Is(err, link.ErrTooFast):
		writeError(w, http.StatusTooManyRequests, err.Error())
		return
	case errors.Is(err, link.ErrRelayDown):
		writeError(w, http.StatusBadGateway, err.Error())
		return
	default:
		writeError(w, http.StatusInternalServerError, "The code couldn't be used.")
		return
	}
	out := map[string]any{"state": res.State}
	if res.State == "linked" {
		for _, p := range s.linkPeersOut(r.Context()) {
			if p.Fingerprint == link.FingerprintOf(res.Peer.ID) && p.Status == "linked" {
				out["peer"] = p
			}
		}
	}
	writeJSON(w, http.StatusOK, out)
}

// GET /api/link/peers (admin): the linked Usherrs.
func (s *Server) handleLinkPeers(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"peers": s.linkPeersOut(r.Context())})
}

// PUT /api/link/peers/{id} {"name"} (admin): call a linked Usherr something
// else here ("" for the name it gives itself).
func (s *Server) handleRenameLinkPeer(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r)
	var body struct {
		Name string `json:"name"`
	}
	if !ok || decodeJSON(r, &body) != nil {
		writeError(w, http.StatusBadRequest, "That request couldn't be read.")
		return
	}
	name := strings.TrimSpace(body.Name)
	if name != "" {
		name = link.CleanName(name)
	}
	res, err := s.db.ExecContext(r.Context(), `UPDATE link_peers SET nickname = ? WHERE id = ?`, name, id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "The name couldn't be saved.")
		return
	}
	if n, _ := res.RowsAffected(); n == 0 {
		writeError(w, http.StatusNotFound, "That Usherr isn't linked.")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"peers": s.linkPeersOut(r.Context())})
}

// DELETE /api/link/peers/{id} (admin): unlink. The other Usherr is told,
// then its keys are forgotten here. An unlinked one is taken off the list.
func (s *Server) handleUnlinkPeer(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r)
	if !ok {
		writeError(w, http.StatusBadRequest, "That request couldn't be read.")
		return
	}
	var their, status string
	if err := s.db.QueryRowContext(r.Context(), `SELECT their_id, status FROM link_peers WHERE id = ?`, id).Scan(&their, &status); err != nil {
		writeError(w, http.StatusNotFound, "That Usherr isn't linked.")
		return
	}
	if status != "linked" || their == "" {
		_, _ = s.db.ExecContext(r.Context(), `DELETE FROM link_peers WHERE id = ?`, id)
		writeJSON(w, http.StatusOK, map[string]any{"peers": s.linkPeersOut(r.Context()), "told": false})
		return
	}
	told := false
	if n := s.linkNode(); n != nil {
		told = n.Unlink(r.Context(), their) == nil
	}
	if !told {
		// Linking is off: forgotten here all the same, so nothing from it is
		// taken in anymore.
		if err := (linkStore{s}).ForgetPeer(r.Context(), their, "me"); err != nil {
			writeError(w, http.StatusInternalServerError, "That Usherr couldn't be unlinked.")
			return
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"peers": s.linkPeersOut(r.Context()), "told": told})
}
