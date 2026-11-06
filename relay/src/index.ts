// The Usherr Link Relay (ULR) on Cloudflare Workers with Durable Objects.
// The same protocol as the Go relay (internal/link/relay); written down in
// docs/home/link-protocol.md.
//
// The relay is blind: it routes sealed envelopes between public keys and
// never sees what's in them. It logs nothing about who talks to whom.
//
// How it's put together:
// - The Worker answers /v1/connect: it sends the challenge, checks the
//   hello's Ed25519 signature, then joins the connection to the Inbox of
//   that key and passes frames both ways.
// - One Inbox Durable Object per identity: it holds that key's live
//   connections (WebSocket Hibernation API, so an idle Usherr costs nothing),
//   its rate limit, and its queue for while it's offline (in DO storage, with
//   an alarm that drops what is more than 24 hours old).
// - One Box Durable Object per invite mailbox: who opened it and until when.

import { DurableObject } from 'cloudflare:workers'

export interface Env {
  INBOX: DurableObjectNamespace<Inbox>
  BOX: DurableObjectNamespace<Box>
}

// The protocol's limits.
const MAX_ENVELOPE = 16 * 1024 // decoded bytes
const MAX_FRAME = 32 * 1024 // a 16 KB envelope in base64, and the rest
const MAX_CONNS = 3 // live connections per identity
const QUEUE_TTL = 24 * 60 * 60 * 1000
const QUEUE_MSGS = 200
const QUEUE_BYTES = 1024 * 1024
const RATE = 20 // messages per second per sender
const BURST = 60
const POST_EVERY = 2000 // one invite.post per connection every 2 s…
const POST_BURST = 5 // …with a burst of 5
const BOX_LIFE = 24 * 60 * 60 * 1000
const BOXES_PER_KEY = 20
const HELLO_WAIT = 10_000
const HELLO_PREFIX = new TextEncoder().encode('ulr-v1-hello')
const MAX_FANOUT = 100 // recipients of one fanout frame
const RELAY_LEVEL = 2 // said in ready's v: this relay knows fanout
// A fanout counts as one message for every ten recipients, at least one.
const fanoutCost = (n: number) => Math.max(1, Math.ceil(n / 10))

// Frame fields, so frames with anything else are refused.
const FIELDS = new Set(['t', 'nonce', 'time', 'id', 'sig', 'v', 'to', 'dests', 'from', 'env', 'mid', 'code', 'box', 'exp', 'queued'])

interface Frame {
  t: string
  nonce?: string
  time?: number
  id?: string
  sig?: string
  v?: number
  to?: string
  dests?: string[]
  from?: string
  env?: string
  mid?: string
  code?: string
  box?: string
  exp?: number
  queued?: number
}

function parseFrame(text: string): Frame | null {
  if (text.length > MAX_FRAME) return null
  let v: unknown
  try {
    v = JSON.parse(text)
  } catch {
    return null
  }
  if (typeof v !== 'object' || v === null || Array.isArray(v)) return null
  const o = v as Record<string, unknown>
  for (const k of Object.keys(o)) if (!FIELDS.has(k)) return null
  if (typeof o.t !== 'string') return null
  for (const k of ['nonce', 'id', 'sig', 'to', 'from', 'env', 'mid', 'code', 'box']) {
    if (o[k] !== undefined && typeof o[k] !== 'string') return null
  }
  for (const k of ['time', 'v', 'exp', 'queued']) {
    if (o[k] !== undefined && (typeof o[k] !== 'number' || !Number.isFinite(o[k]))) return null
  }
  if (o.dests !== undefined && (!Array.isArray(o.dests) || o.dests.length > 1000 || o.dests.some((d) => typeof d !== 'string'))) return null
  return o as unknown as Frame
}

const enc = (f: Frame) => JSON.stringify(f)

function b64(bytes: Uint8Array): string {
  let s = ''
  for (const b of bytes) s += String.fromCharCode(b)
  return btoa(s)
}

function unb64(s: string): Uint8Array | null {
  try {
    const bin = atob(s)
    const out = new Uint8Array(bin.length)
    for (let i = 0; i < bin.length; i++) out[i] = bin.charCodeAt(i)
    return out
  } catch {
    return null
  }
}

