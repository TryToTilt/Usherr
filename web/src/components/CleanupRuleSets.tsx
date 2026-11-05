import { useCallback, useEffect, useState } from 'react'
import {
  api,
  type Account,
  type CleanupCondition,
  type CleanupConditionType,
  type CleanupPlan,
  type CleanupRuleSet,
  type CleanupRulesState,
  type MediaServer,
} from '../api'
import { locale, plural, t, st as serverText } from '../i18n'
import Icon from './Icon'
import Switch from './Switch'
import { useToast } from './Toast'
import { useConfirm } from './ConfirmProvider'
import TipLabel from './TipLabel'

// Usherr: clean-up rule sets (Settings > Media servers), the job Maintainerr
// did. A match goes on Leaving soon first and is only acted on after the
// grace period, if it still matches. Streamed titles are never touched.

const SIMPLE: CleanupConditionType[] = ['watchedDays', 'unwatchedDays', 'idleDays']

function conditionLabel(type: CleanupConditionType): string {
  switch (type) {
    case 'watchedDays':
      return t('Watched more than … days ago')
    case 'unwatchedDays':
      return t('Never watched, added more than … days ago')
    case 'idleDays':
      return t('Nobody watched it in … days')
    case 'watchedAll':
      return t('Watched completely')
    case 'sizeOverGB':
      return t('Files bigger than … GB')
    case 'yearBefore':
      return t('Released before the year …')
    case 'hasTag':
      return t('Has the tag …')
    case 'lacksTag':
      return t("Doesn't have the tag …")
    case 'requestedBy':
      return t('Added or requested by …')
    case 'notRequested':
      return t('Nobody requested it')
    case 'quality':
      return t('Quality is …')
    case 'showEnded':
      return t('The show has ended or was cancelled')
  }
}

const ALL_TYPES: CleanupConditionType[] = ['watchedDays', 'unwatchedDays', 'idleDays', 'watchedAll', 'sizeOverGB', 'yearBefore', 'hasTag', 'lacksTag', 'requestedBy', 'notRequested', 'quality', 'showEnded']

function defaultCondition(type: CleanupConditionType): CleanupCondition {
  switch (type) {
    case 'watchedDays':
    case 'unwatchedDays':
    case 'idleDays':
      return { type, number: 30 }
    case 'sizeOverGB':
      return { type, number: 20 }
    case 'yearBefore':
      return { type, number: 2000 }
    case 'hasTag':
    case 'lacksTag':
      return { type, text: '' }
    case 'quality':
      return { type, text: '2160p' }
    case 'requestedBy':
      return { type, number: 0 }
  }
  return { type }
}

function newRuleSet(library: 'movies' | 'tv'): CleanupRuleSet {
  return {
    id: 0,
    name: library === 'movies' ? t('Movies') : t('TV'),
    enabled: false,
    library,
    root: '',
    mode: 'simple',
    match: 'any',
    conditions: [],
    showScope: 'episodes',
    action: 'delete-unmonitor',
    graceDays: 14,
    keepTags: [],
  }
}

function date(ms?: number): string {
  if (!ms) return ''
  return new Date(ms).toLocaleDateString(locale(), { day: 'numeric', month: 'short', year: 'numeric' })
}

function actionLabel(a: CleanupRuleSet['action']): string {
  if (a === 'unmonitor') return t('Only stop looking for it')
  if (a === 'delete') return t('Delete the files, keep looking (download again later)')
  return t('Delete the files and stop looking for it')
}

