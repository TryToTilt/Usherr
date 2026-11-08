package manga

// The tables of a person's MangaDex account: see
// db/migrations/9250_home_manga_mangadex_account.sql. The sign-in values come in
// and go out already encrypted; this file never sees them in the clear.

import (
	"database/sql"
	"encoding/json"
	"errors"
	"sort"
	"strings"
	"time"
)

// DexAccount is a person's saved account, its secrets still encrypted.
type DexAccount struct {
	UserID       int64
	ClientID     string
	ClientSecret string
	Username     string
	Password     string
	Refresh      string
	State        string // '', ok, refused
	Problem      string
	ConnectedAt  string
	LightAt      string
	FullAt       string
	ListsAt      string
	Stats        DexSyncStats
}

// DexSyncStats is what the last sync did, for the page.
type DexSyncStats struct {
	Follows    int    `json:"follows"`              // followed on MangaDex
	Added      int    `json:"added"`                // added to the library by the last syncs
	Outside    int    `json:"outside"`              // followed there, not in the library
	Waiting    int    `json:"waiting,omitempty"`    // still to be added, at night
	ReadPushed int    `json:"readPushed"`           // read marks sent the last time
	ReadPulled int    `json:"readPulled"`           // read marks taken the last time
	Note       string `json:"note,omitempty"`       // a sentence about the last sync
	Unfinished bool   `json:"unfinished,omitempty"` // a big library: the rest waits for the night
}

// Account states.
const (
	DexAccountNew     = ""
	DexAccountOK      = "ok"
	DexAccountRefused = "refused"
)

const dexAccountCols = `user_id, client_id, client_secret, username, password, refresh_token, state, problem, connected_at, light_at, full_at, lists_at, stats`

func scanDexAccount(r rowScanner) (DexAccount, error) {
	var a DexAccount
	var stats string
	err := r.Scan(&a.UserID, &a.ClientID, &a.ClientSecret, &a.Username, &a.Password, &a.Refresh, &a.State, &a.Problem, &a.ConnectedAt, &a.LightAt, &a.FullAt, &a.ListsAt, &stats)
	if errors.Is(err, sql.ErrNoRows) {
		return a, ErrNotFound
	}
	_ = json.Unmarshal([]byte(stats), &a.Stats)
	return a, err
}

// SaveDexAccount keeps a person's (encrypted) sign-in, starting them afresh.
func (r *Repo) SaveDexAccount(a DexAccount) error {
	_, err := r.db.Exec(`INSERT INTO manga_md_accounts (user_id, client_id, client_secret, username, password, refresh_token, state, problem)
		VALUES (?, ?, ?, ?, ?, '', '', '')
		ON CONFLICT (user_id) DO UPDATE SET client_id = excluded.client_id, client_secret = excluded.client_secret, username = excluded.username,
			password = excluded.password, refresh_token = '', state = '', problem = '', light_at = '', lists_at = ''`,
		a.UserID, a.ClientID, a.ClientSecret, a.Username, a.Password)
	return err
}

// DexAccountOf is a person's account, or ErrNotFound.
func (r *Repo) DexAccountOf(user int64) (DexAccount, error) {
	return scanDexAccount(r.db.QueryRow(`SELECT `+dexAccountCols+` FROM manga_md_accounts WHERE user_id = ?`, user))
}