// An identity: base64url (no padding) of a 32-byte Ed25519 key.
function idBytes(id: unknown): Uint8Array | null {
  if (typeof id !== 'string' || id.length !== 43 || !/^[A-Za-z0-9_-]+$/.test(id)) return null
  const b = unb64(id.replace(/-/g, '+').replace(/_/g, '/') + '=')
  return b && b.length === 32 ? b : null
}

const validBox = (b: unknown): b is string => typeof b === 'string' && /^[0-9a-f]{64}$/.test(b)
const validMid = (m: unknown): m is string => typeof m === 'string' && /^[A-Za-z0-9_-]{1,64}$/.test(m)

function validEnv(env: unknown): env is string {
  if (typeof env !== 'string' || env.length === 0 || env.length > Math.ceil(MAX_ENVELOPE / 3) * 4) return false
  if (!/^[A-Za-z0-9+/]*={0,2}$/.test(env) || env.length % 4 !== 0) return false
  const b = unb64(env)
  return !!b && b.length > 0 && b.length <= MAX_ENVELOPE
}

async function verifyHello(id: string, sig: string, nonce: Uint8Array): Promise<boolean> {
  const pub = idBytes(id)
  const s = unb64(sig)
  if (!pub || !s || s.length !== 64) return false
  try {
    const key = await crypto.subtle.importKey('raw', pub, { name: 'Ed25519' }, false, ['verify'])
    const msg = new Uint8Array(HELLO_PREFIX.length + nonce.length)
    msg.set(HELLO_PREFIX)
    msg.set(nonce, HELLO_PREFIX.length)
    return await crypto.subtle.verify({ name: 'Ed25519' }, key, s, msg)
  } catch {
    return false
  }
}

const textOf = (data: string | ArrayBuffer) => (typeof data === 'string' ? data : null)

export default {
  async fetch(request: Request, env: Env): Promise<Response> {
    const url = new URL(request.url)
    if (url.pathname === '/' || url.pathname === '/healthz') {
      return new Response('Usherr Link Relay\n', { headers: { 'content-type': 'text/plain; charset=utf-8' } })
    }
    if (url.pathname !== '/v1/connect') return new Response('Not found\n', { status: 404 })
    if (request.headers.get('Upgrade')?.toLowerCase() !== 'websocket') {
      return new Response('This address is for Usherrs (a WebSocket).\n', { status: 426 })
    }
    const pair = new WebSocketPair()
    const [client, server] = Object.values(pair)
    server.accept()
    connect(server, env)
    return new Response(null, { status: 101, webSocket: client })
  },
} satisfies ExportedHandler<Env>

// connect runs the challenge and hello, then joins the connection to its
// key's Inbox and passes frames both ways.
function connect(ws: WebSocket, env: Env) {
  const nonce = crypto.getRandomValues(new Uint8Array(32))
  let state: 'hello' | 'joining' | 'open' | 'closed' = 'hello'
  let upstream: WebSocket | null = null
  const early: string[] = [] // frames sent while joining the Inbox
  const close = () => {
    if (state === 'closed') return
    state = 'closed'
    try {
      ws.close(1000, '')
    } catch {}
    try {
      upstream?.close(1000, '')
    } catch {}
  }
  const timer = setTimeout(() => state === 'hello' && close(), HELLO_WAIT)
  ws.send(enc({ t: 'challenge', nonce: b64(nonce), time: Date.now() }))

  ws.addEventListener('message', async (ev) => {
    const text = textOf(ev.data as string | ArrayBuffer)
    if (text === null || text.length > MAX_FRAME) {
      close()
      return
    }
    if (state === 'open') {
      upstream?.send(text)
      return
    }
    if (state === 'joining') {
      if (early.length < 64) early.push(text)
      else close()
      return
    }
    if (state !== 'hello') return
    const f = parseFrame(text)
    if (!f || f.t !== 'hello' || f.v !== 1 || typeof f.id !== 'string' || typeof f.sig !== 'string' || !(await verifyHello(f.id, f.sig, nonce))) {
      close()
      return
    }
    clearTimeout(timer)
    state = 'joining'
    const stub = env.INBOX.get(env.INBOX.idFromName(f.id))
    let res: Response
    try {
      res = await stub.fetch('https://inbox/attach', { headers: { Upgrade: 'websocket', 'X-ULR-Id': f.id } })
    } catch {
      close()
      return
    }
    if (res.status === 429 || !res.webSocket) {
      try {
        ws.send(enc({ t: 'err', code: 'busy' }))
      } catch {}
      close()
      return
    }
    upstream = res.webSocket
    upstream.accept()
    upstream.addEventListener('message', (e) => {
      if (state !== 'closed') ws.send(e.data as string)
    })
    upstream.addEventListener('close', close)
    upstream.addEventListener('error', close)
    if ((state as string) === 'closed') {
      close()
      return
    }
    state = 'open'
    for (const t of early.splice(0)) upstream.send(t)
  })
  ws.addEventListener('close', close)
  ws.addEventListener('error', close)
}

