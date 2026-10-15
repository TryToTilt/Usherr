// Builds the bundle the in-app user guide reads: the topics listed in
// src/guide/topicDefs.ts, taken from the documentation pages in ../docs.
// Runs before `npm run build` and `npm run dev` (see package.json); the result,
// src/guide/guide.generated.json, is not saved in the repository.
//
//   node --experimental-strip-types tools/build-guide.mjs
import { copyFileSync, existsSync, mkdirSync, readFileSync, readdirSync, writeFileSync } from 'node:fs'
import { dirname, join } from 'node:path'
import { fileURLToPath } from 'node:url'
import { buildGuide } from '../src/guide/build.ts'

const here = dirname(fileURLToPath(import.meta.url))
const docs = join(here, '..', '..', 'docs')
const out = join(here, '..', 'src', 'guide', 'guide.generated.json')
mkdirSync(dirname(out), { recursive: true })

if (!existsSync(docs)) {
  // A build from a copy that has no docs folder still works; the guide then points to the website.
  console.warn('guide: no docs folder found, the in-app guide will be empty')
  writeFileSync(out, JSON.stringify({ topics: [] }))
} else {
  const data = buildGuide((file) => readFileSync(join(docs, file), 'utf8'))
  const json = JSON.stringify(data)
  writeFileSync(out, json)
  console.log(`guide: ${data.topics.length} topics, ${(json.length / 1024).toFixed(0)} KB`)
  // home fork: the fork's pages show their pictures (docs/home/images), served
  // as plain files rather than bundled; names starting with _ are left out.
  const pics = join(docs, 'home', 'images')
  const served = join(here, '..', 'public', 'guide-images')
  if (existsSync(pics)) {
    mkdirSync(served, { recursive: true })
    const list = readdirSync(pics).filter((f) => /\.(png|jpe?g|webp)$/i.test(f) && !f.startsWith('_'))
    for (const f of list) copyFileSync(join(pics, f), join(served, f))
    console.log(`guide: ${list.length} pictures for the home fork's pages`)
  }
}
