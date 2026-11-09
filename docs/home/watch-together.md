# Watching together

Movie night with friends who run their own Usherr, in their own homes, with
no Discord needed. Each house plays its own copy of the film; the link only
keeps everyone in step and carries the chat, the reactions and the stubs. No
video ever crosses between houses.

It comes in layers, each optional:

- **Linked Usherrs**: two Usherrs that trust each other.
- **Linked screenings**: a Showtime shared with linked Usherrs, played in step
  in every house, with a lobby, an intermission, a verdict and a stub.
- **Crews**: your movie-night group between parties, with a chat, a shared
  watchlist, a vote on the next film and film clubs.

(Watch parties in a Discord server work too, and on their own: see
[Community](community.md).)

## Linked Usherrs

An administrator switches it on under **Server › People › Linked Usherrs**
(in Classic or Neu: **Settings → Movie night → Linked Usherrs**): **Link with other Usherrs** is off until you do.

1. **Invite a friend's Usherr** makes a one-time code
   (`USH-XXXX-XXXX-XXXX-XXXX-XXXX-X`). It works once, for 24 hours, and can be
   cancelled. Send it to your friend.
2. Your friend pastes it under **Join with a code** on the same page of their
   Usherr. The two are linked. A mistyped, used, cancelled or expired code says
   so.

Each link shows its name, its **fingerprint** (your friend sees the same one
next to your Usherr; compare them if you want to be sure), when it was last
heard from, **Rename** (the name is only for you) and **Unlink**, which tells
the other Usherr and stops the link on both sides at once.

Linked Usherrs talk through the **Usherr Link Relay**, a postman that passes on
sealed envelopes. Every Usherr keeps one connection going out to it, so no
port is opened at home and nobody sees your home's address. Everything is
encrypted and signed end to end, so the relay can't read any of it. The page
shows whether the relay is connected, and warns when the server's clock is off
(screenings start together only when the clocks agree). **My own relay** uses
a relay you run yourself; see
[Running your own relay](link-protocol.md#running-your-own-relay).

This Usherr's keys are kept in `link-identity.key` in the settings folder, not
in the database or its backups. Usherr itself stays on your home network and
Tailscale: linking needs nothing opened to the internet.

## Linked screenings

Book a film in **Showtime** as usual, then press **Invite** on the booking and
tick the linked Usherrs to invite. Only the Usherrs you invite see it.

In each invited house, the screening shows on the Showtime page under **From
linked Usherrs**: who hosts, the film, when it starts and how long the
pre-show is. Anyone there answers **I'm in**, **Can't make it** or **Maybe**,
and the host sees who's coming from each house and whether they have the film.

- **Have the film?** Each Usherr checks its library, saved streams, downloads
  and requests. If it isn't there, **Get it before showtime** asks for it. A
  copy more than two minutes longer or shorter than the host's is flagged as a
  possibly different cut, which may not stay in step.
- **Booked at home too.** When someone says I'm in, the screening is booked in
  their own Showtime, so their Marquee counts down to it. Moving or calling it
  off follows to every invited Usherr; taking an Usherr off the list tells it;
  unlinking removes shared screenings on both sides.
- **Notifications.** Tick **Linked screenings** on a notification for invites,
  moves and cancellations.

## Watching in step

At showtime, open the Showtime page and press **Join the screening** (it opens
a little before the pre-show). Leave the page open: the pre-show and then the
film start there, in step with everyone. Joining late starts where everyone
has got to.

- **The pre-show.** The host's Usherr sends what's in its pre-show, piece by
  piece. Each house plays the same running order with its own cards, clips and
  trailers; anything it doesn't have is a card of the same length, so the film
  starts at the same moment everywhere.
- **Following the host.** The host's play, pause and skips go to everyone.
  Every player stays within a fraction of a second, speeding up or slowing
  down slightly, or jumping when it's further off. Guests see *Following the
  host*, how far off they are, and *Host paused*.
- **Anyone can pause** (off by default): guests can ask to pause, and the
  host's Usherr pauses for everyone. Only the host plays again.
- **If the relay drops**, the film plays on and gets back in step when it's
  back.

This follows in Usherr's own player. Jellyfin and Plex don't follow along.

## The party

### The lobby

Before the film, the Showtime page counts down, shows which part of the
pre-show is playing in step, and has a **seat map** of who's coming. Press
**Take my seat** when you're settled in; everyone else shows as on the way.
Your seat is the one printed on your stub. The lobby works for your own
showtimes too.

In Broadcast the lobby is a full screen made for the remote: pick your seat
with the arrows and OK, chat with quick replies (or **Finish on your phone**
to type there), react, and **Start the film**, **Skip the pre-show**, use your
phone as the remote, or **Leave**. Your crew's vote shows on **Movie night** as
cards you press to vote.

### Chat and reactions

- **Chat** reaches every Usherr at the screening, on the Showtime page and on
  your phone. Mark a message as a **spoiler** and it stays blurred until
  someone taps it.
- **Reactions**: tap **HA**, **EEK**, **WOW**, **NOPE** or **CLAP** and it
  floats up over the picture in every house at once. Each one is kept at its
  second of the film, so anyone who watches that part later sees it float up
  too, and small bars under the picture show where the crew reacted most.

### On your phone

Open the screening's chat on a phone (**Chat** next to the party on the
Showtime page) for the screening's companion: what's playing and where it's
got to, the chat and the five reactions. The host also gets a remote:
**Pause for everyone**, **Play for everyone** and back 10 seconds. Guests can
**Ask to pause** when the host allows it.

