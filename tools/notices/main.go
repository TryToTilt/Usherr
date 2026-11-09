// Command notices writes THIRD_PARTY_NOTICES.md: the licence of every piece of
// other people's work that ends up in Mediarium. That is the Go modules
// compiled into the program, the npm packages bundled into the web app, the
// fonts it ships, and the tools the Docker images install next to it.
//
//	go run ./tools/notices          rewrite THIRD_PARTY_NOTICES.md
//	go run ./tools/notices -check   exit 1 if the file on disk is out of date (used by CI)
//
// It needs the Go module cache (go mod download) and web/node_modules (npm ci),
// because the licence texts are read from there.
package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

type component struct {
	Name    string
	Version string
	Licence string
	Texts   []string // the licence and notice files, as shipped
}

func main() {
	check := flag.Bool("check", false, "fail if THIRD_PARTY_NOTICES.md differs from what is on disk")
	root := flag.String("root", ".", "repository root")
	flag.Parse()

	goMods, err := goModules(*root)
	must(err)
	npm, err := npmPackages(*root)
	must(err)
	fonts, err := fontFiles(*root)
	must(err)
	out := render(goMods, npm, fonts)

	path := filepath.Join(*root, "THIRD_PARTY_NOTICES.md")
	if *check {
		old, _ := os.ReadFile(path)
		if !bytes.Equal(bytes.ReplaceAll(old, []byte("\r\n"), []byte("\n")), out) {
			fmt.Fprintln(os.Stderr, "THIRD_PARTY_NOTICES.md is out of date: run go run ./tools/notices")
			os.Exit(1)
		}
		return
	}
	must(os.WriteFile(path, out, 0o644))
	fmt.Printf("wrote THIRD_PARTY_NOTICES.md (%d Go modules, %d npm packages, %d fonts)\n", len(goMods), len(npm), len(fonts))
}

// goModules lists the modules whose code is compiled into the program (not
// the ones only tests use), with the licence files from the module cache.
func goModules(root string) ([]component, error) {
	cmd := exec.Command("go", "list", "-deps", "-f", `{{with .Module}}{{if not .Main}}{{.Path}}|{{.Version}}|{{.Dir}}{{end}}{{end}}`, "./cmd/app")
	cmd.Dir = root
	cmd.Stderr = os.Stderr
	raw, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("go list: %w", err)
	}
	seen := map[string]bool{}
	var out []component
	for _, line := range strings.Split(strings.TrimSpace(string(raw)), "\n") {
		parts := strings.Split(line, "|")
		if len(parts) != 3 || seen[parts[0]] {
			continue
		}
		seen[parts[0]] = true
		texts, err := licenceTexts(parts[2])
		if err != nil {
			return nil, fmt.Errorf("%s: %w", parts[0], err)
		}
		licence := ""
		if len(texts) > 0 {
			licence = guessLicence(texts[0])
		} else if k, ok := knownLicences[parts[0]]; ok {
			licence, texts = k.licence, []string{k.note}
		} else {
			return nil, fmt.Errorf("%s has no licence file in %s: add it to knownLicences", parts[0], parts[2])
		}
		out = append(out, component{Name: parts[0], Version: parts[1], Licence: licence, Texts: texts})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

// knownLicences covers modules whose published version carries no licence
// file, checked by hand against the project's repository.
var knownLicences = map[string]struct{ licence, note string }{
	"github.com/go-llsqlite/adapter":     {"MPL-2.0", "The version used was published before the project added its LICENSE file. The project is licensed under the Mozilla Public License 2.0: https://github.com/go-llsqlite/adapter/blob/master/LICENSE. Its source is at https://github.com/go-llsqlite/adapter."},
	"github.com/pgaskin/kepubify/_/html": {"BSD-3-Clause", "A copy of golang.org/x/net/html, made by the kepubify project. The Go authors' licence (BSD-3-Clause) is the file golang.org/x/net/LICENSE inside the module. Source: https://github.com/pgaskin/kepubify/tree/_/html."},
}

// npmPackages lists the packages bundled into the web app: everything in the
// lock file that isn't only a development tool.
func npmPackages(root string) ([]component, error) {
	data, err := os.ReadFile(filepath.Join(root, "web", "package-lock.json"))
	if err != nil {
		return nil, err
	}
	var lock struct {
		Packages map[string]struct {
			Version  string `json:"version"`
			License  string `json:"license"`
			Dev      bool   `json:"dev"`
			Optional bool   `json:"optional"`
		} `json:"packages"`
	}
	if err := json.Unmarshal(data, &lock); err != nil {
		return nil, fmt.Errorf("read package-lock.json: %w", err)
	}
	var out []component
	for path, p := range lock.Packages {
		if path == "" || p.Dev || !strings.HasPrefix(path, "node_modules/") {
			continue
		}
		name := path[strings.LastIndex(path, "node_modules/")+len("node_modules/"):]
		dir := filepath.Join(root, "web", filepath.FromSlash(path))
		if _, err := os.Stat(dir); err != nil {
			if p.Optional {
				continue // a platform-specific package not installed here
			}
			return nil, fmt.Errorf("%s is missing from web/node_modules: run npm ci first", name)
		}
		texts, err := licenceTexts(dir)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", name, err)
		}
		licence := p.License
		if licence == "" && len(texts) > 0 {
			licence = guessLicence(texts[0])
		}
		if len(texts) == 0 {
			texts = []string{"Licensed under " + licence + " (the package ships no licence file)."}
		}
		out = append(out, component{Name: name, Version: p.Version, Licence: licence, Texts: texts})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Name != out[j].Name {
			return out[i].Name < out[j].Name
		}
		return out[i].Version < out[j].Version // two versions of one package
	})
	return out, nil
}

