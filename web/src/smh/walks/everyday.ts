// Usherr SMH: the everyday ones. Voting, your stack, Roulette, Tonight's
// picks, stream or keep, the Diary and Wrapped, a family member, and Usherr
// on a phone.
import { api } from '../../api'
import { t } from '../../i18n'
import { streamingOn } from '../../home/streamApi'
import type { SmhChoice, SmhCtx, SmhStep, Walkthrough } from '../types'

const pick = (c: SmhCtx, key: string) => (c.answers[key] as { value: string; label: string } | undefined)?.value

// filmAsk: find a film or show by name; the answer's value is its TMDB id.
const filmAsk = (title: string, text: string, kind: 'movie' | 'tv' = 'movie'): SmhStep => ({
  kind: 'ask',
  key: 'film',
  title,
  text,
  search: async (q) =>
    (await api.tmdbSearch(kind, q)).slice(0, 8).map((f): SmhChoice => ({ value: String(f.tmdbId), label: f.year ? `${f.title} (${f.year})` : f.title, image: f.posterUrl })),
})
const filmPage: SmhStep = { kind: 'go', to: (c) => `/title/${pick(c, 'film')}`, title: (c) => (c.answers.film as { label: string }).label, text: t('Its page.') }

// ---- votes ----

export const vote: Walkthrough = {
  id: 'vote',
  group: 'watch',
  title: t('Teach Usherr your taste'),
  blurb: t('Thumbs up and down make your For you suggestions better. Rate 20 is the quick start.'),
  icon: 'thumb-up',
  words: 'vote votes thumbs like dislike rate 20 taste recommendations for you match',
  simple: [
    {
      kind: 'say',
      title: t('Why vote?'),
      text: t('Every thumb teaches Usherr what you like and what isn’t for you. Your For you row, your match on every title, Tonight’s picks and Roulette all get better with each one. Your votes are your own: nobody else in the house sees them.'),
    },
    { kind: 'go', to: '/discover', title: t('Discover'), text: t('Rate 20 is up here, next to Roulette.') },
    {
      kind: 'point',
      target: 'rate20',
      act: 'click',
      until: { seen: 'rate-deck' },
      title: t('Rate 20'),
      text: t('Twenty titles, one at a time: the quickest way to start.'),
    },
    {
      kind: 'point',
      target: 'rate-deck',
      until: 'next',
      title: t('Like, Not for me, or Skip'),
      text: t('Say if you liked it, or if it looks like your kind of thing. Never seen it and not sure? Skip. On a keyboard: → likes, ← passes, space skips. Go through as many as you like.'),
    },
    {
      kind: 'done',
      title: t('Usherr is learning'),
      text: t('After a few likes, a For you row appears at the top of Discover, each title with its match and why (“because you liked Arrival”).'),
      shortcut: t('Every poster and title page has the thumbs: tap one whenever you finish something.'),
    },
  ],
  advanced: [
    {
      kind: 'say',
      title: t('Why vote?'),
      text: t('Every thumb teaches Usherr your taste: the genres, actors, directors and subjects you keep coming back to. It also learns from what you finish in Jellyfin. Your votes are your own.'),
    },
    { kind: 'go', to: '/discover', title: t('Discover'), text: t('Rate 20 is up here, next to Roulette.') },
    { kind: 'point', target: 'rate20', act: 'click', until: { seen: 'rate-deck' }, title: t('Rate 20'), text: t('Twenty titles, some from your library, some well known.') },
    { kind: 'point', target: 'rate-deck', until: 'next', title: t('Like, Not for me, or Skip'), text: t('→ likes, ← passes, space skips. A title you give a thumbs down stops showing in Discover’s rows.') },
    {
      kind: 'say',
      title: t('Thumbs everywhere'),
      text: t('On Discover the thumbs are on each poster; in the Library under each card; and on every title’s page, next to your match. Tap a lit thumb again to take the vote back. It works for films, shows, music, books and comics.'),
    },
    { kind: 'go', to: '/you', title: t('Your page'), text: t('Your taste in films and shows: who and what you lean towards, and what you don’t. Your votes are listed here too.') },
    {
      kind: 'point',
      target: 'you-share',
      until: 'next',
      optional: true,
      title: t('Share with the household?'),
      text: t('Switched on, you see how much taste you have in common with the others who share theirs. Nobody ever sees your votes themselves.'),
    },
    {
      kind: 'done',
      title: t('Usherr is learning'),
      text: t('A match of 50% is an ordinary title for you; the 80s and 90s are the ones most like your favourites.'),
      shortcut: t('Next time you finish a film, give it a thumb.'),
    },
  ],
}

