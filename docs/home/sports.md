# Sports

**Sports** in the menu shows the games of the coming two weeks (and the last
two days, to catch up), the leagues in your library and the teams you follow.
It runs on Usherr's own engine (leagues and games from TheSportsDB; Usherr
streams and downloads the games itself) or on Sportarr (which keeps the
leagues and downloads the games). Set it up in **Settings → Extras → Sports**
(see [Setting up each part](integrations.md#sports)).

## Finding a league or a team

Type a name in the search box at the top of the Sports page: *NFL*, *Premier
League*, *UFC*, *Formula 1*, *Chiefs*. Leagues and teams come back with their
badges.

- **Add** puts a league in your library: its seasons and games appear in a
  minute, and nothing is downloaded until you ask.
- **Add and get new games** does the same and gets every new game of the
  league from now on.
- **Follow** a team (team sports). With Sportarr, it finds every competition
  the team plays in and watches only its games. On Usherr's own engine, the
  **download** button next to a followed team gets its games in the leagues
  you have added (add the league the team plays in; the league itself doesn't
  need to get its games).

A league already in your library shows **Open**; a team you follow shows
**Following**.

## Games

Games are listed by day with their time, league, round, venue and the channel
that shows them, and a badge when a game is downloaded (with its quality),
downloading or wanted. **Get** on a game wants it: it is searched for at once
when it has been played, otherwise once it has.

A league's page (click it on the Sports page) lists one season at a time:
**Coming up**, **Played** or the **Whole season**.

Scores are hidden so they don't spoil a game you'll watch later; **Show
scores** turns them on (each device remembers it).

## Downloading games (Usherr's own engine)

Usherr downloads games with the indexers and download clients it already uses
for films and shows (its own Usenet downloader, SABnzbd, NZBGet or torrents),
so no other app is needed. It needs a **Sports folder** (Settings → Extras →
Sports; empty means *Sports* next to your TV folder when that sits in a media
folder, like `/data/media/tv`; a TV folder mounted on its own, like `/tv`,
needs a Sports folder mounted and entered).

### What a league gets

A league's page has a **Downloads** box (for people who may manage the
library):

- **Get its new games**: every game from now on is wanted. Games already
  played aren't, unless you **Get** them one by one.
- **Simple** asks the common question for the sport:
  - fight cards (UFC, Bellator, PFL): *the whole card*, *main card and
    prelims* or *main card only*;
  - race weekends (Formula 1, MotoGP, NASCAR): *races only*, *races and
    sprints*, *races, sprints and qualifying*, or *everything, practice too*;
  - team sports: *every game*, or *only the games of the teams I follow*.
- **Advanced** has every choice: each part or session on its own, *only the
  games of the teams I follow*, and the league's **quality profile** (one of
  your own profiles; *Sports' default* is the one set in Settings, else the
  one TV shows use).

**Save** applies the choices to the coming games at once. A game you choose by
hand (**Get**, or the **×** that stops getting it) stays as you chose.

### When Usherr looks

A wanted game is searched for about three hours after it starts (it has to
end, and be posted), then every few hours, then once a day, for two weeks
(**Advanced → Keep looking for a game for**). Each search stops at the first
query that finds the game, and the automatic searches add at most ten
downloads an hour, like everything else: your indexers' daily limits are
spared. Switch off **Look for wanted games by itself** to search only by hand.

On a played game:

- **Search now** looks at once and downloads the best release.
- **Pick** lists every release your indexers have for it, the game's first,
  with its quality, size and indexer, and why the others aren't taken (*it's
  from another day*, *it's the prelims, not the main card*, *the quality
  profile doesn't take it*). **Download** gets the one you choose. Type your
  own words to search differently, and tick *Show the ones that aren't this
  game* to see everything. For a fight card, choose the part first.
- **×** stops getting it.

The Sports page's **Wanted** list shows the played games that are wanted and
have no file yet, and whether Usherr still looks for them by itself. Wanted
games are on the **Calendar** too (filter *Sports*), and their downloads show
in **Activity** under the game's name, with **Retry** as for anything else.

### How Usherr tells a game's release

Release names are read the way sports releases are named:

- the league (*NFL*, *EPL*, *Formula1* or *F1*, *UFC*...),
- the date (*2026.09.28*, *28.09.2026*, *12th Oct 2025*; American games carry
  the US date),
