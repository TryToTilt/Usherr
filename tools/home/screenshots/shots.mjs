// Home fork: takes the pictures for the documentation (docs/home/images)
// from a demo instance of Mediarium, never a real household's. Each picture is
// listed in shots.json: the page, desktop or phone, what to wait for, what to
// show (the whole screen, or one card), and what to point out.
//
// Run it with tools/home/screenshots/run.sh, which starts this in the official
// Playwright image next to the demo instance (see docs/home/screenshots.md).
//
//   BASE=http://localhost:8264 DEMO_USER=alex DEMO_PASS=... node shots.mjs [name...]
import { chromium } from 'playwright'
import { mkdirSync, readFileSync } from 'node:fs'
import { dirname, join } from 'node:path'
import { fileURLToPath } from 'node:url'

const here = dirname(fileURLToPath(import.meta.url))
const out = process.env.OUT ?? join(here, '..', '..', '..', 'docs', 'home', 'images')
const base = process.env.BASE ?? 'http://localhost:8264'
const only = new Set(process.argv.slice(2))
const shots = JSON.parse(readFileSync(join(here, 'shots.json'), 'utf8')).filter((s) => !only.size || only.has(s.name))
mkdirSync(out, { recursive: true })

const VIEWPORTS = {
  desktop: { viewport: { width: 1440, height: 900 }, deviceScaleFactor: 1 },
  phone: { viewport: { width: 390, height: 844 }, deviceScaleFactor: 2, isMobile: true, hasTouch: true },
}

// A heading that starts with this text (card headings also hold badges), or
// else an element with exactly this text.
function heading(page, text) {
  const esc = text.replace(/[.*+?^${}()|[\]\\]/g, '\\$&')
  return page.getByRole('heading', { name: new RegExp(`^${esc}(\\s|$)`) }).or(page.getByText(text, { exact: true })).first()
}

// Callouts: a box around each element named, with a number, in the brand teal.
async function pointOut(page, targets) {
  for (const [i, text] of targets.entries()) {
    const el = page.getByText(text, { exact: true }).first()
    if (!(await el.count())) {
      console.warn(`  callout not found: ${text}`)
      continue
    }
    await el.evaluate((node, n) => {
      const target = node.closest('button, a, label, .card, section, li') ?? node
      const r = target.getBoundingClientRect()
      const box = document.createElement('div')
      box.className = 'docs-callout'
      Object.assign(box.style, {
        position: 'fixed', left: `${r.left - 6}px`, top: `${r.top - 6}px`, width: `${r.width + 12}px`, height: `${r.height + 12}px`,
        border: '3px solid #34d1bf', borderRadius: '12px', boxShadow: '0 0 0 4px rgba(52,209,191,.25)', zIndex: 99999, pointerEvents: 'none',
      })
      const tag = document.createElement('span')
      tag.textContent = String(n)
      Object.assign(tag.style, {
        position: 'absolute', top: '-14px', left: '-14px', width: '26px', height: '26px', borderRadius: '50%', background: '#34d1bf',
        color: '#0b0d18', font: '800 14px/26px system-ui, sans-serif', textAlign: 'center',
      })
      box.appendChild(tag)
      document.body.appendChild(box)
    }, i + 1)
  }
}

