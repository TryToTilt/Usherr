// Usherr SMH: listening and reading. Music (playing it, playlists, bringing
// in a collection) and books (getting one, the audiobook player, the
// reader). Usherr Books opens in a window of its own, where the coach can't
// follow, so those two show the way in and say what's there.
import { t } from '../../i18n'
import type { Walkthrough } from '../types'

export const playMusic: Walkthrough = {
  id: 'play-music',
  group: 'listen',
  needs: 'music',
  title: t('Play some music'),
  blurb: t('Pick an artist, press Play, and it keeps playing while you move around Usherr.'),
  icon: 'music',
  words: 'music play album artist song listen shuffle queue lyrics',
  simple: [
    { kind: 'go', to: '/library?kind=music', title: t('Your music'), text: t('Your artists, with how many of their albums you have.') },
    { kind: 'point', target: 'music-grid', until: { path: '/music/artist/*' }, title: t('Pick an artist'), text: t('Tap an artist. (Find one by name in the box above.)') },
    {
      kind: 'point',
      target: 'artist-play',
      act: 'click',
      until: 'next',
      title: t('Play'),
      text: (c) => (c.watch ? t('Playing it for you: everything of theirs you have, newest album first.') : t('Play queues everything of theirs you have, newest album first; Shuffle mixes it. Each album and track has its own play button too.')),
    },
    {
      kind: 'point',
      target: 'music-bar',
      until: 'next',
      optional: true,
      title: t('The player'),
      text: t('It stays at the bottom while you move around. Queue shows what’s coming (drag to reorder), Lyrics shows the words, and the window button moves the music into a small window of its own. Media keys and headphone buttons work.'),
    },
    { kind: 'done', title: t('Enjoy the music'), text: t('The … menu on any track, album or artist has Play next, Add to queue and Add to playlist.'), shortcut: t('Next time: Library, Music, an artist, Play.') },
  ],
}

export const makePlaylist: Walkthrough = {
  id: 'playlist',
  group: 'listen',
  needs: 'music',
  title: t('Make a playlist'),
  blurb: t('Your own list of tracks, private or shared with the household.'),
  icon: 'playlist-add',
  words: 'playlist music list mix share tracks',
  simple: [
    { kind: 'go', to: '/music/playlists', title: t('Playlists'), text: t('Yours, and the ones others in the house shared.') },
    {
      kind: 'point',
      target: 'pl-create',
      until: 'next',
      title: t('Name it'),
      text: t('Type a name and press New playlist. Share with everyone lets the household play it (only you can change it).'),
    },
    {
      kind: 'say',
      title: t('Fill it'),
      text: t('On any track, album or artist, the … menu has Add to playlist. An album adds its tracks in order; an artist adds their top 20. In the player, Save as playlist turns the whole queue into one.'),
    },
    { kind: 'done', title: t('Your playlist is ready'), text: t('On a playlist: play, shuffle, drag tracks into order, rename it.') },
  ],
}

export const importMusic: Walkthrough = {
  id: 'import-music',
  group: 'library',
  needs: 'music',
  admin: true,
  title: t('Bring in the music you already have'),
  blurb: t('Usherr finds your existing collection where it is: nothing is moved or changed.'),
  icon: 'disc',
  words: 'import music collection existing scan folder albums',
  simple: [
    {
      kind: 'go',
      to: '/music/import',
      title: t('Import my music collection'),
      text: t('Usherr reads your music folder (Artist/Album/tracks works best), looks each album up, and adds what it recognises, where it is. No file is moved, renamed or changed, and only the albums it found are wanted, so nothing starts downloading.'),
    },
    {
      kind: 'point',
      target: 'music-scan',
      until: 'next',
      title: t('Start scanning'),
      text: t('Press Start scanning. It looks albums up one a second, so a big collection takes a while; you can leave the page and come back.'),
    },
    {
      kind: 'say',
      title: t('The results'),
      text: t('Added, already in your library, and not matched, with the reason and the folder. Rename a folder and scan again to fix one: scanning again is safe.'),
    },
    { kind: 'done', title: t('Your collection is in'), text: t('Every artist found is in Library, Music.') },
  ],
}

