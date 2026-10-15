package directdl

// MEGA public file links (https://mega.nz/file/ID#KEY). The file is stored
// encrypted: the key in the link (after #) gives the AES key and counter
// start; the name is in encrypted attributes. MEGA's API hands out a
// temporary address to the encrypted data, which is decrypted on the way in.
// MEGA limits how much an address can fetch in a few hours; past that it
// refuses, and the next host is tried.

import (
	"bytes"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"time"
)

var megaLink = regexp.MustCompile(`mega(?:\.co)?\.nz/(?:file/|#!)([A-Za-z0-9_-]{8})[#!]([A-Za-z0-9_-]{43})`)

func megaB64(s string) ([]byte, error) {
	s = strings.NewReplacer("-", "+", "_", "/", ",", "").Replace(s)
	for len(s)%4 != 0 {
		s += "="
	}
	return base64.StdEncoding.DecodeString(s)
}

// megaKey splits a file link's 256-bit key into the AES key and the counter's
// starting nonce.
func megaKey(k []byte) (key, nonce []byte, err error) {
	if len(k) != 32 {
		return nil, nil, errors.New("the MEGA link's key is the wrong length")
	}
	key = make([]byte, 16)
	for i := range 16 {
		key[i] = k[i] ^ k[i+16]
	}
	return key, k[16:24], nil
}

func (c *Client) fetchMega(ctx context.Context, link, dir string, progress Progress) (string, error) {
	m := megaLink.FindStringSubmatch(link)
	if m == nil {
		return "", errors.New("not a MEGA file link (folders aren't supported)")
	}
	raw, err := megaB64(m[2])
	if err != nil {
		return "", errors.New("the MEGA link's key can't be read")
	}
	key, nonce, err := megaKey(raw)
	if err != nil {
		return "", err
	}
	body, _ := json.Marshal([]map[string]any{{"a": "g", "g": 1, "ssl": 2, "p": m[1]}})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, fmt.Sprintf("https://g.api.mega.co.nz/cs?id=%d", time.Now().UnixNano()%1e9), bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := (&http.Client{Timeout: 30 * time.Second}).Do(req)
	if err != nil {
		return "", err
	}
	var raws json.RawMessage
	err = json.NewDecoder(resp.Body).Decode(&raws)
	resp.Body.Close()
	if err != nil {
		return "", errors.New("MEGA's answer couldn't be read")
	}
	var errCode []int
	if json.Unmarshal(raws, &errCode) == nil && len(errCode) == 1 && errCode[0] < 0 {
		return "", megaError(errCode[0])
	}
	var info []struct {
		Size int64  `json:"s"`
		URL  string `json:"g"`
		Attr string `json:"at"`
		E    int    `json:"e"`
	}
	if err := json.Unmarshal(raws, &info); err != nil || len(info) != 1 {
		var n int
		if json.Unmarshal(raws, &n) == nil && n < 0 {
			return "", megaError(n)
		}
		return "", errors.New("MEGA's answer couldn't be read")
	}
	if info[0].URL == "" {
		return "", errors.New("MEGA gave no download address (the file may be gone, or the hourly limit reached)")
	}
	name := megaName(info[0].Attr, key)
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", err
	}
	iv := make([]byte, 16)
	copy(iv, nonce) // the counter starts at zero after the 8-byte nonce
	dreq, err := http.NewRequestWithContext(ctx, http.MethodGet, info[0].URL, nil)
	if err != nil {
		return "", err
	}
	dresp, err := (&http.Client{}).Do(dreq)
	if err != nil {
		return "", err
	}
	defer dresp.Body.Close()
	if dresp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("MEGA answered %d (the hourly limit may be reached)", dresp.StatusCode)
	}
	stream := cipher.StreamReader{S: cipher.NewCTR(block, iv), R: dresp.Body}
	return save(stream, info[0].Size, name, dir, progress)
}

// megaName decrypts a file's attributes ("MEGA{"n":"name"}", AES-CBC with a
// zero IV) for its name.
func megaName(at string, key []byte) string {
	b, err := megaB64(at)
	if err != nil || len(b) == 0 || len(b)%16 != 0 {
		return "mega-download.cbz"
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return "mega-download.cbz"
	}
	out := make([]byte, len(b))
	cipher.NewCBCDecrypter(block, make([]byte, 16)).CryptBlocks(out, b)
	out = bytes.TrimRight(out, "\x00")
	if !bytes.HasPrefix(out, []byte("MEGA{")) {
		return "mega-download.cbz"
	}
	var a struct {
		N string `json:"n"`
	}
	if json.Unmarshal(out[4:], &a) != nil || a.N == "" {
		return "mega-download.cbz"
	}
	return a.N
}

func megaError(code int) error {
	switch code {
	case -9:
		return errors.New("MEGA says the file is gone")
	case -16:
		return errors.New("MEGA has blocked the file")
	case -17, -4:
		return errors.New("MEGA's download limit is reached for now")
	case -11:
		return errors.New("MEGA refused access")
	}
	return fmt.Errorf("MEGA answered error %d", code)
}
