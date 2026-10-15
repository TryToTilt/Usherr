// Home fork: what a film or episode download is, as small tags under its
// release name on Activity: 4K · Blu-ray Remux · HEVC · Dolby Vision ·
// TrueHD Atmos · English · FRAMESTOR.
import { t } from '../i18n'

export interface ReleaseSpecs {
  resolution?: string
  source?: string
  codec?: string
  hdr?: string
  audio?: string
  edition?: string
  language?: string
  subbed?: boolean
  group?: string
  repack?: boolean
  threeD?: boolean
}

function resolutionName(r: string): string {
  const v = r.toLowerCase()
  if (v === '2160p' || v === '4k' || v === 'uhd') return '4K'
  if (v === '1080p') return '1080p HD'
  if (v === '720p') return '720p HD'
  return r
}

function hdrName(h: string): string {
  const parts = h.split(/[\s,/+]+/).filter(Boolean)
  return h
    .replace(/\bDV\b/i, 'Dolby Vision')
    .replace(/\bDoVi\b/i, 'Dolby Vision')
    .trim() || parts.join(' ')
}

function codecName(c: string): string {
  const v = c.toLowerCase()
  if (v === 'x265' || v === 'h265' || v === 'h.265' || v === 'hevc') return 'HEVC'
  if (v === 'x264' || v === 'h264' || v === 'h.264' || v === 'avc') return 'H.264'
  if (v === 'av1') return 'AV1'
  return c
}

export default function ReleaseSpecTags({ specs }: { specs?: ReleaseSpecs }) {
  if (!specs) return null
  const tags: { text: string; tone: string; title?: string }[] = []
  if (specs.resolution) tags.push({ text: resolutionName(specs.resolution), tone: 'res' })
  if (specs.source) tags.push({ text: specs.source, tone: 'src' })
  if (specs.codec) tags.push({ text: codecName(specs.codec), tone: 'codec', title: specs.codec.toLowerCase() === 'av1' ? t('AV1 doesn’t play on the Shield') : undefined })
  if (specs.hdr) tags.push({ text: hdrName(specs.hdr), tone: 'hdr' })
  if (specs.threeD) tags.push({ text: '3D', tone: 'hdr' })
  if (specs.audio) tags.push({ text: specs.audio, tone: 'audio' })
  if (specs.language) tags.push({ text: specs.language, tone: 'lang' })
  if (specs.subbed) tags.push({ text: t('Subtitled'), tone: 'lang' })
  if (specs.edition) tags.push({ text: specs.edition, tone: 'edition' })
  if (specs.repack) tags.push({ text: t('Repack'), tone: 'edition' })
  if (tags.length === 0 && !specs.group) return null
  return (
    <div className="rel-specs" aria-label={t('What this release is')}>
      {tags.map((g) => (
        <span key={g.tone + g.text} className={`rel-spec tone-${g.tone}${g.tone === 'codec' && g.text === 'AV1' ? ' warn' : ''}`} title={g.title}>
          {g.text}
        </span>
      ))}
      {specs.group && <span className="rel-spec-group">{specs.group}</span>}
    </div>
  )
}
