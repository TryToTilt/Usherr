// Usherr: getting games on Usherr's own sports engine (home_sports_get.go).
// What a league gets (Simple and Advanced), a game's Search now and Pick a
// release, and the wanted games.
import { useCallback, useEffect, useRef, useState, type FormEvent } from 'react'
import { createPortal } from 'react-dom'
import { Link } from 'react-router-dom'
import { api, type QualityProfile } from '../api'
import Icon from '../components/Icon'
import { useToast } from '../components/Toast'
import { formatBytes } from '../format'
import { t } from '../i18n'
import { useFocusTrap } from '../useFocusTrap'
import { partLabel, sportsApi, type SportGame, type SportKind, type SportLeague, type SportRelease, type SportWanted } from './sportsApi'

// useProfiles is the quality profiles to choose from (none when they can't
// be read: the default is then used).
export function useProfiles(): { profiles: QualityProfile[]; tvDefault: string } {
  const [list, setList] = useState<{ profiles: QualityProfile[]; tvDefault: string }>({ profiles: [], tvDefault: '' })
  useEffect(() => {
    api
      .listProfiles()
      .then((r) => {
        const def = r.profiles.find((p) => p.id === (r.defaultTvId || r.defaultId))
        setList({ profiles: r.profiles, tvDefault: def?.name ?? '' })
      })
      .catch(() => undefined)
  }, [])
  return list
}

export function ProfileSelect({ value, onChange, defaultLabel }: { value: number; onChange: (v: number) => void; defaultLabel: string }) {
  const { profiles } = useProfiles()
  return (
    <select value={value} onChange={(e) => onChange(Number(e.target.value))} aria-label={t('Quality profile')}>
      <option value={0}>{defaultLabel}</option>
      {profiles.map((p) => (
        <option key={p.id} value={p.id}>
          {p.name}
        </option>
      ))}
    </select>
  )
}

// The events a league can get, by its kind.
const CHOICES: Partial<Record<SportKind, { key: string; label: string }[]>> = {
  fight: [
    { key: 'early', label: t('Early prelims') },
    { key: 'prelims', label: t('Prelims') },
    { key: 'main', label: t('Main card') },
  ],
  motor: [
    { key: 'race', label: t('Races') },
    { key: 'sprint', label: t('Sprints') },
    { key: 'qualifying', label: t('Qualifying') },
    { key: 'practice', label: t('Practice') },
  ],
}

// Simple mode's few common choices, by kind: a name and the parts it means.
const PRESETS: Partial<Record<SportKind, { label: string; parts: string[] }[]>> = {
  fight: [
    { label: t('The whole card'), parts: [] },
    { label: t('Main card and prelims'), parts: ['main', 'prelims'] },
    { label: t('Main card only'), parts: ['main'] },
  ],
  motor: [
    { label: t('Races only'), parts: ['race'] },
    { label: t('Races and sprints'), parts: ['race', 'sprint'] },
    { label: t('Races, sprints and qualifying'), parts: ['race', 'sprint', 'qualifying'] },
    { label: t('Everything, practice too'), parts: [] },
  ],
}

const same = (a: string[], b: string[]) => a.length === b.length && a.every((x) => b.includes(x))

