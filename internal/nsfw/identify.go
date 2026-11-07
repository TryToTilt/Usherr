package nsfw

// Matching video files to StashDB by fingerprint. Nothing here changes a file,
// and nothing is added to the library until an administrator confirms a match
// (RegisterFile). Only OSHASH is used: it reads 128 KiB per file. Stash's other
// two fingerprints aren't: MD5 reads whole files, and PHASH decodes 25 frames
// of each and has to match Stash's result bit for bit to be any use.

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/rdborg/mediarium/internal/mediainfo"
	"github.com/rdborg/mediarium/internal/organizer"
)

const (
	oshashChunk = 64 * 1024

	// IdentifyBatch is how many files go into one question to StashDB.
	IdentifyBatch = 25
)

// RequestGap is the least time between two questions to StashDB in a long
// run. Tests shorten it.
var RequestGap = time.Second

// FFprobe is the program that reads a file's length; tests may point it elsewhere.
var FFprobe = "ffprobe"

// What became of a file in a "Find matches" run.
const (
	IdentifyMatched    = "matched"
	IdentifyNone       = "none"
	IdentifyUnreadable = "unreadable"
	IdentifyRegistered = "registered"
	IdentifyBlocked    = "blocked"
)

// How a file's length compares with what StashDB says.
const (
	LengthAgrees  = "agrees"
	LengthDiffers = "differs"
	LengthUnknown = "unknown"
)

// OSHash computes a file's OSHASH: its size plus the 64-bit little-endian sums
// of its first and last 64 KiB, in 16 hex digits. The file isn't read further.
func OSHash(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		return "", err
	}
	return oshash(f, st.Size())
}

func oshash(r io.ReaderAt, size int64) (string, error) {
	if size < oshashChunk {
		return "", fmt.Errorf("the file is too small to be a video (%d bytes)", size)
	}
	buf := make([]byte, oshashChunk)
	sum := uint64(size)
	for _, off := range []int64{0, size - oshashChunk} {
		if _, err := r.ReadAt(buf, off); err != nil {
			return "", err
		}
		for i := 0; i+8 <= len(buf); i += 8 {
			sum += binary.LittleEndian.Uint64(buf[i:])
		}
	}
	return fmt.Sprintf("%016x", sum), nil
}

// FingerprintMatch is one video StashDB has for a fingerprint.
type FingerprintMatch struct {
	Video       Video
	Duration    int // the length people submitted with this fingerprint, in seconds
	Submissions int
}

// FindByOSHashes asks StashDB for the videos with each fingerprint. The answer
// has one list per hash, in the same order; a list is empty when nothing has
// that fingerprint. Deleted videos are left out.
func (c *StashBox) FindByOSHashes(ctx context.Context, hashes []string) ([][]FingerprintMatch, error) {
	input := make([][]map[string]string, 0, len(hashes))
	for _, h := range hashes {
		input = append(input, []map[string]string{{"hash": h, "algorithm": "OSHASH"}})
	}
	var out struct {
		R [][]struct {
			wireVideo
			Deleted      bool `json:"deleted"`
			Fingerprints []struct {
				Hash        string `json:"hash"`
				Algorithm   string `json:"algorithm"`
				Duration    int    `json:"duration"`
				Submissions int    `json:"submissions"`
			} `json:"fingerprints"`
		} `json:"findScenesBySceneFingerprints"`
	}
	err := c.query(ctx, `query($fp: [[FingerprintQueryInput!]!]!) { findScenesBySceneFingerprints(fingerprints: $fp) { `+videoFields+` deleted fingerprints { hash algorithm duration submissions } } }`,
		map[string]any{"fp": input}, &out)
	if err != nil {
		return nil, err
	}
	if len(out.R) != len(hashes) {
		return nil, fmt.Errorf("StashDB answered for %d files, not %d", len(out.R), len(hashes))
	}
	res := make([][]FingerprintMatch, len(hashes))
	for i, list := range out.R {
		for _, w := range list {
			if w.Deleted {
				continue
			}
			m := FingerprintMatch{Video: w.toVideo()}
			for _, fp := range w.Fingerprints {
				if fp.Algorithm == "OSHASH" && fp.Hash == hashes[i] {
					m.Duration, m.Submissions = fp.Duration, fp.Submissions
					break
				}
			}
			res[i] = append(res[i], m)
		}
	}
	return res, nil
}

