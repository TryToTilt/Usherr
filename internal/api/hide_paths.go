package api

import (
	"net/http"
	"path"
	"strings"
)

// memberText is a line of text for the person asking: administrators get it as
// it is, everyone else without the server's folder paths.
func memberText(r *http.Request, msg string) string {
	if isAdminRequest(r) {
		return msg
	}
	return hideLibraryPaths(msg)
}

// hideLibraryPaths replaces every absolute file path in an activity line with
// its last part (the file or folder name), for people who are not
// administrators: where files lie on the server is not theirs to see. A path
// runs to the next ": " or to the end of the line, because folder names hold
// spaces. Lines without a path come back as they were.
func hideLibraryPaths(msg string) string {
	var out strings.Builder
	rest := msg
	for {
		i := pathStartIn(rest)
		if i < 0 {
			out.WriteString(rest)
			return out.String()
		}
		out.WriteString(rest[:i])
		p := rest[i:]
		end := len(p)
		if j := strings.Index(p, ": "); j >= 0 {
			end = j
		}
		full := p[:end]
		tail := ""
		// Keep a closing full stop that belongs to the sentence, not the name.
		if strings.HasSuffix(full, ".") && !strings.HasSuffix(full, "..") {
			full, tail = full[:len(full)-1], "."
		}
		out.WriteString(lastPart(full) + tail)
		rest = p[end:]
	}
}

// pathStartIn finds where an absolute path (/a/b or C:\a\b) begins in s, or -1.
func pathStartIn(s string) int {
	for i := 0; i < len(s); i++ {
		if i > 0 && !strings.ContainsRune(" (\"'", rune(s[i-1])) {
			continue
		}
		switch {
		case s[i] == '/' && i+1 < len(s) && s[i+1] != ' ' && s[i+1] != '/' && strings.Contains(s[i+1:], "/"):
			return i
		case i+2 < len(s) && s[i+1] == ':' && (s[i+2] == '\\' || s[i+2] == '/') &&
			(s[i] >= 'A' && s[i] <= 'Z' || s[i] >= 'a' && s[i] <= 'z'):
			return i
		}
	}
	return -1
}

func lastPart(p string) string {
	p = strings.ReplaceAll(p, "\\", "/")
	p = strings.TrimRight(p, "/")
	if b := path.Base(p); b != "." && b != "/" {
		return b
	}
	return ""
}