// LeagueGetPanel says what a league gets: its new games or not, which
// events, only the followed teams' games, and in which quality.
export function LeagueGetPanel({ league, onSaved }: { league: SportLeague; onSaved: () => void }) {
  const toast = useToast()
  const { tvDefault } = useProfiles()
  const kind: SportKind = league.kind ?? 'other'
  const [monitored, setMonitored] = useState(league.monitored)
  const [parts, setParts] = useState<string[]>(league.parts ?? [])
  const [teamsOnly, setTeamsOnly] = useState(!!league.teamsOnly)
  const [profileId, setProfileId] = useState(league.profileId ?? 0)
  const presets = PRESETS[kind]
  const fitsSimple = !presets || presets.some((p) => same(p.parts, parts))
  const [advanced, setAdvanced] = useState(!fitsSimple || (league.profileId ?? 0) !== 0)
  const [busy, setBusy] = useState(false)
  const changed =
    monitored !== league.monitored || !same(parts, league.parts ?? []) || teamsOnly !== !!league.teamsOnly || profileId !== (league.profileId ?? 0)
  const save = () => {
    setBusy(true)
    sportsApi
      .leagueGet(league.id!, { monitored, parts, teamsOnly, profileId })
      .then(() => (toast.success(monitored ? t('{name} gets its new games.', { name: league.name }) : t('Saved.')), onSaved()))
      .catch((e: Error) => toast.error(e.message))
      .finally(() => setBusy(false))
  }
  const choices = CHOICES[kind]
  return (
    <section className="card sp-get" data-walk="sp-get">
      <div className="sp-get-head">
        <h2>{t('Downloads')}</h2>
        <div className="seg" role="group" aria-label={t('Settings mode')}>
          <button type="button" className={!advanced ? 'active' : ''} aria-pressed={!advanced} disabled={!fitsSimple} onClick={() => setAdvanced(false)}>
            {t('Simple')}
          </button>
          <button type="button" className={advanced ? 'active' : ''} aria-pressed={advanced} onClick={() => setAdvanced(true)}>
            {t('Advanced')}
          </button>
        </div>
      </div>
      <label className="sp-check">
        <input type="checkbox" checked={monitored} onChange={(e) => setMonitored(e.target.checked)} />
        {t('Get its new games')}
      </label>
      {monitored && !advanced && presets && (
        <label className="sp-field">
          {kind === 'fight' ? t('Which part of each card') : t('Which sessions of each weekend')}
          <select value={presets.findIndex((p) => same(p.parts, parts))} onChange={(e) => setParts(presets[Number(e.target.value)].parts)}>
            {presets.map((p, i) => (
              <option key={p.label} value={i}>
                {p.label}
              </option>
            ))}
          </select>
        </label>
      )}
      {monitored && !advanced && kind === 'team' && (
        <label className="sp-field">
          {t('Which games')}
          <select value={teamsOnly ? 'teams' : 'all'} onChange={(e) => setTeamsOnly(e.target.value === 'teams')}>
            <option value="all">{t('Every game')}</option>
            <option value="teams">{t('Only the games of the teams I follow')}</option>
          </select>
        </label>
      )}
      {monitored && advanced && (
        <>
          {choices && (
            <fieldset className="sp-parts">
              <legend>{kind === 'fight' ? t('Parts of each card') : t('Sessions of each weekend')}</legend>
              {choices.map((c) => (
                <label key={c.key} className="sp-check">
                  <input type="checkbox" checked={parts.length === 0 || parts.includes(c.key)} onChange={(e) => {
                    const all = choices.map((x) => x.key)
                    const now = parts.length === 0 ? all : parts
                    const next = e.target.checked ? [...now, c.key] : now.filter((x) => x !== c.key)
                    setParts(next.length === all.length ? [] : next)
                  }} />
                  {c.label}
                </label>
              ))}
            </fieldset>
          )}
          {kind !== 'fight' && kind !== 'motor' && (
            <label className="sp-check">
              <input type="checkbox" checked={teamsOnly} onChange={(e) => setTeamsOnly(e.target.checked)} />
              {t('Only the games of the teams I follow')}
            </label>
          )}
          <label className="sp-field">
            {t('Quality profile')}
            <ProfileSelect value={profileId} onChange={setProfileId} defaultLabel={tvDefault ? t('Sports’ default ({name})', { name: tvDefault }) : t('Sports’ default')} />
          </label>
        </>
      )}
      <p className="sp-dim sp-small">
        {monitored
          ? t('Usherr looks for each new game a few hours after it ends, on your indexers, and keeps looking for two weeks. Played games can be got one by one with Get.')
          : t('Nothing is downloaded unless you ask: Get on a game, or switch this on.')}
      </p>
      <div className="sp-btns" style={{ justifyContent: 'flex-start' }}>
        <button className="primary" disabled={!changed || busy} onClick={save}>
          {t('Save')}
        </button>
      </div>
    </section>
  )
}

