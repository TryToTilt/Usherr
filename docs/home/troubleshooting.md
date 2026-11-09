# When something goes wrong

Start with **Server › Setup and health › Logs and errors** (in Classic or Neu:
**Settings → System → Logs and errors**): Usherr lists the errors
it noticed, in plain words, with what to do about each. The Dashboard's
*to fix* count opens the same list. For a failed download, **Explain this** on
its Activity entry asks the AI what went wrong (when the AI is set up).

## Usherr doesn't open after a Windows update

A Windows update of WSL (the part of Windows that Docker Desktop runs in) can
stop Docker's virtual machine, and Docker Desktop doesn't always start it again
on its own. Every container is down; apps that run outside Docker (such as a
Jellyfin installed on Windows) keep working.

- **Fix:** restart Docker Desktop (from its tray icon, or `docker desktop
  restart` in a terminal). Containers with a restart policy start by themselves.
- **Then check:** in an administrator PowerShell, `Get-Service WSLService`
  should say *Automatic*; an update can briefly set it to *Disabled*.
- **Avoid it:** in *Windows Update → Advanced options → Active hours*, cover the
  hours the server is used (up to 18). Windows then installs updates and
  restarts only outside them.

## Restarting Usherr in the middle of an import

Restart or update Usherr only when **Activity** shows nothing downloading,
importing, unpacking or verifying. A restart part-way through an import can
leave a finished download unimported, or tidy it away as a leftover.
**Server › Setup and health › Server and updates** (**Settings → System →
Server** in Classic or Neu) can pause downloads first.

## No sound in the browser

Films with Dolby (E-AC3, Atmos), TrueHD or DTS sound play silently in Chrome
and Edge. Usherr notices within a few seconds and plays the sound converted
beside the picture; the picture isn't touched. If it stays silent:

- Check the browser tab isn't muted, and the player's own volume.
- A file played from the library is converted by Jellyfin when it's
  connected (**Server › Watching › Media servers**; **Settings → Connections →
  Media servers** in Classic or Neu), else by Usherr.
- **Play on TV** or the Jellyfin app play these sounds as they are.

## A stream plays sound but no picture

The release is probably AV1, which some TV boxes (the NVIDIA Shield among
them) can't decode in hardware. Pick a release in H.264 or HEVC instead, or
leave AV1 out with a quality profile (**Server › Libraries › Quality**; **Settings → Library → Quality** in
Classic or Neu).

## Streams start slowly, or stop

- Too few Usenet connections are left for streaming. Each stream needs several;
  if Usherr, StreamNZB and other apps share one provider, split the
  connections between them (see [Usenet and indexers](integrations.md#usenet-and-indexers)).
- Downloads running at the same time take bandwidth and connections:
  **While someone is watching** (on Media servers: Server › Watching, or
  Settings → Connections) pauses
  them while anyone is watching in Jellyfin.
- A second provider as backup fills in articles the first one is missing.
- With StreamNZB as the engine, **Test it** on the Streaming (StreamNZB) card
  says whether Usherr can reach StreamNZB at all.

## The AI is slow, or says it can't be reached

- **Ollama:** the first answer after a quiet spell loads the model, which can
  take up to a minute. Check Ollama is running, and that the address on the AI
  card works from inside Docker (`host.docker.internal`, not `localhost`).
- **Claude:** check the key on the AI card with **Test it**; a key with no
  credit left is refused.
- The family filter's estimates and news summaries keep working on titles that
  already have answers; new ones wait until the AI answers again.

## Jellyfin doesn't show new downloads or streamed titles

- **Media servers** (**Server › Watching** in Broadcast, **Settings →
  Connections** in Classic or Neu): test the connection; check the
  folder mapping if Jellyfin sees your folders under other names.
- For streamed titles, check the *Streamed library* card (**Server › Getting media › Streaming** in Broadcast,
  Settings › Extras in Classic or Neu): the folder
  must be mapped into Usherr at `/data/streamed`, and be part of a Jellyfin
  library.

## Plex says a streamed film or show is unavailable

Plex reads streamed titles from the [virtual drive](integrations.md#virtual-drive-for-plex)
(`U:` to start with). "Unavailable" means Plex can't open it right now.

- **On the Plex computer, open the drive.** If it opens slowly or not at all,
  rclone can't reach Usherr. The usual reason is that a restart gave the
  computer a new address on your network, and rclone still has the old one.
- **Plex on the same computer as Usherr:** on the drive card (**Server › Getting media › Streaming**, or Settings ›
  Extras in Classic or Neu), set
  **Plex runs on** to *This computer*, run the setup command again, and mount
  the drive again (or sign out and in, with the startup file). rclone then
  uses `127.0.0.1`, which never changes.
- **Plex on another computer:** reserve the Usherr computer's address in your
  router (see [Usherr's address on your network](integrations.md#usherrs-address-on-your-network)),
  put the current one in that card, then run the setup command again and
  mount the drive again.
- **Something not right?** in Show me how checks it: it warns when Plex was
  asked to look at a new title and nothing came to read the drive.
- Titles come back by themselves once the drive opens, as long as **Empty
  trash automatically** is unticked in Plex.

## Discover's "Hide what I have" still shows something you have

It hides what's on disk, what's streamed and what's wanted, as you choose in the
menu next to the switch. What the media server has is read every few minutes,
so a title added to Jellyfin outside Usherr shows up there a little later.

## A comic download says "the library has no #22 to file it as"

The release is a different edition from the one in your library. Manga is
often sold in two: Vagabond comes as 37 standard volumes and as 12 VizBig
volumes of three each, and "Vagabond Volume 022" is a standard volume.

- Add the other edition too (**Comics → Add**, the same series from the same
  publisher, with the other number of volumes). A release that names a volume
  one edition doesn't have is then filed in the edition that has it.
- Then **Retry** the failed download in Activity: it uses the files already
  downloaded, and files them in the right edition.