// DexAccounts are every connected account MangaDex hasn't turned down.
func (r *Repo) DexAccounts() ([]DexAccount, error) {
	rows, err := r.db.Query(`SELECT ` + dexAccountCols + ` FROM manga_md_accounts WHERE state != 'refused' ORDER BY user_id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []DexAccount
	for rows.Next() {
		a, err := scanDexAccount(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

// SetDexAccountState notes how the sign-in went.
func (r *Repo) SetDexAccountState(user int64, state, problem string) error {
	_, err := r.db.Exec(`UPDATE manga_md_accounts SET state = ?, problem = ? WHERE user_id = ?`, state, problem, user)
	return err
}

// SetDexRefresh keeps a new (encrypted) refresh token.
func (r *Repo) SetDexRefresh(user int64, enc string) error {
	_, err := r.db.Exec(`UPDATE manga_md_accounts SET refresh_token = ? WHERE user_id = ?`, enc, user)
	return err
}

// NoteDexSync records a sync: which ("light", "full", "lists") and what it did.
func (r *Repo) NoteDexSync(user int64, which string, at time.Time, stats *DexSyncStats) error {
	col := map[string]string{"light": "light_at", "full": "full_at", "lists": "lists_at"}[which]
	if col == "" {
		return errors.New("unknown sync")
	}
	if _, err := r.db.Exec(`UPDATE manga_md_accounts SET `+col+` = ? WHERE user_id = ?`, at.UTC().Format(time.RFC3339), user); err != nil {
		return err
	}
	if stats == nil {
		return nil
	}
	b, _ := json.Marshal(stats)
	_, err := r.db.Exec(`UPDATE manga_md_accounts SET stats = ? WHERE user_id = ?`, string(b), user)
	return err
}

// DeleteDexAccount forgets everything kept from a person's MangaDex: the sign-in,
// what was synced, lists, feed and history. Their follows and reading status in
// Usherr stay: those are Usherr's.
func (r *Repo) DeleteDexAccount(user int64) error {
	tx, err := r.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, table := range []string{"manga_md_accounts", "manga_md_follow_synced", "manga_md_read_synced", "manga_md_outside", "manga_md_lists", "manga_md_feed", "manga_md_history"} {
		if _, err := tx.Exec(`DELETE FROM `+table+` WHERE user_id = ?`, user); err != nil {
			return err
		}
	}
	if _, err := tx.Exec(`UPDATE manga_md_status SET synced = '' WHERE user_id = ?`, user); err != nil {
		return err
	}
	return tx.Commit()
}

// ---- follows in Usherr ----

// SetFollowing follows a library series for a person, or stops.
func (r *Repo) SetFollowing(user, seriesID int64, on bool) error {
	var err error
	if on {
		_, err = r.db.Exec(`INSERT OR IGNORE INTO manga_md_follows (user_id, series_id) SELECT ?, id FROM manga_series WHERE id = ?`, user, seriesID)
	} else {
		_, err = r.db.Exec(`DELETE FROM manga_md_follows WHERE user_id = ? AND series_id = ?`, user, seriesID)
	}
	return err
}

// Following says whether a person follows a series.
func (r *Repo) Following(user, seriesID int64) bool {
	var n int
	_ = r.db.QueryRow(`SELECT COUNT(*) FROM manga_md_follows WHERE user_id = ? AND series_id = ?`, user, seriesID).Scan(&n)
	return n > 0
}

// FollowedSeries are the series a person follows.
func (r *Repo) FollowedSeries(user int64) ([]int64, error) {
	return r.ids(`SELECT series_id FROM manga_md_follows WHERE user_id = ? ORDER BY series_id`, user)
}

func (r *Repo) ids(q string, args ...any) ([]int64, error) {
	rows, err := r.db.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}

func (r *Repo) strings(q string, args ...any) ([]string, error) {
	rows, err := r.db.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var s string
		if err := rows.Scan(&s); err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

func setOf(list []string) map[string]bool {
	out := make(map[string]bool, len(list))
	for _, s := range list {
		out[s] = true
	}
	return out
}

// SortedKeys are a set's members in order.
func SortedKeys(set map[string]bool) []string {
	out := make([]string, 0, len(set))
	for k, on := range set {
		if on {
			out = append(out, k)
		}
	}
	sort.Strings(out)
	return out
}

// ---- links between the library and MangaDex ----

// DexLinks are the library's series linked to an entry on MangaDex: series id to
// MangaDex id.
func (r *Repo) DexLinks() (map[int64]string, error) {
	rows, err := r.db.Query(`SELECT series_id, dex_id FROM manga_dex_series WHERE link = 'linked' AND dex_id != ''`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[int64]string{}
	for rows.Next() {
		var id int64
		var dex string
		if err := rows.Scan(&id, &dex); err != nil {
			return nil, err
		}
		out[id] = strings.ToLower(dex)
	}
	return out, rows.Err()
}

// SeriesByDex is the library series linked to a MangaDex entry.
func (r *Repo) SeriesByDex(dexID string) (Series, error) {
	var id int64
	err := r.db.QueryRow(`SELECT series_id FROM manga_dex_series WHERE link = 'linked' AND lower(dex_id) = ? LIMIT 1`, strings.ToLower(dexID)).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return Series{}, ErrNotFound
	}
	if err != nil {
		return Series{}, err
	}
	return r.GetSeries(id)
}

// LinkToDex links a series to its MangaDex entry, as the account says it is,
// unless a person or AniList already linked it.
func (r *Repo) LinkToDex(seriesID int64, dexID string) error {
	d := r.DexStateOf(seriesID)
	d.SeriesID = seriesID
	if d.Link == DexLinked && d.DexID != "" {
		return nil
	}
	d.DexID, d.Link, d.LinkHow, d.Candidates = strings.ToLower(dexID), DexLinked, "mangadex", nil
	return r.SaveDexState(d)
}

// ---- what both sides agreed ----

// FollowSynced are the MangaDex ids both sides followed at the last sync.
func (r *Repo) FollowSynced(user int64) (map[string]bool, error) {
	list, err := r.strings(`SELECT dex_id FROM manga_md_follow_synced WHERE user_id = ?`, user)
	return setOf(list), err
}

// SetFollowSynced replaces them.
func (r *Repo) SetFollowSynced(user int64, set map[string]bool) error {
	tx, err := r.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`DELETE FROM manga_md_follow_synced WHERE user_id = ?`, user); err != nil {
		return err
	}
	for _, id := range SortedKeys(set) {
		if _, err := tx.Exec(`INSERT INTO manga_md_follow_synced (user_id, dex_id) VALUES (?, ?)`, user, id); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// SetFollowSyncedOne changes one, after a follow sent at once.
func (r *Repo) SetFollowSyncedOne(user int64, dexID string, on bool) error {
	var err error
	if on {
		_, err = r.db.Exec(`INSERT OR IGNORE INTO manga_md_follow_synced (user_id, dex_id) VALUES (?, ?)`, user, dexID)
	} else {
		_, err = r.db.Exec(`DELETE FROM manga_md_follow_synced WHERE user_id = ? AND dex_id = ?`, user, dexID)
	}
	return err
}

// FollowedOnDex says whether any connected account follows a MangaDex entry there.
func (r *Repo) FollowedOnDex(dexID string) bool {
	var n int
	_ = r.db.QueryRow(`SELECT COUNT(*) FROM manga_md_follow_synced f JOIN manga_md_accounts a ON a.user_id = f.user_id WHERE f.dex_id = ? AND a.state = 'ok'`, strings.ToLower(dexID)).Scan(&n)
	return n > 0
}

// ReadSynced are the chapter numbers of a series both sides had read at the last sync.
func (r *Repo) ReadSynced(user int64, dexID string) (map[string]bool, error) {
	list, err := r.strings(`SELECT number FROM manga_md_read_synced WHERE user_id = ? AND dex_id = ?`, user, dexID)
	return setOf(list), err
}

// SetReadSynced replaces them.
func (r *Repo) SetReadSynced(user int64, dexID string, set map[string]bool) error {
	tx, err := r.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`DELETE FROM manga_md_read_synced WHERE user_id = ? AND dex_id = ?`, user, dexID); err != nil {
		return err
	}
	for _, n := range SortedKeys(set) {
		if _, err := tx.Exec(`INSERT INTO manga_md_read_synced (user_id, dex_id, number) VALUES (?, ?, ?)`, user, dexID, n); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// ---- followed there, not here ----

// SetDexOutside replaces the list of followed series that aren't in the library.
func (r *Repo) SetDexOutside(user int64, reasons map[string]string) error {
	tx, err := r.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`DELETE FROM manga_md_outside WHERE user_id = ?`, user); err != nil {
		return err
	}
	for id, why := range reasons {
		if _, err := tx.Exec(`INSERT INTO manga_md_outside (user_id, dex_id, reason) VALUES (?, ?, ?)`, user, id, why); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// DexOutside is that list: MangaDex id to reason.
func (r *Repo) DexOutside(user int64) (map[string]string, error) {
	rows, err := r.db.Query(`SELECT dex_id, reason FROM manga_md_outside WHERE user_id = ?`, user)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]string{}
	for rows.Next() {
		var id, why string
		if err := rows.Scan(&id, &why); err != nil {
			return nil, err
		}
		out[id] = why
	}
	return out, rows.Err()
}

// ---- reading status ----

// ReadingStatus is a person's status of a series, and what both sides last had.
func (r *Repo) ReadingStatus(user, seriesID int64) (status, synced string) {
	_ = r.db.QueryRow(`SELECT status, synced FROM manga_md_status WHERE user_id = ? AND series_id = ?`, user, seriesID).Scan(&status, &synced)
	return status, synced
}

// SetReadingStatus changes a person's status of a series here.
func (r *Repo) SetReadingStatus(user, seriesID int64, status string) error {
	_, err := r.db.Exec(`INSERT INTO manga_md_status (user_id, series_id, status) SELECT ?, id, ? FROM manga_series WHERE id = ?
		ON CONFLICT (user_id, series_id) DO UPDATE SET status = excluded.status`, user, status, seriesID)
	return err
}

// SetReadingStatusSynced sets both the status and what both sides now have.
func (r *Repo) SetReadingStatusSynced(user, seriesID int64, status string) error {
	_, err := r.db.Exec(`INSERT INTO manga_md_status (user_id, series_id, status, synced) SELECT ?, id, ?, ? FROM manga_series WHERE id = ?
		ON CONFLICT (user_id, series_id) DO UPDATE SET status = excluded.status, synced = excluded.synced`, user, status, status, seriesID)
	return err
}

// ---- lists, feed, history ----

// SaveDexLists replaces a person's lists.
func (r *Repo) SaveDexLists(user int64, lists []DexAccountList) error {
	tx, err := r.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`DELETE FROM manga_md_lists WHERE user_id = ?`, user); err != nil {
		return err
	}
	for i, l := range lists {
		b, _ := json.Marshal(l.Manga)
		if _, err := tx.Exec(`INSERT INTO manga_md_lists (user_id, list_id, name, visibility, manga, position) VALUES (?, ?, ?, ?, ?, ?)`, user, l.ID, l.Name, l.Visibility, string(b), i); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// DexLists are a person's lists, as last read.
func (r *Repo) DexLists(user int64) ([]DexAccountList, error) {
	rows, err := r.db.Query(`SELECT list_id, name, visibility, manga FROM manga_md_lists WHERE user_id = ? ORDER BY position`, user)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []DexAccountList{}
	for rows.Next() {
		var l DexAccountList
		var manga string
		if err := rows.Scan(&l.ID, &l.Name, &l.Visibility, &manga); err != nil {
			return nil, err
		}
		_ = json.Unmarshal([]byte(manga), &l.Manga)
		if l.Manga == nil {
			l.Manga = []string{}
		}
		out = append(out, l)
	}
	return out, rows.Err()
}

// SetInDexList changes one list here, after the change was made on MangaDex.
func (r *Repo) SetInDexList(user int64, listID, dexID string, in bool) error {
	var manga string
	if err := r.db.QueryRow(`SELECT manga FROM manga_md_lists WHERE user_id = ? AND list_id = ?`, user, listID).Scan(&manga); err != nil {
		return err
	}
	var ids []string
	_ = json.Unmarshal([]byte(manga), &ids)
	keep := ids[:0]
	for _, id := range ids {
		if id != dexID {
			keep = append(keep, id)
		}
	}
	if in {
		keep = append(keep, dexID)
	}
	b, _ := json.Marshal(keep)
	_, err := r.db.Exec(`UPDATE manga_md_lists SET manga = ? WHERE user_id = ? AND list_id = ?`, string(b), user, listID)
	return err
}

// SaveDexFeed replaces a person's follow feed.
func (r *Repo) SaveDexFeed(user int64, list []DexFeedChapter) error {
	tx, err := r.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`DELETE FROM manga_md_feed WHERE user_id = ?`, user); err != nil {
		return err
	}
	for _, c := range list {
		if _, err := tx.Exec(`INSERT OR IGNORE INTO manga_md_feed (user_id, chapter_id, dex_id, number, volume, title, group_name, readable_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
			user, c.ID, c.DexID, c.Number, c.Volume, c.Title, c.Group, c.ReadableAt); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// DexFeed is a person's follow feed, newest first.
func (r *Repo) DexFeed(user int64) ([]DexFeedChapter, error) {
	rows, err := r.db.Query(`SELECT chapter_id, dex_id, number, volume, title, group_name, readable_at FROM manga_md_feed WHERE user_id = ? ORDER BY readable_at DESC`, user)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []DexFeedChapter{}
	for rows.Next() {
		var c DexFeedChapter
		if err := rows.Scan(&c.ID, &c.DexID, &c.Number, &c.Volume, &c.Title, &c.Group, &c.ReadableAt); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// SaveDexHistory replaces a person's reading history.
func (r *Repo) SaveDexHistory(user int64, list []DexRead) error {
	tx, err := r.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`DELETE FROM manga_md_history WHERE user_id = ?`, user); err != nil {
		return err
	}
	for _, h := range list {
		if _, err := tx.Exec(`INSERT OR IGNORE INTO manga_md_history (user_id, chapter_id, read_at) VALUES (?, ?, ?)`, user, h.ChapterID, h.ReadAt); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// DexHistoryItem is a read chapter, with its series when known.
type DexHistoryItem struct {
	ChapterID string
	DexID     string
	Number    string
	ReadAt    string
}

// DexHistory is a person's reading history, newest first.
func (r *Repo) DexHistory(user int64) ([]DexHistoryItem, error) {
	rows, err := r.db.Query(`SELECT h.chapter_id, COALESCE(c.dex_id, ''), COALESCE(c.number, ''), h.read_at FROM manga_md_history h
		LEFT JOIN manga_md_chapters c ON c.chapter_id = h.chapter_id WHERE h.user_id = ? ORDER BY h.read_at DESC`, user)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []DexHistoryItem{}
	for rows.Next() {
		var h DexHistoryItem
		if err := rows.Scan(&h.ChapterID, &h.DexID, &h.Number, &h.ReadAt); err != nil {
			return nil, err
		}
		out = append(out, h)
	}
	return out, rows.Err()
}

// ---- which chapter is which, and titles ----

// SaveChapterRefs keeps chapter lookups.
func (r *Repo) SaveChapterRefs(refs []DexChapterRef) error {
	tx, err := r.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, c := range refs {
		if _, err := tx.Exec(`INSERT INTO manga_md_chapters (chapter_id, dex_id, volume, number) VALUES (?, ?, ?, ?)
			ON CONFLICT (chapter_id) DO UPDATE SET dex_id = excluded.dex_id, volume = excluded.volume, number = excluded.number`, c.ChapterID, c.DexID, c.Volume, c.Number); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// NoteAggregated says a series' aggregate was read.
func (r *Repo) NoteAggregated(dexID, language string, at time.Time) error {
	_, err := r.db.Exec(`INSERT INTO manga_md_aggregated (dex_id, language, fetched_at) VALUES (?, ?, ?)
		ON CONFLICT (dex_id, language) DO UPDATE SET fetched_at = excluded.fetched_at`, dexID, language, at.UTC().Format(time.RFC3339))
	return err
}

// AggregatedAt is when a series' aggregate was last read, or zero.
func (r *Repo) AggregatedAt(dexID, language string) time.Time {
	var at string
	_ = r.db.QueryRow(`SELECT fetched_at FROM manga_md_aggregated WHERE dex_id = ? AND language = ?`, dexID, language).Scan(&at)
	t, _ := time.Parse(time.RFC3339, at)
	return t
}

// KnownChapterIDs says which of these chapter ids are already known.
func (r *Repo) KnownChapterIDs(ids []string) map[string]bool {
	out := map[string]bool{}
	for _, id := range ids {
		var n int
		_ = r.db.QueryRow(`SELECT COUNT(*) FROM manga_md_chapters WHERE chapter_id = ?`, id).Scan(&n)
		if n == 0 {
			_ = r.db.QueryRow(`SELECT COUNT(*) FROM manga_chapter_copies WHERE lower(dex_id) = ?`, id).Scan(&n)
		}
		if n > 0 {
			out[id] = true
		}
	}
	return out
}

// DexChapterMap is what's known of one series' chapters on MangaDex: chapter id
// to number, and number to the ids of its uploads (the one Usherr chose first),
// and each number's volume.
type DexChapterMap struct {
	Number map[string]string
	IDs    map[string][]string
	Volume map[string]string
}

// ChapterMapOf puts together a series' chapter ids from its own chapters and from
// the lookups.
func (r *Repo) ChapterMapOf(seriesID int64, dexID string) (DexChapterMap, error) {
	m := DexChapterMap{Number: map[string]string{}, IDs: map[string][]string{}, Volume: map[string]string{}}
	add := func(id, number, volume string, first bool) {
		id = strings.ToLower(id)
		if !IsDexID(id) || number == "" {
			return
		}
		if _, ok := m.Number[id]; ok {
			return
		}
		m.Number[id] = number
		if first {
			m.IDs[number] = append([]string{id}, m.IDs[number]...)
		} else {
			m.IDs[number] = append(m.IDs[number], id)
		}
		if volume != "" && m.Volume[number] == "" {
			m.Volume[number] = volume
		}
	}
	if seriesID > 0 {
		rows, err := r.db.Query(`SELECT c.number, c.volume, c.dex_id, COALESCE(p.dex_id, '') FROM manga_chapters c
			LEFT JOIN manga_chapter_copies p ON p.chapter_id = c.id WHERE c.series_id = ?`, seriesID)
		if err != nil {
			return m, err
		}
		for rows.Next() {
			var number, volume, chosen, copy string
			if err := rows.Scan(&number, &volume, &chosen, &copy); err != nil {
				rows.Close()
				return m, err
			}
			add(chosen, number, volume, true)
			add(copy, number, volume, false)
		}
		rows.Close()
	}
	rows, err := r.db.Query(`SELECT chapter_id, number, volume FROM manga_md_chapters WHERE dex_id = ? ORDER BY chapter_id`, dexID)
	if err != nil {
		return m, err
	}
	defer rows.Close()
	for rows.Next() {
		var id, number, volume string
		if err := rows.Scan(&id, &number, &volume); err != nil {
			return m, err
		}
		add(id, number, volume, false)
	}
	return m, rows.Err()
}

// LocalReading is what a person has read of a series here, by chapter number, and
// which numbers Usherr can say anything about: those with a chapter row, and those
// of a volume they've read. A number Usherr can't show is left to MangaDex.
type LocalReading struct {
	Read     map[string]bool
	Shown    map[string]bool
	ByVolume map[string]string // a number read only through its volume: that volume
	LatestMs int64             // the newest read mark here
}

// LocalReadingOf works that out from the chapter rows, the read volumes and the map.
func (r *Repo) LocalReadingOf(user, seriesID int64, m DexChapterMap) (LocalReading, error) {
	lr := LocalReading{Read: map[string]bool{}, Shown: map[string]bool{}, ByVolume: map[string]string{}}
	readVol := map[string]bool{}
	rows, err := r.db.Query(`SELECT v.number, m.read_ms FROM manga_read m JOIN manga_volumes v ON v.id = m.volume_id WHERE m.user_id = ? AND v.series_id = ?`, user, seriesID)
	if err != nil {
		return lr, err
	}
	for rows.Next() {
		var n string
		var ms int64
		if err := rows.Scan(&n, &ms); err != nil {
			rows.Close()
			return lr, err
		}
		readVol[n] = true
		lr.LatestMs = max(lr.LatestMs, ms)
	}
	rows.Close()
	rows, err = r.db.Query(`SELECT c.number, c.volume, COALESCE(m.read_ms, 0) FROM manga_chapters c
		LEFT JOIN manga_chapter_read m ON m.chapter_id = c.id AND m.user_id = ? WHERE c.series_id = ?`, user, seriesID)
	if err != nil {
		return lr, err
	}
	for rows.Next() {
		var number, volume string
		var ms int64
		if err := rows.Scan(&number, &volume, &ms); err != nil {
			rows.Close()
			return lr, err
		}
		lr.Shown[number] = true
		if ms > 0 {
			lr.Read[number] = true
			lr.LatestMs = max(lr.LatestMs, ms)
		} else if volume != "" && readVol[volume] {
			lr.Read[number] = true
			lr.ByVolume[number] = volume
		}
	}
	rows.Close()
	for number, volume := range m.Volume {
		if volume != "" && readVol[volume] && !lr.Shown[number] {
			lr.Shown[number], lr.Read[number], lr.ByVolume[number] = true, true, volume
		}
	}
	return lr, nil
}

// ApplyDexReading brings a person's read marks here in line with the merged set:
// chapters read there are marked here, chapters unread there are unmarked here
// (a volume read here loses its mark, its other chapters keeping theirs), and a
// volume whose every chapter is read counts as read.
func (r *Repo) ApplyDexReading(user, seriesID int64, m DexChapterMap, lr LocalReading, scope, merged map[string]bool, now time.Time) (int, error) {
	tx, err := r.db.Begin()
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	changed := 0
	ms := now.UnixMilli()
	chapterRow := func(number string) int64 {
		var id int64
		_ = tx.QueryRow(`SELECT id FROM manga_chapters WHERE series_id = ? AND number = ?`, seriesID, number).Scan(&id)
		return id
	}
	unreadVolumes := map[string]bool{}
	for number := range scope {
		was, is := lr.Read[number], merged[number]
		switch {
		case is && !was:
			if id := chapterRow(number); id > 0 {
				if _, err := tx.Exec(`INSERT OR IGNORE INTO manga_chapter_read (user_id, chapter_id, read_ms) VALUES (?, ?, ?)`, user, id, ms); err != nil {
					return 0, err
				}
				changed++
			}
		case was && !is:
			if v := lr.ByVolume[number]; v != "" {
				unreadVolumes[v] = true
			} else if v := m.Volume[number]; v != "" {
				unreadVolumes[v] = true // its volume, if read here, would keep it read
			}
			if id := chapterRow(number); id > 0 {
				if _, err := tx.Exec(`DELETE FROM manga_chapter_read WHERE user_id = ? AND chapter_id = ?`, user, id); err != nil {
					return 0, err
				}
			}
			changed++
		}
	}
	for v := range unreadVolumes {
		if _, err := tx.Exec(`DELETE FROM manga_read WHERE user_id = ? AND volume_id IN (SELECT id FROM manga_volumes WHERE series_id = ? AND number = ?)`, user, seriesID, v); err != nil {
			return 0, err
		}
		// Its other chapters stay read, as chapters.
		rows, err := tx.Query(`SELECT id, number FROM manga_chapters WHERE series_id = ? AND volume = ?`, seriesID, v)
		if err != nil {
			return 0, err
		}
		var keep []int64
		for rows.Next() {
			var id int64
			var number string
			if err := rows.Scan(&id, &number); err != nil {
				rows.Close()
				return 0, err
			}
			if merged[number] {
				keep = append(keep, id)
			}
		}
		rows.Close()
		for _, id := range keep {
			if _, err := tx.Exec(`INSERT OR IGNORE INTO manga_chapter_read (user_id, chapter_id, read_ms) VALUES (?, ?, ?)`, user, id, ms); err != nil {
				return 0, err
			}
		}
	}
	// A volume here whose every known chapter is read is read.
	byVolume := map[string][]string{}
	for number, v := range m.Volume {
		if v != "" {
			byVolume[v] = append(byVolume[v], number)
		}
	}
	for v, numbers := range byVolume {
		if unreadVolumes[v] {
			continue
		}
		all := len(numbers) > 0
		for _, n := range numbers {
			if !merged[n] {
				all = false
				break
			}
		}
		if !all {
			continue
		}
		res, err := tx.Exec(`INSERT OR IGNORE INTO manga_read (user_id, volume_id, read_ms) SELECT ?, id, ? FROM manga_volumes WHERE series_id = ? AND number = ?`, user, ms, seriesID, v)
		if err != nil {
			return 0, err
		}
		if n, _ := res.RowsAffected(); n > 0 {
			changed++
		}
	}
	return changed, tx.Commit()
}

// SaveDexTitles keeps series' titles, ratings and covers.
func (r *Repo) SaveDexTitles(list []DexFollowTitle, now time.Time) error {
	tx, err := r.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, t := range list {
		if _, err := tx.Exec(`INSERT INTO manga_md_titles (dex_id, title, rating, cover, year, anilist_id, fetched_at) VALUES (?, ?, ?, ?, ?, ?, ?)
			ON CONFLICT (dex_id) DO UPDATE SET title = excluded.title, rating = excluded.rating, cover = excluded.cover, year = excluded.year,
				anilist_id = excluded.anilist_id, fetched_at = excluded.fetched_at`,
			strings.ToLower(t.ID), t.Title, t.Rating, t.Cover, t.Year, t.AniListID, now.UTC().Format(time.RFC3339)); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// DexTitlesOf are the kept titles of these ids.
func (r *Repo) DexTitlesOf(ids []string) (map[string]DexFollowTitle, error) {
	out := map[string]DexFollowTitle{}
	for _, id := range ids {
		var t DexFollowTitle
		err := r.db.QueryRow(`SELECT dex_id, title, rating, cover, year, anilist_id FROM manga_md_titles WHERE dex_id = ?`, id).Scan(&t.ID, &t.Title, &t.Rating, &t.Cover, &t.Year, &t.AniListID)
		if errors.Is(err, sql.ErrNoRows) {
			continue
		}
		if err != nil {
			return nil, err
		}
		out[id] = t
	}
	return out, nil
}

// DexCoverKnown says whether a cover address came from MangaDex for a kept title.
func (r *Repo) DexCoverKnown(u string) bool {
	var n int
	_ = r.db.QueryRow(`SELECT COUNT(*) FROM manga_md_titles WHERE cover = ?`, u).Scan(&n)
	return n > 0
}

// RecentlyReadSeries are the series a person read something of since a time.
func (r *Repo) RecentlyReadSeries(user, sinceMs int64) ([]int64, error) {
	return r.ids(`SELECT c.series_id FROM manga_chapter_read m JOIN manga_chapters c ON c.id = m.chapter_id WHERE m.user_id = ? AND m.read_ms >= ?
		UNION SELECT v.series_id FROM manga_read m JOIN manga_volumes v ON v.id = m.volume_id WHERE m.user_id = ? AND m.read_ms >= ?`, user, sinceMs, user, sinceMs)
}

// HasReadVolumes says whether a person has read any volume of a series.
func (r *Repo) HasReadVolumes(user, seriesID int64) bool {
	var n int
	_ = r.db.QueryRow(`SELECT COUNT(*) FROM manga_read m JOIN manga_volumes v ON v.id = m.volume_id WHERE m.user_id = ? AND v.series_id = ?`, user, seriesID).Scan(&n)
	return n > 0
}
