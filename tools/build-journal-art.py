#!/usr/bin/env python3
"""Draws the journal's own artwork: parchment, leather, title and chapter bars, tabs, the medallion and the
settings preview scene. Everything is computed here from nothing but numbers, so the files carry the project's
licence. Run from the repository root; needs only the Python standard library."""
import math
import random
import struct
from pathlib import Path

OUT = Path(__file__).resolve().parent.parent / "looking-for-guidance" / "Media"


def lattice(seed, width, height):
    rnd = random.Random(seed)
    return [[rnd.random() for _ in range(width)] for _ in range(height)]


def noise(grid, x, y):
    """Smooth value noise that repeats at the grid's edges, so textures tile."""
    h, w = len(grid), len(grid[0])
    x0, y0 = math.floor(x), math.floor(y)
    fx, fy = x - x0, y - y0
    fx, fy = fx * fx * (3 - 2 * fx), fy * fy * (3 - 2 * fy)
    a, b = grid[y0 % h][x0 % w], grid[y0 % h][(x0 + 1) % w]
    c, d = grid[(y0 + 1) % h][x0 % w], grid[(y0 + 1) % h][(x0 + 1) % w]
    return (a + (b - a) * fx) * (1 - fy) + (c + (d - c) * fx) * fy


def cloud(seed, cells_x, cells_y, octaves=4):
    grids = [lattice(seed + o, cells_x << o, cells_y << o) for o in range(octaves)]

    def at(u, v):
        total, weight = 0.0, 0.0
        for o, grid in enumerate(grids):
            amp = 0.5 ** o
            total += amp * noise(grid, u * (cells_x << o), v * (cells_y << o))
            weight += amp
        return total / weight

    return at


def mix(a, b, t):
    t = min(1.0, max(0.0, t))
    return tuple(a[i] + (b[i] - a[i]) * t for i in range(len(a)))


def save(name, width, height, pixel, alpha=True):
    """pixel(x, y) returns (r, g, b[, a]) in 0..255."""
    depth, flags = (32, 0x28) if alpha else (24, 0x20)
    data = bytearray(struct.pack("<BBBHHBHHHHBB", 0, 0, 2, 0, 0, 0, 0, 0, width, height, depth, flags))
    for y in range(height):
        for x in range(width):
            c = [min(255, max(0, round(v))) for v in pixel(x, y)]
            data += bytes((c[2], c[1], c[0], c[3] if len(c) > 3 else 255) if alpha else (c[2], c[1], c[0]))
    path = OUT / name
    path.parent.mkdir(parents=True, exist_ok=True)
    path.write_bytes(data)


def paper():
    w, h = 512, 128
    tone, fibre, speck = cloud(11, 4, 1), cloud(23, 64, 2, 2), cloud(37, 32, 8, 3)

    def pixel(x, y):
        u, v = x / w, y / h
        shade = (tone(u, v) - 0.5) * 34 + (fibre(u, v) - 0.5) * 10 + (speck(u, v) - 0.5) * 12
        return tuple(c + shade for c in mix((203, 168, 110), (216, 184, 128), tone(u + 0.37, v))) + (255,)

    save("Journal/Paper.tga", w, h, pixel)


def leather():
    w, h = 256, 64
    hide, grain = cloud(51, 6, 2), cloud(67, 64, 16, 3)

    def pixel(x, y):
        u, v = x / w, y / h
        shade = (hide(u, v) - 0.5) * 20 + (grain(u, v) - 0.5) * 16
        return tuple(c + shade for c in (58, 38, 24)) + (255,)

    save("Journal/Leather.tga", w, h, pixel)


def rail(y, h, inset):
    """Brightness of a thin raised rail near the top and bottom edges."""
    for edge in (inset, h - 1 - inset):
        d = y - edge
        if d == 0:
            return 1.0
        if d == 1:
            return 0.45
        if d == -1:
            return -0.5
    return 0.0


def title():
    w, h = 256, 64
    grain = cloud(83, 24, 2, 3)

    def pixel(x, y):
        u, v = x / w, y / h
        base = mix((70, 44, 26), (38, 24, 15), abs(v - 0.42) * 1.9)
        base = tuple(c + (grain(u, v) - 0.5) * 18 for c in base)
        gold = rail(y, h, 5)
        if gold > 0:
            base = mix(base, (214, 172, 84), gold)
        elif gold < 0:
            base = mix(base, (14, 9, 6), -gold)
        return base + (255,)

    save("Journal/Title.tga", w, h, pixel)


