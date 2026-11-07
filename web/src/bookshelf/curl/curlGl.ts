// Draws a page curl with WebGL: one rectangle over the reading area and a
// fragment shader that works out, for every pixel, which part of which page
// is seen there, so the curve stays perfectly smooth with no mesh to run out of
// detail. See curlMath.ts for the fold itself.
import type { Fold, Rect } from './curlMath'

const VERTEX = `
attribute vec2 aPos;
uniform vec2 uRes;
varying vec2 vP;
void main() {
  vP = vec2(aPos.x * 0.5 + 0.5, 0.5 - aPos.y * 0.5) * uRes;
  gl_Position = vec4(aPos, 0.0, 1.0);
}`

const FRAGMENT = `
precision highp float;
varying vec2 vP;
uniform sampler2D uFront, uOther;
uniform vec4 uPage, uLeaf;
uniform vec2 uA, uN;
uniform float uR, uSpine, uSpread, uMirror, uPx, uLift, uDark;
uniform vec3 uPaper, uBack;
const float PI = 3.14159265;

float box(vec2 p, vec4 r) {
  vec2 q = abs(p - (r.xy + r.zw) * 0.5) - (r.zw - r.xy) * 0.5;
  return length(max(q, 0.0)) + min(max(q.x, q.y), 0.0);
}
vec3 tex(sampler2D t, vec2 p) {
  vec2 q = (p - uPage.xy) / (uPage.zw - uPage.xy);
  if (uMirror > 0.5) q.x = 1.0 - q.x;
  return texture2D(t, q).rgb;
}
float inside(float sd) { return clamp(0.5 - sd, 0.0, 1.0); }

void main() {
  vec2 p = vP;
  if (uMirror > 0.5) p.x = uPage.x + uPage.z - p.x;
  float d = dot(p - uA, uN);
  float px = uPx;

  // The page underneath, and the shadows that fall on it.
  float inPage = inside(box(p, uPage));
  vec3 col = (uSpread > 0.5 && p.x < uSpine) ? tex(uFront, p) : tex(uOther, p);

  // How far outside the sheet a point of the flap above this pixel would be: the
  // soft shadow round the flap's free edges falls off with that distance.
  float asn = uR > 0.01 ? asin(clamp(d / uR, -1.0, 1.0)) : 0.0;
  float sBack = d < 0.0 ? PI * uR - d : uR * (PI - asn);
  vec2 back = p + uN * (sBack - d);
  float rim = clamp(uR - d + 0.5, 0.0, 1.0);
  float gone = box(back, uLeaf);
  float strength = mix(0.34, 0.62, uDark);
  float flapShadow = d < uR + 1.0 ? strength * exp(-max(gone, 0.0) / (uR * 0.55 + 7.0 * px)) * smoothstep(0.0, 26.0 * px, uR) : 0.0;
  // ... and the roll's own shadow, ahead of it, only where the roll really is.
  vec2 onRim = p - uN * max(d - uR, 0.0) + uN * (PI * 0.5 * uR - uR);
  float rollThere = clamp(1.0 - box(onRim, uLeaf) / (uR * 0.7 + 4.0 * px), 0.0, 1.0);
  float rollShadow = d >= uR - 1.0 ? (strength + 0.06) * exp(-(d - uR) / (uR * 0.95 + 5.0 * px)) * rollThere * smoothstep(0.0, 20.0 * px, uR) : 0.0;
  float shadow = 1.0 - (1.0 - flapShadow * step(d, uR)) * (1.0 - rollShadow);

  // An open book: the fold of the spine deepens while a leaf is in the air.
  float spine = uSpread > 0.5 ? uLift * mix(0.11, 0.3, uDark) * exp(-abs(p.x - uSpine) / (46.0 * px)) : 0.0;

  vec3 rgb = col * (1.0 - spine) * (1.0 - shadow) * inPage;
  float alpha = inPage + (1.0 - inPage) * shadow;

  if (d < uR + 1.0) {
    // The page's own face: flat, then rising round the inside of the roll.
    float s = d < 0.0 ? d : uR * asn;
    vec2 src = p + uN * (s - d);
    float cover = inside(box(src, uLeaf)) * rim;
    if (cover > 0.0) {
      float k = d < 0.0 ? 0.0 : asn / (PI * 0.5);
      // Light from the upper left: the rising paper catches it a little, then
      // turns away and falls into the shade of the roll above it.
      float lit = 1.0 + 0.05 * sin(k * PI) - 0.62 * k * k * k;
      float lead = 1.0 - 0.07 * exp(min(d, 0.0) / (uR * 1.2 + 4.0 * px)) * smoothstep(0.0, 20.0 * px, uR);
      vec3 c = tex(uFront, src) * lit * lead;
      c *= 1.0 - spine;
      c *= 1.0 - flapShadow;
      rgb = c * cover + rgb * (1.0 - cover);
      alpha = cover + alpha * (1.0 - cover);
    }

    // Over the top of the roll, and the back of the sheet coming down flat.
    float flap = inside(gone) * rim;
    if (flap > 0.0) {
      float phi = d < 0.0 ? PI : PI - asn; // PI/2 at the far side of the roll, PI on top
      float u = (phi - PI * 0.5) / (PI * 0.5);
      vec3 c;
      if (uSpread > 0.5) {
        // The sheet's other side is the next page, laid out for when it lands.
        c = tex(uOther, vec2(2.0 * uSpine - back.x, back.y));
      } else {
        // The back of a printed page: plain paper, the print just showing through.
        vec3 through = tex(uFront, back) - uPaper;
        c = uBack + through * mix(0.13, 0.16, uDark);
      }
      float lit = mix(0.5, 0.95, smoothstep(0.0, 0.6, u));
      // The gloss along the bend.
      float g = (u - 0.62) / 0.15;
      float gloss = exp(-g * g) * mix(0.2, 0.09, uDark);
      // The flat part, a little brighter again away from the roll.
      float settle = d < 0.0 ? 1.03 - 0.03 * exp(d / (uR * 2.0 + 8.0 * px)) : 1.0;
      c = c * lit * settle + vec3(gloss);
      rgb = c * flap + rgb * (1.0 - flap);
      alpha = flap + alpha * (1.0 - flap);
    }
  }
  gl_FragColor = vec4(rgb, alpha);
}`

