import { useCallback, useEffect, useState } from 'react'
import { Link } from 'react-router-dom'
import { api, type ServiceCheck, type ServiceCheckInput, type ServiceExpect, type ServiceResult, type ServiceSuggestion } from '../../api'
import Icon from '../../components/Icon'
import { useConfirm } from '../../components/ConfirmProvider'
import { useToast } from '../../components/Toast'
import { timeAgo } from '../../format'
import { plural, t } from '../../i18n'
import { useLive } from '../../useLive'
import './serviceChecks.css'

// Usherr: Settings > System > Service checks. Usherr asks the addresses listed
// here every so often (Jellyfin, a download client, Prowlarr, anything with a
// web address), shows how each is doing, and sends a "down" and a "back"
// message through the notifications that chose those events. It does the job
// of a separate watchdog.

const errText = (e: unknown) => (e instanceof Error ? e.message : String(e))

const EXPECTS: { value: ServiceExpect; label: string }[] = [
  { value: 'ok', label: t('Any 2xx or 3xx answer') },
  { value: '200', label: t('200 only') },
  { value: 'any', label: t('Any answer (even a login page)') },
]

const INTERVALS: { sec: number; label: string }[] = [
  { sec: 30, label: t('30 seconds') },
  { sec: 60, label: t('1 minute') },
  { sec: 120, label: t('2 minutes') },
  { sec: 300, label: t('5 minutes') },
  { sec: 600, label: t('10 minutes') },
  { sec: 1800, label: t('30 minutes') },
  { sec: 3600, label: t('1 hour') },
]

const BLANK: ServiceCheckInput = { name: '', url: '', expect: 'ok', keyword: '', intervalSec: 60, failAfter: 3, enabled: true }

function intervalText(sec: number): string {
  const known = INTERVALS.find((i) => i.sec === sec)
  if (known) return known.label
  return sec % 60 === 0 ? plural(sec / 60, '{n} minute', '{n} minutes') : plural(sec, '{n} second', '{n} seconds')
}

function percent(u: { checks: number; percent: number }): string {
  if (u.checks === 0) return '–'
  const p = u.percent
  return `${p === 100 || p === 0 ? p.toFixed(0) : p.toFixed(p >= 99 ? 2 : 1)}%`
}

// What the state line under a service's name says.
function stateLine(c: ServiceCheck): string {
  if (!c.enabled) return t('Paused')
  if (c.state === 'down') return c.failSince ? t('Down · stopped answering {when}', { when: timeAgo(c.failSince) }) : t('Down')
  if (c.fails > 0) return t('Not answering: {n} of {max} failed checks before a message', { n: c.fails, max: c.failAfter })
  if (c.state === 'up') return c.changed ? t('Up · came up {when}', { when: timeAgo(c.changed) }) : t('Up')
  return t('Not checked yet')
}

function stateClass(c: ServiceCheck): string {
  if (!c.enabled) return 'paused'
  if (c.state === 'down') return 'down'
  if (c.fails > 0) return 'shaky'
  return c.state
}

function Strip({ recent }: { recent: ServiceResult[] }) {
  if (recent.length === 0) return null
  const slowest = Math.max(1, ...recent.map((r) => r.ms))
  return (
    <div className="svc-strip" aria-label={t('The latest checks, oldest first')}>
      {recent.map((r) => (
        <span
          key={r.at}
          className={r.ok ? 'ok' : 'bad'}
          style={{ height: r.ok ? `${Math.max(25, Math.round((r.ms / slowest) * 100))}%` : '100%' }}
          title={`${new Date(r.at).toLocaleString()}: ${r.ok ? t('worked in {ms} ms', { ms: r.ms }) : r.error || t('failed')}`}
        />
      ))}
    </div>
  )
}

