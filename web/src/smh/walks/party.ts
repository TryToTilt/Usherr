// Usherr SMH: watching with friends who run their own Usherr. Linking the
// two Usherrs, hosting a screening for them (the lobby, the intermission and
// the verdict), the phone page, and crews: a chat, a shared watchlist, a
// vote on the next film and film clubs.
import { crewsApi } from '../../home/crewsApi'
import { t } from '../../i18n'
import type { SmhCtx, SmhStep, Walkthrough } from '../types'
import { bc } from '../look' // Usherr Broadcast: words for its own pages

// Usherr Broadcast: the lobby is its own screen there, opened from Movie night.
export const openLobby = (): SmhStep => ({
  kind: 'point',
  look: 'broadcast',
  target: 'night-lobby',
  act: 'click',
  until: { seen: 'lobby' },
  optional: true,
  title: t('Open the lobby'),
  text: t('On Movie night, for the next showtime.'),
})

const pick = (c: SmhCtx, key: string) => (c.answers[key] as { value: string } | undefined)?.value
const has = (id: string) => !!document.querySelector(`[data-walk="${id}"]`)

// ---- linking two Usherrs ----

export const linkUsherr: Walkthrough = {
  id: 'link-usherr',
  group: 'night',
  admin: true,
  title: t('Link with a friend’s Usherr'),
  blurb: t('So you can watch together, with no Discord: a one-time code, and the two Usherrs stay linked.'),
  icon: 'external',
  words: 'link linked usherr friend friends code relay invite join together watch party screening crew',
  simple: [
    {
      kind: 'say',
      title: t('What linking does'),
      text: t('Two Usherrs that are linked can share screenings, chat at them and make crews. Each house plays its own copy of the film, and the link keeps everyone in step: no video crosses between houses, and nobody opens a port.'),
    },
    { kind: 'go', to: '/settings/linked', title: t('Linked Usherrs'), text: bc(t('Under Server, People.'), t('Under Settings, Movie night.')) },
    {
      kind: 'point',
      target: 'linked-switch',
      until: 'next',
      title: t('Switch it on'),
      text: t('Switch on Link with other Usherrs. A moment later the Relay line says it’s connected. If a note says the server’s clock is off, set its time right first, or screenings won’t start together.'),
    },
    {
      kind: 'ask',
      key: 'how',
      title: t('Who makes the code?'),
      choices: [
        { value: 'invite', label: t('I’ll make one for my friend'), recommended: true },
        { value: 'join', label: t('My friend sent me one') },
      ],
    },
    {
      kind: 'point',
      target: 'linked-invite',
      until: 'next',
      when: (c) => pick(c, 'how') !== 'join',
      title: t('Make a code'),
      text: t('Press Make a code, then Copy, and send it to your friend in a message. It works once, for 24 hours, and Cancel stops it. Your friend pastes it under Join with a code in their Usherr.'),
    },
    {
      kind: 'point',
      target: 'linked-join',
      until: 'next',
      when: (c) => pick(c, 'how') === 'join',
      title: t('Paste the code'),
      text: t('Paste the code your friend sent (it starts with USH-) and press Link.'),
    },
    {
      kind: 'point',
      target: 'linked-list',
      until: 'next',
      title: t('Your linked Usherrs'),
      text: t('Their Usherr shows here once the link is made. Rename gives it a name of your own. Compare the fingerprint with your friend if you want to be sure it’s really theirs. Unlink ends it at once, on both sides.'),
    },
    {
      kind: 'done',
      title: t('You’re linked'),
      text: t('Now share a showtime with them, or make a crew together.'),
      then: { id: 'host-screening', label: t('Host a screening for friends') },
    },
  ],
}

// ---- hosting a screening ----