// ---- the stack ----

export const stack: Walkthrough = {
  id: 'stack',
  group: 'watch',
  title: t('Keep a list of what to watch next'),
  blurb: t('Your stack: the films and shows you’ve pulled out to watch, as a stack of tickets that follows you to your phone.'),
  icon: 'stack',
  words: 'stack watchlist watch list later queue save tickets next',
  simple: [
    filmAsk(t('Pick something to watch later'), t('Any film. (Shows work the same way.)')),
    filmPage,
    {
      kind: 'point',
      target: 'stack-btn',
      act: 'click',
      until: 'next',
      title: t('Put it on your stack'),
      text: (c) => (c.watch ? t('Putting it on your stack.') : t('Press the stack button. Press it again to take it off.')),
    },
    {
      kind: 'point',
      target: 'side-stack',
      until: 'next',
      title: t('Your stack'),
      text: t('It lives in the menu: the front ticket is the newest. Swipe it, or use the arrows, to go through. Tap the poster to open it or play it.'),
    },
    {
      kind: 'done',
      title: t('That’s your stack'),
      text: t('It’s yours alone, holds up to 50, and follows you to your phone.'),
      shortcut: t('The stack button is on every poster card and title page.'),
    },
  ],
}

// ---- Roulette and Tonight's picks ----

export const roulette: Walkthrough = {
  id: 'roulette',
  group: 'watch',
  title: t('Let Usherr pick (Roulette)'),
  blurb: t('Can’t decide? Spin for one title, from what you have or something new.'),
  icon: 'dice',
  words: 'roulette random spin pick decide surprise what to watch',
  simple: [
    { kind: 'point', target: 'roulette', act: 'click', until: { seen: 'roulette-box' }, title: t('Roulette'), text: t('The dice, at the top of every page.') },
    {
      kind: 'point',
      target: 'roulette-box',
      until: 'next',
      title: t('Set it up, then spin'),
      text: t('Already here picks from what you can play and haven’t seen; Something new from well-rated titles the house doesn’t have. Choose a film or show, who’s watching, a mood and how long it may be, then spin. Spin again for another.'),
    },
    {
      kind: 'done',
      title: t('Enjoy the film'),
      text: t('The better-rated titles, and those that fit the people watching, come up more often. Anything any of them gave a thumbs down never does.'),
      shortcut: t('Next time: the dice at the top.'),
    },
  ],
}

export const tonight: Walkthrough = {
  id: 'tonight',
  group: 'watch',
  title: t('Find something for tonight'),
  blurb: t('Three picks from what you already have: what you’re part-way through, the next episode, or something unseen.'),
  icon: 'sparkles',
  words: 'tonight picks what to watch suggestion continue next episode',
  simple: [
    { kind: 'point', target: 'tonight', act: 'click', until: { seen: 'tonight-box' }, title: t('Tonight’s picks'), text: t('At the top of every page.') },
    {
      kind: 'point',
      target: 'tonight-box',
      until: 'next',
      title: t('Three picks'),
      text: t('From what’s already here: something you’re part-way through, the next episode of a show you watch, and films or shows you have but haven’t seen. Tap one to play it.'),
    },
    {
      kind: 'done',
      title: t('Enjoy'),
      text: t('Want more choice? Roulette spins for one; the Ask bar takes it in your own words (“a 90s heist movie, not too violent”).'),
      shortcut: t('Next time: Tonight’s picks, at the top.'),
    },
  ],
}

// ---- stream now, or keep it ----

const inLib = (c: SmhCtx) => c.answers.inLib === true
const canStream = (c: SmhCtx) => c.answers.canStream === true

