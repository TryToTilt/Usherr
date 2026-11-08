# Linked Usherrs: the Usherr Link Relay protocol (ULR v1)

Two Usherrs that linked with a one-time code talk to each other through the
**Usherr Link Relay** (ULR): a small, blind postman. Every Usherr keeps one
outgoing WebSocket to the relay; nothing reaches into anyone's home, no port
is opened, and nobody's home address is shown to the other side.

The relay only routes **sealed envelopes** between public keys. It sees "key
A sent N bytes to key B at time T", never what. Everything inside is
encrypted and signed end to end.

The Usherr project runs a relay for everyone at `wss://relay.usherr.app`
(Cloudflare Workers, code in [`relay/`](../../relay/)). Anyone can run their
own with the same protocol ([Running your own relay](#running-your-own-relay)).

Code: `internal/link` (both ends), `internal/link/relay` and
`cmd/usherr-relay` (the Go relay), `relay/` (the Cloudflare Worker),
`internal/api/home_link.go` (Settings, Linked Usherrs).

Notation: **b64** is standard base64 with padding; **b64url** is URL-safe
base64 without padding; `||` joins byte strings; times are unix milliseconds.

## Identity

Each Usherr makes, once, an **Ed25519** key (signing) and an **X25519** key
(encryption). They're kept in the settings folder in `link-identity.key`
(permissions 0600), next to `secret.key`, and never in the database. Backups
don't include it: a restore onto a new machine makes a new identity, and
links are made again.

- **Identity** = b64url(Ed25519 public key): 43 characters.
- **Fingerprint** (for people) = the first 16 hex digits of
  SHA-256(Ed25519 public key), in groups of four: `a1b2 c3d4 e5f6 0718`.
  Settings shows it for this Usherr and for each linked one, so two people
  can compare.

## Connecting to the relay

WebSocket to `<relay>/v1/connect`, JSON text frames. A frame with an unknown
field, or that isn't JSON, is answered `{"t":"err","code":"bad"}`.

1. The relay sends
   `{"t":"challenge","nonce":"<b64 32 random bytes>","time":<relay clock, ms>}`.
   The Usherr compares `time` with its own clock; Settings warns when it's
   more than 5 seconds off (screenings start by the clock).
2. The Usherr answers
   `{"t":"hello","id":"<identity>","sig":"<b64 Ed25519 signature of 'ulr-v1-hello' || nonce>","v":1}`
   within 10 seconds. The relay checks the signature, or closes the
   connection.
3. An identity may have at most **3 live connections**. A fourth gets
   `{"t":"err","code":"busy"}` and is closed.
4. The relay says `{"t":"ready","queued":<n>,"v":2}` (`queued` left out when
   0), then hands over what waited for this identity (below) and keeps
   nothing of it. `v` is what the relay speaks: **2** means it knows
   `fanout` (below); a step-1 relay leaves it out, and an Usherr then sends
   a fanout as one `send` per recipient.

Keepalive: the Usherr sends `{"t":"ping"}` every 30 seconds; the relay answers
`{"t":"pong"}`. A connection silent for 90 seconds is closed. The Usherr
reconnects after a drop with a growing wait (1 s doubling to 2 minutes, plus
up to as much again at random).

### Frames

| From the Usherr | What it does |
|---|---|
| `{"t":"send","to":"<identity>","env":"<b64 envelope>","mid":"<id>"}` | Delivers `{"t":"msg","from":"<sender>","env":"…","mid":"…"}` to every live connection of `to`, or queues it. |
| `{"t":"fanout","dests":["<identity>",…],"env":"…","mid":"…"}` | (v 2) One envelope for 1 to **100** different identities: each gets the same ordinary `msg` frame as for a `send` (live, or queued), so an Usherr that only receives needs nothing new. `mid` is required. Answered `ack` when every one got it or has it queued, else `err` `full` (the others still got it). |
| `{"t":"invite.open","box":"<hex 32 bytes>","exp":<ms>,"mid":"…"}` | Opens an invite mailbox for this identity until `exp` (at most 24 h; opening it again after a reconnect never makes it longer). `exp` and `mid` are optional. |
| `{"t":"invite.post","box":"…","env":"…","mid":"…"}` | Delivers `{"t":"invite.msg","box":"…","from":"<sender>","env":"…"}` to whoever opened the mailbox (live, or queued like a message). |
| `{"t":"invite.close","box":"…","mid":"…"}` | Closes a mailbox (only its owner can). |
| `{"t":"ping"}` | `{"t":"pong"}` |

`mid` is chosen by the sender: 1 to 64 of `A-Z a-z 0-9 _ -`. Every frame with
a `mid` is answered `{"t":"ack","mid":"…"}` or
`{"t":"err","mid":"…","code":"…"}`:

| code | means |
|---|---|
| `rate` | too fast: 20 messages a second per identity, burst 60, a `fanout` counting as one for every ten recipients (at least one: a fanout to 100 counts 10, so a host can send one to 100 Usherrs twice a second, or six at once); `invite.post` one every 2 s per connection, burst 5 |
| `size` | the envelope is over 16 KB (decoded), or a `fanout` lists more than 100 |
| `unknown` | no such mailbox (never opened, closed, or older than 24 h), a recipient that isn't an identity, or a frame type the relay doesn't know |
| `full` | the recipient's queue is full (for a `fanout`: one of them), or the sender has 20 mailboxes open |
| `bad` | the frame can't be read, the mailbox belongs to someone else, or a `fanout`'s list is empty, has someone twice or something that isn't an identity |
| `busy` | (no `mid`) a fourth connection |

**Queue** for an Usherr that isn't connected: at most 200 messages and 1 MB
per recipient, each kept 24 hours. Delivered on the next connect and then
forgotten.

## Envelopes

`env` is b64 of:

```
version (1 byte, = 1) || nonce (24 bytes) || XChaCha20-Poly1305 ciphertext
```

The sender's keys aren't in it: the receiver knows them from the link.

**Key** = HKDF-SHA256(ikm = X25519(my private, their public),
salt = the two Ed25519 public keys sorted bytewise and joined,
info = `"ulr-v1-env"`), 32 bytes. Both sides get the same key. A low-order
X25519 key (all-zero shared secret) is refused.

**Plaintext**:

```json
{"m": {"type":"link.ping","id":"<uuid v4>","at":<ms>,"body":{…}}, "sig":"<b64 Ed25519 signature of the bytes of m>"}
```

`m` is signed exactly as it appears. The receiver, in this order:

1. drops the envelope unless the sender is a linked Usherr (an answer to a
   join is the one exception, below);
2. decrypts it, reads it strictly (no unknown fields, a type of lower-case
   letters and dots, a UUID id, a JSON object body) and checks `sig` against
   the linked Ed25519 key;
3. drops it when `at` is more than **10 minutes** from its own clock;
4. drops it when its type isn't registered (before any handler runs), or
   its body is bigger than the type allows;
5. drops it when the same sender's `id` was seen in the last **48 hours**;
6. only then hands it to the type's handler.

Nothing that's dropped is logged with its content.

### Message types

| type | body | does |
|---|---|---|
| `link.hello` | `{"name","version","caps":[types]}` | sent after linking and on connecting (at most every 12 h): the name the other side shows, its version and the types it knows |
| `link.rename` | `{"name"}` | this Usherr's name changed |
| `link.unlink` | `{}` | the link ends: the receiver forgets the sender's keys at once |
| `link.ping` | `{"echo"?: "<id>"}` | answered with a `link.ping` whose `echo` is the ping's id |

Names are shown as plain text: control characters are taken out and they're
cut to 60 characters.

The list is fixed in code (`link.Registry`); step 2 adds the screening
types below, step 3 `playback`, step 4 `chat.*` and `react.*`, and step 5
`reward.stub` and `reward.stamp`, Crews the `crew.*` types, and the party
`lobby.*`, `room.status`, `room.intermission` and `verdict.*`. No message can
ask for a file, a library listing or settings.

### Group envelopes (screening keys)

A host gives each Usherr it invites to a screening a random 32-byte
**screening key**, inside their own (pair) envelope. A message meant for
everyone at the screening is then sealed **once** with that key and sent as
one `fanout` frame:

```
version (1 byte, = 2) || key id (16 bytes) || nonce (24 bytes) || XChaCha20-Poly1305 ciphertext
```

- key id = the first 16 bytes of SHA-256(`"ulr-v1-kid"` || key), so the
  receiver finds the key without trying them all;
- associated data = `"ulr-v1-group"` || key id;
- the plaintext is the same signed `{"m","sig"}` as a pair envelope, signed
  with the **host's** Ed25519 key.

The receiver takes one in only when: it was given that key, the relay's
`from` is the key's owner (the host), and the signature is the host's; then
the same steps as above (time, type, size, replay). A type comes either only
in pair envelopes or only in group envelopes; one that arrives in the other
kind is dropped. A guest has the key too, but anything it seals with it is
dropped, because it isn't the key's owner. When a guest leaves (taken off
the list, or unlinked), the host makes a new key and sends it to the
others, so the one who left can't read on.

### Screenings (step 2)

Code: `internal/link/screening` (the messages, their checks and who may send
what), `internal/api/home_link_screenings.go` (what they do in Showtime).
Every body has `"v":1` and is read strictly (no unknown fields); `sid` is
the screening's id, a UUID the host made.

| type | way | envelope | body (largest) | does |
|---|---|---|---|---|
| `screening.offer` | host → guest | pair | `{"v","sid","rev","film":{"tmdb","title","year"?,"runtime"?,"copy"?,"edition"?,"poster"?},"at","preshow","host","key"}` (4 KB) | a screening this Usherr is invited to: TMDB id, title, year, runtime (TMDB), `copy` the host's own copy's minutes, start (unix ms), pre-show minutes (0–60), who's hosting, and the screening key (b64). `poster` only as an `https://image.tmdb.org/` address. |
| `screening.update` | host → guest | pair | the same (4 KB) | it changed (time, pre-show, a new key); `rev` goes up with each change. Sent again until the guest says it saw the latest (`ready`'s `rev`): every 10 minutes, and every hour anyway until it starts. |
| `screening.cancel` | host → guest | pair | `{"v","sid"}` (512 B) | called off, or this guest isn't invited anymore |
| `screening.roster` | host → everyone | group (fanout) | `{"v","sid","rev","guests":[{"name","answer"?,"count"?}]}` (12 KB, ≤ 101 entries) | who's coming: first the host's own household (its Usherr's name, `going` and how many, or `not`; step 3), then each invited Usherr's own name, its answer and how many |
| `rsvp` | guest → host | pair | `{"v","sid","answer","count"}` (512 B) | `going` / `not` / `maybe` for the household, and how many (0–20; 0 for `not`) |
| `ready` | guest → host | pair | `{"v","sid","rev","has","runtime"?}` (512 B) | has the film: `yes` / `getting` / `no`, and its copy's minutes; sent on every offer and update (so it also says the screening arrived) and when it changes |

Who may send what (dropped otherwise, counted as `refused`):

- `screening.offer`, `update` and `cancel` only from the Usherr that hosts
  that `sid`. An offer for a `sid` known from another host, or one this
  Usherr hosts itself, is refused. An update for one not known yet is taken
  as an offer (the offer was lost).
- `screening.roster` only sealed with that screening's key, by its host,
  and with the key's `sid`.
- `rsvp` and `ready` only from a linked Usherr invited to that screening.
- Unlinking removes a link's screenings on both sides at once.

A link sees only the screenings it's invited to.

When anyone at a guest Usherr says `going`, the screening is booked in that
Usherr's own Showtime (the Marquee counts down to it), marked as hosted by
the linked Usherr; it moves with `screening.update` and goes with
`screening.cancel`, unlinking, or when nobody there is going anymore. Only
the host can move or cancel it.

### Playback (step 3)

Code: `internal/link/screening/playback.go`, `internal/api/home_link_room.go`
(the room), `web/src/home/RoomFollow.tsx` and `syncMath.ts` (the player).

| type | way | envelope | body (largest) | does |
|---|---|---|---|---|
| `playback` | host → everyone | group (fanout) | `{"v","sid","seq","state","position","atWallTime","anyonePause"?}` (512 B) | where the film is: at `atWallTime` (unix ms, **the relay's clock**) it was at `position` (seconds into the film; negative in the pre-show), `state` `preshow` / `film` / `paused` / `ended`. `seq` goes up with each change the host makes; heartbeats repeat it. `anyonePause`: the host lets guests ask to pause. |
| `pause.request` | guest → host | pair | `{"v","sid"}` (512 B) | please pause; the host's Usherr pauses for everyone when it lets anyone pause and the film is playing, and drops it otherwise |
| `screening.preshow` | host → everyone | group (fanout) | `{"v","sid","rev","items":[{"kind","card"?,"text"?,"category"?,"tmdb"?,"tv"?,"season"?,"youtube"?,"title"?,"poster"?,"release"?,"why"?,"seconds"}]}` (12 KB, ≤ 80 items, ≤ 60 min in all) | the pre-show's recipe: each piece in order and how long it runs. A card by its kind, a clip by its category only, a trailer by its film (TMDB id, a show's season) and its public YouTube video. Never a file or address of the host's. |

Who may send what: `playback` and `screening.preshow` only sealed with that
screening's key, by its host, with the key's `sid` (a guest has the key, but
anything it seals with it is dropped as `sender`); `pause.request` only from
an Usherr invited to that screening.

**Clocks.** Each Usherr knows how far its clock is from the relay's (the
`time` in the relay's `challenge` at every connect, kept as `skewMs`). The
host sends `atWallTime` = its own time + its skew; a guest takes it as `atWallTime` − its own skew. Then a film
playing is at `position + (now − at) / 1000` on every Usherr's own clock.
Before the host has said anything, the schedule holds: the film at the
booked time, the pre-show just before it.

**The host** sends `playback` whenever whoever booked it (or an
administrator) plays, pauses or seeks, and again every 5 seconds while the
pre-show or film plays (every 15 while paused, once when it ends), so an
Usherr that missed a message, joined late or was offline is right within
seconds. A guest keeps the newest `seq`; while the relay is out of reach it
plays on by its own clock and is put right by the next heartbeat.

**Following.** A player compares where it is with where it should be:
closer than 0.1 s, nothing; up to 1 s off, it plays up to 6% faster or
slower (8% per second off) until it's back; more than 1 s off, it seeks
there (0.25 s ahead). A copy converted on the fly can't seek: it's
converted again from there, at most every 20 seconds.

**The pre-show.** From 15 minutes before it starts, the host fixes its
pre-show (the plan it plays itself) and sends the recipe, again every
minute until the film starts. Each guest builds it from its own cards,
clips of the same category and trailers of the same film (else the
YouTube video); whatever it lacks is a card of the same length (the
coming-soon card of a trailer it hasn't got, else the welcome card). Every
piece has the host's length, so the pre-show starts and ends at the same
moment everywhere; a piece that ends early leaves the welcome card for the
rest of its slot. Moving the screening starts the pre-show and playback
over.

### Chat and reactions (step 4)

Code: `internal/link/screening/chat.go`, `internal/api/home_link_chat.go`,
`web/src/home/PartyChat.tsx`, `PartyOverlay.tsx`, `reactions.ts` and
`PartyPhone.tsx` (the phone page).

| type | way | envelope | body (largest) | does |
|---|---|---|---|---|
| `chat.send` | guest → host | pair | `{"v","sid","id","name","text","spoiler"?,"pos"}` (4 KB) | someone at a guest Usherr said something: plain text, at most 500 characters; `spoiler` blurs it until tapped; `pos` is the second of the film it was said at |
| `chat.msg` | host → everyone | group (fanout) | `{"v","sid","id","name","house"?,"text","spoiler"?,"pos","at"}` (4 KB) | something said at the screening, passed on by the host: `house` is the Usherr it was said at, `at` unix ms |
| `react.send` | guest → host | pair | `{"v","sid","id","name","word","pos"}` (512 B) | a reaction: `word` is `HA`, `EEK`, `WOW`, `NOPE` or `CLAP` |
| `react.moment` | host → everyone | group (fanout) | `{"v","sid","id","name","house"?,"word","pos","at"}` (512 B) | a reaction at the screening, pinned to second `pos` of the film |

Guests are linked to the host, not to each other, so the host's Usherr is
the hub: it keeps what a guest's people say or send and passes it on to
everyone else at the screening, sealed with the screening key (never back
to the Usherr it came from, which has it). `id` is a UUID the sender makes;
each message is kept once, by screening and id.

Who may send what: `chat.send` and `react.send` only from an Usherr invited
to that screening, in the pair's envelope; `chat.msg` and `react.moment`
only sealed with the screening's key, by its host, with the key's `sid`.
Text is shown as plain text only (no markup, no links that open by
themselves): control and format characters are taken out, a line break is
a space. Names are cut to 60 characters.

**Rates.** Each person may send a burst of 6 messages, then one a second,
and a burst of 5 reactions, then one a second, on their own Usherr. At the
host, each guest Usherr (for its whole household) may send a burst of 12
messages, then one a second, and a burst of 20 reactions, then five a
second; anything over is dropped.

**Pinned to the moment.** The host takes a guest's `pos` while it's within
15 seconds of where the room is (else the room's position); after the
screening, someone playing it back pins to where they are. Each Usherr
keeps the last 500 messages and 5,000 reactions of every screening, the
reactions also by film (TMDB id), so they float up again at the same second
for anyone who plays that part later, and a film's most-reacted moment can
be found. Nothing said is written to the logs.

### Rewards (step 5)

Code: `internal/link/rewards`, `internal/api/home_link_rewards.go` (stubs
and stamps), `internal/api/home_rewards.go` (badges).

| type | way | envelope | body (largest) | does |
|---|---|---|---|---|
| `reward.stub` | host → guest | pair | `{"v","sid","film":{"tmdb","title","year"?,"runtime"?,"poster"?},"at","host","seat","count","no","with"?}` (4 KB) | at the door (when the film starts), a block of seats for the guest's people: seats `seat` … `seat+count-1` (1 and up; 0 is the host's), stub numbers `no` … `no+count-1`, numbered by the host. `with`: who else is there (≤ 40 names: the host's household and the other Usherrs). A new block follows when more of its people come once it started. |
| `reward.stamp` | guest → host | pair | `{"v","sid","seats":[n]}` (1 KB, ≤ 80 seats) | these of its seats were stamped (they stayed to the end) |
| `reward.stamp` | host → guest | pair | the same | every seat of that guest the host holds stamped, said back after each guest stamp, so both sides agree |

Who may send what: `reward.stub` only from the Usherr hosting that `sid`.
`reward.stamp` from a guest only for seats the host gave that guest (an
Usherr not invited, or a seat of another guest, is refused); `reward.stamp`
to a guest only from that screening's host. Like every message, both are
signed by the sender's key, so a stub or a stamp can't be made up by anyone
else. Nothing about a screening's stubs goes beyond its own Usherrs.

A guest's Usherr gives a stub to each of its people who said I'm in, in the
order they said so. Stamp it works from the film's start until half an hour
after its end; Usherr's player stamps at 90%. A stamp not yet confirmed is
sent again every 10 minutes for two days.

Badges (first party, five nights, regular, season ticket, perfect
attendance, night owl, house lights, double feature) are worked out by each
Usherr from its own people's stubs, those from linked Usherrs included; they
aren't sent over the link.

### Lobby, intermission and verdict (the party)

Code: `internal/link/screening/party.go` (the messages, their checks and who
may send what), `internal/api/home_link_party.go` (what they do),
`web/src/home/Party.tsx` and `partyLink.ts`.

| type | way | envelope | body (largest) | does |
|---|---|---|---|---|
| `lobby.seat` | guest → host | pair | `{"v","sid","people":[{"id","name","seated"?}]}` (4 KB, ≤ 20 people) | the guest's people who said I'm in, in the order they said so (the order its stubs' seats go in), and whether each has pressed Take my seat. `id` is a person's token (8–32 hex), made by their own Usherr for this screening with its own key, so it says nothing about who they are elsewhere. Sent when someone takes or leaves a seat, and every minute while it changes (again every 5 minutes) from 3 hours before the film. |
| `lobby.roster` | host → everyone | group (fanout) | `{"v","sid","seq","seats":[{"seat","id"?,"name"?,"house"?,"seated"?}]}` (12 KB, ≤ 120 seats) | the seat map: seat 0 is the host's (Row A, Seat 1), eight a row, in the order the stubs are handed out at the door (the host's household, then a block for each guest Usherr in the order they answered); seats already handed out keep their number. A seat with no `name` is held for someone at `house` not named yet. `seq` goes up with each one; an older one is ignored. |
| `room.status` | guest → host | pair | `{"v","sid","id","name","status"}` (512 B) | at the intermission, what someone is up to: `snacks`, `drinks`, `break` or `back` (`""` takes it back). Taken only while the intermission is on. |
| `room.intermission` | host → everyone | group (fanout) | `{"v","sid","seq","on","until"?,"statuses"?:[{"name","house"?,"status"}]}` (12 KB, ≤ 100 statuses) | the intermission is on (until `until`, unix ms, the relay's clock, like `playback`) or over, with everyone's status. Sent on every change, and with the paused heartbeat while it's on. The film itself is paused and resumed with `playback`. |
| `verdict.vote` | guest → host | pair | `{"v","sid","id","stars","share"?,"name"?}` (512 B) | someone's stars for the film, 1–10 (half stars out of five). `name` only with `share` (dropped otherwise). Sent again (for 12 hours) until it gets through. |
| `verdict.tally` | host → everyone | group (fanout) | `{"v","sid","seq","count","total","shared"?:[{"name","house"?,"stars"}]}` (12 KB, ≤ 100 names) | the crew's verdict: how many voted, their stars added up (the average is `total / count / 2` out of five), and the names and stars of those who share. `count` ≤ 2,000; `total` between `count` and `count × 10`. |

Who may send what (dropped otherwise, counted as `refused`): `lobby.seat`,
`room.status` and `verdict.vote` only from an Usherr invited to that
screening, in the pair's envelope, at most 10 at once and then one every 2
seconds per Usherr; `lobby.roster`, `room.intermission` and `verdict.tally`
only sealed with that screening's key, by its host, with the key's `sid`.
Names are cut to 60 characters and shown as plain text.

Nothing else about the party travels: a verdict's one line stays in that
person's own Diary (and their review, if they choose), no user ids, and a
voter's name only when they chose to share it. Only the host calls the
intermission (pausing the film for everyone, a 10-minute countdown, 5 more
minutes at a time, at most an hour from now) and rolls the credits
(`playback` `ended`), which opens the verdict everywhere.

### Crews

Code: `internal/link/crew` (the messages, their checks and who may send
what), `internal/api/home_crews*.go` (what they do). Every body has
`"v":1` and is read strictly; `cid` is the crew's id, a UUID its owner made.

A crew is a named group of linked Usherrs ("houses"). The Usherr that made
it owns it and is the hub: it invites houses it is linked with, every house
talks to the owner alone (two houses in a crew needn't be linked with each
other), and the owner sends each house the latest. Each house has a number
in the crew; the owner is 1. All are pair envelopes.

| type | way | body (largest) | does |
|---|---|---|---|
| `crew.invite` | owner → house | `{"v","cid","rev","name","by","you","houses":[{"h","id","name","state","people"?}]}` (8 KB, ≤ 12 houses, ≤ 12 people each) | the crew as it is now; the first is the invite. `you` is the receiving house's number; `id` each house's link identity (so the host of a vote can share the screening with the houses it is linked with); `people` the names of who opted in there |
| `crew.answer` | house → owner | `{"v","cid","answer","people"?}` (1 KB) | `join` (with who opted in), `decline`, `leave` |
| `crew.end` | owner → house | `{"v","cid"}` (1 KB) | the crew ended, or this house is no longer in it |
| `crew.chat` | house → owner → every other house | `{"v","cid","mid","h"?,"who","text","at","card"?:{"tmdb","title","year"?,"poster"?,"note"?,"to"?,"toWho"?}}` (4 KB) | a chat line: plain text, ≤ 500 characters (shown as text, never HTML). `card` is a film sent to house `to` (person `toWho`); that house tells its people. `h` is set by the owner. |
| `crew.list` | house → owner: `add`, `remove`, `have`; owner → house: `all` | `{"v","cid","op","film"?,"who"?,"has"?:[{"tmdb","has"}],"items"?:[…],"seq"?}` (14 KB, ≤ 40 films) | the shared watchlist. `have`: whether this house has each film, `yes` / `getting` / `no`, never anything else. `all` carries every film with who added it and each house's answer, and a `seq` (an older one that comes late is ignored). |
| `crew.vote` | house → owner: `open`, `cast`; owner → house: `ballot` | `{"v","cid","op","vid"?,"who"?,"tmdb"?,"films"?,"closes"?,"at"?,"pre"?,"ballot"?}` (10 KB, 2–8 films) | the next film vote: whoever opens it hosts; one vote per person (`tmdb` 0 takes it back). The ballot has who voted for what and each house's answer, its `state` (`open`, `closed`, `booked`, `nobody`) and the `winner`. The host's house books the winner in its Showtime and shares it with the crew's houses it is linked with (`screening.offer`). |
| `crew.club` | house → owner: `add`, `join`, `leave`, `pick`, `remove`; owner → house: `all` | `{"v","cid","op","kid"?,"who"?,"def"?,"film"?,"clubs"?,"seq"?}` (14 KB, ≤ 6 clubs, ≤ 24 seats) | film clubs: a weekday and time (the owner's), every 1 or 2 weeks, `pick` (turns) or `vote`. Two days before a night the owner books the picker's pick (a closed ballot with one film) or opens a vote from the list. |
| `crew.activity` | house → owner → every other house | `{"v","cid","lines":[{"h"?,"who","kind","title","part"?,"stars"?,"at"}]}` (4 KB, ≤ 10 lines) | what people who turned on sharing themselves watched (`finished`, `watched`, `rated`), from the Diary; nothing NSFW |

Who may send what (dropped otherwise, counted as `refused`): `crew.invite`
and `crew.end` only from the crew's owner (an invite for a crew known from
another owner, or one this Usherr owns, is refused); `crew.answer` only
from a house invited to or in a crew this Usherr owns; everything else from
the owner (only `all` and `ballot`) or from a house that joined (never
`all` or `ballot`). Unlinking removes the crews a link owns, and takes it
out of the crews this Usherr owns.

## Invite codes

```
USH-7QPM-2RD9-HXK4-MW3T-0A6B-C
```

Twenty characters of Crockford's base 32 (no I, L, O or U) hold a **100-bit
random secret** (13 bytes, the first 4 bits zero); the last character is a
check (each digit times an odd weight, 1, 3, 5 … 39, summed mod 32), so
any one mistyped character is refused before anything is sent. Reading a code is
forgiving: lower case, spaces, O for 0, I or L for 1, and a code inside a
pasted message are all fine.

When the inviting Usherr uses the default relay, the relay isn't in the code.
With its own relay, the relay is written after `USH@`:
`USH@relay.example.com-XXXX-…-C` for `wss://relay.example.com`, or
`USH@ws://192.168.1.5:8787-XXXX-…-C` for a relay without TLS on a home
network. The link uses that relay on both sides.

A code works **once**, for **24 hours**, and can be cancelled. At most five
wait at a time.

### Handshake

From the secret:

- **mailbox** `box` = hex(SHA-256(`"ulr-box"` || secret)), all the relay
  ever sees of the code;
- **invite key** = HKDF-SHA256(secret, no salt, info `"ulr-v1-invite"`);
- **proof key** = HKDF-SHA256(secret, no salt, info `"ulr-v1-invite-proof"`).

Invite envelopes have the same layout as message envelopes, encrypted with
the invite key, with associated data `"ulr-v1-join"` (joiner to host) or
`"ulr-v1-host"` (host to joiner), so one can't be passed off as the other.

1. The host makes the code and sends `invite.open` for its mailbox (and again
   after every reconnect, until the code is used, cancelled or 24 h old). It
   keeps the secret encrypted with `secret.key` in its database, so the code
   can be shown again.
2. The joiner sends `invite.post` with
   `{"v":1,"sign":"<its identity>","x":"<b64url X25519 public>","name","version","proof"}`,
   proof = b64(HMAC-SHA256(proof key, `"ulr-v1-join"` || Ed25519 public ||
   X25519 public)). If the relay answers `unknown`, the code was used,
   cancelled or is too old.
3. The host gets `invite.msg` with the relay-checked `from`. It checks that
   `sign` is `from` and the proof (constant-time), marks the code used (one
   use, in one database step), stores the link, closes the mailbox and
   answers with a direct `send` to the joiner:
   `{"v":1,"ok":true,"sign","x","name","version","proof"}`, proof =
   HMAC(proof key, `"ulr-v1-host"` || host Ed25519 || host X25519 || joiner
   Ed25519). A code that was used, cancelled or is too old is answered
   `{"v":1,"ok":false,"why":"used|expired|cancelled"}`.
4. The joiner tries a message from an identity it doesn't know against its
   waiting joins, checks `sign` = `from` and the proof, and stores the link.
5. Both send `link.hello` in normal envelopes.

If the host is offline, the relay keeps the request for 24 hours; the
joiner's page says it's waiting, and the link is made when the host comes
back. Guessing a 100-bit code is hopeless, and `invite.post` is rate limited
per connection anyway.

**Unlink** sends `link.unlink`, then forgets the other side's keys. From then
on its messages are dropped as coming from a stranger.

## What the relay can and can't see

It can see: which identities connect, when, from which IP address (Cloudflare
sees that for any website), and who sends how many bytes to whom. It can
also see a mailbox name being opened and posted to.

It can't see: what's in any message, the invite codes, names, films,
anything about a library. It keeps nothing once delivered, except up to 24
hours of queue for an Usherr that's offline, and no logs.

## Running your own relay

Settings → Movie night → Linked Usherrs → Relay → My own relay. Codes made
there carry your relay's address, so your friend's Usherr uses it too.

The Go relay is one small program, `usherr-relay`, with everything in memory:

```sh
go build -o usherr-relay ./cmd/usherr-relay
./usherr-relay -addr :8787
```

In Docker (from the repository):

```yaml
services:
  usherr-relay:
    image: golang:1.27-alpine
    command: sh -c "cd /src && go run ./cmd/usherr-relay -addr :8787"
    volumes:
      - ./:/src:ro
    ports:
      - "8787:8787"
    restart: unless-stopped
```

- Over the internet, give it a `wss://` address: put it behind a reverse
  proxy with TLS (Caddy, Traefik, nginx), or pass `-tls-cert` and `-tls-key`.
  Then enter `wss://relay.example.com` in Usherr.
- Only on your home network, `ws://192.168.1.5:8787` works too.
- Limits are the protocol's: 3 connections per identity, 16 KB envelopes,
  20 messages a second (burst 60; a fanout counts one per ten recipients,
  at most 100 recipients), 200 messages / 1 MB / 24 hours of queue.
- It logs no addresses, keys or messages. A restart forgets the queues and
  open mailboxes (codes waiting are opened again when the Usherrs
  reconnect).

`relay/README.md` has the Cloudflare version.

## Testing

`go test ./internal/link/...` runs both ends and the Go relay, including
`TestLinkRelayConformance`, which checks a relay from the outside. Point it at
another relay with `ULR_TEST_URL`, for example the Worker under
`npx wrangler dev`:

```sh
ULR_TEST_URL=ws://127.0.0.1:8787 go test ./internal/link/ -run 'Conformance|TwoUsherrs'
ULR_TEST_URL=ws://127.0.0.1:8787 go test ./internal/link/screening/
```

The conformance test covers `fanout` too (a list of 3 with one offline, one
of 60, the refusals, and the rate it counts). Under `wrangler dev`, making
dozens of new inboxes at once takes some seconds; the test waits for it.
