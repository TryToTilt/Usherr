// Usherr Broadcast: the one menu, the Find page, the one kinds row and the
// settings in seven groups belong to the Broadcast look only. Classic, Neu and
// the lobby layout keep their own menus and pages exactly as they were.
// main.tsx (look.ts) sets data-look="broadcast" before the first paint.
//
// No React in this file, so it can be used from plain modules and tests.

export function broadcastOn(): boolean {
  return typeof document !== 'undefined' && document.documentElement.dataset.look === 'broadcast'
}