export const streamOrKeep: Walkthrough = {
  id: 'stream-or-keep',
  group: 'watch',
  title: t('Watch a film now, or keep it'),
  blurb: t('Stream plays it straight away without downloading; Add to library downloads it to keep.'),
  icon: 'play',
  words: 'stream now play download keep library add watch without waiting',
  simple: [
    filmAsk(t('Which film?'), t('Any film.')),
    {
      kind: 'do',
      title: t('Looking it up'),
      run: async (c) => {
        const [m, on] = await Promise.all([api.tmdbMovieDetail(Number(pick(c, 'film'))), streamingOn().catch(() => false)])
        return { inLib: m.status === 'downloaded', canStream: on }
      },
    },
    filmPage,
    {
      kind: 'point',
      target: 'title-play',
      until: 'next',
      when: inLib,
      title: t('You already have it'),
      text: t('It’s downloaded: Play plays it right here, or open it in Jellyfin on the TV.'),
    },
    {
      kind: 'ask',
      key: 'how',
      when: (c) => !inLib(c) && canStream(c),
      title: t('Now, or to keep?'),
      card: {
        title: t('Stream or add to library'),
        options: [
          { name: t('Stream'), good: t('Plays in a few seconds, without waiting for a download. Nothing is kept.'), bad: t('Needs a good internet connection while you watch.') },
          { name: t('Add to library'), good: t('Downloaded to keep: plays anywhere, in Jellyfin too, even offline.'), bad: t('Takes a while to download, and uses disk space.') },
        ],
        recommend: t('Stream to watch tonight; Add to keep it for good.'),
        later: t('While streaming, Keep it downloads it after all.'),
      },
      choices: [
        { value: 'stream', label: t('Stream it now'), recommended: true },
        { value: 'add', label: t('Add it to the library') },
      ],
    },
    {
      kind: 'point',
      target: 'stream-btn',
      act: 'click',
      until: { seen: 'stream-sheet' },
      when: (c) => !inLib(c) && pick(c, 'how') === 'stream',
      title: t('Stream'),
      text: t('Press Stream.'),
    },
    {
      kind: 'point',
      target: 'stream-sheet',
      until: 'next',
      when: (c) => !inLib(c) && pick(c, 'how') === 'stream',
      title: t('Quality, and where to play'),
      text: t('Pick the quality (remembered on this device) and where to play it. It starts in a few seconds. If it stalls, Try another release in the player.'),
    },
    {
      kind: 'point',
      target: 'title-add',
      until: 'next',
      when: (c) => !inLib(c) && (pick(c, 'how') === 'add' || !canStream(c)),
      title: t('Add to library'),
      text: (c) => (canStream(c) ? '' : t('Streaming isn’t set up on this Usherr (an administrator can switch it on under Settings, Extras), so this one goes in the library. ')) + t('Press Add to library, keep the usual choices, and Usherr finds a copy and downloads it. Activity shows how it’s going.'),
    },
    {
      kind: 'done',
      title: t('Enjoy the film'),
      text: t('Titles marked Streams in the Library aren’t on disk: they always play this way.'),
      shortcut: t('Next time: a film’s page, then Stream or Add to library.'),
    },
  ],
}

// ---- Diary and Wrapped ----

export const diary: Walkthrough = {
  id: 'diary',
  group: 'watch',
  title: t('Your diary and your year (Wrapped)'),
  blurb: t('What you watched and when, with stars and notes, and your year in watching.'),
  icon: 'calendar',
  words: 'diary wrapped history watched log letterboxd trakt year stats stars review',
  simple: [
    {
      kind: 'go',
      to: '/you/diary',
      title: t('Your diary'),
      text: t('What you watched and when, newest first. It fills itself in from what you finish in Jellyfin.'),
    },
    {
      kind: 'point',
      target: 'diary-row',
      until: 'next',
      optional: true,
      title: t('Stars and a note'),
      text: t('Tap a star for that many (again for a half less). The pencil writes a note. Four stars and up count as a like for your taste.'),
    },
    {
      kind: 'point',
      target: 'diary-buttons',
      until: 'next',
      title: t('Log something, or bring your history in'),
      text: t('Log something adds a film or show you watched elsewhere, with the day. Import brings in your Letterboxd diary or your Trakt history.'),
    },
    { kind: 'go', to: '/you/wrapped', title: t('Wrapped'), text: t('Your year in watching: hours, films, episodes, your longest run of days, the shows you couldn’t stop and what your year was made of. Earlier years are a tap away.') },
    { kind: 'done', title: t('That’s your diary'), text: t('The more it knows, the better your suggestions.'), shortcut: t('Next time: your initial, top right, then You, Diary.') },
  ],
}

// ---- a family member ----

const role = (c: SmhCtx) => pick(c, 'role')

