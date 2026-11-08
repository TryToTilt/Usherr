# Running the server

The parts an administrator uses to keep an eye on things and keep the
library tidy. Each one does the job of a separate app, so those can go:
Streamystats or Tautulli (Watching), a watchdog container (Service checks),
Maintainerr (Clean-up rule sets), and Radarr's and Sonarr's own profile and
tag pages.

In the new layout (**Settings → General → Look → Neu, new layout**), the
**Server** area gathers these with what needs you and every settings group.

## Watching

**Settings → System → Watching.** Usherr keeps its own watch history from
Plex, Jellyfin and Emby. It asks the servers what is playing every half
minute (every minute when nothing is) and keeps each playback: who, the film
or episode, the app and device, at home or away, direct play, direct stream or
transcode and why, the picture and sound before and after, how long it was
really watched (pauses don't count) and how far it got (90% counts as
finished).

- **Now playing**: live, with progress, how it plays and the bandwidth.
- **History**: filter by person, server, title, device, how it played and
  dates.
- **Stats** for 7, 30 or 90 days or a year: hours per day and per person, top
  movies and shows, apps and devices, how much played directly, the most common
  reasons for transcoding, and the busiest hours and days.

People are matched to their Usherr account when they sign in with Jellyfin or
Plex, or have the same name. A film's or show's page adds who watched it to
its *Watched* line. Members see only their own history unless you switch on
**Members see everyone's history and stats**.

The history is kept for a year to start with (30 days up to forever; older
playbacks go once a day). Players' IP addresses are only kept if you switch
**Keep each player's IP address** on, and **Clear addresses** removes them.

## Service checks

**Settings → System → Service checks** (also a tile in the Server area).
Usherr asks the addresses you list every so often and shows whether each is
answering, since when, its response time, the latest checks as a strip, uptime
over the last 24 hours and 7 days, and its outages.

- **Add a service**: Jellyfin, Plex, your download clients, Prowlarr or any
  web address. Usherr suggests the media servers, download clients, Prowlarr,
  FlareSolverr and add-ons it already knows.
- For each: **What counts as working** (200 only, any 2xx or 3xx, or any
  answer), an optional **word the page must contain**, how often to check, and
  how many **failed checks in a row before a message** (3 to start with, so a
  restart or a blip stays quiet). **Check this service** tests an address
  before you save it.
- **Messages**: tick **Service down** and **Service back** on a notification
  (Settings → Notifications). You get one message when a service stops
  answering and one when it's back.

Leave user names and passwords out of the address. A key in the address is
fine: only administrators see it, and it never goes in a message.

## Clean-up rule sets and Leaving soon

**Settings → Connections → Media servers**, under *What's been watched*. Rule sets go
further than the basic clean-up rules (which keep working as they did).

1. **Add a rule set** for the movies, the TV shows or one library folder.
   Conditions are joined by *all* or *any*: watched more than so many days ago,
   never watched, nobody watched it lately, watched completely, files bigger
   than so many GB, released before a year, has or doesn't have a tag, added or
   requested by someone, nobody requested it, quality, and the show has ended.
   **Simple** shows the three common rules; **Advanced** shows every condition.
2. For shows, choose whether a rule acts on **each episode**, **a whole
   season** once it's watched, or **the whole show**.
3. Choose what happens: **delete the files and stop looking for it** (the
   default), **only stop looking for it**, or **delete the files, keep looking**
   (download again later). The recycle bin applies when it's on.
4. **Preview the next run** lists exactly what would go on Leaving soon and
   what would be acted on now, with the reason for each. Then **Switch on**.

**Leaving soon** (Upcoming → Leaving soon): a title that matches goes on this
list with a date first (14 days unless you change it), and is only acted on
when that date comes, if it still matches then. Everyone can see the list.
Administrators can let members press **Keep it**, which keeps the title for
good; who kept it is shown and written to Activity. Leaving soon can also show
as a collection on Plex, Jellyfin or Emby (a switch for each server).

Safe by default:

- Every rule set is off until you switch it on.
- One run acts on at most 50 titles (a setting); the rest wait.
- Titles with a keep tag, and kept titles, are never touched.
- **Streamed titles are never touched**: `.strm` files, the Stream folders and
  links into the streaming drive.
- Nothing is done unless what's been watched was read from every media server
  in the last two days.
- Every action is listed under Activity.

## Quality profiles: Simple and Advanced

**Settings → Library → Quality**, the switch beside *Quality profiles*.

- **Simple**: pick a quality (**Save space**, **HD**, **Best HD**, **4K** or
  **4K, else HD**), set the largest file with the slider or by typing any size
  up to 1000 GB (or no limit), and three switches. A list shows which example
  releases the profile would take, and why. Settings made in Advanced are kept.
- **Advanced**: everything a profile has, plus sizes per quality, the smallest
  score gain worth an upgrade, stopping upgrades some days after release, TRaSH
  score sets in one step, preferred and never-taken release groups, patterns
  between slashes in the release-name rules (like `/AMZN|NF/`), the profile's
  own audio languages, which indexers it searches, Usenet or torrents only,
  release age limits, and **Export as JSON**.
- Both have **Try a release name**: it judges any release name against the
  profile as it stands in the form, with the same checks a search makes.

**Custom formats from Radarr and Sonarr** (Settings → System → Move from
other apps, tick *Custom formats*) come over ready to score in Usherr's own profiles. Their profiles
stay behind. Importing again updates the copies instead of adding more, and a
format both apps have under one name is kept as two, the TV one named
"*name* (TV)".

## Tags that do something

**Settings → Library → Automatic searching**, **Tags**.

- **Auto-tagging**: a rule gives its tag to every movie or show that matches
  it, by genre, years, the language it was made in, movie or show, and library
  folder. It can **take the tag off titles that stop matching**. Rules run when
  saved, from **Tag the library now**, and every few hours for new titles.
- **Tag rules**: titles with a tag search only the indexers ticked for it, or
  only Usenet or torrents, in automatic searches. With a profile that limits
  indexers too, only indexers both allow are used.

Tags still become collections in Plex, Jellyfin and Emby.

## Friends sign in with Jellyfin or Plex

**Settings → Account & Access → Ways to sign in → Sign in with Jellyfin or
Plex** (off until switched on).

- **Sign in with Jellyfin**: the sign-in form checks the name and password with
  your Jellyfin server.
- **Sign in with Plex** adds a button. Only Plex accounts your Plex server is
  shared with get in.

The first sign-in makes a member (or viewer, as you choose) account, found
again later by the Jellyfin or Plex account's own id. Names that have a
password in Usherr keep signing in with it, and two-step sign-in still
applies.
