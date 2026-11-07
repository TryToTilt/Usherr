# Setting up each part

Every service Usherr can use, one card each: what it does, whether you
need it, where to get it, where to put it in Usherr, and how to check it
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
provider is where they're downloaded from. Usherr has its own downloader,
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
  StreamNZB uses the same account, split them (for example 20 for Usherr
  and 20 for StreamNZB) so streams don't starve downloads.

![Settings → Usenet: the provider with its connections and a test button](images/settings-usenet.png)

## Jellyfin or Emby

**Optional, recommended.** Plays everything on your TVs and phones, and is what
the player, Live TV and the streamed library work through.

- **Get it:** install Jellyfin (free) or Emby on the same server, with libraries
  on the same folders Usherr files into.
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

## Plex

**Optional.** Usherr works with Plex beside Jellyfin, or instead of it: your
Diary and taste from Plex's history, the Marquee's *Now showing*, intro and
credits markers, Re-Posterr, the pre-show as Plex's pre-roll, Live TV in
Plex's guide, and streamed titles through the [virtual drive](#virtual-drive-for-plex).

- **Put it in:** **Settings → Connections → Media servers → Sign in with
  Plex**. For a Plex on the same computer as Docker, Usherr uses
  `http://host.docker.internal:32400`.
- **Converting for Usherr's player.** When a browser can't play a file as it
  is and no Jellyfin or Emby converts it, Plex does, before Usherr's own
  (slower) conversion. On the graphics card it needs Plex Pass and, in Plex,
  **Settings → Transcoder → Use hardware acceleration when available**;
  Plex's dashboard then shows *Transcode (hw)*. Plex only converts a file
  it has in a library. With Jellyfin connected too, **Settings → Extras →
  Converting for the player** chooses who goes first: whichever is connected
  (Jellyfin or Emby, then Plex), Jellyfin, Plex, or Usherr itself. Whichever
  can't convert a file hands on to the next, and Usherr is always last.

## Usherr's address on your network

**Optional, needed for Live TV, the streamed library, the Stremio and Nuvio
add-on, and a virtual drive mounted on another computer.** The address your
TV, phone and media server use to reach Usherr. It has to work from another
device, so it's your server's network address, not `localhost`.

- **Put it in:** **Settings → Extras → Usherr's address on your network**,
  for example `http://192.168.1.20:8264`.
- **Check:** open that address on your phone while on the same network.
- **Keep it from changing.** Your router hands out these addresses, and can
  give the server a new one after a restart: then everything using the old
  one stops working. In your router's settings, look for **DHCP
  reservation** (some routers say *static lease* or *address reservation*)
  and reserve the address the server has now. Do the same for a separate
  Plex or Jellyfin computer, and for a NAS that holds your films.
- **Not the same as *Address of Mediarium*** (**Settings → General**). That
  one is the full `https://` address people open Usherr at from anywhere,
  used for links in messages, passkeys and single sign-on. This one is the
  plain address at home. Many households only need this one.

