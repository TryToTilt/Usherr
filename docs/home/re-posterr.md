# Re-Posterr

Re-Posterr is Usherr's artwork picker. It picks the poster, background and logo for each film, show and
collection in Jellyfin or Plex, from TMDB and fanart.tv, and puts them on the
server's titles, so a film looks the same in every Jellyfin or Plex app, on the
Marquee and in Usherr. Only pictures change, never a file, and every change can
be put back.

It's on **Server › Libraries › Artwork (Re-Posterr)** in Broadcast
(**Settings → Re-Posterr** in Classic or Neu; `/settings/re-posterr`), for
administrators. It needs Jellyfin or Plex (Server › Watching › Media servers,
or Settings → Connections → Media servers) and a
TMDB key. With both, a title's pictures go to both.

**With Plex**: posters and backgrounds go to Plex and are locked there, so
Plex's own metadata refresh doesn't swap them back. Plex doesn't keep logos,
so logos (and, for now, styled season posters and title cards) are
Jellyfin's. Undoing a picture on a Plex title that had none before leaves the
new one in Plex.

**The same pictures on both, or different ones.** With Jellyfin and Plex
both, every picture goes to both by default. Turn on **Different pictures
for each media server** (on the Re-Posterr page) and:

- **Choosing by hand**, a title's artwork picker asks **Both servers**,
  **Jellyfin** or **Plex**: the picture goes only where you say, and the
  other server keeps its own.
- **Your style**: the library list also has **Jellyfin: every library**,
  **Plex: every library** and each of Plex's libraries (*Plex: Movies*), so
  each server (or one of its libraries) can have its own poster style. A
  copy takes its library's style, else its server's, else the house style.

## What it picks

For each title, Usherr looks at every picture TMDB has (and fanart.tv, with a
key) and takes the best:

- **Posters and logos** in your language first, then English, then without
  words.
- **Backgrounds** without words first (*Backgrounds without words*, on by
  default), so the Marquee and the pre-show can put the title on a clean
  picture.
- Big enough before small, then the best liked.

Jellyfin gets each at the size a TV tile wants: posters 1000 pixels wide,
backgrounds 1920, logos 800. A film in two libraries (on disk and streamed)
is one title: both copies get the same pictures.

## Nothing changes without your say

**What it does** chooses how much Usherr does on its own:

| Setting | What happens |
|---|---|
| **Ask me first** (the default) | Usherr's picks wait under **Waiting for you**, beside what Jellyfin has now. **Use it** or **Skip** each, or **Use all** / **Skip all**. Nothing reaches Jellyfin until you say so. |
| **Fill in what's missing** | Titles with no poster, background or logo get one by itself. Pictures Jellyfin has stay. |
| **Fill in and replace pictures that are too small** | Also replaces a poster under 600 pixels wide or a background under 1280. |

A skipped pick isn't proposed again, nor is one you put back.

## Choosing one yourself

The **Re-Posterr** button on a film's or show's page (administrators) shows what
Jellyfin has now and every poster, background and logo TMDB and fanart.tv
have, side by side, with each one's size, language and source. Click one and
it goes on at once, on every copy in Jellyfin. A picture chosen by hand is
kept: no nightly look replaces it.

## Your style: posters Re-Posterr makes

Re-Posterr can also make the pictures itself, the way Posterizarr does: it
takes art without words (a textless poster, or a background cut to a
poster's shape) and puts the title on it in your style, so the whole library
matches. It makes three kinds:

- **Posters** for films, shows and collections: the title (and the year, if
  you like), or the title's own logo when it has one.
- **Season posters**: the show's art with "Season 2" (and the show's name or
  logo above it, if you like).
