package directdl

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"
)

const searchPage = `<html><body>
<article class="post-1 post"><h1 class="post-title"><a href="https://getcomics.org/marvel/daredevil-black-armor-2-2023/">Daredevil &#8211; Black Armor #2 (2023)</a></h1>
<p><strong>Year : </strong>2023 | <strong>Size :</strong> 62 MB</p></article>
<article class="post-2 post"><h1 class="post-title"><a href="https://getcomics.org/marvel/daredevil-omnibus-vol-2-2023/">Daredevil Omnibus Vol. 2 (2023)</a></h1>
<p>Year : 2023 | Size : 3.6 GB</p></article>
</body></html>`

func TestParseSearch(t *testing.T) {
	ps := parseSearch(searchPage)
	if len(ps) != 2 {
		t.Fatalf("got %d posts", len(ps))
	}
	if ps[0].Title != "Daredevil – Black Armor #2 (2023)" || ps[0].SizeBytes != 62<<20 || ps[0].Year != 2023 {
		t.Errorf("first = %+v", ps[0])
	}
	if ps[1].SizeBytes < 3<<30 {
		t.Errorf("second size = %d", ps[1].SizeBytes)
	}
}

func TestParseLinks(t *testing.T) {
	page := `<a class="aio-blue" href="https://getcomics.org/dls/b" title="TERABOX">TERABOX</a>
<a class="aio-purple" href="https://getcomics.org/dls/c" title="MEGA">MEGA</a>
<a class="aio-red" href="https://getcomics.org/dls/a" title="Download Now">Download Now</a>
<a href="https://readcomicsonline.ru/x">Read Online</a>
<a class="aio-orange" href="https://getcomics.org/dls/d" title="MEDIAFIRE">MEDIAFIRE</a>`
	ls := parseLinks(page)
	var hosts []string
	for _, l := range ls {
		hosts = append(hosts, l.Host)
	}
	if got := strings.Join(hosts, ","); got != "main,mediafire,mega,terabox" {
		t.Fatalf("hosts = %s", got)
	}
}

func TestMegaName(t *testing.T) {
	key := []byte("0123456789abcdef")
	plain := []byte(`MEGA{"n":"Daredevil 002 (2024).cbz"}`)
	for len(plain)%16 != 0 {
		plain = append(plain, 0)
	}
	block, _ := aes.NewCipher(key)
	enc := make([]byte, len(plain))
	cipher.NewCBCEncrypter(block, make([]byte, 16)).CryptBlocks(enc, plain)
	at := strings.TrimRight(base64.URLEncoding.EncodeToString(enc), "=")
	if got := megaName(at, key); got != "Daredevil 002 (2024).cbz" {
		t.Fatalf("name = %q", got)
	}
	k := bytes.Repeat([]byte{7}, 32)
	if _, _, err := megaKey(k); err != nil {
		t.Fatal(err)
	}
	_ = json.Valid
}

// Home fork: a post is dead only when every host refused for good; a busy
// host (429) or a network failure leaves it to be tried again.
func TestHostRefused(t *testing.T) {
	for msg, want := range map[string]bool{
		"fs2.comicfiles.ru answered 403":                      true,
		"pixeldrain.com answered 404":                         true,
		"www.mediafire.com answered 410":                      true,
		"pixeldrain.com answered 429":                         false,
		"fs2.comicfiles.ru answered 503":                      false,
		"dial tcp: lookup pixeldrain.com: no such host":       false,
		"MEGA answered 509 (the hourly limit may be reached)": false,
	} {
		if got := hostRefused.MatchString(msg); got != want {
			t.Errorf("%q: refused = %v, want %v", msg, got, want)
		}
	}
}
