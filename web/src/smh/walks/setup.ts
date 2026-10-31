// Usherr SMH: setting Usherr up. Each one leads to the next (media types,
// a Usenet provider, an indexer, quality), so a new administrator is walked
// through the lot; the rest stand on their own.
import { t } from '../../i18n'
import type { Walkthrough } from '../types'

export const mediaTypes: Walkthrough = {
  id: 'media-types',
  group: 'setup',
  admin: true,
  title: t('Choose what Usherr handles'),
  blurb: t('Films, shows, anime, music, books, comics, sports: switch on the kinds you want.'),
  icon: 'grid',
  words: 'media types modules switch on off music books comics anime setup',
  simple: [
    { kind: 'go', to: '/settings/modules', title: t('Media types'), text: t('Every kind of media Usherr can handle, each with its own switch.') },
    {
      kind: 'point',
      target: 'modules-grid',
      until: 'next',
      title: t('Switch on what you want'),
      text: t('A kind that’s off leaves the menus and Discover, and nothing is deleted: switch it back on and everything is where it was. Start with films and shows; add the others when you like.'),
    },
    { kind: 'done', title: t('Next: where the downloads come from'), text: t('Usherr gets films and shows from Usenet: a Usenet provider, and an indexer to search it.'), then: { id: 'usenet-provider', label: t('Add a Usenet provider') } },
  ],
}

export const usenetProvider: Walkthrough = {
  id: 'usenet-provider',
  group: 'setup',
  admin: true,
  title: t('Add a Usenet provider'),
  blurb: t('The service your downloads come from: what it is, how to choose one, and where it goes.'),
  icon: 'server',
  words: 'usenet provider news server nntp newshosting eweka download setup ssl connections',
  simple: [
    {
      kind: 'ask',
      key: 'kind',
      title: t('What is a Usenet provider?'),
      card: {
        title: t('Choosing a provider'),
        options: [
          { name: t('An unlimited plan'), good: t('One monthly price, download as much as you like, fast. What most people use.'), cost: t('roughly a few dollars to $15 a month') },
          { name: t('A block account'), good: t('Pay once for a set amount of data. Ideal as a second provider that fills the gaps the first one is missing.'), bad: t('Runs out; not for everyday use on its own.') },
        ],
        recommend: t('One unlimited plan; a block account from a different company later, if downloads often fail as incomplete.'),
        later: t('Add, change or remove providers any time.'),
      },
      choices: [
        { value: 'got', label: t('I have one') },
        { value: 'later', label: t('I’ll get one first') },
      ],
    },
    {
      kind: 'say',
      when: (c) => (c.answers.kind as { value: string } | undefined)?.value === 'later',
      title: t('Getting one'),
      text: t('Sign up on the provider’s own website (look for one with good retention and SSL). They e-mail you a server address, a username and a password. Then carry on here.'),
    },
    { kind: 'go', to: '/settings/usenet', title: t('Usenet'), text: t('Where your providers go.') },
    {
      kind: 'point',
      target: 'usenet-section',
      until: 'next',
      title: t('Your provider’s details'),
      text: t('The server address from their e-mail, port 563 with SSL on, your username and password, and how many connections your plan allows (often 20 to 50; more isn’t faster than your line). Test checks it, then Save.'),
    },
    { kind: 'done', title: t('Provider added'), text: t('Next, an indexer: the search engine that finds what’s on Usenet.'), then: { id: 'indexer', label: t('Add an indexer') } },
  ],
}

