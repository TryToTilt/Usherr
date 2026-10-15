"""Usherr: the icon in each colour choice (web/public/usherr-mark-<id>.png).

The wine and red of the icon take the palette's hue; the gold stays gold.
Uses the same palette list as tools/usherr/palettes.py.

    python tools/usherr/recolor_mark.py
"""

import colorsys
import os

from PIL import Image

from palettes import PALETTES


def recolor(src, hue, strength):
    out = src.copy()
    px = out.load()
    target = hue / 360
    k = strength / 100
    for y in range(out.height):
        for x in range(out.width):
            r, g, b, a = px[x, y]
            if a == 0:
                continue
            h, l, s = colorsys.rgb_to_hls(r / 255, g / 255, b / 255)
            deg = h * 360
            # wine, burgundy and the curtains' red: from magenta round to red-orange
            if s < 0.12 or not (deg >= 300 or deg < 22):
                continue
            # fade out towards the orange edge, so red meets gold smoothly
            w = 1.0 if deg >= 300 or deg < 12 else (22 - deg) / 10
            nh = target
            ns = s * (0.35 + 0.65 * k) if k < 0.5 else s
            nr, ng, nb = colorsys.hls_to_rgb(nh, l, ns)
            px[x, y] = (round(r + (nr * 255 - r) * w), round(g + (ng * 255 - g) * w), round(b + (nb * 255 - b) * w), a)
    return out


def main():
    here = os.path.dirname(os.path.abspath(__file__))
    pub = os.path.join(here, '..', '..', 'web', 'public')
    src = Image.open(os.path.join(pub, 'usherr-mark.png')).convert('RGBA')
    for pid, (hue, strength) in PALETTES.items():
        recolor(src, hue, strength).save(os.path.join(pub, f'usherr-mark-{pid}.png'), optimize=True)
    print('wrote', len(PALETTES), 'icons')


if __name__ == '__main__':
    main()
