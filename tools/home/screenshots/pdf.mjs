// Home fork: the printable guide. Turns the fork's documentation pages
// (docs/home) into one PDF with their pictures, laid out for paper: light
// background, a contents page, and each part starting on a new page.
//
// Run it with tools/home/screenshots/pdf.sh (in the official Playwright image).
import { chromium } from 'playwright'
import { marked } from 'marked'
import { readFileSync, writeFileSync } from 'node:fs'
import { join } from 'node:path'

const docs = process.env.DOCS ?? '/docs'
const fonts = process.env.FONTS ?? '/fonts' // the app's own (web/public/fonts)
const font = (file) => `url(data:font/ttf;base64,${readFileSync(join(fonts, file)).toString('base64')}) format('truetype')`
const out = process.env.PDF ?? join(docs, 'usherr-guide.pdf') // Usherr
// Usherr: the app's name, as the app shows it (web/src/brand.ts): addresses keep "Mediarium".
const brand = (s) => s.replace(/(?<![\w/.@-])Mediarium(?![\w/-])/g, 'Usherr')
const PAGES = [
  ['getting-started.md', 'Getting started'],
  ['finding.md', 'Finding something to watch'],
  ['watching.md', 'Watching it'],
  ['integrations.md', 'Setting up each part'],
  ['troubleshooting.md', 'When something goes wrong'],
]

const slug = (s) =>
  s
    .toLowerCase()
    .replace(/<[^>]+>/g, '')
    .replace(/[^\p{L}\p{N}\s-]/gu, '')
    .trim()
    .replace(/\s+/g, '-')

// Headings get ids like GitHub's, prefixed per page so they stay apart; links
// between the pages become links inside the PDF.
const prefix = (file) => file.replace(/\.md$/, '')
let current = ''
marked.use({
  renderer: {
    heading({ tokens, depth }) {
      const text = this.parser.parseInline(tokens)
      const id = depth === 1 ? prefix(current) : `${prefix(current)}--${slug(text)}`
      return `<h${depth} id="${id}">${text}</h${depth}>\n`
    },
    link({ href, tokens }) {
      const text = this.parser.parseInline(tokens)
      const m = /^([\w-]+\.md)?(?:#(.*))?$/.exec(href)
      if (m && (m[1] || m[2])) {
        const file = m[1] ?? current
        const target = m[2] ? `${prefix(file)}--${m[2]}` : prefix(file)
        return `<a href="#${target}">${text}</a>`
      }
      return `<a href="${href}">${text}</a>`
    },
    image({ href, text }) {
      const name = /^images\/([\w.-]+)$/.exec(href)?.[1]
      if (!name) return ''
      const data = readFileSync(join(docs, 'images', name)).toString('base64')
      return `<figure><img src="data:image/png;base64,${data}" alt="${text}"><figcaption>${text}</figcaption></figure>`
    },
  },
})

const parts = PAGES.map(([file]) => {
  current = file
  return `<section class="part">${marked.parse(brand(readFileSync(join(docs, file), 'utf8')))}</section>`
})
const toc = PAGES.map(([file, title]) => {
  const md = brand(readFileSync(join(docs, file), 'utf8'))
  const subs = [...md.matchAll(/^## (.+)$/gm)].map((m) => `<li><a href="#${prefix(file)}--${slug(m[1])}">${m[1]}</a></li>`).join('')
  return `<li><a href="#${prefix(file)}">${title}</a><ol>${subs}</ol></li>`
}).join('')

const html = `<!doctype html><html><head><meta charset="utf-8"><title>Usherr: the guide</title><style>
  @font-face { font-family: 'Host Grotesk'; src: ${font('HostGrotesk-Variable.ttf')}; font-weight: 300 800; }
  @font-face { font-family: 'Wix Madefor Display'; src: ${font('WixMadeforDisplay-Variable.ttf')}; font-weight: 400 800; }
  @font-face { font-family: 'Reddit Mono'; src: ${font('RedditMono-Variable.ttf')}; font-weight: 200 900; }
  @page { size: A4; margin: 18mm 16mm 20mm; }
  body { font: 10.5pt/1.55 'Host Grotesk', system-ui, sans-serif; color: #1b1f27; }
  h1, h2, h3, .cover h1 { font-family: 'Wix Madefor Display', 'Host Grotesk', sans-serif; font-weight: 700; } strong { font-weight: 700; }
  .cover { height: 230mm; display: flex; flex-direction: column; justify-content: center; }
  .cover h1 { font-size: 30pt; margin: 0 0 6mm; letter-spacing: -0.02em; }
  .cover p { font-size: 12pt; color: #4a5160; max-width: 120mm; }
  .cover .brand { width: 18mm; height: 18mm; border-radius: 5mm; background: #821a3b; border: 1mm solid #c99a42; margin-bottom: 8mm; } /* Usherr */
  nav { page-break-after: always; }
  nav ol { padding-left: 5mm; } nav > ol > li { margin: 3mm 0; font-weight: 600; } nav ol ol { font-weight: 400; }
  nav a { color: inherit; text-decoration: none; }
  .part { page-break-before: always; }
  h1 { font-size: 20pt; margin: 0 0 4mm; } h2 { font-size: 14pt; margin: 8mm 0 2mm; padding-top: 3mm; border-top: 1px solid #d9dde3; page-break-after: avoid; }
  h3 { font-size: 11.5pt; page-break-after: avoid; }
  a { color: #0d7f73; } code { font: 9pt 'Reddit Mono', ui-monospace, monospace; background: #eef0f3; padding: 0 3px; border-radius: 3px; }
  pre { background: #eef0f3; padding: 3mm; border-radius: 2mm; font-size: 8.5pt; overflow: hidden; page-break-inside: avoid; }
  table { border-collapse: collapse; width: 100%; font-size: 9.5pt; page-break-inside: avoid; } th, td { border: 1px solid #d9dde3; padding: 1.5mm 2mm; text-align: left; vertical-align: top; } th { background: #eef0f3; }
  figure { margin: 4mm 0; page-break-inside: avoid; } figure img { max-width: 100%; max-height: 120mm; border-radius: 2mm; border: 1px solid #c9ced6; }
  figcaption { font-size: 8.5pt; color: #5f6673; margin-top: 1mm; }
  ul, ol { padding-left: 6mm; } li { margin: 1mm 0; }
</style></head><body>
<div class="cover"><div class="brand"></div><h1>Usherr: the guide</h1><p>Setting it up, every part it can use, and what to do when something goes wrong.</p></div>
<nav><h1>Contents</h1><ol>${toc}</ol></nav>
${parts.join('\n')}
</body></html>`

const browser = await chromium.launch()
const page = await browser.newPage()
await page.setContent(html, { waitUntil: 'load' })
await page.pdf({
  path: out,
  format: 'A4',
  printBackground: true,
  displayHeaderFooter: true,
  headerTemplate: '<span></span>',
  footerTemplate: '<div style="width:100%;font-size:8px;color:#888;text-align:center;font-family:system-ui">Usherr · <span class="pageNumber"></span> / <span class="totalPages"></span></div>',
  margin: { top: '18mm', bottom: '20mm', left: '16mm', right: '16mm' },
})
await browser.close()
writeFileSync(out + '.html', html) // the same, to read in a browser
console.log(`wrote ${out}`)
