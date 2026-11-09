// Usherr SMH: listening and reading. Music (playing it, playlists, bringing
// in a collection) and books (getting one, the audiobook player, the
// reader). Usherr Books opens in a window of its own, where the coach can't
// follow, so those two show the way in and say what's there.
import { t } from '../../i18n'
import type { Walkthrough } from '../types'
import { bc, PRESS } from '../look' // Usherr Broadcast: words for its own pages

// Usherr Broadcast: the players' button is in the menu there, not the top bar.
const playersText = bc(t('In the menu, the play button opens the players and apps.'), t('The play button at the top opens the players and apps.'))

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
  words: 'audiobook listen player books sleep timer speed chapters bookmark skip',
  simple: [
    { kind: 'point', target: 'tool-play', act: 'click', until: { seen: 'books-listen' }, title: t('Players'), text: playersText, remote: bc(PRESS.menu, '') },
    { kind: 'point', target: 'books-listen', until: 'next', title: t('Usherr Books'), text: t('Opens your audiobooks in a window of its own. Tap a book’s Listen to start.') },
    {
      kind: 'say',
      title: t('The player'),
      text: t('The skip buttons go back and on (choose 5 to 60 seconds in the player’s settings); the arrows go a chapter back or on. The speed button goes from 0.8× to 2×. Sleep timer stops after 15 to 60 minutes, or at the end of the chapter. Bookmark keeps a spot, with a note if you like. After a pause it goes back a little, so you don’t lose the thread. Your place is kept on every device.'),
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
  words: 'ebook read reader kindle epub kobo e-reader opds koreader readest send email highlight scroll page turn curl',
  simple: [
    { kind: 'point', target: 'tool-play', act: 'click', until: { seen: 'books-read' }, title: t('Players'), text: playersText, remote: bc(PRESS.menu, '') },
    { kind: 'point', target: 'books-read', until: 'next', title: t('Usherr Books'), text: t('Opens your ebooks in a window of its own. Tap a book’s Read to open it.') },
    {
      kind: 'say',
      title: t('The reader'),
      text: t('Turn pages with the arrows, the arrow keys, the space bar or a swipe: the page curls like paper after your finger. Contents jumps to a chapter; the slider to any place. Aa sets the text size, line spacing, margins, the font, the page colour (a black page too), Page turn (Curl, Slide or None) and Reading (Pages, or Scroll as one long column). Highlight words in four colours, add notes and bookmarks. Tap a footnote mark to read the note. Kindle books and PDFs open too.'),
    },
    {
      kind: 'say',
      title: t('On an e-reader'),
      text: t('Your profile page has Read on an e-reader: Kobo sync puts your books on a Kobo, the OPDS catalogue works in reading apps such as KOReader, and KOReader and Readest keep your place with Usherr’s reader, signing in with your app password. Save your Kindle’s address on your profile, and Send to e-reader on a book’s page emails it there.'),
    },
    { kind: 'done', title: t('Happy reading'), text: t('Your place is kept on the server, so a book opens where you stopped, on any device.') },
  ],
}

// ---- Tidy up the music (Picard-style) ----

export const tidyMusic: Walkthrough = {
  id: 'tidy-music',
  group: 'library',
  needs: 'music',
  admin: true,
  title: t('Tidy up your music'),
  blurb: t('Neat folder and file names, correct tags and covers, and albums you have twice: you see it all before anything changes.'),
  icon: 'sparkles',
  words: 'music tidy clean up rename tags covers picard beets musicbrainz acoustid duplicates albums folder names',
  simple: [
    { kind: 'go', to: '/music/tidy', title: t('Tidy up my music'), text: t('Music, Tidy up. Usherr works out what each album really is, then shows the new names and tags. Checking changes nothing.') },
    {
      kind: 'point',
      target: 'tidy-check',
      act: 'click',
      until: 'next',
      title: t('Check my music'),
      text: t('Safe to run as often as you like. The music database allows one lookup a second, so a big collection takes a while: you can leave the page, the check keeps going.'),
    },
    {
      kind: 'point',
      target: 'tidy-key',
      until: 'next',
      optional: true,
      title: t('Recognise music by its sound'),
      text: t('Optional: a free AcoustID application key lets Usherr recognise albums whose tags say too little. Paste it here and Save.'),
    },
    {
      kind: 'point',
      target: 'tidy-ready',
      until: 'next',
      optional: true,
      title: t('Pick the albums'),
      text: t('Each album shows its new folder and file names, tags and cover. Start with a few albums you know, tick them, and press Tidy. Albums you have twice are shown too.'),
    },
    {
      kind: 'say',
      when: () => !document.querySelector('[data-walk="tidy-ready"]'),
      title: t('When the check is done'),
      text: t('Come back to this page: the albums ready to tidy are listed here, each with what would change. Nothing changes until you tick albums and press Tidy.'),
    },
    {
      kind: 'done',
      title: t('Tidy, and safe'),
      text: t('Originals are kept for 30 days, and Undo under Earlier tidying puts an album back as it was.'),
    },
  ],
}

// ---- Music in a phone app (Subsonic) ----

export const musicApps: Walkthrough = {
  id: 'music-apps',
  group: 'listen',
  needs: 'music',
  title: t('Play your music in a phone app'),
  blurb: t('Symfonium, DSub, Substreamer, Feishin and other Subsonic apps play your music from Usherr, with your app password.'),
  icon: 'headphones',
  words: 'music phone app subsonic opensubsonic symfonium dsub substreamer play:sub feishin app password last.fm scrobble listenbrainz mobile data',
  simple: [
    { kind: 'go', to: '/settings/services', title: t('Linked services'), text: bc(t('Under You, Your settings.'), t('Under Settings, Account & Access.')) },
    {
      kind: 'point',
      target: 'phone-apps',
      until: 'next',
      title: t('Phone apps'),
      text: t('Switch on Let phone apps sign in, then press Make an app password and copy it into the app now: it isn’t shown again. In the app, add a Subsonic server with the Server address and User name shown here. If it says an administrator switched phone apps off, ask one to switch them on.'),
    },
    {
      kind: 'point',
      target: 'lastfm',
      until: 'next',
      optional: true,
      title: t('Last.fm too?'),
      text: t('Connect your own Last.fm account here and what you play goes there (and to ListenBrainz). The heart in Usherr’s player loves the track on Last.fm too.'),
    },
    { kind: 'done', title: t('Your music, in your pocket'), text: t('The app password is the same one e-readers use: making a new one changes both. On mobile data, the player can ask for smaller files.') },
  ],
}
