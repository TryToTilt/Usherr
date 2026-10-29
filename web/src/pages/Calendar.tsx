import { useEffect, useState } from 'react'
import { createPortal } from 'react-dom'
import { api, isAdmin, isViewer, type CalendarEntry } from '../api'
import { useComicsOn } from '../home/libraryKinds' // home fork
import { useAuth } from '../AuthContext'
import CalendarGrid from '../components/CalendarGrid'
import Icon from '../components/Icon'
import KindGlyph, { type GlyphKind } from '../components/KindGlyph'
import Loading from '../components/Loading'
import { useToast } from '../components/Toast'
import { copyText } from '../components/CopyBox'
import { useKinds, useModules } from '../ModulesContext'
import { t, tx } from '../i18n'
import { useUrlState } from '../useUrlState'
import { COMMON_ZONES, deviceZone, readZonePref, resolveZone, writeZonePref } from '../airTime'
import { useLive } from '../useLive'
import { CalendarMatches, entryKey, textMatch } from '../home/CalendarSearch' // Usherr
import { setCalendarSearch, useCalendarSearch } from '../home/calendarSearchStore' // Usherr: typed in the top bar
import ActionMenu from '../components/ActionMenu' // Usherr

// home fork: the Library's kinds, in its order (anime apart from TV, ebooks
// apart from audiobooks, and comics).
type Show = 'all' | 'movie' | 'theaters' | 'episode' | 'anime' | 'ebook' | 'audiobook' | 'album' | 'comic'
const SHOW_LABEL: Record<Show, string> = { all: t('All'), movie: t('Movies'), theaters: t('In theaters'), episode: t('TV'), anime: t('Anime'), ebook: t('Ebooks'), audiobook: t('Audiobooks'), album: t('Music'), comic: t('Comics') }

// Monitored movie releases, episode airs and album releases on one month
// view, with simple filters and a link to add it to a calendar app.
// Each filter wears its kind's glyph and colour, as on Discover and Library.
const SHOW_GLYPH: Record<Exclude<Show, 'all'>, GlyphKind> = { movie: 'movie', theaters: 'movie', episode: 'tv', anime: 'tv', ebook: 'book', audiobook: 'audiobook', album: 'music', comic: 'comic' }
const SHOW_COLOR: Record<Exclude<Show, 'all'>, string> = { movie: 'var(--c-movie)', theaters: 'var(--c-movie)', episode: 'var(--c-tv)', anime: 'var(--c-tv)', ebook: 'var(--c-book)', audiobook: 'var(--c-audiobook)', album: 'var(--c-music)', comic: 'var(--c-comics)' }

// matches says whether an entry belongs under a kind.
function matches(e: CalendarEntry, show: Show, anime: ReadonlySet<number>): boolean {
  switch (show) {
    case 'all':
      return true
    case 'movie': // Usherr: your movies; films showing in cinemas have their own filter
      return e.kind === 'movie' && e.status !== 'theaters'
    case 'theaters':
      return e.status === 'theaters'
    case 'episode':
      return e.kind === 'episode' && !e.anime && !anime.has(e.seriesId ?? 0)
    case 'anime':
      return e.kind === 'episode' && (!!e.anime || anime.has(e.seriesId ?? 0))
    case 'ebook':
      return e.kind === 'book' && (e.formats ?? ['ebook']).includes('ebook')
    case 'audiobook':
      return e.kind === 'book' && (e.formats ?? []).includes('audiobook')
    default:
      return e.kind === show
  }
}