- **Title cards** for episodes: a still from the episode (TMDB's) with its
  name and "Season 1 · Episode 3".
- **Backgrounds**: the wide picture behind a title, art without words with
  the title (or its logo) on it.

The style is set under **Your style** on the Re-Posterr page, one for each
kind:

- **A layout to start from**: Fade (the picture darkens towards the bottom),
  Band (a solid strip), Logo, Centre (big in the middle) or Framed (a border
  and a band).
- **The words**: the font (Bebas Neue, Anton, Oswald, Playfair Display or
  Usherr's own), where they sit (top, middle, bottom), the largest size (long
  titles shrink to fit, on up to two lines), the colour, capitals.
- **Behind the words**: a fade or a band, how strong, how tall, its colour.
- **A border**: its width and colour.
- **More for the words**: a dark outline round the letters, a soft shadow,
  how many lines a long title may take (1 to 4), the wording of season
  posters ("Series {n}") and title cards ("S{s} E{e}"), and collections
  without the word "Collection". A title in another alphabet (Japanese,
  Korean, Chinese, Cyrillic, Greek) is drawn in a font that has its letters.
- **An overlay**: a PNG of your own laid over the whole picture (a frame, a
  fade, a badge; see-through parts show the picture). **Add an overlay…**
  keeps it in the artwork folder under `overlays`. With **Another by the
  copy's resolution**, each copy in Jellyfin gets the one for the size of its
  picture (4K, 1080p, 720p, smaller): a 4K badge on the 4K copy only.
- **A library's own style**: under **For**, pick a Jellyfin library and
  **Give it its own style** (Anime unlike Movies, say); the others keep the
  house style. Each copy of a title gets its own library's.
- **As a file**: **Save as a file** keeps every style in a file; **Load a
  file…** brings them back, here or on another Usherr.

The picture on the right is drawn again with each change, on a title of your
choosing (search your titles under **Try it on**). Nothing goes to Jellyfin
until you save.

To use it:

- **On one title**: the **Re-Posterr** button, **Your style** tab. It shows
  the poster and background made from the best art without words (pick
  another below it, or send **Your own picture…**; type **Your own words** to
  put something else than the title on them), and for a show its first season
  poster and first title card. **Use it**
  puts the poster on; **Make them** makes every season poster or every title
  card, in the background. A long show's title cards take a while.
- **On the whole library**: switch on **Make these in the nightly look** for
  each kind. They then follow **What it does**: with **Ask me first** they
  wait under **Waiting for you** (shown as they'll look); otherwise they go on
  by themselves. A title with no art without words is left as it is. Change
  the style later and the nightly look offers the new look again.

A picture chosen by hand (on the Poster tab or with **Use it**) stays as it
is. Season posters and title cards are put back together, with **Put the old
ones back**.

## In Usherr too

A poster Re-Posterr puts on a title in Jellyfin shows on Usherr's own pages
too (the title's page, rows, searches), kept in the artwork folder under
`posters`. Put the old one back and Usherr shows TMDB's poster again.

## Putting one back

Before Usherr replaces a picture in Jellyfin, it keeps the old one in the
artwork folder (`/data/artwork` when it's mapped into the container,
otherwise `artwork` in Usherr's config folder). **Put the old one back**
(under **Changed lately**, or with the **Re-Posterr** button) returns it; for a title
that had none, the picture is taken off again.

```yaml
    volumes:
      - /path/with/room/Artwork:/data/artwork
```

## When it looks

- **Every night**, between the hours you choose (1 AM to 7 AM by default), a
  title every few seconds, the ones not looked at for longest first. It stops
  for the day at the end of the hours and carries on the next night.
- **Never while someone is watching**: it checks Jellyfin and Plex before it starts
  and every few titles, and waits when something is playing.
- **Not when Jellyfin's disk is low**: Jellyfin keeps the pictures in its
  metadata folder. When that disk has less free than **Keep free on
  Jellyfin's disk** (20 GB by default), nothing is sent in bulk. Jellyfin
  replaces a picture rather than adding one, so most changes take no extra
  room.
- **When a film is booked as a Showtime**, it's looked at straight away, so
  its art is ready before the night.
- **Look now** looks at once instead of waiting for the night (it still
  stops when someone starts watching).

A first look over a big library takes a night or two; after that, only new
titles need looking at.

## Collections

**Server › Libraries › Collections** (**Settings → Re-Posterr → Collections**
in Classic or Neu). Usherr makes collections in
Jellyfin and Plex and keeps them up to date, so Kometa isn't needed for them.
A collection's titles come from:

- one of your **import lists** (Trakt, MDBList, TMDB lists, IMDb, Letterboxd),
- **a TMDB collection** (a film series, like "Alien Collection", by name or
  number),
- TMDB's trending titles,
- **a rule over your library**: genres, years, a minimum rating, added in the
  last so many days, or only the best rated few,
- or titles you pick yourself.

Titles are found in each server's library by their TMDB, IMDb or TheTVDB id.
Jellyfin gets one collection (the kept copy of a film rather than its streamed
one), and each Plex library with matching titles gets its own. Choose
**Jellyfin**, **Plex** or both, a sort title, a poster (the server's own, one
you upload, or one made in your Re-Posterr style), where Plex shows it (the
library's Recommended tab, your Home, friends' Home), whether titles that no
longer belong come out, and whether missing titles are **added to Usherr** as
wanted.

**See what's in it** shows the matches and what's missing before you save.
Collections sync every 12 hours (or what you choose) and on **Sync all now**.
One of the same name already on a server, say one Kometa made, is taken over
instead of made twice. Removing a collection asks whether to delete it from
the servers too; the titles themselves are never touched.

## Poster badges

**Server › Libraries › Poster badges** (**Settings → Re-Posterr → Poster
badges** in Classic or Neu; off until you switch them on).
Small badges on the posters Jellyfin and Plex show: 4K, 1080p or 720p, HDR,
HDR10+ or Dolby Vision, the best sound (Atmos, DTS:X, TrueHD, DTS-HD MA), 5.1
or 7.1, the rating (your media server's, or IMDb's from MDBList scores) and
**New** for titles added lately. Picture and sound badges are for films;
shows get the rating and New.

Choose which badges, the libraries that get them, the corners, a dark or
colourful look and the size, with a live preview of one of your films.

Usherr keeps every original poster and always draws on it, so badges never
pile up. Posters are drawn again when a file or a rating changes (it checks
every hour), and switching badges off, or unticking a library, puts the
originals back. A poster changed another way, by hand or by Re-Posterr,
becomes the new original, and Re-Posterr keeps the poster without badges when
it replaces one.

## TheTVDB

Optional, for shows: a TheTVDB API key (thetvdb.com, your account, API keys)
under **TheTVDB** on the Re-Posterr page adds TheTVDB's posters, backgrounds
and logos to TMDB's and fanart.tv's. **Test it** asks for a well-known show's.

## The full Posterizarr app

Optional. Put in Posterizarr's address (as Usherr reaches it) under
**Posterizarr**: **Test it**, **Open Posterizarr**. **Let Posterizarr make the
posters** has Re-Posterr's style stand aside in the nightly look (it still
fills in what's missing, and the Re-Posterr button works as before).

## fanart.tv

Optional. fanart.tv has many more logos, textless posters and backgrounds,
above all for older films and smaller shows. Make a free account at
fanart.tv, copy a personal API key from your profile, and paste it under
**fanart.tv** on the Re-Posterr page; **Test it** checks it. Usherr keeps it
encrypted with its other keys. Shows are found on fanart.tv by their TheTVDB
id, which Jellyfin usually has; without one, a show gets TMDB's pictures.