function ConditionEditor({ c, onChange, onRemove, accounts, disabled }: { c: CleanupCondition; onChange: (c: CleanupCondition) => void; onRemove: () => void; accounts: Account[]; disabled?: boolean }) {
  let value = null
  switch (c.type) {
    case 'watchedDays':
    case 'unwatchedDays':
    case 'idleDays':
    case 'sizeOverGB':
    case 'yearBefore':
      value = (
        <input
          type="number"
          min={c.type === 'yearBefore' ? 1900 : 1}
          max={c.type === 'yearBefore' ? 2200 : 3650}
          value={c.number ?? 0}
          disabled={disabled}
          onChange={(e) => onChange({ ...c, number: Number(e.target.value) || 0 })}
          style={{ width: 90 }}
        />
      )
      break
    case 'hasTag':
    case 'lacksTag':
    case 'quality':
      value = <input value={c.text ?? ''} disabled={disabled} onChange={(e) => onChange({ ...c, text: e.target.value })} placeholder={c.type === 'quality' ? '2160p' : t('Kids')} style={{ width: 140 }} />
      break
    case 'requestedBy':
      value = (
        <select value={c.number ?? 0} disabled={disabled} onChange={(e) => onChange({ ...c, number: Number(e.target.value) })}>
          <option value={0}>{t('Pick an account')}</option>
          {accounts.map((a) => (
            <option key={a.id} value={a.id}>
              {a.name || a.username}
            </option>
          ))}
        </select>
      )
      break
  }
  return (
    <div className="cleanup-rule">
      <select value={c.type} disabled={disabled} onChange={(e) => onChange(defaultCondition(e.target.value as CleanupConditionType))}>
        {ALL_TYPES.map((ty) => (
          <option key={ty} value={ty}>
            {conditionLabel(ty)}
          </option>
        ))}
      </select>
      {value}
      <button type="button" className="btn-sm danger-ghost btn-with-icon" disabled={disabled} onClick={onRemove} aria-label={t('Remove this condition')}>
        <Icon name="x" size={13} />
      </button>
    </div>
  )
}

function SimpleDays({ rs, type, label, hint, onChange, disabled }: { rs: CleanupRuleSet; type: CleanupConditionType; label: string; hint: string; onChange: (rs: CleanupRuleSet) => void; disabled?: boolean }) {
  const cur = rs.conditions.find((c) => c.type === type)
  const set = (n: number) => {
    const rest = rs.conditions.filter((c) => c.type !== type)
    onChange({ ...rs, match: 'any', conditions: n > 0 ? [...rest, { type, number: n }] : rest })
  }
  const [before, after] = t('after {days} days').split('{days}').map((s) => s.trim())
  return (
    <div className="cleanup-rule">
      <Switch checked={!!cur} onChange={(v) => set(v ? 30 : 0)} disabled={disabled} label={label} description={hint} />
      {cur && (
        <label className="cleanup-days">
          {before}
          <input type="number" min={1} max={3650} value={cur.number ?? 30} disabled={disabled} onChange={(e) => set(Math.max(1, Math.min(3650, Number(e.target.value) || 1)))} />
          {after}
        </label>
      )}
    </div>
  )
}

