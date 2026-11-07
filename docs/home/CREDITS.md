# Credits for the home fork

- **MangaDex** (https://mangadex.org): chapters, when you switch that on, come through its public API
  (https://api.mangadex.org/docs/), free and run by its volunteers. Thanks also to the scanlation groups, who translated
  them: each is named on the Chapters tab and in every file's ComicInfo.xml (`internal/manga/mangadex.go`).
- **Mangarr** (https://github.com/Kha-kis/manga-arr, AGPL-3.0): the way Manga reads release names (ranges before single
  volumes, numbers as decimals so half volumes work, a language reject list, aliases from AniList) follows its ideas.
  **Tranga** (https://github.com/C9Glax/tranga, GPL-3.0): keeping a series' volumes separate from where they come from,
  and, by its absence, saying why a series is empty. Both written anew in Go (`internal/manga`,
  `internal/api/home_manga*.go`); no code from either is copied.
- **InfiniDysk** (https://github.com/infinidysk/infinidysk, MIT, © 2026 hoivikaj), the successor of **nzbdav**
  (https://github.com/nzbdav-dev/nzbdav, MIT, © 2025 nzbdav-dev): the way Usherr's own streaming plays past a few
  missing articles follows its gap-fill limits (2 in a row, 5 in all, 1% of the file), which containers can take it,
  and keeping "couldn't ask" apart from "missing" (`internal/download/stream_holes.go`). The read path's article
  timeouts and read-ahead that grows on steady reads follow what it does too (`internal/download/stream_read.go`). The
  logic was rewritten in Go from its C#; each adapted file says so.
- **koreader-sync-server** and **KOReader** (https://github.com/koreader, AGPL-3.0): Usherr's KOSync server
  (`internal/api/kosync_api.go`) answers as the reference server does, and the file fingerprint and chapter
  places (`internal/kosync`) follow KOReader. Written for Usherr in Go; no code is copied. The test values
  for the fingerprint come from **kosync-conformance** (https://github.com/pid1/kosync-conformance, spec and
  vectors CC0).
- **Calibre-Web-Automated** (https://github.com/crocodilestick/Calibre-Web-Automated, GPL-3.0): the list of
  faults that the Kindle fix mends (`internal/ebookconv/kindlefix.go`) follows what its EPUB fixer lists.
  No code is copied.

- **Pullbox** (https://github.com/pullboxapp/pullbox, GPL-3.0-or-later): the comics release checking and
  scoring, the review list for doubtful matches and the per-issue search history follow Pullbox's release
  validator, intervention queue and search log. Re-written in Go for this fork (`internal/comics/evaluate.go`,
  `internal/api/home_comic_review.go`); no Pullbox code is copied. GPL-3.0 work may be combined with this
  AGPL-3.0 project.
- **Pullbox** and **Kapowarr** (https://github.com/Casvt/Kapowarr, GPL-3.0): the direct-download support for comics
  (GetComics posts, their host links, FlareSolverr for Cloudflare checks, packs filling a whole run) follows what
  those apps do. Written for this fork in Go (`internal/directdl`, `internal/api/home_direct.go`,
  `internal/api/home_comic_pack.go`); the MEGA download follows MEGA's public file-link format.
- **Pullbox** and **Komga** (https://github.com/gotson/komga, MIT): the Comics tab's Continue, Want to Read and
  Read views, next issue, and the reader's directions and webtoon view follow what those apps offer. Written anew
  for this fork (`internal/comics/home_reading.go`, `internal/api/home_comic_reading.go`,
  `web/src/home/ComicWebtoon.tsx`); no code from either is copied.
- **Sportarr** (https://github.com/Sportarr/Sportarr, GPL-3.0): Usherr's Sports section works with a household's
  Sportarr through Sportarr's own API (`internal/api/home_sports_sportarr.go`); league, team and game data,
  artwork and downloads come from Sportarr and the metadata service it uses. No Sportarr code is copied.
  Usherr's own sports downloads (`internal/sports`, `internal/api/home_sports_get.go`) follow Sportarr's
  approach to sports releases (fight cards in parts, race weekends in sessions, date-based games, the
  TV-style League/Season/S2026E05 file layout), written anew in Go.
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
- **DroppedNeedle** (https://github.com/droppedneedle/DroppedNeedle, AGPL-3.0): the model for parts of Usherr's
  music section. Its Python logic was re-written in Go; each ported file names the DroppedNeedle file it follows.
  - Last.fm scrobbling (`internal/lastfm`, `internal/api/music_lastfm.go`): the signed calls, the web sign-in
    and sending each play to Last.fm and ListenBrainz side by side.
  - Tidying the music folder (`internal/musictidy`, `internal/api/music_tidy*.go`, migration 9110): the AcoustID
    lookup and its score limit, the preview, apply and undo flow with its journal, and the tag names per format.
  - Likes and play history (`internal/api/music_favorites.go`, migration 9091): its favourites and play history tables.
  - Converting music while streaming (`internal/api/music_transcode.go`): when to convert, the bitrate rules,
    the ffmpeg command lines and the limit on conversions at once.
  - The Subsonic / OpenSubsonic API for phone apps (`internal/subsonic`, `internal/api/subsonic*.go`): an
    app password instead of the account password (Usherr uses its existing e-reader password for this), the
    strict sign-in checks, the answer envelope, ids with a kind prefix, and the endpoint behaviour Symfonium
    and others expect.
  - Download checks (`internal/music/release_sanity.go`, `internal/music/files.go`): leaving out karaoke,
    tribute and live releases, releases far too small for the album, and files whose length doesn't fit the
    track (15 seconds or 10%).

- [Audiobookshelf](https://github.com/advplyr/audiobookshelf) (GPL-3.0): the rules for reading an audiobook's folder
  (`internal/books/scan_audio.go`): `{Narrator}`, `[ASIN]`, the series number forms, the nearest three folders as
  author, series and title, which side files count and which tags hold the narrator and series. They are re-written
  in Go from its `server/utils/scandir.js` and `server/scanner/`; no code of theirs is included.

- [stash-box](https://github.com/stashapp/stash-box) and [Stash](https://github.com/stashapp/stash) (AGPL-3.0):
  the NSFW 18+ module's fingerprint and favorites lookups (`internal/nsfw/identify.go`, `favorites.go`) use
  stash-box's public GraphQL API, and its OSHASH fingerprint follows the method those projects use. No code of
  theirs is included.