export const getBook: Walkthrough = {
  id: 'get-book',
  group: 'read',
  needs: 'ebooks',
  title: t('Get a book'),
  blurb: t('Find an ebook (or audiobook) by title or author and Usherr gets it.'),
  icon: 'book',
  words: 'book ebook audiobook get add find author title download',
  simple: [
    { kind: 'go', to: '/library?kind=ebook', title: t('Your ebooks'), text: t('Every book you have, and the ones Usherr is still looking for.') },
    { kind: 'point', target: 'books-add', act: 'click', until: { seen: 'books-search' }, title: t('Add'), text: t('Opens the book search.') },
    {
      kind: 'point',
      target: 'books-search',
      until: 'next',
      title: t('Find it'),
      text: t('Type a title or an author, then Add on the right one. Tick Get the audiobook too to want both. Usherr starts looking straight away.'),
    },
    { kind: 'done', title: t('Your book is on its way'), text: t('Discover has an eBooks & Audiobooks tab too: what people are reading, with a banner saying which versions exist.'), shortcut: t('Next time: Library, Ebooks, Add.') },
  ],
}

export const listenAudiobook: Walkthrough = {
  id: 'listen-audiobook',
  group: 'listen',
  needs: 'audiobooks',
  title: t('Listen to an audiobook'),
  blurb: t('Usherr Books: your audiobooks, with chapters, speed, a sleep timer, and your place kept on every device.'),
  icon: 'headphones',
  words: 'audiobook listen player books sleep timer speed chapters',
  simple: [
    { kind: 'point', target: 'tool-play', act: 'click', until: { seen: 'books-listen' }, title: t('Players'), text: t('The play button at the top opens the players and apps.') },
    { kind: 'point', target: 'books-listen', until: 'next', title: t('Usherr Books'), text: t('Opens your audiobooks in a window of its own. Tap a book’s Listen to start.') },
    {
      kind: 'say',
      title: t('The player'),
      text: t('−30 and +30 skip half a minute; the arrows go a chapter back or on. The speed button goes from 0.8× to 2×. Sleep timer stops after 15 to 60 minutes, or at the end of the chapter. Your place is kept, so it carries on where you stopped, on any device.'),
    },
    {
      kind: 'say',
      title: t('Make it an app'),
      text: t('In Usherr Books, choose Install in the address bar (on a phone: Add to Home Screen). It gets its own icon. On a phone, the lock screen and headphone buttons work.'),
    },
    { kind: 'done', title: t('Happy listening'), text: t('Want an audiobook of an ebook you have? Turn an ebook into an audiobook, in Show me how.') },
  ],
}

export const readEbook: Walkthrough = {
  id: 'read-ebook',
  group: 'read',
  needs: 'ebooks',
  title: t('Read an ebook'),
  blurb: t('Usherr Books: a reader with your text size and colours, and your place kept on every device. Or send books to an e-reader.'),
  icon: 'book',
  words: 'ebook read reader kindle epub kobo e-reader opds',
  simple: [
    { kind: 'point', target: 'tool-play', act: 'click', until: { seen: 'books-read' }, title: t('Players'), text: t('The play button at the top opens the players and apps.') },
    { kind: 'point', target: 'books-read', until: 'next', title: t('Usherr Books'), text: t('Opens your ebooks in a window of its own. Tap a book’s Read to open it.') },
    {
      kind: 'say',
      title: t('The reader'),
      text: t('Turn pages with the arrows, the arrow keys, the space bar or a swipe. Contents jumps to a chapter; the slider to any place. Aa sets the text size, the page colour (light, sepia, dark), the font and how pages turn. Kindle books open too.'),
    },
    {
      kind: 'say',
      title: t('On an e-reader'),
      text: t('Your profile page has Read on an e-reader: Kobo sync puts your books on a Kobo, and the OPDS catalogue works in reading apps such as KOReader. Each only reads; it can’t change anything.'),
    },
    { kind: 'done', title: t('Happy reading'), text: t('Your place is kept on the server, so a book opens where you stopped, on any device.') },
  ],
}