const browser = await chromium.launch()
let failed = 0
// Who takes the picture: the administrator, or "viewer" (a household member,
// for the pages everyone sees).
const ACCOUNTS = {
  admin: [process.env.DEMO_USER, process.env.DEMO_PASS],
  viewer: [process.env.VIEWER_USER, process.env.VIEWER_PASS],
}
for (const vpName of ['desktop', 'phone']) for (const as of ['admin', 'viewer']) {
  const list = shots.filter((s) => (s.viewport ?? 'desktop') === vpName && (s.as ?? 'admin') === as)
  if (!list.length) continue
  const [username, password] = ACCOUNTS[as]
  if (!username) throw new Error(`no ${as} account given for: ${list.map((s) => s.name).join(', ')}`)
  const context = await browser.newContext({ ...VIEWPORTS[vpName], colorScheme: 'dark', baseURL: base })
  const login = await context.request.post('/api/auth/login', { data: { username, password } })
  if (!login.ok()) throw new Error(`sign-in to the demo as ${as} failed: ${login.status()}`)
  const page = await context.newPage()
  page.on('pageerror', (e) => console.log(`
  page error: ${e.message}`)) // a crash shows up here
  page.on('console', (m) => m.type() === 'error' && console.log(`
  console error: ${m.text().slice(0, 300)}`))
  await page.addInitScript(() => {
    try {
      localStorage.setItem('mediarium-theme', 'dark')
      // The first-visit tour stays away, except on pictures of it (?docs-tour).
      for (let id = 1; id <= 50; id++) {
        if (location.search.includes('docs-tour')) localStorage.removeItem(`mediarium.tour.done.${id}`)
        else localStorage.setItem(`mediarium.tour.done.${id}`, '1')
      }
    } catch {}
  })
  for (const s of list) {
    process.stdout.write(`${s.name} (${vpName}${as === 'viewer' ? ', viewer' : ''}) … `)
    try {
      await page.goto(s.path, { waitUntil: 'domcontentloaded' })
      if (s.waitFor) await page.getByText(s.waitFor, { exact: s.exact ?? false }).first().waitFor({ timeout: 15000 })
      for (const step of s.before ?? []) {
        // An optional step (a question that may or may not appear) is skipped when it isn't there.
        const opt = step.optional ? { timeout: 1500 } : {}
        try {
          if (step.click) await page.getByText(step.click, { exact: true }).first().click(opt)
          if (step.press) await page.locator(step.press).first().click(opt) // by CSS selector
        } catch (e) {
          if (!step.optional) throw e
        }
        if (step.scroll) await page.getByText(step.scroll, { exact: true }).first().scrollIntoViewIfNeeded()
        await page.waitForTimeout(step.wait ?? 400)
      }
      await page.waitForTimeout(s.settle ?? 1500) // posters and fades
      await pointOut(page, s.callouts ?? [])
      const file = join(out, `${s.name}.png`)
      if (s.element) {
        // An element by CSS selector, with room under it for an open menu.
        const box = await page.locator(s.element).first().boundingBox()
        if (!box) throw new Error(`no element ${s.element}`)
        const pad = 8
        await page.screenshot({ path: file, clip: { x: Math.max(0, box.x - pad), y: Math.max(0, box.y - pad), width: box.width + pad * 2, height: box.height + pad * 2 + (s.below ?? 0) } })
      } else if (s.card) {
        // The card (or section) around the named heading.
        const found = await heading(page, s.card).evaluate((node) => {
          const box = node.closest('.card, section, .settings-card, article') ?? node.parentElement
          box?.setAttribute('data-docs-shot', '1')
          return !!box
        })
        if (!found) throw new Error(`no card around ${s.card}`)
        const card = page.locator('[data-docs-shot]')
        // A card taller than the window: make the window tall enough first,
        // since the app scrolls inside its content area.
        const size = page.viewportSize()
        const h = (await card.boundingBox())?.height ?? 0
        if (h > size.height - 160) {
          await page.setViewportSize({ width: size.width, height: Math.min(Math.ceil(h) + 260, 9000) })
          await page.waitForTimeout(800)
        }
        await card.scrollIntoViewIfNeeded()
        await card.screenshot({ path: file })
        if (h > size.height - 160) await page.setViewportSize(size)
        await card.evaluate((n) => n.removeAttribute('data-docs-shot'))
      } else {
        if (s.fullPage) {
          // The app scrolls inside its content area, not the window: make the
          // window as tall as that content for the picture.
          const size = page.viewportSize()
          const tall = await page.evaluate(() => {
            const scrollers = [...document.querySelectorAll('*')].filter((e) => e.scrollHeight > e.clientHeight + 40 && /(auto|scroll)/.test(getComputedStyle(e).overflowY))
            const main = scrollers.sort((a, b) => b.scrollHeight - a.scrollHeight)[0]
            return main ? main.scrollHeight - main.clientHeight + window.innerHeight : document.documentElement.scrollHeight
          })
          await page.setViewportSize({ width: size.width, height: Math.min(tall, 9000) })
          await page.waitForTimeout(1200)
          await page.screenshot({ path: file })
          await page.setViewportSize(size)
        } else {
          await page.screenshot({ path: file })
        }
      }
      await page.evaluate(() => document.querySelectorAll('.docs-callout').forEach((n) => n.remove()))
      console.log('ok')
    } catch (e) {
      failed++
      console.log(`FAILED: ${e.message.split('\n')[0]}`)
      await page.screenshot({ path: join(out, `_failed-${s.name}.png`) }).catch(() => undefined) // what the browser saw
    }
  }
  await context.close()
}
await browser.close()
if (failed) {
  console.error(`${failed} picture(s) failed`)
  process.exit(1)
}