export const indexer: Walkthrough = {
  id: 'indexer',
  group: 'setup',
  admin: true,
  title: t('Add an indexer'),
  blurb: t('The search engine for Usenet: what it is, where its key comes from, and Prowlarr if you use it.'),
  icon: 'search',
  words: 'indexer newznab prowlarr api key search nzbgeek setup',
  simple: [
    {
      kind: 'ask',
      key: 'how',
      title: t('What is an indexer?'),
      card: {
        title: t('Adding indexers'),
        options: [
          { name: t('Add it here'), good: t('Pick your indexer from the list (or Newznab), paste its address and your API key from your account page there.'), bad: t('One by one.') },
          { name: t('Through Prowlarr'), good: t('Already use Prowlarr? Settings, Move from other apps brings every indexer over at once.'), bad: t('Prowlarr has to be running.') },
        ],
        recommend: t('Two or three indexers find far more than one. Most limit how many searches a day, so Usherr spreads its searches out.'),
        later: t('Add or remove indexers any time.'),
      },
      choices: [
        { value: 'here', label: t('Add it here'), recommended: true },
        { value: 'prowlarr', label: t('Through Prowlarr') },
      ],
    },
    { kind: 'go', when: (c) => (c.answers.how as { value: string } | undefined)?.value === 'prowlarr', to: '/settings/migrate', title: t('Move from other apps'), text: t('Connect Prowlarr with its address and API key (Prowlarr, Settings, General), and its indexers come over.') },
    { kind: 'go', when: (c) => (c.answers.how as { value: string } | undefined)?.value !== 'prowlarr', to: '/settings/indexers', title: t('Indexers'), text: t('Where your indexers go.') },
    {
      kind: 'point',
      target: 'indexer-add',
      until: 'next',
      when: (c) => (c.answers.how as { value: string } | undefined)?.value !== 'prowlarr',
      title: t('Add an indexer'),
      text: t('Pick it from the list, paste the API key from your account page on the indexer’s site, and Test: Usherr checks it answers before saving. Some indexers are invite-only or paid; most have a free tier with a daily limit.'),
    },
    { kind: 'done', title: t('Indexer added'), text: t('Last of the basics: how good a copy to get.'), then: { id: 'quality', label: t('Choose the quality') } },
  ],
}

export const quality: Walkthrough = {
  id: 'quality',
  group: 'setup',
  admin: true,
  title: t('Choose the quality'),
  blurb: t('1080p or 4K: what each costs in disk space, and what Usherr looks for.'),
  icon: 'sliders',
  words: 'quality profile 1080p 4k hdr size disk space resolution remux',
  simple: [
    {
      kind: 'ask',
      key: 'q',
      title: t('How good a copy?'),
      card: {
        title: t('1080p or 4K'),
        options: [
          { name: t('1080p (HD)'), good: t('Looks great on most TVs; plays everywhere, also over the internet.'), cost: t('roughly 2 to 15 GB a film') },
          { name: t('4K (UHD, often HDR)'), good: t('The best picture on a 4K TV.'), bad: t('Big files; some devices and streams struggle.'), cost: t('roughly 15 to 60 GB a film, more for a remux') },
        ],
        recommend: t('1080p for most of the library; 4K for the films you love, as a second copy if you like.'),
        later: t('Change a title’s profile any time; Usherr then looks for a copy that fits.'),
      },
      choices: [
        { value: '1080', label: t('1080p'), recommended: true },
        { value: '4k', label: t('4K') },
      ],
    },
    { kind: 'go', to: '/settings/quality', title: t('Quality'), text: t('Quality profiles say which qualities Usherr may take, and when to stop looking for better.') },
    {
      kind: 'point',
      target: 'quality-profiles',
      until: 'next',
      title: t('Your profiles'),
      text: (c) =>
        (c.answers.q as { value: string } | undefined)?.value === '4k'
          ? t('Make a 4K profile the default (Make default), or use it just for some titles when you add them.')
          : t('Make a 1080p profile the default (Make default). Every new title uses it unless you pick another when adding.'),
    },
    { kind: 'done', title: t('The basics are done'), text: t('Usherr can now find and download films and shows. Add family members, and point it at the library you already have, from Show me how.'), then: { id: 'import-library', label: t('Bring in what you already have') } },
  ],
}

