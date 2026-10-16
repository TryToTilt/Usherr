# Usherr changelog

Usherr's own releases. Each one is built on a Mediarium release by rdborg,
whose changes are in [CHANGELOG.md](CHANGELOG.md).

## [1.0.2] - 2026-10-16

Built on **Mediarium 3.1.0**.

**New**
- **In theaters**: an "In theaters now" row on the Dashboard and at the top of Library → Movies, cinema openings on the calendar, and on a film's page links to its showtimes at your theater and everywhere else it's playing. Set your ZIP and theater under *Settings → Extras → In theaters*.
- **Marquee**: a lobby display for a tablet next to the TV, at `/marquee`. It shows what's playing now (from Jellyfin or Emby, and from StreamNZB for Nuvio or Stremio) with a progress bar, featured posters with their trailers, what just arrived, what's in theaters and the week ahead, in large print. Tall scenes scroll slowly. *Settings → Extras → Marquee* sets the order and length of each scene, the sign's own words, the top bar, sizes, trailer sound, quiet hours and colours.
- **Anime folder**: *Settings → Library → Folders* has an anime folder next to the TV folder (or set `ANIME_DIR`). New anime shows go there, and the Library's Anime tab finds them by it.
- Entertainment news starts with three headlines; the heading opens the rest.

**Fixed**
- The Comics settings tab no longer crashes on a new install.
- Comics and films cut into numbered pieces (`.cbr.000`, `.mkv.001`...) are joined back together after downloading.
- Comic releases in another language (German, French, Italian, Spanish, Dutch, Portuguese) are turned down.
- The calendar labels comics as comics and anime as anime, not as movies.

## [1.0.1] - 2026-10-15

Built on **Mediarium 3.1.0**.

- **Colours**: pick Usherr's colours under *Settings → General*: Wine, Cherry, Amber, Emerald, Teal, Blue, Violet, Rose or Graphite, in dark or light. The icon and the browser tab's icon take the colour too. Each person picks their own; it's kept in their browser.
- Skipping a release that's already downloaded no longer leaves the title showing "downloading" for a moment.
- The AI's answers (Ask, why a download failed) call the app Usherr.
- The logo no longer drifts among the shapes behind pages.

## [1.0.0] - 2026-10-15

The first release of Usherr, built on **Mediarium 3.1.0**.

- **For you**: a taste for each person from thumbs up and down and what they watch, with For you rows, a match on every title and why; **Rate 20** to get started.
- **You**: your taste, your votes, and taste in common with the household when you share it.
- **Diary** filled in from Jellyfin, with stars and notes, and imports from Letterboxd and Trakt; **Wrapped**, your year in watching.
- **Tonight**, **Pick for the Room** and **Roulette**; **Discover recipes**, ready-made rows, your services, a family filter and following people.
- **Streaming** from Usenet through StreamNZB, a streamed library next to your files, and **Streams for you**.
- **Live TV** channels from your library; **Comics** from ComicVine with a page-turning reader.
- A news feed, critic scores, complete your library, schedules and quiet hours, a guided setup and an in-app guide.
- **NSFW 18+** stays locked unless the server allows it (`USHERR_ALLOW_ADULT=1`), then needs an adults-only confirmation.
- Usherr's own name, logo and cinema look, its own updates and signing key, and nothing sent to anyone: no usage numbers, no private reports.