// ReleasePicker lists every release the indexers have for a game, the
// game's first, with why the others aren't taken; one is downloaded by hand.
export function ReleasePicker({ game, onClose, onGrabbed }: { game: SportGame; onClose: () => void; onGrabbed: () => void }) {
  const toast = useToast()
  const box = useRef<HTMLDivElement>(null)
  useFocusTrap(box)
  const parts = game.parts ?? []
  const [part, setPart] = useState(parts.length ? parts.find((p) => !(game.have ?? []).includes(p)) ?? parts[parts.length - 1] : '')
  const [q, setQ] = useState('')
  const [typed, setTyped] = useState('')
  const [all, setAll] = useState(false)
  const [res, setRes] = useState<{ releases: SportRelease[]; searched: string[] } | null>(null)
  const [error, setError] = useState('')
  const [busy, setBusy] = useState('')
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => e.key === 'Escape' && onClose()
    window.addEventListener('keydown', onKey)
    return () => window.removeEventListener('keydown', onKey)
  }, [onClose])
  const load = useCallback(() => {
    setRes(null)
    setError('')
    sportsApi
      .releases(game.id, part, q)
      .then(setRes)
      .catch((e: Error) => setError(e.message))
  }, [game.id, part, q])
  useEffect(load, [load])
  const shown = (res?.releases ?? []).filter((r) => all || r.matches)
  const hidden = (res?.releases.length ?? 0) - shown.length
  const grab = (r: SportRelease) => {
    setBusy(r.downloadUrl)
    sportsApi
      .grab(game.id, r, part)
      .then(() => (toast.success(t('Downloading {title}.', { title: game.title })), onGrabbed(), onClose()))
      .catch((e: Error) => toast.error(e.message))
      .finally(() => setBusy(''))
  }
  const submit = (e: FormEvent) => {
    e.preventDefault()
    setQ(typed.trim())
  }
  return createPortal(
    <div className="modal-backdrop" onMouseDown={(e) => e.target === e.currentTarget && onClose()}>
      <div ref={box} tabIndex={-1} className="modal sp-picker" role="dialog" aria-modal="true" aria-label={t('Pick a release')}>
        <div className="sp-get-head">
          <h2>{t('Pick a release')}</h2>
          <button className="icon-btn" onClick={onClose} aria-label={t('Close')}>
            <Icon name="x" />
          </button>
        </div>
        <p className="sp-dim">{game.title}</p>
        <div className="sp-filters">
          {parts.length > 1 && (
            <select value={part} onChange={(e) => setPart(e.target.value)} aria-label={t('Part')}>
              {parts.map((p) => (
                <option key={p} value={p}>
                  {partLabel(p)}
                  {(game.have ?? []).includes(p) ? ' ✓' : ''}
                </option>
              ))}
            </select>
          )}
          <form className="sp-search" onSubmit={submit} role="search">
            <input value={typed} onChange={(e) => setTyped(e.target.value)} placeholder={res?.searched.join(' · ') ?? t('Search your own words…')} aria-label={t('Search your own words')} />
            <button type="submit" className="btn-sm">
              <Icon name="search" />
            </button>
          </form>
          <label className="sp-check">
            <input type="checkbox" checked={all} onChange={(e) => setAll(e.target.checked)} /> {t('Show the ones that aren’t this game')}
          </label>
        </div>
        {error && <p className="error-text">{error}</p>}
        {!res && !error && <p className="sp-dim">{t('Searching your indexers…')}</p>}
        {res && shown.length === 0 && <p className="sp-dim">{hidden ? t('Nothing that is this game. {n} other releases are hidden.', { n: hidden }) : t('Nothing was found.')}</p>}
        <ul className="sp-list sp-releases">
          {shown.map((r) => (
            <li key={r.downloadUrl + r.title} className={r.matches && r.accepted ? '' : 'sp-off-match'}>
              <div className="sp-game-main">
                <strong title={r.title}>{r.title}</strong>
                <span>{[r.quality, r.sizeBytes ? formatBytes(r.sizeBytes) : '', r.indexerName, r.protocol === 'torrent' ? t('Torrent') : t('Usenet')].filter(Boolean).join(' · ')}</span>
                {(r.rejections ?? []).map((why) => (
                  <span key={why} className="sp-why">
                    {why}
                  </span>
                ))}
              </div>
              {r.matches && r.accepted && !r.blocklisted && <span className="badge downloaded">{t('This game')}</span>}
              <button className={r.matches && r.accepted ? 'btn-sm primary btn-with-icon' : 'btn-sm btn-with-icon'} disabled={busy !== ''} onClick={() => grab(r)}>
                <Icon name="download" /> {t('Download')}
              </button>
            </li>
          ))}
        </ul>
      </div>
    </div>,
    document.body,
  )
}