// LengthAgreement compares a file's length with StashDB's, in seconds. More
// than 10 seconds or more than 5% apart is a disagreement. Either length
// missing is unknown.
func LengthAgreement(file, stash int) string {
	if file <= 0 || stash <= 0 {
		return LengthUnknown
	}
	diff := math.Abs(float64(file - stash))
	if diff > 10 || diff > 0.05*float64(stash) {
		return LengthDiffers
	}
	return LengthAgrees
}

// Candidate is one video proposed for a file.
type Candidate struct {
	Video       Video  `json:"video"`
	Matched     string `json:"matched"` // how: "OSHASH"
	Submissions int    `json:"submissions"`
	Length      string `json:"length"` // LengthAgrees, LengthDiffers or LengthUnknown
	StashLength int    `json:"stashLength"`
	Unsure      bool   `json:"unsure"`
}

// Propose turns StashDB's answer into candidates, the best first: lengths that
// agree before the others, then the most submissions. A candidate is unsure
// when the length doesn't agree, or when more than one video has the fingerprint.
func Propose(fileLength int, matches []FingerprintMatch) []Candidate {
	out := make([]Candidate, 0, len(matches))
	for _, m := range matches {
		stash := m.Video.Duration
		if stash <= 0 {
			stash = m.Duration // the video has no length: what people submitted will do
		}
		length := LengthAgreement(fileLength, stash)
		out = append(out, Candidate{Video: m.Video, Matched: "OSHASH", Submissions: m.Submissions, Length: length, StashLength: stash,
			Unsure: length != LengthAgrees || len(matches) > 1})
	}
	rank := map[string]int{LengthAgrees: 0, LengthUnknown: 1, LengthDiffers: 2}
	sort.SliceStable(out, func(i, j int) bool {
		if rank[out[i].Length] != rank[out[j].Length] {
			return rank[out[i].Length] < rank[out[j].Length]
		}
		return out[i].Submissions > out[j].Submissions
	})
	return out
}

// isVideoName reports whether a file name is a video's: the library's usual
// kinds and a few older or web ones found in collections like this.
func isVideoName(name string) bool {
	if organizer.IsVideoFile(name) {
		return true
	}
	switch strings.ToLower(filepath.Ext(name)) {
	case ".webm", ".mpg", ".mpeg", ".flv", ".vob":
		return true
	}
	return false
}

// FoundFile is a video file to identify.
type FoundFile struct {
	Path    string
	Size    int64
	ModNs   int64
	VideoID int64 // when it belongs to a video in the library without a StashDB id
}

// ScanFiles lists the video files under root that aren't in known. Links are
// not followed (one may lead to a virtual drive, where reading costs a
// download), and folders starting with a dot are skipped.
func ScanFiles(ctx context.Context, root string, known map[string]bool) ([]FoundFile, error) {
	var out []FoundFile
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			if p == root {
				return err
			}
			return nil // a folder that can't be read is passed over
		}
		if cerr := ctx.Err(); cerr != nil {
			return cerr
		}
		if d.IsDir() {
			if p != root && strings.HasPrefix(d.Name(), ".") {
				return filepath.SkipDir
			}
			return nil
		}
		if !d.Type().IsRegular() || !isVideoName(d.Name()) || known[filepath.Clean(p)] {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return nil
		}
		out = append(out, FoundFile{Path: p, Size: info.Size(), ModNs: info.ModTime().UnixNano()})
		return nil
	})
	return out, err
}

