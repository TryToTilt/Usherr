package subtitles

import (
	"bytes"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"

	"golang.org/x/text/encoding"
	"golang.org/x/text/encoding/charmap"
	"golang.org/x/text/encoding/japanese"
	"golang.org/x/text/encoding/korean"
	"golang.org/x/text/encoding/simplifiedchinese"
	"golang.org/x/text/encoding/traditionalchinese"
	"golang.org/x/text/encoding/unicode"
)

// ToUTF8 returns subtitle text as UTF-8. A lot of subtitle files, Romanian,
// Polish, Greek and Hebrew ones among them, are saved in an older Windows code
// page, and players show them as garbage unless they are converted. Text that
// already is UTF-8 (with or without a byte order mark) is left alone; anything
// else is read as the code page that language is usually saved in.
func ToUTF8(data []byte, language string) []byte {
	switch {
	case bytes.HasPrefix(data, []byte{0xEF, 0xBB, 0xBF}):
		return data[3:]
	case bytes.HasPrefix(data, []byte{0xFF, 0xFE}), bytes.HasPrefix(data, []byte{0xFE, 0xFF}):
		if out, err := unicode.UTF16(unicode.LittleEndian, unicode.UseBOM).NewDecoder().Bytes(data); err == nil {
			return out
		}
		return data
	case utf8.Valid(data):
		return data
	case mostlyUTF8(data):
		// One stray bad byte in an otherwise UTF-8 file: keep the text, mark
		// the bad spot, rather than reading everything as a code page.
		return []byte(strings.ToValidUTF8(string(data), "\uFFFD"))
	}
	enc := codePageFor(language)
	if enc == nil {
		return data // no legacy code page is known for this language: leave it alone
	}
	if baseLanguage(strings.ToLower(language)) == "sr" && mostlyCyrillic(data) {
		enc = charmap.Windows1251
	}
	if out, err := enc.NewDecoder().Bytes(data); err == nil {
		return out
	}
	return data
}

// mostlyUTF8 reports whether data is UTF-8 with a few damaged bytes: plenty of
// valid multi-byte characters and at most one bad byte in ten of them. A file
// in an old single-byte code page has almost no valid multi-byte sequences.
func mostlyUTF8(data []byte) bool {
	multi, bad := 0, 0
	for i := 0; i < len(data); {
		r, size := utf8.DecodeRune(data[i:])
		switch {
		case r == utf8.RuneError && size <= 1:
			bad++
		case size > 1:
			multi++
		}
		i += size
	}
	return multi >= 10 && bad*10 <= multi
}

// mostlyCyrillic tells a Serbian subtitle saved in Windows-1251 (Cyrillic)
// from one saved in Windows-1250 (Latin). OpenSubtitles has one "sr" for both.
// In a Cyrillic file nearly every letter is a byte above 0x7F; in a Latin one
// only the few letters with a hook or a stroke (š, đ, č, ć, ž) are.
func mostlyCyrillic(data []byte) bool {
	high, ascii := 0, 0
	for _, b := range data {
		switch {
		case b >= 0x80:
			high++
		case b >= 'A' && b <= 'Z', b >= 'a' && b <= 'z':
			ascii++
		}
	}
	return high > ascii
}

// codePageFor is the Windows code page subtitles in a language are usually
// saved in.
// It is nil for a language with no known legacy page, whose text is then left
// as it came.
func codePageFor(language string) encoding.Encoding {
	var e encoding.Encoding
	lang := strings.ToLower(language)
	switch baseLanguage(lang) {
	case "zh":
		// Traditional Chinese (Taiwan, Hong Kong, Macau) is Big5; the rest GBK.
		if strings.ContainsAny(lang, "-_") && (strings.Contains(lang, "tw") || strings.Contains(lang, "hk") || strings.Contains(lang, "mo") || strings.Contains(lang, "hant")) {
			e = traditionalchinese.Big5
		} else {
			e = simplifiedchinese.GB18030
		}
	case "ja":
		e = japanese.ShiftJIS
	case "ko":
		e = korean.EUCKR
	case "th":
		e = charmap.Windows874
	case "ro", "pl", "cs", "sk", "hu", "hr", "sl", "sr", "bs", "sq":
		e = charmap.Windows1250
	case "ru", "uk", "bg", "be", "mk", "kk":
		e = charmap.Windows1251
	case "el":
		e = charmap.Windows1253
	case "tr", "az":
		e = charmap.Windows1254
	case "he":
		e = charmap.Windows1255
	case "ar", "fa", "ur":
		e = charmap.Windows1256
	case "et", "lv", "lt":
		e = charmap.Windows1257
	case "vi":
		e = charmap.Windows1258
	case "", "en", "fr", "de", "es", "pt", "it", "nl", "sv", "da", "no", "nb", "nn", "fi", "is", "ca", "gl", "eu", "af", "id", "ms", "sw", "ga", "cy", "la", "tl", "fil", "lb", "eo":
		e = charmap.Windows1252
	}
	return e
}

var (
	vttTiming = regexp.MustCompile(`^(?:(\d+):)?(\d{2}):(\d{2})\.(\d{3})\s*-->\s*(?:(\d+):)?(\d{2}):(\d{2})\.(\d{3})`)
	vttTag    = regexp.MustCompile(`</?(?:c|v|lang|ruby|rt)[^>]*>`)
)

// VTTToSRT converts WebVTT text to SRT: the header and notes go, cues are
// numbered, and the time stamps use a comma before the milliseconds.
func VTTToSRT(data []byte) []byte {
	text := strings.ReplaceAll(strings.ReplaceAll(string(data), "\r\n", "\n"), "\r", "\n")
	blocks := strings.Split(text, "\n\n")
	var out strings.Builder
	n := 0
	for _, b := range blocks {
		lines := strings.Split(strings.TrimSpace(b), "\n")
		at := -1
		for i, l := range lines {
			if vttTiming.MatchString(strings.TrimSpace(l)) {
				at = i
				break
			}
		}
		if at < 0 {
			continue // the WEBVTT header, a NOTE or a STYLE block
		}
		m := vttTiming.FindStringSubmatch(strings.TrimSpace(lines[at]))
		stamp := func(h, mi, s, ms string) string {
			if h == "" {
				h = "0"
			}
			return strings.Repeat("0", max(0, 2-len(h))) + h + ":" + mi + ":" + s + "," + ms
		}
		n++
		out.WriteString(strings.Join([]string{
			strconv.Itoa(n),
			stamp(m[1], m[2], m[3], m[4]) + " --> " + stamp(m[5], m[6], m[7], m[8]),
			vttTag.ReplaceAllString(strings.Join(lines[at+1:], "\n"), ""),
		}, "\n"))
		out.WriteString("\n\n")
	}
	return []byte(out.String())
}