// What a connection remembers across hibernation.
interface Attachment {
  id: string
  postTokens: number
  postAt: number
}

interface QueueMeta {
  count: number
  bytes: number
}

interface Queued {
  f: string
  exp: number
}

// Inbox: one per identity.
export class Inbox extends DurableObject<Env> {
  private tokens = BURST
  private tokensAt = Date.now()

  constructor(ctx: DurableObjectState, env: Env) {
    super(ctx, env)
    // Keepalive pings are answered without waking the object up.
    ctx.setWebSocketAutoResponse(new WebSocketRequestResponsePair('{"t":"ping"}', '{"t":"pong"}'))
  }

  async fetch(request: Request): Promise<Response> {
    const url = new URL(request.url)
    if (url.pathname === '/attach') return this.attach(request)
    if (url.pathname === '/deliver' && request.method === 'POST') {
      const code = await this.deliver(await request.text())
      return Response.json({ code })
    }
    return new Response('Not found', { status: 404 })
  }

  private async attach(request: Request): Promise<Response> {
    const id = request.headers.get('X-ULR-Id') ?? ''
    if (!idBytes(id)) return new Response('bad', { status: 400 })
    if (this.ctx.getWebSockets().length >= MAX_CONNS) return new Response('busy', { status: 429 })
    const pair = new WebSocketPair()
    const [client, server] = Object.values(pair)
    this.ctx.acceptWebSocket(server)
    server.serializeAttachment({ id, postTokens: POST_BURST, postAt: Date.now() } satisfies Attachment)
    // Whatever waited is handed over, and nothing of it is kept.
    const waiting = await this.takeQueue()
    server.send(enc({ t: 'ready', ...(waiting.length ? { queued: waiting.length } : {}), v: RELAY_LEVEL }))
    for (const f of waiting) server.send(f)
    return new Response(null, { status: 101, webSocket: client })
  }

  private allow(n = 1): boolean {
    const now = Date.now()
    this.tokens = Math.min(BURST, this.tokens + ((now - this.tokensAt) / 1000) * RATE)
    this.tokensAt = now
    if (this.tokens < n) return false
    this.tokens -= n
    return true
  }

