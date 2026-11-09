// Usherr Broadcast: what the remote's keys do in a reader, kept free of the
// page so it can be tested on its own (remoteReading.test.ts).
// Left and Right turn the page the way the book reads (a right-to-left manga
// goes on with Left), Down goes on, Up shows the bar (in a long scroll it goes
// back up), OK shows or hides the bar, Space and Page Down go on, Escape closes.

export type ReaderMove = { kind: 'turn'; d: 1 | -1 } | { kind: 'bar' } | { kind: 'toggle' } | { kind: 'close' } | null

export interface ReadingWay {
  rtl: boolean // pages go from right to left
  vertical: boolean // a long scroll (webtoon): there are no sides
}

/** What a key pressed on the page does. d = 1 is on in the book, -1 back. */
export function readerKey(key: string, way: ReadingWay): ReaderMove {
  const rtl = way.rtl && !way.vertical
  switch (key) {
    case 'ArrowRight':
    case 'Right':
      return { kind: 'turn', d: rtl ? -1 : 1 }
    case 'ArrowLeft':
    case 'Left':
      return { kind: 'turn', d: rtl ? 1 : -1 }
    case 'ArrowDown':
    case 'Down':
    case ' ':
    case 'PageDown':
      return { kind: 'turn', d: 1 }
    case 'PageUp':
      return { kind: 'turn', d: -1 }
    case 'ArrowUp':
    case 'Up':
      return way.vertical ? { kind: 'turn', d: -1 } : { kind: 'bar' }
    case 'Enter':
      return { kind: 'toggle' }
    case 'Escape':
    case 'Esc':
      return { kind: 'close' }
  }
  return null
}

/** The remote's media keys: Fast forward (or Next) goes on a page, Rewind (or Previous) back. */
export function remoteTurn(action: string): 1 | -1 | 0 {
  if (action === 'fastforward' || action === 'next') return 1
  if (action === 'rewind' || action === 'previous') return -1
  return 0
}