// GameGetButtons are a played game's Search now and Pick a release (Usherr's
// own engine), and Don't get for a wanted game.
export function GameGetButtons({ g, onChange }: { g: SportGame; onChange: () => void }) {
  const toast = useToast()
  const [busy, setBusy] = useState(false)
  const [picking, setPicking] = useState(false)
  const played = new Date(g.when).getTime() < Date.now()
  const complete = g.hasFile && (!g.parts || g.parts.length <= (g.have?.length ?? 0))
  return (
    <>
      {played && !complete && !g.downloading && (
        <button
          className="btn-sm btn-with-icon"
          disabled={busy}
          title={t('Look for it on your indexers now and download the best release')}
          onClick={() => {
            setBusy(true)
            sportsApi
              .searchNow(g.id)
              .then((r) => (r.grabbed ? toast.success(r.message) : toast.info(r.message), onChange()))
              .catch((e: Error) => toast.error(e.message))
              .finally(() => setBusy(false))
          }}
        >
          <Icon name="search" /> {busy ? t('Searching…') : t('Search now')}
        </button>
      )}
      {played && (
        <button className="btn-sm btn-with-icon" title={t('See every release and choose one')} onClick={() => setPicking(true)}>
          <Icon name="list" /> {t('Pick')}
        </button>
      )}
      {g.monitored && !complete && (
        <button
          className="icon-btn"
          title={t('Don’t get this game')}
          aria-label={t('Don’t get {title}', { title: g.title })}
          onClick={() =>
            sportsApi
              .setWanted(g.id, false)
              .then(onChange)
              .catch((e: Error) => toast.error(e.message))
          }
        >
          <Icon name="x" />
        </button>
      )}
      {picking && <ReleasePicker game={g} onClose={() => setPicking(false)} onGrabbed={onChange} />}
    </>
  )
}

// WantedGames lists the played games that are wanted and lack a file.
export function WantedGames({ refresh }: { refresh: number }) {
  const [d, setD] = useState<{ on: boolean; canGet: boolean; games: SportWanted[] } | null>(null)
  const [n, setN] = useState(0)
  useEffect(() => {
    sportsApi
      .wanted()
      .then(setD)
      .catch(() => setD(null))
  }, [refresh, n])
  if (!d || !d.on || d.games.length === 0) return null
  return (
    <section data-walk="sp-wanted">
      <h2>
        {t('Wanted')} <span className="sp-dim">({d.games.length})</span>
      </h2>
      <ul className="sp-games">
        {d.games.slice(0, 50).map((g) => (
          <li key={g.id} className="sp-game">
            <div className="sp-game-main">
              <strong>{g.title}</strong>
              <span>
                {[
                  new Date(g.when).toLocaleDateString(undefined, { month: 'short', day: 'numeric' }),
                  g.league,
                  g.missing.some((p) => p) ? g.missing.map(partLabel).join(', ') : '',
                  g.searching ? t('Usherr keeps looking') : t('No longer looked for: search by hand'),
                ]
                  .filter(Boolean)
                  .join(' · ')}
              </span>
            </div>
            {g.leagueId && (
              <Link className="btn-sm" to={`/sports/league/${g.leagueId}`}>
                {t('League')}
              </Link>
            )}
            {d.canGet && <GameGetButtons g={g} onChange={() => setN((x) => x + 1)} />}
          </li>
        ))}
      </ul>
    </section>
  )
}
