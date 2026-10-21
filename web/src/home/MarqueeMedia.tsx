// Usherr: your own Marquee slides and pre-show pieces: upload pictures and
// videos, add a web page or a slide of words, put them in order, switch them
// on and off.
import { useCallback, useEffect, useRef, useState } from 'react'
import Icon from '../components/Icon'
import { useConfirm } from '../components/ConfirmProvider'
import { useToast } from '../components/Toast'
import { t } from '../i18n'
import type { MediaItem } from './marqueeData'

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

// upload sends a file with its progress (fetch can't report it).
function upload(form: FormData, onProgress: (pct: number) => void): Promise<{ media: MediaItem[] }> {
  return new Promise((resolve, reject) => {
    const x = new XMLHttpRequest()
    x.open('POST', '/api/home/marquee/media')
    x.withCredentials = true
    x.upload.onprogress = (e) => e.lengthComputable && onProgress(Math.round((e.loaded / e.total) * 100))
    x.onload = () => {
      let data: { error?: string; media?: MediaItem[] } = {}
      try {
        data = JSON.parse(x.responseText) as typeof data
      } catch {
        data = {}
      }
      if (x.status >= 200 && x.status < 300) resolve({ media: data.media ?? [] })
      else reject(new Error(data.error ?? t('Something went wrong (error {status}).', { status: x.status })))
    }
    x.onerror = () => reject(new Error(t('The upload stopped. Check the connection and try again.')))
    x.send(form)
  })
}

const sizeOf = (n?: number) => (!n ? '' : n >= 1 << 30 ? `${(n / (1 << 30)).toFixed(1)} GB` : n >= 1 << 20 ? `${Math.round(n / (1 << 20))} MB` : `${Math.max(1, Math.round(n / 1024))} KB`)

export default function MarqueeMedia() {
  const [media, setMedia] = useState<MediaItem[] | null>(null)
  const toast = useToast()
  const load = useCallback(() => {
    call<{ media: MediaItem[] }>('GET', '/api/home/marquee/media')
      .then((d) => setMedia(d.media ?? []))
      .catch(() => setMedia([]))
  }, [])
  useEffect(load, [load])
  if (!media) return null
  const saved = (d: { media: MediaItem[] }) => setMedia(d.media ?? [])
  const fail = (e: unknown) => toast.error(e instanceof Error ? e.message : String(e))
  return (
    <div className="mqm-media">
      <Place place="slide" title={t('Your slides')} blurb={t('Pictures, videos, web pages and your own words, taking turns in the Slides scene.')} list={media.filter((m) => m.place === 'slide')} saved={saved} fail={fail} />
      <Place
        place="preshow"
        title={t('Your pre-show')}
        blurb={t('Your own ads, trailers or intros, played in the pre-show after the popcorn and phones. MP4 or WebM videos play to their end, with sound when the screen allows it.')}
        list={media.filter((m) => m.place === 'preshow')}
        saved={saved}
        fail={fail}
      />
    </div>
  )
}

