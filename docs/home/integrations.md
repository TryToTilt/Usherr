# Setting up each part

Every service the home fork can use, one card each: what it does, whether you
need it, where to get it, where to put it in Mediarium, and how to check it
works. Only the first two are required.

Most optional parts live in **Settings → Extras**. Each card there shows its
state (**Ready**, **Needs setting up** or **Off**) and a **Needs:** note that
says exactly what's missing.

## TMDB

**Required.** The Movie Database is where every title, poster, cast list and
episode list comes from, and what Discover browses.

- **Get it:** make a free account at themoviedb.org, then *Settings → API* and
  ask for a developer key. The "API Key" (v3) is the one to copy.
- **Put it in:** the setup wizard asks for it, or **Settings → Info, lists and
  subtitles → Movie and show info**.
- **Check:** Discover shows posters. Without the key, Discover is empty and
  adding a title says to add the key first.

![Settings → Movie and show info, with the TMDB key](images/settings-metadata.png)

## Usenet and indexers

**Required** (or torrents instead). An indexer finds releases; a Usenet
provider is where they're downloaded from. Mediarium has its own downloader,
so no SABnzbd or NZBGet is needed.

- **Get it:** a paid Usenet provider (any with a long retention), and one or
  more Newznab indexers (most have a free tier with a daily limit). If you
  already use Prowlarr, add each indexer with its own feed address from
  Prowlarr, or bring them all over with **Settings → System → Move from other
  apps**.
- **Put it in:** **Settings → Indexers & Search** for indexers,
  **Settings → Downloading → Usenet** for the provider (address, port, SSL,
  username, password, connections).
- **Check:** each indexer and provider has a test button. Then search a title's
  releases from its page.
- **Connections:** a provider allows a set number of connections at once. If
  StreamNZB uses the same account, split them (for example 20 for Mediarium
  and 20 for StreamNZB) so streams don't starve downloads.

![Settings → Usenet: the provider with its connections and a test button](images/settings-usenet.png)

## Jellyfin or Emby

**Optional, recommended.** Plays everything on your TVs and phones, and is what
the player, Live TV and the streamed library work through.

- **Get it:** install Jellyfin (free) or Emby on the same server, with libraries
  on the same folders Mediarium files into.
- **Put it in:** **Settings → Connections → Media servers**: its address and an
  API key made in Jellyfin's *Dashboard → API Keys*. If Jellyfin sees your
  folders under other names (Jellyfin runs outside Docker, for example),
  add a **folder mapping** there too.
- **Check:** the card in **Settings → Extras** says **Ready**, and new
  downloads appear in Jellyfin within a minute.
- **While someone is watching** on the same page pauses downloads whenever
  someone is watching in Jellyfin, and resumes them about a minute after. With
  a slow line, or when streams share a Usenet account with downloads, it keeps
  playback smooth. It's off until you switch it on.

![The Jellyfin or Emby card in Settings → Extras](images/extras-jellyfin.png)

## Mediarium's address on your network

**Optional, needed for Live TV and the streamed library.** The address your TV
and Jellyfin use to reach Mediarium. It has to work from another device, so
it's your server's network address, not `localhost`.

- **Put it in:** **Settings → Extras → Mediarium's address on your network**,
  for example `http://192.168.1.20:8264`.
- **Check:** open that address on your phone while on the same network.

