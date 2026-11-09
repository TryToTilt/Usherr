// Usherr Broadcast: the hero's first trailer (components/TitleHero.tsx), for
// the Trailer button beside a title's main button (MoreSheet.tsx).
import { createContext } from 'react'

export const HeroTrailer = createContext<(() => void) | null>(null)