  async webSocketMessage(ws: WebSocket, data: string | ArrayBuffer): Promise<void> {
    const text = textOf(data)
    const f = text === null ? null : parseFrame(text)
    const reply = (mid: string | undefined, code: string) => {
      if (code) ws.send(enc({ t: 'err', ...(mid ? { mid } : {}), code }))
      else if (mid) ws.send(enc({ t: 'ack', mid }))
    }
    if (!f) {
      reply(undefined, 'bad')
      return
    }
    if (f.mid !== undefined && !validMid(f.mid)) {
      reply(undefined, 'bad')
      return
    }
    const me = (ws.deserializeAttachment() as Attachment).id
    switch (f.t) {
      case 'ping':
        ws.send(enc({ t: 'pong' }))
        return
      case 'send': {
        if (!idBytes(f.to) || !f.mid) return reply(f.mid, 'unknown')
        if (!validEnv(f.env)) return reply(f.mid, 'size')
        if (!this.allow()) return reply(f.mid, 'rate')
        const code = await this.deliverTo(f.to!, enc({ t: 'msg', from: me, env: f.env, mid: f.mid }))
        return reply(f.mid, code)
      }
      case 'fanout': {
        // One envelope (sealed with a screening key) for up to 100
        // identities: each gets an ordinary msg frame, live or queued.
        if (!f.mid) return reply(undefined, 'bad')
        const dests = f.dests ?? []
        if (dests.length > MAX_FANOUT) return reply(f.mid, 'size')
        if (dests.length === 0 || new Set(dests).size !== dests.length || !dests.every((d) => idBytes(d))) return reply(f.mid, 'bad')
        if (!validEnv(f.env)) return reply(f.mid, 'size')
        if (!this.allow(fanoutCost(dests.length))) return reply(f.mid, 'rate')
        const frame = enc({ t: 'msg', from: me, env: f.env, mid: f.mid })
        const codes = await Promise.all(dests.map((to) => this.deliverTo(to, frame).catch(() => 'full')))
        return reply(f.mid, codes.find((c) => c) ?? '') // the others still got it
      }
      case 'invite.open': {
        if (!validBox(f.box)) return reply(f.mid, 'bad')
        if (!this.allow()) return reply(f.mid, 'rate')
        const now = Date.now()
        const boxes = ((await this.ctx.storage.get<Record<string, number>>('boxes')) ?? {}) as Record<string, number>
        for (const [b, exp] of Object.entries(boxes)) if (exp <= now) delete boxes[b]
        if (!(f.box in boxes) && Object.keys(boxes).length >= BOXES_PER_KEY) return reply(f.mid, 'full')
        const res = await this.box(f.box).fetch('https://box/open', { method: 'POST', body: JSON.stringify({ box: f.box, owner: me, exp: f.exp ?? 0 }) })
        const { code, exp } = (await res.json()) as { code: string; exp?: number }
        if (!code && exp) boxes[f.box] = exp
        await this.ctx.storage.put('boxes', boxes)
        return reply(f.mid, code)
      }
      case 'invite.close': {
        if (!validBox(f.box)) return reply(f.mid, 'bad')
        await this.box(f.box).fetch('https://box/close', { method: 'POST', body: JSON.stringify({ owner: me }) })
        const boxes = ((await this.ctx.storage.get<Record<string, number>>('boxes')) ?? {}) as Record<string, number>
        delete boxes[f.box]
        await this.ctx.storage.put('boxes', boxes)
        return reply(f.mid, '')
      }
      case 'invite.post': {
        // Per connection: a guessed code is useless (100 bits), but posting
        // is still kept slow.
        const a = ws.deserializeAttachment() as Attachment
        const now = Date.now()
        a.postTokens = Math.min(POST_BURST, a.postTokens + (now - a.postAt) / POST_EVERY)
        a.postAt = now
        if (a.postTokens < 1) {
          ws.serializeAttachment(a)
          return reply(f.mid, 'rate')
        }
        a.postTokens -= 1
        ws.serializeAttachment(a)
        if (!validBox(f.box)) return reply(f.mid, 'unknown')
        if (!validEnv(f.env)) return reply(f.mid, 'size')
        const res = await this.box(f.box).fetch('https://box/post', { method: 'POST', body: JSON.stringify({ box: f.box, from: me, env: f.env }) })
        const { code } = (await res.json()) as { code: string }
        return reply(f.mid, code)
      }
      default:
        return reply(f.mid, 'unknown')
    }
  }

  async webSocketClose(ws: WebSocket): Promise<void> {
    try {
      ws.close(1000, '')
    } catch {}
  }

  async webSocketError(ws: WebSocket): Promise<void> {
    try {
      ws.close(1011, '')
    } catch {}
  }

  private box(name: string) {
    return this.env.BOX.get(this.env.BOX.idFromName(name))
  }

  private async deliverTo(to: string, frame: string): Promise<string> {
    const res = await this.env.INBOX.get(this.env.INBOX.idFromName(to)).fetch('https://inbox/deliver', { method: 'POST', body: frame })
    return ((await res.json()) as { code: string }).code
  }