![Usherr's address on your network](images/extras-address.png)

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

- **Get it:** run StreamNZB next to Usherr (its own container), with your
  Usenet provider set up in it.
- **Put it in:** **Settings → Extras → Streaming (StreamNZB)**: its address
  (empty means `http://host.docker.internal:7000`) and its admin token, from
  StreamNZB's `config.json` (`admin_token`). Or map that `config.json` into
  Usherr at `/streamnzb/config.json` (read only) and leave the token empty.
- **Check:** **Test it** on the card, then **Stream now** on any title page.

![The Streaming (StreamNZB) card: the address, the admin token, Save and Test it](images/extras-streamnzb.png)

## Streamed library

**Optional, needs a streaming engine (Usherr's own is enough, see above) and Jellyfin.** Titles you
stream instead of download show in the Library next to the ones on disk, and in
Jellyfin as small pointer files that start the stream when played. A title you
later download plays from disk instead.

- **Set it up:**
  1. Map a folder on your server to `/data/streamed` in Usherr's container.
  2. Add that folder to a Jellyfin library (or make one, such as "Streamed").
  3. In the card, enter the folder **as Jellyfin sees it**, for example
     `D:\Streamed` or `/media/streamed`. Leave it empty if the folder mapping
     under Media servers already covers it.
  4. **Put streamed titles into Jellyfin's libraries**.
- **Check:** a streamed title shows in the Library marked **Streams** (not on
  disk: plays through the streaming engine), and in Jellyfin.

![The Streamed library card](images/extras-streamed.png)

## Virtual drive (for Plex)

**Optional, for Plex.** Plex can't play the Streams library's pointer files.
The virtual drive puts the same streamed films and shows on a drive of
ordinary video files that Plex (or Jellyfin) reads like any folder; a file is
played straight from Usenet, only the part being watched. Usherr serves the
drive (WebDAV, read only); **rclone** mounts it on the computer that runs
Plex, with **WinFsp** on Windows. Both are free and come from their own
websites: Usherr doesn't install anything on your computer.

- **It's on by itself** when Plex is your media server and Jellyfin and Emby
  aren't. Otherwise switch it on in **Settings → Extras → Virtual drive (for
  Plex)**.
- **What's on the drive.** A drive needs each file's size before it's played,
  so a streamed film or episode appears once Usherr knows its release. Anything
  someone plays (in Usherr, Jellyfin or the add-on) is kept at once, at no
  cost.
- **Streams you add go on right away.** **Add as stream** puts the film, or the
  show's newest season, on the drive at once: one search a film or episode, a
  few seconds apart, and Plex has each a couple of minutes later. For a title
  already in your streams, its page has **Put it on the virtual drive now**
  (the newest season, for a show). Both use the day's searches.
- **The rest, a few a night.** Everything else is looked for one at a time
  (**Searches a day**, 30 to start with), so your indexers' daily limits are
  never spent in one go: newest first, and a new episode of a show you stream
  goes to the front of the queue. **Get one ready now** looks for the next one
  straight away.
- **Only at night.** This search runs only between the hours on the card,
  **2 AM to 9 AM** to start with, because during the day your streams need the
  Usenet connections. The day's searches are spread evenly over those hours
  (60 searches in 7 hours is one every 7 minutes), never closer than a minute
  apart. The same hour twice means all day. What you start yourself isn't held
  back. The hours follow Usherr's clock, which the card shows: that's the `TZ`
  in Usherr's compose file. If it's left at `Etc/UTC` the card warns you, and
  2 AM means 2 AM UTC until you set your own zone.
- **One bad article doesn't cost you the title.** A release leaves the drive
  only when its articles are confirmed gone from your Usenet servers, and
  another is then looked for.
- **Its own sign-in.** The drive has its own user (`usherr`) and password,
  shown on the card. It only opens the drive, and only to read; it is never a
  person's account. **New password** makes another (then run rclone's setup
  command again).