const hostSteps = (advanced: boolean): Walkthrough['simple'] => [
  {
    kind: 'say',
    title: t('First, book the film'),
    text: bc(t('Book a showtime as usual: the film, the time and the pre-show. Linked Usherrs need to be switched on (Server, People, Linked Usherrs).'), t('Book a showtime as usual: the film, the time and the pre-show. Linked Usherrs need to be switched on (Settings, Movie night, Linked Usherrs).')),
  },
  { kind: 'go', to: '/showtime', title: t('The Showtime page'), text: t('Your showtimes are listed further down.') },
  {
    kind: 'point',
    target: 'showtime-invite',
    until: 'next',
    optional: true,
    title: t('Invite'),
    text: t('Press Invite on the showtime and tick the linked Usherrs to invite, then Save. Only those see it: in their own Showtime, under From linked Usherrs, their people answer I’m in, Can’t make it or Maybe.'),
  },
  {
    kind: 'say',
    when: () => !has('showtime-invite'),
    title: t('No Invite button?'),
    text: t('It shows on a showtime you booked, once Linked Usherrs is switched on. Book a showtime first, or link with a friend’s Usherr.'),
  },
  {
    kind: 'say',
    title: t('The lobby'),
    text: bc(
      t('Before the film, Open the lobby on Movie night counts down, says which part of the pre-show is playing in step at every house, and has a seat map of who’s coming. Press a free seat when you’re settled in: everyone else shows as on the way. Your seat is the one printed on your ticket stub.'),
      t('Before the film, the Showtime page counts down, shows which part of the pre-show is playing in step at every house, and has a seat map of who’s coming. Press Take my seat when you’re settled in: everyone else shows as on the way. Your seat is the one printed on your ticket stub.'),
    ),
  },
  {
    kind: 'say',
    title: t('Intermission'),
    text: t('As the host, press Intermission in the player, on the Showtime page or on your phone: the film pauses everywhere with a 10-minute countdown. +5 minutes adds time, Resume for everyone starts it again. Guests can say what they’re up to: snacks, drinks, a quick break, or back in their seat.'),
  },
  {
    kind: 'say',
    title: t('The verdict'),
    text: t('When the film ends, or you press Roll credits, everyone gives it stars (half stars too) and a line if they like. It goes to each person’s Diary. The crew sees the average, and names only for people who tick Show my name and stars to the crew. Then Collect your stub.'),
  },
  ...(advanced
    ? ([
        {
          kind: 'say',
          title: t('Chat and reactions'),
          text: t('The chat on the Showtime page reaches everyone at the screening, at every house. Tick Spoiler and a message stays blurred until someone taps it. HA, EEK, WOW, NOPE and CLAP float up over the picture in every house at once, and stay at their second of the film.'),
        },
        {
          kind: 'say',
          title: t('Each house watches its own copy'),
          text: t('A guest who doesn’t have the film sees Get it before showtime. A copy that may be a different cut is marked, as it may not stay in step. Moving or cancelling the showtime follows to every house.'),
        },
      ] as Walkthrough['simple'])
    : []),
  {
    kind: 'done',
    title: t('Enjoy the show'),
    text: t('Leave the Showtime page open on the TV: the pre-show and the film start together at every house.'),
    then: { id: 'party-phone', label: t('Use your phone at a screening') },
  },
]

export const hostScreening: Walkthrough = {
  id: 'host-screening',
  group: 'night',
  title: t('Host a screening for friends'),
  blurb: t('Share a showtime with linked Usherrs: a lobby with a seat map, an intermission, and everyone’s verdict at the credits.'),
  icon: 'clock',
  words: 'host watch party screening linked usherr invite friends lobby seat map intermission verdict credits stars together in step',
  simple: hostSteps(false),
  advanced: hostSteps(true),
}

// ---- the phone at a screening ----