function RuleSetEditor({
  initial,
  state,
  accounts,
  onSaved,
  onCancel,
}: {
  initial: CleanupRuleSet
  state: CleanupRulesState
  accounts: Account[]
  onSaved: () => void
  onCancel: () => void
}) {
  const toast = useToast()
  const [rs, setRs] = useState<CleanupRuleSet>(initial)
  const [tags, setTags] = useState(initial.keepTags.join(', '))
  const [busy, setBusy] = useState(false)
  const folders = rs.library === 'movies' ? state.movieFolders : state.tvFolders
  const hidden = rs.mode === 'simple' && (rs.match === 'all' || rs.conditions.some((c) => !SIMPLE.includes(c.type)))
  const isTV = rs.library === 'tv'

  async function save() {
    setBusy(true)
    try {
      await api.saveCleanupRuleSet({ ...rs, keepTags: tags.split(',').map((x) => x.trim()).filter(Boolean) })
      toast.success(t('Rule set saved.'))
      onSaved()
    } catch (e) {
      toast.error(e instanceof Error ? e.message : String(e))
    } finally {
      setBusy(false)
    }
  }

  return (
    <div className="cleanup-preview">
      <div className="row-actions" style={{ alignItems: 'center', justifyContent: 'space-between' }}>
        <label style={{ display: 'grid', gap: 4 }}>
          {t('Name')}
          <input value={rs.name} maxLength={80} disabled={busy} onChange={(e) => setRs({ ...rs, name: e.target.value })} />
        </label>
        <div className="seg" role="group" aria-label={t('Rule set mode')}>
          <button type="button" className={rs.mode === 'simple' ? 'active' : ''} aria-pressed={rs.mode === 'simple'} onClick={() => setRs({ ...rs, mode: 'simple' })}>
            {t('Simple')}
          </button>
          <button type="button" className={rs.mode === 'advanced' ? 'active' : ''} aria-pressed={rs.mode === 'advanced'} onClick={() => setRs({ ...rs, mode: 'advanced' })}>
            {t('Advanced')}
          </button>
        </div>
      </div>
      <div className="row-actions" style={{ marginTop: 10 }}>
        <label style={{ display: 'grid', gap: 4 }}>
          {t('Library')}
          <select value={rs.library} disabled={busy || rs.id > 0} onChange={(e) => setRs({ ...rs, library: e.target.value as 'movies' | 'tv', root: '' })}>
            <option value="movies">{t('Movies')}</option>
            <option value="tv">{t('TV')}</option>
          </select>
        </label>
        {folders.length > 1 && (
          <label style={{ display: 'grid', gap: 4 }}>
            {t('Folder')}
            <select value={rs.root} disabled={busy} onChange={(e) => setRs({ ...rs, root: e.target.value })}>
              <option value="">{t('Every folder')}</option>
              {folders.map((f) => (
                <option key={f} value={f}>
                  {f}
                </option>
              ))}
            </select>
          </label>
        )}
        {isTV && (
          <label style={{ display: 'grid', gap: 4 }}>
            <TipLabel text={t('Whole season and whole show only count as watched when every downloaded episode in it has been watched.')}>{t('For shows, act on')}</TipLabel>
            <select value={rs.showScope} disabled={busy} onChange={(e) => setRs({ ...rs, showScope: e.target.value as CleanupRuleSet['showScope'] })}>
              <option value="episodes">{t('Each episode')}</option>
              <option value="season">{t('A whole season')}</option>
              <option value="show">{t('The whole show')}</option>
            </select>
          </label>
        )}
      </div>

      {rs.mode === 'simple' ? (
        <div style={{ marginTop: 8 }}>
          <SimpleDays rs={rs} type="watchedDays" label={isTV ? t('Watched a while ago') : t('Movies watched a while ago')} hint={t('Once it was last watched this many days ago.')} onChange={setRs} disabled={busy} />
          <SimpleDays rs={rs} type="unwatchedDays" label={t('Nobody watched it')} hint={t('Never watched, this many days after it arrived.')} onChange={setRs} disabled={busy} />
          <SimpleDays rs={rs} type="idleDays" label={t('Not watched lately')} hint={t('Nobody has watched it in this many days, or ever.')} onChange={setRs} disabled={busy} />
          {hidden && <p className="hint">{t('This rule set has conditions only Advanced shows. They are kept.')}</p>}
        </div>
      ) : (
        <div style={{ marginTop: 8 }}>
          <label className="cleanup-days">
            {t('Match')}
            <select value={rs.match} disabled={busy} onChange={(e) => setRs({ ...rs, match: e.target.value as 'all' | 'any' })}>
              <option value="any">{t('any of these conditions')}</option>
              <option value="all">{t('all of these conditions')}</option>
            </select>
          </label>
          {rs.conditions.map((c, i) => (
            <ConditionEditor
              key={i}
              c={c}
              accounts={accounts}
              disabled={busy}
              onChange={(n) => setRs({ ...rs, conditions: rs.conditions.map((x, j) => (j === i ? n : x)) })}
              onRemove={() => setRs({ ...rs, conditions: rs.conditions.filter((_, j) => j !== i) })}
            />
          ))}
          <button type="button" className="btn-sm btn-with-icon" style={{ marginTop: 8 }} disabled={busy || rs.conditions.length >= 20} onClick={() => setRs({ ...rs, conditions: [...rs.conditions, defaultCondition('watchedDays')] })}>
            <Icon name="plus" size={13} /> {t('Add a condition')}
          </button>
        </div>
      )}

      <div className="row-actions" style={{ marginTop: 12 }}>
        <label style={{ display: 'grid', gap: 4 }}>
          {t('Then')}
          <select value={rs.action} disabled={busy} onChange={(e) => setRs({ ...rs, action: e.target.value as CleanupRuleSet['action'] })}>
            {(['delete-unmonitor', 'unmonitor', 'delete'] as const).map((a) => (
              <option key={a} value={a}>
                {actionLabel(a)}
              </option>
            ))}
          </select>
        </label>
        <label style={{ display: 'grid', gap: 4 }}>
          <TipLabel text={t('A title that matches goes on Leaving soon first. It is only acted on after this many days, and only if it still matches then.')}>{t('Leaving soon for')}</TipLabel>
          <span className="cleanup-days">
            <input type="number" min={0} max={365} value={rs.graceDays} disabled={busy} onChange={(e) => setRs({ ...rs, graceDays: Math.max(0, Math.min(365, Number(e.target.value) || 0)) })} />
            {t('days')}
          </span>
        </label>
      </div>
      <label className="cleanup-keep">
        <TipLabel text={t('A movie or show with one of these tags is never touched by this rule set.')}>{t('Always keep titles tagged')}</TipLabel>
        <input value={tags} onChange={(e) => setTags(e.target.value)} placeholder={t('Keep, Kids')} disabled={busy} />
      </label>
      <div className="row-actions" style={{ marginTop: 12 }}>
        <button className="primary" disabled={busy} onClick={() => void save()}>
          {t('Save rule set')}
        </button>
        <button disabled={busy} onClick={onCancel}>
          {t('Cancel')}
        </button>
      </div>
    </div>
  )
}