  // deliver hands a frame to every live connection, or queues it.
  private async deliver(frame: string): Promise<string> {
    if (frame.length > MAX_FRAME) return 'size'
    const live = this.ctx.getWebSockets()
    if (live.length > 0) {
      for (const ws of live) {
        try {
          ws.send(frame)
        } catch {}
      }
      return ''
    }
    await this.prune()
    const meta = (await this.ctx.storage.get<QueueMeta>('qmeta')) ?? { count: 0, bytes: 0 }
    if (meta.count >= QUEUE_MSGS || meta.bytes + frame.length > QUEUE_BYTES) return 'full'
    const now = Date.now()
    const key = `q:${String(now).padStart(15, '0')}:${crypto.randomUUID()}`
    await this.ctx.storage.put({ [key]: { f: frame, exp: now + QUEUE_TTL } satisfies Queued, qmeta: { count: meta.count + 1, bytes: meta.bytes + frame.length } })
    if ((await this.ctx.storage.getAlarm()) === null) await this.ctx.storage.setAlarm(now + QUEUE_TTL)
    return ''
  }

  private async takeQueue(): Promise<string[]> {
    const items = await this.ctx.storage.list<Queued>({ prefix: 'q:' })
    const now = Date.now()
    const out: string[] = []
    for (const q of items.values()) if (q.exp > now) out.push(q.f)
    if (items.size) await this.ctx.storage.delete([...items.keys()])
    await this.ctx.storage.delete('qmeta')
    return out
  }

  // prune drops what waited more than 24 hours, and recounts.
  private async prune(): Promise<void> {
    const items = await this.ctx.storage.list<Queued>({ prefix: 'q:' })
    const now = Date.now()
    const gone: string[] = []
    let count = 0
    let bytes = 0
    let next = 0
    for (const [k, q] of items) {
      if (q.exp <= now) gone.push(k)
      else {
        count++
        bytes += q.f.length
        if (!next || q.exp < next) next = q.exp
      }
    }
    for (let i = 0; i < gone.length; i += 128) await this.ctx.storage.delete(gone.slice(i, i + 128))
    if (count) await this.ctx.storage.put('qmeta', { count, bytes })
    else await this.ctx.storage.delete('qmeta')
    if (next) await this.ctx.storage.setAlarm(next)
  }

  async alarm(): Promise<void> {
    await this.prune()
  }
}

interface BoxState {
  owner: string
  exp: number
}

// Box: one per invite mailbox. The relay knows only the code's hash.
export class Box extends DurableObject<Env> {
  async fetch(request: Request): Promise<Response> {
    const path = new URL(request.url).pathname
    const body = (await request.json()) as { box?: string; owner?: string; exp?: number; from?: string; env?: string }
    const now = Date.now()
    let cur = await this.ctx.storage.get<BoxState>('box')
    if (cur && cur.exp <= now) {
      await this.ctx.storage.deleteAll()
      cur = undefined
    }
    switch (path) {
      case '/open': {
        let until = now + BOX_LIFE
        if (body.exp && body.exp > 0 && body.exp < until) until = body.exp
        if (until <= now || !body.owner) return Response.json({ code: 'bad' })
        if (cur) {
          if (cur.owner !== body.owner) return Response.json({ code: 'bad' })
          until = Math.min(until, cur.exp) // never longer than first asked
        }
        await this.ctx.storage.put('box', { owner: body.owner, exp: until } satisfies BoxState)
        await this.ctx.storage.setAlarm(until)
        return Response.json({ code: '', exp: until })
      }
      case '/close':
        if (cur && cur.owner === body.owner) await this.ctx.storage.deleteAll()
        return Response.json({ code: '' })
      case '/post': {
        if (!cur || !body.box || !body.from || !body.env) return Response.json({ code: 'unknown' })
        const frame = enc({ t: 'invite.msg', box: body.box, from: body.from, env: body.env })
        const res = await this.env.INBOX.get(this.env.INBOX.idFromName(cur.owner)).fetch('https://inbox/deliver', { method: 'POST', body: frame })
        return Response.json(await res.json())
      }
    }
    return new Response('Not found', { status: 404 })
  }

  async alarm(): Promise<void> {
    await this.ctx.storage.deleteAll()
  }
}
