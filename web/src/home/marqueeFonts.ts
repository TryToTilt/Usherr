// Usherr: the Marquee's display fonts, bundled with the app (no outside
// requests). Each is a few dozen kilobytes and only downloads when a screen
// (or the settings preview) uses it.
import '@fontsource/limelight/latin-400.css'
import '@fontsource/monoton/latin-400.css'
import '@fontsource/bebas-neue/latin-400.css'
import '@fontsource/cinzel/latin-400.css'
import '@fontsource/cinzel/latin-700.css'
import '@fontsource/righteous/latin-400.css'
import '@fontsource/abril-fatface/latin-400.css'
import '@fontsource/oswald/latin-400.css'
import '@fontsource/oswald/latin-600.css'
import '@fontsource/playfair-display/latin-400.css'
import '@fontsource/playfair-display/latin-700.css'
import type { MarqueeFont } from './marqueeData'

// The font each choice stands for, for the settings' samples.
export const MARQUEE_FONT_FAMILY: Record<MarqueeFont, string> = {
  classic: "Georgia, 'Times New Roman', serif",
  deco: "'Limelight', Georgia, serif",
  neon: "'Monoton', sans-serif",
  poster: "'Bebas Neue', sans-serif",
  roman: "'Cinzel', Georgia, serif",
  retro: "'Righteous', sans-serif",
  bold: "'Abril Fatface', Georgia, serif",
  condensed: "'Oswald', sans-serif",
  elegant: "'Playfair Display', Georgia, serif",
  modern: "system-ui, -apple-system, 'Segoe UI', sans-serif",
}
