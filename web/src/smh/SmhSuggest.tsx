// Usherr SMH: under the Ask bar, the walkthroughs that match what's being
// typed ("how do I turn a book into an audiobook", "I want to read a
// comic"). Plain word matching, no AI: it shows when the words clearly point
// at a walkthrough, or the question starts like one ("how do I…").
import { useEffect, useState } from 'react'
import { isAdmin } from '../api'
import { useAuth } from '../AuthContext'
import Icon from '../components/Icon'
import { useComicsOn } from '../home/libraryKinds'
import { t } from '../i18n'
import { useModules } from '../ModulesContext'
import { openDoctor, startSmh } from './engine'
import type { Walkthrough } from './types'

const fold = (s: string) => s.toLowerCase().normalize('NFD').replace(/[̀-ͯ]/g, '')
const INTENT = /^(how (do|can|to|would)|i (want|need|would like|'d like) to|i wanna|show me|help me|where (do|can) i|can i|teach me|walk me|(make|set ?up|add|turn|book|follow|import|move|restyle|swap|connect|install|pair|switch) )/
// Words that say nothing about which walkthrough.
const STOP = new Set('a add an and are as at be but by can do does for from get got have help how i i\'d in into is it its let like make me my need of on or set show so some that the this to up use want wanna way what when where which who why will with would you your'.split(' '))

function words(s: string): string[] {
  return fold(s)
    .split(/[^a-z0-9]+/)
    .filter((w) => w.length > 2 && !STOP.has(w))
}

// Does a query word match a walkthrough word? "audiobook" finds
// "audiobooks", "comics" finds "comic"; a short word must match whole.
const hit = (x: string, h: string) => {
  const stem = x.slice(0, Math.max(4, x.length - 2))
  return h.startsWith(stem) || (h.length >= 5 && x.startsWith(h.slice(0, h.length - 1)))
}

// How well a walkthrough fits: each query word it has, weighted by how rare
// the word is among all walkthroughs ("phone" says more than "watch"), and
// double in the title.
function scorer(walks: Walkthrough[]) {
  const docs = walks.map((w) => ({ w, title: words(w.title), all: words(`${w.title} ${w.blurb} ${w.words ?? ''}`) }))
  const weight = (x: string) => {
    const df = docs.filter((d) => d.all.some((h) => hit(x, h))).length
    return df ? Math.log(1 + walks.length / df) : 0
  }
  return (q: string[]) =>
    docs.map(({ w, title, all }) => {
      let s = 0
      let n = 0
      for (const x of q) {
        if (!all.some((h) => hit(x, h))) continue
        n++
        s += weight(x) * (title.some((h) => hit(x, h)) ? 2 : 1)
      }
      return { w, s, n }
    })
}

export default function SmhSuggest({ q }: { q: string }) {
  const { user } = useAuth()
  const modules = useModules()
  const admin = isAdmin(user)
  const comicsOn = useComicsOn(admin)
  const [walks, setWalks] = useState<Walkthrough[] | null>(null)
  const text = fold(q.trim())
  const intent = INTENT.test(text)
  const qw = words(text)
  const worth = intent || qw.length >= 2
  // The list of walkthroughs loads only once something worth matching is typed.
  useEffect(() => {
    if (worth && !walks) void import('./walks').then((m) => setWalks(m.WALKS))
  }, [worth, walks])
  // "Something's not working": the Doctor.
  const trouble = /(not working|doesn'?t work|isn'?t working|broken|wrong|problem|error|stuck|fix)/.test(text)
  if (trouble && admin)
    return (
      <div className="smh-suggest" role="group" aria-label={t('Show me how')}>
        <span className="smh-suggest-label">
          <Icon name="shield" size={14} /> {t('Something not right?')}
        </span>
        <button type="button" className="room-chip smh-suggest-chip" onClick={openDoctor}>
          {t('Check Usherr')}
        </button>
      </div>
    )
  if (!walks || !worth || qw.length === 0) return null
  // The Ask bar's own walkthrough isn't suggested in the Ask bar.
  const can = (w: Walkthrough) => w.id !== 'ask-ai' && (!w.admin || admin) && (!w.needs || (w.needs === 'comics' ? comicsOn : modules.on(w.needs)))
  const ranked = scorer(walks.filter(can))(qw)
    .filter(({ n }) => (intent ? n >= 1 : n >= 2 && n >= qw.length * 0.6))
    .sort((a, b) => b.s - a.s)
  // A second suggestion only when it's nearly as good as the first.
  const best = ranked.filter((r, i) => i === 0 || (i === 1 && r.s >= ranked[0].s * 0.75))
  if (best.length === 0) return null
  return (
    <div className="smh-suggest" role="group" aria-label={t('Show me how')}>
      <span className="smh-suggest-label">
        <Icon name="compass" size={14} /> {t('Show me how:')}
      </span>
      {best.map(({ w }) => (
        <button key={w.id} type="button" className="room-chip smh-suggest-chip" onClick={() => startSmh(w.id, 'simple')} title={w.blurb}>
          {w.title}
        </button>
      ))}
    </div>
  )
}