export const partyPhone: Walkthrough = {
  id: 'party-phone',
  group: 'night',
  title: t('Use your phone at a screening'),
  blurb: t('Chat, react at the right moment, and, for the host, pause or play for everyone from the sofa.'),
  icon: 'monitor',
  words: 'phone companion remote react reactions chat spoiler pause play host screening watch party sofa',
  simple: [
    { kind: 'go', to: '/showtime', title: bc(t('Movie night'), t('The Showtime page')), text: bc(t('The lobby has the phone link.'), t('A screening’s chat is here, under the countdown.')) },
    openLobby(),
    {
      kind: 'point',
      look: 'broadcast',
      target: 'lobby-phone',
      until: 'next',
      optional: true,
      title: t('On your phone'),
      text: t('Use my phone as the remote shows a code: scan it with your phone and the screening’s page opens there, signed in as you. Leave it open while the film plays on the TV.'),
    },
    {
      kind: 'say',
      look: 'broadcast',
      when: () => !has('lobby-phone'),
      title: t('When a screening is on'),
      text: t('Movie night has Open the lobby for your next showtime: the phone code is there. Or open Usherr on your phone, then Movie night.'),
      qr: () => `${window.location.origin}/showtime`,
    },
    {
      kind: 'point',
      look: 'other',
      target: 'party-phone-link',
      until: 'next',
      optional: true,
      title: t('On your phone'),
      text: t('This link opens the screening’s page for a phone. Open it on your phone (or open Usherr there, then Showtime, then On your phone) and leave it open while the film plays on the TV.'),
    },
    {
      kind: 'say',
      look: 'other',
      when: () => !has('party-phone-link'),
      title: t('When a screening is on'),
      text: t('The chat and its On your phone link show on the Showtime page for a screening you’re at: one you host or said I’m in to.'),
      qr: () => `${window.location.origin}/showtime`,
    },
    {
      kind: 'say',
      title: t('What’s on the phone page'),
      text: t('What’s playing and where it’s got to, the chat with everyone at the screening (tick Spoiler to blur a message), and the five reactions: HA, EEK, WOW, NOPE and CLAP float up over the picture in every house at once.'),
    },
    {
      kind: 'say',
      title: t('For the host'),
      text: t('A remote: Pause for everyone, Play for everyone and back 10 seconds, plus Intermission and Roll credits. A guest can press Ask to pause. When the credits roll, give your verdict right there.'),
    },
    { kind: 'done', title: t('Your phone is ready'), text: t('Reactions are kept at their second of the film: anyone who watches that part later sees them float up too.') },
  ],
}

// ---- crews ----

export const startCrew: Walkthrough = {
  id: 'start-crew',
  group: 'night',
  title: t('Start a crew'),
  blurb: t('Your movie-night group between parties: a chat, a shared watchlist, a vote on the next film and film clubs.'),
  icon: 'chat',
  words: 'crew crews group friends movie night chat shared watchlist join invite wrapped linked usherr',
  simple: [
    { kind: 'go', to: '/crews', title: t('Crews'), text: t('In the menu, under Movie night. A crew is made of linked Usherrs, and in each house only the people who join it themselves are in it.') },
    {
      kind: 'point',
      target: 'crew-join',
      until: 'next',
      optional: true,
      title: t('An invite waiting'),
      text: t('Another Usherr invited you to a crew. Join the crew puts you in it (just you: the others in the house decide for themselves), and the crew sees your first name. No thanks turns it down.'),
    },
    {
      kind: 'point',
      target: 'crew-new',
      act: 'click',
      until: { seen: 'crew-make' },
      title: t('Make a crew'),
      text: t('Opens the form.'),
    },
    {
      kind: 'say',
      when: () => !has('crew-make'),
      title: t('Link an Usherr first'),
      text: bc(t('A crew is made of linked Usherrs. An administrator links this Usherr with a friend’s under Server, People, Linked Usherrs.'), t('A crew is made of linked Usherrs. An administrator links this Usherr with a friend’s under Settings, Movie night, Linked Usherrs.')),
    },
    {
      kind: 'point',
      target: 'crew-make',
      until: 'next',
      when: () => has('crew-make'),
      title: t('Name it, pick who’s in'),
      text: t('Type a name (The Friday Crew), tick the linked Usherrs in it and press Make the crew. Each one is sent an invite.'),
    },
    {
      kind: 'point',
      target: 'crew-tabs',
      until: 'next',
      optional: true,
      title: t('Your crew’s space'),
      text: t('Chat keeps going between parties (plain text, up to 500 characters). Shared watchlist is films anyone in the crew adds. Next film vote and Film clubs pick what you watch together.'),
    },
    {
      kind: 'say',
      title: t('What the crew is watching'),
      text: t('Under Chat, Share what I watch with my crews shows the others what you’re watching. It’s off until you switch it on yourself, nobody can switch it on for you, and nothing NSFW is ever shared.'),
    },
    {
      kind: 'done',
      title: t('Your crew is started'),
      text: t('From December through January, the crew’s Wrapped button shows its year together: parties, favourites, the most-reacted moment and whose picks won.'),
      then: { id: 'crew-vote', label: t('Pick the next film with your crew') },
    },
  ],
}