export const tvTime: Walkthrough = {
  id: 'tv-time',
  group: 'setup',
  admin: true,
  title: t('Keep the TV smooth while downloading'),
  blurb: t('Schedules: slow downloads while the family watches, and do the heavy work at night.'),
  icon: 'clock',
  words: 'schedule tv time speed limit throttle quiet hours night bandwidth',
  simple: [
    { kind: 'go', to: '/settings/schedules', title: t('Schedules'), text: t('Times of the week when Usherr behaves differently.') },
    {
      kind: 'point',
      target: 'schedules-card',
      until: 'next',
      title: t('Make a schedule'),
      text: t('For example, TV time from 8 in the morning to midnight: downloads are limited (say to 15 MB/s) so streams and the TV never stutter, and they go full speed overnight. Pick the days, the hours, and what changes.'),
    },
    { kind: 'done', title: t('Smooth TV'), text: t('Heavy work (scans, conversions, Read-to-Me) can wait for quiet hours the same way.') },
  ],
}

export const vpn: Walkthrough = {
  id: 'vpn',
  group: 'setup',
  admin: true,
  title: t('Set up a VPN for torrents'),
  blurb: t('Keep torrent traffic private: what a VPN does here, and how to add yours.'),
  icon: 'shield',
  words: 'vpn wireguard torrents privacy mullvad proton pia',
  simple: [
    {
      kind: 'say',
      title: t('Do you need one?'),
      text: t('For torrents, yes in most places: everyone sharing a torrent can see your address. Usenet doesn’t need one; its connection is already encrypted (SSL). With a VPN on, only torrents go through it.'),
    },
    { kind: 'go', to: '/settings/vpn', title: t('VPN'), text: t('Your VPN connections and whether they’re working.') },
    {
      kind: 'point',
      target: 'vpn-add',
      until: 'next',
      title: t('Add your VPN'),
      text: t('Download a WireGuard file from your VPN provider’s website (Mullvad, Proton, PIA and others offer one) and load it here. Test makes a small real request through it and says whether traffic flows.'),
    },
    { kind: 'done', title: t('Torrents are private'), text: t('The VPN page shows whether it’s connected and when the VPN server last answered; Test checks it any time.') },
  ],
}

export const backups: Walkthrough = {
  id: 'backups',
  group: 'setup',
  admin: true,
  title: t('Backups and updates'),
  blurb: t('Keep a copy of your settings and library list, and keep Usherr up to date.'),
  icon: 'download',
  words: 'backup restore update upgrade version safety copy',
  simple: [
    { kind: 'go', to: '/settings/backup', title: t('Backups'), text: t('A backup holds Usherr’s settings and database: your library list, accounts, votes, diary. Not the films themselves.') },
    { kind: 'point', target: 'backup-card', until: 'next', title: t('Make a backup'), text: t('Download one now and keep it somewhere else (another drive, a cloud folder). Restore puts it back.') },
    { kind: 'go', to: '/settings/system', title: t('Updates'), text: t('New Usherr versions show up here.') },
    { kind: 'point', target: 'updates-card', until: 'next', title: t('Update now'), text: t('Installs the new version, checked against Usherr’s own signing key first, then restarts. Usherr saves a copy of its database before changing anything.') },
    { kind: 'done', title: t('You’re covered'), text: t('A backup before a big change is never wasted.') },
  ],
}

export const migrate: Walkthrough = {
  id: 'migrate',
  group: 'setup',
  admin: true,
  title: t('Move over from Radarr, Sonarr or Prowlarr'),
  blurb: t('Bring your films, shows, indexers and settings from the apps you use now; nothing is deleted there.'),
  icon: 'share',
  words: 'migrate move radarr sonarr prowlarr bazarr from other apps import settings',
  simple: [
    { kind: 'go', to: '/settings/migrate', title: t('Move from other apps'), text: t('Usherr reads what your current apps know and brings it over. Your old apps keep running and nothing in them is changed.') },
    {
      kind: 'point',
      target: 'migrate-connect',
      until: 'next',
      title: t('Connect your apps'),
      text: t('For each app: its address and its API key (in that app, Settings, General). Then pick what to bring: the library, indexers, download clients, quality profiles.'),
    },
    { kind: 'done', title: t('Moving in'), text: t('Run it again to pick up anything added in the old apps since. When you’re happy, you can switch them off.') },
  ],
}