export type PageTexture = WebGLTexture

export interface CurlFrame {
  page: Rect // device pixels, on the canvas
  fold: Fold // device pixels, in the book's own direction
  leaf: Rect
  spread: boolean
  mirror: boolean
  front: PageTexture
  other: PageTexture
  paper: [number, number, number]
  back: [number, number, number]
  dark: boolean
  lift: number // 0..1, the sheet off the page
  scale: number // device pixels per CSS pixel
}

export class CurlGl {
  readonly gl: WebGLRenderingContext
  private loc = new Map<string, WebGLUniformLocation | null>()

  // create returns null when WebGL (or precise enough numbers) isn't there.
  static create(canvas: HTMLCanvasElement): CurlGl | null {
    let gl: WebGLRenderingContext | null = null
    try {
      gl = canvas.getContext('webgl', { alpha: true, premultipliedAlpha: true, antialias: false, depth: false, stencil: false })
    } catch {
      return null
    }
    if (!gl) return null
    const high = gl.getShaderPrecisionFormat(gl.FRAGMENT_SHADER, gl.HIGH_FLOAT)
    if (!high || high.precision === 0) return null
    try {
      return new CurlGl(gl)
    } catch {
      return null
    }
  }

  private constructor(gl: WebGLRenderingContext) {
    this.gl = gl
    const program = gl.createProgram()!
    for (const [type, text] of [
      [gl.VERTEX_SHADER, VERTEX],
      [gl.FRAGMENT_SHADER, FRAGMENT],
    ] as const) {
      const sh = gl.createShader(type)!
      gl.shaderSource(sh, text)
      gl.compileShader(sh)
      if (!gl.getShaderParameter(sh, gl.COMPILE_STATUS)) throw new Error(gl.getShaderInfoLog(sh) ?? 'shader')
      gl.attachShader(program, sh)
    }
    gl.linkProgram(program)
    if (!gl.getProgramParameter(program, gl.LINK_STATUS)) throw new Error(gl.getProgramInfoLog(program) ?? 'program')
    gl.useProgram(program)
    gl.bindBuffer(gl.ARRAY_BUFFER, gl.createBuffer())
    gl.bufferData(gl.ARRAY_BUFFER, new Float32Array([-1, -1, 3, -1, -1, 3]), gl.STATIC_DRAW)
    const at = gl.getAttribLocation(program, 'aPos')
    gl.enableVertexAttribArray(at)
    gl.vertexAttribPointer(at, 2, gl.FLOAT, false, 0, 0)
    for (const name of ['uRes', 'uFront', 'uOther', 'uPage', 'uLeaf', 'uA', 'uN', 'uR', 'uSpine', 'uSpread', 'uMirror', 'uPx', 'uLift', 'uDark', 'uPaper', 'uBack']) {
      this.loc.set(name, gl.getUniformLocation(program, name))
    }
    gl.uniform1i(this.u('uFront'), 0)
    gl.uniform1i(this.u('uOther'), 1)
    gl.pixelStorei(gl.UNPACK_FLIP_Y_WEBGL, false)
    gl.pixelStorei(gl.UNPACK_PREMULTIPLY_ALPHA_WEBGL, false)
    gl.disable(gl.BLEND)
    gl.disable(gl.DEPTH_TEST)
  }

