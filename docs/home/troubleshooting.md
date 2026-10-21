# When something goes wrong

Start with **Settings → System → Logs and errors**: Mediarium lists the errors
it noticed, in plain words, with what to do about each. The Dashboard's
*to fix* count opens the same list. For a failed download, **Explain this** on
its Activity entry asks the AI what went wrong (when the AI is set up).

## Mediarium doesn't open after a Windows update

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

## Restarting Mediarium in the middle of an import

Restart or update Mediarium only when **Activity** shows nothing downloading,
importing, unpacking or verifying. A restart part-way through an import can
leave a finished download unimported, or tidy it away as a leftover.
**Settings → System → Server** can pause downloads first.

## No sound in the browser

Films with Dolby (E-AC3, Atmos), TrueHD or DTS sound play silently in Chrome
and Edge. Usherr notices within a few seconds and plays the sound converted
beside the picture; the picture isn't touched. If it stays silent:

- Check the browser tab isn't muted, and the player's own volume.
- A file played from the library is converted by Jellyfin when it's
  connected (**Settings → Connections → Media servers**), else by Usherr.
- **Play on TV** or the Jellyfin app play these sounds as they are.

## A stream plays sound but no picture

The release is probably AV1, which some TV boxes (the NVIDIA Shield among
them) can't decode in hardware. Pick a release in H.264 or HEVC instead, or
leave AV1 out with a quality profile (**Settings → Library → Quality**).

## Streams start slowly, or stop

- Too few Usenet connections are left for streaming. Each stream needs several;
  if Mediarium, StreamNZB and other apps share one provider, split the
  connections between them (see [Usenet and indexers](integrations.md#usenet-and-indexers)).
- Downloads running at the same time take bandwidth and connections:
  **While someone is watching** (Settings → Connections → Media servers) pauses
  them while anyone is watching in Jellyfin.
- A second provider as backup fills in articles the first one is missing.
- **Test it** on the Streaming (StreamNZB) card says whether Mediarium can reach
  StreamNZB at all.

## The AI is slow, or says it can't be reached

- **Ollama:** the first answer after a quiet spell loads the model, which can
  take up to a minute. Check Ollama is running, and that the address on the AI
  card works from inside Docker (`host.docker.internal`, not `localhost`).
- **Claude:** check the key on the AI card with **Test it**; a key with no
  credit left is refused.
- The family filter's estimates and news summaries keep working on titles that
  already have answers; new ones wait until the AI answers again.

## Jellyfin doesn't show new downloads or streamed titles

- **Settings → Connections → Media servers:** test the connection; check the
  folder mapping if Jellyfin sees your folders under other names.
- For streamed titles, check the *Streamed library* card in Extras: the folder
  must be mapped into Mediarium at `/data/streamed`, and be part of a Jellyfin
  library.

## Discover's "Hide what I have" still shows something you have

It hides what's on disk, what's streamed and what's wanted, as you choose in the
menu next to the switch. What the media server has is read every few minutes,
so a title added to Jellyfin outside Mediarium shows up there a little later.

## A comic download says "the library has no #22 to file it as"

The release is a different edition from the one in your library. Manga is
often sold in two: Vagabond comes as 37 standard volumes and as 12 VizBig
volumes of three each, and "Vagabond Volume 022" is a standard volume.

- Add the other edition too (**Comics → Add**, the same series from the same
  publisher, with the other number of volumes). A release that names a volume
  one edition doesn't have is then filed in the edition that has it.
- Then **Retry** the failed download in Activity: it uses the files already
  downloaded, and files them in the right edition.