function PlanView({ plan }: { plan: CleanupPlan }) {
  const list = (items: { unit: string; kind: string; title: string; reason: string; actMs?: number; ruleSet?: string }[], withDate: boolean) => (
    <ul>
      {items.slice(0, 100).map((it) => (
        <li key={it.unit}>
          <Icon name={it.kind === 'movie' ? 'film' : 'tv'} size={13} /> <strong>{it.title}</strong> <span className="hint">· {serverText(it.reason)}</span>
          {withDate && it.actMs ? <span className="hint"> · {t('on {date}', { date: date(it.actMs) })}</span> : null}
        </li>
      ))}
    </ul>
  )
  const nothing = plan.newLeaving.length + plan.due.length + plan.waiting.length + plan.dropped.length === 0
  return (
    <div className="cleanup-preview">
      {plan.stale && <p className="error-text">{serverText(plan.stale)}</p>}
      {nothing && <p className="hint">{t('Nothing matches right now.')}</p>}
      {plan.due.length > 0 && (
        <>
          <p>
            <strong>{plural(plan.due.length, 'Acted on now: {n} title', 'Acted on now: {n} titles')}</strong>
          </p>
          {list(plan.due, false)}
          {plan.more > 0 && <p className="hint">{plural(plan.more, 'One more waits for the next run (a run acts on up to {limit}).', '{n} more wait for the next run (a run acts on up to {limit}).', { limit: plan.limit })}</p>}
        </>
      )}
      {plan.newLeaving.length > 0 && (
        <>
          <p>
            <strong>{plural(plan.newLeaving.length, 'Goes on Leaving soon: {n} title', 'Go on Leaving soon: {n} titles')}</strong>
          </p>
          {list(plan.newLeaving, true)}
        </>
      )}
      {plan.waiting.length > 0 && (
        <>
          <p>
            <strong>{plural(plan.waiting.length, 'Leaving soon already: {n} title', 'Leaving soon already: {n} titles')}</strong>
          </p>
          {list(plan.waiting, true)}
        </>
      )}
      {plan.dropped.length > 0 && (
        <>
          <p>
            <strong>{plural(plan.dropped.length, 'No longer matches, comes off Leaving soon: {n} title', 'No longer match, come off Leaving soon: {n} titles')}</strong>
          </p>
          {list(plan.dropped, false)}
        </>
      )}
      {(plan.skippedStreamed > 0 || plan.skippedKept > 0) && (
        <p className="hint">
          {t('Left out: {streamed} streamed, {kept} kept or tagged to keep.', { streamed: plan.skippedStreamed, kept: plan.skippedKept })}
        </p>
      )}
      <p className="hint" style={{ marginBottom: 0 }}>
        {t('Nothing has been changed.')}
      </p>
    </div>
  )
}

