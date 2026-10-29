# Sports

**Sports** in the menu shows the games of the coming two weeks (and the last
two days, to catch up), the leagues in your library and the teams you follow.
It runs on Sportarr (which keeps the leagues and downloads the games) or on
Usherr's own engine (leagues and games from TheSportsDB). Set it up in
**Settings → Extras → Sports** (see [Setting up each part](integrations.md#sports)).

## Finding a league or a team

Type a name in the search box at the top of the Sports page: *NFL*, *Premier
League*, *UFC*, *Formula 1*, *Chiefs*. Leagues and teams come back with their
badges.

- **Add** puts a league in your library: its seasons and games appear in a
  minute, and nothing is downloaded until you ask.
- **Add and get new games** (with Sportarr) does the same and gets every new
  game of the league from now on.
- **Follow** a team (team sports): Sportarr finds every competition the team
  plays in and watches only its games.

A league already in your library shows **Open**; a team you follow shows
**Following**.

## Games

Games are listed by day with their time, league, round, venue and the channel
that shows them, and a badge when a game is downloaded (with its quality) or
wanted. **Get** on a game asks Sportarr to watch for it, and to search for it
at once when it has been played.

A league's page (click it on the Sports page) lists one season at a time:
**Coming up**, **Played** or the **Whole season**.

Scores are hidden so they don't spoil a game you'll watch later; **Show
scores** turns them on (each device remembers it).

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
following a team and **Get** need *Manage the library* (**Settings →
Accounts**). Stopping following a
team (the **×** next to it) keeps its games and files.

Sports artwork comes through Usherr, so it shows on every device the same way.
Usherr's Sports connector talks to Sportarr through Sportarr's own API; Sportarr
is GPL-3.0 software by its own authors. League, team and game data on Usherr's
own engine comes from TheSportsDB (see the credits).
