// Usherr SMH: people and devices. Requests from the kids, the Stremio and
// Nuvio add-on, and notices on your phone.
import { t } from '../../i18n'
import type { Walkthrough } from '../types'

export const kidsRequests: Walkthrough = {
  id: 'kids-requests',
  group: 'people',
  admin: true,
  title: t('Let the kids ask instead of add'),
  blurb: t('Their Add button becomes Request; you approve or decline.'),
  icon: 'user',
  words: 'kids children request approve decline ask permission parental',
  simple: [
    {
      kind: 'say',
      title: t('How it works'),
      text: t('On a child’s account (a Basic user), switch off Add titles themselves. Their Add button then says Request: what they pick is kept exactly as they chose it, and they’re told it went to you. Set it when you add the account, or with Edit on it later.'),
    },
    { kind: 'go', to: '/queue', title: t('Activity'), text: t('Requests wait here.') },
    {
      kind: 'point',
      target: 'queue-requests',
      act: 'click',
      until: 'next',
      title: t('Requests'),
      text: t('Approve adds the title for them, as they asked, and the library says who asked. Decline can carry a note they’ll see. The number on the tab is how many are waiting.'),
    },
    {
      kind: 'say',
      title: t('Hear about them'),
      text: t('A notice on your phone for each new request: tick Requests on a notification under Settings, Notifications. Get notices on your phone, in Show me how, sets that up.'),
    },
    { kind: 'done', title: t('Requests are set'), text: t('The family filter on Discover keeps their rows to their age, too.'), then: { id: 'notices', label: t('Get notices on your phone') } },
  ],
}

export const stremioAddon: Walkthrough = {
  id: 'stremio-addon',
  group: 'people',
  title: t('Usherr in Stremio or Nuvio'),
  blurb: t('One add-on link puts your library, streams, watchlist and Live TV in the Stremio and Nuvio apps.'),
  icon: 'share',
  words: 'stremio nuvio addon add-on manifest link tv app phone',
  simple: [
    { kind: 'go', to: '/you', title: t('Your page'), text: t('Your own add-on link is further down, under Stremio and Nuvio.') },
    {
      kind: 'point',
      target: 'addon-form',
      until: 'next',
      optional: true,
      title: t('Your add-on link'),
      text: t('The address your phone or TV reaches Usherr at, and the link made from it. It’s yours: it shows your watchlist and your picks.'),
    },
    {
      kind: 'say',
      title: t('In Stremio'),
      text: t('Open Stremio, go to Add-ons, paste the link in the search box at the top (or use Install here, on the same device), and press Install. In Nuvio: Settings, Add-ons, add from a link. You only do it once.'),
    },
    { kind: 'done', title: t('Usherr is in the app'), text: t('Your library, streamed titles, thumbs-up watchlist and Live TV channels show up in the app, with streams for any title.') },
  ],
}

export const notices: Walkthrough = {
  id: 'notices',
  group: 'people',
  admin: true,
  title: t('Get notices on your phone'),
  blurb: t('A message when a film arrives, a request comes in, an audiobook is ready, or a show you set a reminder for starts.'),
  icon: 'mail',
  words: 'notifications notices ntfy phone push alerts messages reminders',
  simple: [
    {
      kind: 'say',
      title: t('On your phone first'),
      text: t('Install the free ntfy app (App Store or Google Play); no account needed. Tap +, and subscribe to a topic: a name only you know, like usherr-k7q2x. Anyone who knows the name can read it, so make it hard to guess.'),
    },
    { kind: 'go', to: '/settings/notifications', title: t('Notifications'), text: t('Where Usherr sends its notices.') },
    {
      kind: 'point',
      target: 'notif-add',
      until: 'next',
      title: t('Add a notification'),
      text: t('Pick ntfy, keep https://ntfy.sh as the server (or your own), and type the same topic. Tick what you want to hear about: films and episodes downloaded, requests, audiobooks made, Live TV reminders. Then Test it.'),
    },
    { kind: 'done', title: t('Notices are on'), text: t('Your phone buzzes when something you ticked happens. Each kind can be switched off any time.') },
  ],
}

export const resetByEmail: Walkthrough = {
  id: 'reset-by-email',
  group: 'people',
  admin: true,
  title: t('Let people reset a forgotten password'),
  blurb: t('Forgot password? on the sign-in page emails a link that sets a new password, so nobody needs you or the command line.'),
  icon: 'mail',
  words: 'forgot forgotten password reset email link sign in locked out',
  simple: [
    {
      kind: 'ask',
      key: 'mail',
      title: t('Is an email notification set up?'),
      text: t('The link goes out through the mail server of an email notification (only to the account’s own address), and starts with the address you open Usherr at (Link in messages, under Notifications).'),
      choices: [
        { value: 'yes', label: t('Yes, email works') },
        { value: 'no', label: t('Not yet') },
      ],
    },
    { kind: 'go', when: (c) => (c.answers.mail as { value: string } | undefined)?.value === 'no', to: '/settings/notifications', title: t('Notifications'), text: t('Add an email notification (your mail server, from and to addresses) and Test it. Fill in Link in messages with the address you open Usherr at.') },
    {
      kind: 'point',
      when: (c) => (c.answers.mail as { value: string } | undefined)?.value === 'no',
      target: 'notif-add',
      until: 'next',
      title: t('Add an email notification'),
      text: t('Pick Email, fill in your mail server and Test it. Gmail and Outlook need an app password.'),
    },
    { kind: 'go', to: '/settings/sign-in', title: t('Ways to sign in'), text: t('Where the ways to sign in are switched on and off.') },
    {
      kind: 'point',
      target: 'signin-email-reset',
      until: 'next',
      title: t('Turn on reset by email'),
      text: t('If the button is greyed out, the line above it says what’s missing. A link works once, for 30 minutes, signs the account out everywhere, and two-step sign-in still asks for its code.'),
    },
    {
      kind: 'done',
      title: t('Forgot password? works'),
      text: t('Accounts need an email address for it: add one under All accounts, Edit. The link opens on devices that can reach the address you open Usherr at.'),
      then: { id: 'family-member', label: t('Add a family member, kids too') },
    },
  ],
}
