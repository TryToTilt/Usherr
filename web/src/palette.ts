// Usherr: the colour choice's own icon. The icon and the browser tab's icon
// take the palette's colour (web/public/usherr-mark-<id>.png, made by
// tools/usherr/recolor_mark.py); wine is the original.
import { withBase } from './base'

export function currentPalette(): string {
  return document.documentElement.dataset.palette ?? 'wine'
}

export function markSrc(palette = currentPalette()): string {
  if (document.documentElement.dataset.look === 'neu') return withBase('/neu-mark.svg') // Usherr Neu
  return withBase(palette === 'wine' ? '/usherr-mark.png' : `/usherr-mark-${palette}.png`)
}

// Sets the palette on the page and swaps the icons already showing.
export function applyPalette(palette: string) {
  if (palette === 'wine') delete document.documentElement.dataset.palette
  else document.documentElement.dataset.palette = palette
  const src = markSrc(palette)
  document.querySelectorAll<HTMLImageElement>('img.brand-mark, img.usherr-glyph').forEach((img) => {
    img.src = src
  })
  tabIcon(palette)
}

// Usherr: the main colour (BaseChoice.tsx), the tint under the colours.
export function applyBase(base: string) {
  if (base === 'palette') delete document.documentElement.dataset.base
  else document.documentElement.dataset.base = base
}

// The browser tab: the original icons for wine, the coloured icon otherwise.
export function tabIcon(palette: string) {
  if (palette === 'wine') return
  document.querySelectorAll<HTMLLinkElement>('link[rel~="icon"]').forEach((l) => l.remove())
  const link = document.createElement('link')
  link.rel = 'icon'
  link.type = 'image/png'
  link.href = markSrc(palette)
  document.head.appendChild(link)
}
