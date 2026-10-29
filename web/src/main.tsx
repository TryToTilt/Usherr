import { loadLanguage } from './i18n'
import { applyBase, applyPalette } from './palette' // Usherr

// Dark unless light was picked (ThemeToggle.tsx), set before anything paints.
let theme = 'dark'
try {
  if (localStorage.getItem('mediarium-theme') === 'light') theme = 'light'
} catch {
  // blocked storage: dark
}
document.documentElement.dataset.theme = theme
// Usherr: the chosen colours (PaletteChoice.tsx), also before anything paints.
try {
  const palette = localStorage.getItem('usherr-palette')
  if (palette && palette !== 'wine' && /^[a-z]+$/.test(palette)) applyPalette(palette)
  const base = localStorage.getItem('usherr-base') // and the main colour (BaseChoice.tsx)
  if (base && base !== 'palette' && /^[a-z]+$/.test(base)) applyBase(base)
} catch {
  // blocked storage: wine
}
// Usherr Neu (neu/look.ts), also before anything paints.
try {
  if (localStorage.getItem('usherr-look') === 'neu') document.documentElement.dataset.look = 'neu'
} catch {
  // blocked storage: Classic
}

// The app puts a page where it belongs when going back or forward (App shell); the browser's own guess would fight it.
try {
  history.scrollRestoration = 'manual'
} catch {
  // not available: nothing to do
}

// The chosen language is loaded before anything else, so text the app sets up
// as it starts is already in that language.
void loadLanguage().finally(() => import('./start.tsx'))

// A picture that fails to load (a cover that does not exist, a host that is down) shows the browser's broken-image
// icon. Hide it instead; the box it sits in keeps its own background. A new address that loads clears the mark.
document.addEventListener(
  'error',
  (e) => {
    if (e.target instanceof HTMLImageElement) e.target.dataset.broken = '1'
  },
  true,
)
document.addEventListener(
  'load',
  (e) => {
    if (e.target instanceof HTMLImageElement) delete e.target.dataset.broken
  },
  true,
)
