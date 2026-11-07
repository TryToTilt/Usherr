// Usherr: a volume that came as a PDF, as pictures the Manga reader can show like
// any other page. pdf.js draws each page in the browser, as the book reader's PDF
// view does, and only the ones near where you are stay in memory.
import type { PDFDocumentProxy } from 'pdfjs-dist'
import { t } from '../i18n'

export interface PdfPages {
  count: number
  size: [number, number] // the first page's, as a stand-in for them all
  url: (page: number, width: number) => Promise<string>
  keepOnly: (pages: readonly number[]) => void
  destroy: () => void
}

export async function openPdf(fileUrl: string): Promise<PdfPages> {
  const [pdfjs, worker] = await Promise.all([import('pdfjs-dist'), import('pdfjs-dist/build/pdf.worker.min.mjs?url')])
  pdfjs.GlobalWorkerOptions.workerSrc = worker.default
  const res = await fetch(fileUrl, { credentials: 'include' })
  if (!res.ok) throw new Error(t('This file’s pages can’t be read (error {status}).', { status: String(res.status) }))
  const doc: PDFDocumentProxy = await pdfjs.getDocument({ data: new Uint8Array(await res.arrayBuffer()) }).promise
  const first = (await doc.getPage(1)).getViewport({ scale: 1 })
  const made = new Map<number, Promise<string>>()

  const draw = async (page: number, width: number): Promise<string> => {
    const pg = await doc.getPage(page)
    const base = pg.getViewport({ scale: 1 })
    const vp = pg.getViewport({ scale: width / base.width })
    const canvas = document.createElement('canvas')
    canvas.width = Math.floor(vp.width)
    canvas.height = Math.floor(vp.height)
    await pg.render({ canvas, viewport: vp }).promise
    const blob = await new Promise<Blob | null>((resolve) => canvas.toBlob(resolve, 'image/jpeg', 0.9))
    canvas.width = 0
    canvas.height = 0
    pg.cleanup()
    if (!blob) throw new Error('page')
    return URL.createObjectURL(blob)
  }

  const forget = (page: number) => {
    const u = made.get(page)
    made.delete(page)
    void u?.then((url) => URL.revokeObjectURL(url)).catch(() => undefined)
  }

  return {
    count: doc.numPages,
    size: [first.width, first.height],
    url: (page, width) => {
      let u = made.get(page)
      if (!u) {
        u = draw(page, width)
        made.set(page, u)
        u.catch(() => made.delete(page))
      }
      return u
    },
    keepOnly: (pages) => {
      for (const page of [...made.keys()]) if (!pages.includes(page)) forget(page)
    },
    destroy: () => {
      for (const page of [...made.keys()]) forget(page)
      void doc.loadingTask.destroy()
    },
  }
}