- the round or week (*Round15*, *R15*, *Week 4*, *Matchweek 10*),
- the teams (*Eagles vs Bears*, *Leafs @ Canadiens*; a city or a nickname is
  enough when the date is there),
- a fight card's number and part (*UFC 300*, *Fight Night 288*, *Prelims*,
  *Early Prelims*, *PPV* for the main card),
- a race weekend's session (*Race*, *Sprint*, *Qualifying*, *FP1*),
- and the quality (*1080p WEB*, *720p HDTV*, *1080p50*).

A release is taken only when it is the game: the same league, the same day (a
day either way for time zones) or round, both teams, the card's number, the
same part or session. Build-up shows, highlights, press conferences and
notebooks are never taken. Among the right releases, your quality profile
decides, as for an episode.

### Where the files go

Games are filed the way Plex and Jellyfin read a TV library, like Sportarr:

    Sports/NFL/Season 2026/NFL - S2026E05 - Philadelphia Eagles vs Chicago Bears - 1080p WEB.mkv
    Sports/UFC/Season 2026/UFC - S2026E07 - UFC 300 Pereira vs Hill - Prelims - 720p WEB - pt2.mkv

The league is the show, the season's year the season, the game's place in the
season the episode. A fight card's parts share an episode and end in *pt1*
(early prelims), *pt2* (prelims) and *pt3* (main card), so Plex plays them in
order. Usherr writes a `tvshow.nfo` for each league and an `.nfo` for each game
(its name, day, teams and venue), which Jellyfin reads. A better copy of a game
replaces the earlier one. Jellyfin and Plex are told to scan, as for an
episode.

Add the Sports folder to Plex or Jellyfin as a **TV shows** library.

## Moving from Sportarr

1. In **Settings → Extras → Sports**, choose **Usherr's own engine**, set the
   **Sports folder**, the **Quality for games**, and save. A folder of its own
   is simplest while you compare. It can also be the folder Sportarr fills:
   Usherr only replaces a file there that has exactly the name it would give
   its own copy, and only with a better one; it never deletes Sportarr's other
   files.
2. Open **Bring my followed leagues from Sportarr** and press **Bring them
   over**. Usherr reads Sportarr's leagues (with whether each gets its new
   games) and its followed teams. It only reads: Sportarr, its profiles and its
   files stay as they are, and Sportarr's profiles are never copied (set the
   quality on each league's page, or use the default).
3. On each league's page, check the **Downloads** box: the parts or sessions,
   the teams, the quality.
4. Let both run side by side for a while and compare: the games Usherr gets
   (Activity, the Wanted list) against Sportarr's. To avoid downloading
   everything twice meanwhile, switch off Sportarr's monitoring (or Usherr's
   automatic search) for the leagues you are comparing.
5. Only when you're happy, stop Sportarr. Your files stay where they are.

Before retiring Sportarr, check that: each league Sportarr got is in Usherr
and gets its new games; a recent game of each sport came in through Usherr
(the right release, the right part or session, the quality you want); Plex or
Jellyfin show the Sports library; and nothing else (Prowlarr's app list, a
script) still talks to Sportarr. Remove Sportarr from Prowlarr's apps too.

## Streaming a game

**Stream** on a game that has started or been played plays it in Usherr's
player, without downloading, like a film: the release that best matches the
game, at your device's quality, with *Try another release* in the player if it
doesn't start. With Sportarr, the release is the one Sportarr matched to the
game (its sport-by-sport rules); on Usherr's own engine, Usherr searches your
indexers for the league with the game's date or teams. A game's search is
remembered for 20 minutes, to spare the indexers' daily limits.

The Marquee can show the day's games too: a scoreboard, tonight's games, a
score ticker and a kick-off countdown (see [The Marquee](marquee.md#sports)).

The Dashboard's **Sports** card counts the games downloaded and streamed, the
leagues, and the games wanted.

## Who can do what

Everyone can look. **Stream** needs the *Play* permission; adding a league,
following a team, **Get**, **Search now**, **Pick** and a league's
**Downloads** need *Manage the library* (**Settings → Accounts**). Stopping
following a team (the **×** next to it) keeps its games and files.

Sports artwork comes through Usherr, so it shows on every device the same way.
Usherr's Sports connector talks to Sportarr through Sportarr's own API; Sportarr
is GPL-3.0 software by its own authors, and Usherr's sports release rules
follow Sportarr's, rewritten. League, team and game data on Usherr's own engine
comes from TheSportsDB (see the credits).