function Place({ place, title, blurb, list, saved, fail }: { place: 'slide' | 'preshow'; title: string; blurb: string; list: MediaItem[]; saved: (d: { media: MediaItem[] }) => void; fail: (e: unknown) => void }) {
  const confirm = useConfirm()
  const file = useRef<HTMLInputElement>(null)
  const [progress, setProgress] = useState<number | null>(null)
  const [adding, setAdding] = useState<'' | 'web' | 'text'>('')
  const [form, setForm] = useState({ title: '', url: '', body: '', seconds: 12 })

  const put = (m: MediaItem, patch: Partial<MediaItem>) =>
    call<{ media: MediaItem[] }>('PUT', `/api/home/marquee/media/${m.id}`, { ...m, ...patch })
      .then(saved)
      .catch(fail)
  const move = (i: number, by: number) => {
    const j = i + by
    if (j < 0 || j >= list.length) return
    const a = list[i]
    const b = list[j]
    // Swap their places in the order.
    void call<{ media: MediaItem[] }>('PUT', `/api/home/marquee/media/${a.id}`, { ...a, sort: b.sort === a.sort ? a.sort + by : b.sort })
      .then(() => call<{ media: MediaItem[] }>('PUT', `/api/home/marquee/media/${b.id}`, { ...b, sort: a.sort }))
      .then(saved)
      .catch(fail)
  }
  const remove = async (m: MediaItem) => {
    if (!(await confirm({ title: t('Remove “{name}”?', { name: m.title || t('this slide') }), body: m.fileUrl ? t('Its file is deleted from Usherr too.') : undefined, confirmLabel: t('Remove'), danger: true }))) return
    call<{ media: MediaItem[] }>('DELETE', `/api/home/marquee/media/${m.id}`).then(saved).catch(fail)
  }
  const send = (f: File) => {
    const fd = new FormData()
    fd.append('place', place)
    fd.append('title', f.name.replace(/\.[^.]+$/, ''))
    fd.append('seconds', '12')
    fd.append('file', f)
    setProgress(0)
    upload(fd, setProgress)
      .then(saved)
      .catch(fail)
      .finally(() => setProgress(null))
  }
  const addPage = () =>
    call<{ media: MediaItem[] }>('POST', '/api/home/marquee/media/page', { place, kind: adding, title: form.title, url: form.url, body: form.body, seconds: form.seconds })
      .then((d) => {
        saved(d)
        setAdding('')
        setForm({ title: '', url: '', body: '', seconds: 12 })
      })
      .catch(fail)

  return (
    <section className="mqm-place">
      <h3>{title}</h3>
      <p className="hint">{blurb}</p>
      {list.length === 0 && <p className="mqm-none">{t('Nothing here yet.')}</p>}
      <ul className="mqm-list">
        {list.map((m, i) => (
          <li key={m.id} className={m.enabled ? '' : 'off'}>
            <span className="mqm-thumb">
              {m.kind === 'image' && <img src={m.fileUrl} alt="" loading="lazy" />}
              {m.kind === 'video' && <video src={m.fileUrl} muted preload="metadata" />}
              {m.kind === 'web' && <Icon name="globe" size={20} />}
              {m.kind === 'text' && <Icon name="edit" size={20} />}
            </span>
            <span className="mqm-what">
              <input key={`${m.id}-${m.title}`} defaultValue={m.title} placeholder={t('Title')} maxLength={100} onBlur={(e) => e.target.value !== m.title && void put(m, { title: e.target.value })} aria-label={t('Title')} />
              <small>
                {m.kind === 'web' ? m.url : m.kind === 'text' ? m.body : sizeOf(m.size)}
                {m.kind !== 'video' && (
                  <>
                    {' · '}
                    <input className="mqm-secs" type="number" min={3} max={600} defaultValue={m.seconds} onBlur={(e) => Number(e.target.value) !== m.seconds && void put(m, { seconds: Number(e.target.value) || 12 })} aria-label={t('Seconds')} /> {t('sec')}
                  </>
                )}
              </small>
            </span>
            <label className="cx-check" title={t('On')}>
              <input type="checkbox" checked={m.enabled} onChange={(e) => void put(m, { enabled: e.target.checked })} aria-label={t('On')} />
            </label>
            <button className="icon-btn" onClick={() => move(i, -1)} disabled={i === 0} aria-label={t('Move up')}>
              <Icon name="chevron-up" size={15} />
            </button>
            <button className="icon-btn" onClick={() => move(i, 1)} disabled={i === list.length - 1} aria-label={t('Move down')}>
              <Icon name="chevron-down" size={15} />
            </button>
            <button className="icon-btn" onClick={() => void remove(m)} aria-label={t('Remove')}>
              <Icon name="trash" size={15} />
            </button>
          </li>
        ))}
      </ul>
      <div className="mqm-add">
        <input
          ref={file}
          type="file"
          hidden
          accept="image/png,image/jpeg,image/webp,image/gif,video/mp4,video/webm"
          onChange={(e) => {
            const f = e.target.files?.[0]
            e.target.value = ''
            if (f) send(f)
          }}
        />
        <button className="btn-sm btn-with-icon" disabled={progress !== null} onClick={() => file.current?.click()}>
          <Icon name="plus" size={14} /> {progress !== null ? t('Uploading… {n}%', { n: progress }) : t('Picture or video')}
        </button>
        <button className="btn-sm btn-with-icon" onClick={() => setAdding(adding === 'web' ? '' : 'web')}>
          <Icon name="globe" size={14} /> {t('Web page')}
        </button>
        <button className="btn-sm btn-with-icon" onClick={() => setAdding(adding === 'text' ? '' : 'text')}>
          <Icon name="edit" size={14} /> {t('Your words')}
        </button>
      </div>
      {adding && (
        <div className="grid-form mqm-form">
          <label>
            {t('Title')}
            <input value={form.title} maxLength={100} onChange={(e) => setForm({ ...form, title: e.target.value })} placeholder={adding === 'web' ? t('Weather') : t('Movie night')} />
          </label>
          {adding === 'web' ? (
            <label>
              {t('Address')}
              <input value={form.url} onChange={(e) => setForm({ ...form, url: e.target.value })} placeholder="https://" inputMode="url" />
            </label>
          ) : (
            <label>
              {t('Words')}
              <textarea value={form.body} maxLength={600} rows={3} onChange={(e) => setForm({ ...form, body: e.target.value })} placeholder={t('Friday at 8, bring snacks')} />
            </label>
          )}
          <label>
            {t('Seconds on screen')}
            <input type="number" min={3} max={600} value={form.seconds} onChange={(e) => setForm({ ...form, seconds: Number(e.target.value) || 12 })} />
          </label>
          {adding === 'web' && <span className="hint">{t('Some sites refuse to be shown inside another page; those stay blank.')}</span>}
          <div style={{ display: 'flex', gap: 8 }}>
            <button className="primary btn-sm" onClick={() => void addPage()}>
              {t('Add')}
            </button>
            <button className="btn-sm" onClick={() => setAdding('')}>
              {t('Cancel')}
            </button>
          </div>
        </div>
      )}
    </section>
  )
}
