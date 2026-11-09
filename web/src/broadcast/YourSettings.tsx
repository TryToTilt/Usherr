// Usherr Broadcast: You > Your settings (the "Look and language" page,
// /settings/general), as the mock-up has it: one row per setting, Left and
// Right change it, and what the focused row does is explained beside the list.
// The look and the language reload the page, so those take effect on OK. Then
// your other settings pages, one row each. Everything here is kept in this
// browser, as before; the same choices as the cards in other looks.
import { useState } from 'react'
import { isAdmin, isViewer } from '../api'
import { useAuth } from '../AuthContext'
import { MENU_HIDE_EVENT, menuHideSeconds, STORAGE_KEY as MENU_HIDE_KEY } from '../components/menuHide'
import { chooseLanguage, currentLanguage, LANGUAGES, t, type Lang } from '../i18n'
import { chooseLook, chooseTvSize, savedLook, tvSizeOn, type Look } from '../neu/look'
import { chooseMenuMode, menuMode, type MenuMode } from '../onemenu/menuMode'
import { YOUR_SETTINGS } from '../onemenu/broadcastNav'
import { settingsGroupsFor } from '../settingsNav'
import { ChoiceRow, LinkRow, SelectRow } from './Rows'
import './screens.css'

const THEME_KEY = 'mediarium-theme' // components/ThemeToggle.tsx

function savedTheme(): 'light' | 'dark' {
  try {
    return localStorage.getItem(THEME_KEY) === 'light' ? 'light' : 'dark'
  } catch {
    return 'dark'
  }
}

function keep(key: string, value: string) {
  try {
    localStorage.setItem(key, value)
  } catch {
    // a blocked localStorage just means the choice will not survive a reload
  }
}

export default function YourSettings() {
  const { user } = useAuth()
  const [mode, setMode] = useState<MenuMode>(menuMode)
  const [hide, setHide] = useState(menuHideSeconds)
  const [theme, setTheme] = useState(savedTheme)
  const [tv, setTv] = useState(tvSizeOn)
  const [help, setHelp] = useState<{ title: string; text: string }>({ title: t('The menu'), text: t('Try each one, then press Left at the edge of any page. With a mouse it also follows the next setting.') })
  const say = (title: string, text: string) => () => setHelp({ title, text })

  const mine = settingsGroupsFor(isAdmin(user), isViewer(user)).find((g) => g.key === YOUR_SETTINGS)?.pages.filter((p) => p.to !== '/settings/general') ?? []
  const secs = [0, 1, 3, 5, 10]

  return (
    <div className="b-ys span-all">
      <div className="b-ys-rows" role="group" aria-label={t('Your settings')}>
        <ChoiceRow
          autoFocus
          walk="menu-mode"
          label={t('The menu')}
          choices={[
            { id: 'open', label: t('Always open') },
            { id: 'icons', label: t('Icons, opens when you go left') },
            { id: 'hidden', label: t('Hidden, opens when you go left') },
          ]}
          value={mode}
          onChoose={(m) => {
            setMode(m)
            chooseMenuMode(m)
          }}
          onFocus={say(t('The menu'), t('Try each one, then press Left at the edge of any page. With a mouse it also follows the next setting.'))}
        />
        <ChoiceRow
          label={t('With a mouse, the menu slides away')}
          choices={secs.map((s) => ({ id: String(s), label: s === 0 ? t('Right away') : s === 1 ? t('After 1 second') : t('After {n} seconds', { n: s }) }))}
          value={String(hide)}
          onChoose={(v) => {
            const n = Number(v)
            setHide(n)
            keep(MENU_HIDE_KEY, String(n))
            window.dispatchEvent(new Event(MENU_HIDE_EVENT))
          }}
          onFocus={say(t('With a mouse, the menu slides away'), t('Only matters with a mouse or touch: how long the open menu waits after the pointer leaves.'))}
        />
        <ChoiceRow
          applyOnOk
          walk="look-row"
          label={t('Look')}
          choices={[
            { id: 'broadcast', label: t('Broadcast') },
            { id: 'neu', label: t('Usherr Neu') },
            { id: 'lobby', label: t('Neu, new layout') },
            { id: 'classic', label: t('Usherr Classic') },
          ]}
          value={savedLook()}
          onChoose={(l: Look) => chooseLook(l)}
          onFocus={say(t('Look'), t('Broadcast is this look, made for a TV remote as well as a mouse. Classic and Neu stay for computers. Switching reloads the page.'))}
        />
        <ChoiceRow
          label={t('Light or dark')}
          choices={[
            { id: 'dark', label: t('Dark') },
            { id: 'light', label: t('Light') },
          ]}
          value={theme}
          onChoose={(v) => {
            setTheme(v)
            keep(THEME_KEY, v)
            document.documentElement.dataset.theme = v
          }}
          onFocus={say(t('Light or dark'), t('Broadcast is made dark first, for a TV in a dim room.'))}
        />
        <ChoiceRow
          walk="text-size"
          label={t('Text size')}
          choices={[
            { id: 'regular', label: t('Regular') },
            { id: 'tv', label: t('TV') },
          ]}
          value={tv ? 'tv' : 'regular'}
          onChoose={(v) => {
            chooseTvSize(v === 'tv')
            setTv(v === 'tv')
          }}
          onFocus={say(t('Text size'), t('TV makes everything bigger, for the sofa. A TV’s own browser picks it by itself.'))}
        />
        <SelectRow
          label={t('Language')}
          ariaLabel={t('App language')}
          choices={LANGUAGES.map((l) => ({ id: l.code, label: l.name }))}
          value={currentLanguage()}
          onChoose={(v: Lang) => chooseLanguage(v)}
          onFocus={say(t('Language'), t('Menus and buttons. Audio and subtitle languages are in the player. Switching reloads the page.'))}
        />
        {mine.map((p) => (
          <LinkRow key={p.to} to={p.to} label={p.label} onFocus={say(p.label, p.blurb)} />
        ))}
      </div>
      <aside className="b-ys-help" aria-live="polite">
        <p className="b-ys-help-title">{help.title}</p>
        <p>{help.text}</p>
        <p className="b-ys-help-keys">{t('Left and Right change a setting. OK opens a page. Everything here is kept in this browser.')}</p>
      </aside>
    </div>
  )
}
