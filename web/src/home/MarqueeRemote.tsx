// Usherr: the Marquee remote (/marquee/remote), made for a phone, and the
// Screens tab of the Marquee's page. Every
// browser showing /marquee is a screen here: what it shows now, buttons to
// steer it (previous, pause, next, a scene, pin, dim, blank, reload), its
// name, and settings of its own or the shared ones.
import { useCallback, useEffect, useState } from 'react'
import { Link } from 'react-router-dom'
import Icon from '../components/Icon'
import { useConfirm } from '../components/ConfirmProvider'
import { useToast } from '../components/Toast'
import { useDocumentTitle } from '../documentTitle'
import { t } from '../i18n'
import { ownLabel, type MarqueeSettings, type MarqueeStyle, type SceneKind } from './marqueeData'
import MarqueeForm, { MQ_SCENE_LABEL } from './MarqueeForm'
import { MQ_STYLES } from './MarqueeLookForm'
import './cinema.css'

interface Showing {
  scene: string
  title?: string
  posterUrl?: string
  paused: boolean
  pinned: boolean
  dim: boolean
  blank: boolean
  info?: boolean
  quiet: boolean
}
interface Screen {
  id: string
  name: string
  online: boolean
  seenMs: number
  width: number
  height: number
  own: boolean
  ownKeys?: string[] // which settings it keeps of its own
  settings: MarqueeSettings
  showing?: Showing
}
interface Answer {
  screens: Screen[]
  profiles: string[]
}

const POLL_MS = 3000

async function call<T>(method: string, path: string, body?: unknown): Promise<T> {
  const res = await fetch(path, {
    method,
    credentials: 'include',
    cache: 'no-store',
    headers: body === undefined ? undefined : { 'Content-Type': 'application/json' },
    body: body === undefined ? undefined : JSON.stringify(body),
  })
  const text = await res.text()
  let data: { error?: string } | null = null
  try {
    data = text ? (JSON.parse(text) as { error?: string }) : null
  } catch {
    data = null
  }
  if (!res.ok) throw new Error(data?.error ?? t('Something went wrong (error {status}).', { status: res.status }))
  return data as T
}

function seenAgo(ms: number): string {
  if (!ms) return t('never')
  const min = Math.round((Date.now() - ms) / 60000)
  if (min < 1) return t('just now')
  if (min < 60) return t('{n} min ago', { n: min })
  const h = Math.round(min / 60)
  if (h < 48) return t('{n} h ago', { n: h })
  return new Date(ms).toLocaleDateString()
}

// The phone remote's own page (/marquee/remote): the screens, nothing else.
export default function MarqueeRemote() {
  useDocumentTitle(t('Marquee remote'))
  return (
    <div className="page-pad mqr">
      <div className="page-header">
        <h1>{t('Marquee remote')}</h1>
      </div>
      <MarqueeScreens />
      <p className="hint mqr-foot">
        <Link to="/marquee/manage">{t('The Marquee’s page: its look, your slides and pre-show, Home Assistant')}</Link>
      </p>
    </div>
  )
}

