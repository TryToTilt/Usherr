// Usherr SMH: Plex and the virtual drive. Plex can't play the Streams
// library's pointer files, so streamed titles reach it through a drive that
// Usherr serves and rclone mounts on the computer that runs Plex.
import { t } from '../../i18n'
import type { Walkthrough } from '../types'

export const plexDrive: Walkthrough = {
  id: 'plex-drive',
  group: 'setup',
  admin: true,
  title: t('Set up Plex with the virtual drive'),
  blurb: t('Streamed films and shows in Plex: Usherr serves them as a drive of ordinary video files, and rclone puts it on your computer.'),
  icon: 'server',
  words: 'plex virtual drive webdav rclone winfsp mount streams streamed library',
  simple: [
    {
      kind: 'ask',
      key: 'why',
      title: t('Jellyfin or Plex for your streams?'),
      card: {
        title: t('How streamed titles reach your media server'),
        options: [
          { name: t('Jellyfin: pointer files'), good: t('Nothing to install: Jellyfin plays the Streams library’s small pointer files.'), bad: t('Plex ignores them.') },
          { name: t('Plex: the virtual drive'), good: t('Plex sees ordinary video files and plays them like any other.'), bad: t('rclone and WinFsp to install once, and a file is on the drive only once its release is known (played, or looked for a few a day).') },
        ],
        recommend: t('The virtual drive if you watch in Plex; both can run side by side.'),
        later: t('Switch the drive on or off any time; your Streams library doesn’t change.'),
      },
      choices: [
        { value: 'plex', label: t('Plex: set up the drive'), recommended: true },
        { value: 'jf', label: t('Jellyfin: I don’t need it') },
      ],
    },
    {
      kind: 'done',
      when: (c) => (c.answers.why as { value: string } | undefined)?.value === 'jf',
      title: t('Nothing to do for Jellyfin'),
      text: t('Jellyfin plays the Streams library as it is.'),
    },
    { kind: 'go', to: '/settings/extras', title: t('The virtual drive'), text: t('Its card is under Extras.') },
    {
      kind: 'point',
      target: 'drive-card',
      until: 'next',
      title: t('Switch it on'),
      text: t('Serve the virtual drive. It’s already on if Plex is your media server and Jellyfin isn’t.'),
    },
    {
      kind: 'point',
      target: 'drive-status',
      until: 'next',
      optional: true,
      title: t('How far along it is'),
      text: t('Each streamed film or episode appears once Usherr knows its release: anything someone plays is kept at once, and the rest are looked for a few a day, so your indexers’ daily limits are never spent in one go.'),
    },
    {
      kind: 'point',
      target: 'drive-steps',
      until: 'next',
      optional: true,
      title: t('Mount it on the Plex computer'),
      text: t('Install WinFsp and rclone from their own sites (free), then copy the two commands into a terminal: the first tells rclone about the drive, the second mounts it as drive U:.'),
    },
    {
      kind: 'ask',
      key: 'startup',
      title: t('Mount it every time Windows starts?'),
      text: t('Optional. Usherr can give you a small file for your Windows Startup folder that mounts the drive each time you sign in. It is installed as a startup item: it runs at every sign-in until you delete it. Without it, you run the mount command yourself after each restart.'),
      choices: [
        { value: 'yes', label: t('Yes, mount it at sign-in') },
        { value: 'no', label: t('No, I’ll mount it myself') },
      ],
    },
    {
      kind: 'point',
      when: (c) => (c.answers.startup as { value: string } | undefined)?.value === 'yes',
      target: 'drive-startup',
      until: 'next',
      title: t('Your startup file'),
      text: t('Tick the box and read the warning, then download usherr-drive.cmd. Press Windows+R, type shell:startup and press Enter, and move the file into the folder that opens.'),
    },
    {
      kind: 'say',
      title: t('Add it to Plex, the light way'),
      text: t('In Plex, add U:\\Movies to a Movies library and U:\\TV and U:\\Anime to a TV library. In Plex, Settings, Library: set video preview thumbnails, chapter thumbnails and voice activity data to never, scan periodically (Windows doesn’t tell Plex about new files on the drive), and untick Empty trash automatically, so a moment without the drive doesn’t empty its titles. In Scheduled Tasks, untick extensive media analysis. Each of these would read whole films from Usenet.'),
    },
    {
      kind: 'done',
      title: t('Plex has your streams'),
      text: t('New streamed titles appear on the drive as their releases are found. If Plex stops seeing it, Something not right? in Show me how checks the drive.'),
    },
  ],
}
