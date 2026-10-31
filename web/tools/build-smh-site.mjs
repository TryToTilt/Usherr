// Builds the Show Me How website from the walkthroughs themselves
// (src/smh/walks): a page for each, and a list of them all, written to
// ../site/guide/ and published on GitHub Pages with the Start page. Nobody
// writes it by hand: when a walkthrough changes, run this again.
//
//   node --experimental-strip-types --no-warnings tools/build-smh-site.mjs
import { mkdirSync, readFileSync, writeFileSync } from 'node:fs'
import { register } from 'node:module'
import { dirname, join } from 'node:path'
import { fileURLToPath, pathToFileURL } from 'node:url'

const here = dirname(fileURLToPath(import.meta.url))
register(pathToFileURL(join(here, 'smh-loader.mjs')).href)
const { WALKS, GROUPS } = await import(pathToFileURL(join(here, '..', 'src', 'smh', 'walks', 'index.ts')).href)
const out = join(here, '..', '..', 'site', 'guide')
mkdirSync(out, { recursive: true })

// The words of a step, read the way a first-time reader would see them.
// An earlier answer (the film picked, the series found) reads as a stand-in.
const standIn = { label: 'the film', value: '', name: 'the series', data: { title: 'The film' } }
const ctx = { answers: new Proxy({}, { get: (_, k) => (typeof k === 'string' ? standIn : undefined) }), mode: 'advanced', watch: false, admin: true }
const say = (x) => {
  try {
    return typeof x === 'function' ? x(ctx) : (x ?? '')
  } catch {
    return '' // words made from an earlier answer (a film's name): left out
  }
}
const esc = (s) => String(s).replace(/[&<>"]/g, (c) => ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;' })[c])

function card(d) {
  const rows = d.options
    .map((o) => `<tr><th>${esc(o.name)}</th><td><span class="good">${esc(o.good)}</span>${o.bad ? `<span class="bad">${esc(o.bad)}</span>` : ''}${o.cost ? `<span class="cost">Costs: ${esc(o.cost)}</span>` : ''}</td></tr>`)
    .join('')
  return `<div class="card"><div class="ct">${esc(d.title)}</div><table>${rows}</table>${d.recommend ? `<p class="rec">★ ${esc(d.recommend)}</p>` : ''}<p class="later">↻ ${esc(d.later)}</p></div>`
}

function steps(list) {
  const items = []
  for (const s of list) {
    if (s.kind === 'do') continue // something Usherr does by itself
    const title = say(s.title)
    const text = say(s.text)
    if (!title && !text) continue
    const maybe = s.when ? '<span class="maybe">If it applies</span> ' : ''
    let extra = ''
    if (s.kind === 'ask') {
      if (s.card) extra += card(s.card)
      const choices = typeof s.choices === 'function' ? say(s.choices) : s.choices
      if (Array.isArray(choices) && !s.card) extra += `<ul class="choices">${choices.map((c) => `<li>${esc(c.label)}</li>`).join('')}</ul>`
      if (s.search) extra += '<p class="hint">You type a name and pick it from the list.</p>'
    }
    if (s.kind === 'done') {
      if (s.shortcut) extra += `<p class="short">${esc(say(s.shortcut))}</p>`
    }
    items.push(`<li>${maybe}<b>${esc(title)}</b>${text ? `<p>${esc(text)}</p>` : ''}${extra}</li>`)
  }
  return `<ol class="steps">${items.join('')}</ol>`
}

// The look: the Start page's (GitHub's colours, light or dark).
const startCss = readFileSync(join(here, '..', '..', 'site', 'start', 'index.html'), 'utf8').match(/<style>([\s\S]*?)<\/style>/)[1]
const css =
  startCss +
  `
.steps { padding-left: 22px; } .steps > li { margin: 14px 0; } .steps p { margin: 4px 0; color: var(--muted); }
.maybe { font-size: 0.72rem; border: 1px solid var(--line); border-radius: 2em; padding: 0 7px; color: var(--muted); }
.card .rec { color: var(--text); margin: 8px 0 0; } .card .later { color: var(--ok); margin: 6px 0 0; }
.card .good::before { content: '+ '; color: var(--ok); font-weight: 600; } .card .bad::before { content: '− '; color: var(--bad); font-weight: 600; }
.card .good, .card .bad, .card .cost { display: block; } .card .cost { color: var(--muted); }
.short { background: var(--panel-2); border: 1px solid var(--line); border-radius: 6px; padding: 8px 12px; }
.walks { list-style: none; padding: 0; display: grid; gap: 10px; } .walks a { display: block; padding: 12px 14px; border: 1px solid var(--line); border-radius: 6px; } .walks small { display: block; color: var(--muted); }
.crumb { margin: 24px 0 0; font-size: 0.9rem; } .admin { font-size: 0.72rem; color: var(--muted); border: 1px solid var(--line); border-radius: 2em; padding: 0 7px; margin-left: 6px; }
`
const page = (title, body) => `<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>${esc(title)} · Usherr</title>
<link rel="icon" href="../start/icon.png">
<style>${css}</style>
</head>
<body><div class="wrap">${body}
<footer>Made from Usherr’s own Show Me How walkthroughs. In the app, <b>Show me how</b> in the top bar walks you through each of these on the real screens.</footer>
</div></body>
</html>
`

const needs = (w) => (w.admin ? '<span class="admin">Administrators</span>' : '')
for (const w of WALKS) {
  const simple = steps(w.simple)
  const adv = w.advanced ? `<h2>Advanced: every option</h2>${steps(w.advanced)}` : ''
  writeFileSync(
    join(out, `${w.id}.html`),
    page(
      w.title,
      `<p class="crumb"><a href="./">← Show me how</a></p><header style="text-align:left;padding:16px 0 0"><h1>${esc(w.title)}${needs(w)}</h1><p>${esc(w.blurb)}</p></header>${w.advanced ? '<h2>Simple</h2>' : ''}${simple}${adv}`,
    ),
  )
}
const list = GROUPS.map((g) => {
  const here = WALKS.filter((w) => w.group === g.id)
  if (!here.length) return ''
  return `<h2>${esc(g.label())}</h2><ul class="walks">${here.map((w) => `<li><a href="${w.id}.html"><b>${esc(w.title)}</b>${needs(w)}<small>${esc(w.blurb)}</small></a></li>`).join('')}</ul>`
}).join('')
writeFileSync(
  join(out, 'index.html'),
  page(
    'Show me how',
    `<p class="crumb"><a href="../start/">← Start page</a></p><header><h1>Show me how</h1><p>Everything you can do with Usherr, step by step. In the app, the same walkthroughs light up the real buttons for you.</p></header>${list}`,
  ),
)
console.log(`smh site: ${WALKS.length} walkthroughs written to site/guide`)
