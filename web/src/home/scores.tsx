// Home fork: critic and audience scores from MDBList (IMDb, Rotten Tomatoes,
// Metacritic, Letterboxd) and Common Sense Media's age. Cards on a page ask
// together: their ids are gathered for a moment and sent in one request.
import { useEffect, useState } from 'react'
import { t } from '../i18n'

export interface Scores {
  imdb?: number
  imdbVotes?: number
  tomatoes?: number
  popcorn?: number
  metacritic?: number
  letterboxd?: number
  trakt?: number
  commonSense?: number
  imdbId?: string
  rtPath?: string
  lbPath?: string
}

type Kind = 'movie' | 'tv'
const known = new Map<string, Scores | null>()
const waiting = new Map<Kind, Map<number, ((s: Scores | null) => void)[]>>()
let timer = 0

function flush() {
  timer = 0
  for (const [kind, ids] of waiting) {
    waiting.delete(kind)
    const list = [...ids.keys()]
    for (let i = 0; i < list.length; i += 100) {
      const chunk = list.slice(i, i + 100)
      fetch(`/api/home/scores?kind=${kind}&ids=${chunk.join(',')}`, { credentials: 'include' })
        .then((r) => (r.ok ? r.json() : {}))
        .catch(() => ({}))
        .then((got: Record<string, Scores>) => {
          for (const id of chunk) {
            const sc = got[String(id)] ?? null
            known.set(`${kind}:${id}`, sc)
            for (const done of ids.get(id) ?? []) done(sc)
          }
        })
    }
  }
}

function scoresOf(kind: Kind, id: number): Promise<Scores | null> {
  const key = `${kind}:${id}`
  if (known.has(key)) return Promise.resolve(known.get(key) ?? null)
  return new Promise((done) => {
    const byKind = waiting.get(kind) ?? new Map<number, ((s: Scores | null) => void)[]>()
    waiting.set(kind, byKind)
    byKind.set(id, [...(byKind.get(id) ?? []), done])
    if (!timer) timer = window.setTimeout(flush, 60)
  })
}

export function useScores(kind: Kind, id: number | undefined): Scores | null {
  const [sc, setSc] = useState<Scores | null>(() => (id ? (known.get(`${kind}:${id}`) ?? null) : null))
  useEffect(() => {
    if (!id) return
    let live = true
    scoresOf(kind, id).then((s) => live && setSc(s))
    return () => {
      live = false
    }
  }, [kind, id])
  return sc
}

// The small line on a poster: Rotten Tomatoes and IMDb.
export function CardScores({ kind, tmdbId }: { kind: Kind; tmdbId: number }) {
  const sc = useScores(kind, tmdbId)
  if (!sc || (!sc.tomatoes && !sc.imdb)) return null
  return (
    <span className="card-scores">
      {sc.tomatoes ? (
        <span className={`sc-rt${sc.tomatoes >= 60 ? ' fresh' : ' rotten'}`} title={t('Rotten Tomatoes critics')}>
          {sc.tomatoes >= 60 ? '🍅' : 'RT'} {sc.tomatoes}%
        </span>
      ) : null}
      {sc.imdb ? (
        <span className="sc-imdb" title={t('IMDb rating')}>
          IMDb {sc.imdb.toFixed(1)}
        </span>
      ) : null}
    </span>
  )
}

function votes(n?: number): string {
  if (!n) return ''
  return n >= 1_000_000 ? `${(n / 1_000_000).toFixed(1)}M` : n >= 1000 ? `${Math.round(n / 1000)}k` : String(n)
}

// The full line on a title page, each score linked to its site.
export function ScoresLine({ kind, tmdbId }: { kind: Kind; tmdbId: number }) {
  const sc = useScores(kind, tmdbId)
  if (!sc) return null
  const items: { key: string; label: string; value: string; href?: string; cls?: string }[] = []
  if (sc.imdb) items.push({ key: 'imdb', label: 'IMDb', value: `${sc.imdb.toFixed(1)}${sc.imdbVotes ? ` · ${votes(sc.imdbVotes)}` : ''}`, href: sc.imdbId ? `https://www.imdb.com/title/${sc.imdbId}/` : undefined })
  if (sc.tomatoes) items.push({ key: 'rt', label: (sc.tomatoes >= 60 ? '🍅 ' : 'RT ') + t('Critics'), value: `${sc.tomatoes}%`, href: sc.rtPath ? `https://www.rottentomatoes.com${sc.rtPath}` : undefined, cls: sc.tomatoes >= 60 ? 'fresh' : 'rotten' })
  if (sc.popcorn) items.push({ key: 'pop', label: '🍿 ' + t('Audience'), value: `${sc.popcorn}%`, href: sc.rtPath ? `https://www.rottentomatoes.com${sc.rtPath}` : undefined })
  if (sc.metacritic) items.push({ key: 'mc', label: 'Metacritic', value: String(sc.metacritic), cls: sc.metacritic >= 61 ? 'fresh' : sc.metacritic >= 40 ? '' : 'rotten' })
  if (sc.letterboxd) items.push({ key: 'lb', label: 'Letterboxd', value: `${sc.letterboxd.toFixed(1)}/5`, href: sc.lbPath ? `https://letterboxd.com${sc.lbPath}` : undefined })
  if (sc.commonSense) items.push({ key: 'cs', label: t('Common Sense'), value: t('age {n}+', { n: sc.commonSense }) })
  if (items.length === 0) return null
  return (
    <div className="scores-line">
      {items.map((i) =>
        i.href ? (
          <a key={i.key} className={`score-chip ${i.cls ?? ''}`} href={i.href} target="_blank" rel="noreferrer noopener">
            <span>{i.label}</span> <strong>{i.value}</strong>
          </a>
        ) : (
          <span key={i.key} className={`score-chip ${i.cls ?? ''}`}>
            <span>{i.label}</span> <strong>{i.value}</strong>
          </span>
        ),
      )}
    </div>
  )
}
