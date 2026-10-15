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

**Optional.** Scheduled channels made from your library (and from streams),
with a programme guide. Watch them on the **Live TV** page, or add Mediarium
as a tuner so the channels appear in Jellyfin's Live TV on every TV.

- **Set it up:** make a channel on the **Live TV** page (**New channel**), then
  **Add to Jellyfin's Live TV** on the card in Extras.
- **Check:** the channel plays on the Live TV page, and shows in Jellyfin's
  guide after its next refresh.

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

## AI

**Optional.** AI Search on Discover (find a title from a description or a
photo), *What should I watch tonight?*, *Catch me up*, *What's it like?*,
mood playlists, news summaries and the family filter's estimates. The AI only
suggests: every title is checked on TMDB, and nothing is added without you.

- **Choose one:**
  - **Ollama**, a model running on your own computer: free and private, needs a
    graphics card for good speed. Install Ollama, then `ollama pull gemma3:4b`.
    The address is usually `http://host.docker.internal:11434`. The first
    answer after a while takes up to a minute while the model loads.
  - **Claude**: knows films and shows much better and answers in seconds; needs
    an API key from console.anthropic.com, paid by use (a few dollars a month
    for a household).
- **Put it in:** **Settings → Extras → AI**. *Answers a day for each person*
  keeps use in check (administrators aren't limited).
- **Check:** **Test it**, then ask something in the bar at the top of Discover.

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
