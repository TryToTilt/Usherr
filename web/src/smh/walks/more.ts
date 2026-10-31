// Usherr SMH: the last few. A music artist, import lists, file naming,
// download clients, and connecting Jellyfin after setup.
import { t } from '../../i18n'
import type { Walkthrough } from '../types'

export const addArtist: Walkthrough = {
  id: 'add-artist',
  group: 'listen',
  needs: 'music',
  title: t('Get music by an artist'),
  blurb: t('Add an artist and choose which albums Usherr gets: all of them, only new ones, or ones you pick.'),
  icon: 'music',
  words: 'music artist band album add get follow discography',
  simple: [
    { kind: 'go', to: '/discover?kind=music', title: t('Music on Discover'), text: t('Popular albums, new releases and what’s coming. To find an artist by name, type it in the search at the top of the page.') },
    {
      kind: 'say',
      title: t('Add'),
      text: t('On an album or an artist, press Add. (An album you add brings its artist along, with only that album wanted.)'),
    },
    {
      kind: 'ask',
      key: 'which',
      title: t('Which albums?'),
      card: {
        title: t('What to follow'),
        options: [
          { name: t('Follow all albums'), good: t('Every album, EP and single, and new releases as they come out.'), bad: t('A long career can be a lot to download.') },
          { name: t('Only future albums'), good: t('Just what’s still to come.'), bad: t('Nothing older.') },
          { name: t('Just add the artist'), good: t('Nothing yet: pick the albums you want on their page.'), bad: t('You do the picking.') },
        ],
        recommend: t('Follow all albums for an artist you love; Just add the artist to pick a few.'),
        later: t('Switch any album on or off later, on the artist’s page.'),
      },
      choices: [
        { value: 'all', label: t('Follow all albums'), recommended: true },
        { value: 'future', label: t('Only future albums') },
        { value: 'none', label: t('Just add the artist') },
      ],
    },
    {
      kind: 'point',
      target: 'music-add-dialog',
      until: { gone: 'music-add-dialog' },
      optional: true,
      title: t('Choose it in the window'),
      text: (c) => t('Pick {choice}, keep the usual quality, and press Add.', { choice: (c.answers.which as { label: string } | undefined)?.label ?? '' }),
    },
    { kind: 'done', title: t('The music is on its way'), text: t('Only live albums, compilations and the like are left out by default, so the list is the artist’s own records.'), shortcut: t('Next time: Discover, Music, then Add.') },
  ],
}

export const importLists: Walkthrough = {
  id: 'import-lists',
  group: 'setup',
  admin: true,
  title: t('Follow a list from Trakt, IMDb or Letterboxd'),
  blurb: t('A list somewhere else, read on a schedule: whatever is new on it comes into your library.'),
  icon: 'list',
  words: 'import list lists trakt imdb letterboxd tmdb mdblist simkl watchlist follow sync',
  simple: [
    {
      kind: 'say',
      title: t('What a list does'),
      text: t('An import list is a list on Trakt, TMDB, IMDb, MDBList, Simkl or Letterboxd that Usherr reads now and then. Whatever is new on it is added, with the quality, folder and tags you choose for the list. Nothing is ever removed, and what you marked Not interested is skipped.'),
    },
    { kind: 'go', to: '/settings/lists', title: t('Import lists'), text: t('Trakt and Simkl need a key here first; TMDB, IMDb and Letterboxd lists don’t.') },
    {
      kind: 'point',
      target: 'lists-page',
      until: 'next',
      title: t('Add a list'),
      text: t('Pick where the list lives and paste its address (or, for IMDb and Letterboxd, choose the exported CSV file). Then the quality, folder, monitoring and tags for what it adds.'),
    },
    { kind: 'done', title: t('Your list is followed'), text: t('A title is added once: remove it later and the list doesn’t bring it back.') },
  ],
}

