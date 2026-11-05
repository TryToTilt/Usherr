// Usherr: Settings > Extras > Live TV picture. Which video encoder Live TV
// channels use (Automatic tries NVIDIA, Intel Quick Sync, VAAPI, then the
// CPU) and a watermark in a corner of the picture: the channel's logo, one
// uploaded picture, or the channel's number or name. With the watermark off
// the channels send the picture as it is, without encoding.
import { useCallback, useEffect, useState } from 'react'
import Switch from '../components/Switch'
import { useToast } from '../components/Toast'
import { t } from '../i18n'

type Encoder = 'auto' | 'cpu' | 'nvenc' | 'qsv' | 'vaapi'
type Show = 'logo' | 'image' | 'number' | 'name'
type Position = 'top-left' | 'top-right' | 'bottom-left' | 'bottom-right'

interface Watermark {
  on: boolean
  show: Show
  position: Position
  size: number
  opacity: number
  margin: number
}

interface TVVideo {
  encoder: Encoder
  watermark: Watermark
  results: { kind: Exclude<Encoder, 'auto'>; works: boolean; message: string }[]
  using: Exclude<Encoder, 'auto'>
  hasImage: boolean
  font: boolean
  channels: { id: number; number: number; name: string; logo: boolean; live: boolean; watermark: boolean }[]
}

const ENCODERS: Record<Encoder, string> = {
  auto: t('Automatic'),
  cpu: t('CPU (libx264)'),
  nvenc: t('NVIDIA (NVENC)'),
  qsv: t('Intel Quick Sync'),
  vaapi: t('VAAPI (Intel/AMD on Linux)'),
}

async function call<T>(method: string, path: string, body?: unknown): Promise<T> {
  const form = body instanceof FormData
  const r = await fetch(path, {
    method,
    credentials: 'include',
    cache: 'no-store',
    headers: body === undefined || form ? undefined : { 'Content-Type': 'application/json' },
    body: body === undefined ? undefined : form ? body : JSON.stringify(body),
  })
  const d = await r.json().catch(() => ({}))
  if (!r.ok) throw new Error((d as { error?: string }).error ?? t('Something went wrong (error {status}).', { status: r.status }))
  return d as T
}

const row = { display: 'flex', gap: 8, flexWrap: 'wrap', alignItems: 'center' } as const
const small = { color: 'var(--text-dim)', margin: 0, fontSize: 13 } as const

