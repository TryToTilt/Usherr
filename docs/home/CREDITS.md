# Credits for the home fork

- **Pullbox** (https://github.com/pullboxapp/pullbox, GPL-3.0-or-later): the comics release checking and
  scoring, the review list for doubtful matches and the per-issue search history follow Pullbox's release
  validator, intervention queue and search log. Re-written in Go for this fork (`internal/comics/evaluate.go`,
  `internal/api/home_comic_review.go`); no Pullbox code is copied. GPL-3.0 work may be combined with this
  AGPL-3.0 project.
- **Pullbox** and **Kapowarr** (https://github.com/Casvt/Kapowarr, GPL-3.0): the direct-download support for comics
  (GetComics posts, their host links, FlareSolverr for Cloudflare checks, packs filling a whole run) follows what
  those apps do. Written for this fork in Go (`internal/directdl`, `internal/api/home_direct.go`,
  `internal/api/home_comic_pack.go`); the MEGA download follows MEGA's public file-link format.
