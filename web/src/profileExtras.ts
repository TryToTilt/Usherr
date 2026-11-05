// Usherr: the profile editor's Advanced options as the form holds them (text
// as typed), and the Simple mode's quality choices.
import type { ProfileExtras, TierSize } from './api'

export interface SizeRow {
  min: string
  pref: string
  max: string
}

export interface ExtrasDraft {
  kind: '' | 'movies' | 'tv' | 'anime'
  sizes: Record<string, SizeRow>
  preferSmaller: boolean
  preferHevc: boolean
  minUpgradeGain: string
  upgradeDays: string
  preferGroups: string
  neverGroups: string
  audioLanguage: string
  wantSubs: boolean
  indexers: number[]
  protocol: '' | 'usenet' | 'torrent'
  minAge: string
  maxAge: string
}

const num = (n: number | undefined) => (n ? String(n) : '')
const groups = (s: string) =>
  s
    .split(/[,\n]/)
    .map((g) => g.trim())
    .filter(Boolean)

export function extrasDraft(e: ProfileExtras | undefined): ExtrasDraft {
  const x = e ?? {}
  const sizes: Record<string, SizeRow> = {}
  for (const [tier, s] of Object.entries(x.tierSizes ?? {})) sizes[tier] = { min: num(s.minGB), pref: num(s.preferredGB), max: num(s.maxGB) }
  return {
    kind: x.kind ?? '',
    sizes,
    preferSmaller: !!x.preferSmaller,
    preferHevc: !!x.preferHevc,
    minUpgradeGain: num(x.minUpgradeGain),
    upgradeDays: num(x.upgradeDays),
    preferGroups: (x.preferGroups ?? []).join(', '),
    neverGroups: (x.neverGroups ?? []).join(', '),
    audioLanguage: x.audioLanguage ?? '',
    wantSubs: !!x.wantSubs,
    indexers: x.indexers ?? [],
    protocol: x.protocol ?? '',
    minAge: num(x.minAgeMinutes),
    maxAge: num(x.maxAgeDays),
  }
}

const toNum = (s: string) => {
  const n = Number(s.trim())
  return s.trim() === '' || !Number.isFinite(n) ? 0 : n
}

export function extrasFrom(d: ExtrasDraft): ProfileExtras {
  const tierSizes: Record<string, TierSize> = {}
  for (const [tier, r] of Object.entries(d.sizes)) {
    const s: TierSize = { minGB: toNum(r.min), preferredGB: toNum(r.pref), maxGB: toNum(r.max) }
    if (s.minGB || s.preferredGB || s.maxGB) tierSizes[tier] = s
  }
  return {
    kind: d.kind,
    tierSizes,
    preferSmaller: d.preferSmaller,
    preferHevc: d.preferHevc,
    minUpgradeGain: Math.round(toNum(d.minUpgradeGain)),
    upgradeDays: Math.round(toNum(d.upgradeDays)),
    preferGroups: groups(d.preferGroups),
    neverGroups: groups(d.neverGroups),
    audioLanguage: d.audioLanguage,
    wantSubs: d.wantSubs,
    indexers: d.indexers,
    protocol: d.protocol,
    minAgeMinutes: Math.round(toNum(d.minAge)),
    maxAgeDays: Math.round(toNum(d.maxAge)),
  }
}

// What the form's sizes and numbers would be refused for, null when they are fine.
export function extrasProblem(d: ExtrasDraft, msg: { size: (tier: string) => string; order: (tier: string) => string; whole: (what: string) => string }): string | null {
  const bad = (s: string, max: number) => s.trim() !== '' && !(Number(s) >= 0 && Number(s) <= max)
  for (const [tier, r] of Object.entries(d.sizes)) {
    if (bad(r.min, 1000) || bad(r.pref, 1000) || bad(r.max, 1000)) return msg.size(tier)
    const [mn, pf, mx] = [toNum(r.min), toNum(r.pref), toNum(r.max)]
    if ((mx > 0 && mn > mx) || (pf > 0 && (pf < mn || (mx > 0 && pf > mx)))) return msg.order(tier)
  }
  const whole = (s: string, max: number) => s.trim() !== '' && !(/^\d+$/.test(s.trim()) && Number(s) <= max)
  if (whole(d.minUpgradeGain, 2000000)) return msg.whole('gain')
  if (whole(d.upgradeDays, 3650)) return msg.whole('days')
  if (whole(d.minAge, 10080)) return msg.whole('minAge')
  if (whole(d.maxAge, 20000)) return msg.whole('maxAge')
  return null
}

// Whether a profile has Advanced options the Simple mode doesn't show.
export function advancedCount(d: ExtrasDraft): number {
  let n = 0
  if (Object.values(d.sizes).some((r) => r.min || r.pref || r.max)) n++
  if (d.minUpgradeGain || d.upgradeDays) n++
  if (d.preferGroups || d.neverGroups) n++
  if (d.audioLanguage || d.wantSubs) n++
  if (d.indexers.length || d.protocol || d.minAge || d.maxAge) n++
  return n
}

// ---- Simple mode ----

export type SimpleQuality = 'save' | 'hd' | 'besthd' | '4k' | '4kfb'

export const SIMPLE_QUALITIES: Record<SimpleQuality, { allowed: string[]; cutoff: string; upgrades?: boolean }> = {
  save: { allowed: ['HDTV-720p', 'WEBDL-720p', 'Bluray-720p'], cutoff: 'Bluray-720p' },
  hd: { allowed: ['WEBDL-1080p', 'Bluray-1080p'], cutoff: 'Bluray-1080p' },
  besthd: { allowed: ['Bluray-1080p', 'Remux-1080p'], cutoff: 'Remux-1080p' },
  '4k': { allowed: ['WEBDL-2160p', 'Bluray-2160p', 'Remux-2160p'], cutoff: 'Remux-2160p' },
  '4kfb': { allowed: ['WEBDL-1080p', 'Bluray-1080p', 'WEBDL-2160p', 'Bluray-2160p', 'Remux-2160p'], cutoff: 'Remux-2160p', upgrades: true },
}

// The Simple choice a profile's qualities are, null when they are none of them.
export function simpleQualityOf(allowed: string[], customOrder: boolean): SimpleQuality | null {
  if (customOrder) return null
  const have = [...allowed].sort().join(',')
  for (const [k, q] of Object.entries(SIMPLE_QUALITIES)) if ([...q.allowed].sort().join(',') === have) return k as SimpleQuality
  return null
}

// Releases the Simple mode judges, to show what a profile would pick.
export const SAMPLE_RELEASES: { title: string; sizeGB: number }[] = [
  { title: 'Dune.Part.Two.2024.1080p.BluRay.DV.Atmos.x264-FraMeSToR', sizeGB: 14 },
  { title: 'Dune.Part.Two.2024.1080p.AMZN.WEB-DL.DDP5.1.H.264-FLUX', sizeGB: 8 },
  { title: 'Dune.Part.Two.2024.1080p.WEBRip.x265-YIFY', sizeGB: 2 },
  { title: 'Dune.Part.Two.2024.2160p.UHD.BluRay.REMUX.DV.HDR.Atmos-FGT', sizeGB: 96 },
  { title: 'Dune.Part.Two.2024.720p.BluRay.x264-SPARKS', sizeGB: 5 },
]

// The largest file slider: its last notch (151) is "no limit".
export const SLIDER_MAX = 150
export const sliderValue = (gb: number) => (gb <= 0 ? SLIDER_MAX + 1 : Math.min(gb, SLIDER_MAX))
export const sliderGB = (v: number) => (v > SLIDER_MAX ? 0 : v)