export default function TVPictureCard() {
  const toast = useToast()
  const [v, setV] = useState<TVVideo | null>(null)
  const [encoder, setEncoder] = useState<Encoder>('auto')
  const [wm, setWm] = useState<Watermark | null>(null)
  const [busy, setBusy] = useState(false)
  const [previewCh, setPreviewCh] = useState(0)
  const [preview, setPreview] = useState('')
  const [previewing, setPreviewing] = useState(false)

  const take = useCallback((d: TVVideo) => {
    setV(d)
    setEncoder(d.encoder)
    setWm(d.watermark)
    setPreviewCh((c) => c || (d.channels.find((ch) => !ch.live)?.id ?? 0))
  }, [])
  useEffect(() => {
    call<TVVideo>('GET', '/api/tv/video')
      .then(take)
      .catch(() => setV(null))
  }, [take])
  useEffect(() => () => URL.revokeObjectURL(preview), [preview])
  if (!v || !wm) return null

  const changed = encoder !== v.encoder || JSON.stringify(wm) !== JSON.stringify(v.watermark)
  const set = (p: Partial<Watermark>) => setWm({ ...wm, ...p })
  const run = (method: string, path: string, body: unknown, done: string) => {
    setBusy(true)
    call<TVVideo>(method, path, body)
      .then((d) => {
        take(d)
        toast.success(done)
      })
      .catch((e: Error) => toast.error(e.message))
      .finally(() => setBusy(false))
  }
  const upload = (f: File | undefined) => {
    if (!f) return
    const fd = new FormData()
    fd.append('file', f)
    run('POST', '/api/tv/video/image', fd, t('Picture uploaded.'))
  }
  const channelMark = (id: number, on: boolean) =>
    call<{ on: boolean }>('PUT', `/api/tv/channels/${id}/watermark`, { on })
      .then(() => {
        setV({ ...v, channels: v.channels.map((c) => (c.id === id ? { ...c, watermark: on } : c)) })
        toast.success(on ? t('This channel shows the watermark.') : t('This channel has no watermark.'))
      })
      .catch((e: Error) => toast.error(e.message))
  const showPreview = () => {
    setPreviewing(true)
    fetch('/api/tv/video/preview', { method: 'POST', credentials: 'include', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ channelId: previewCh, watermark: wm }) })
      .then(async (r) => {
        if (!r.ok) {
          const d = await r.json().catch(() => ({}))
          throw new Error((d as { error?: string }).error ?? t('Something went wrong (error {status}).', { status: r.status }))
        }
        setPreview(URL.createObjectURL(await r.blob()))
      })
      .catch((e: Error) => toast.error(e.message))
      .finally(() => setPreviewing(false))
  }
  const scheduled = v.channels.filter((c) => !c.live)

  return (
    <section id="tv-picture" className={`card service-card ${v.watermark.on ? 'is-included' : 'is-optional'}`}>
      <h2>
        {t('Live TV picture')} <span className="req-badge optional">{t('Optional')}</span>{' '}
        <span className={`badge ${v.watermark.on ? 'downloaded' : 'missing'}`}>{v.watermark.on ? t('watermark on') : t('watermark off')}</span>
      </h2>
      <p style={{ color: 'var(--text-dim)' }}>
        {t('How your channels make their picture when it has to be converted: for a web browser that can’t play a file, and when a watermark is on. Without a watermark, channels send the picture as it is.')}
      </p>

      <div className="grid-form">
        <label>
          {t('Video encoder')}
          <select value={encoder} onChange={(e) => setEncoder(e.target.value as Encoder)}>
            {(Object.keys(ENCODERS) as Encoder[]).map((k) => (
              <option key={k} value={k}>
                {ENCODERS[k]}
              </option>
            ))}
          </select>
        </label>
        <p style={small}>{encoder === 'auto' ? t('Automatic uses the first that works: NVIDIA, Intel Quick Sync, VAAPI, then the CPU. Now: {name}.', { name: ENCODERS[v.using] }) : t('If it fails while playing, the CPU takes over for that program.')}</p>
        <ul style={{ ...small, paddingLeft: 18 }}>
          {v.results.map((r) => (
            <li key={r.kind}>
              <strong>{ENCODERS[r.kind]}</strong>: <span style={{ color: r.works ? 'var(--success, inherit)' : 'var(--danger, inherit)' }}>{r.works ? t('works') : r.message}</span>
            </li>
          ))}
        </ul>
        <div style={row}>
          <button type="button" className="btn-sm" disabled={busy} onClick={() => run('POST', '/api/tv/video/test', {}, t('Encoders tested.'))}>
            {busy ? t('Testing…') : t('Test again')}
          </button>
        </div>
      </div>

      <Switch checked={wm.on} onChange={(on) => set({ on })} label={t('Show a watermark on channels')} description={t('Turning it on makes channels encode their picture, which takes more of the computer (or the graphics card).')} />
      {wm.on && (
        <div className="grid-form">
          <label>
            {t('What to show')}
            <select value={wm.show} onChange={(e) => set({ show: e.target.value as Show })}>
              <option value="logo">{t('Each channel’s logo')}</option>
              <option value="image">{t('A picture you upload')}</option>
              <option value="number">{t('The channel number')}</option>
              <option value="name">{t('The channel name')}</option>
            </select>
          </label>
          {wm.show === 'logo' && <p style={small}>{t('Channels without a logo show no watermark.')}</p>}
          {(wm.show === 'number' || wm.show === 'name') && !v.font && <p style={small}>{t('No font was found in this Usherr image, so text can’t be drawn.')}</p>}
          {wm.show === 'image' && (
            <div style={row}>
              <label className="btn-sm" style={{ cursor: 'pointer' }}>
                {v.hasImage ? t('Replace the picture') : t('Upload a picture')}
                <input type="file" accept="image/png,image/jpeg,image/webp" style={{ display: 'none' }} onChange={(e) => upload(e.target.files?.[0])} />
              </label>
              {v.hasImage && (
                <button type="button" className="btn-sm" disabled={busy} onClick={() => run('DELETE', '/api/tv/video/image', undefined, t('Picture removed.'))}>
                  {t('Remove it')}
                </button>
              )}
              <span style={small}>{v.hasImage ? t('A picture is uploaded.') : t('PNG, JPG or WebP, up to 2 MB. A PNG with a see-through background looks best.')}</span>
            </div>
          )}
          <label>
            {t('Corner')}
            <select value={wm.position} onChange={(e) => set({ position: e.target.value as Position })}>
              <option value="top-left">{t('Top left')}</option>
              <option value="top-right">{t('Top right')}</option>
              <option value="bottom-left">{t('Bottom left')}</option>
              <option value="bottom-right">{t('Bottom right')}</option>
            </select>
          </label>
          <label>
            {t('Size: {n}% of the picture’s width', { n: wm.size })}
            <input type="range" min={5} max={25} step={1} value={wm.size} onChange={(e) => set({ size: Number(e.target.value) })} />
          </label>
          <label>
            {t('Opacity: {n}%', { n: wm.opacity })}
            <input type="range" min={10} max={100} step={5} value={wm.opacity} onChange={(e) => set({ opacity: Number(e.target.value) })} />
          </label>
          <label>
            {t('Distance from the edge: {n}%', { n: wm.margin })}
            <input type="range" min={0} max={10} step={1} value={wm.margin} onChange={(e) => set({ margin: Number(e.target.value) })} />
          </label>
          {scheduled.length > 0 && (
            <div style={row}>
              <label style={row}>
                {t('Preview on')}
                <select value={previewCh} onChange={(e) => setPreviewCh(Number(e.target.value))}>
                  {scheduled.map((c) => (
                    <option key={c.id} value={c.id}>
                      {c.number} {c.name}
                    </option>
                  ))}
                </select>
              </label>
              <button type="button" className="btn-sm" disabled={previewing || !previewCh} onClick={showPreview}>
                {previewing ? t('Making a preview…') : t('Preview')}
              </button>
            </div>
          )}
          {preview && <img src={preview} alt={t('Preview of the watermark')} style={{ width: '100%', maxWidth: 640, borderRadius: 6 }} />}
          {scheduled.length > 0 && (
            <div>
              <p style={small}>{t('Channels with the watermark (saved straight away):')}</p>
              {scheduled.map((c) => (
                <Switch key={c.id} checked={c.watermark} onChange={(on) => void channelMark(c.id, on)} label={`${c.number} ${c.name}`} />
              ))}
            </div>
          )}
        </div>
      )}
      <div style={{ ...row, marginTop: 12 }}>
        <button type="button" className="primary" disabled={busy || !changed} onClick={() => run('PUT', '/api/tv/video', { encoder, watermark: wm }, t('Saved. Encoders tested again.'))}>
          {busy ? t('Saving…') : t('Save')}
        </button>
        {changed && <span style={small}>{t('Changes apply from the next program on each channel.')}</span>}
      </div>
    </section>
  )
}
