import { expect, test, type Page } from '@playwright/test'
import { readFileSync } from 'node:fs'
import { crc32 } from 'node:zlib'
import { expectNoErrors, watchErrors } from './helpers'

// The ebook reader's page turn, on a small EPUB made here (own text, an embedded
// font and a picture) and served in place of a real book, so no library is needed.

function zip(files: [string, Buffer | string][]): Buffer {
  const parts: Buffer[] = []
  const central: Buffer[] = []
  let at = 0
  for (const [name, content] of files) {
    const data = Buffer.from(content)
    const n = Buffer.from(name)
    const crc = crc32(data)
    const head = Buffer.alloc(30)
    head.writeUInt32LE(0x04034b50, 0)
    head.writeUInt16LE(20, 4)
    head.writeUInt32LE(crc, 14)
    head.writeUInt32LE(data.length, 18)
    head.writeUInt32LE(data.length, 22)
    head.writeUInt16LE(n.length, 26)
    parts.push(head, n, data)
    const dir = Buffer.alloc(46)
    dir.writeUInt32LE(0x02014b50, 0)
    dir.writeUInt16LE(20, 4)
    dir.writeUInt16LE(20, 6)
    dir.writeUInt32LE(crc, 16)
    dir.writeUInt32LE(data.length, 20)
    dir.writeUInt32LE(data.length, 24)
    dir.writeUInt16LE(n.length, 28)
    dir.writeUInt32LE(at, 42)
    central.push(dir, n)
    at += 30 + n.length + data.length
  }
  const size = central.reduce((s, b) => s + b.length, 0)
  const end = Buffer.alloc(22)
  end.writeUInt32LE(0x06054b50, 0)
  end.writeUInt16LE(files.length, 8)
  end.writeUInt16LE(files.length, 10)
  end.writeUInt32LE(size, 12)
  end.writeUInt32LE(at, 16)
  return Buffer.concat([...parts, ...central, end])
}

const LINES = [
  'The lamp over the harbour office had been burning since before the tide turned, and nobody could say who had lit it.',
  'Marit walked the length of the quay with her collar up, counting the boats the way her father used to count them.',
  'There were eleven that morning, which was one more than the ledger allowed for.',
  'Inside the office the stove ticked as it cooled, and the clerk wrote slowly, forming every letter as though it would be inspected.',
  'Outside, the fog was lifting off the water in long slow pieces, like someone folding away a sheet.',
]
const text = Array.from({ length: 60 }, (_, i) => `<p>${LINES[i % 5]} ${LINES[(i + 2) % 5]}</p>`).join('')
// A one-pixel PNG, stretched: enough to prove a picture in a book survives.
const PIXEL = Buffer.from('iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mNkYPhfDwAChwGA60e6kgAAAABJRU5ErkJggg==', 'base64')
const chapter = (title: string, body: string) =>
  `<?xml version="1.0" encoding="utf-8"?><html xmlns="http://www.w3.org/1999/xhtml" xmlns:epub="http://www.idpf.org/2007/ops"><head><title>${title}</title><link rel="stylesheet" type="text/css" href="s.css"/></head><body><h1>${title}</h1>${body}</body></html>`

const sampleEpub = (rtl = false) =>
  zip([
    ['mimetype', 'application/epub+zip'],
    ['META-INF/container.xml', '<?xml version="1.0"?><container version="1.0" xmlns="urn:oasis:names:tc:opendocument:xmlns:container"><rootfiles><rootfile full-path="b.opf" media-type="application/oebps-package+xml"/></rootfiles></container>'],
    [
      'b.opf',
      `<?xml version="1.0"?><package xmlns="http://www.idpf.org/2007/opf" version="3.0" unique-identifier="i"><metadata xmlns:dc="http://purl.org/dc/elements/1.1/"><dc:identifier id="i">urn:usherr:turn</dc:identifier><dc:title>Turn test</dc:title><dc:language>en</dc:language></metadata><manifest><item id="n" href="n.xhtml" media-type="application/xhtml+xml" properties="nav"/><item id="s" href="s.css" media-type="text/css"/><item id="f" href="f.woff2" media-type="font/woff2"/><item id="p" href="p.png" media-type="image/png"/><item id="a" href="a.xhtml" media-type="application/xhtml+xml"/><item id="b" href="b.xhtml" media-type="application/xhtml+xml"/></manifest><spine${rtl ? ' page-progression-direction="rtl"' : ''}><itemref idref="a"/><itemref idref="b"/></spine></package>`,
    ],
    ['n.xhtml', '<?xml version="1.0"?><html xmlns="http://www.w3.org/1999/xhtml" xmlns:epub="http://www.idpf.org/2007/ops"><head><title>n</title></head><body><nav epub:type="toc"><ol><li><a href="a.xhtml">One</a></li><li><a href="b.xhtml">Two</a></li></ol></nav></body></html>'],
    ['s.css', '@font-face { font-family: "Sample"; src: url(f.woff2) format("woff2"); } h1 { font-family: "Sample", serif; } p { margin: 0 0 0.6em; } img { width: 60%; height: 80px; display: block; }'],
    ['f.woff2', readFileSync('node_modules/@fontsource/abril-fatface/files/abril-fatface-latin-400-normal.woff2')],
    ['p.png', PIXEL],
    [
      'a.xhtml',
      chapter(
        'One',
        `<p><img src="p.png" alt=""/></p><p>A line with a mark<a id="r1" epub:type="noteref" href="#fn1">1</a>, one to another chapter<sup><a id="r2" href="b.xhtml#fn2">2</a></sup> and a <a id="r3" href="b.xhtml">plain link</a>.</p>${text}<aside id="fn1" epub:type="footnote"><p><a href="#r1">1</a> The ledger was kept in pencil.</p></aside>`,
      ),
    ],
    ['b.xhtml', chapter('Two', `${text}<p id="fn2"><a href="a.xhtml#r2">2</a> Her father's hand, not hers.</p>`)],
  ])

const BOOK = 987654

// A place kept as the server would: what the reader last saved, for when it reopens.
function keepProgress(page: Page) {
  const kept: { saved: unknown } = { saved: null }
  return {
    kept,
    route: () =>
      page.route(`**/api/books/${BOOK}/progress*`, (r) => {
        if (r.request().method() !== 'GET') {
          kept.saved = { ...(r.request().postDataJSON() as object), updatedAt: '2026-01-01T00:00:00Z' }
          return r.fulfill({ json: {} })
        }
        return kept.saved ? r.fulfill({ json: kept.saved }) : r.fulfill({ status: 404, json: {} })
      }),
  }
}