### Intermission

The host can call an **Intermission** from the player, the Showtime page or
the phone. The film pauses everywhere, everyone sees a 10-minute countdown
and when it's back, and the host can add 5 minutes or **Resume for everyone**.
Guests can say what they're up to: getting snacks, refilling drinks, a quick
break or back in their seat.

### The verdict

When the film ends (or the host presses **Roll credits**), everyone gives it
stars, half stars too, and a line if they like. It goes to your own Diary,
and to your review if you tick **Post it as my review too**. The crew's card
shows the average and how many voted, with names only for people who tick
**Show my name and stars to the crew**, plus the most-reacted moment. Then
**Collect your stub**.

### Ticket stubs and badges

When the screening starts, the host hands your Usherr a stub for each of your
people who said I'm in, numbered by the host and signed with its key. A stamp
you make at home goes back to the host, which confirms it, so both sides
agree. Nobody outside the screening sees any of it. The stubs, the **SEEN**
stamp and the badge cabinet are under **You → Ticket stubs**; see
[Ticket stubs](community.md#ticket-stubs) for how they're stamped and every
badge.

## Crews

A crew is your movie-night group between parties. Open **Movie night ›
Crews and votes** in the menu (in Classic or Neu: **Crews**, next to Showtime)
and press **Make a crew**: give it a name and tick the
linked Usherrs in it. Each one gets an invite, and in each house **only the
people who join it themselves are in it** (**Join the crew**). The crew sees
your first name. **Leave the crew** works any time. **End the crew**, for
whoever runs it, ends it for every Usherr in it: its chat, list, votes and
clubs go.

Each crew has:

- **Chat** that keeps going between parties: plain text, up to 500
  characters.
- **A shared watchlist.** Anyone in the crew can add a film. Each one shows
  who added it and whether each house has it: yes, getting it, or not yet.
  Nothing else about anyone's library is shared. **Add to my list** puts it on
  your own list, and **Send to** sends it to one friend: it shows in the chat as
  a card, and their Usherr lets them know.
- **Pick the next film.** **Start a vote**: pick two to eight films from the
  shared watchlist and when it plays. A film not everyone can play is marked.
  One vote each. When voting closes, the winner is booked in Showtime for the
  crew, with the pre-show whoever started the vote picked; they host.
- **Film clubs.** **Start a club**: a night, every week or every other week,
  and who picks: turns (**Who picks rotates**) or the crew's vote. Join or leave
  any time. Two days before each night, Usherr books the pick or opens the
  vote, and keeps count of the nights in a row. Whoever started a club can
  **End the club**: no more nights are arranged, and the past ones are kept.
- **What the crew is watching**, from people who switch on **Share what I
  watch with my crews** themselves. It's off until you do, nobody can switch it
  on for you, and nothing NSFW is ever shared.

### Crew Wrapped

Each crew gets its year in review on the Crews page. The button shows from
December 1 through January, and **Past Wrapped** opens earlier years. Five
slides: how many parties and hours the crew had together and on which nights,
the crew's favourite and most divisive film (from the stars people shared),
the year's most-reacted moment, whose picks won the votes, and next year.
**Save as picture** makes a PNG of any slide on your computer; Usherr never
posts it anywhere. A year without a party says so instead of showing zeros.

## What crosses the link

- Only what a screening or a crew needs: the film, the times, who's coming,
  the chat, reactions, verdicts people chose to share, stubs and stamps.
- Whether a house has a film is yes, getting it, or not yet. Nothing else
  about anyone's library is shared.
- What you watch is shared only if you switch on **Share what I watch with my
  crews** yourself, and never anything NSFW.
- The relay passes sealed envelopes and can't read them. The technical
  details are in [the protocol](link-protocol.md).