def chapter():
    w, h = 512, 64
    grain = cloud(97, 40, 2, 3)

    def pixel(x, y):
        u, v = x / w, y / h
        edge = min(x, y, w - 1 - x, h - 1 - y)
        base = mix((82, 50, 30), (56, 34, 21), v)
        base = tuple(c + (grain(u, v) - 0.5) * 22 for c in base)
        if edge < 2:
            base = (20, 13, 8)
        elif edge < 4:
            base = mix(base, (120, 82, 50) if y < h / 2 else (24, 15, 10), 0.7)
        gold = rail(y, h, 9)
        if gold > 0 and 12 < x < w - 13:
            base = mix(base, (190, 148, 70), gold * 0.8)
        return base + (255,)

    save("Journal/Chapter.tga", w, h, pixel)


def tab(name, face, seed):
    size = 64
    stone = cloud(seed, 8, 8, 3)

    def pixel(x, y):
        edge = min(x, y, size - 1 - x, size - 1 - y)
        corner = (min(x, size - 1 - x) + min(y, size - 1 - y)) < 7
        if corner:
            return (0, 0, 0, 0)
        if edge < 3 or (min(x, size - 1 - x) + min(y, size - 1 - y)) < 10:
            return (16, 14, 14, 255)
        base = tuple(c + (stone(x / size, y / size) - 0.5) * 22 for c in face)
        if edge < 7:
            lit = y < size / 2 if min(y, size - 1 - y) <= min(x, size - 1 - x) else x < size / 2
            base = mix(base, (255, 255, 255) if lit else (0, 0, 0), 0.28 if lit else 0.42)
        else:
            base = mix(base, (255, 255, 255), max(0.0, 0.10 - y / size * 0.16))
        return base + (255,)

    save(name, size, size, pixel)


def medallion():
    size, mid = 128, 63.5

    def sample(x, y):
        dx, dy = x - mid, y - mid
        r = math.hypot(dx, dy)
        if r > 58:
            return (0, 0, 0, 0)
        if r > 46:
            lit = (-dx - dy) / (r * 1.4142)
            ring = mix((150, 104, 40), (246, 210, 122), lit * 0.5 + 0.5)
            return mix(ring, (60, 38, 14), max(0.0, abs(r - 52) - 4) / 2) + (255,)
        disc = mix((30, 44, 72), (12, 18, 34), r / 46)
        # A four-pointed compass star with a shorter diagonal cross behind it.
        ax, ay = abs(dx), abs(dy)
        if ax / 9 + ay / 36 <= 1 or ax / 36 + ay / 9 <= 1:
            return mix((250, 218, 132), (176, 126, 52), (ax + ay) / 40) + (255,)
        px, py = abs(dx + dy) / 1.4142, abs(dx - dy) / 1.4142
        if px / 5 + py / 20 <= 1 or px / 20 + py / 5 <= 1:
            return (146, 108, 56, 255)
        return disc + (255,)

    def pixel(x, y):
        acc = [0.0, 0.0, 0.0, 0.0]
        for sy in range(3):
            for sx in range(3):
                c = sample(x + (sx - 1) / 3, y + (sy - 1) / 3)
                a = c[3] / 255
                acc = [acc[0] + c[0] * a, acc[1] + c[1] * a, acc[2] + c[2] * a, acc[3] + a]
        if acc[3] == 0:
            return (0, 0, 0, 0)
        return (acc[0] / acc[3], acc[1] / acc[3], acc[2] / acc[3], acc[3] / 9 * 255)

    save("Journal/Book.tga", size, size, pixel)


def scene():
    """A night road for the appearance preview: fog, tree trunks and one lantern."""
    w, h = 512, 256
    fog, bark = cloud(131, 5, 3), cloud(149, 90, 6, 2)
    trunks = [(60, 26), (148, 14), (236, 34), (330, 18), (402, 46), (486, 30)]

    def pixel(x, y):
        u, v = x / w, y / h
        sky = mix((16, 30, 34), (5, 9, 11), v * 0.9)
        sky = tuple(c + (fog(u, v) - 0.5) * 26 * (1 - v * 0.5) for c in sky)
        glow = math.exp(-(((x - 196) / 70) ** 2 + ((y - 214) / 60) ** 2))
        sky = mix(sky, (150, 170, 84), glow * 0.55)
        for centre, width in trunks:
            half = width / 2 + (bark(u, v) - 0.5) * 3 + v * width * 0.15
            d = abs(x - centre)
            if d < half:
                depth = 0.55 + width / 100
                sky = mix(sky, (6, 8, 8), depth * min(1.0, (half - d) / 2))
        if y > 232:
            sky = mix(sky, (4, 6, 6), (y - 232) / 30)
        return sky

    save("BrillPreview.tga", w, h, pixel, alpha=False)


if __name__ == "__main__":
    paper()
    leather()
    title()
    chapter()
    tab("Journal/Tab.tga", (86, 86, 92), 163)
    tab("Journal/RedTab.tga", (128, 26, 22), 179)
    medallion()
    scene()
    print("journal artwork written to", OUT)