async function openBook(page: Page, prefs: Record<string, unknown> = {}, rtl = false, progress?: () => Promise<void>) {
  await page.route(`**/api/books/${BOOK}`, (r) =>
    r.fulfill({
      json: { id: BOOK, olKey: 'OLTURN', title: 'Turn test', author: 'Sample', addedAt: '2026-01-01T00:00:00Z', ebook: { status: 'downloaded', format: 'epub' }, audiobook: { status: 'none' } },
    }),
  )
  await page.route(`**/api/books/${BOOK}/read*`, (r) => r.fulfill({ body: sampleEpub(rtl), contentType: 'application/epub+zip' }))
  if (progress) await progress()
  else await page.route(`**/api/books/${BOOK}/progress*`, (r) => (r.request().method() === 'GET' ? r.fulfill({ status: 404, json: {} }) : r.fulfill({ json: {} })))
  await page.request.put('/api/modules', { data: { ebooks: true } })
  await page.goto('/')
  await page.evaluate((p) => localStorage.setItem('mediarium.reader', JSON.stringify({ size: 105, theme: 'light', font: 'serif', ...p })), prefs)
  await page.goto(`/bookshelf/read/${BOOK}`)
  await expect(page.frameLocator('.rdr-viewer iframe').locator('h1')).toBeVisible({ timeout: 30_000 })
}

// Where the book is: the chapter and how far along it.
const place = (page: Page) =>
  page.evaluate(() => {
    const c = document.querySelector('.rdr-viewer .epub-container')
    const f = document.querySelector<HTMLIFrameElement>('.rdr-viewer iframe') // none for a moment between chapters
    return `${f?.contentDocument?.querySelector('h1')?.textContent}@${Math.round(c?.scrollLeft ?? 0)}`
  })
const curlShown = (page: Page) => page.evaluate(() => (document.querySelector('canvas.rdr-curl') as HTMLElement | null)?.style.display === 'block')

// The curl's pictures of the neighbouring pages are made a moment after a page settles.
async function curlReady(page: Page) {
  await expect(page.locator('canvas.rdr-curl')).toHaveCount(1)
  await page.waitForTimeout(1200)
}

async function pageBox(page: Page) {
  const box = await page.locator('.rdr-viewer').boundingBox()
  if (!box) throw new Error('no page')
  return box
}

async function settled(page: Page) {
  await expect.poll(() => curlShown(page)).toBe(false)
}

const frame = (page: Page) => page.frameLocator('.rdr-viewer iframe')

// The first words on the page: what a reader would call the place.
const firstWords = (page: Page) =>
  page.evaluate(() => {
    const c = document.querySelector('.rdr-viewer .epub-container') as HTMLElement
    const f = document.querySelector('.rdr-viewer iframe') as HTMLIFrameElement
    const doc = f.contentDocument as Document
    const fr = f.getBoundingClientRect()
    const box = c.getBoundingClientRect()
    const walk = doc.createTreeWalker(doc.body, NodeFilter.SHOW_TEXT)
    for (let n = walk.nextNode(); n; n = walk.nextNode()) {
      const text = n.textContent ?? ''
      if (!text.trim()) continue
      const r = doc.createRange()
      for (let i = text.search(/\S/); i >= 0; i = text.indexOf(' ', i + 1)) {
        const at = i === 0 || text[i] !== ' ' ? i : i + 1
        r.setStart(n, at)
        r.setEnd(n, Math.min(text.length, at + 1))
        const b = r.getBoundingClientRect()
        if (b.width > 0 && b.left + fr.left >= box.left && b.left + fr.left < box.right) return text.slice(at, at + 40)
      }
    }
    return ''
  })

// The reader shows the book only once it is on its place.
const opened = async (page: Page) => {
  await expect(page.locator('.rdr-viewer iframe')).toHaveCount(1, { timeout: 30_000 })
  await expect(page.locator('.rdr-opening')).toHaveCount(0, { timeout: 15_000 })
}

// How far the curl's picture of the page is from the live page: the share of pixels
// clearly different, with a drag only just begun so that the sheet covers next to nothing.
async function pictureDiff(page: Page) {
  const b = await pageBox(page)
  const shot = async () => (await page.screenshot({ clip: { x: b.x + b.width * 0.1, y: b.y + 10, width: b.width * 0.5, height: b.height - 20 } })).toString('base64')
  const live = await shot()
  const y = b.y + b.height * 0.9
  await page.mouse.move(b.x + b.width - 6, y)
  await page.mouse.down()
  await page.mouse.move(b.x + b.width - 18, y, { steps: 2 })
  await page.waitForTimeout(500)
  expect(await curlShown(page)).toBe(true)
  const picture = await shot()
  await page.mouse.move(b.x + b.width - 4, y)
  await page.mouse.up()
  await settled(page)
  return page.evaluate(
    async ([x, z]) => {
      const load = async (s: string) => {
        const i = new Image()
        i.src = `data:image/png;base64,${s}`
        await i.decode()
        const c = document.createElement('canvas')
        c.width = i.width
        c.height = i.height
        const g = c.getContext('2d') as CanvasRenderingContext2D
        g.drawImage(i, 0, 0)
        return g.getImageData(0, 0, i.width, i.height).data
      }
      const [p, q] = [await load(x), await load(z)]
      let off = 0
      for (let n = 0; n < p.length; n += 4) if (Math.abs(p[n] - q[n]) > 90) off++
      return off / (p.length / 4)
    },
    [live, picture],
  )
}

async function openSettings(page: Page) {
  await page.getByRole('button', { name: 'Text and colours' }).click()
  return page.locator('.rdr-settings')
}

const storedPrefs = (page: Page) => page.evaluate(() => JSON.parse(localStorage.getItem('mediarium.reader') ?? '{}'))