// ProbeVideo reads a file's length in whole seconds and its quality ("1080p"),
// from its headers. Containers Usherr can't open itself go to ffprobe.
func ProbeVideo(ctx context.Context, path string) (int, string, error) {
	if info, err := mediainfo.ReadFile(path); err == nil && info.Duration > 0 {
		quality := ""
		if info.Video != nil {
			quality = info.Video.Resolution()
		}
		return int(math.Round(info.Duration)), quality, nil
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, FFprobe, "-v", "error", "-select_streams", "v:0", "-show_entries", "stream=width,height:format=duration", "-of", "json", "--", path)
	var out bytes.Buffer
	cmd.Stdout = &out
	if err := cmd.Run(); err != nil {
		return 0, "", fmt.Errorf("ffprobe: %w", err)
	}
	return parseVideoProbe(out.Bytes())
}

func parseVideoProbe(data []byte) (int, string, error) {
	var j struct {
		Streams []struct {
			Width  int `json:"width"`
			Height int `json:"height"`
		} `json:"streams"`
		Format struct {
			Duration string `json:"duration"`
		} `json:"format"`
	}
	if err := json.Unmarshal(data, &j); err != nil {
		return 0, "", fmt.Errorf("ffprobe: %w", err)
	}
	sec, _ := strconv.ParseFloat(j.Format.Duration, 64)
	quality := ""
	if len(j.Streams) > 0 {
		quality = mediainfo.Video{Width: j.Streams[0].Width, Height: j.Streams[0].Height}.Resolution()
	}
	return int(math.Round(sec)), quality, nil
}

