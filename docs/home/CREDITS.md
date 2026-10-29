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
- **Sportarr** (https://github.com/Sportarr/Sportarr, GPL-3.0): Usherr's Sports section works with a household's
  Sportarr through Sportarr's own API (`internal/api/home_sports_sportarr.go`); league, team and game data,
  artwork and downloads come from Sportarr and the metadata service it uses. No Sportarr code is copied.
- **Kokoro** (https://github.com/hexgrad/kokoro, Apache-2.0, the voice model) and **Kokoro-FastAPI**
  (https://github.com/remsky/Kokoro-FastAPI, Apache-2.0, the server around it): the everyday voices of
  Read-to-Me. Usherr talks to it through its OpenAI-style speech API (`internal/api/home_readtome_tts.go`);
  no code is copied.
- **Chatterbox** (https://github.com/resemble-ai/chatterbox, MIT, Resemble AI) and the **Chatterbox TTS API**
  (https://github.com/travisvn/chatterbox-tts-api, AGPL-3.0, travisvn): the expressive and copied voices of
  Read-to-Me, run as a separate container the household builds from that project's source and talked to
  through its API; no code is copied.
- **TheSportsDB** (https://www.thesportsdb.com): league, team and game data and artwork for Usherr's own sports
  engine (`internal/api/home_sports_tsdb.go`), through its free test key or the household's own paid key.