test.describe('the ebook page turn', () => {
  test.use({ serviceWorkers: 'block' })

  test('a drag past halfway turns the page, and it curls on the way', async ({ page }) => {
    const errors = watchErrors(page)
    await openBook(page)
    await curlReady(page)
    const start = await place(page)
    const b = await pageBox(page)
    const y = b.y + b.height * 0.8
    await page.mouse.move(b.x + b.width - 8, y)
    await page.mouse.down()
    await page.mouse.move(b.x + b.width * 0.8, y, { steps: 5 })
    await page.waitForTimeout(200)
    expect(await curlShown(page)).toBe(true)
    expect(await place(page)).toBe(start) // the live page hasn't moved yet
    await page.mouse.move(b.x + b.width * 0.3, y, { steps: 8 })
    await page.waitForTimeout(300) // a slow release: position decides, not speed
    await page.mouse.up()
    await expect.poll(() => place(page)).not.toBe(start)
    await settled(page)
    await expectNoErrors(errors)
  })

  test('a drag brought back falls back, with the page where it was', async ({ page }) => {
    await openBook(page)
    await curlReady(page)
    const start = await place(page)
    const b = await pageBox(page)
    const y = b.y + b.height * 0.5
    await page.mouse.move(b.x + b.width - 8, y)
    await page.mouse.down()
    await page.mouse.move(b.x + b.width * 0.35, y, { steps: 8 })
    await page.waitForTimeout(150)
    await page.mouse.move(b.x + b.width * 0.9, y, { steps: 8 })
    await page.waitForTimeout(300)
    expect(await curlShown(page)).toBe(true)
    await page.mouse.up()
    await page.waitForTimeout(100)
    await settled(page)
    expect(await place(page)).toBe(start)
  })

  test('a flick from near the edge turns the page', async ({ page }) => {
    await openBook(page)
    await curlReady(page)
    const start = await place(page)
    const b = await pageBox(page)
    const y = b.y + b.height * 0.2
    // Sent from inside the page: a slow test machine would make real mouse moves a slow drag.
    await page.evaluate(
      async ({ from, to, y }) => {
        const grip = document.querySelector('.rdr-grip.right') as HTMLElement
        const send = (type: string, x: number) => grip.dispatchEvent(new PointerEvent(type, { bubbles: true, pointerId: 7, isPrimary: true, pointerType: 'touch', button: 0, clientX: x, clientY: y }))
        send('pointerdown', from)
        await new Promise((r) => setTimeout(r, 16))
        send('pointermove', to)
        send('pointerup', to)
      },
      { from: b.x + b.width - 8, to: b.x + b.width * 0.7, y },
    )
    await expect.poll(() => place(page)).not.toBe(start)
    await settled(page)
  })

  test('a tap and the arrow keys play a whole turn, both ways, across a chapter too', async ({ page }) => {
    const errors = watchErrors(page)
    await openBook(page)
    await curlReady(page)
    const start = await place(page)
    const b = await pageBox(page)
    await page.mouse.click(b.x + b.width - 8, b.y + b.height / 2)
    await expect.poll(() => curlShown(page)).toBe(true)
    await expect.poll(() => place(page)).not.toBe(start)
    await settled(page)
    await page.waitForTimeout(500)
    const second = await place(page)
    await page.keyboard.press('ArrowRight')
    await expect.poll(() => place(page)).not.toBe(second)
    await settled(page)
    await page.waitForTimeout(500)
    await page.keyboard.press('ArrowLeft')
    await expect.poll(() => place(page)).toBe(second)
    await settled(page)
    // A drag back from the left edge.
    await page.waitForTimeout(500)
    const y = b.y + b.height * 0.75
    await page.mouse.move(b.x + 8, y)
    await page.mouse.down()
    await page.mouse.move(b.x + b.width * 0.75, y, { steps: 10 })
    await page.waitForTimeout(250)
    await page.mouse.up()
    await expect.poll(() => place(page)).toBe(start)
    await settled(page)
    // The first page of the next chapter comes from a look ahead, not the open page.
    for (let i = 0; i < 60 && !(await place(page)).startsWith('Two'); i++) {
      const before = await place(page)
      await page.keyboard.press('PageDown')
      await expect.poll(() => place(page)).not.toBe(before)
      await settled(page)
    }
    expect(await place(page)).toBe('Two@0')
    await expectNoErrors(errors)
  })

  test('with the setting on None a page turns with no animation', async ({ page }) => {
    await openBook(page)
    await curlReady(page)
    await page.getByRole('button', { name: 'Text and colours' }).click()
    await page.locator('.rdr-settings').getByRole('button', { name: 'None', exact: true }).click()
    await expect(page.locator('canvas.rdr-curl')).toHaveCount(0)
    await page.getByRole('button', { name: 'Text and colours' }).click()
    const start = await place(page)
    const b = await pageBox(page)
    await page.mouse.click(b.x + b.width - 8, b.y + b.height / 2)
    await expect.poll(() => place(page)).not.toBe(start)
    await expect(page.locator('canvas.rdr-curl')).toHaveCount(0)
    const stored = await page.evaluate(() => JSON.parse(localStorage.getItem('mediarium.reader') ?? '{}'))
    expect(stored.effect).toBe('none')
  })

  test('a system asking for less motion gets no animation, unless the curl is chosen', async ({ page }) => {
    await page.emulateMedia({ reducedMotion: 'reduce' })
    await openBook(page)
    await page.waitForTimeout(1000)
    await expect(page.locator('canvas.rdr-curl')).toHaveCount(0)
    await page.getByRole('button', { name: 'Text and colours' }).click()
    await expect(page.locator('.rdr-settings').getByRole('button', { name: 'None', exact: true })).toHaveClass(/primary/)
    await page.locator('.rdr-settings').getByRole('button', { name: 'Curl', exact: true }).click()
    await expect(page.locator('canvas.rdr-curl')).toHaveCount(1)
  })

  test('a right-to-left book turns from the left edge', async ({ page }) => {
    await openBook(page, {}, true)
    await curlReady(page)
    const start = await place(page)
    const b = await pageBox(page)
    const y = b.y + b.height * 0.8
    await page.mouse.move(b.x + 8, y)
    await page.mouse.down()
    await page.mouse.move(b.x + b.width * 0.75, y, { steps: 10 })
    await page.waitForTimeout(300)
    expect(await curlShown(page)).toBe(true)
    await page.mouse.up()
    await expect.poll(() => place(page)).not.toBe(start)
    await settled(page)
    // And it was forward: the right arrow key comes back.
    await page.waitForTimeout(500)
    await page.keyboard.press('ArrowRight')
    await expect.poll(() => place(page)).toBe(start)
  })

  test('a sideways finger drag in the middle of the page turns it', async ({ page }, info) => {
    test.skip(info.project.name !== 'app-phone', 'a touch screen')
    await openBook(page)
    await curlReady(page)
    const start = await place(page)
    const b = await pageBox(page)
    const cdp = await page.context().newCDPSession(page)
    const touch = (type: string, x: number, y: number) => cdp.send('Input.dispatchTouchEvent', { type, touchPoints: type === 'touchEnd' ? [] : [{ x, y }] } as never).catch(() => undefined)
    const y = b.y + b.height * 0.6
    const x = b.x + b.width * 0.6
    void touch('touchStart', x, y)
    await page.waitForTimeout(50)
    for (let i = 1; i <= 8; i++) {
      void touch('touchMove', x - i * (b.width * 0.06), y)
      await page.waitForTimeout(30)
    }
    await expect.poll(() => curlShown(page)).toBe(true)
    await page.waitForTimeout(300)
    void touch('touchEnd', 0, 0)
    await expect.poll(() => place(page)).not.toBe(start)
    await settled(page)
  })

  test('dragging on the text selects it and leaves the page alone', async ({ page }, info) => {
    test.skip(info.project.name !== 'app', 'mouse selection')
    await openBook(page)
    await curlReady(page)
    const start = await place(page)
    const line = await page.frameLocator('.rdr-viewer iframe').locator('p').nth(3).boundingBox()
    if (!line) throw new Error('no text')
    const y = line.y + 8
    const from = line.x + 30
    await page.mouse.move(from, y)
    await page.mouse.down()
    // Chromium never answers a mouse move over text in a sandboxed frame, though it does act on it.
    void page.mouse.move(from + 60, y).catch(() => undefined)
    await page.waitForTimeout(100)
    void page.mouse.move(from + 160, y).catch(() => undefined)
    await page.waitForTimeout(100)
    void page.mouse.up().catch(() => undefined)
    await page.waitForTimeout(300)
    const picked = await page.evaluate(() => (document.querySelector('.rdr-viewer iframe') as HTMLIFrameElement).contentWindow?.getSelection()?.toString() ?? '')
    expect(picked.trim().length).toBeGreaterThan(3)
    expect(await curlShown(page)).toBe(false)
    expect(await place(page)).toBe(start)
    // The words can be highlighted, as ever.
    await expect(page.locator('.rdr-pick')).toBeVisible()
  })

  test('a book reopens on the same page, with the curl on or off', async ({ page }) => {
    test.setTimeout(150_000)
    const { kept, route } = keepProgress(page)
    await openBook(page, {}, false, route)
    for (const effect of ['curl', 'none']) {
      await page.evaluate((e) => localStorage.setItem('mediarium.reader', JSON.stringify({ ...JSON.parse(localStorage.getItem('mediarium.reader') ?? '{}'), effect: e })), effect)
      for (let i = 0; i < 2; i++) {
        await page.reload()
        await opened(page)
        await page.waitForTimeout(1200)
        // The sample repeats its sentences, so the words alone could be the same on two pages.
        const here = async () => `${await place(page)} ${await firstWords(page)}`
        const before = await here()
        await page.keyboard.press('ArrowRight')
        await expect.poll(here).not.toBe(before)
        await settled(page)
        const left = await here()
        await page.waitForTimeout(2200) // the place is saved a moment after a turn
        expect(kept.saved).not.toBeNull()
        await page.reload()
        await opened(page)
        await page.waitForTimeout(800)
        expect(await here()).toBe(left)
      }
    }
  })

  // Why a book reopened a page early: the chapter's name arrived after the place was
  // found, the bar above grew, and the shorter page moved every word after it.
  test('the page keeps its height from the moment the book opens', async ({ page }) => {
    await page.addInitScript(() => {
      const seen = new Set<number>()
      ;(window as unknown as { heights: Set<number> }).heights = seen
      const look = () => {
        const h = document.querySelector('.rdr-viewer')?.clientHeight
        if (h) seen.add(h)
        requestAnimationFrame(look)
      }
      look()
    })
    await openBook(page)
    await curlReady(page)
    await page.keyboard.press('ArrowRight')
    await settled(page)
    await page.waitForTimeout(500)
    expect(await page.evaluate(() => [...(window as unknown as { heights: Set<number> }).heights])).toHaveLength(1)
  })

  test('the settings change the page, stay remembered, and the place stays', async ({ page }) => {
    const errors = watchErrors(page)
    await openBook(page)
    await curlReady(page)
    await page.keyboard.press('ArrowRight')
    await settled(page)
    await page.waitForTimeout(800)
    const word = await firstWords(page)
    const p3 = frame(page).locator('p').nth(5)
    const style = (name: string) => p3.evaluate((el, n) => getComputedStyle(el).getPropertyValue(n), name)
    const panel = await openSettings(page)
    // Line spacing, in rules a book can't beat.
    await panel.locator('.rdr-row', { hasText: 'Line spacing' }).getByRole('button', { name: 'Wide', exact: true }).click()
    await expect.poll(async () => parseFloat(await style('line-height')) / parseFloat(await style('font-size'))).toBeCloseTo(1.85, 1)
    // Margins: new columns, the same words on the screen.
    const pad = () => frame(page).locator('body').evaluate((b) => parseFloat(getComputedStyle(b).paddingLeft))
    const padBefore = await pad()
    await panel.locator('.rdr-row', { hasText: 'Margins' }).getByRole('button', { name: 'Wide', exact: true }).click()
    await expect.poll(pad).toBeGreaterThan(padBefore * 1.5)
    await panel.locator('.rdr-row', { hasText: 'Justify' }).getByRole('button', { name: 'Off', exact: true }).click()
    await expect.poll(() => style('text-align')).toBe('start')
    await panel.locator('.rdr-row', { hasText: 'Justify' }).getByRole('button', { name: 'On', exact: true }).click()
    await expect.poll(() => style('text-align')).toBe('justify')
    await panel.locator('.rdr-row', { hasText: 'Font' }).getByRole('button', { name: 'Typewriter', exact: true }).click()
    await expect.poll(() => style('font-family')).toContain('monospace')
    await panel.getByRole('button', { name: 'Black', exact: true }).click()
    await expect.poll(() => frame(page).locator('body').evaluate((b) => getComputedStyle(b).backgroundColor)).toBe('rgb(0, 0, 0)')
    await page.waitForTimeout(1500)
    const all = await frame(page).locator('body').evaluate((b) => b.textContent ?? '')
    expect(all).toContain(word)
    expect(await page.evaluate(() => document.querySelectorAll('.rdr-viewer iframe').length)).toBe(1)
    const visible = await page.evaluate(
      (w) => {
        const f = document.querySelector('.rdr-viewer iframe') as HTMLIFrameElement
        const doc = f.contentDocument as Document
        const box = (document.querySelector('.rdr-viewer .epub-container') as HTMLElement).getBoundingClientRect()
        const fr = f.getBoundingClientRect()
        const walk = doc.createTreeWalker(doc.body, NodeFilter.SHOW_TEXT)
        for (let n = walk.nextNode(); n; n = walk.nextNode()) {
          const at = (n.textContent ?? '').indexOf(w)
          if (at < 0) continue
          const r = doc.createRange()
          r.setStart(n, at)
          r.setEnd(n, at + 1)
          const b = r.getBoundingClientRect()
          if (b.left + fr.left >= box.left - 2 && b.left + fr.left < box.right) return true
        }
        return false
      },
      word.slice(0, 25),
    )
    expect(visible).toBe(true)
    const stored = await storedPrefs(page)
    expect(stored).toMatchObject({ line: 'wide', margin: 'wide', justify: 'on', font: 'mono', theme: 'black' })
    // Remembered.
    await page.reload()
    await expect(frame(page).locator('h1')).toBeAttached({ timeout: 30_000 })
    await expect.poll(() => frame(page).locator('body').evaluate((b) => parseFloat(getComputedStyle(b).paddingLeft))).toBeGreaterThan(padBefore * 1.5)
    await expect.poll(() => frame(page).locator('p').nth(5).evaluate((el) => getComputedStyle(el).textAlign)).toBe('justify')
    await expectNoErrors(errors)
  })

  test('a setting that moves words makes the next page picture match the live page', async ({ page }) => {
    await openBook(page)
    await curlReady(page)
    const panel = await openSettings(page)
    await panel.locator('.rdr-row', { hasText: 'Margins' }).getByRole('button', { name: 'Wide', exact: true }).click()
    await panel.locator('.rdr-row', { hasText: 'Line spacing' }).getByRole('button', { name: 'Wide', exact: true }).click()
    await page.getByRole('button', { name: 'Text and colours' }).click()
    await curlReady(page)
    await page.waitForTimeout(1500)
    expect(await pictureDiff(page)).toBeLessThan(0.02)
  })

  test('a footnote opens in a card over the page, which stays where it is', async ({ page }) => {
    const errors = watchErrors(page)
    await openBook(page)
    await curlReady(page)
    const start = await place(page)
    await frame(page).locator('#r1').click()
    await expect(page.locator('.rdr-note')).toContainText('The ledger was kept in pencil.')
    await expect(page.locator('.rdr-note')).not.toContainText('1 The') // its own number goes
    expect(await place(page)).toBe(start)
    await page.locator('.rdr-note').getByRole('button', { name: 'Close' }).click()
    await expect(page.locator('.rdr-note')).toHaveCount(0)
    // One in another chapter, closed with Esc.
    await frame(page).locator('#r2').click()
    await expect(page.locator('.rdr-note')).toContainText("Her father's hand, not hers.")
    await page.keyboard.press('Escape')
    await expect(page.locator('.rdr-note')).toHaveCount(0)
    expect(await place(page)).toBe(start)
    // A link that isn't a note still goes there, even under the strip along the page's edge.
    const plain = await frame(page).locator('#r3').boundingBox()
    if (!plain) throw new Error('no link')
    await page.mouse.click(plain.x + plain.width / 2, plain.y + plain.height / 2)
    await expect.poll(() => place(page)).toBe('Two@0')
    await expectNoErrors(errors)
  })

  test('a saved Fade becomes Slide, and the page slides', async ({ page }) => {
    for (const saved of [{ effect: 'fade' }, { turn: 'fade' }]) {
      await openBook(page, saved)
      await page.waitForTimeout(1000)
      await expect(page.locator('canvas.rdr-curl')).toHaveCount(0)
      expect((await storedPrefs(page)).effect).toBe('slide')
      const panel = await openSettings(page)
      await expect(panel.getByRole('button', { name: 'Slide', exact: true })).toHaveClass(/primary/)
      await page.getByRole('button', { name: 'Text and colours' }).click()
      const start = await place(page)
      const box = await pageBox(page)
      const moved = page.evaluate(
        () =>
          new Promise<boolean>((resolve) => {
            const v = document.querySelector('.rdr-viewer') as HTMLElement
            const look = () => (v.style.transform.includes('translateX') ? resolve(true) : requestAnimationFrame(look))
            look()
            setTimeout(() => resolve(false), 3000)
          }),
      )
      await page.mouse.click(box.x + box.width - 8, box.y + box.height / 2)
      expect(await moved).toBe(true)
      await expect.poll(() => place(page)).not.toBe(start)
    }
  })

  test('where the curl cannot draw the page slides, from the start and mid-turn', async ({ page }) => {
    // The screen takes the drawing away during a whole turn: the page still turns, the plain way.
    await openBook(page)
    await curlReady(page)
    const lose = () => page.evaluate(() => (document.querySelector('canvas.rdr-curl') as HTMLCanvasElement).getContext('webgl')?.getExtension('WEBGL_lose_context')?.loseContext())
    const first = await place(page)
    await page.keyboard.press('ArrowRight')
    await expect.poll(() => curlShown(page)).toBe(true)
    await lose()
    await expect.poll(() => place(page)).not.toBe(first)
    await settled(page)
    // A drag from then on, and in a fresh book a drag that loses it half way.
    await openBook(page)
    await curlReady(page)
    const start = await place(page)
    const b = await pageBox(page)
    const y = b.y + b.height * 0.8
    await page.mouse.move(b.x + b.width - 8, y)
    await page.mouse.down()
    await page.mouse.move(b.x + b.width * 0.5, y, { steps: 6 })
    await page.waitForTimeout(150)
    expect(await curlShown(page)).toBe(true)
    await lose()
    await page.waitForTimeout(100)
    await page.mouse.move(b.x + b.width * 0.2, y, { steps: 3 })
    await page.mouse.up()
    await expect.poll(() => place(page)).not.toBe(start)
    await settled(page)
    const second = await place(page)
    await page.waitForTimeout(600)
    await page.keyboard.press('ArrowRight')
    await expect.poll(() => place(page)).not.toBe(second)
    // No WebGL at all: nothing to see of the curl, and the page turns.
    await page.addInitScript(() => {
      const real = HTMLCanvasElement.prototype.getContext
      HTMLCanvasElement.prototype.getContext = function (this: HTMLCanvasElement, type: string, ...rest: unknown[]) {
        return type.startsWith('webgl') ? null : real.apply(this, [type, ...rest] as never)
      } as never
    })
    await openBook(page)
    await page.waitForTimeout(1000)
    const third = await place(page)
    await page.keyboard.press('ArrowRight')
    await expect.poll(() => place(page)).not.toBe(third)
    await expect(page.locator('canvas.rdr-curl')).toHaveCount(0)
  })

  test('words selected without any event from the book are offered for a highlight', async ({ page }) => {
    await openBook(page)
    await page.waitForTimeout(800)
    // What a long press does in iPhone Safari: a selection, and no word of it to the page.
    await page.evaluate(() => {
      const f = document.querySelector('.rdr-viewer iframe') as HTMLIFrameElement
      const doc = f.contentDocument as Document
      const stop = (e: Event) => e.stopImmediatePropagation()
      doc.addEventListener('selectionchange', stop, true)
      doc.defaultView?.addEventListener('selectionchange', stop, true)
      const t = doc.querySelectorAll('p')[4].firstChild as Text
      const r = doc.createRange()
      r.setStart(t, 4)
      r.setEnd(t, 30)
      const s = f.contentWindow?.getSelection()
      s?.removeAllRanges()
      s?.addRange(r)
    })
    await expect(page.locator('.rdr-pick')).toBeVisible({ timeout: 3000 })
  })

  test('a highlight sits on its words on the page and in the picture of it', async ({ page }) => {
    const mark = {
      id: 77,
      kind: 'highlight',
      position: 'epubcfi(/6/2!/4/8,/1:4,/1:30)',
      excerpt: 'x',
      note: '',
      color: 'yellow',
      label: '',
      percent: 1,
    }
    await page.route(`**/api/books/${BOOK}/marks*`, (r) => (r.request().method() === 'GET' ? r.fulfill({ json: [mark] }) : r.fallback()))
    await openBook(page)
    await curlReady(page)
    const off = await page.evaluate(() => {
      const f = document.querySelector('.rdr-viewer iframe') as HTMLIFrameElement
      const rect = f.parentElement?.querySelector(':scope > svg rect')
      if (!rect) return null
      const doc = f.contentDocument as Document
      const t = doc.querySelectorAll('body > *')[3].firstChild as Text
      const r = doc.createRange()
      r.setStart(t, 4)
      r.setEnd(t, 30)
      const words = r.getBoundingClientRect()
      const fr = f.getBoundingClientRect()
      const band = rect.getBoundingClientRect()
      return { dy: band.top - (words.top + fr.top), dx: band.left - (words.left + fr.left) }
    })
    expect(off).not.toBeNull()
    expect(Math.abs(off?.dy ?? 99)).toBeLessThan(1.5)
    expect(Math.abs(off?.dx ?? 99)).toBeLessThan(1.5)
    expect(await pictureDiff(page)).toBeLessThan(0.02)
  })

  // WebKit gives the book's sandboxed frame no events, as on an iPhone. Run with a
  // WebKit project, as the normal one is Chromium.
  test('in WebKit a selection and a footnote work without any event from the book', async ({ page, browserName }) => {
    test.skip(browserName !== 'webkit', 'WebKit only')
    await openBook(page)
    await page.waitForTimeout(1500)
    const deaf = await page.evaluate(() => {
      const f = document.querySelector('.rdr-viewer iframe') as HTMLIFrameElement
      const doc = f.contentDocument as Document
      let heard = false
      const l = () => {
        heard = true
      }
      doc.addEventListener('rdr-test', l)
      doc.dispatchEvent(new Event('rdr-test'))
      doc.removeEventListener('rdr-test', l)
      return { heard, href: doc.getElementById('r1')?.getAttribute('href') }
    })
    console.log('frame events', JSON.stringify(deaf))
    expect(deaf.heard).toBe(false)
    // The address of a link is one that can be seen changing.
    expect(deaf.href).toContain('#rdr-link-')
    await page.evaluate(() => {
      const f = document.querySelector('.rdr-viewer iframe') as HTMLIFrameElement
      const doc = f.contentDocument as Document
      const t = doc.querySelectorAll('p')[4].firstChild as Text
      const r = doc.createRange()
      r.setStart(t, 4)
      r.setEnd(t, 30)
      const s = f.contentWindow?.getSelection()
      s?.removeAllRanges()
      s?.addRange(r)
    })
    await expect(page.locator('.rdr-pick')).toBeVisible({ timeout: 3000 })
    await page.getByRole('dialog', { name: 'Highlight' }).getByRole('button', { name: 'Cancel' }).click()
    // A real tap on the mark.
    const start = await place(page)
    const box = await frame(page).locator('#r1').boundingBox()
    if (!box) throw new Error('no link')
    await page.touchscreen.tap(box.x + box.width / 2, box.y + box.height / 2)
    await expect(page.locator('.rdr-note')).toContainText('The ledger was kept in pencil.', { timeout: 3000 })
    await page.locator('.rdr-note').getByRole('button', { name: 'Close' }).click()
    expect(await place(page)).toBe(start)
    // The same mark again, and then an ordinary link, which still goes there.
    await page.touchscreen.tap(box.x + box.width / 2, box.y + box.height / 2)
    await expect(page.locator('.rdr-note')).toBeVisible({ timeout: 3000 })
    await page.locator('.rdr-note').getByRole('button', { name: 'Close' }).click()
    const plain = await frame(page).locator('#r3').boundingBox()
    if (!plain) throw new Error('no link')
    await page.touchscreen.tap(plain.x + plain.width / 2, plain.y + plain.height / 2)
    await expect.poll(() => place(page)).toBe('Two@0')
  })
})