// wait pauses for d, or until ctx ends.
func wait(ctx context.Context, d time.Duration) error {
	if d <= 0 {
		return ctx.Err()
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

// Identifier runs the fingerprint matching.
type Identifier struct {
	Repo  *Repo
	Stash *StashBox
	Probe func(ctx context.Context, path string) (int, string, error) // ProbeVideo when nil
	// Working and Step report progress; either may be nil.
	Working func(name string)
	Step    func()
}

// Run hashes files and asks StashDB about them in batches, one batch a second
// at most, storing each answer as it comes. Files that were looked at stay
// stored when it stops early or StashDB fails.
func (id *Identifier) Run(ctx context.Context, files []FoundFile) error {
	probe := id.Probe
	if probe == nil {
		probe = ProbeVideo
	}
	step := func() {
		if id.Step != nil {
			id.Step()
		}
	}
	first := true
	for start := 0; start < len(files); start += IdentifyBatch {
		batch := files[start:min(start+IdentifyBatch, len(files))]
		var hashed []FoundFile
		var hashes []string
		for _, f := range batch {
			if err := ctx.Err(); err != nil {
				return err
			}
			if id.Working != nil {
				id.Working(filepath.Base(f.Path))
			}
			h, err := OSHash(f.Path)
			if err != nil {
				if err := id.Repo.addIdentifyItem(IdentifyItem{Path: f.Path, Size: f.Size, ModNs: f.ModNs, VideoID: f.VideoID, Status: IdentifyUnreadable, Reason: "This file couldn't be read."}); err != nil {
					return err
				}
				step()
				continue
			}
			hashed = append(hashed, f)
			hashes = append(hashes, h)
		}
		if len(hashes) == 0 {
			continue
		}
		if !first {
			if err := wait(ctx, RequestGap); err != nil {
				return err
			}
		}
		first = false
		answers, err := id.Stash.FindByOSHashes(ctx, hashes)
		if err != nil {
			return err
		}
		for i, f := range hashed {
			it := IdentifyItem{Path: f.Path, Size: f.Size, ModNs: f.ModNs, VideoID: f.VideoID, OSHash: hashes[i], Status: IdentifyNone}
			if len(answers[i]) > 0 {
				it.Status = IdentifyMatched
				// A length that can't be read leaves the match unsure, not lost.
				it.Duration, it.Quality, _ = probe(ctx, f.Path)
				if it.Quality == "" {
					it.Quality = ParseRelease(filepath.Base(f.Path)).Quality()
				}
				it.Candidates = Propose(it.Duration, answers[i])
			}
			if err := id.Repo.addIdentifyItem(it); err != nil {
				return err
			}
			step()
		}
	}
	return nil
}

// IdentifyItem is one file of a "Find matches" run.
type IdentifyItem struct {
	ID         int64       `json:"id"`
	Path       string      `json:"path"`
	Size       int64       `json:"size"`
	ModNs      int64       `json:"-"`
	VideoID    int64       `json:"videoId,omitempty"`
	OSHash     string      `json:"oshash"`
	Duration   int         `json:"duration"`
	Quality    string      `json:"quality"`
	Status     string      `json:"status"`
	Reason     string      `json:"reason,omitempty"`
	Candidates []Candidate `json:"candidates"`
}

// ClearIdentify forgets the last run. What it registered stays in the library.
func (r *Repo) ClearIdentify() error {
	_, err := r.db.Exec(`DELETE FROM nsfw_identify_items`)
	return err
}

func (r *Repo) addIdentifyItem(it IdentifyItem) error {
	if it.Candidates == nil {
		it.Candidates = []Candidate{}
	}
	cands, err := json.Marshal(it.Candidates)
	if err != nil {
		return err
	}
	var videoID any
	if it.VideoID > 0 {
		videoID = it.VideoID
	}
	_, err = r.db.Exec(`INSERT INTO nsfw_identify_items (path, size, mtime_ns, video_id, oshash, duration, quality, status, reason, candidates) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(path) DO UPDATE SET size = excluded.size, mtime_ns = excluded.mtime_ns, video_id = excluded.video_id, oshash = excluded.oshash, duration = excluded.duration,
			quality = excluded.quality, status = excluded.status, reason = excluded.reason, candidates = excluded.candidates`,
		it.Path, it.Size, it.ModNs, videoID, it.OSHash, it.Duration, it.Quality, it.Status, it.Reason, string(cands))
	if err != nil {
		return fmt.Errorf("store a match: %w", err)
	}
	return nil
}

// IdentifyItems returns the last run's files; ids, when given, limits it.
func (r *Repo) IdentifyItems(ids ...int64) ([]IdentifyItem, error) {
	q := `SELECT id, path, size, mtime_ns, COALESCE(video_id, 0), oshash, duration, quality, status, reason, candidates FROM nsfw_identify_items`
	var args []any
	if len(ids) > 0 {
		q += ` WHERE id IN (?` + strings.Repeat(`, ?`, len(ids)-1) + `)`
		for _, id := range ids {
			args = append(args, id)
		}
	}
	rows, err := r.db.Query(q+` ORDER BY path`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []IdentifyItem{}
	for rows.Next() {
		var it IdentifyItem
		var cands string
		if err := rows.Scan(&it.ID, &it.Path, &it.Size, &it.ModNs, &it.VideoID, &it.OSHash, &it.Duration, &it.Quality, &it.Status, &it.Reason, &cands); err != nil {
			return nil, err
		}
		_ = json.Unmarshal([]byte(cands), &it.Candidates)
		if it.Candidates == nil {
			it.Candidates = []Candidate{}
		}
		out = append(out, it)
	}
	return out, rows.Err()
}

func (r *Repo) setIdentifyStatus(id int64, status, reason string) error {
	_, err := r.db.Exec(`UPDATE nsfw_identify_items SET status = ?, reason = ? WHERE id = ?`, status, reason, id)
	return err
}

// KnownFilePaths returns every file the library knows about.
func (r *Repo) KnownFilePaths() (map[string]bool, error) {
	rows, err := r.db.Query(`SELECT file_path FROM nsfw_videos WHERE file_path <> ''`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]bool{}
	for rows.Next() {
		var p string
		if err := rows.Scan(&p); err != nil {
			return nil, err
		}
		out[filepath.Clean(p)] = true
	}
	return out, rows.Err()
}

// FilesWithoutStashID lists the files of library videos with no StashDB id,
// wherever they are.
func (r *Repo) FilesWithoutStashID() ([]FoundFile, error) {
	list, err := r.listNsfw(`WHERE stash_id = '' AND file_path <> '' ORDER BY id`)
	if err != nil {
		return nil, err
	}
	var out []FoundFile
	for _, v := range list {
		st, err := os.Lstat(v.FilePath)
		if err != nil || !st.Mode().IsRegular() {
			continue
		}
		out = append(out, FoundFile{Path: v.FilePath, Size: st.Size(), ModNs: st.ModTime().UnixNano(), VideoID: v.ID})
	}
	return out, nil
}

// RegisterFile records a confirmed match: the video is in the library as
// downloaded, with the file where it is. The file isn't touched, and a video
// that is new to the library isn't marked wanted, so nothing can search for
// it. blocked says why it wasn't done, when something stands in the way. A
// file already known, or changed since it was looked at, is refused, and so is
// a video that has another file or a download running.
func (r *Repo) RegisterFile(it IdentifyItem, stashID string) (blocked string, err error) {
	var pick *Candidate
	for i := range it.Candidates {
		if it.Candidates[i].Video.StashID == stashID {
			pick = &it.Candidates[i]
			break
		}
	}
	if pick == nil {
		return "That isn't one of the videos found for this file.", nil
	}
	if it.Status != IdentifyMatched && it.Status != IdentifyBlocked {
		return "This file isn't waiting to be confirmed.", nil
	}
	st, err := os.Lstat(it.Path)
	if err != nil || !st.Mode().IsRegular() {
		return "This file isn't there any more.", nil
	}
	if st.Size() != it.Size || st.ModTime().UnixNano() != it.ModNs {
		return "This file changed since it was looked at. Find matches again.", nil
	}
	known, err := r.KnownFilePaths()
	if err != nil {
		return "", err
	}
	if known[filepath.Clean(it.Path)] && it.VideoID == 0 {
		return "This file is already in your library.", nil
	}
	have, err := r.GetVideoByStashID(stashID)
	switch {
	case errors.Is(err, ErrNotFound):
	case err != nil:
		return "", err
	default:
		if it.VideoID > 0 && have.ID != it.VideoID {
			return "This video is already in your library as another entry.", nil
		}
		if have.Status == StatusDownloaded && have.FilePath != "" && have.FilePath != it.Path {
			if _, serr := os.Lstat(have.FilePath); serr == nil {
				return "You already have this video, in another file: " + have.FilePath, nil
			}
		}
		busy, err := r.HasActiveDownload(have.ID)
		if err != nil {
			return "", err
		}
		if busy {
			return "This video is downloading right now.", nil
		}
	}
	if it.VideoID > 0 {
		if _, err := r.db.Exec(`UPDATE nsfw_videos SET stash_id = ?, updated_at = ? WHERE id = ? AND stash_id = ''`, stashID, now(), it.VideoID); err != nil {
			return "", fmt.Errorf("give a video its id: %w", err)
		}
	}
	v := pick.Video
	v.AddedBy, v.Wanted = AddedImport, false
	saved, _, err := r.AddVideo(v)
	if err != nil {
		return "", err
	}
	if err := r.SetState(saved.ID, StatusDownloaded, it.Path, it.Quality); err != nil {
		return "", err
	}
	return "", nil
}

// ConfirmIdentify registers one file and records the outcome on its row. It
// reports whether the file was registered.
func (r *Repo) ConfirmIdentify(it IdentifyItem, stashID string) (bool, error) {
	blocked, err := r.RegisterFile(it, stashID)
	if err != nil {
		return false, err
	}
	if blocked != "" {
		return false, r.setIdentifyStatus(it.ID, IdentifyBlocked, blocked)
	}
	return true, r.setIdentifyStatus(it.ID, IdentifyRegistered, "")
}

// IdentifyStats counts what a run found.
func (r *Repo) IdentifyStats() (map[string]int, error) {
	rows, err := r.db.Query(`SELECT status, COUNT(*) FROM nsfw_identify_items GROUP BY status`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]int{}
	for rows.Next() {
		var status string
		var n int
		if err := rows.Scan(&status, &n); err != nil {
			return nil, err
		}
		out[status] = n
	}
	return out, rows.Err()
}