export default function Calendar() {
  const [entries, setEntries] = useState<CalendarEntry[] | null>(null)
  const [error, setError] = useState('')
  // The kind and the "not downloaded" tick live in the address, so Back returns to them.
  const [ui, patchUi] = useUrlState({ show: 'all', missing: '', month: '' })
  const show: Show = (['movie', 'theaters', 'episode', 'anime', 'ebook', 'audiobook', 'album', 'comic'] as const).find((k) => k === ui.show) ?? (ui.show === 'book' ? 'ebook' : 'all')
  const setShow = (k: Show) => patchUi({ show: k })
  const missingOnly = ui.missing === '1'
  const setMissingOnly = (v: boolean) => patchUi({ missing: v ? '1' : '' })
  const [feedOpen, setFeedOpen] = useState(false)
  const { q: searchQ, pick } = useCalendarSearch() // Usherr: what the top bar's search found
  useEffect(() => () => setCalendarSearch({ q: '', pick: null }), []) // a new visit starts with the whole calendar
  // Usherr: the search bar sits at the top of Upcoming and the kind picker at the end of its tabs.
  const [slots, setSlots] = useState<{ top: HTMLElement | null; tabs: HTMLElement | null }>({ top: null, tabs: null })
  useEffect(() => setSlots({ top: document.getElementById('upcoming-top'), tabs: document.getElementById('upcoming-tabs-end') }), [])
  // Which time zone the days and times are read in: this device's, or one picked here (kept in this browser only).
  const [zonePref, setZonePref] = useState(readZonePref)
  const zone = resolveZone(zonePref)
  const pickZone = (z: string) => {
    setZonePref(z)
    writeZonePref(z)
  }
  // The private calendar address belongs to an account that can change things.
  const viewer = isViewer(useAuth().user)
  const kinds = useKinds()
  const { on } = useModules()
  const [anime, setAnime] = useState<ReadonlySet<number>>(new Set())
  useEffect(() => {
    fetch('/api/home/anime-series', { credentials: 'include' })
      .then((r) => (r.ok ? r.json() : []))
      .then((ids: number[]) => setAnime(new Set(ids)))
      .catch(() => undefined)
  }, [])

  useEffect(() => {
    api
      .calendar()
      .then(setEntries)
      .catch((e) => setError(e instanceof Error ? e.message : String(e)))
  }, [])
  useLive(() => api.calendar().then(setEntries).catch(() => undefined), 30000)

  const hasComics = useComicsOn(isAdmin(useAuth().user)) // home fork
  const options: Show[] = [
    'all',
    ...(kinds.includes('movie') ? (['movie'] as const) : []),
    ...((entries ?? []).some((e) => e.status === 'theaters') ? (['theaters'] as const) : []), // Usherr
    ...(kinds.includes('tv') ? (['episode', 'anime'] as const) : []),
    ...(on('ebooks') ? (['ebook'] as const) : []),
    ...(on('audiobooks') ? (['audiobook'] as const) : []),
    ...(kinds.includes('music') ? (['album'] as const) : []),
    ...(hasComics ? (['comic'] as const) : []),
  ]
  const shown = (entries ?? []).filter((e) => matches(e, show, anime) && (!missingOnly || e.status !== 'downloaded')).map((e) => (e.kind === 'episode' && anime.has(e.seriesId ?? 0) ? { ...e, anime: true } : e)) // Usherr: anime labelled as anime
  // Usherr: the search narrows the calendar to what the AI picked, or to the titles typed.
  const searching = pick !== null || searchQ.trim() !== ''
  const found = pick ? shown.filter((e) => pick.keys.has(entryKey(e))) : searchQ.trim() ? shown.filter((e) => textMatch(e, searchQ)) : shown

  return (
    <div>
      {feedOpen && !viewer && <FeedPanel />}
      {error && <p className="error-text">{error}</p>}
      {entries === null && !error && <Loading height={420} />}
      {entries !== null && (
        <>
          {slots.tabs &&
            createPortal(
              <>
              {options.length > 2 && (
              <label className="cal-kind-pick" style={show === 'all' ? undefined : { ['--kc' as string]: SHOW_COLOR[show] }}>
                {show === 'all' ? <Icon name="grid" size={15} /> : <KindGlyph kind={SHOW_GLYPH[show]} size={16} />}
                <select value={show} onChange={(e) => setShow(e.target.value as Show)} aria-label={t('Show')}>
                  {options.map((k) => (
                    <option key={k} value={k}>
                      {SHOW_LABEL[k]}
                    </option>
                  ))}
                </select>
              </label>
              )}
              {/* Usherr: the calendar's extras in one menu */}
              <ActionMenu
                label={t('More')}
                icon="more"
                choices={[
                  { id: 'missing', label: missingOnly ? t('Show downloaded too') : t('Only what isn\u2019t downloaded yet') },
                  ...(!viewer ? [{ id: 'feed', label: t('Add to your calendar app') }] : []),
                ]}
                onPick={(id) => (id === 'missing' ? setMissingOnly(!missingOnly) : setFeedOpen((v) => !v))}
              />
              </>,
              slots.tabs,
            )}
          {missingOnly && (
            <div className="cbar-chips">
              <button type="button" className="cbar-chip" onClick={() => setMissingOnly(false)}>
                {t('Not downloaded yet')} <Icon name="x" size={12} />
              </button>
            </div>
          )}
          {pick?.answer && (
            <p className="cal-search-answer">
              <Icon name="sparkles" size={14} /> {pick.answer}
            </p>
          )}
          {searching && <CalendarMatches entries={found} onMonth={(month) => patchUi({ month })} />}
          {entries.length === 0 && <p style={{ color: 'var(--text-dim)' }}>{t('Nothing is scheduled yet. Monitored movies and shows appear here once their release or air dates are known.')}</p>}
          <CalendarGrid entries={found} zone={zone} />
          <div className="cal-foot">
            {entries.some((e) => e.kind === 'episode' && e.airAt) && <ZonePicker pref={zonePref} onPick={pickZone} />}
            <p className="field-hint cal-credit">
              {tx('Air times from {link}', {
                link: (
                  <a href="https://www.tvmaze.com" target="_blank" rel="noopener noreferrer">
                    TVmaze
                  </a>
                ),
              })}
            </p>
          </div>
        </>
      )}
    </div>
  )
}