// MarqueeScreens lists every screen with its remote, kept fresh.
export function MarqueeScreens() {
  const toast = useToast()
  const [answer, setAnswer] = useState<Answer | null>(null)
  const [failed, setFailed] = useState('')

  const load = useCallback(() => {
    call<Answer>('GET', '/api/home/marquee/screens')
      .then((d) => {
        setAnswer({ screens: d.screens ?? [], profiles: d.profiles ?? [] })
        setFailed('')
      })
      .catch((e: unknown) => setFailed(e instanceof Error ? e.message : String(e)))
  }, [])

  // Kept fresh while the page is in view, for the preview.
  useEffect(() => {
    load()
    const id = window.setInterval(() => {
      if (document.visibilityState === 'visible') load()
    }, POLL_MS)
    return () => window.clearInterval(id)
  }, [load])

  const press = async (sc: Screen, cmd: string, arg?: string) => {
    try {
      await call('POST', `/api/home/marquee/screens/${sc.id}/command`, { cmd, arg: arg ?? '' })
      window.setTimeout(load, 600)
    } catch (e) {
      toast.error(e instanceof Error ? e.message : String(e))
    }
  }
  const saved = (d: Answer) => setAnswer({ screens: d.screens ?? [], profiles: d.profiles ?? [] })
  // Up or down the list: seen at once, then kept on the server.
  const move = async (i: number, by: -1 | 1) => {
    if (!answer) return
    const list = [...answer.screens]
    const j = i + by
    if (j < 0 || j >= list.length) return
    ;[list[i], list[j]] = [list[j], list[i]]
    setAnswer({ ...answer, screens: list })
    try {
      saved(await call<Answer>('PUT', '/api/home/marquee/screens/order', { order: list.map((sc) => sc.id) }))
    } catch (e) {
      toast.error(e instanceof Error ? e.message : String(e))
      load()
    }
  }

  const screens = answer?.screens ?? []
  return (
    <div className="mqr">
      {failed && !answer && <p className="hint">{failed}</p>}
      {answer && screens.length === 0 && (
        <section className="card mqr-empty">
          <Icon name="monitor" size={28} />
          <h2>{t('No screens yet')}</h2>
          <p>{t('Open {address} on the tablet or the screen by the TV. It shows up here by itself, and you can steer it from this page.', { address: `${window.location.origin}/marquee` })}</p>
        </section>
      )}
      <div className="mqr-list">
        {screens.map((sc, i) => (
          <ScreenCard
            key={sc.id}
            sc={sc}
            profiles={answer?.profiles ?? []}
            press={(cmd, arg) => void press(sc, cmd, arg)}
            saved={saved}
            move={screens.length > 1 ? { up: i > 0 ? () => void move(i, -1) : undefined, down: i < screens.length - 1 ? () => void move(i, 1) : undefined } : undefined}
          />
        ))}
      </div>
    </div>
  )
}

