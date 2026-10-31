// Usherr SMH in the setup wizard: on each step, what it is, why it matters,
// the choices with their good and bad sides, and whether it can be changed
// later. The coach card can't run here (the wizard comes before the app), so
// the wizard's own steps get the explanations instead. Folded or open is
// remembered in this browser.
import { useState } from 'react'
import Icon from '../components/Icon'
import { t } from '../i18n'
import DecisionCard from './DecisionCard'
import type { SmhDecision } from './types'
import './smh.css'

export type WizardStep = 'admin' | 'types' | 'paths' | 'services' | 'sources' | 'usenet' | 'quality' | 'server' | 'extras'

interface Help {
  what: string
  steps?: string[]
  card?: SmhDecision
  later: string
}

function help(step: WizardStep): Help {
  switch (step) {
    case 'admin':
      return {
        what: t('This is the administrator: the one account that can change everything. Everyone else in the house gets an account of their own later, with only the rights you give them.'),
        steps: [t('Pick a username and a password you don’t use anywhere else.'), t('Choose the app’s language at the top right if you like.')],
        later: t('Change your password any time from your profile.'),
      }
    case 'types':
      return {
        what: t('Usherr handles films and shows, and can also do anime, music, ebooks, audiobooks, comics and sports. Each one you pick adds its own steps and menus.'),
        card: {
          title: t('Start small, or switch on everything?'),
          options: [
            { name: t('Films and shows first'), good: t('Fewer questions now, and a tidy menu. Add the rest when you’re ready.'), bad: t('Music and books wait until you switch them on.') },
            { name: t('Everything you’ll use'), good: t('Set up in one go.'), bad: t('More to fill in now: folders and keys for each.') },
          ],
          recommend: t('Films and shows first.'),
          later: t('Switch any kind on or off later; switching one off deletes nothing.'),
        },
        later: t('Settings, Media types, any time.'),
      }
    case 'paths':
      return {
        what: t('Where your films, shows and downloads are, as Usherr sees them inside its container: /data/movies is the folder your setup file mapped (for example D:\\Movies on Windows).'),
        steps: [t('If you made your setup file with the Start page, the boxes are already right: press Continue.'), t('A box with a warning means that folder isn’t mapped in the setup file: check it there.')],
        later: t('Settings, Library, Folders; more folders can be added there too.'),
      }
    case 'services':
      return {
        what: t('Usherr gets every film and show’s details, posters and cast from TMDB, a free film database. It needs a free key from them; the other services here are optional.'),
        steps: [
          t('Go to themoviedb.org, make a free account, then Settings, API, and ask for a key (choose personal use).'),
          t('Copy the API key (the shorter one, not the long “read access token”) and paste it here. Usherr checks it right away.'),
        ],
        later: t('Settings, Info, lists and subtitles holds all the keys.'),
      }
    case 'sources':
      return {
        what: t('An indexer is a search engine for Usenet: it tells Usherr which downloads exist for a film or episode. You sign up on its website and paste your key here.'),
        card: {
          title: t('Which indexers?'),
          options: [
            { name: t('A free tier'), good: t('Costs nothing; fine to start.'), bad: t('A small number of searches a day.') },
            { name: t('A paid membership'), good: t('Many more searches a day, often better results.'), cost: t('roughly $10 to $20 a year') },
            { name: t('Prowlarr, if you have it'), good: t('Brings every indexer you already set up there, in one go.'), bad: t('Done later: Settings, Move from other apps.') },
          ],
          recommend: t('Two or three indexers find far more than one. Usherr spreads its searches out so the daily limits last.'),
          later: t('Add or remove indexers any time.'),
        },
        later: t('Settings, Indexers.'),
      }
    case 'usenet':
      return {
        what: t('The Usenet provider is the paid service the downloads come from. The provider e-mails you a server address, a username and a password when you sign up.'),
        card: {
          title: t('Which kind of plan?'),
          options: [
            { name: t('An unlimited plan'), good: t('One monthly price, as much as you like, fast. What most people use.'), cost: t('roughly a few dollars to $15 a month') },
            { name: t('A block account'), good: t('Pay once for a set amount: a good second provider, to fill the gaps of the first.'), bad: t('Runs out; not for everyday use on its own.') },
          ],
          recommend: t('One unlimited plan. Use port 563 with SSL on.'),
          later: t('Add a second provider later if downloads often fail as incomplete.'),
        },
        later: t('Settings, Usenet.'),
      }
    case 'quality':
      return {
        what: t('How good a copy Usherr looks for, and how the files are named in your folders.'),
        card: {
          title: t('1080p or 4K'),
          options: [
            { name: t('1080p (HD)'), good: t('Looks great on most TVs; plays everywhere, also over the internet.'), cost: t('roughly 2 to 15 GB a film') },
            { name: t('4K (UHD, often HDR)'), good: t('The best picture on a 4K TV.'), bad: t('Big files; some devices and streams struggle.'), cost: t('roughly 15 to 60 GB a film') },
          ],
          recommend: t('1080p for most of the library. For file names, the Jellyfin style is the safe pick if you use Jellyfin.'),
          later: t('Change any title’s quality later; existing files can be renamed in one go.'),
        },
        later: t('Settings, Quality; Settings, Naming.'),
      }
    case 'server':
      return {
        what: t('Connecting Jellyfin lets Usherr tell it when something new arrives (so it shows up at once), show Play in Jellyfin buttons, and keep your Diary from what you watch.'),
        steps: [
          t('The address: http://jellyfin:8096 if Jellyfin is in the same setup file as Usherr; otherwise this computer’s address, such as http://192.168.1.50:8096.'),
          t('The key: in Jellyfin, Dashboard, API Keys, press + and name it Usherr. Paste it here.'),
        ],
        later: t('Settings, Media servers.'),
      }
    case 'extras':
      return {
        what: t('Optional extras: streaming without downloading, the AI, ratings, what’s in theaters, the Marquee, the pre-show and more. None of them is needed to start.'),
        steps: [t('Skip anything you’re not sure about. Show me how, in the top bar, walks you through each one when you want it.')],
        later: t('Settings, Extras, any time.'),
      }
  }
}

const KEY = 'usherr.wizhelp.folded'

export default function WizardHelp({ step }: { step: WizardStep }) {
  const [folded, setFolded] = useState(() => {
    try {
      return localStorage.getItem(KEY) === '1'
    } catch {
      return false
    }
  })
  const h = help(step)
  return (
    <details
      className="wiz-help"
      open={!folded}
      onToggle={(e) => {
        const open = (e.currentTarget as HTMLDetailsElement).open
        setFolded(!open)
        try {
          localStorage.setItem(KEY, open ? '0' : '1')
        } catch {
          // only this visit then
        }
      }}
    >
      <summary>
        <Icon name="compass" size={15} /> {t('Show me how: this step')}
      </summary>
      <p>{h.what}</p>
      {h.steps && (
        <ol>
          {h.steps.map((s) => (
            <li key={s}>{s}</li>
          ))}
        </ol>
      )}
      {h.card && <DecisionCard d={h.card} />}
      {!h.card && (
        <p className="smh-card-later">
          <Icon name="refresh" size={13} /> {h.later}
        </p>
      )}
    </details>
  )
}
