// Loader for tools/build-smh-site.mjs: lets Node read the Show Me How
// walkthroughs (src/smh/walks) on their own. Their imports from inside src/smh
// get the .ts ending Node needs; anything else they import (the API, the
// translator, other parts of the app) is a small stand-in, since building the
// website only reads their words.
import { dirname, extname, resolve as resolvePath } from 'node:path'
import { fileURLToPath, pathToFileURL } from 'node:url'

const smh = resolvePath(dirname(fileURLToPath(import.meta.url)), '..', 'src', 'smh')

const STUB = `
const fill = (s, v) => (v ? s.replace(/\\{(\\w+)\\}/g, (m, k) => (k in v ? String(v[k]) : m)) : s)
export const t = (s, v) => fill(s, v)
export const api = {}
export const comicsApi = {}
export const sportsApi = {}
export function streamingOn() { return Promise.resolve(false) }
export function openShowtimeBooking() {}
export function listShowtimes() { return Promise.resolve({ showtimes: [], nowMs: 0 }) }
export function showtimeEnds() { return 0 }
`
const STUB_URL = 'data:text/javascript,' + encodeURIComponent(STUB)

export async function resolve(specifier, context, next) {
  const parent = context.parentURL?.startsWith('file:') ? fileURLToPath(context.parentURL) : ''
  if (parent.startsWith(smh) && specifier.startsWith('.')) {
    const target = resolvePath(dirname(parent), specifier)
    if (!target.startsWith(smh)) return { url: STUB_URL, shortCircuit: true }
    return { url: pathToFileURL(extname(target) ? target : `${target}.ts`).href, shortCircuit: true }
  }
  return next(specifier, context)
}
