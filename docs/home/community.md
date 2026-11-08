# Community (preview)

Watch parties and reviews with your friends, in your own Discord server.
Usherr does it through **your own Discord bot**: you make the bot once in
Discord's Developer Portal, give Usherr its token, and invite it to your
server. Usherr keeps the bot connected while it runs. The connection goes
out from your home to Discord, like Usherr's other internet traffic, so
nothing on your network is opened to the internet.

This is a preview. Usherr connects, shows which servers the bot is in, posts a
test card in the channel you choose, lets each person link their Discord
account with `/link`, shares Showtimes as **watch parties** with their chat,
and posts **reviews**.

Everything is on **Settings → Movie night → Community**
(`/settings/community`), for administrators. **Show me how** walks you
through it.

## Make the bot

1. On a computer, open
   [discord.com/developers/applications](https://discord.com/developers/applications)
   and sign in with your Discord account. The first time, Discord asks what
   brings you there: pick **Build a Bot** and press **Continue to Portal**.
2. Press **New Application**, name it *Usherr* (or anything you like) and press
   **Create**.
3. Open **Bot** on the left:
   - Press **Reset Token**, then **Copy**. That long text is the bot's token.
   - Under **Privileged Gateway Intents**, switch on **Message Content
     Intent** and press **Save Changes**. Without it the bot still connects,
     but Usherr can't read what people write (chat and `#review`), and the
     page says so.
   - To keep it private, so only you can invite it: first open
     **Installation** on the left, set **Install Link** to **None** and save.
     Then, back in **Bot**, switch off **Public Bot** and save. (Discord refuses
     a private bot that still has an install link.) Usherr gives you its own
     invite link, which works for a private bot.

The token is the bot's password: anyone with it can post as your bot. Usherr
keeps it encrypted and never shows it again. If it ever gets out, press
**Reset Token** in the Developer Portal and paste the new one into Usherr.

## Connect it

1. Paste the token in **Bot token** and press **Save and connect**. The light
   turns green and says *Connected as* your bot's name.
2. Press **Invite the bot**, choose your server and press **Authorize**. You need
   the *Manage Server* permission there. The bot asks only for what it uses:
   reading and posting in channels, cards, threads, webhooks and events.
3. Your server shows on the page by itself a moment later. Choose it, then
   the channel Usherr posts in, such as a new `#usherr-showtimes`.
4. Press **Post a test card** and look in the channel.

If the test says the bot isn't allowed to post, open the channel's settings in
Discord, then **Permissions**, and give the bot **View Channel**, **Send
Messages** and **Embed Links**. A private channel needs the bot added to it.

## Link your Discord account

Each person links their own Discord account to their Usherr account, so
Usherr knows who presses *I'm in* and whose review is whose:

1. In the Discord server, type `/link` in any channel, choose Usherr's
   `/link` from the list and press Enter. The bot answers with six numbers.
   Only you see them, and they work for 10 minutes.
2. In Usherr, open **Settings → Account & Access → Linked services**, type
   the numbers under **Discord** and press **Link**.

Linking posts nothing: only what you choose to share is ever posted. **Unlink**
on the same card undoes it. A Discord account links to one Usherr account at a
time; linking it again moves it. Administrators see who is linked on the
Community page. *Show me how* has *Link your Discord account*.

`/link` appears in the server a minute or so after the bot first connects. If
it doesn't, restart Discord (Ctrl+R on a computer).

## Watch parties

A watch party is a Showtime shared to your Discord server. Everyone watches the
film in their own Usherr, on their own TV, at the same time, and talks about
it in Discord.

To share one, tick **Share to Discord as a watch party** when you book a
Showtime (Showtime on a film's page). For one already booked, press **Share**
next to it on the Showtime page. Usherr then:

- **Posts a card** in the channel: the poster, the pre-show and film times
  (in each reader's own time zone), a countdown that counts down by itself,
  the runtime, who's going, and **I'm in** and **Can't make it** buttons.
- **Opens a thread** under the card for the party's chat.
- **Puts it on the server's calendar** as an event, where people can press
  *Interested* and get Discord's reminder when it starts.

Pressing **I'm in** answers privately with the times, adds the person to the
thread so they're told what's said in it, and adds their name to the card.
On the Showtime page, a shared Showtime says who's going.

As the Showtime runs, the thread is told when the pre-show starts and when the
film starts, and the card says so. Moving the Showtime moves the event and
says so in the thread. Cancelling it marks the card called off, removes the
event and tells the thread. If Usherr was off at the time, nothing is
announced late.

Stopping a Showtime while its film plays ends the party ("Thanks for coming!"
on the card, and the thread is told) rather than calling it off.

### Join a watch party in your own Usherr

Each card has a **showtime code**, three groups of letters and numbers like
`HXK4-7QPM-2RD9C`. The private answer to **I'm in** has it too. In your own
Usherr, open **Showtime**, paste it under **Join a watch party**, choose where
the film plays and press **Join**. The same film is booked for the same moment,
with a pre-show of the same length, so the film starts with everyone else's.
The pre-show itself is your own: your clips, trailers and theme, or Usherr's
cards if you have none. Nothing of the host's crosses over.

The code carries only the film, the time and the pre-show's length. The two
Usherrs never talk to each other, so it works from any home. A mistyped code is
refused rather than booking the wrong film. Pasting the whole message works
too. If the party is moved, its card shows a new code. Each person needs the
film in their library or Streams, or Usherr streams it for the night.
*Show me how* has *Join a watch party from Discord*.

## The chat

The party's thread in Discord shows live in Usherr, so nobody has to switch
apps:

- **On the Showtime page,** under the countdown: everything said in the
  thread, as it's said, with pictures and GIFs, and a box to type in.
- **On your phone,** while the film plays on the TV: press **Chat** next to the
  party on the Showtime page (or **On your phone** in the chat) for a page with
  only the chat on it.

- **Over the film,** when it plays in Usherr's player (the Showtime screen):
  press **Chat** on the player, and each new message slides in at the bottom
  right of the picture for a few seconds, then fades. It's for reading; type
  on your phone. It's off until you switch it on, and the browser remembers
  your choice. With it on, use the player's own **full screen** button (or
  double-click the picture) so the chat shows in full screen too. If the film
  plays in the Jellyfin or Plex app instead, Usherr can't draw over it, so use
  your phone.

What you type is posted in the thread under your own Discord name and picture,
so link your Discord account first (Settings, Linked services). Nobody can be
pinged from Usherr. Edits and deletions in Discord show in Usherr too, and the
chat stays with the party after the film. The Discord app shows the same
thread, so people can chat from either.

The chat needs Message Content switched on for the bot (see *Make the bot*).
Without it, Discord doesn't let the bot read what people write.

## Ticket stubs

Like a cinema's: a stub for each watch party you went to.

- **At the door:** when a party's film starts, everyone who pressed **I'm in**
  gets a stub (their seat is the order they said so), and the host gets one
  too. The 🎬 feature message in the thread names who got one. Pressing I'm in
  after the film started gets you one then.
- **On the way out:** at the credits (the film's start plus its length), the
  bot posts **Get my stub stamped** in the party's thread. It works for 30
  minutes, and only on your own stub. A stamped stub says *Stayed to the end*.
  If the host stops the party during the film, the stamp button goes up then.
- **Stamp it in Usherr:** every stub under **You → Ticket stubs** has a
  **Stamp it** button from the film's start until 30 minutes after it ends,
  for the host and every guest. Usherr's player stamps it by itself once
  you've watched 90% of the film.
- **Your stubs:** in Usherr under **You → Ticket stubs** (with your Discord
  account linked), drawn as tickets with the film's poster, the night, your
  row and seat, the host, who else was there and the stub's number. A stamped
  stub has a round **SEEN** stamp, with your stars if you rated the film.
  **Save as picture** makes a PNG of it on your computer (Usherr never posts
  it anywhere). In Discord, `/stubs` lists yours.
- **Badges:** above your stubs, the badge cabinet: First party, Five nights,
  Regular (10), Season ticket (25), Perfect attendance (every party you were
  invited to in a month, three or more), Night owl (a party that ended after
  2 AM), House lights (hosted five) and Double feature (two films one night).
  Pick one to see how it's earned and when you got it. They're worked out
  from your stubs, linked Usherrs' parties included.
- **A friend's party, in your own Usherr:** a friend's bot can't reach your
  Usherr, and its `/link` codes only work in theirs. Paste the party's
  showtime code under **Showtime → Join a watch party** instead: your own
  Usherr gives you a stub at the door when the film starts, with nothing
  linked. It's stamped by itself at 90%, or press **Stamp it**. The
  friend's bot keeps its own stub for you too: `/stubs` in their server.
- **A linked Usherr's screening:** its host hands your Usherr a stub for
  each of your people who said I'm in, numbered by the host. A stamp you make
  at home goes back to the host, which confirms it.

Nobody can prove from afar that someone really watched, since each Usherr is
on its own. The stamp makes a stub mean something: it takes being there at the
end.

## Reviews

One review each per film or show: stars (half stars too) and a few words.
Posting again replaces yours. There are three ways to post one:

- **In Usherr:** on a film's or show's page, under **From the community**,
  press **Write a review**. Or in your **Diary**, the speech-bubble button on
  an entry posts its stars and note. The bot posts a card in the Community
  channel under your linked Discord name.
- **With `/review` in Discord:** anyone in the server, with Usherr or not.
  Start typing the film's name in the **film** box and pick it from the list.
  Discord hides the **stars** and **review** boxes until they're picked, so
  when they're left out, the bot opens a small form asking for them. In a
  film's post in the reviews forum, or a watch party's thread, the film box can
  stay empty: it's that film.
- **As `#review` in a watch party's thread:** a message such as `#review 4.5/5
  The diner scene!` counts as a review of that party's film (`8/10`, `★★★★½`
  and `4 stars` work too). The bot puts a ⭐ under it. Editing the message
  changes the review, and deleting it removes it.

**Where reviews go.** Choose it on the Community page under **Reviews go to**:
a text channel, or a **forum**. In a forum, each film or show gets its own post
the first time it's reviewed (its title, poster and a link), and every review
of it is a reply there, so the forum reads like a shelf of films with the
newest activity on top. People can reply to a review to talk about it. Every
review lands there however it was written: a `#review` typed in a watch party
is copied in, with a link back to where it was said, so nobody posts twice.

Inside a film's post, people talk freely: a reply is just conversation, and
only one starting with `#review` counts as a review (it shows on the film's
page, and the bot puts a ⭐ under it). People can open posts of their own too,
for any talk ("Best Nolan film?"). Usherr leaves those alone apart from one
note on how reviews work, since it can't tell which film a `#review` there
would be about. With no reviews channel chosen, reviews go to the watch parties' channel.

Every film's page shows its reviews under **From the community**, with
spoilers (`||like this||`) hidden until tapped. **Remove** on a review card
works only for whoever wrote it. On the film's page, **Change your review**
then **Remove it** takes yours away, in Discord too.

## Moving to another server

Which server and channel is only a setting. Invite the same bot to the new
server, choose it here, and Usherr posts there. Nothing has to be set up
again. Usherr keeps its own copy of what it posts, so later versions can post
a server's history again in the new one.

## Turning it off

Switch off **Connect the bot**. The bot goes offline and Usherr posts nothing.
The token stays saved, so switching it on again needs nothing else. To remove
the bot from a server, open the server's settings in Discord, then
**Integrations**, and remove it.

## What it never does

- **Pinging people:** nothing Usherr posts can ping anyone, even text with
  `@everyone` in it.
- **Retrying a refused token:** if Discord refuses the token, Usherr stops and
  says so on the page. It doesn't keep trying (Discord blocks bots that do).
- **Sharing without you:** it posts only where you chose, and only what you
  share.