function ServiceForm({ initial, onSaved, onCancel, id }: { initial: ServiceCheckInput; id?: number; onSaved: (c: ServiceCheck) => void; onCancel: () => void }) {
  const toast = useToast()
  const [form, setForm] = useState<ServiceCheckInput>(initial)
  const [busy, setBusy] = useState(false)
  const [tried, setTried] = useState<ServiceResult | null>(null)
  const set = (patch: Partial<ServiceCheckInput>) => {
    setForm({ ...form, ...patch })
    setTried(null)
  }

  async function save() {
    setBusy(true)
    try {
      const out = id ? await api.updateServiceCheck(id, form) : await api.addServiceCheck(form)
      toast.success(id ? t('Saved.') : t('Added. Usherr checks it from now on.'))
      onSaved(out)
    } catch (e) {
      toast.error(errText(e))
    } finally {
      setBusy(false)
    }
  }

  async function tryIt() {
    setBusy(true)
    try {
      setTried(await api.tryServiceCheck(form))
    } catch (e) {
      toast.error(errText(e))
    } finally {
      setBusy(false)
    }
  }

  return (
    <div className="grid-form svc-form">
      <label>
        {t('Name')}
        <input value={form.name} maxLength={60} placeholder="Jellyfin" onChange={(e) => set({ name: e.target.value })} />
      </label>
      <label>
        {t('Address')}
        <input value={form.url} maxLength={500} placeholder="http://192.168.1.10:8096/health" onChange={(e) => set({ url: e.target.value })} />
      </label>
      <label>
        {t('What counts as working')}
        <select value={form.expect} onChange={(e) => set({ expect: e.target.value as ServiceExpect })}>
          {EXPECTS.map((x) => (
            <option key={x.value} value={x.value}>
              {x.label}
            </option>
          ))}
        </select>
      </label>
      <label>
        {t('Word the page must contain (optional)')}
        <input value={form.keyword} maxLength={100} placeholder="Healthy" onChange={(e) => set({ keyword: e.target.value })} />
      </label>
      <label>
        {t('Check every')}
        <select value={form.intervalSec} onChange={(e) => set({ intervalSec: Number(e.target.value) })}>
          {!INTERVALS.some((i) => i.sec === form.intervalSec) && <option value={form.intervalSec}>{intervalText(form.intervalSec)}</option>}
          {INTERVALS.map((i) => (
            <option key={i.sec} value={i.sec}>
              {i.label}
            </option>
          ))}
        </select>
      </label>
      <label>
        {t('Failed checks in a row before a message')}
        <input type="number" min={1} max={20} value={form.failAfter} onChange={(e) => set({ failAfter: Number(e.target.value) })} />
      </label>
      <label>
        <input type="checkbox" checked={form.enabled} onChange={(e) => set({ enabled: e.target.checked })} />
        {t('Check this service')}
      </label>
      <p className="field-hint">
        {t('Three failed checks in a row (the default) keep a restart or a short blip quiet. Leave user names and passwords out of the address; a key in the address is fine, it is only shown to administrators and never put in a message.')}
      </p>
      {tried && (
        <p className={`svc-tried ${tried.ok ? 'ok' : 'bad'}`}>
          <Icon name={tried.ok ? 'check' : 'warning'} size={14} />{' '}
          {tried.ok ? t('It works: answered {code} in {ms} ms.', { code: tried.code, ms: tried.ms }) : t('It does not work: {why}.', { why: tried.error || t('no answer') })}
        </p>
      )}
      <div className="row-actions">
        <button type="button" className="btn-sm btn-with-icon" disabled={busy || !form.url.trim()} onClick={() => void tryIt()}>
          <Icon name="refresh" size={14} /> {t('Try it')}
        </button>
        <button type="button" className="btn-sm" disabled={busy} onClick={onCancel}>
          {t('Cancel')}
        </button>
        <button type="button" className="primary" disabled={busy || !form.name.trim() || !form.url.trim()} onClick={() => void save()}>
          {id ? t('Save changes') : t('Add service')}
        </button>
      </div>
    </div>
  )
}