![Mediarium's address on your network](images/extras-address.png)

## Streaming engine (Usherr or StreamNZB)

**Settings → Extras → Streaming engine** says which engine plays streams:
**Stream now**, the streamed library (the Jellyfin pointer files) and streamed
Live TV channels. If the chosen one finds nothing, the other is tried.

- **Usherr** searches the indexers you already have, checks the release is on
  your Usenet servers and reads the video from them as you watch. No separate
  program: it needs a Usenet server and an indexer. It finds daily shows by
  their air date (General Hospital, late night), reads inside stored RAR sets,
  and keeps the other releases in line: if one stops part-way, the next plays
  from where you were (or press **Try another release**).
- **StreamNZB** is the separate program below. It is still fully supported:
  an install that already has it connected keeps using it until you choose
  otherwise, and a new install uses Usherr's engine and needs nothing else.

**Coming from StreamNZB?** **Import from StreamNZB…** on the same card copies
its Usenet providers, indexers and size limits. Choose its `config.json` (or
map it into Usherr at `/streamnzb/config.json`, read only) and you see what is
in it first: what Usherr already has is marked and left alone, and only what
you tick is copied. Logins go into Usherr's own encrypted settings and are
never shown on the page. Check the indexer addresses afterwards: they are
copied as StreamNZB uses them, and Usherr may reach them at another address.

The same card holds the **device qualities**: for 4K, 1080p and 720p, the most
a release may weigh and words to leave out (AV1 is left out because TVs can't
play it). A phone asks for 720p, the TV for 4K. Below it, **Right now** shows
the speed, how many connections are in use on each Usenet server, what is
streaming and how many searches each indexer has done today. Searches are
remembered for a few minutes, so tuning the same title again doesn't ask the
indexers again.

## Streaming (StreamNZB)

**Optional.** [StreamNZB](https://github.com/Gaisberg/streamnzb) plays a
release straight from Usenet without downloading it first. With it,
title pages get **Stream now**, and you get the streamed library and streamed
Live TV channels.

- **Get it:** run StreamNZB next to Mediarium (its own container), with your
  Usenet provider set up in it.
- **Put it in:** **Settings → Extras → Streaming (StreamNZB)**: its address
  (empty means `http://host.docker.internal:7000`) and its admin token, from
  StreamNZB's `config.json` (`admin_token`). Or map that `config.json` into
  Mediarium at `/streamnzb/config.json` (read only) and leave the token empty.
- **Check:** **Test it** on the card, then **Stream now** on any title page.

![The Streaming (StreamNZB) card: the address, the admin token, Save and Test it](images/extras-streamnzb.png)

## Streamed library

**Optional, needs StreamNZB, the address above and Jellyfin.** Titles you
stream instead of download show in the Library next to the ones on disk, and in
Jellyfin as small pointer files that start the stream when played. A title you
later download plays from disk instead.

- **Set it up:**
  1. Map a folder on your server to `/data/streamed` in Mediarium's container.
  2. Add that folder to a Jellyfin library (or make one, such as "Streamed").
  3. In the card, enter the folder **as Jellyfin sees it**, for example
     `D:\Streamed` or `/media/streamed`. Leave it empty if the folder mapping
     under Media servers already covers it.
  4. **Put streamed titles into Jellyfin's libraries**.
- **Check:** a streamed title shows in the Library marked **Streams** (not on
  disk: plays through StreamNZB), and in Jellyfin.

![The Streamed library card](images/extras-streamed.png)

## Live TV

**Optional.** Scheduled channels made from your library, from streams, from
free live channels (Pluto, PBS) or from a real network's day (Real TV), with
a programme guide. Watch them on the **Live TV** page, in Jellyfin, in Plex
or in an IPTV app.

- **Set it up:** make a channel on the **Live TV** page (**New channel**, a
  **Channel pack**, or **Build with AI**), then **Add to Jellyfin** on the
  Live TV card here.
- **Check:** the channel plays on the Live TV page, and shows in Jellyfin's
  guide after its next refresh.

See [Live TV](live-tv.md) for making channels, scheduling, Real TV, Plex and
IPTV apps.

![The Live TV page with its guide](images/live-tv.png)

## Play on TV

**Optional.** Sends a stream to an Android TV box (such as an NVIDIA Shield),
which plays it in VLC or opens it in Stremio. Mediarium uses the box's network
debugging to do it.

- **On the TV box:** turn on *Developer options* (press *Build* seven times in
  *Settings → About*), then *Network debugging* (*USB debugging* on some boxes).
  A fixed address for the box in your router helps.
- **Put it in:** **Settings → Extras → Play on TV**: the box's address and a
  name, then **Pair** and allow the prompt that appears on the TV.
- **Check:** **Play on TV** on a title page starts it on the box.

![The Play on TV card](images/extras-play-on-tv.png)

## Stremio and Nuvio

**Optional.** One add-on that works in both [Stremio](https://www.stremio.com)
and Nuvio: Usherr's own catalogs and streams inside the apps, on a phone,
a TV or a computer.

- **What it gives them:**
  - **Catalogs:** *Usherr Movies* and *Usherr TV* (your library and your streamed
    titles; the apps' search finds them too), *Usherr Watchlist* (the titles you
    gave a thumbs up) and *Usherr Live TV* (your channels).
  - **Streams** for any film or episode the app shows, even one that isn't in
    Usherr: first your library's own file, then the best releases Usherr's
    streaming engine finds on your indexers (up to six, with picture size, file
    size and indexer). A release opens when you pick it, and if it can't be read
    the next one is tried.
  - **Subtitles** from your library's files: the text subtitles in the file or
    next to it.
  - **Live TV:** a channel plays through the same address the tuner feed uses.
- **Put it in:** **Settings → Extras → Stremio and Nuvio**. Check the address your
  devices use for Usherr (your home network address, such as
  `http://192.168.1.20:8264`, or your https address from outside), then copy the
  link or scan the QR code. In Stremio: *Add-ons → Add add-on*, paste the link.
  In Nuvio: add it under its add-ons or plugins with the same link. This is done
  once in each app; with an https address the **Install in Stremio** button does
  it in one tap.
- **It is private:** the link has your own key in it, so the apps need no
  sign-in, and each person has their own (watchlist and permissions are theirs).
  **Make a new link** stops the old one working, for example if it was shared by
  mistake.
- **Streams from Usenet** need a Usenet server and an indexer set up (the same
  ones downloads use). Without them the library, watchlist and Live TV still
  work.

## AI

**Optional.** The Ask bar (find a title from a description or a photo,
*Tonight*, *Pick for the Room*, *Catch me up*, *What's it like?*, the skip
guide, reading orders, playlists, why a download failed), news summaries,
the family filter's estimates, and Live TV's AI channel builder and Real TV.
The AI only suggests: every title is checked on TMDB, and nothing is added
without you.

- **Choose one:** **Ollama** on your own computer (free and private), **Claude**
  (knows films best; paid by use, and `claude-haiku-5-5` as the model keeps it
  to pennies), or an **OpenAI-compatible server** such as LM Studio.
- **Put it in:** **Settings → Extras → AI**. *Answers a day for each person*
  keeps use in check (administrators aren't limited).
- **Check:** **Test it**, then ask something in the bar at the top of Discover.

See [The AI](ai.md) for every feature, the models and what they cost.

![The AI card: which AI, its address and model, the daily allowance, Save and Test it](images/extras-ai.png)

## Scores (MDBList)

**Optional.** IMDb, Rotten Tomatoes, Metacritic and Letterboxd scores and Common
Sense Media ages on Discover and title pages, and as filters in Discover
recipes. The family filter uses the Common Sense ages first.

- **Get it:** a free API key from mdblist.com (*Preferences → API*).
- **Put it in:** **Settings → Extras → Scores (MDBList)**.
- **Check:** **Test it**. Your library's titles are filled in first, a little
  each hour; Discover's are fetched as you browse.

![The Scores (MDBList) card](images/extras-mdblist.png)

## Entertainment news

**Optional, needs the AI.** Short news under *Tonight* on the Dashboard: film
and TV, home theater, books, comics and music. Every few hours Mediarium reads
the feeds and the AI writes a short summary of each story, linking titles it
names to your library.

- **Set it up:** it comes with a list of feeds; untick any you don't want, or
  **Add a feed** (any RSS address). *Stories summarised a day* limits AI use.
- **Check:** **Read the feeds now**, then look at the Dashboard.

![The Entertainment news card with its feeds](images/extras-news.png)

## In theaters

**Optional.** Films in cinemas near you: the *In theaters now* rows on the
Dashboard and at the top of Library → Movies, cinema openings on the
calendar, and links to showtimes on a film's page.

- **Set it up:** tick *Show films in theaters*, give your ZIP or postcode, your
  theater and the country (two letters, such as US).
- **Showtimes** under each film opens a search with every theater near you
  showing it, and **Tickets** opens it on your ticket service. **Tickets
  from** chooses Fandango (the default in the US), Atom Tickets, Cinemark,
  Moviefone, Showtimes.com, a link of your own (such as an AMC, Regal or
  local theater's search page, with `{title}` where the film's name goes and
  `{zip}` for your ZIP code), or none. With Fandango, **All showtimes near**
  (your ZIP) lists everything playing nearby.
- **Upload a picture** puts any picture you like on the chosen service's
  button, such as its logo; each service keeps its own, and Mediarium
  doesn't come with any.
- **Optional:** an AMC key (from developers.amctheatres.com) makes the row
  follow what your AMC theater is showing, with its showtimes.

## Marquee

**Optional.** A lobby display for a tablet or a screen by the TV, with its own
page (**Marquee** in the menu). See [The Marquee](marquee.md) for setting up a
screen, the looks, the pre-show, Home Assistant and the API.

## Comics

**Optional.** Follow comic series, get new issues and read them in a
page-flip reader, with story arcs and a weekly pull list.

- **Get it:** a free ComicVine API key from comicvine.gamespot.com/api.
- **Put it in:** **Turn Comics on** in **Settings → Extras**, then the key under
  *Settings* on the **Comics** page. Comics are searched in your indexers'
  comics category.
- **Direct downloads (optional):** the *GetComics* card adds GetComics as a
  source. If GetComics shows a Cloudflare check, run FlareSolverr and enter its
  address (usually `http://host.docker.internal:8191`).
- **Check:** search for a series on the Comics page and add it.

![The Comics card in Settings → Extras](images/extras-comics.png)

## NSFW 18+

**Optional, off unless you turn it on.** Adult videos, with performers and
studios. Only administrators ever see it: it is hidden from everyone else,
keeps its own library and its own download queue, and never sends
notifications. The server must allow it (`USHERR_ALLOW_ADULT`).

## Schedules

**Settings → Schedules** keeps heavy work to the hours you choose. Make named
schedules (one time range every day, or days and times), then give each task
one: automatic searches, downloads, repair and unpacking, and media server
refreshes. Anything you start by hand always runs at once. **Quiet for…**
holds every heavy task for a while, such as when TV time starts early;
everything picks up again by itself.

## Notifications

**Optional.** Messages when something is added, downloaded or fails, by email,
ntfy, Discord, Telegram and others, under **Settings → Connections →
Notifications**. Each person can also choose their own under
**Settings → Your profile**.

## Reaching Mediarium away from home

Mediarium is meant to sit behind your home network. To use it from your phone
away from home, use a private network such as Tailscale (nothing opened to the
internet), or a reverse proxy with HTTPS. Turn on two-step sign-in or passkeys
under **Settings → Account & Access → Security** before opening it up.
