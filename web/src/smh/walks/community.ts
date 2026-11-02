// Usherr SMH: Community (preview). Make the household's own Discord bot in
// Discord's Developer Portal, give Usherr its token, invite it to a server
// and choose the channel it posts in.
import { t } from '../../i18n'
import type { Walkthrough } from '../types'

export const communityBot: Walkthrough = {
  id: 'community-bot',
  group: 'night',
  admin: true,
  title: t('Connect Usherr to your Discord server'),
  blurb: t('Your own Discord bot posts watch parties and reviews in your server. Make it once in Discord’s Developer Portal, paste its token, invite it.'),
  icon: 'chat',
  words: 'discord bot token server channel community watch party parties reviews friends developer portal invite message content intent',
  simple: [
    {
      kind: 'say',
      title: t('Make the bot'),
      text: t('On a computer, open discord.com/developers/applications and sign in with your Discord account. The first time, Discord asks what brings you there: pick Build a Bot and press Continue to Portal. Press New Application, name it Usherr (or anything you like), tick the box and press Create.'),
    },
    {
      kind: 'say',
      title: t('Its token, and one switch'),
      text: t('Open Bot on the left. Press Reset Token, then Copy: that long text is the token. Further down, under Privileged Gateway Intents, switch on Message Content Intent (so Usherr can read chat and #review) and press Save Changes. To make it private (only you can invite it): first open Installation on the left, set Install Link to None and save; then back in Bot, switch off Public Bot and save. Usherr gives you its own invite link.'),
    },
    {
      kind: 'say',
      title: t('A channel for Usherr'),
      text: t('In your Discord server, make a text channel such as #usherr-showtimes. While you try it out, make it private (only you), and add the bot to it after inviting it.'),
    },
    { kind: 'go', to: '/settings/community', title: t('Community'), text: t('Under Settings, Movie night.') },
    {
      kind: 'point',
      target: 'community-token',
      until: 'next',
      title: t('Paste the token'),
      text: t('Paste it in Bot token and press Save and connect. Usherr keeps it encrypted and never shows it again.'),
    },
    {
      kind: 'point',
      target: 'community-status',
      until: 'next',
      title: t('Connected?'),
      text: t('A green light and Connected as your bot’s name. If a yellow note says Message Content is off, go back to Bot in the Developer Portal and switch it on: the rest works without it.'),
    },
    {
      kind: 'point',
      target: 'community-invite',
      until: 'next',
      optional: true,
      title: t('Invite it'),
      text: t('Press Invite the bot, choose your server and press Authorize. Your server shows here by itself a moment later. For a private channel: in Discord, open the channel’s settings, then Permissions, and add the bot.'),
    },
    {
      kind: 'point',
      target: 'community-where',
      until: 'next',
      optional: true,
      title: t('Where it posts'),
      text: t('Choose the server, then the channel. It saves by itself.'),
    },
    {
      kind: 'point',
      target: 'community-test',
      until: 'next',
      optional: true,
      title: t('Post a test card'),
      text: t('Press it and look in the channel in Discord: a card says Usherr is connected. If it says the bot isn’t allowed, give the bot View Channel, Send Messages and Embed Links in that channel’s Permissions.'),
    },
    {
      kind: 'done',
      title: t('Usherr is in your Discord'),
      text: t('The bot stays connected while Usherr runs. Next, link your own Discord account with /link, and ask the others in the house to do the same.'),
      then: { id: 'link-discord', label: t('Link your Discord account') },
    },
  ],
}

export const joinWatchParty: Walkthrough = {
  id: 'join-watch-party',
  group: 'night',
  title: t('Join a watch party from Discord'),
  blurb: t('A friend shared a film in Discord: book the same moment in your own Usherr with the showtime code on its card.'),
  icon: 'clock',
  words: 'discord watch party join code showtime friends together same time card',
  simple: [
    {
      kind: 'say',
      title: t('Copy the showtime code'),
      text: t('On the watch party’s card in Discord, press I’m in: the answer only you see has the code, three groups of letters and numbers like HXK4-7QPM-2RD9C. It’s on the card too, under Showtime code. Copy it.'),
    },
    { kind: 'go', to: '/showtime', title: t('The Showtime page'), text: t('Where your showtimes are.') },
    {
      kind: 'point',
      target: 'showtime-join',
      until: 'next',
      title: t('Paste it and press Join'),
      text: t('Choose where the film plays, then press Join. The same film is booked for the same moment, with a pre-show of the same length made of your own clips and trailers. You need the film in your library or Streams, or Usherr streams it for the night.'),
    },
    {
      kind: 'done',
      title: t('You’re in'),
      text: t('Leave the Showtime page open on the TV: the pre-show and the film start on time, with everyone else. The chat is in the party’s thread in Discord.'),
    },
  ],
}