export const crewVote: Walkthrough = {
  id: 'crew-vote',
  group: 'night',
  title: t('Pick the next film with your crew'),
  blurb: t('The shared watchlist, a vote on the next film, and film clubs that meet every week or every other week.'),
  icon: 'list',
  words: 'crew vote ballot next film pick shared watchlist film club weekly turns send to friend book showtime',
  simple: [
    {
      kind: 'do',
      title: t('Looking for your crews'),
      run: async () => {
        const d = await crewsApi.list().catch(() => null)
        return { inCrew: !!d?.crews.some((c) => c.joined) }
      },
    },
    {
      kind: 'done',
      when: (c) => c.answers.inCrew !== true,
      title: t('You’re not in a crew yet'),
      text: t('The vote and film clubs live in a crew. Start one, or join one you were invited to.'),
      then: { id: 'start-crew', label: t('Start a crew') },
    },
    { kind: 'go', to: '/crews', title: t('Crews'), text: t('Your crews, with their tabs.') },
    {
      kind: 'point',
      target: 'crew-tab-list',
      act: 'click',
      until: 'next',
      title: t('The shared watchlist'),
      text: t('Anyone in the crew can add a film. Each one says who added it and whether each house has it: yes, getting it, or not yet.'),
    },
    {
      kind: 'point',
      target: 'crew-list-add',
      until: 'next',
      optional: true,
      title: t('Add a film'),
      text: t('Type its name and pick it. Add to my list puts one on your own list; Send to sends it to a friend, as a card in the chat.'),
    },
    {
      kind: 'point',
      target: 'crew-tab-vote',
      act: 'click',
      until: 'next',
      title: t('Next film vote'),
      text: t('A vote takes its films from the shared watchlist, so add at least two first.'),
    },
    {
      kind: 'point',
      target: 'crew-vote-start',
      until: 'next',
      optional: true,
      title: t('Start a vote'),
      text: t('Pick two to eight films and when it plays. A film not every house can play is marked. One vote each. When voting closes, the winner is booked in Showtime for the crew, hosted by whoever started the vote, with their pre-show.'),
    },
    {
      kind: 'point',
      target: 'crew-tab-clubs',
      act: 'click',
      until: 'next',
      title: t('Film clubs'),
      text: t('A night that comes back: every week or every other week.'),
    },
    {
      kind: 'point',
      target: 'crew-club-new',
      until: 'next',
      optional: true,
      title: t('Start a club'),
      text: t('Give it a name, a night and how often, and who picks: turns, or the crew’s vote. Two days before each night, Usherr books the pick or opens the vote, and counts the nights in a row. Join or leave any time.'),
    },
    { kind: 'done', title: t('Let the crew choose'), text: t('A film you picked or voted for that wins three times earns the Good taste badge.') },
  ],
}

// ---- badges ----

export const badges: Walkthrough = {
  id: 'badges',
  group: 'night',
  title: t('See your badges and save a stub'),
  blurb: t('Your badge cabinet and your collectible ticket stubs, each one saved as a picture if you like.'),
  icon: 'trophy',
  words: 'badge badges cabinet stub stubs ticket collect save picture png first party night owl double feature good taste generous loudest scream class clown',
  simple: [
    { kind: 'go', to: '/you/stubs', title: t('Ticket stubs'), text: t('Under You. A stub for every screening you said I’m in to: here, at a friend’s party, or at a linked Usherr.') },
    {
      kind: 'point',
      target: 'badge-cabinet',
      until: 'next',
      optional: true,
      title: t('Your badge cabinet'),
      text: t('First party, Five nights, Regular, Season ticket, Perfect attendance, Night owl, House lights, Double feature, Loudest scream, Class clown, Good taste and Generous. Tap one to see how it’s earned, and when you did.'),
    },
    {
      kind: 'point',
      target: 'ticket-stubs',
      until: 'next',
      title: t('Your stubs'),
      text: t('Each shows the film’s poster, the date and time, your row and seat, who hosted and its number. Stamped, it has a round SEEN stamp with your stars. Save as picture makes a PNG on your computer: Usherr never posts it anywhere.'),
    },
    { kind: 'done', title: t('Keep collecting'), text: t('Stay to the end and give your verdict at the credits: Collect your stub takes you straight here.') },
  ],
}