- **Where Plex runs.** **Plex runs on** on the card says whether Plex is on
  the computer Usherr runs on, or another one. Usherr works it out from
  Plex's address until you choose.
  - **This computer:** rclone reaches Usherr at `127.0.0.1`, which never
    changes, so the drive keeps working after every restart.
  - **Another computer** (a media PC, a NAS): rclone there uses [Usherr's
    address on your network](#usherrs-address-on-your-network). Reserve that
    address in your router, or a restart can move it and the drive stops
    opening: Plex then says streamed titles are unavailable.

  After changing it, run the setup command (step 2 below) again.
- **Mount it** (the card has the exact commands, with your address and password
  filled in):
  1. Install [WinFsp](https://winfsp.dev/rel/) and [rclone](https://rclone.org/downloads/)
     (on Linux, rclone alone).
  2. Tell rclone about the drive, once (Usherr's own port, if Docker publishes
     it as something other than 8264):
     `rclone config create usherr webdav url=http://<address>:8264/dav/ vendor=other user=usherr pass=<the drive's password> --obscure`
     where `<address>` is `127.0.0.1` with Plex on this computer, else Usherr's
     address on your network.
  3. Mount it, on Windows as drive U: (any free letter):
     `rclone mount usherr: U: --read-only --vfs-cache-mode off --dir-cache-time 1m --network-mode`
     If rclone isn't on your PATH, put the full path to `rclone.exe` in the
     card's **Where rclone is** box: the commands use it.
  4. **Optional, Windows: mount it at sign-in.** Tick the box under the
     commands to download `usherr-drive.cmd`, then move it into your Startup
     folder (Windows+R, `shell:startup`). **This installs a startup item**: it
     runs every time you sign in, waits until Usherr answers, then mounts the
     drive with its window hidden. It holds no password (rclone keeps it).
     Delete the file from that folder to stop it.
  5. With Plex connected to Usherr (**Settings → Connections → Media servers**), press **Add
     them to Plex for me** on the card: Usherr makes **Movies-Stream**,
     **TV-Stream** and **Anime-Stream** (a folder already in one of your
     libraries is left as it is). Or, by hand: in Plex, add `U:\Movies` to a Movies library and `U:\TV` (and `U:\Anime`)
     to a TV library.
- **Keep Plex light.** These are Plex settings for the whole server. In
  **Settings → Library**, set video preview thumbnails, chapter thumbnails
  and voice activity data to *never*: each would read whole films from
  Usenet. Turn on **Scan my library periodically** (Windows doesn't tell
  Plex about new files on the drive), and untick **Empty trash automatically
  after every scan**, so a moment without the drive (its window closed,
  Usherr restarting) doesn't empty its titles from Plex. In **Scheduled
  Tasks**, untick **Perform extensive media analysis during maintenance**.
  Intro and credits detection is only offered with Plex Pass.
- **Plex has new titles within minutes.** When a title comes onto the drive,
  Usherr asks each connected media server with a library on the drive to look
  at just that title's folder (never a whole library). For that it needs to
  know where the drive is mounted on the Plex computer: **Where Plex sees the
  drive** on the card, `U:` to start with (or a folder such as `/mnt/usherr`
  on Linux). A Windows letter there is also the one the commands mount.
- **Check:** the card counts the films and episodes ready, and says when
  something last read the drive. **Something not right?** in Show me how warns
  when Plex was asked to look at a new title and nothing came to read the
  drive within 15 minutes (it isn't mounted, or rclone has an old address),
  and when nothing has read it for a day while Plex is connected.

**Show me how → Set up Plex with the virtual drive** walks you through all of
this. WinFsp is Windows File System Proxy, copyright Bill Zissimopoulos
([github.com/winfsp/winfsp](https://github.com/winfsp/winfsp)); rclone is by
Nick Craig-Wood and its contributors ([rclone.org](https://rclone.org)).

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
which plays it in VLC or opens it in Stremio. Usherr uses the box's network
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
- **Put it in:** **Settings → Extras → Stremio and Nuvio** (an administrator), or the **You** page (everyone: each person's own link is there). Check the address your
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
and TV, home theater, books, comics and music. Every few hours Usherr reads
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
  button, such as its logo; each service keeps its own, and Usherr
  doesn't come with any.
- **Optional:** an AMC key (from developers.amctheatres.com) makes the row
  follow what your AMC theater is showing, with its showtimes.

## Marquee

**Optional.** A lobby display for a tablet or a screen by the TV, with its own
page (**Marquee** in the menu). See [The Marquee](marquee.md) for setting up a
screen, the looks, the pre-show, Home Assistant and the API.

## Pre-show

**Optional, nothing needed.** A cinema's pre-show before a film: a welcome,
trailers for what's coming, your own clips, the rating and the countdown. It's
ready as it is ("Movie night"); set it up on **Settings → Movie night →
Pre-show**.

- **Your clips:** put videos in the pre-show folder (`preshow` in Usherr's
  config folder), one folder per category, or upload them there.
- **Coming-soon trailers:** on by default; Usherr downloads them with yt-dlp
  (in its image). Map a folder with room to `/data/trailers`.
- **In the Jellyfin app:** **Set it up** installs the NeXroll Intros plugin
  in Jellyfin, then **Restart Jellyfin** loads it.
- **Check:** **Watch a sample**, then play a film in the Jellyfin app.
  See [The pre-show](preshow.md).

## Re-Posterr

**Optional.** Re-Posterr, Usherr's artwork picker, picks the poster, background and logo for each title in
Jellyfin and Plex from TMDB (and fanart.tv), and asks before changing anything.

- **Needs:** Jellyfin or Plex connected, and the TMDB key. Plex keeps posters
  and backgrounds; logos, season posters and title cards are Jellyfin's.
- **fanart.tv (optional):** a free personal key from your fanart.tv profile,
  pasted on **Settings → Re-Posterr**.
- **Your style (optional):** posters, season posters, episode title cards and
  backgrounds made by Re-Posterr, the title in your font and layout on art
  without words; your own overlays, a style per library.
- **TheTVDB (optional):** a TheTVDB API key, for more art for shows.
- **Posterizarr (optional):** its address; it can take over the posters.
- **Check:** **Look now**, then see **Waiting for you**; or the **Re-Posterr** button
  on a film's page. See [Re-Posterr](re-posterr.md).

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

## Sports

**Optional.** A **Sports** page for everyone: leagues, the teams you follow and
the games of the coming two weeks. Games **stream** like films and episodes,
and **download** into a Sports folder that Plex and Jellyfin read. Choose what
it runs on in the *Sports* card in **Settings → Extras**:

- **Sportarr (stacked):** run Sportarr (github.com/Sportarr/Sportarr) next to
  Usherr, like Radarr or Sonarr, with your indexers (add it to Prowlarr as a
  *Sonarr* app), your download client, a root folder for sports and a quality
  profile. Enter its address (from Usherr's container, usually
  `http://host.docker.internal:1867`) and its API key (Sportarr's
  *Settings → General*). Streams use the releases Sportarr matches to a game;
  **Get** downloads through Sportarr.
- **Usherr's own engine:** no other app. Leagues and games come from
  TheSportsDB. Without a key Usherr uses TheSportsDB's free test key (search
  and part of each season, slowly); a paid key (thesportsdb.com/pricing, in
  your profile there once you subscribe) gives full schedules and scores. Games
  stream from your own indexers and download through your own download
  clients, into the **Sports folder** (empty: *Sports* next to your TV folder,
  when that sits in a media folder such as `/data/media/tv`; with a TV folder
  mounted on its own, like `/tv`, mount a folder for sports and enter it).
  Add that folder to Plex or Jellyfin as a **TV** library. Choose the
  **Quality for games** (one of your quality profiles; empty: the same as TV
  shows) and whether Usherr **looks for wanted games by itself**. Already run
  Sportarr? **Bring my followed leagues from Sportarr** copies its leagues and
  teams (see [Moving from Sportarr](sports.md#moving-from-sportarr)).
- **Streaming** needs Usherr's streaming engine (see *Streaming engine* above).
- **Check:** **Test Sportarr** or **Test TheSportsDB**, then open **Sports** in
  the menu and find a league. See [Sports](sports.md).

## Read-to-Me

**Optional.** Turns an ebook (an EPUB) into an audiobook with a computer voice on
your own graphics card: **Kokoro** for everyday narration, **Chatterbox** (started
only when wanted) for more expressive voices and copied voices. Each ebook's page
gets a **Read to me** button; the finished audiobook is filed with your audiobooks.

- **Get them:** run Kokoro (and, if you want it, Chatterbox) as containers on a
  computer with an NVIDIA card. The compose file and the steps are in
  [Read-to-Me](read-to-me.md#setting-it-up).
- **Put them in:** **Settings → Extras → Read-to-Me**: Kokoro's address (from
  Usherr's container, `http://host.docker.internal:8881`) and, optionally,
  Chatterbox's (`http://host.docker.internal:4123`). **Night only** keeps the work
  to the hours you choose.
- **Check:** **Test Kokoro**, then open an ebook and press **Read to me**; the
  first words of the book play in the voice you pick. See [Read-to-Me](read-to-me.md).

## NSFW 18+

**Optional, off unless you turn it on.** Adult videos, with performers and
studios. Only administrators ever see it: it is hidden from everyone else,
keeps its own library and its own download queue, and never sends
notifications. The server must allow it (`USHERR_ALLOW_ADULT`).

The **Tools** tab, for administrators, has three helpers. Matching and renaming
run only when you press their button, and one job runs at a time, with progress
and **Stop**.

- **Match files by fingerprint** finds video files in your NSFW folder that
  Usherr doesn't know, and looks them up on StashDB (one request a second at
  most). It reads 128 KiB of each file, the OSHASH fingerprint. You see each
  file with the video it matched and whether the length agrees. More than 10
  seconds or 5% apart, or a fingerprint shared by several videos, is marked
  **Unsure** and isn't ticked for you. Files you confirm are added as
  downloaded, where they are. A file StashDB doesn't know stays unknown:
  fingerprints only work for files other people have submitted.
- **Bring in my StashDB favorites** shows the favorite performers and studios of
  the account behind your key, and follows the ones you pick. They are followed
  from today: nothing older is added and no search starts. After that the usual
  follow check adds their new videos as wanted, and your automatic video search
  looks for them if it's on (see **Settings**). It never unfollows anyone, and
  someone you stopped following here stays that way. The switch below it, off by
  default, repeats this every night between 2 and 9.
- **Rename and file** shows each downloaded video's old and new name,
  `Studio/Studio - YYYY-MM-DD - Title [quality].ext`, before anything changes.
  You pick the videos. Subtitles and `.nfo` files of the same name move along,
  and your media servers are told. A name that is taken blocks that video. A
  file is only ever renamed, never copied, overwritten or deleted, so one on
  another drive stays where it is, with the reason shown. Files outside the NSFW
  folder aren't ticked for you: an app that still manages them, like Whisparr,
  would lose track of them. **Undo** puts it all back, except a file that changed
  since or whose old name is taken, which stays and is listed.

## Schedules

**Settings → Schedules** keeps heavy work to the hours you choose. Make named
schedules (one time range every day, or days and times), then give each task
one: automatic searches, downloads, repair and unpacking, and media server
refreshes. Anything you start by hand always runs at once. **Quiet for…**
holds every heavy task for a while, such as when TV time starts early;
everything picks up again by itself.

## Music in phone apps (Subsonic)

**Optional.** Play Usherr's music in any app that speaks Subsonic or
OpenSubsonic, the same way you would with Navidrome: Symfonium and Tempo
(Android), play:Sub, Substreamer and Amperfy (iPhone), DSub, and Feishin
(desktop). Apps can browse, search, play, download for offline, keep
playlists, star songs (the same as the heart in Usherr), see lyrics and
send plays to ListenBrainz and Last.fm.

1. **Settings → Services → Phone apps.** Switch on **Let phone apps sign in**.
2. Click **Make an app password** and copy it now: it is shown once. It is
   your one app password for reading and music apps, the same as your
   e-reader password (if you already use one for an e-reader, making a new
   one means typing the new one there too).
3. In the app, add a Subsonic (or "Navidrome / OpenSubsonic") server:
   - **Server:** Usherr's address, as shown on the card (for example
     `http://192.168.1.10:8264/` or your Tailscale address).
   - **User name:** your Usherr user name.
   - **Password:** the app password, never your own password.
   - Leave "legacy" or "plain password" sign-in off where the app offers it.
4. Check: the app lists your artists. If it says the password is wrong, make
   sure you used the app password and that phone apps are switched on.

Good to know:
- Switching **Let phone apps sign in** off stops every music app at once, and
  leaves your e-reader alone. Removing the app password stops both.
- An e-reader password made before this version works in apps that send the
  password as it is, but apps that use Subsonic's token sign-in (most of them)
  need a new one: the card says so.
- Your Usherr permissions apply. Without **Play**, an app can browse but not
  play. A view-only account can play but not change playlists or stars.
- On mobile data, set the app's streaming quality (for example 128 kbit/s).
  Usherr makes those tracks smaller on the fly, and sends files that already
  fit as they are. An administrator can change the format and top bitrate in
  the player's settings (the sliders button in the player, **Advanced**).
- An administrator can switch phone apps off for everyone in the same place.
- Usherr only answers at home and on Tailscale. Don't open it to the internet
  for this.

## Last.fm

**Optional.** Each person can send what they play to their own Last.fm
account, from the web player and from phone apps.

1. An administrator makes an API account at
   [last.fm/api/account/create](https://www.last.fm/api/account/create) (any
   name and description; leave the callback empty), then pastes its **API
   key** and **Shared secret** under **Settings → Services → Last.fm**.
2. Each person opens **Settings → Services → Last.fm**, clicks **Connect
   Last.fm**, approves Usherr on last.fm, and comes back connected.
3. Optional: **Love what I like** loves a track on Last.fm when you give it a
   heart in Usherr.

A track counts once it is longer than 30 seconds and you have heard half of
it, or four minutes. Plays made while Last.fm can't be reached are kept and
sent later (up to two weeks old).

## Notifications

**Optional.** Messages when something is added, downloaded or fails, by email,
ntfy, Discord, Telegram and others, under **Settings → Connections →
Notifications**. Each person can also choose their own under
**Settings → Your profile**.

## Reaching Usherr away from home

Usherr is meant to sit behind your home network. To use it from your phone
away from home, use a private network such as Tailscale (nothing opened to the
internet), or a reverse proxy with HTTPS. Turn on two-step sign-in or passkeys
under **Settings → Account & Access → Security** before opening it up.