export const partyChat: Walkthrough = {
  id: 'party-chat',
  group: 'night',
  title: t('Chat during a watch party'),
  blurb: t('The party’s Discord thread, live in Usherr: beside the countdown, and on your phone while the film plays.'),
  icon: 'chat',
  words: 'discord watch party chat phone thread talk message during film',
  simple: [
    { kind: 'go', to: '/showtime', title: t('The Showtime page'), text: t('A showtime shared as a watch party has its chat here.') },
    {
      kind: 'point',
      target: 'party-chat',
      until: 'next',
      optional: true,
      title: t('The chat'),
      text: t('Everything said in the party’s thread in Discord shows here as it’s said. Type below and press Send: it goes into the thread under your Discord name (link your Discord account first, in Linked services).'),
    },
    {
      kind: 'say',
      title: t('On your phone during the film'),
      text: t('Open Usherr on your phone, then Showtime, and press Chat next to the party (or On your phone, here). Leave it open while the film plays on the TV. The Discord app works just as well: it’s the same thread.'),
    },
    {
      kind: 'say',
      title: t('Over the film, on the TV'),
      text: t('When the film plays on the Showtime screen, press Chat on the player: new messages fade in at a corner of the picture for a few seconds. It’s off until you switch it on. For full screen, use the player’s own full screen button, so the chat shows there too.'),
    },
    { kind: 'done', title: t('Chat away'), text: t('After the film, the chat stays on the Showtime page with the party.') },
  ],
}

export const reviewInDiscord: Walkthrough = {
  id: 'review-in-discord',
  group: 'night',
  title: t('Review a film for your Discord'),
  blurb: t('Stars and a few words, posted in your Discord server under your name, and shown on the film’s page in Usherr.'),
  icon: 'star',
  words: 'review rate stars discord community post diary film show opinion spoiler',
  simple: [
    {
      kind: 'say',
      title: t('On a film’s page'),
      text: t('Open any film or show. Under From the community, press Write a review: pick the stars, write a few words (||like this|| hides a spoiler) and press Post to Discord. Your Discord account has to be linked (Linked services).'),
    },
    { kind: 'go', to: '/you/diary', title: t('Or from your Diary'), text: t('Every film and show you watched.') },
    {
      kind: 'point',
      target: 'diary-review',
      until: 'next',
      optional: true,
      title: t('Post an entry as your review'),
      text: t('On an entry with stars or a note, this button posts them to Discord as your review.'),
    },
    {
      kind: 'say',
      title: t('Right in Discord'),
      text: t('Anyone in the server can type /review, start typing the film’s name and pick it, then the stars and a few words. In a watch party’s thread, a message starting with #review 4.5/5 counts as a review of that party’s film, and is copied to the reviews channel (Settings, Community, Reviews go to). In a reviews forum, each film has its own post, and #review works in there too.'),
    },
    {
      kind: 'done',
      title: t('Reviews, everywhere'),
      text: t('One review each per film: posting again replaces yours. Remove on the card in Discord, or on the film’s page, takes it away.'),
    },
  ],
}

export const ticketStubs: Walkthrough = {
  id: 'ticket-stubs',
  group: 'night',
  title: t('Collect ticket stubs'),
  blurb: t('A stub for each watch party you went to: handed out at the door, stamped if you stay to the end.'),
  icon: 'film',
  words: 'ticket stub stubs collect stamp watch party discord seat admit one',
  simple: [
    {
      kind: 'say',
      title: t('At the door'),
      text: t('Press I’m in on a watch party’s card in Discord. When the film starts, you get a ticket stub, with a seat in the order people said I’m in.'),
    },
    {
      kind: 'say',
      title: t('On the way out'),
      text: t('At the credits, the party’s thread shows Get my stub stamped. Press it within 30 minutes: your stub says you stayed to the end.'),
    },
    { kind: 'go', to: '/you/stubs', title: t('Your ticket stubs'), text: t('Under You. In Discord, /stubs lists them too. A friend’s party joined with its showtime code gets a stub here as well, stamped when your player reaches the credits.') },
    { kind: 'done', title: t('Start a collection'), text: t('Every party you go to adds one.') },
  ],
}

export const linkDiscord: Walkthrough = {
  id: 'link-discord',
  group: 'people',
  title: t('Link your Discord account'),
  blurb: t('So Usherr knows who you are in your Discord server: type /link there, and the six numbers it gives you here.'),
  icon: 'chat',
  words: 'discord link account code slash command community watch party name',
  simple: [
    {
      kind: 'say',
      title: t('Type /link in Discord'),
      text: t('In the Discord server Usherr posts in, type /link in any channel, choose Usherr’s /link from the list and press Enter. The bot answers with six numbers. Only you see them, and they work for 10 minutes.'),
    },
    { kind: 'go', to: '/settings/services', title: t('Linked services'), text: t('Under Settings, Account & Access.') },
    {
      kind: 'point',
      target: 'discord-link',
      until: 'next',
      title: t('Type the code'),
      text: t('Type the six numbers and press Link. Your Discord name shows here once it’s linked. Linking posts nothing: only what you choose to share is ever posted.'),
    },
    {
      kind: 'done',
      title: t('You’re linked'),
      text: t('Usherr now knows your Discord name. Unlink any time on the same card.'),
    },
  ],
}
