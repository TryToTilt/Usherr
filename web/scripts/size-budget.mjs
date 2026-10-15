// Home fork: keep the app light. The build fails when the code every page
// needs grows past its budget, so a change can't make the app slow to open
// without anyone noticing. Pages loaded on demand aren't counted.
import { readdirSync, readFileSync } from 'node:fs'
import { join } from 'node:path'
import { gzipSync } from 'node:zlib'

const dir = new URL('../dist/assets/', import.meta.url).pathname.replace(/^\/([A-Za-z]:)/, '$1')
const budgets = [
  { what: 'main code', match: /^index-.*\.js$/, kb: 160 }, // compressed
  { what: 'main styles', match: /^index-.*\.css$/, kb: 45 },
]
let over = false
for (const b of budgets) {
  for (const f of readdirSync(dir).filter((n) => b.match.test(n))) {
    const kb = gzipSync(readFileSync(join(dir, f)), { level: 9 }).length / 1024
    const ok = kb <= b.kb
    over ||= !ok
    console.log(`${ok ? 'ok  ' : 'OVER'} ${b.what}: ${kb.toFixed(0)} KB compressed (budget ${b.kb} KB) ${f}`)
  }
}
if (over) {
  console.error('The app grew past its size budget: load the new part on demand, or raise the budget on purpose.')
  process.exit(1)
}
