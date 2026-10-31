// Usherr: Show Me How (SMH). A walkthrough is plain data: a list of steps
// the engine (SmhHost.tsx) plays on the real screens. Buttons and fields a
// step points at carry a data-walk="…" label, so a change of layout doesn't
// break a walkthrough.
import type { ModuleKey } from '../api'
import type { IconName } from '../components/Icon'

// Simple asks only what matters and does the rest ("Watch me do it"; the
// person can take over with "Let me do it"). Advanced walks every option.
export type SmhMode = 'simple' | 'advanced'

// What a step can read: the answers given so far, and who is asking.
export interface SmhCtx {
  answers: Record<string, unknown>
  mode: SmhMode
  watch: boolean // Usherr presses the buttons itself
  admin: boolean
}

export type SmhText = string | ((c: SmhCtx) => string)

// A choice in an ask step (a film from a search, an option of a setting).
export interface SmhChoice {
  value: string
  label: string
  hint?: string
  image?: string
  recommended?: boolean
  data?: unknown // kept with the answer (a film's details, for example)
}

// A decision card: what the choice is, the options with their good and bad
// sides, and whether it can be changed later (what nervous people most want
// to know).
export interface SmhDecision {
  title: string
  options: { name: string; good: string; bad?: string; cost?: string }[]
  recommend?: string
  later: string
}

interface StepBase {
  title: SmhText
  text?: SmhText
  when?: (c: SmhCtx) => boolean // skipped when false
  path?: string // the page this step is on; wandering off offers "Take me back"
}

export type SmhStep =
  // A card with a Next button.
  | (StepBase & { kind: 'say' })
  // A page to be on. Usherr goes there itself; in "Let me do it" with via,
  // it points at the link to tap.
  | (StepBase & { kind: 'go'; to: string | ((c: SmhCtx) => string); via?: string })
  // Point at something on the page and wait: for a tap on it (click), for
  // the person to say they're done (next), for a page, or for something to
  // appear or go away. act: what Usherr does in "Watch me do it".
  | (StepBase & {
      kind: 'point'
      target: string | ((c: SmhCtx) => string)
      // a path ending in * matches any page under it (/comics/*)
      until?: 'click' | 'next' | { path: string } | { seen: string } | { gone: string }
      act?: 'click' | 'focus'
      optional?: boolean // passed over when it doesn't show up (content still loading, or none)
    })
  // A question in the card: choices, a search, or a box to type in (with a
  // live test, for keys). The answer is kept under key.
  | (StepBase & {
      kind: 'ask'
      key: string
      choices?: SmhChoice[] | ((c: SmhCtx) => SmhChoice[])
      search?: (q: string) => Promise<SmhChoice[]>
      input?: { placeholder?: string; secret?: boolean; test?: (v: string) => Promise<{ ok: boolean; message: string }> }
      card?: SmhDecision
    })
  // Something that takes a while (a download, an audiobook being read).
  | (StepBase & { kind: 'wait'; check: (c: SmhCtx) => Promise<boolean>; every?: number })
  // Usherr does it. What it returns is added to the answers (something it
  // looked up, for the steps after).
  | (StepBase & { kind: 'do'; run: (c: SmhCtx) => Promise<void | Record<string, unknown>> })
  // The end, with a shortcut for next time, and maybe the walkthrough that
  // naturally comes next.
  | (StepBase & { kind: 'done'; shortcut?: SmhText; then?: { id: string; label: SmhText; when?: (c: SmhCtx) => boolean } })

export type SmhGroup = 'watch' | 'night' | 'listen' | 'read' | 'people' | 'library' | 'setup'

export interface Walkthrough {
  id: string
  group: SmhGroup
  title: string
  blurb: string
  icon: IconName
  words?: string // more words to find it by in the menu's search
  admin?: boolean // only administrators see it
  needs?: ModuleKey | 'comics' // only when this kind of media is on
  simple: SmhStep[]
  advanced?: SmhStep[]
}