// One whole page sits between the bars and nothing moves up and down, whatever the
// window and the settings. Scrolling is only there when Scroll is chosen.
test.describe('the page fits the window', () => {
  test.use({ serviceWorkers: 'block' })
  const LARGEST = { size: 200, line: 'wide', font: 'clear', margin: 'wide', justify: 'on' }

  // What is too tall, by name, and where the last line of the page ends against the bars.
  const measure = (page: Page) =>
    page.evaluate(() => {
      const tooTall: string[] = []
      for (let e: Element | null = document.querySelector('.rdr-viewer'); e; e = e.parentElement) {
        if (e.scrollHeight > e.clientHeight + 1) tooTall.push(`${e.tagName}.${e.className}: ${e.scrollHeight} in ${e.clientHeight}`)
      }
      window.scrollTo(0, 500)
      document.body.scrollTo(0, 500)
      const moved = window.scrollY + document.body.scrollTop
      const f = document.querySelector('.rdr-viewer iframe') as HTMLIFrameElement
      const doc = f.contentDocument as Document
      if (doc.documentElement.scrollHeight > doc.documentElement.clientHeight + 1) tooTall.push(`the frame: ${doc.documentElement.scrollHeight} in ${doc.documentElement.clientHeight}`)
      const box = (document.querySelector('.rdr-viewer .epub-container') as HTMLElement).getBoundingClientRect()
      const frame = f.getBoundingClientRect()
      const foot = (document.querySelector('.rdr-foot') as HTMLElement).getBoundingClientRect()
      const top = (document.querySelector('.rdr-top') as HTMLElement).getBoundingClientRect()
      let first = Infinity
      let last = 0
      let lines = 0
      const range = doc.createRange()
      const walk = doc.createTreeWalker(doc.body, NodeFilter.SHOW_TEXT)
      for (let n = walk.nextNode(); n; n = walk.nextNode()) {
        range.selectNodeContents(n)
        for (const r of Array.from(range.getClientRects())) {
          if (r.width > 0 && r.left + frame.left >= box.left - 1 && r.left + frame.left < box.right) {
            lines++
            first = Math.min(first, r.top + frame.top)
            last = Math.max(last, r.bottom + frame.top)
          }
        }
      }
      const pad = parseFloat(getComputedStyle(doc.body).paddingBottom)
      return {
        tooTall,
        moved,
        lines,
        first,
        last,
        textBottom: frame.bottom - pad, // where the text column stops
        frameBottom: frame.bottom,
        columnsFor: parseFloat(doc.body.style.height),
        room: box.height,
        topBar: top.bottom,
        footTop: foot.top,
        footBottom: foot.bottom,
        window: innerHeight,
      }
    })

  async function pageFits(page: Page, label = '') {
    const m = await measure(page)
    expect(m.tooTall, label).toEqual([])
    expect(m.moved, label).toBe(0)
    expect(m.lines, label).toBeGreaterThan(0)
    expect(m.footBottom, label).toBeLessThanOrEqual(m.window + 1)
    expect(m.first, label).toBeGreaterThanOrEqual(m.topBar - 1)
    expect(m.frameBottom, label).toBeLessThanOrEqual(m.footTop + 1)
    // The last line is whole: it ends inside the text column, above the bar.
    expect(m.last, label).toBeLessThanOrEqual(m.textBottom + 1)
    expect(m.last, label).toBeLessThanOrEqual(m.footTop)
    // The columns were made for the room there is now.
    expect(Math.abs(m.columnsFor - m.room), label).toBeLessThanOrEqual(1)
    return m
  }

  // The size the columns were made for has caught up with the window.
  async function refitted(page: Page) {
    await expect
      .poll(() => page.evaluate(() => Math.abs(parseFloat((document.querySelector('.rdr-viewer iframe') as HTMLIFrameElement | null)?.contentDocument?.body?.style.height ?? '0') - ((document.querySelector('.rdr-viewer') as HTMLElement | null)?.clientHeight ?? 0))))
      .toBeLessThanOrEqual(1)
    await opened(page)
    await page.waitForTimeout(1000)
  }

  test('at every size, with every setting at its biggest, in one page and two', async ({ page }) => {
    test.setTimeout(240_000)
    const sizes = [
      [390, 844],
      [390, 660], // a phone with its address bar showing
      [360, 560],
      [844, 390], // on its side
      [1280, 800], // two pages
      [1920, 1080],
    ]
    for (const [width, height] of sizes) {
      for (const prefs of [{ effect: 'none' }, { effect: 'none', ...LARGEST }]) {
        await page.setViewportSize({ width, height })
        await openBook(page, prefs)
        await opened(page)
        await page.waitForTimeout(800)
        const label = `${width}x${height} ${JSON.stringify(prefs)}`
        for (let i = 0; i < 3; i++) {
          await pageFits(page, label)
          await page.keyboard.press('ArrowRight')
          await page.waitForTimeout(500)
        }
        if (width >= 1280) expect(await page.evaluate(() => (document.querySelector('.rdr-viewer iframe') as HTMLIFrameElement).contentDocument?.body.style.columnWidth), label).not.toBe(`${width}px`)
      }
    }
  })

  test('with each page turn, and the curl draws the same page', async ({ page }) => {
    test.setTimeout(120_000)
    await page.setViewportSize({ width: 390, height: 660 })
    for (const effect of ['slide', 'none', 'curl']) {
      await openBook(page, { effect })
      if (effect === 'curl') await curlReady(page)
      await page.keyboard.press('ArrowRight')
      await settled(page)
      await page.waitForTimeout(900)
      await pageFits(page, effect)
    }
    const c = await page.evaluate(() => {
      const r = (document.querySelector('canvas.rdr-curl') as HTMLElement).getBoundingClientRect()
      const v = (document.querySelector('.rdr-viewer') as HTMLElement).getBoundingClientRect()
      return { canvasBottom: r.bottom, viewerBottom: v.bottom }
    })
    expect(c.canvasBottom).toBeLessThanOrEqual(c.viewerBottom + 1)
    await page.waitForTimeout(800)
    expect(await pictureDiff(page)).toBeLessThan(0.02)
  })

  test("when the window changes the page is made anew, and it's the same page", async ({ page }) => {
    test.setTimeout(120_000)
    await page.setViewportSize({ width: 390, height: 844 })
    await openBook(page, {})
    await curlReady(page)
    for (let i = 0; i < 3; i++) {
      await page.keyboard.press('ArrowRight')
      await settled(page)
      await page.waitForTimeout(500)
    }
    await page.waitForTimeout(800)
    const words = await firstWords(page)
    const start = await place(page)
    const shown = () => page.evaluate(() => (document.querySelector('.rdr-viewer iframe') as HTMLIFrameElement).contentDocument?.body.textContent ?? '')
    // The address bar shows and hides, again and again.
    for (const height of [660, 844, 700, 844]) {
      await page.setViewportSize({ width: 390, height })
      await refitted(page)
      await pageFits(page, `${height}`)
    }
    expect(await firstWords(page)).toBe(words)
    expect(await place(page)).toBe(start)
    expect(await shown()).toContain(words)
    // On its side and back: the words are still on the page.
    await page.setViewportSize({ width: 844, height: 390 })
    await refitted(page)
    await pageFits(page, 'on its side')
    await page.setViewportSize({ width: 390, height: 844 })
    await refitted(page)
    await pageFits(page, 'upright again')
    expect(await firstWords(page)).toBe(words)
    // The curl's picture is of the new layout, and a turn from here starts at this page.
    await page.waitForTimeout(1500)
    expect(await pictureDiff(page)).toBeLessThan(0.02)
    await page.keyboard.press('ArrowRight')
    await expect.poll(() => firstWords(page)).not.toBe(words)
  })

  test("the page behind the reader can't move, even with something extra in it", async ({ page }) => {
    await page.setViewportSize({ width: 390, height: 660 })
    await openBook(page, { effect: 'none' })
    await opened(page)
    const moved = await page.evaluate(() => {
      const tall = document.createElement('div')
      tall.style.height = '56px' // as tall as an address bar
      document.querySelector('.bks')?.appendChild(tall)
      window.scrollTo(0, 500)
      return window.scrollY
    })
    expect(moved).toBe(0)
    const place = await page.evaluate(() => ({ top: document.querySelector('.rdr')?.getBoundingClientRect().top, bottom: document.querySelector('.rdr-foot')?.getBoundingClientRect().bottom, h: innerHeight }))
    expect(place.top).toBe(0)
    expect(place.bottom).toBeLessThanOrEqual(place.h)
  })

  test("the bars stay clear of the phone's own bars", async ({ page, browserName }) => {
    test.skip(browserName !== 'chromium', 'set through Chromium')
    await page.setViewportSize({ width: 390, height: 780 })
    const cdp = await page.context().newCDPSession(page)
    await cdp.send('Emulation.setSafeAreaInsetsOverride' as never, { insets: { top: 24, bottom: 48, left: 0, right: 0 } } as never)
    await openBook(page, { effect: 'none' })
    await opened(page)
    await page.waitForTimeout(800)
    await pageFits(page)
    const m = await page.evaluate(() => ({
      back: (document.querySelector('.rdr-top a') as HTMLElement).getBoundingClientRect().top,
      slider: (document.querySelector('.rdr-foot input') as HTMLElement).getBoundingClientRect().bottom,
      h: innerHeight,
    }))
    expect(m.back).toBeGreaterThanOrEqual(24)
    expect(m.slider).toBeLessThanOrEqual(m.h - 48)
  })

  test('Scroll is a choice: the text scrolls and the place, highlights and bookmarks hold', async ({ page }) => {
    test.setTimeout(150_000)
    const mark = { id: 78, kind: 'highlight', position: 'epubcfi(/6/2!/4/8,/1:4,/1:30)', excerpt: 'x', note: '', color: 'yellow', label: '', percent: 1 }
    await page.route(`**/api/books/${BOOK}/marks*`, (r) => (r.request().method() === 'GET' ? r.fulfill({ json: [mark] }) : r.fallback()))
    await page.setViewportSize({ width: 390, height: 660 })
    const { kept, route } = keepProgress(page)
    await openBook(page, { effect: 'none' }, false, route)
    await opened(page)
    await page.keyboard.press('ArrowRight')
    await page.waitForTimeout(600)
    const words = await firstWords(page)

    // Chosen beside the page turn. The same words are still in front of the reader.
    let panel = await openSettings(page)
    await expect(panel.locator('.rdr-row', { hasText: 'Page turn' })).toBeVisible()
    await panel.locator('.rdr-row', { hasText: 'Reading' }).getByRole('button', { name: 'Scroll', exact: true }).click()
    await expect(panel.locator('.rdr-row', { hasText: 'Page turn' })).toHaveCount(0)
    await page.getByRole('button', { name: 'Text and colours' }).click()
    await expect(page.locator('.rdr-scroll')).toHaveCount(1)
    await opened(page)
    await page.waitForTimeout(1500)
    expect((await storedPrefs(page)).reading).toBe('scroll')
    const scroller = () =>
      page.evaluate(() => {
        const c = document.querySelector('.rdr-viewer .epub-container') as HTMLElement
        return { top: c.scrollTop, height: c.scrollHeight, room: c.clientHeight, doc: window.scrollY, frames: c.querySelectorAll('iframe').length }
      })
    const at = await scroller()
    expect(at.height).toBeGreaterThan(at.room)
    expect(at.top).toBeGreaterThan(0)
    const near = await page.evaluate((w) => {
      const c = (document.querySelector('.rdr-viewer .epub-container') as HTMLElement).getBoundingClientRect()
      for (const f of Array.from(document.querySelectorAll<HTMLIFrameElement>('.rdr-viewer iframe'))) {
        const doc = f.contentDocument as Document
        const walk = doc.createTreeWalker(doc.body, NodeFilter.SHOW_TEXT)
        for (let n = walk.nextNode(); n; n = walk.nextNode()) {
          const i = (n.textContent ?? '').indexOf(w)
          if (i < 0) continue
          const r = doc.createRange()
          r.setStart(n, i)
          r.setEnd(n, i + 1)
          const y = r.getBoundingClientRect().top + f.getBoundingClientRect().top
          if (y >= c.top - 2 && y < c.bottom) return true
        }
      }
      return false
    }, words.slice(0, 20))
    expect(near).toBe(true)

    // A finger or a wheel on the text moves the text and never the page behind it.
    const box = await pageBox(page)
    await page.mouse.move(box.x + box.width / 2, box.y + box.height / 2)
    await page.mouse.wheel(0, 1500)
    await expect.poll(async () => (await scroller()).top).toBeGreaterThan(at.top + 400)
    expect((await scroller()).doc).toBe(0)

    // It goes on across a chapter, keeps the place, and comes back there.
    await page.waitForTimeout(2500)
    await expect.poll(() => (kept.saved as { position?: string } | null)?.position ?? '').toContain('epubcfi')
    const savedAt = (kept.saved as { position: string }).position
    await page.reload()
    await opened(page)
    await page.waitForTimeout(1500)
    expect((await scroller()).top).toBeGreaterThan(at.top + 400)
    expect(savedAt).toContain('epubcfi')

    // The highlight is on its words, and a bookmark can be made here.
    await expect(page.locator('.rdr-viewer .rdr-hl')).not.toHaveCount(0)
    await page.getByRole('button', { name: 'Bookmarks' }).click()
    await expect(page.locator('.rdr-panel')).toBeVisible()
    await page.getByRole('button', { name: 'Bookmarks' }).click()

    // Back to pages: whole pages again, around the same place.
    panel = await openSettings(page)
    await panel.locator('.rdr-row', { hasText: 'Reading' }).getByRole('button', { name: 'Pages', exact: true }).click()
    await page.getByRole('button', { name: 'Text and colours' }).click()
    await expect(page.locator('.rdr-scroll')).toHaveCount(0)
    await opened(page)
    await page.waitForTimeout(1200)
    await refitted(page)
    await pageFits(page, 'back to pages')
  })

  test('a PDF shows a whole page in the window, and Fit width is the choice that scrolls', async ({ page }) => {
    const pdf = `%PDF-1.4
1 0 obj<</Type/Catalog/Pages 2 0 R>>endobj
2 0 obj<</Type/Pages/Kids[3 0 R]/Count 1>>endobj
3 0 obj<</Type/Page/Parent 2 0 R/MediaBox[0 0 400 900]>>endobj
trailer<</Root 1 0 R/Size 4>>
%%EOF`
    await page.route(`**/api/books/${BOOK}`, (r) =>
      r.fulfill({ json: { id: BOOK, olKey: 'OLTURN', title: 'Turn test', author: 'Sample', addedAt: '2026-01-01T00:00:00Z', ebook: { status: 'downloaded', format: 'pdf' }, audiobook: { status: 'none' } } }),
    )
    await page.route(`**/api/books/${BOOK}/read*`, (r) => r.fulfill({ body: pdf, contentType: 'application/pdf' }))
    await page.route(`**/api/books/${BOOK}/progress*`, (r) => (r.request().method() === 'GET' ? r.fulfill({ status: 404, json: {} }) : r.fulfill({ json: {} })))
    await page.setViewportSize({ width: 390, height: 660 })
    await page.request.put('/api/modules', { data: { ebooks: true } })
    await page.goto(`/bookshelf/read/${BOOK}`)
    await expect(page.locator('.rdr-pdf .rdr-title small')).toBeVisible({ timeout: 30_000 })
    const look = () =>
      page.evaluate(() => {
        const v = document.querySelector('.rdr-pdf-view') as HTMLElement
        const c = v.querySelector('canvas') as HTMLElement
        window.scrollTo(0, 500)
        return { drawn: c.offsetHeight, view: v.clientHeight, inside: v.scrollHeight, moved: window.scrollY, foot: (document.querySelector('.rdr-foot') as HTMLElement).getBoundingClientRect().bottom, h: innerHeight }
      })
    await expect.poll(async () => (await look()).drawn).toBeGreaterThan(200)
    let m = await look()
    expect(m.inside).toBeLessThanOrEqual(m.view)
    expect(m.moved).toBe(0)
    expect(m.foot).toBeLessThanOrEqual(m.h)
    // The window gets shorter: the page is drawn to fit that.
    await page.setViewportSize({ width: 390, height: 560 })
    await expect.poll(async () => (await look()).inside <= (await look()).view && (await look()).drawn < m.drawn).toBe(true)
    await page.getByRole('button', { name: 'Fit width' }).click()
    await expect.poll(async () => (await look()).inside).toBeGreaterThan((await look()).view)
    m = await look()
    expect(m.moved).toBe(0)
  })
})