export const familyMember: Walkthrough = {
  id: 'family-member',
  group: 'people',
  admin: true,
  title: t('Add a family member'),
  blurb: t('Their own account, with their own taste, stack and diary, and only the rights you give them.'),
  icon: 'user',
  words: 'account user family member kid child add person login role permissions request',
  simple: [
    {
      kind: 'go',
      to: '/settings/accounts',
      title: t('Accounts'),
      text: t('Everyone gets their own sign-in, so their votes, stack, diary and picks stay their own.'),
    },
    { kind: 'point', target: 'account-add', until: 'next', title: t('Their name and password'), text: t('A username to sign in with, a name, and a password of at least 8 characters. They can change the password themselves later.') },
    {
      kind: 'ask',
      key: 'role',
      title: t('What may they do?'),
      card: {
        title: t('The three roles'),
        options: [
          { name: t('Basic user'), good: t('Finds, adds and plays films and shows. You choose what else (below).'), bad: t('Can’t change settings or other accounts.') },
          { name: t('Viewer'), good: t('Looks and plays, never changes anything. For a guest or a screen on the wall.'), bad: t('Can’t add or ask for anything.') },
          { name: t('Admin'), good: t('Can do everything, like you.'), bad: t('Can change and delete anything.') },
        ],
        recommend: t('Basic user for family.'),
        later: t('Change it any time: Edit on their account.'),
      },
      choices: [
        { value: 'member', label: t('Basic user'), recommended: true },
        { value: 'viewer', label: t('Viewer') },
        { value: 'admin', label: t('Admin') },
      ],
    },
    {
      kind: 'point',
      target: 'account-role',
      until: 'next',
      title: (c) => t('Pick {role}', { role: (c.answers.role as { label: string }).label }),
      text: t('Under What can they do?'),
    },
    {
      kind: 'ask',
      key: 'kid',
      when: (c) => role(c) === 'member',
      title: t('Add things themselves, or ask you first?'),
      card: {
        title: t('Add titles themselves'),
        options: [
          { name: t('Yes, add them'), good: t('What they pick is added straight away.'), bad: t('No check on what comes in.') },
          { name: t('No, send me requests'), good: t('Their Add button says Request; you Approve or Decline under Activity, Requests. Good for children.'), bad: t('They wait for you.') },
        ],
        later: t('A switch on their account, any time.'),
      },
      choices: [
        { value: 'yes', label: t('Yes, add them'), recommended: true },
        { value: 'no', label: t('No, send me requests') },
      ],
    },
    {
      kind: 'point',
      target: 'account-perms',
      until: 'next',
      when: (c) => role(c) === 'member',
      title: t('Permissions'),
      text: (c) =>
        pick(c, 'kid') === 'no'
          ? t('Untick Add titles themselves. The kinds of media they may ask for are here too.')
          : t('Leave Add titles themselves on. Untick any kind of media they shouldn’t add.'),
    },
    { kind: 'point', target: 'account-addbtn', until: 'next', title: t('Add the account'), text: t('Press Add account.') },
    {
      kind: 'done',
      title: t('Tell them how to sign in'),
      text: t('Give them the username and password. Usherr on your phone, in Show me how, gets them set up on theirs.'),
      then: { id: 'phone', label: t('Usherr on a phone') },
    },
  ],
}

// ---- Usherr on a phone ----

const here = () => window.location.origin
const local = () => /^(localhost|127\.|\[::1\])/.test(window.location.hostname)

export const phone: Walkthrough = {
  id: 'phone',
  group: 'people',
  title: t('Usherr on your phone'),
  blurb: t('Open Usherr on a phone and keep it on the home screen, like an app.'),
  icon: 'monitor',
  words: 'phone mobile app iphone android home screen install tailscale away',
  simple: [
    {
      kind: 'ask',
      key: 'os',
      title: t('Which phone?'),
      choices: [
        { value: 'ios', label: t('iPhone or iPad') },
        { value: 'android', label: t('Android') },
      ],
    },
    {
      kind: 'say',
      title: t('Open Usherr on the phone'),
      text: () =>
        local()
          ? t('On the phone, open the browser and type this computer’s address on your home network instead of “localhost”: the same address with the computer’s IP, for example http://192.168.1.50:8264.')
          : t('Point the phone’s camera at this code and tap the link, or type the address into its browser. The phone has to be on your home Wi-Fi.'),
      qr: () => here(),
    },
    {
      kind: 'say',
      title: t('Put it on the home screen'),
      text: (c) =>
        pick(c, 'os') === 'ios'
          ? t('In Safari, tap the Share button (the square with an arrow), then Add to Home Screen, then Add. Usherr now opens like an app, full screen.')
          : t('In Chrome, tap the ⋮ menu, then Add to Home screen (or Install app). Usherr now opens like an app, full screen.'),
    },
    {
      kind: 'say',
      title: t('Away from home'),
      text: t('Usherr stays on your home network. To use it when you’re out, install Tailscale on the phone and sign in to the same Tailscale account as this computer: it then works anywhere, still private.'),
    },
    { kind: 'done', title: t('Usherr is on your phone'), text: t('Your stack, votes and diary are the same there: it’s your account.') },
  ],
}
