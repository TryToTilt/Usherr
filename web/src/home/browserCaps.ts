// Usherr: what this browser can decode, measured on this device: sent to the
// server, which picks how a film (Player) or a pre-show clip (PreshowRun) plays.
export function browserCaps() {
  const ms = typeof MediaSource !== 'undefined' ? MediaSource : null
  const can = (type: string) => {
    try {
      return !!ms && ms.isTypeSupported(type)
    } catch {
      return false
    }
  }
  const v = document.createElement('video')
  const hdr = typeof window.matchMedia === 'function' && window.matchMedia('(dynamic-range: high)').matches
  return {
    hevc: can('video/mp4; codecs="hvc1.2.4.L153.B0"') || can('video/mp4; codecs="hev1.2.4.L153.B0"'),
    av1: can('video/mp4; codecs="av01.0.12M.10"'),
    vp9: can('video/webm; codecs="vp9"'),
    dv: can('video/mp4; codecs="dvh1.05.06"') || can('video/mp4; codecs="dvhe.05.06"'),
    hdr,
    ac3: can('audio/mp4; codecs="ac-3"'),
    eac3: can('audio/mp4; codecs="ec-3"'),
    opus: can('audio/mp4; codecs="opus"'),
    flac: can('audio/mp4; codecs="flac"'),
    mkv: v.canPlayType('video/x-matroska') !== '',
    nativeHls: v.canPlayType('application/vnd.apple.mpegurl') !== '',
  }
}