function ScreenCard({
  sc,
  profiles,
  press,
  saved,
  move,
}: {
  sc: Screen
  profiles: string[]
  press: (cmd: string, arg?: string) => void
  saved: (d: Answer) => void
  move?: { up?: () => void; down?: () => void }
}) {
  const toast = useToast()
  const confirm = useConfirm()
  const [naming, setNaming] = useState<string | null>(null)
  const [own, setOwn] = useState<MarqueeSettings | null>(null) // being edited
  const [busy, setBusy] = useState(false)
  const sh = sc.showing
  const live = sc.online && !!sh

  const put = async (body: { name?: string; settings?: MarqueeSettings | null }, done?: string) => {
    setBusy(true)
    try {
      saved(await call<Answer>('PUT', `/api/home/marquee/screens/${sc.id}`, body))
      if (done) toast.success(done)
      return true
    } catch (e) {
      toast.error(e instanceof Error ? e.message : String(e))
      return false
    } finally {
      setBusy(false)
    }
  }
  const forget = async () => {
    const ok = await confirm({
      title: t('Forget {name}?', { name: sc.name }),
      body: t('It leaves this list with its own settings. If it is still showing the Marquee, it comes back as a new screen.'),
      confirmLabel: t('Forget'),
      danger: true,
    })
    if (!ok) return
    try {
      saved(await call<Answer>('DELETE', `/api/home/marquee/screens/${sc.id}`))
    } catch (e) {
      toast.error(e instanceof Error ? e.message : String(e))
    }
  }
  const sceneLabel = sh?.scene ? (MQ_SCENE_LABEL[sh.scene as SceneKind] ?? sh.scene) : ''
  const state = !sh ? '' : sh.blank ? t('Blank') : sh.quiet ? t('Resting (quiet hours)') : sh.paused ? t('Paused') : sh.pinned ? t('Pinned') : sh.dim ? t('Dimmed') : t('Playing')

  return (
    <section className={`card mqr-screen${sc.online ? '' : ' off'}`}>
      <header className="mqr-head">
        <span className={`mqr-dot${sc.online ? ' on' : ''}`} aria-hidden="true" />
        {naming === null ? (
          <>
            <h2>{sc.name}</h2>
            <button className="icon-btn" onClick={() => setNaming(sc.name)} aria-label={t('Rename {name}', { name: sc.name })}>
              <Icon name="edit" size={15} />
            </button>
          </>
        ) : (
          <form
            className="mqr-rename"
            onSubmit={(e) => {
              e.preventDefault()
              void put({ name: naming }).then((ok) => ok && setNaming(null))
            }}
          >
            <input value={naming} maxLength={40} autoFocus onChange={(e) => setNaming(e.target.value)} aria-label={t('Screen name')} />
            <button className="primary btn-sm" disabled={busy || !naming.trim()}>
              {t('Save')}
            </button>
            <button type="button" className="btn-sm" onClick={() => setNaming(null)}>
              {t('Cancel')}
            </button>
          </form>
        )}
        {move && naming === null && (
          <span className="mqr-move">
            <button className="icon-btn" onClick={move.up} disabled={!move.up} aria-label={t('Move {name} up', { name: sc.name })} title={t('Move up')}>
              <Icon name="chevron-up" size={15} />
            </button>
            <button className="icon-btn" onClick={move.down} disabled={!move.down} aria-label={t('Move {name} down', { name: sc.name })} title={t('Move down')}>
              <Icon name="chevron-down" size={15} />
            </button>
          </span>
        )}
        <small className="mqr-meta">
          {sc.online ? t('On') : t('Off · seen {when}', { when: seenAgo(sc.seenMs) })}
          {sc.width > 0 && sc.height > 0 && ` · ${sc.width}×${sc.height}`}
        </small>
      </header>

      <div className="mqr-preview">
        <div className={`mqr-poster${sh?.dim ? ' dim' : ''}${sh?.blank ? ' blank' : ''}`}>
          {live && sh?.posterUrl && !sh.blank ? <img src={sh.posterUrl} alt="" /> : <Icon name="monitor" size={30} />}
        </div>
        <div className="mqr-now">
          {live ? (
            <>
              <span className="mqr-state">{state}</span>
              <strong>{sh?.title || sceneLabel || t('Nothing to show yet.')}</strong>
              {sceneLabel && sh?.title && <small>{sceneLabel}</small>}
            </>
          ) : (
            <span className="hint">{sc.online ? t('Waiting to hear what it shows…') : t('This screen is off. Open the Marquee on it to steer it.')}</span>
          )}
        </div>
      </div>

      <div className="mqr-pad" role="group" aria-label={t('Steer {name}', { name: sc.name })}>
        <button className="mqr-btn" disabled={!sc.online} onClick={() => press('prev')} aria-label={t('Previous')}>
          <Icon name="skip-prev" size={22} />
        </button>
        <button className="mqr-btn mqr-play" disabled={!sc.online} onClick={() => press('pause', sh?.paused ? 'off' : 'on')} aria-label={sh?.paused ? t('Play') : t('Pause')}>
          <Icon name={sh?.paused ? 'play' : 'pause'} size={26} />
        </button>
        <button className="mqr-btn" disabled={!sc.online} onClick={() => press('next')} aria-label={t('Next')}>
          <Icon name="skip-next" size={22} />
        </button>
      </div>

      <div className="mqr-toggles">
        <button className={`mqr-chip${sh?.pinned ? ' on' : ''}`} disabled={!sc.online} aria-pressed={!!sh?.pinned} onClick={() => press('pin', sh?.pinned ? 'off' : 'on')}>
          <Icon name="repeat" size={15} /> {t('Pin')}
        </button>
        <button className={`mqr-chip${sh?.dim ? ' on' : ''}`} disabled={!sc.online} aria-pressed={!!sh?.dim} onClick={() => press('dim', sh?.dim ? 'off' : 'on')}>
          <Icon name="moon" size={15} /> {t('Dim')}
        </button>
        <button className={`mqr-chip${sh?.blank ? ' on' : ''}`} disabled={!sc.online} aria-pressed={!!sh?.blank} onClick={() => press('blank', sh?.blank ? 'off' : 'on')}>
          <Icon name="eye-off" size={15} /> {t('Blank')}
        </button>
        <button className={`mqr-chip${sh?.info ? ' on' : ''}`} disabled={!sc.online} aria-pressed={!!sh?.info} onClick={() => press('info', sh?.info ? 'off' : 'on')}>
          <Icon name="info" size={15} /> {t('Info')}
        </button>
        <button className="mqr-chip" disabled={!sc.online} onClick={() => press('preshow')}>
          <Icon name="film" size={15} /> {t('Pre-show')}
        </button>
        <button className="mqr-chip" disabled={!sc.online} onClick={() => press('reload')}>
          <Icon name="refresh" size={15} /> {t('Reload')}
        </button>
      </div>

      <label className="mqr-scene">
        <span>{t('Look')}</span>
        <select
          value={sc.settings.style}
          disabled={busy}
          onChange={(e) => void put({ settings: { ...sc.settings, style: e.target.value as MarqueeStyle } }, t('{name} changes its look now.', { name: sc.name }))}
        >
          {MQ_STYLES.map((x) => (
            <option key={x.key} value={x.key}>
              {x.label}
            </option>
          ))}
        </select>
        <small className="hint">{t('A look picked here is this screen’s own; everything else follows the shared settings.')}</small>
      </label>

      <label className="mqr-scene">
        <span>{t('Show now')}</span>
        <select
          value=""
          disabled={!sc.online}
          onChange={(e) => {
            if (e.target.value) press('scene', e.target.value)
          }}
        >
          <option value="">{t('Pick a scene…')}</option>
          {sc.settings.scenes.map((s) => (
            <option key={s.kind} value={s.kind}>
              {MQ_SCENE_LABEL[s.kind]}
            </option>
          ))}
        </select>
      </label>

      <details className="mqr-settings" onToggle={(e) => !(e.target as HTMLDetailsElement).open && setOwn(null)}>
        <summary>{sc.own ? t('Its own: {what}; the rest is shared', { what: ownLabel(sc.ownKeys) }) : t('Settings: the shared ones')}</summary>
        {!sc.own && !own && (
          <div className="mqr-row">
            <p className="hint">{t('This screen shows the shared settings. Change any of them for this screen only; what you change is its own, and the rest keeps following the shared settings.')}</p>
            <button className="btn-sm" onClick={() => setOwn(sc.settings)}>
              {t('Give it its own settings')}
            </button>
          </div>
        )}
        {sc.own && !own && (
          <div className="mqr-row">
            <button className="btn-sm" onClick={() => setOwn(sc.settings)}>
              {t('Change its settings')}
            </button>
            <button className="btn-sm" disabled={busy} onClick={() => void put({ settings: null }, t('{name} shows the shared settings again.', { name: sc.name }))}>
              {t('Use the shared settings for all of it')}
            </button>
          </div>
        )}
        {own && (
          <div className="mq-set">
            <MarqueeForm m={own} profiles={profiles} change={(patch) => setOwn({ ...own, ...patch })} name={`mq-${sc.id}`} />
            <div className="mqr-row">
              <button className="primary" disabled={busy} onClick={() => void put({ settings: own }, t('Saved. {name} shows them now.', { name: sc.name })).then((ok) => ok && setOwn(null))}>
                {t('Save')}
              </button>
              <button className="btn-sm" onClick={() => setOwn(null)}>
                {t('Cancel')}
              </button>
            </div>
          </div>
        )}
        <div className="mqr-row">
          <button className="btn-sm danger" onClick={() => void forget()}>
            <Icon name="trash" size={14} /> {t('Forget this screen')}
          </button>
        </div>
      </details>
    </section>
  )
}
