// Usherr: a watch party's live chat stream (GET /api/home/showtimes/{id}/chat/events),
// one per party in this browser, shared by everything showing it: the chat,
// the chat over the film, the reactions and the phone page. A browser only
// keeps a few live streams open to one server, so they're not opened twice.
export interface PartyEvent {
  kind: string // new, edit, delete; react (a linked screening's reaction)
  message: unknown
}

type Sub = { onEvent: (ev: PartyEvent) => void; onOpen?: () => void }

const streams = new Map<number, { es: EventSource; subs: Set<Sub> }>()

// subscribeParty listens to a party's stream; onOpen runs each time it
// (re)connects, so a listener can read what it missed. It gives back the
// function that stops listening.
export function subscribeParty(id: number, onEvent: Sub['onEvent'], onOpen?: Sub['onOpen']): () => void {
  let s = streams.get(id)
  if (!s) {
    const es = new EventSource(`/api/home/showtimes/${id}/chat/events`, { withCredentials: true })
    const subs = new Set<Sub>()
    es.onmessage = (e) => {
      let ev: PartyEvent
      try {
        ev = JSON.parse(e.data as string) as PartyEvent
      } catch {
        return
      }
      for (const sub of subs) sub.onEvent(ev)
    }
    es.onopen = () => {
      for (const sub of subs) sub.onOpen?.()
    }
    s = { es, subs }
    streams.set(id, s)
  }
  const sub: Sub = { onEvent, onOpen }
  s.subs.add(sub)
  const mine = s
  return () => {
    mine.subs.delete(sub)
    if (mine.subs.size === 0) {
      mine.es.close()
      if (streams.get(id) === mine) streams.delete(id)
    }
  }
}
