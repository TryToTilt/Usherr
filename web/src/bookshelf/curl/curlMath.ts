// The shape of a page being turned, worked out from where the finger is. The
// page wraps round a cylinder lying on it: flat up to the cylinder's axis, then
// over the top, then flat again on its way back. All lengths are in pixels with
// y going down, "forward" always means the free edge travels towards smaller x
// (a right-to-left book is mirrored by the drawing code, not here).

export interface Rect {
  x0: number
  y0: number
  x1: number
  y1: number
}

export interface Fold {
  ax: number // a point on the cylinder's axis
  ay: number
  nx: number // unit vector across the axis, pointing at the free edge
  ny: number
  r: number // the cylinder's radius
}

export interface TurnShape {
  leaf: Rect // the sheet that moves
  spread: boolean // an open book: the leaf is hinged at the spine and lands on the left page
  cx: number // the point of the free edge the finger holds
  cy: number
  dMax: number // how far that point travels by the end of a full turn
  rMax: number
  tilt: number // how much the fold leans by itself as the page lifts (grabbed high or low), radians
}

const smooth = (a: number, b: number, x: number) => {
  const t = Math.min(1, Math.max(0, (x - a) / (b - a)))
  return t * t * (3 - 2 * t)
}

// shapeFor describes a turn of the given leaf, grabbed at height gy. The top and
// bottom thirds hold the corner, so the fold leans; the middle holds the edge.
export function shapeFor(page: Rect, spread: boolean, gy: number): TurnShape {
  const leaf: Rect = spread ? { ...page, x0: (page.x0 + page.x1) / 2 } : page
  const w = leaf.x1 - leaf.x0
  const h = leaf.y1 - leaf.y0
  const k = Math.max(-1, Math.min(1, ((gy - leaf.y0) / h) * 2 - 1))
  const corner = Math.abs(k) > 1 / 3
  const cy = corner ? (k < 0 ? leaf.y0 : leaf.y1) : gy
  const rMax = Math.max(14, Math.min(w * 0.13, h * 0.1))
  // Single page: the sheet leaves altogether past the far edge. Open book: it lands.
  const dMax = spread ? 2 * w : 2 * (w + rMax + 8) - Math.PI * rMax
  // Across the page, n points at the corner held: up and over for a top corner.
  const tilt = corner ? (k < 0 ? -1 : 1) * (0.16 + 0.2 * (Math.abs(k) - 1 / 3) * 1.5) : k * 0.12
  return { leaf, spread, cx: leaf.x1, cy, dMax, rMax, tilt }
}

// arcFor: how far along the sheet the held point sits from the axis, given how
// far it has moved on screen. On the cylinder that is s - r sin(s / r), past the
// top the sheet comes back flat at twice the speed.
export function arcFor(moved: number, r: number): number {
  if (moved <= 0) return 0
  if (r <= 0.001) return moved / 2
  if (moved >= Math.PI * r) return (moved + Math.PI * r) / 2
  let s = Math.cbrt(6 * moved * r * r)
  for (let i = 0; i < 5; i++) {
    const f = s - r * Math.sin(s / r) - moved
    const df = 1 - Math.cos(s / r)
    if (df < 1e-5) break
    s = Math.max(0, Math.min(Math.PI * r, s - f / df))
  }
  return s
}

// foldAt places the fold for a held point moved by d along the turn and dy across it.
export function foldAt(shape: TurnShape, d: number, dy: number): Fold {
  const t = Math.min(1, Math.max(0, d / shape.dMax))
  const w = shape.leaf.x1 - shape.leaf.x0
  let r = shape.rMax * (0.4 + 0.6 * smooth(0, w * 0.4, d))
  // Landing on the left page the roll has to flatten out before the spine.
  if (shape.spread) r = Math.max(0.01, Math.min(r, ((2 * w - d) / Math.PI) * 0.9))
  // The lean: from the corner grabbed, gone again by the end of the turn.
  const settle = smooth(0, 0.14, t) * (1 - smooth(0.55, 1, t))
  const lean = shape.tilt * settle + Math.atan2(dy, Math.max(d, 60)) * (1 - t) * -1
  const limit = 0.62
  const angle = Math.max(-limit, Math.min(limit, lean))
  const nx = Math.cos(angle)
  const ny = Math.sin(angle)
  const s = arcFor(d, r)
  return { ax: shape.cx - nx * s, ay: shape.cy - ny * s, nx, ny, r }
}

// Whether a release keeps the turn going: a flick decides, otherwise being past
// halfway. Speed is in pixels a millisecond along the turn (positive: turning on).
export function finishes(progress: number, speed: number): boolean {
  if (speed > 0.35) return true
  if (speed < -0.35) return false
  return progress >= 0.5
}