function ServiceRow({ c, onChanged, onRemoved }: { c: ServiceCheck; onChanged: (c: ServiceCheck) => void; onRemoved: () => void }) {
  const toast = useToast()
  const confirm = useConfirm()
  const [editing, setEditing] = useState(false)
  const [busy, setBusy] = useState(false)
  const [open, setOpen] = useState(false)

  async function checkNow() {
    setBusy(true)
    try {
      const out = await api.checkServiceNow(c.id)
      onChanged(out)
      if (out.lastError) toast.error(t('{name} did not answer as expected: {why}.', { name: c.name, why: out.lastError }))
      else toast.success(t('{name} answered in {ms} ms.', { name: c.name, ms: out.lastMs }))
    } catch (e) {
      toast.error(errText(e))
    } finally {
      setBusy(false)
    }
  }

  async function remove() {
    if (!(await confirm({ title: t('Remove {name}?', { name: c.name }), body: <p>{t('Usherr stops checking it and forgets its history.')}</p>, confirmLabel: t('Remove'), danger: true }))) return
    try {
      await api.deleteServiceCheck(c.id)
      onRemoved()
    } catch (e) {
      toast.error(errText(e))
    }
  }

  return (
    <li className={`svc-row ${stateClass(c)}`}>
      <span className="svc-dot" aria-hidden="true" />
      <div className="svc-main">
        <div className="svc-head">
          <strong>{c.name}</strong>
          <small>{stateLine(c)}</small>
        </div>
        {c.enabled && c.lastError && <div className="watch-why">{c.lastError}</div>}
        <Strip recent={c.recent} />
        <div className="svc-numbers">
          <span>
            {t('Last 24 hours')} <b>{percent(c.uptime24h)}</b>
          </span>
          <span>
            {t('Last 7 days')} <b>{percent(c.uptime7d)}</b>
          </span>
          <span>
            {t('Average answer')} <b>{c.uptime24h.avgMs ? `${c.uptime24h.avgMs} ms` : '–'}</b>
          </span>
          <span>
            {t('Checked')} <b>{c.checked ? timeAgo(c.checked) : t('not yet')}</b> · {intervalText(c.intervalSec)}
          </span>
        </div>
        {c.changes.length > 0 && (
          <button type="button" className="link-btn" onClick={() => setOpen(!open)}>
            {open ? t('Hide the outages') : plural(c.changes.filter((x) => x.state === 'down').length, 'Show the outages ({n})', 'Show the outages ({n})')}
          </button>
        )}
        {open && (
          <ul className="svc-changes">
            {c.changes.map((x) => (
              <li key={x.at + x.state} className={x.state}>
                <b>{x.state === 'down' ? t('Down') : t('Back')}</b> {new Date(x.at).toLocaleString()} <small>{x.detail}</small>
              </li>
            ))}
          </ul>
        )}
        {editing && (
          <ServiceForm
            id={c.id}
            initial={{ name: c.name, url: c.url, expect: c.expect, keyword: c.keyword, intervalSec: c.intervalSec, failAfter: c.failAfter, enabled: c.enabled }}
            onSaved={(out) => {
              setEditing(false)
              onChanged(out)
            }}
            onCancel={() => setEditing(false)}
          />
        )}
      </div>
      {!editing && (
        <div className="svc-actions">
          <button type="button" className="btn-sm" disabled={busy || !c.enabled} onClick={() => void checkNow()}>
            {busy ? t('Checking…') : t('Check now')}
          </button>
          <button type="button" className="btn-sm" onClick={() => setEditing(true)}>
            {t('Edit')}
          </button>
          <button type="button" className="btn-sm" onClick={() => void remove()}>
            {t('Remove')}
          </button>
        </div>
      )}
    </li>
  )
}

