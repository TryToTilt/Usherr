// Usherr: linked screenings, step 4 (internal/api/home_link_chat.go). The
// reactions' arithmetic, kept apart so it can be tested: the heat bars
// (how many reactions each stretch of the film got), which kept reactions
// are due to float up again as the film plays past them, and a film
// position as a clock.

export const WORDS = ['HA', 'EEK', 'WOW', 'NOPE', 'CLAP'] as const
export type Word = (typeof WORDS)[number]

export interface Reaction {
  id: string
  name?: string
  house?: string
  word: Word
  pos: number // seconds into the film
  atMs?: number
}

export interface HeatBar {
  pos: number // the stretch's start, seconds into the film
  count: number
  top: Word // the word sent most there
}

export const BUCKET = 30 // seconds a heat bar covers (the server's reactBucket)

export const isWord = (w: string): w is Word => (WORDS as readonly string[]).includes(w)

// heatOf makes the heat bars of a list of reactions (pre-show ones left
// out), in order. Ties go to the first word in WORDS.
export function heatOf(list: readonly Reaction[], bucket = BUCKET): HeatBar[] {
  const by = new Map<number, Map<Word, number>>()
  for (const r of list) {
    if (!(r.pos >= 0) || !isWord(r.word)) continue
    const b = Math.floor(r.pos / bucket)
    const words = by.get(b) ?? new Map<Word, number>()
    words.set(r.word, (words.get(r.word) ?? 0) + 1)
    by.set(b, words)
  }
  return [...by.keys()]
    .sort((a, b) => a - b)
    .map((b) => {
      const words = by.get(b) as Map<Word, number>
      let top: Word = WORDS[0]
      let most = 0
      let count = 0
      for (const w of WORDS) {
        const n = words.get(w) ?? 0
        count += n
        if (n > most) {
          top = w
          most = n
        }
      }
      return { pos: b * bucket, count, top }
    })
}

// dueReplays is the kept reactions to float up as the film went from one
// position to the next (seconds): those between them not shown yet. Only
// while it plays on normally: a seek (back, or more than 2 s ahead) floats
// nothing, so jumping about doesn't set off a shower.
export function dueReplays(list: readonly Reaction[], from: number, to: number, shown: ReadonlySet<string>): Reaction[] {
  if (!(to > from) || to - from > 2) return []
  return list.filter((r) => r.pos > from && r.pos <= to && !shown.has(r.id))
}

// filmClock is a film position as "1:12:40" (or "12:40" under an hour;
// "-0:30" in the pre-show).
export function filmClock(secs: number): string {
  const neg = secs < 0
  const t = Math.floor(Math.abs(secs))
  const h = Math.floor(t / 3600)
  const m = Math.floor(t / 60) % 60
  const s = String(t % 60).padStart(2, '0')
  const out = h > 0 ? `${h}:${String(m).padStart(2, '0')}:${s}` : `${m}:${s}`
  return neg ? `-${out}` : out
}