export const naming: Walkthrough = {
  id: 'naming',
  group: 'setup',
  admin: true,
  title: t('Choose how files are named'),
  blurb: t('How films and episodes are named and filed, to suit Jellyfin, Plex or your own taste.'),
  icon: 'edit',
  words: 'naming rename file names folders preset jellyfin plex format',
  simple: [
    { kind: 'go', to: '/settings/naming', title: t('Naming'), text: t('How each new download is named and filed.') },
    {
      kind: 'point',
      target: 'naming-page',
      until: 'next',
      title: t('Pick a preset'),
      text: t('Naming preset suits your player: the Jellyfin one for Jellyfin (it’s what the setup picks). The preview shows a film and an episode as they’ll be named. Keep the original file names, if you prefer the release’s own names.'),
    },
    {
      kind: 'say',
      title: t('Files you already have'),
      text: t('They keep their names until you rename them: the Library can rename existing files to the new style in one go, and nothing is overwritten.'),
    },
    { kind: 'done', title: t('Naming is set'), text: t('Jellyfin matches films and shows by folder and file name, so the Jellyfin preset is the safe choice.') },
  ],
}

export const downloadClients: Walkthrough = {
  id: 'download-clients',
  group: 'setup',
  admin: true,
  title: t('Download clients'),
  blurb: t('Usherr downloads by itself; connect SABnzbd, qBittorrent or a debrid account only if you want to.'),
  icon: 'download',
  words: 'download client sabnzbd nzbget qbittorrent transmission debrid real-debrid torbox',
  simple: [
    {
      kind: 'ask',
      key: 'need',
      title: t('Do you need one?'),
      card: {
        title: t('Built in, or your own downloader'),
        options: [
          { name: t('Usherr’s own (built in)'), good: t('Downloads from Usenet and torrents by itself: nothing else to install or connect.'), bad: t('One more job for the computer running Usherr.') },
          { name: t('Your own (SABnzbd, qBittorrent…)'), good: t('Keep the downloader you know, its settings and its own screen.'), bad: t('One more app to run and connect.') },
          { name: t('A debrid account'), good: t('Torrents download on the debrid service’s fast servers, not yours.'), cost: t('a paid subscription') },
        ],
        recommend: t('The built-in one, unless you already run a downloader you like.'),
        later: t('Connect or disconnect a client any time.'),
      },
      choices: [
        { value: 'builtin', label: t('The built-in one is fine'), recommended: true },
        { value: 'own', label: t('Connect my own') },
      ],
    },
    { kind: 'go', to: '/queue', title: t('Activity'), text: t('Download clients live under Activity, on their own tab.') },
    {
      kind: 'point',
      target: 'queue-clients',
      act: 'click',
      until: 'next',
      title: t('Clients'),
      text: (c) =>
        (c.answers.need as { value: string } | undefined)?.value === 'own'
          ? t('Add your downloader here: its address, and its API key or password. Test checks Usherr can reach it.')
          : t('The built-in Usenet downloader and torrent client are here, with their speed and what’s left. Nothing to set up.'),
    },
    { kind: 'done', title: t('That’s the download clients'), text: t('Each client’s speed and what’s left show in Activity.') },
  ],
}

export const connectJellyfin: Walkthrough = {
  id: 'connect-jellyfin',
  group: 'setup',
  admin: true,
  title: t('Connect Jellyfin'),
  blurb: t('So new films show up in Jellyfin at once, Play in Jellyfin buttons appear, and your Diary fills itself.'),
  icon: 'tv',
  words: 'jellyfin plex emby media server connect api key refresh watch in',
  simple: [
    {
      kind: 'say',
      title: t('What connecting does'),
      text: t('After every download Usherr tells Jellyfin to look at the new folder, so it appears there within a minute. Title pages get a Watch in Jellyfin button, and what you finish in Jellyfin fills your Diary. Plex and Emby work too.'),
    },
    {
      kind: 'say',
      title: t('Get a key from Jellyfin'),
      text: t('In Jellyfin: Dashboard, API Keys, press + and name it Usherr. Copy the key.'),
    },
    { kind: 'go', to: '/settings/media-servers', title: t('Media servers'), text: t('Where Jellyfin, Plex and Emby are connected.') },
    {
      kind: 'point',
      target: 'servers-page',
      until: 'next',
      title: t('Add Jellyfin'),
      text: t('The address: http://jellyfin:8096 when Jellyfin is in the same setup file as Usherr, otherwise this computer’s address (http://192.168.1.50:8096, say). Paste the key, Test, Save.'),
    },
    { kind: 'done', title: t('Jellyfin is connected'), text: t('Re-Posterr, Live TV in Jellyfin and the Diary all use this connection.') },
  ],
}