export default function ServiceChecksSettings() {
  const toast = useToast()
  const [checks, setChecks] = useState<ServiceCheck[] | null>(null)
  const [alertTargets, setAlertTargets] = useState(0)
  const [ideas, setIdeas] = useState<ServiceSuggestion[]>([])
  const [adding, setAdding] = useState<ServiceCheckInput | null>(null)

  const load = useCallback(() => {
    api
      .serviceChecks()
      .then((r) => {
        setChecks(r.checks)
        setAlertTargets(r.alertTargets)
      })
      .catch(() => setChecks((c) => c ?? []))
  }, [])
  const loadIdeas = useCallback(() => {
    api
      .serviceCheckSuggestions()
      .then(setIdeas)
      .catch(() => setIdeas([]))
  }, [])
  useEffect(load, [load])
  useEffect(loadIdeas, [loadIdeas])
  useLive(load, 15_000)

  async function addIdea(x: ServiceSuggestion) {
    try {
      const out = await api.addServiceCheck({ ...BLANK, name: x.name, url: x.url, expect: x.expect })
      setChecks((list) => [...(list ?? []), out])
      setIdeas((list) => list.filter((y) => y.url !== x.url))
      toast.success(t('Added {name}. Usherr checks it from now on.', { name: x.name }))
    } catch (e) {
      toast.error(errText(e))
    }
  }

  if (!checks) return null
  const down = checks.filter((c) => c.enabled && c.state === 'down').length
  return (
    <div className="settings-stack">
      <section className="card wide">
        <h2>{t('Service checks')}</h2>
        <p style={{ color: 'var(--text-dim)', marginTop: 0 }}>
          {t('Usherr asks each address below every so often. When one stops answering a few times in a row, you get one message; when it answers again, you get another. Use it for Jellyfin, Plex, your download clients, Prowlarr or any other program with a web address.')}
        </p>
        {alertTargets === 0 ? (
          <p className="svc-alert-note bad">
            <Icon name="warning" size={14} /> {t('No notification sends these messages yet. Open a notification and tick "Service down" and "Service back".')}{' '}
            <Link to="/settings/notifications">{t('Notifications')}</Link>
          </p>
        ) : (
          <p className="svc-alert-note ok">
            <Icon name="check" size={14} /> {plural(alertTargets, 'Messages go to {n} notification.', 'Messages go to {n} notifications.')}{' '}
            <Link to="/settings/notifications">{t('Notifications')}</Link>
          </p>
        )}
        {checks.length > 0 && (
          <p className="svc-summary">
            {down === 0 ? t('All services are answering.') : plural(down, '{n} service is down.', '{n} services are down.')}
          </p>
        )}
        {checks.length === 0 && !adding && <p>{t('No services yet. Add one below, or pick one Usherr already knows.')}</p>}
        {checks.length > 0 && (
          <ul className="svc-list">
            {checks.map((c) => (
              <ServiceRow
                key={c.id}
                c={c}
                onChanged={(out) => setChecks((list) => (list ?? []).map((x) => (x.id === out.id ? out : x)))}
                onRemoved={() => {
                  setChecks((list) => (list ?? []).filter((x) => x.id !== c.id))
                  loadIdeas()
                }}
              />
            ))}
          </ul>
        )}
        {adding ? (
          <ServiceForm
            initial={adding}
            onSaved={(out) => {
              setAdding(null)
              setChecks((list) => [...(list ?? []), out])
              loadIdeas()
            }}
            onCancel={() => setAdding(null)}
          />
        ) : (
          <div className="row-actions">
            <button type="button" className="btn-sm btn-with-icon" disabled={checks.length >= 50} onClick={() => setAdding(BLANK)}>
              <Icon name="plus" size={14} /> {t('Add a service')}
            </button>
          </div>
        )}
      </section>
      {ideas.length > 0 && (
        <section className="card wide">
          <h2>{t('Usherr already knows these')}</h2>
          <p style={{ color: 'var(--text-dim)', marginTop: 0 }}>{t('From your other settings. Add one with its usual address, then change it if you like.')}</p>
          <ul className="svc-ideas">
            {ideas.map((x) => (
              <li key={x.url}>
                <div>
                  <strong>{x.name}</strong> <small>{x.from}</small>
                  <div className="svc-url">{x.url}</div>
                </div>
                <button type="button" className="btn-sm btn-with-icon" disabled={checks.length >= 50} onClick={() => void addIdea(x)}>
                  <Icon name="plus" size={14} /> {t('Add')}
                </button>
              </li>
            ))}
          </ul>
        </section>
      )}
    </div>
  )
}
