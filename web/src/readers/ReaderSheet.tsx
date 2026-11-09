// Usherr Broadcast: the reader settings as a sheet a remote can use (the
// mock-up's "Reader" sheet): one row per setting, Left and Right change it,
// Back or Done close it. The same settings as the panel (ReaderSettings.tsx),
// saved the same way; each reader adds its own rows.
import Sheet, { type SheetRow } from '../broadcast/Sheet'
import { t } from '../i18n'
import type { LayoutChoice } from './ReaderSettings'
import { progressParts, type ReaderOptions } from './readerOptions'

const onOff = () => [
  { id: 'on', label: t('On') },
  { id: 'off', label: t('Off') },
]

/** Reading direction: right to left or left to right. */
export function directionRow(direction: 'ltr' | 'rtl', onDirection: (d: 'ltr' | 'rtl') => void, disabled = false): SheetRow {
  return {
    key: 'direction',
    label: t('Reading direction'),
    choices: [
      { id: 'rtl', label: t('Right to left') },
      { id: 'ltr', label: t('Left to right') },
    ],
    value: direction,
    onChoose: (v) => onDirection(v === 'ltr' ? 'ltr' : 'rtl'),
    disabled,
  }
}

/** How pages show: the reader's own layouts. */
export function layoutRow(layouts: LayoutChoice[], layout: string, onLayout: (id: string) => void): SheetRow {
  return { key: 'layout', label: t('How pages show'), choices: layouts.map((l) => ({ id: l.id, label: l.label, disabled: l.disabled })), value: layout, onChoose: onLayout }
}

/** The options both picture readers share (readerOptions.ts). */
export function sharedRows(opts: ReaderOptions, onOpts: (patch: Partial<ReaderOptions>) => void): SheetRow[] {
  const parts = progressParts(opts)
  const rows: SheetRow[] = [
    { key: 'header', label: t('The bar at the top'), choices: [{ id: 'on', label: t('Shown') }, { id: 'off', label: t('Hidden') }], value: opts.header ? 'on' : 'off', onChoose: (v) => onOpts({ header: v === 'on' }) },
    {
      key: 'progress',
      label: t('Progress bar'),
      choices: [
        { id: 'normal', label: t('Normal') },
        { id: 'thin', label: t('Thin line') },
        { id: 'hidden', label: t('Hidden') },
      ],
      value: opts.progress,
      onChoose: (v) => onOpts({ progress: v as ReaderOptions['progress'] }),
    },
  ]
  if (parts.bar)
    rows.push({
      key: 'side',
      label: t('Where the progress bar goes'),
      choices: [
        { id: 'bottom', label: t('Bottom') },
        { id: 'left', label: t('Left') },
        { id: 'right', label: t('Right') },
      ],
      value: opts.side,
      onChoose: (v) => onOpts({ side: v as ReaderOptions['side'] }),
    })
  else rows.push({ key: 'number', label: t('Show the page number'), choices: onOff(), value: opts.pageNumber ? 'on' : 'off', onChoose: (v) => onOpts({ pageNumber: v === 'on' }) })
  rows.push(
    { key: 'grey', label: t('Greyscale'), choices: onOff(), value: opts.greyscale ? 'on' : 'off', onChoose: (v) => onOpts({ greyscale: v === 'on' }) },
    { key: 'dim', label: t('Dim the pages'), choices: onOff(), value: opts.dim ? 'on' : 'off', onChoose: (v) => onOpts({ dim: v === 'on' }) },
    {
      key: 'background',
      label: t('Background'),
      choices: [
        { id: 'reader', label: t('Dark') },
        { id: 'theme', label: t('Usherr’s theme') },
        { id: 'white', label: t('White') },
        { id: 'black', label: t('Black') },
      ],
      value: opts.background,
      onChoose: (v) => onOpts({ background: v as ReaderOptions['background'] }),
    },
  )
  return rows
}

export default function ReaderSheet({ eyebrow, rows, onClose }: { eyebrow?: string; rows: SheetRow[]; onClose: () => void }) {
  return <Sheet place="side" eyebrow={eyebrow} title={t('Reader')} text={t('Left and Right change each one. Kept for this series.')} rows={rows} onClose={onClose} walk="reader-sheet" />
}
