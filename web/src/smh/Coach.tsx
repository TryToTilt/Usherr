// Usherr: Show Me How's coach card. It plays a walkthrough's steps on the
// real screens: it lights up the button a step is about (found by its
// data-walk label), waits for the person to use it, or, in "Watch me do it",
// presses it for them. It never covers the page: everything stays clickable.
import { useCallback, useEffect, useLayoutEffect, useRef, useState } from 'react'
import { createPortal } from 'react-dom'
import { useLocation, useNavigate } from 'react-router-dom'
import { isAdmin } from '../api'
import { useAuth } from '../AuthContext'
import Icon from '../components/Icon'
import { QRCode } from '../components/TwoStepCard'
import { t } from '../i18n'
import DecisionCard from './DecisionCard'
import { markSmhDone, setSmhRun, smhSecrets, startSmh, stopSmh, type SmhRun } from './engine'
import type { SmhChoice, SmhCtx, SmhStep, SmhText } from './types'
import { walkById } from './walks'
import './smh.css'

const say = (x: SmhText | undefined, c: SmhCtx) => (typeof x === 'function' ? x(c) : (x ?? ''))
const sel = (id: string) => `[data-walk="${id}"]`
const ACT_DELAY = 1100 // how long Usherr shows what it's about to press

// A page matches the path, or any page under it when the path ends in *.
const onPage = (path: string, want: string) => (want.endsWith('*') ? path.startsWith(want.slice(0, -1)) : path === want)

function visible(el: Element | null): el is HTMLElement {
  if (!el) return false
  const r = el.getBoundingClientRect()
  return r.width > 0 && r.height > 0
}

function find(id: string): HTMLElement | null {
  return [...document.querySelectorAll(sel(id))].find(visible) ?? null
}

// useBox follows something on the page: where it is now, or null.
function useBox(id: string | undefined): { el: HTMLElement | null; box: DOMRect | null } {
  const [found, setFound] = useState<{ el: HTMLElement | null; box: DOMRect | null }>({ el: null, box: null })
  useLayoutEffect(() => {
    if (!id) return
    let scrolled = false
    let opened = false
    let menuAt = 0
    const look = () => {
      // Something in a folded-away header (the pull-down bar of the Library,
      // Discover, Comics) is shown by opening the header first.
      const raw = document.querySelector(sel(id))
      const folded = raw?.closest('.cbar:not(.down)')
      if (folded && !opened) {
        opened = true
        folded.querySelector<HTMLElement>('.cbar-pull')?.click()
      }
      // A settings card folded to its title row is unfolded.
      const card = raw?.closest('[data-folded]')
      if (card) (card.firstElementChild as HTMLElement | null)?.click()
      // Something in the side menu (your stack) opens the menu, and keeps it
      // open while the step is about it (it slides away when left alone).
      const menu = raw?.closest('#main-menu')
      if (menu && !menu.classList.contains('open') && Date.now() - menuAt > 1500) {
        menuAt = Date.now()
        document.querySelector<HTMLElement>('.menu-button[aria-controls="main-menu"]:not(.menu-close)')?.click()
      }
      const el = find(id)
      const r = el ? el.getBoundingClientRect() : null
      if (el && !scrolled) {
        scrolled = true
        if (r && (r.top < 70 || r.bottom > window.innerHeight - 40)) el.scrollIntoView({ block: 'center', behavior: 'smooth' })
      }
      setFound((f) => {
        const b = f.box
        if (f.el === el && ((!b && !r) || (b && r && b.left === r.left && b.top === r.top && b.width === r.width && b.height === r.height))) return f
        return { el, box: r }
      })
    }
    look()
    const timer = window.setInterval(look, 300) // the page may still be loading
    window.addEventListener('resize', look)
    window.addEventListener('scroll', look, true)
    return () => {
      window.clearInterval(timer)
      window.removeEventListener('resize', look)
      window.removeEventListener('scroll', look, true)
    }
  }, [id])
  return id ? found : NOTHING
}

const NOTHING = { el: null, box: null }

