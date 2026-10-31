# Usherr changelog

Usherr's own releases. Each one is built on a Mediarium release by rdborg,
whose changes are in [CHANGELOG.md](CHANGELOG.md).

## [Unreleased]

## [2.16.0] - 2026-10-31

### Added

- **Show Me How**, looking after the library: swap a film for a better copy, get subtitles, move a film to another folder, make a Kids shelf (tags as Jellyfin collections), why didn't my download work (Activity, a title's What happened, Wanted), bring in the films and shows you already have, and restyle a poster (Re-Posterr). Walkthroughs about a film you have search your library first, and say so when a film isn't in it, when subtitles are off, or when there's only one library folder.

## [2.15.0] - 2026-10-31

### Added

- **Show Me How**, listening and reading: play some music (and the player: queue, lyrics, its own window), make a playlist, bring in the music you already have, get a book, listen to an audiobook and read an ebook in Usherr Books (how to open it, the player and reader, installing it as an app, e-readers).

## [2.14.0] - 2026-10-31

### Added

- **Show Me How**, movie night and TV: make a Marquee screen (a code to scan on the tablet), steer it from your phone, watch Live TV, make a Live TV channel (channel packs, Build with AI or your own), follow a sports team (it says when Sports isn't set up), and set up Play on TV. The coach card now unfolds a folded settings card, and a page's tab in its address (?tab=) counts as being on it.

## [2.13.0] - 2026-10-31

### Added

- **Show Me How**, finding things: browse Discover (what to show, Filters, Hide what I have, the family filter, recipes), get a TV show (all episodes, only new ones, or ones you pick), follow an actor or director, see what's coming (Calendar, Wanted, Following), fill the gaps in your library (Complete your library), and ask for something in your own words (the Ask bar, What's it like?, Catch me up).

## [2.12.0] - 2026-10-31

### Added

- **Show Me How**, the everyday ones: find something for tonight, let Usherr pick (Roulette), watch a film now or keep it (Stream or Add to library, only offered when streaming is set up), teach Usherr your taste (votes and Rate 20), keep a list of what to watch next (your stack), your diary and your year (Wrapped), add a family member (roles, and requests for children), and Usherr on your phone (a code to scan, then Add to Home Screen; Tailscale for away from home). The coach card opens the side menu by itself when a step is about something in it.

## [2.11.0] - 2026-10-31

### Added