  private u(name: string) {
    return this.loc.get(name) ?? null
  }

  // upload turns a drawn page into a texture. It throws on a canvas the browser
  // considers tainted, which is how a browser that won't let us read an SVG
  // picture of the page shows itself.
  upload(source: TexImageSource): PageTexture {
    const gl = this.gl
    const tex = gl.createTexture()
    if (!tex) throw new Error('no texture')
    gl.activeTexture(gl.TEXTURE0)
    gl.bindTexture(gl.TEXTURE_2D, tex)
    gl.texParameteri(gl.TEXTURE_2D, gl.TEXTURE_MIN_FILTER, gl.LINEAR)
    gl.texParameteri(gl.TEXTURE_2D, gl.TEXTURE_MAG_FILTER, gl.LINEAR)
    gl.texParameteri(gl.TEXTURE_2D, gl.TEXTURE_WRAP_S, gl.CLAMP_TO_EDGE)
    gl.texParameteri(gl.TEXTURE_2D, gl.TEXTURE_WRAP_T, gl.CLAMP_TO_EDGE)
    gl.texImage2D(gl.TEXTURE_2D, 0, gl.RGBA, gl.RGBA, gl.UNSIGNED_BYTE, source)
    return tex
  }

  free(tex: PageTexture) {
    this.gl.deleteTexture(tex)
  }

  draw(f: CurlFrame) {
    const gl = this.gl
    const w = gl.drawingBufferWidth
    const h = gl.drawingBufferHeight
    gl.viewport(0, 0, w, h)
    gl.uniform2f(this.u('uRes'), w, h)
    gl.activeTexture(gl.TEXTURE0)
    gl.bindTexture(gl.TEXTURE_2D, f.front)
    gl.activeTexture(gl.TEXTURE1)
    gl.bindTexture(gl.TEXTURE_2D, f.other)
    gl.uniform4f(this.u('uPage'), f.page.x0, f.page.y0, f.page.x1, f.page.y1)
    gl.uniform4f(this.u('uLeaf'), f.leaf.x0, f.leaf.y0, f.leaf.x1, f.leaf.y1)
    gl.uniform2f(this.u('uA'), f.fold.ax, f.fold.ay)
    gl.uniform2f(this.u('uN'), f.fold.nx, f.fold.ny)
    gl.uniform1f(this.u('uR'), f.fold.r)
    gl.uniform1f(this.u('uSpine'), (f.page.x0 + f.page.x1) / 2)
    gl.uniform1f(this.u('uSpread'), f.spread ? 1 : 0)
    gl.uniform1f(this.u('uMirror'), f.mirror ? 1 : 0)
    gl.uniform1f(this.u('uPx'), f.scale)
    gl.uniform1f(this.u('uLift'), f.lift)
    gl.uniform1f(this.u('uDark'), f.dark ? 1 : 0)
    gl.uniform3f(this.u('uPaper'), f.paper[0], f.paper[1], f.paper[2])
    gl.uniform3f(this.u('uBack'), f.back[0], f.back[1], f.back[2])
    gl.drawArrays(gl.TRIANGLES, 0, 3)
  }
}