var fontLicence = regexp.MustCompile(`^OFL-(.+)\.txt$`)

func fontFiles(root string) ([]component, error) {
	var out []component
	// Usherr: Re-Posterr's fonts (internal/api/artfonts) too.
	for _, dir := range []string{filepath.Join(root, "web", "public", "fonts"), filepath.Join(root, "internal", "api", "artfonts")} {
		entries, err := os.ReadDir(dir)
		if err != nil {
			return nil, err
		}
		for _, e := range entries {
			m := fontLicence.FindStringSubmatch(e.Name())
			if m == nil {
				continue
			}
			text, err := os.ReadFile(filepath.Join(dir, e.Name()))
			if err != nil {
				return nil, err
			}
			out = append(out, component{Name: m[1] + " font", Licence: "OFL-1.1", Texts: []string{clean(text)}})
		}
	}
	return out, nil
}

var licenceFile = regexp.MustCompile(`(?i)^(licen[cs]e|copying|notice|unlicense)([._-].*)?$`)

func licenceTexts(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var names []string
	for _, e := range entries {
		if !e.IsDir() && licenceFile.MatchString(e.Name()) {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)
	var texts []string
	for _, n := range names {
		b, err := os.ReadFile(filepath.Join(dir, n))
		if err != nil {
			return nil, err
		}
		if t := clean(b); t != "" {
			texts = append(texts, t)
		}
	}
	return texts, nil
}

func clean(b []byte) string {
	s := strings.ReplaceAll(string(b), "\r\n", "\n")
	return strings.TrimSpace(strings.TrimPrefix(s, "\uFEFF"))
}

func guessLicence(text string) string {
	t := strings.ToLower(text)
	switch {
	case strings.Contains(t, "apache license") && strings.Contains(t, "version 2.0"):
		return "Apache-2.0"
	case strings.Contains(t, "mozilla public license") && strings.Contains(t, "2.0"):
		return "MPL-2.0"
	case strings.Contains(t, "gnu lesser general public license"):
		return "LGPL"
	case strings.Contains(t, "gnu general public license"):
		return "GPL"
	case strings.Contains(t, "permission is hereby granted, free of charge"):
		return "MIT"
	case strings.Contains(t, "permission to use, copy, modify, and/or distribute"), strings.Contains(t, "permission to use, copy, modify, and distribute"):
		return "ISC"
	case strings.Contains(t, "neither the name") || strings.Contains(t, "names of its contributors"):
		return "BSD-3-Clause"
	case strings.Contains(t, "redistributions in binary form"):
		return "BSD-2-Clause"
	case strings.Contains(t, "this is free and unencumbered software"):
		return "Unlicense"
	}
	return "see the text below"
}

// imageTools are installed into the Docker images from the distribution's
// own packages, not built by us. Their sources are the distributions'.
const imageTools = `## Tools in the Docker images

Both images are built on Debian and install these from Debian packages, unchanged, except where the table says otherwise. Their licence files are in the image under ` + "`/usr/share/doc/<package>/copyright`" + `. The source of each Debian package is available from https://sources.debian.org.

| Tool | What Usherr uses it for | Licence |
|---|---|---|
| par2cmdline | checking and repairing Usenet downloads | GPL-2.0-or-later |
| p7zip | unpacking .7z archives | LGPL-2.1-or-later, with the unRAR restriction for its RAR code |
| tini (standard image), dumb-init (` + "`-full`" + `) | process 1 in the container | MIT |
| setpriv (util-linux) | dropping to your PUID/PGID | GPL-2.0-or-later |
| ca-certificates | trusting https sites | MPL-2.0 |
| tzdata | time zones | public domain |
| jellyfin-ffmpeg (standard image; from Jellyfin's own Debian repository, https://github.com/jellyfin/jellyfin-ffmpeg) | TV channels, pre-shows, sound and video checks, encoding on the graphics card | GPL-3.0-or-later |
| adb (android-tools) | starting playback on an Android TV | Apache-2.0 |
| DejaVu and WenQuanYi Zen Hei fonts | text on posters, slates and pre-show cards | Bitstream Vera licence; GPL-2.0 with font exception |
| yt-dlp and yt-dlp-ejs (standard image; from PyPI) | the pre-show's coming-soon trailers | Unlicense |
| Deno (standard image; from the official denoland/deno image) | running yt-dlp's YouTube helper | MIT |
| Python 3 (standard image) | running yt-dlp | PSF-2.0 |

The ` + "`-full`" + ` image is built on the official FlareSolverr image (MIT), which brings Python, Chromium and their own licences: see https://github.com/FlareSolverr/FlareSolverr.
`

// Usherr: designs the look follows. No code, logo, name or brand colours are
// copied, so nothing ships from them; the design language is credited here.
const designs = `## Designs

| Design | What Usherr uses it for | Licence |
|---|---|---|
| [Clickarr's design language](https://github.com/thegregstengel/clickarr/blob/main/docs/design/design-language.md) | the Broadcast look: depth by lightness, the corner sizes, one focus colour, the remote's focus rules | MIT |

Broadcast follows Clickarr's design language, adapted. Clickarr's name, logo and brand gradient are not used.
`

// dataSources are services whose data the program shows. They are not code, so
// they bring no files into the build; the licence of the data is credited here.
const dataSources = `## Data from other services

| Service | What Mediarium uses it for | Licence |
|---|---|---|
| [TVmaze](https://www.tvmaze.com) | the time episodes air, shown on the calendar | CC BY-SA 4.0 (https://creativecommons.org/licenses/by-sa/4.0/) |

Air times from TVmaze. Only a show's TheTVDB or IMDb id, or its name and year, is sent to it, and it can be switched off under Settings > Metadata.

Chapters from [MangaDex](https://mangadex.org), when you switch that on in Settings > Manga, under its [acceptable use policy](https://api.mangadex.org/docs/): MangaDex and the scanlation groups are credited in the app and in each file, and it's not for ads or paid access. The chapters belong to their authors and translators. Most aren't licensed.
`

func render(goMods, npm, fonts []component) []byte {
	var b strings.Builder
	b.WriteString("# Third-party notices\n\n")
	b.WriteString("Mediarium is licensed under the GNU Affero General Public License v3.0 (see LICENSE). It is built on other people's work, listed here with their licences. This file is generated by `go run ./tools/notices`; don't edit it by hand.\n\n")
	b.WriteString("## Contents\n\n")
	fmt.Fprintf(&b, "- [Go modules compiled into the program](#go-modules) (%d)\n", len(goMods))
	fmt.Fprintf(&b, "- [Packages bundled into the web app](#web-app-packages) (%d)\n", len(npm))
	fmt.Fprintf(&b, "- [Fonts](#fonts) (%d)\n", len(fonts))
	b.WriteString("- [Tools in the Docker images](#tools-in-the-docker-images)\n")
	b.WriteString("- [Data from other services](#data-from-other-services)\n")
	b.WriteString("- [Designs](#designs)\n\n") // Usherr
	b.WriteString(imageTools)
	b.WriteString("\n" + dataSources)
	b.WriteString("\n" + designs) // Usherr
	section := func(title, anchor string, list []component) {
		fmt.Fprintf(&b, "\n## %s\n\n<a id=\"%s\"></a>\n\n| Name | Version | Licence |\n|---|---|---|\n", title, anchor)
		for _, c := range list {
			fmt.Fprintf(&b, "| %s | %s | %s |\n", c.Name, orDash(c.Version), c.Licence)
		}
		for _, c := range list {
			fmt.Fprintf(&b, "\n### %s", c.Name)
			if c.Version != "" {
				fmt.Fprintf(&b, " %s", c.Version)
			}
			b.WriteString("\n\n")
			for _, t := range c.Texts {
				b.WriteString("```text\n" + strings.ReplaceAll(t, "```", "` ` `") + "\n```\n\n")
			}
		}
	}
	section("Go modules", "go-modules", goMods)
	section("Web app packages", "web-app-packages", npm)
	section("Fonts", "fonts", fonts)
	return []byte(strings.TrimRight(b.String(), "\n") + "\n")
}

func orDash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}

func must(err error) {
	if err != nil {
		fmt.Fprintln(os.Stderr, "notices:", err)
		os.Exit(1)
	}
}