export default function CleanupRuleSets() {
  const toast = useToast()
  const confirm = useConfirm()
  const [state, setState] = useState<CleanupRulesState | null>(null)
  const [servers, setServers] = useState<MediaServer[]>([])
  const [accounts, setAccounts] = useState<Account[]>([])
  const [editing, setEditing] = useState<CleanupRuleSet | null>(null)
  const [plan, setPlan] = useState<{ title: string; plan: CleanupPlan } | null>(null)
  const [busy, setBusy] = useState('')
  const [maxPerRun, setMaxPerRun] = useState(50)

  const load = useCallback(() => {
    api
      .cleanupRuleSets()
      .then((s) => {
        setState(s)
        setMaxPerRun(s.options.maxPerRun)
      })
      .catch(() => setState(null))
  }, [])
  useEffect(() => {
    load()
    api
      .listMediaServers()
      .then((l) => setServers(l.filter((s) => s.kind === 'plex' || s.kind === 'jellyfin' || s.kind === 'emby')))
      .catch(() => setServers([]))
    api
      .listAccounts()
      .then(setAccounts)
      .catch(() => setAccounts([]))
  }, [load])

  async function run(label: string, fn: () => Promise<void>) {
    setBusy(label)
    try {
      await fn()
    } catch (e) {
      toast.error(e instanceof Error ? e.message : String(e))
    } finally {
      setBusy('')
    }
  }

  if (!state) return null
  const opts = state.options
  const anyOn = state.ruleSets.some((r) => r.enabled)
  const saveOptions = (o: typeof opts, msg: string) =>
    void run('options', async () => {
      await api.putCleanupOptions(o)
      toast.success(msg)
      load()
    })

  return (
    <fieldset className="group span-all">
      <legend>
        <Icon name="trash" size={14} /> <TipLabel help="media-servers#cleanup-rules">{t('Clean-up rule sets')}</TipLabel>
      </legend>
      <p style={{ color: 'var(--text-dim)', marginTop: 0 }}>
        {t('Rule sets for each library, with more conditions than the rules above. A title that matches goes on Leaving soon first, where everyone can see it; it is only acted on after the grace period, and only if it still matches. Streamed titles are never touched. Every rule set is off until you switch it on, and every action is listed under Activity.')}
      </p>

      {state.ruleSets.length === 0 && !editing && <p className="hint">{t('No rule sets yet.')}</p>}
      {state.ruleSets.map((rs) =>
        editing && editing.id === rs.id ? (
          <RuleSetEditor key={rs.id} initial={editing} state={state} accounts={accounts} onCancel={() => setEditing(null)} onSaved={() => (setEditing(null), load())} />
        ) : (
          <div key={rs.id} className="cleanup-rule">
            <Switch
              checked={rs.enabled}
              disabled={!!busy || !!editing}
              onChange={(v) =>
                void run('enable', async () => {
                  if (v && !(await confirm({ title: t('Switch this rule set on?'), body: t('Once a day, titles that match go on Leaving soon, and are acted on when their date comes if they still match. Check the preview first.'), confirmLabel: t('Switch on'), danger: true })))
                    return
                  await api.saveCleanupRuleSet({ ...rs, enabled: v })
                  toast.success(v ? t('The rule set is on. It runs once a day.') : t('The rule set is off. What it put on Leaving soon came off.'))
                  load()
                })
              }
              label={rs.name}
              description={`${rs.library === 'movies' ? t('Movies') : t('TV')}${rs.root ? ` · ${rs.root}` : ''} · ${actionLabel(rs.action)} · ${plural(rs.graceDays, 'leaving soon for {n} day', 'leaving soon for {n} days')}`}
            />
            <span className="row-actions">
              <button
                className="btn-sm btn-with-icon"
                disabled={!!busy || !!editing}
                onClick={() =>
                  void run('preview', async () => {
                    setPlan({ title: rs.name, plan: await api.cleanupRulesPreview(rs.id) })
                  })
                }
              >
                <Icon name="eye" size={13} /> {t('Preview')}
              </button>
              <button className="btn-sm btn-with-icon" disabled={!!busy || !!editing} onClick={() => setEditing(rs)}>
                <Icon name="edit" size={13} /> {t('Edit')}
              </button>
              <button
                className="btn-sm danger-ghost btn-with-icon"
                disabled={!!busy || !!editing}
                aria-label={t('Delete rule set')}
                onClick={() =>
                  void run('delete', async () => {
                    if (!(await confirm({ title: t('Delete this rule set?'), body: t('What it put on Leaving soon comes off. No files are touched.'), confirmLabel: t('Delete'), danger: true }))) return
                    await api.deleteCleanupRuleSet(rs.id)
                    load()
                  })
                }
              >
                <Icon name="trash" size={13} />
              </button>
            </span>
          </div>
        ),
      )}
      {editing && editing.id === 0 && <RuleSetEditor initial={editing} state={state} accounts={accounts} onCancel={() => setEditing(null)} onSaved={() => (setEditing(null), load())} />}

      <div className="row-actions" style={{ marginTop: 12 }}>
        <button className="btn-with-icon" disabled={!!busy || !!editing} onClick={() => setEditing(newRuleSet('movies'))}>
          <Icon name="plus" size={14} /> {t('Add a rule set')}
        </button>
        <button
          className="btn-with-icon"
          disabled={!!busy || !!editing || !anyOn}
          onClick={() =>
            void run('preview', async () => {
              setPlan({ title: t('Next run'), plan: await api.cleanupRulesPreview() })
            })
          }
        >
          <Icon name="eye" size={14} /> {busy === 'preview' ? t('Checking…') : t('Preview the next run')}
        </button>
        <button
          className="danger-ghost btn-with-icon"
          disabled={!!busy || !!editing || !anyOn}
          onClick={() =>
            void run('run', async () => {
              if (!(await confirm({ title: t('Run the rule sets now?'), body: t('New matches go on Leaving soon. Titles whose date has come, and that still match, are acted on now.'), confirmLabel: t('Run now'), danger: true }))) return
              const r = await api.runCleanupRules()
              toast.success(t('{added} went on Leaving soon, {acted} acted on, {failed} failed.', { added: r.added.length, acted: r.acted.length, failed: r.failed.length }))
              setPlan(null)
              load()
            })
          }
        >
          <Icon name="play" size={14} /> {t('Run now')}
        </button>
      </div>
      {plan && (
        <>
          <p style={{ marginBottom: 0 }}>
            <strong>{t('Preview: {name}', { name: plan.title })}</strong>
          </p>
          <PlanView plan={plan.plan} />
        </>
      )}

      <h4 style={{ marginBottom: 4 }}>{t('Options')}</h4>
      <div className="cleanup-rule">
        <label className="cleanup-days">
          <TipLabel text={t('A safety net: one run acts on at most this many titles. The rest wait for the next run.')}>{t('A run acts on at most')}</TipLabel>
          <input type="number" min={1} max={500} value={maxPerRun} disabled={!!busy} onChange={(e) => setMaxPerRun(Math.max(1, Math.min(500, Number(e.target.value) || 1)))} />
          {t('titles')}
        </label>
        {maxPerRun !== opts.maxPerRun && (
          <button className="btn-sm" disabled={!!busy} onClick={() => saveOptions({ ...opts, maxPerRun }, t('Saved.'))}>
            {t('Save')}
          </button>
        )}
      </div>
      <Switch
        checked={opts.membersKeep}
        disabled={!!busy}
        onChange={(v) => saveOptions({ ...opts, membersKeep: v }, v ? t('Members can keep titles on Leaving soon.') : t('Only administrators can keep titles now.'))}
        label={t('Members can press "Keep it"')}
        description={t('A kept title is never touched by the rule sets. Who kept it is listed below and under Activity.')}
      />
      {servers.map((srv) => (
        <Switch
          key={srv.id}
          checked={opts.collectionServers.includes(srv.id)}
          disabled={!!busy}
          onChange={(v) =>
            saveOptions(
              { ...opts, collectionServers: v ? [...opts.collectionServers, srv.id] : opts.collectionServers.filter((id) => id !== srv.id) },
              v ? t('Leaving soon shows as a collection on {name}.', { name: srv.name }) : t('The Leaving soon collection on {name} is being emptied.', { name: srv.name }),
            )
          }
          label={t('"{collection}" collection on {name}', { collection: opts.collectionName, name: srv.name })}
          description={t('Off unless you switch it on.')}
        />
      ))}

      {state.exempt.length > 0 && (
        <>
          <h4 style={{ marginBottom: 4 }}>{t('Kept titles')}</h4>
          <ul style={{ listStyle: 'none', padding: 0, margin: 0 }}>
            {state.exempt.map((e) => (
              <li key={e.unit} className="cleanup-rule">
                <span>
                  <Icon name={e.kind === 'movie' ? 'film' : 'tv'} size={13} /> <strong>{e.title}</strong>
                  {e.byName ? <span className="hint"> · {t('kept by {name}', { name: e.byName })}</span> : null}
                </span>
                <button
                  className="btn-sm"
                  disabled={!!busy}
                  onClick={() =>
                    void run('unkeep', async () => {
                      await api.unkeepCleanupTitle(e.unit)
                      load()
                    })
                  }
                >
                  {t('Let the rules apply again')}
                </button>
              </li>
            ))}
          </ul>
        </>
      )}
      {opts.lastRun && (
        <p className="hint" style={{ marginBottom: 0 }}>
          {t('Last run {when}.', { when: new Date(opts.lastRun).toLocaleString(locale(), { day: 'numeric', month: 'short', hour: '2-digit', minute: '2-digit' }) })}
        </p>
      )}
      {opts.lastError && <p className="error-text">{serverText(opts.lastError)}</p>}
    </fieldset>
  )
}
