package directdl

// Following a post's buttons to the file, and downloading it.

import (
	"context"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"
)

// MaxFile is the largest file fetched (a big pack or omnibus fits).
const MaxFile = 40 << 30

// Progress is told how much has come in, of how much (0 when unknown).
type Progress func(done, total int64)

// Fetch downloads the post's file into dir, trying its hosts best first,
// and returns the file's path.
func (c *Client) Fetch(ctx context.Context, postURL, dir string, progress Progress) (string, error) {
	links, err := c.Links(ctx, postURL)
	if err != nil {
		return "", err
	}
	if len(links) == 0 {
		return "", errors.New("the post has no download links")
	}
	var errs []string
	gone := true // every host answered that the file isn't there (or isn't ours to have)
	for _, l := range links {
		if l.Host == "terabox" {
			continue // needs an account
		}
		if ctx.Err() != nil {
			return "", ctx.Err()
		}
		file, err := c.fetchLink(ctx, postURL, l, dir, progress)
		if err == nil {
			return file, nil
		}
		errs = append(errs, fmt.Sprintf("%s: %v", l.Name, err))
		gone = gone && hostRefused.MatchString(err.Error())
	}
	err = fmt.Errorf("no host gave the file (%s)", strings.Join(errs, "; "))
	if gone && len(errs) > 0 {
		return "", &GoneError{err: err}
	}
	return "", err
}

// hostRefused is a host's answer that won't change by asking again later:
// 403 (refused), 404 (gone), 410 and the like, but not 429 (too many
// requests, which passes).
var hostRefused = regexp.MustCompile(`answered 4(0[0-9]|1[0-9]|2[0-8]|[3-9][0-9])$`)

// GoneError is Fetch's error when every host answered that the file isn't
// there: the post is dead, so another search should pick something else.
type GoneError struct{ err error }

func (e *GoneError) Error() string { return e.err.Error() }
func (e *GoneError) Unwrap() error { return e.err }

// fetchLink follows one button: GetComics' /dls/ links redirect to the host.
func (c *Client) fetchLink(ctx context.Context, postURL string, l Link, dir string, progress Progress) (string, error) {
	target := l.URL
	if strings.Contains(target, "/dls/") {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
		if err != nil {
			return "", err
		}
		req.Header.Set("User-Agent", UserAgent)
		req.Header.Set("Referer", postURL)
		resp, err := c.noRedirect.Do(req)
		if err != nil {
			return "", err
		}
		resp.Body.Close()
		loc := resp.Header.Get("Location")
		if resp.StatusCode/100 != 3 || loc == "" {
			return "", fmt.Errorf("the link didn't lead anywhere (%d)", resp.StatusCode)
		}
		target = loc
	}
	switch h := hostOf(target); {
	case strings.HasSuffix(h, "mega.nz") || strings.HasSuffix(h, "mega.co.nz"):
		return c.fetchMega(ctx, target, dir, progress)
	case strings.HasSuffix(h, "pixeldrain.com"):
		id := path.Base(strings.TrimSuffix(target, "/"))
		return c.download(ctx, "https://pixeldrain.com/api/file/"+url.PathEscape(id)+"?download", "", dir, progress)
	case strings.HasSuffix(h, "mediafire.com"):
		direct, err := c.mediafireDirect(ctx, target)
		if err != nil {
			return "", err
		}
		return c.download(ctx, direct, target, dir, progress)
	default:
		return c.download(ctx, target, postURL, dir, progress)
	}
}

var mediafireButton = regexp.MustCompile(`href="(https?://download[^"]+\.mediafire\.com/[^"]+)"`)

func (c *Client) mediafireDirect(ctx context.Context, page string) (string, error) {
	body, err := c.Page(ctx, page)
	if err != nil {
		return "", err
	}
	m := mediafireButton.FindStringSubmatch(body)
	if m == nil {
		return "", errors.New("MediaFire's page has no download button (the file may be gone)")
	}
	return m[1], nil
}

// download saves u into dir under the name the server gives (or the URL's).
func (c *Client) download(ctx context.Context, u, referer, dir string, progress Progress) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", UserAgent)
	if referer != "" {
		req.Header.Set("Referer", referer)
	}
	cl := &http.Client{} // no overall timeout: big files take a while; ctx stops it
	resp, err := cl.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("%s answered %d", hostOf(u), resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); strings.HasPrefix(ct, "text/html") {
		return "", fmt.Errorf("%s sent a web page, not the file (it may need a browser or have a limit)", hostOf(u))
	}
	name := ""
	if _, params, err := mime.ParseMediaType(resp.Header.Get("Content-Disposition")); err == nil {
		name = params["filename"]
	}
	if name == "" {
		if p, err := url.Parse(resp.Request.URL.String()); err == nil {
			name, _ = url.PathUnescape(path.Base(p.Path))
		}
	}
	return save(resp.Body, resp.ContentLength, name, dir, progress)
}

// save writes r into dir as name (made safe), reporting progress.
func save(r io.Reader, total int64, name, dir string, progress Progress) (string, error) {
	name = safeName(name)
	if total > MaxFile {
		return "", fmt.Errorf("the file is %d GB, more than this fetches", total>>30)
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	dst := filepath.Join(dir, name)
	part := dst + ".part"
	f, err := os.Create(part)
	if err != nil {
		return "", err
	}
	buf := make([]byte, 1<<20)
	var done int64
	for {
		n, rerr := r.Read(buf)
		if n > 0 {
			if _, werr := f.Write(buf[:n]); werr != nil {
				f.Close()
				os.Remove(part)
				return "", werr
			}
			done += int64(n)
			if done > MaxFile {
				f.Close()
				os.Remove(part)
				return "", errors.New("the file is larger than this fetches")
			}
			if progress != nil {
				progress(done, total)
			}
		}
		if rerr == io.EOF {
			break
		}
		if rerr != nil {
			f.Close()
			os.Remove(part)
			return "", rerr
		}
	}
	if err := f.Close(); err != nil {
		os.Remove(part)
		return "", err
	}
	if total > 0 && done != total {
		os.Remove(part)
		return "", fmt.Errorf("the download stopped short (%d of %d bytes)", done, total)
	}
	if err := os.Rename(part, dst); err != nil {
		return "", err
	}
	return dst, nil
}

var unsafeChars = regexp.MustCompile(`[<>:"/\\|?*\x00-\x1f]`)

func safeName(name string) string {
	name = strings.TrimSpace(unsafeChars.ReplaceAllString(filepath.Base(name), "_"))
	if name == "" || name == "." || name == ".." {
		name = "download.cbz"
	}
	if len(name) > 200 {
		ext := filepath.Ext(name)
		name = name[:200-len(ext)] + ext
	}
	return name
}
