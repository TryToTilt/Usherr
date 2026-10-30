# The pre-show

Before a film, Usherr can play a short show, like a cinema's: a welcome,
*Please silence your phones*, trailers for what's coming, your own clips (a
sound logo, a holiday bumper, an ad from the old days), the countdown and
*Feature presentation*. It plays:

- on the **Showtime** screen before a booked film, timed so the feature starts
  on the minute;
- before any film you play in Usherr's player, if you turn that on for your
  browser;
- in the **Jellyfin app**, on every TV and phone, through the NeXroll Intros
  plugin (see [In the Jellyfin app](#the-pre-show-in-the-jellyfin-app)).

The Marquee follows along: it shows "Coming soon" with the trailer's poster,
the rating, the countdown and the feature as each piece plays.

**Skip** and **Skip the pre-show** are always on screen. A film picked up
part-way starts without a pre-show.

Everything is on **Settings → Movie night → Pre-show** (`/preshow/manage`),
for administrators.

## Nothing to set up

Two themes come ready: **Movie night** and **Quick start**. Press
**Watch a sample** to see tonight's theme without a film. Clips, schedules and
the Jellyfin app are all optional.

## Themes

A theme is the pre-show, slot by slot, in order. Each slot plays one of:

| Slot | What plays |
|---|---|
| A card | Welcome (with the film's poster and your theater's name), Grab your popcorn, Please silence your phones, Coming attractions, a Coming soon reel of posters, an announcement in your own words, a QR code for Usherr, the rating, the countdown, Feature presentation. |
| Clips from a category | One or more clips from a category (a folder). Least lately played first, or **In order** by name. |
| One clip | A clip you pick. |
| The holiday's clip | A clip from the category named after the holiday whose season it is; nothing outside a season. |
| Coming-soon trailers | Trailers of what's coming, each after a *Coming soon* card with its poster and date. |

**Only when…** limits a slot to some films:

- **Sound**: Dolby Atmos, DTS:X. **Picture**: 4K, HDR, Dolby Vision, IMAX.
  With both ticked, pick **Any ticked sound or picture is enough** (good for
  files whose labels aren't all there) or **both**.
- **Film**: kids' films (G, PG) or not; a booked Showtime or just pressing
  play.
- **Season**: only in chosen holidays' seasons, or any holiday's.
- **Genres** and **Hours**.

A Dolby or THX sound logo before films with Atmos, or a Halloween
"Presented by" only in October, are both a slot with **Only when…**.

A Showtime's pre-show fits its minutes: when it would run long, trailers go
first (the last one first), then clips. Cards always stay.

## When each theme plays

A Showtime can pick its own theme. Otherwise the schedule decides, then the
default theme (**Make default**). A schedule plays a theme:

- in a holiday's season (Halloween, Christmas and others are built in, with
  their dates shown under **Settings → Holidays**);
- between two dates every year (month-day, such as `12-01` to `12-31`);
- on some days of the week.

A holiday beats dates, and dates beat days of the week.

## Clips

Your own videos live in the pre-show folder: `/data/preshow` when a folder of
yours is mapped there, otherwise `preshow` in Usherr's config folder (which,
in a Docker volume, your computer's file browser may not show). Change it
under **Settings → Clips folder**, as Usherr sees it. To drop clips in from
your computer, map a folder in Usherr's compose file and restart Usherr:

```yaml
    volumes:
      - C:/Usherr/Preshow:/data/preshow
```

Usherr starts a new clips folder with the usual categories (Bumpers, Policy,
Commercials, Sound, Intermission, Halloween, Christmas), and the first time it
uses a mapped folder it copies the clips from the one in its config over. One
folder level is the category:

```
preshow/
  Bumpers/
  Policy/
  Commercials/
  Sound/
  Halloween/
  Christmas/
  Presented by/
```

- **Upload clips** puts files into the category typed beside it. Or copy files
  into the folder: Usherr finds them by itself (or press **Look in the folder
  again**). A file still being copied is left alone until it has stopped
  changing for 20 seconds, so a clip never plays cut short.
- A category named after a holiday plays in that holiday's season.
- Untick a clip to leave it out; **Remove** moves the file to `.removed` in
  the folder, so it can be put back.
- MP4 (H.264 and AAC) plays everywhere. A clip in another container, such as
  MKV, is best converted first.

## Coming-soon trailers

Usherr downloads the trailers of what's on its way, so they play at once, in
the Jellyfin app too:

- films you want (on the Wanted list),
- your shows' new seasons,
- the most talked-about films coming to cinemas, and a few showing now.

A trailer is fetched from 60 days before the release, a few at a time every
4 hours, the household's own and the soonest first. It is removed 14 days
after the release, or as soon as the film is in your library, and the oldest
go first when the space limit (10 GB) is reached. **Keep** holds one past
both.

Under **When they come and go**: how early and how late, the space limit,
the quality (**4K, or the best each trailer has**, 1440p, 1080p or 720p) and
the folder. A 4K trailer is about 200 MB, a 1080p one about 50 MB; many
trailers only go up to 1080p.

The trailers folder is `/data/trailers` when it is mapped into the container,
otherwise `trailers` in Usherr's config folder. Put it on a disk with room,
not on a streamed or network-mounted one:

```yaml
    volumes:
      - /path/with/room/Trailers:/data/trailers
```

Downloading needs yt-dlp, which is in Usherr's image. Without it, Usherr's
player plays trailers from YouTube instead (without captions) and the
Jellyfin app plays none.

## The pre-show in the Jellyfin app

The same pre-show plays before films in every Jellyfin app (TV, phone,
browser), using the **NeXroll Intros** plugin: Jellyfin asks Usherr what to
play, and Usherr answers with your clips, the downloaded trailers and the
cards made into short videos in the Marquee's colours. YouTube trailers are
left out there.

Under **Pre-show → In the Jellyfin app**:

1. **Set it up** adds the plugin's repository to Jellyfin and installs
   NeXroll Intros.
2. **Restart Jellyfin** loads it (the button waits until nobody is playing).
3. Usherr then gives the plugin its address and key by itself.

Then choose **Before films** (on), **Before episodes** (off; every episode,
so best with a short theme) and the theme (or *As everywhere*).

Each Jellyfin app plays intros when **Cinema mode** is on in its own
**Settings → Playback**, which it is unless someone turned it off.

While the pre-show plays, Usherr reads the start and the end of the film
through Jellyfin, so a streamed copy is already fetching when its turn
comes.

**By hand**: in Jellyfin, **Dashboard → Plugins → Repositories**, add the
NeXroll Intros repository, install **NeXroll Intros** from the Catalog and
restart Jellyfin. In the plugin's settings, put Usherr's address (as Jellyfin
reaches it) as the NeXroll server and the key from **Address, key, and
setting it up by hand** as its API key. **New key** replaces the key; Usherr
gives the plugin the new one when it can.

The plugin's own request is `GET /plugin/intros?media_type=Movie&item_id=…`
with the key in `X-Api-Key`; each piece comes from a signed
`/plugin/stream/…` link.

## Troubleshooting

- **A slot never plays.** Look at its **Only when…**: with sound and
  picture both ticked, try **Any ticked sound or picture is enough**. The
  Jellyfin app judges sound and picture by the copy Jellyfin plays.
- **Nothing plays in the Jellyfin app.** The steps under *In the Jellyfin
  app* say what's left; **Jellyfin last asked at …** shows the last request.
  Check that Cinema mode is on in that app.
- **Trailers stay waiting.** They're fetched a few at a time every 4 hours;
  **Look now** starts a round. A trailer that fails three times stays
  *Not downloaded*; **Try again** tries once more.
