// The Show Me How self-check, run before every build (package.json
// prebuild): every button or field a walkthrough points at must still carry
// its data-walk label somewhere in the app. A change of layout that drops one
// stops the build with the walkthrough and the label, instead of shipping a
// walkthrough that points at nothing.
//
//   node --experimental-strip-types --no-warnings tools/check-smh.mjs
import { readdirSync, readFileSync, statSync } from 'node:fs'
import { register } from 'node:module'
import { dirname, join } from 'node:path'
import { fileURLToPath, pathToFileURL } from 'node:url'

const here = dirname(fileURLToPath(import.meta.url))
const src = join(here, '..', 'src')
register(pathToFileURL(join(here, 'smh-loader.mjs')).href)
const { WALKS } = await import(pathToFileURL(join(src, 'smh', 'walks', 'index.ts')).href)

// Every label in the app: data-walk="id", and data-walk={`prefix-${…}`}
// (a family of labels, such as rtm-book, rtm-voice).
const exact = new Set()
const prefixes = []
const walk = (dir) => {
  for (const f of readdirSync(dir)) {
    const p = join(dir, f)
    if (statSync(p).isDirectory()) walk(p)
    else if (/\.tsx?$/.test(f) && !p.includes(`${join('smh', 'walks')}`)) {
      const s = readFileSync(p, 'utf8')
      for (const m of s.matchAll(/data-walk="([a-z0-9-]+)"/g)) exact.add(m[1])
      for (const m of s.matchAll(/data-walk=\{[^`}]*`([a-z0-9-]+)\$\{/g)) prefixes.push(m[1])
    }
  }
}
walk(src)
const exists = (id) => exact.has(id) || prefixes.some((p) => id.startsWith(p))

// What each walkthrough points at. A target worked out from an answer is
// tried with every choice the walkthrough offers.
const problems = []
for (const w of WALKS) {
  const choices = [...w.simple, ...(w.advanced ?? [])].flatMap((s) => (s.kind === 'ask' && Array.isArray(s.choices) ? s.choices : []))
  for (const mode of ['simple', 'advanced']) {
    for (const s of w[mode] ?? []) {
      const ids = []
      if (s.kind === 'point') {
        if (typeof s.target === 'string') ids.push(s.target)
        else for (const c of choices) {
          const answers = new Proxy({}, { get: () => ({ value: c.value, label: c.label }) })
          try {
            ids.push(s.target({ answers, mode, watch: false, admin: true }))
          } catch {
            // a target that needs another kind of answer
          }
        }
      }
      if (s.kind === 'go' && s.via) ids.push(s.via)
      for (const id of ids) if (id && !exists(id)) problems.push(`${w.id} (${mode}) points at "${id}", which no element in the app is labelled with`)
      if (s.kind === 'done' && s.then && !WALKS.some((x) => x.id === s.then.id)) problems.push(`${w.id} leads to "${s.then.id}", which isn't a walkthrough`)
    }
  }
}
const ids = WALKS.map((w) => w.id)
for (const id of new Set(ids.filter((x, i) => ids.indexOf(x) !== i))) problems.push(`two walkthroughs are called "${id}"`)

if (problems.length) {
  console.error(`Show Me How: ${problems.length} problem(s):\n  ` + [...new Set(problems)].join('\n  '))
  process.exit(1)
}
console.log(`Show Me How: ${WALKS.length} walkthroughs, every target found`)