- **Show Me How**: a button in the top bar opens a menu of things you can do, grouped (Watch, Movie night, Listen, Read, My library, Setup) with a search. Each one walks you through it on the real screens, in **Simple** (a few questions, then Usherr presses the buttons for you while you watch; **Let me do it** hands over) or **Advanced** (every option, explained). A coach card lights up each button and never blocks the page; choices come with a decision card (the options, their good and bad sides, what we recommend, and whether you can change it later), and keys are tested as you paste them. A walkthrough can be paused and carried on later. The first ones: book a showtime, set up a pre-show, read a comic (panels or page turn), read a story arc in order, this week's new comics, the comics Review tab, set up comics, turn an ebook into an audiobook, and complete a film series.
- A Start page (https://trytotilt.github.io/Usherr/start/): six steps on Windows from nothing to Usherr and Jellyfin running, each with what you'll see, how to tell it worked and the usual fixes. It writes your compose.yaml from a few answers, in the browser; nothing is sent anywhere.

## [2.10.0] - 2026-10-30

Built on **Mediarium 3.9.0** (everything in Mediarium 3.8.1 to 3.9.0 is in, see [CHANGELOG.md](CHANGELOG.md)): release lists past 60 results, download speed and what is left per client in Activity, extra indexer categories, a VPN test, why a torrent failed, umlaut spellings in searches, and security updates.

### Changed

- A show named in another language (Mediarium 3.9.0's folder language) keeps its existing folder, alongside Usherr's own rule that new episodes go into the show's and the season's existing folders.

## [2.9.7] - 2026-10-30

### Changed

- Pre-show clips: Usherr uses `/data/preshow` by itself when a folder is mapped there (as for trailers), so clips can be dropped in from the computer; a new clips folder starts with the usual categories (Bumpers, Policy, Commercials, Sound, Intermission, Halloween, Christmas), and clips in the old folder are copied over. The Pre-show page says when the folder is out of sight in Usherr's own storage, and how to map one.

## [2.9.6] - 2026-10-30

### Changed

- The top bar's Showtime button is a rounded rectangle in every look (Neu made it an oval).

## [2.9.5] - 2026-10-29

### Added

- Showtime from any page: a Showtime button in the top bar counts down to the next booking (its time, then "Pre-show starts 12:34", "Show starting 07:48", "Now showing"; on a phone the countdown alone) and always opens the Showtime screen. Showtime in the left menu, a line on the Dashboard, Book a showtime on film cards (Library, Streams, Discover), and Book a film on the Showtime page.

## [2.9.4] - 2026-10-29

### Fixed

- Showtime: a film that isn't in the library has its stream found during the pre-show (five minutes before at the latest), trying the next release when one won't start, so it plays at showtime. While it starts, the Showtime page says so ("Starting the film: finding its stream") instead of "No showtime here yet", and if it can't, Try again.
- Showtime plays a film that's in Streams (the streamed library) from its saved release, as its Play button does, instead of looking for a new stream.

## [2.9.3] - 2026-10-29

### Changed

- Change the style, from a title's Re-Posterr, opens the style page trying it on that title (first in Try it on), not a random one.

### Fixed

- A title's Re-Posterr window no longer breaks the page when fanart.tv doesn't answer: fanart.tv and TheTVDB get eight seconds, one that doesn't answer is left out for five minutes, and the window opens with TMDB's pictures.

## [2.9.2] - 2026-10-29

### Added

- Re-Posterr, more of Posterizarr's options: an outline and a shadow on the words, up to 1-4 lines, your own wording for season posters and title cards, collections without "Collection", and titles in other alphabets drawn in a font that has their letters (WenQuanYi Zen Hei for Japanese, Korean and Chinese; DejaVu for the rest). Overlays: your own PNGs laid over the picture, by the copy's resolution too (a 4K badge on the 4K copy). A library's own poster style. Backgrounds with the title on, by hand or in the nightly look. TheTVDB as a source for shows (with a key). Your own picture and your own words on one title. Styles saved as a file and loaded back. The full Posterizarr app connected, able to take over the posters.

## [2.9.1] - 2026-10-29

### Changed

- A poster Re-Posterr puts on a title in Jellyfin (picked or made in your style) shows on Usherr's own pages too: the title page, rows, searches and the rest. Putting the old one back brings TMDB's back in Usherr as well.

## [2.9.0] - 2026-10-29

### Added

- Re-Posterr makes posters in your style (after Posterizarr): art without words with the title in a font and layout of your choosing (Fade, Band, Logo, Centre or Framed to start from; Bebas Neue, Anton, Oswald, Playfair Display or Usherr's own font; place, size, colour, capitals; a fade or band behind; a border), or the title's logo. Posters, season posters ("Season 2") and episode title cards (a still with the episode's name and number). Designed under Your style on the Re-Posterr page, tried live on any title; used on one title (the Re-Posterr button's Your style tab) or by the nightly look (asked first, or by itself), and put back like any change. A new style is offered again. Kept as a versioned recipe of layers, so more of Posterizarr's options can be added.

## [2.8.6] - 2026-10-29

### Changed

- Re-Posterr opens a title quicker: the titles its nightly look keeps say which Jellyfin items it is (Jellyfin's whole list is read only for a title not among them), and fanart.tv's answers are kept half an hour, so choosing a picture doesn't ask again.

## [2.8.5] - 2026-10-29

### Fixed

- Re-Posterr can put a picture on in Jellyfin 12: the upload carries Usherr's key the way Jellyfin wants it (it answered 401 before).

## [2.8.4] - 2026-10-29

### Fixed

- Re-Posterr's fanart.tv pictures show in the side-by-side: Usherr fetches fanart.tv's small copies itself (making one from the picture when fanart.tv has none), and a picture that won't load at all is left out instead of shown as an empty box.
- A title's page on an Android phone no longer shows a stale second copy of its facts (year, runtime, director, writers) while the trailer plays behind them: the header's lines don't keep a layer of their own once they've risen into place.

## [2.8.3] - 2026-10-29

### Fixed

- The Re-Posterr button is on a streamed film's or show's page too (one not in the library), since Jellyfin has it.

## [2.8.2] - 2026-10-29

### Added

- The setup wizard's Extras step (and Settings → Extras) has a Re-Posterr card: what it does, ask first or on its own, turning on its nightly look, the fanart.tv key with Test it, and its page.

## [2.8.1] - 2026-10-29

### Changed

- The button on a film's or show's page that picks its poster, background and logo is called Re-Posterr, like its page.

## [2.8.0] - 2026-10-29

### Added

- Re-Posterr, Usherr's artwork picker (after Posterizarr): it picks the poster, background and logo for each film, show and collection in Jellyfin from TMDB and fanart.tv (your language first, backgrounds without words, big enough, the best liked) and puts them on every copy, at the size a TV tile wants. Ask me first (the default: picks wait beside what Jellyfin has, to use or skip), fill in what's missing, or also replace pictures that are too small. At night, a title every few seconds, never while someone is watching on Jellyfin and not when its metadata disk is low; a booked Showtime's film straight away. Settings → Re-Posterr, a page of its own.
- Change artwork on a film's or show's page: every candidate side by side; a picture chosen by hand goes on at once and is kept. Every picture replaced is kept first, so Put the old one back returns it.

## [2.7.2] - 2026-10-29

### Added

- A guide page for the pre-show (themes, Only when..., schedules, clips, coming-soon trailers, the Jellyfin app), in the in-app guide too, and the guide brought up to date: picking up where you left off, Usherr's look (Neu, main colours, the menu drawer), Read-to-Me and Sports in the README.
- The setup wizard's Extras step has a Pre-show card, and the Setup checklist shows the pre-show in the Jellyfin app, coming-soon trailers, Sports and Read-to-Me.
- Tooltips on the pre-show page's controls: the theme editor, schedules, clips, trailers and the Jellyfin app.

## [2.7.1] - 2026-10-29

### Changed

- The menu goes away with a click or touch anywhere outside it, on a wide screen too.

## [2.7.0] - 2026-10-29

### Added

- Coming-soon trailers downloaded ahead for the pre-show (after NeXroll's NeX-Up): the trailers of films you want, your shows' new seasons and the most talked-about films coming to and showing in cinemas, fetched with yt-dlp within 60 days of the release (4K, or the best each trailer has), every few hours a few at a time, and removed 14 days after the release or once the film is in the library, under a 10 GB cap. They play at once, in the Jellyfin app too; the pre-show takes the household's own and upcoming ones first. Pre-show page → Coming-soon trailers: what's ready and waiting, Watch, Keep, Remove, Look now, and when they come and go.
- New pre-show cards: Coming soon before each trailer (its poster and when it comes), a coming-soon reel of posters, an announcement in your own words, and a QR code for Usherr; drawn live and, for the Jellyfin app, as short videos.
- The image has yt-dlp, its YouTube helper and Deno.

## [2.6.5] - 2026-10-28

### Changed

- The pre-show plays full screen (asked for at the start, kept for the film; a Full screen button when the browser wants a click first, as for a Showtime that starts by itself).
- YouTube trailers play without captions, in the pre-show and on the Marquee (the pre-show also turns off captions a trailer forces on).

## [2.6.4] - 2026-10-28

### Fixed

- A pre-show clip still arriving can't play cut short: uploads are written under a hidden name and named once whole, a file copied into the folder waits until it has settled (20 seconds unchanged), and the Jellyfin pre-show leaves out a clip whose file is still being written.

## [2.6.3] - 2026-10-28

### Added

- The film gets ready while the Jellyfin pre-show plays: Usherr reads its start and end through Jellyfin, so a streamed copy (nzbdav's or a .strm) is fetching before its turn.
- A slot with both sound and picture ticked can play when either matches ("Any ticked sound or picture is enough"), for files whose labels aren't all there.
- The theme editor's One clip: a slot that plays a chosen clip shows (and picks) it.

### Fixed

- The Jellyfin pre-show judges the sound and picture by the copy Jellyfin plays, not the library's own release of the film.

## [2.6.2] - 2026-10-28

### Added

- A pre-show theme's slot can play only in a holiday's season (Only when… → Season: chosen holidays, or any holiday), for clips like a Halloween "Presented by".

## [2.6.1] - 2026-10-28

### Added

- The pre-show in the Jellyfin app: Usherr answers the NeXroll Intros plugin (`/plugin/intros`, with its own key and signed links), so the same pre-show plays before films in every Jellyfin app: your clips, and the cards made into short videos in the Marquee's colours (welcome with the film's poster, the rating, silence your phones, the countdown, the feature presentation). YouTube trailers are left out there for now. The Marquee follows each piece as Jellyfin plays it.
- Pre-show page → In the Jellyfin app: Set it up adds the plugin's repository and installs it, Restart Jellyfin (only when nobody is playing) loads it, and Usherr gives the plugin its address and key. Before films (on), before episodes (off), and the theme.

## [2.6.0] - 2026-10-28

### Added

- The pre-show: Usherr's own, after NeXroll. Clips from a pre-show folder (one folder level is the category: Bumpers, Halloween, Christmas...) or uploaded, themes made of blocks (welcome card, a clip from a category, holiday clips, coming-soon trailers, the rating card, silence your phones, the countdown, the feature), each with "Only when..." conditions (genre, rating, sound and picture formats, movie or episode, time of day), and schedules (holidays built in, dates, weekdays, hours) that pick the theme. "Movie night" and "Quick start" are ready out of the box. Clips and trailers rotate least-lately-played first, and the plan fits a time budget.
- A booked Showtime runs its pre-show so the feature starts on the minute; each person can also have it before any film. Skip and Skip the pre-show are always there. The Marquee follows each piece ("Coming soon" with the poster, the rating, the countdown).
- Settings → Movie night → Pre-show: themes, schedules, clips, theater name and trailer length.

### Fixed

- The settings menu: the group of the page you're on stays open when clicked, the Movie night group (Marquee, Pre-show) is reachable from any page, and on a phone opening a group shows its pages instead of jumping to the first.

## [2.5.6] - 2026-10-28

### Added

- Everything picks up where you left off: movies, episodes, NSFW videos, streams and sports games open at the place you stopped, and comics at the page, kept for each person on the server (`home_positions`), so on any device. The player says "Picked up at …" with a Start over button, and Start over stays in its top bar. Finished (past 95%, or the last 45 seconds) or barely begun (under 15 seconds), it starts at the beginning. Live TV and the channels are live and never resumed.
- Start over in the ebook reader, the audiobook player and the Bookshelf's comic reader; a book or comic read to the end opens at the start again.

### Changed

- Music starts each song from the top: the queue kept from last time no longer picks up part-way through a track.

## [2.5.5] - 2026-10-28

### Added

- A main colour for the whole app, under the highlight colour (Settings → General): Midnight navy, Espresso, Forest, Plum, Oxblood, Ocean, Stone or OLED black, each with a dark and a light look, or the highlight's own tint as before. Kept per browser. Made by `tools/usherr/bases.py` into `usherr-bases.css`.

## [2.5.4] - 2026-10-28

**Changed**
- Posters first: the menu is a hamburger drawer on every screen. On a wider screen it stays open while you move between pages and slides away after 8 seconds with no mouse movement over it (or with the hamburger, or Escape).
- The filter bar of the Library, Discover, Upcoming, Wanted, Diary, Comics and NSFW pulls down from under the top bar; closed, a slim handle says what is showing. It stays open or closed as you left it.
- Rows by sits beside the sort (Music's Genre too), and the Comics kind pick is its own size.

## [2.5.3] - 2026-10-28

**Changed**
- Tonight's picks moved from the wide Dashboard button to the top bar, beside the dice: it opens in a window from any page and asks straight away.

**Fixed**
- Usherr Neu: the news ticker's headlines have room between them instead of running into each other.

## [2.5.2] - 2026-10-28

**Fixed**
- The pinned filter bar (Library, Discover, Upcoming, Wanted, Diary) and the Books header no longer leave a slit under the top bar that posters scroll through.

## [2.5.1] - 2026-10-28

**Changed**
- Usherr Neu: Comics and NSFW cards float in recessed slots and lift on hover, like every other library.

## [2.5.0] - 2026-10-28

**New**
- **Usherr Neu**: a soft, raised look of Usherr's own (light and dark, new icons and font, covers in recessed slots, floating tiles). Pick it in Settings › General › Look; Usherr Classic stays the default.
- **The Read-to-Me page**: making an audiobook moved from the pop-up to a full page, in colour-coded steps that fold, with hints on every part and a guide that walks new readers through it.
- A poster size slider in every library: Music, Ebooks, Audiobooks, Comics, Discover and NSFW (Find too).

**Changed**
- The library totals moved from the Dashboard to Statistics, which now shows every kind (anime, books, comics, sports too) with streams counted; each card opens its library.

## [2.4.0] - 2026-10-27

Built on **Mediarium 3.8.0** (everything in Mediarium 3.4.0 to 3.8.0 is in, see [CHANGELOG.md](CHANGELOG.md)).

**New**
- **Read-to-Me**: make an audiobook from an ebook with Kokoro or Chatterbox. Pick the voice, the speed and the chapters, listen while it is made, and get a notice when it is done. A full cast gives the dialogue its own voices. Presets, a pronunciation list, lines read again, and copied voices (a WAV put in the voices folder becomes a voice).
- **Sports**, stacked on Sportarr with Usherr's own TheSportsDB engine: leagues, teams and games for everyone, games streamed with your language first, live scores and a Dashboard card.
- **Marquee Sports**: scoreboard, tonight's games, score ticker, kick-off countdown, and the game being watched as a matchup card.
- From Mediarium: calendar times in your own time zone, too-short files turned away at import, custom season numbering, a Quality profile filter and sort in the Library, Ended and Continuing filters for shows, more Discover filters (original language, lowest rating, runtime), and much faster big libraries.

**Changed**
- The Library and Settings menus keep Usherr's way: open on their own pages, folded elsewhere, one at a time.

**Fixed**
- Household members can get their own Stremio and Nuvio add-on link: the card is on the **You** page too (Extras is for administrators).
- Each NZB is fetched from the indexer once.

## [2.3.0] - 2026-10-21

Built on **Mediarium 3.3.0**.

**New**
- **Skip intro**: a button in Usherr's player while an intro plays, from the file's chapters (Intro, Opening, OP...) or Jellyfin's Intro Skipper segments; when the credits are known, *Up next* comes up as they start.
- **Releases that play**: Usherr's streaming engine remembers which release groups and indexers open, fail to open, break part-way or get *Try another release*, and puts the ones that keep failing at the back of the list.
- The Marquee shows something that starts playing in Usherr's player within seconds, not at its next look.

## [2.2.0] - 2026-10-21

Built on **Mediarium 3.3.0**.

**New**
- **Showtimes at home**: book a film for a time from its page or your stack, with minutes of pre-show and where it plays (the Showtime page left open on the TV starts it by itself, the TV through Play on TV, or yourself in another app); "right after" another booking makes a double feature. The Marquee shows a countdown band and starts the pre-show on time by itself. The Showtime page lists the bookings, to move or cancel.
- **Up next**: near the end of an episode in Usherr's player, a card offers the next one and plays it after a 10-second countdown, from your file or streamed (only episodes that have aired; not on Live TV). *Play the next episode by itself* is kept on each device.

## [2.1.0] - 2026-10-21

Built on **Mediarium 3.3.0**.

**New**
- **Marquee badges for everything that plays**: the sound (Dolby Atmos, DTS:X, TrueHD, DTS-HD MA, Dolby Digital+... and 7.1, 5.1, Stereo), the picture's shape (2.39:1, 1.85:1, 16:9; measured inside black bars) and the cinema format: how it was shown and shot (IMAX, IMAX 70mm, Dolby Cinema, 70mm, 65mm or 35mm film, Digital, Black & white), from IMDb's technical specifications, with TMDB's release notes when IMDb has nothing; *Cinema formats from IMDb* turns IMDb off. What Usherr plays itself is looked at once, so a file marked HDR10 that is really Dolby Vision says so.
- **Trailers fill** on the Marquee: trailers on a portrait screen can be zoomed to a poster's size or to all the space there is, instead of a small letterboxed strip; the pre-show's trailers and your own videos too.
- **The Marquee shows what's playing, wherever it plays**: Usherr's own player in the browser (library files, Stream now, Live TV with its channel) and its Stremio and Nuvio add-on now count, next to Jellyfin, Emby and StreamNZB, with progress and the picture's badges. *Usherr itself* under *Now showing comes from* turns it off.

**Fixed**
- A Marquee screen keeps only the settings changed for it (a look picked on the remote, say) and follows the shared settings for everything else. Before, picking a look gave it a frozen copy of every setting, so later changes on *Look and scenes* seemed not to work. Screens that had a copy keep just what differed. *Look and scenes* lists what each screen keeps of its own, with a button to drop it.
- Security: built with Go 1.27.3, which fixes a flaw in Go's own certificate checking (GO-2026-6809).
- A long sign such as *Feature presentation* fits a portrait Marquee screen in the Cinema and Posterr looks, and the trailer and title under it stay centred instead of being pushed off to one side.
- A stream played from a title's page shows that title's poster and details on the Marquee; a stream started from a list of releases is looked up by its name.
- The Marquee in Home Assistant could be left showing a screen's old look after a new one was chosen there.

**New** (from Mediarium 3.3.0)
- **SABnzbd or NZBGet for Usenet**: *Settings → Downloading → Usenet → Use SABnzbd or NZBGet* sends Usenet releases to your own downloader instead of the built-in one (off unless switched on; streaming isn't affected).
- **What's inside your files**: codec, resolution, HDR10, HDR10+, Dolby Vision, every audio track (with Atmos) and subtitle track, read from the files' headers in the background and shown on a film's page and each episode. The Library's Filters gain video codec, HDR, resolution, audio and subtitle language and "no {language} audio", each a chip when on. The Marquee's badges use these details for library films.
- **Ask your library from an AI assistant or Home Assistant's voice assistant** (MCP, *Settings → Other apps*; off until switched on, administrator key only, asks before changing anything).
- **Quality order and groups** in a quality profile, like Radarr and Sonarr, with a TRaSH layout.
- **nzb360 and other Radarr/Sonarr phone apps**: release search, history, scores and more.
- Music: rescan an album folder, delete one file, search for missing tracks. Separate size limits for movies and TV, a switch for the built-in minimum size, up to 500 downloads at once, TRaSH folder name presets, settings in the search box, WebP covers.

**Fixed** (from Mediarium 3.3.0)
- Short TV episodes are no longer skipped as too small; a FlareSolverr check no longer times out while it solves; the search list's Score shows the whole score; Open Library searches with odd characters work.
- Everything else in Mediarium 3.2.17 to 3.3.0, listed in [CHANGELOG.md](CHANGELOG.md).

## [2.0.0] - 2026-10-20

Built on **Mediarium 3.2.16**.

**New**
- **A choice of streaming engine**: *Settings → Extras → Streaming engine* picks Usherr's own engine or StreamNZB for Stream now, the streamed library and streamed Live TV. The one chosen goes first; the other is the way out when it finds nothing. StreamNZB stays the default.
- Usherr's engine now serves **Stream now**: movies and episodes are searched on your indexers (daily shows by date), listed best first, and played straight from Usenet. A release that breaks part-way hands over to the next one in line, from where you were; **Try another release** does the same by hand.
- **Device qualities** (4K, 1080p, 720p): the biggest file and the words to leave out, for the TV and for a phone.
- **StreamNZB is optional**: a new install streams with Usherr's own engine and needs nothing else (the setup wizard's Extras step and the setup checklist count either engine as ready). An install that already has StreamNZB connected keeps it until you choose Usherr.
- **Import from StreamNZB**: on the Streaming engine card, copy StreamNZB's Usenet providers, indexers and size limits from its `config.json`. You see what is in it first, what Usherr already has is left alone, only what you tick is copied, and logins are never shown.
- **Real TV picks from the whole lineup**: instead of six network buttons, the dialog lists every channel at your zip code to tick (up to 40 at a time), with search and News, Sports, Movies, Family and Talk filters. *Channels from* chooses the stations on the air (every affiliate and subchannel: MeTV, Cozi, ION, Telemundo...) or a cable or satellite provider's lineup (ESPN, HGTV, TNT...). The main networks start ticked. Channels are built four at a time, and subchannels get their own numbers (4.2 is channel 42).
- **Stremio and Nuvio add-on**: *Settings → Extras → Stremio and Nuvio* gives each person a private link (with a QR code and an Install in Stremio button on an https address) to add once in each app. The apps then show *Usherr Movies*, *Usherr TV*, *Usherr Watchlist* (thumbs up) and *Usherr Live TV*, and any title they open gets streams: your library's file first, then the best releases Usherr's engine finds on your indexers, opened when picked and handing over to the next if one can't be read. Subtitles come from your library's files. *Make a new link* stops the old one.
- **Live TV tunes in without a long wait**: when a streamed program's release takes more than a few seconds to find, the channel stays on its static and asks again shortly, instead of holding the page; every asker shares one lookup. A few minutes after start-up (and every three hours) the programs on now, or starting within half an hour, on every channel are looked up in advance, three at a time.
- **Fewer failed tunes**: a stream that can't be read at first look is looked at once more before the channel falls back to its backup, and the log now says why a look failed (ffprobe's own words).
- **Real TV's AI has room for every slot**: the answer's length now grows with the number of slots (a full day with backups ran past 4000 tokens and was asked twice), and a cut-off answer is asked again with up to 16000.
- A **Right now** panel on the card: speed, connections in use on each Usenet server, what is streaming and the searches each indexer has done today.
- **Sound in the browser, and 4K stays 4K**: a stream in Dolby (E-AC3, Atmos), TrueHD or DTS no longer plays silently in Chrome and Edge. The picture plays as it is and the sound comes beside it, converted to AAC with its channels, in step and seeking as usual. Every conversion now keeps what the browser plays: a picture it shows is copied (4K, HDR, HEVC), a sound it plays is copied, 5.1 stays 5.1, and nothing is made smaller unless you pick a lower quality. That holds for files on disk (converted by Jellyfin or by Usherr), streams and Live TV, which no longer turns pictures into 720p.
- **Sound sync**: the sound played beside a stream's picture allows for the delay your speakers add (Dolby Atmos or DTS:X processing, a receiver, Bluetooth), and the player's *− Sync +* buttons move it earlier or later, 50 ms a tap, kept on each device. Releases whose sound begins a moment after the picture start in step.
- **Marquee screens in your own order**: up and down arrows on *Marquee → Screens* (and the phone remote) set the order the screens are listed in; it stays put instead of moving whichever is on to the top.
- **Pick the language of a stream**: *Audio & subtitles* in the player now lists a stream's sound tracks too, and a MULTi release starts in the language you listen in, even when the file's first track is another.
- **Your stack**: the films and shows you want to watch next, as a stack of tickets in the menu. Put a title on it with the stack button on any poster card (Discover, the Library) or on its page; swipe through it (or use the arrows), move a title up or down, take it off, and tap the poster for its page or **Play now** (a film that isn't downloaded offers Stream). Each person has their own, kept on the server, so a phone has it too. To make room, *Settings* folds away in the menu until you open it.
- **The Marquee gets its own page** (*Marquee* in the menu, and in Settings), with tabs for Screens, Look and scenes, Slides and pre-show, and Home Assistant and API:
  - Every screen is listed, with what it shows now. A screen can have settings of its own, and a **phone remote** (`/marquee/remote`) steers it: previous, pause, next, pin, dim, blank, info, the pre-show, its look and *Show now*.
  - **Seven looks** (Usherr, Cinema, One-sheet, the Poster wall and more), each with its own themes, fonts, frames and effects.
  - New scenes: tonight on Live TV, spotlights, trivia, your own **slides** (pictures, videos, web pages and words), a **pre-show** before the film, and **theme music**.
  - **Home Assistant** (over MQTT: each screen appears by itself, with buttons, switches for pause, pin, dim and blank, and its look and scene) and an **API** for scripts and buttons.
- **Ticket services for In theaters**: *Tickets from* chooses Fandango (the default in the US), Atom Tickets, Cinemark, Moviefone, Showtimes.com, a link of your own (with `{title}` and `{zip}`), or none. Each service's button can show a picture you upload, such as its logo. With Fandango, *All showtimes near* lists everything playing near your ZIP.
- The setup wizard's Extras step has the **In theaters** and **Marquee** cards, the setup checklist counts them, and the first-visit tour points at your stack.
- **The user guide** has pages of its own for Live TV, the AI and the Marquee, and the gaps elsewhere are filled, with new pictures.

**New** (from Mediarium 3.2.16)
- **Custom formats from the TRaSH Guides** are scored with TRaSH's own scores, can be added to several profiles at once, and can be kept up to date by themselves (*Keep TRaSH formats up to date*, daily or weekly) or with *Update from TRaSH*, which shows what would change first.
- **Why a release was picked**: a manual search marks the release an automatic search would take as *Best match*, and an arrow after each score lists the custom formats behind it.
- **Minimum seeders** on each torrent indexer, and stopping or removing a download also removes it from qBittorrent or Transmission (a switch on the download client card).
- **Country for release dates** under *Settings → Info, lists and subtitles*; a film's page shows when it's in cinemas and when it comes out digitally, for your country.
- Indexers are listed by priority in compact cards, and the problem report form says when a newer version is out.

**Fixed** (from Mediarium 3.2.16)
- An indexer's seeding rules now reach the download client; SubDL subtitles download again; shows with the same name and folders shared by several shows are matched correctly.
- Everything else in Mediarium 3.1.4 to 3.2.16, listed in [CHANGELOG.md](CHANGELOG.md).

## [1.0.4] - 2026-10-17

Built on **Mediarium 3.1.3**.

**New** (from Mediarium 3.1.3)
- **Signed-in devices**: *Settings → Account & Access → Security* lists them, with *Sign out all other devices*.
- **Keep the original file names**: a switch under *Settings → Library → File names and importing*; custom naming gets `{Original Filename}` and `{Original Title}`.
- Account changes and API keys made or removed are written to the log and Activity for administrators.
- Change a show's folder, locate a renamed file, set your own book cover, and administrators can see the folder of a title's files.

**Fixed** (from Mediarium 3.1.3)
- Titles with accents or in other alphabets (Pokémon, Amélie, Japanese, Russian, Greek) are found, matched and imported.
- Password-protected RAR releases open with their password; a release whose link gives a login page is no longer grabbed again and again.
- Big libraries are much faster: the Dashboard, Wanted (now in pages) and the Library tabs.
- Security: the setup code can't be skipped, sign-in limits hold however fast the guesses come, and a site entry can't be pointed at your home network.
- A learned daily indexer limit is forgotten after a week and can't lock an indexer out for good.
- Torrents already running when a VPN connects, or the kill switch is switched on, are stopped within about a minute instead of sending from your home address.
- Everything else in Mediarium 3.1.3, listed in [CHANGELOG.md](CHANGELOG.md).

## [1.0.3] - 2026-10-16

Built on **Mediarium 3.1.2**.

**New**
- **Daily limits for indexers** (from Mediarium 3.1.2): Usherr learns each indexer's daily request limit, stops automatic searches just before it, shows "12 of 25 requests today" on the indexer's card and starts again after midnight (UTC). An indexer can also be given a known limit under *Requests per day*.
- The menu lists every Library tab under *Library*, Anime, Comics and NSFW 18+ included; they fold up under Library, like a settings group.
- **Calendar search**: the search and AI bar now sits at the top of the calendar too. Search narrows the calendar as you type and lists the matches by date (a date opens that month); Ask takes questions like "anime this weekend" or "superhero films next month".
- The calendar's kinds are a drop-down next to its tabs, with *In theaters* as a kind of its own.
- Streamed shows are on the calendar too: new episodes of shows you stream (but don't keep in the library) appear marked *Streams*, looked up on TMDB about twice a day.
- **A shorter Dashboard**: the search and AI bar moved into the top bar, where it works from every page; Roulette is the dice next to it; *What should I watch tonight?* is a button of its own; and the folded calendar is gone (*Coming up* shows the same).
- **News in one line**: the Dashboard's news is a single line at the top, one headline at a time or a ticker, and *All news* opens the full list. Each person picks the style, or turns it off, under *Settings → General* or in All news.
- The Dashboard has **Anime** and **Comics** cards next to the others, each with its own notch; the TV card now counts TV without the anime.
- **A slim toolbar on Discover and the Library**: one row with the kind (a drop-down), Filters (it says how many are on) and the sort, pinned under the top bar as you scroll. Filters that are on show as small chips with an x. The Library's status (Downloaded, Waiting…) is one of its filters. The pages' own search and AI bars are gone: the top bar's does the same and now knows which page and kind you're on. *Complete your library* comes after the first row instead of before the posters.
- **Live TV programmed like a network**: a channel can start its shows on the hour and every 15, 30 or 60 minutes, give each part of the day (Morning, Mid-morning, Afternoon, Evening, Prime time, Late night, Overnight) a line-up of its own, and pin shows or movies to set times ("Seinfeld, weeknights at 7", a Sunday Night Movie). The time between shows plays bumpers and ads from a folder you choose, then an "Up next" card, drawn in the browser and in Jellyfin.
- **Live TV guide looks and surfing**: the guide can be a modern grid, a '90s cable box, the Prevue Channel (a preview on top, the line-up rolling below), newspaper listings or "Now on" tiles; each column can cover 15, 30 or 60 minutes, 6 to 24 hours ahead. Star favourite channels, hide others. While watching: channel up and down (arrow keys, buttons or a swipe), type a number, *Last* channel, a burst of static between channels (with its hiss, if you like), an info banner with what's on, the time left and what's next, and an optional CRT look.
- **More Live TV**:
  - Special programming: holiday episodes in their season (Halloween to the Fourth of July), new episodes premiering in prime time marked NEW, family hours (nothing for older than about ten before 9 PM), and commercial breaks inside shows from the filler folder.
  - Channel packs (Saturday Morning, Sitcom Gold, TGIF, Sci-Fi, Crime & Mystery, Movie Channel, Creature Feature, Holiday, Rewind, Discovery Hour) and *Build with AI*: describe a channel and it's drafted from your library, with famous shows you don't have streamed.
  - Remind me (a phone message five minutes before a show starts; *TV reminders* in Notifications), Start over, catch-up on the last three hours, and Multiview (four channels at once).
  - *Other apps*: the channel list and guide for IPTV apps such as TiviMate, and a tuner address for Plex's Live TV & DVR.
  - The Marquee has an *On Live TV* scene.
- **Tidier screens everywhere**:
  - Settings pages fold each card to its title row (with its status); tap one to open it, or *Open all*. A card with an error opens by itself. Extras goes from about thirteen phone screens to two.
  - Music, Ebooks, Audiobooks, Wanted and Discover's *Browse all* use the same slim toolbar; Comics and NSFW 18+ pick the kind from the same drop-down.
  - Activity's counts are one line, and *Import a file* and *Add a download* by hand are in one *By hand* menu.
  - A film's or show's trailers are one *Trailers* button, and Reviews, Website, IMDb and TMDB one *Links* menu.
  - On the Calendar the top bar searches the calendar; *Only what isn't downloaded* and *Add to your calendar app* are in its ⋯ menu.
  - The Diary's kind and year are drop-downs in one row.
- Titles you stream but haven't downloaded say **Streams** in search, on Discover and on their page, and adding one says you already stream it before it downloads a copy.
- A streamed title in the Library opens its own page, like any other title, instead of the separate Streamed list. A streamed show's page has *Watch (streams)*, which opens its episodes right there.

**Changed**
- The NSFW 18+ module is called **NSFW** everywhere: its page is at `/nsfw` (old `/scenes` links still work), its folder variable is `NSFW_DIR` (`SCENES_DIR` is still read), and an item in it is a **video**. Existing libraries move over on their own when you update; nothing is lost.

**Fixed**
- **Folders for comics, anime and NSFW 18+ on a new install.** They are set under *Settings → Library → Folders*, next to movies and TV. With nothing set, comics and NSFW 18+ go in `/data/comics` and `/data/nsfw` (made on the first download), and anime finds `/data/anime` when it exists. The example compose file and the install guide list `COMICS_DIR`, `ANIME_DIR` and `NSFW_DIR`.
- Mediarium's own connection checks no longer use up free indexers' daily requests (from Mediarium 3.1.2).
- Everything else in Mediarium 3.1.1 and 3.1.2, listed in [CHANGELOG.md](CHANGELOG.md).

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