export default function Coach({ run }: { run: SmhRun }) {
  const { user } = useAuth()
  const navigate = useNavigate()
  const location = useLocation()
  const walk = walkById(run.id)
  const steps = (walk && ((run.mode === 'advanced' && walk.advanced) || walk.simple)) || []
  const withSecrets = (a: Record<string, unknown>) => ({ ...a, ...Object.fromEntries([...smhSecrets].map(([k, v]) => [k, { value: v, label: '•••' }])) })
  const ctx: SmhCtx = { answers: withSecrets(run.answers), mode: run.mode, watch: run.watch, admin: isAdmin(user) }
  const step: SmhStep | undefined = steps[run.i]

  // The next step that applies (a step's when can skip it), or the end.
  const advance = useCallback(
    (answers: Record<string, unknown> = run.answers) => {
      const c = { ...ctx, answers: withSecrets(answers) }
      let j = run.i + 1
      while (j < steps.length && steps[j].when && !steps[j].when!(c)) j++
      if (j >= steps.length) {
        markSmhDone(run.id)
        stopSmh()
        return
      }
      setSmhRun({ ...run, i: j, answers })
    },
    // eslint-disable-next-line react-hooks/exhaustive-deps
    [run],
  )
  const back = () => {
    let j = run.i - 1
    while (j > 0 && (['do', 'wait', 'go'].includes(steps[j].kind) || (steps[j].when && !steps[j].when!(ctx)))) j--
    setSmhRun({ ...run, i: Math.max(0, j) })
  }

  useEffect(() => {
    if (!walk) stopSmh()
  }, [walk])
  // A step that doesn't apply (any more) is passed over.
  useEffect(() => {
    if (step?.when && !step.when(ctx)) advance()
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [run.i])

  const target = step?.kind === 'point' ? (typeof step.target === 'function' ? step.target(ctx) : step.target) : step?.kind === 'go' && !run.watch ? step.via : undefined
  const { el, box } = useBox(target)
  const [lost, setLost] = useState(false)
  useEffect(() => {
    setLost(false)
    if (!target) return
    const id = window.setTimeout(() => setLost(true), 4000)
    return () => window.clearTimeout(id)
  }, [target, run.i])
  // An optional step whose button never shows up (nothing to review, a list
  // with no Pull button) is passed over.
  useEffect(() => {
    if (lost && !el && step?.kind === 'point' && step.optional) advance()
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [lost, el])

  // go: Usherr opens the page itself, unless the person taps the link.
  const goTo = step?.kind === 'go' ? (typeof step.to === 'function' ? step.to(ctx) : step.to) : null
  // A page with a ?tab= in its address is matched with it.
  const here = location.pathname + (goTo?.includes('?') ? location.search : '')
  useEffect(() => {
    if (goTo && (run.watch || !(step?.kind === 'go' && step.via)) && here !== goTo) navigate(goTo)
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [goTo, run.i])
  const arrived = goTo != null && here === goTo

  // point: wait for a tap on it, a page, or something to show up or go away.
  useEffect(() => {
    if (step?.kind !== 'point') return
    const until = step.until ?? 'click'
    if (until === 'click') {
      if (!el) return
      const onClick = (e: MouseEvent) => {
        if (e.target instanceof Node && el.contains(e.target)) window.setTimeout(() => advance(), 60)
      }
      document.addEventListener('click', onClick, true)
      return () => document.removeEventListener('click', onClick, true)
    }
    if (until === 'next') return
    if ('path' in until) {
      if (onPage(location.pathname, until.path)) advance()
      return
    }
    const check = () => {
      const there = !!find('seen' in until ? until.seen : until.gone)
      if ('seen' in until ? there : !there) advance()
    }
    const id = window.setInterval(check, 400)
    return () => window.clearInterval(id)
  }, [step, el, location.pathname, advance])

  // "Watch me do it": Usherr presses the button itself, after showing it.
  const acted = useRef(-1)
  useEffect(() => {
    if (step?.kind !== 'point' || !step.act || !run.watch || !el || acted.current === run.i) return
    const id = window.setTimeout(() => {
      acted.current = run.i
      if (step.act === 'click') el.click()
      else (el.matches('input, textarea') ? el : (el.querySelector<HTMLElement>('input, textarea') ?? el)).focus()
    }, ACT_DELAY)
    return () => window.clearTimeout(id)
  }, [step, el, run.watch, run.i])

  // do: Usherr does it, once.
  const [doing, setDoing] = useState<'idle' | 'busy' | 'failed'>('idle')
  const [doErr, setDoErr] = useState('')
  const ran = useRef(-1)
  const doIt = useCallback(async () => {
    if (step?.kind !== 'do') return
    ran.current = run.i
    setDoing('busy')
    try {
      const found = await step.run(ctx)
      setDoing('idle')
      advance(found ? { ...run.answers, ...found } : run.answers)
    } catch (e) {
      setDoErr(e instanceof Error ? e.message : String(e))
      setDoing('failed')
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [step, run])
  useEffect(() => {
    if (step?.kind === 'do' && ran.current !== run.i) void doIt()
  }, [step, run.i, doIt])

  // wait: check now and then until it's ready.
  useEffect(() => {
    if (step?.kind !== 'wait') return
    let live = true
    const check = () =>
      step
        .check(ctx)
        .then((ok) => live && ok && advance())
        .catch(() => undefined)
    void check()
    const id = window.setInterval(check, step.every ?? 5000)
    return () => {
      live = false
      window.clearInterval(id)
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [step, run.i])

  if (!walk || !step) return null
  const wandered = !!step.path && !location.pathname.startsWith(step.path)
  const atBottom = !!box && box.bottom > window.innerHeight * 0.55
  const phone = window.innerWidth < 640
  const pos = phone ? (atBottom ? 'top' : 'bottom') : box && box.right > window.innerWidth - 400 && atBottom ? 'top' : 'bottom'

  const pause = () => setSmhRun({ ...run, paused: true })
  const finish = () => {
    markSmhDone(run.id)
    stopSmh()
  }

  return createPortal(
    <>
      {box && step.kind === 'point' && (
        <div className={`smh-spot${run.watch && step.act ? ' acting' : ''}`} style={{ left: box.left - 6, top: box.top - 6, width: box.width + 12, height: box.height + 12 }} />
      )}
      {box && step.kind === 'go' && !run.watch && <div className="smh-spot" style={{ left: box.left - 6, top: box.top - 6, width: box.width + 12, height: box.height + 12 }} />}
      <section className={`smh-coach card smh-${pos}`} role="dialog" aria-label={t('Show Me How')} aria-live="polite">
        <header className="smh-head">
          <Icon name={walk.icon} size={16} />
          <span className="smh-walk">{walk.title}</span>
          <span className="smh-count">{t('{n} of {total}', { n: run.i + 1, total: steps.length })}</span>
          <button type="button" className="icon-btn smh-x" onClick={pause} aria-label={t('Pause: carry on later from Show me how')} title={t('Pause: carry on later from Show me how')}>
            <Icon name="x" size={16} />
          </button>
        </header>
        <div className="smh-bar" aria-hidden="true">
          <i style={{ width: `${((run.i + 1) / steps.length) * 100}%` }} />
        </div>
        <h2>{say(step.title, ctx)}</h2>
        {step.text && <p className="smh-text">{say(step.text, ctx)}</p>}

        {wandered && (
          <p className="smh-note">
            {t('This step is on another page.')}{' '}
            <button type="button" className="btn-link link-btn" onClick={() => navigate(step.path!)}>
              {t('Take me back')}
            </button>
          </p>
        )}
        {!wandered && lost && target && !box && <p className="smh-note">{t('I can’t find it on this page yet. If a window closed, go back a step.')}</p>}

        {step.kind === 'say' && step.qr && <Qr text={step.qr(ctx)} />}
        {step.kind === 'ask' && (
          <Ask
            key={run.i}
            step={step}
            ctx={ctx}
            onAnswer={(a) => {
              if (step.input?.secret) {
                smhSecrets.set(step.key, a.value)
                advance(run.answers)
              } else advance({ ...run.answers, [step.key]: a })
            }}
          />
        )}
        {step.kind === 'do' && doing === 'busy' && <p className="smh-busy">{t('Working on it…')}</p>}
        {step.kind === 'do' && doing === 'failed' && (
          <p className="smh-note bad">
            {doErr}{' '}
            <button type="button" className="btn-link link-btn" onClick={() => void doIt()}>
              {t('Try again')}
            </button>
          </p>
        )}
        {step.kind === 'wait' && <p className="smh-busy">{t('I’ll keep watching. You can use the rest of Usherr meanwhile.')}</p>}
        {step.kind === 'point' && run.watch && step.act && <p className="smh-busy">{t('Watch: I’m pressing it for you.')}</p>}
        {step.kind === 'done' && step.shortcut && <p className="smh-short">{say(step.shortcut, ctx)}</p>}

        <footer className="smh-nav">
          {run.i > 0 && step.kind !== 'done' && (
            <button type="button" className="btn-sm" onClick={back}>
              <Icon name="chevron-left" size={14} /> {t('Back')}
            </button>
          )}
          {run.mode === 'simple' && step.kind !== 'done' && (
            <button type="button" className="btn-link link-btn smh-watch" onClick={() => setSmhRun({ ...run, watch: !run.watch })}>
              {run.watch ? t('Let me do it') : t('Do it for me')}
            </button>
          )}
          <span className="spacer" />
          {(step.kind === 'say' || (step.kind === 'point' && step.until === 'next') || (step.kind === 'go' && arrived)) && (
            <button type="button" className="btn-sm primary" onClick={() => advance()} autoFocus>
              {t('Next')} <Icon name="chevron-right" size={14} />
            </button>
          )}
          {step.kind === 'done' && step.then && (!step.then.when || step.then.when(ctx)) && (
            <button
              type="button"
              className="btn-sm"
              onClick={() => {
                markSmhDone(run.id)
                startSmh(step.then!.id, run.mode)
              }}
            >
              {say(step.then.label, ctx)} <Icon name="chevron-right" size={14} />
            </button>
          )}
          {step.kind === 'done' && (
            <button type="button" className="btn-sm primary" onClick={finish} autoFocus>
              <Icon name="check" size={14} /> {t('Done')}
            </button>
          )}
        </footer>
      </section>
    </>,
    document.body,
  )
}

// Qr: a code to scan with a phone's camera (drawn by the server).
function Qr({ text }: { text: string }) {
  const [rows, setRows] = useState<string[] | null>(null)
  useEffect(() => {
    let gone = false
    fetch(`/api/home/marquee/qr?text=${encodeURIComponent(text)}`, { credentials: 'include' })
      .then((r) => (r.ok ? (r.json() as Promise<{ rows: string[] }>) : null))
      .then((d) => !gone && d && setRows(d.rows))
      .catch(() => undefined)
    return () => {
      gone = true
    }
  }, [text])
  if (!rows) return null
  return (
    <div className="smh-qr">
      <QRCode rows={rows} label={text} />
      <code>{text}</code>
    </div>
  )
}

// Ask: the question part of the card. Choices, a search, or a box to type in.
function Ask({ step, ctx, onAnswer }: { step: Extract<SmhStep, { kind: 'ask' }>; ctx: SmhCtx; onAnswer: (a: { value: string; label: string; data?: unknown }) => void }) {
  const choices = typeof step.choices === 'function' ? step.choices(ctx) : step.choices
  const [q, setQ] = useState('')
  const [found, setFound] = useState<SmhChoice[]>([])
  const [looking, setLooking] = useState(false)
  const [val, setVal] = useState('')
  const [test, setTest] = useState<{ ok: boolean; message: string } | null>(null)
  const [testing, setTesting] = useState(false)

  useEffect(() => {
    if (!step.search || q.trim().length < 2) return
    const id = window.setTimeout(() => {
      setLooking(true)
      step
        .search!(q.trim())
        .then(setFound)
        .catch(() => setFound([]))
        .finally(() => setLooking(false))
    }, 350)
    return () => window.clearTimeout(id)
  }, [q, step])

  const pick = (c: SmhChoice) => onAnswer({ value: c.value, label: c.label, data: c.data })
  const runTest = async () => {
    if (!step.input?.test) return
    setTesting(true)
    try {
      setTest(await step.input.test(val.trim()))
    } catch (e) {
      setTest({ ok: false, message: e instanceof Error ? e.message : String(e) })
    } finally {
      setTesting(false)
    }
  }

  return (
    <div className="smh-ask">
      {step.card && <DecisionCard d={step.card} />}
      {choices && (
        <div className="smh-choices">
          {choices.map((c) => (
            <button type="button" key={c.value} className={`smh-choice${c.recommended ? ' rec' : ''}`} onClick={() => pick(c)}>
              <b>{c.label}</b>
              {c.recommended && <span className="smh-rec">{t('Recommended')}</span>}
              {c.hint && <small>{c.hint}</small>}
            </button>
          ))}
        </div>
      )}
      {step.search && (
        <>
          <input type="search" value={q} onChange={(e) => setQ(e.target.value)} placeholder={t('Type a name')} aria-label={say(step.title, ctx)} autoFocus />
          {looking && found.length === 0 && <p className="smh-busy">{t('Looking…')}</p>}
          {found.length > 0 && q.trim().length >= 2 && (
            <ul className="smh-found">
              {found.map((c) => (
                <li key={c.value}>
                  <button type="button" onClick={() => pick(c)}>
                    {c.image ? <img src={c.image} alt="" loading="lazy" /> : <span className="smh-noimg" />}
                    <span>{c.label}</span>
                  </button>
                </li>
              ))}
            </ul>
          )}
        </>
      )}
      {step.input && (
        <div className="smh-input">
          <input
            type={step.input.secret ? 'password' : 'text'}
            value={val}
            onChange={(e) => {
              setVal(e.target.value)
              setTest(null)
            }}
            placeholder={step.input.placeholder}
            aria-label={say(step.title, ctx)}
            autoComplete="off"
            spellCheck={false}
            autoFocus
          />
          {step.input.test ? (
            <button type="button" className="btn-sm" disabled={!val.trim() || testing} onClick={() => void runTest()}>
              {testing ? t('Testing…') : t('Test it')}
            </button>
          ) : null}
          {test && <p className={`smh-test ${test.ok ? 'ok' : 'bad'}`}>{test.ok ? '✓ ' : '✗ '}{test.message}</p>}
          <button type="button" className="btn-sm primary" disabled={!val.trim() || (!!step.input.test && !test?.ok)} onClick={() => onAnswer({ value: val.trim(), label: step.input?.secret ? '•••' : val.trim() })}>
            {t('Next')} <Icon name="chevron-right" size={14} />
          </button>
        </div>
      )}
    </div>
  )
}
