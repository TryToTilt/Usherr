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
  blurb: t('Forgot password? on the sign-in page emails a link that sets a new password, so nobody needs you or the command line. Set up email (Gmail, say), then switch it on.'),
  icon: 'mail',
  words: 'forgot forgotten password reset email link sign in locked out gmail outlook smtp app password',
  simple: [
    {
      kind: 'ask',
      key: 'mail',
      title: t('Does Usherr send email yet?'),
      text: t('The link goes out through an email notification’s mail server, only to the account’s own address. If Send a test on an email notification already works, skip ahead.'),
      choices: [
        { value: 'no', label: t('Not yet: set it up with Gmail'), recommended: true },
        { value: 'yes', label: t('Yes, email works') },
      ],
    },
    {
      kind: 'say',
      when: (c) => (c.answers.mail as { value: string } | undefined)?.value !== 'yes',
      title: t('First, a Gmail app password'),
      text: t('Gmail won’t take your normal password here. In your Google account, switch on 2-Step Verification (Security), then open myaccount.google.com/apppasswords, type Usherr and press Create. Google shows 16 letters in four groups: copy them and type them WITHOUT THE SPACES. With the spaces, Gmail refuses it (error 534).'),
    },
    { kind: 'go', when: (c) => (c.answers.mail as { value: string } | undefined)?.value !== 'yes', to: '/settings/notifications', title: t('Notifications'), text: t('Where Usherr’s email is set up.') },
    {
      kind: 'point',
      when: (c) => (c.answers.mail as { value: string } | undefined)?.value !== 'yes',
      target: 'notif-add',
      until: 'next',
      title: t('Add an email notification'),
      text: t('Pick Email. SMTP server: smtp.gmail.com (type it: the grey text is only an example). Port 587, STARTTLS. Username: your Gmail address. Password: the app password, no spaces. From address: your Gmail address too. Send to: where notifications go. Press Send a test: when it ends with Done: accepted by the server, press Add Email.'),
    },
    { kind: 'go', to: '/settings/general', title: t('The address you open Usherr at'), text: t('The link in the email starts with it.') },
    {
      kind: 'point',
      target: 'public-address',
      until: 'next',
      title: t('Your Usherr address'),
      text: t('The address your phones and computers open Usherr at, for example your Tailscale name with :8264. It saves by itself. A link opens on any device that can reach this address.'),
    },
    { kind: 'go', to: '/settings/profile', title: t('Your profile'), text: t('The link goes to the email address on the account.') },
    {
      kind: 'point',
      target: 'profile-email',
      until: 'next',
      title: t('Your email address'),
      text: t('Fill it in and save. For the others in the house: All accounts, Edit, Email. An account without one can’t use the email reset (an administrator sets its password instead).'),
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
      kind: 'say',
      title: t('Try it'),
      text: t('In a private window, open Usherr, press Forgot your password?, type your username and press Email me a link. The email comes within a minute (look in spam the first time). You don’t have to set a new password: the link simply stops working after 30 minutes.'),
    },
    {
      kind: 'done',
      title: t('Forgot password? works'),
      text: t('Anyone with an email address on their account can now reset a forgotten password themselves. Every request and reset shows on Activity.'),
      then: { id: 'family-member', label: t('Add a family member, kids too') },
    },
  ],
}