// Which time zone episode days and times are shown in. Episodes TVmaze has a time for move to the day they
// air in this zone; the choice stays in this browser.
function ZonePicker({ pref, onPick }: { pref: string; onPick: (zone: string) => void }) {
  const zones = pref && !COMMON_ZONES.includes(pref) ? [pref, ...COMMON_ZONES] : COMMON_ZONES
  return (
    <label className="inline-field cal-zone">
      {t('Show times in')}
      <select value={pref} onChange={(e) => onPick(e.target.value)}>
        <option value="">{t('This device ({zone})', { zone: deviceZone() })}</option>
        {zones.map((z) => (
          <option key={z} value={z}>
            {z === 'UTC' ? t('UTC') : z.replace(/_/g, ' ')}
          </option>
        ))}
      </select>
    </label>
  )
}

// The calendar feed: a private address that Google, Apple or Outlook
// calendars subscribe to.
function FeedPanel() {
  const toast = useToast()
  const [path, setPath] = useState<string | null>(null)
  const [busy, setBusy] = useState(false)
  useEffect(() => {
    api
      .calendarFeed()
      .then((f) => setPath(f.on ? (f.path ?? '') : ''))
      .catch(() => setPath(''))
  }, [])
  const url = path ? `${window.location.origin}${path}` : ''

  async function run(fn: () => Promise<{ on: boolean; path?: string }>, done: string) {
    setBusy(true)
    try {
      const f = await fn()
      setPath(f.on ? (f.path ?? '') : '')
      toast.success(done)
    } catch (e) {
      toast.error(e instanceof Error ? e.message : String(e))
    } finally {
      setBusy(false)
    }
  }

  if (path === null) return null
  return (
    <div className="card feed-panel">
      {url ? (
        <>
          <p style={{ marginTop: 0 }}>
            {tx('Add this address to your calendar app as a subscription (in Google Calendar: {google}; on an iPhone: {iphone}). It updates by itself.', {
              google: <em>{t('Other calendars → From URL')}</em>,
              iphone: <em>{t('Settings → Calendar → Accounts → Add Subscribed Calendar')}</em>,
            })}
          </p>
          <div className="feed-url">
            <code>{url}</code>
            <button className="btn-sm" onClick={() => void copyText(url).then((ok) => (ok ? toast.success(t('Address copied.')) : toast.error(t('Copy did not work. Select the address and copy it by hand.'))))}>
              {t('Copy')}
            </button>
          </div>
          <p className="field-hint">
            {t('Anyone with this address can see your calendar, so keep it to yourself. A new address stops the old one working.')}
          </p>
          <div className="row-actions">
            <button className="btn-sm" disabled={busy} onClick={() => void run(() => api.newCalendarFeed(), t('New address made. Update it in your calendar app.'))}>
              {t('Make a new address')}
            </button>
            <button className="btn-sm" disabled={busy} onClick={() => void run(() => api.removeCalendarFeed(), t('The calendar address is turned off.'))}>
              {t('Turn it off')}
            </button>
          </div>
          {!window.location.protocol.startsWith('https') && <p className="field-hint">{t('Calendar apps on the internet can only reach this address if Mediarium is reachable from outside, for example through your reverse proxy.')}</p>}
        </>
      ) : (
        <>
          <p style={{ marginTop: 0 }}>{t('Get a private address that calendar apps (Google, Apple, Outlook) can subscribe to, so releases and episodes show up in your own calendar.')}</p>
          <button className="primary" disabled={busy} onClick={() => void run(() => api.newCalendarFeed(), t('Your calendar address is ready.'))}>
            {t('Make my calendar address')}
          </button>
        </>
      )}
    </div>
  )
}
