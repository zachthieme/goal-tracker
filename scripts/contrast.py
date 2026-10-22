#!/usr/bin/env python3
"""WCAG contrast ratios for the colour tokens in internal/web/static/app.css.

Prints each text and control-edge token's ratio on each surface, for the light
theme and both dark blocks, and flags any under 4.5:1 (text) or 3:1 (edges).
Then checks every ratio DESIGN.md's tables state against the stylesheet. Exits
1 if anything is flagged, the two dark blocks differ, or DESIGN.md disagrees.

Usage:     scripts/contrast.py
Self-test: python3 -B -m doctest scripts/contrast.py
"""

import re
import sys
from pathlib import Path

# The stylesheet's three token blocks: light, dark when the OS asks for it and
# no theme is pinned, and dark when the page pins it. The two dark blocks are
# meant to be identical.
THEME_SELECTORS = {
    "light": ":root",
    "dark-os": ':root:not([data-theme="light"])',
    "dark-pinned": ':root[data-theme="dark"]',
}

# The surfaces text and control edges sit on: the flat frame's canvas,
# sidebar, the sidebar's hovered and current nav item, hovered row and group
# header, then the card surfaces pages that haven't moved to the frame still
# use (--color-primary-light is the selected fill; --color-primary-subtle is
# the hover fill of quiet controls and of a Name), and a signal chip's or
# tag's fill. The marks are checked on each with the ratio WCAG asks of it:
# 4.5:1 for text, 3:1 for a control's edge, a focus ring or a Health shape.
# --color-border is left out: it draws rules, not control edges.
SURFACES = [
    "--color-canvas",
    "--color-sidebar",
    "--color-sidebar-hover",
    "--color-sidebar-on",
    "--color-row-hover",
    "--color-group",
    "--color-surface",
    "--color-surface-hover",
    "--color-surface-alt",
    "--color-primary-light",
    "--color-primary-subtle",
    "--color-chip-bg",
]
# Surfaces only text sits on, so the edge and shape marks aren't checked
# there: --color-primary-light fills the current Theme choice and History's
# current chip, the sidebar's hovered and current nav item hold a label and
# its count, and --color-chip-bg holds a chip's or tag's text. No input, chip
# edge or Health shape sits on any of them.
TEXT_ONLY = {"--color-primary-light", "--color-sidebar-hover", "--color-sidebar-on", "--color-chip-bg"}
MARKS = {
    "--color-ink": 4.5,
    "--color-ink-2": 4.5,
    "--color-ink-muted": 4.5,
    "--color-primary-strong": 4.5,
    "--color-danger-ink": 4.5,
    "--color-chip-ink": 4.5,
    "--color-border-strong": 3.0,
    "--color-focus": 3.0,
    "--health-r-mark": 3.0,
    "--health-y-mark": 3.0,
    "--health-g-mark": 3.0,
}
# Text marks no text may use on a surface, because the pair falls under 4.5:1
# in some theme: each is printed with × and not counted, and DESIGN.md
# § Deviations 10 lists it as not for text. --color-primary-strong on
# --color-sidebar-on is 4.48:1 in light. The current nav item's label and
# count are --color-ink and --color-ink-muted, and no link sits on it, while
# raising the link colour would move every link and filled button in the app.
NOT_FOR_TEXT = {("--color-primary-strong", "--color-sidebar-on")}
# The Check-in page's unpicked Health choices: each Health's word and shape in
# its ink on its tint, the fill at half strength, which in dark is an alpha
# laid over the canvas the choices sit on. Text, so 4.5:1.
TINTS = {
    "Red": ("--health-r-ink", "--health-r-tint"),
    "Yellow": ("--health-y-ink", "--health-y-tint"),
    "Green": ("--health-g-ink", "--health-g-tint"),
}
TINT_UNDER = "--color-canvas"


def flag(mark, surface, r):
    """How the table marks mark's ratio r on surface: "—" for a pair not
    checked, "×" for a pair no text may use, "!" for one under its minimum,
    else a space. Only "!" counts against the run.

    >>> flag("--color-border-strong", "--color-chip-bg", 2.95)
    '—'
    >>> flag("--color-primary-strong", "--color-sidebar-on", 4.48)
    '×'
    >>> flag("--color-ink-muted", "--color-sidebar-on", 4.49), flag("--color-chip-ink", "--color-chip-bg", 7.50)
    ('!', ' ')
    >>> flag("--color-border-strong", "--color-sidebar", 2.99), flag("--color-primary-strong", "--color-sidebar-hover", 4.49)
    ('!', '!')
    """
    minimum = MARKS[mark]
    if minimum < 4.5 and surface in TEXT_ONLY:
        return "—"
    if (mark, surface) in NOT_FOR_TEXT:
        return "×"
    return "!" if r < minimum else " "


def themes(css):
    """The colour tokens of each theme block in css, var() references resolved.

    A dark block declares only what it changes, so it starts from the light
    tokens.

    >>> t = themes('''
    ... :root { --color-ink: #18211F; /* note */ --color-canvas: #F6F6F3; --nav-ink: var(--color-ink); }
    ... @media (prefers-color-scheme: dark) {
    ...   :root:not([data-theme="light"]) { --color-ink: #E8E8E8; }
    ... }
    ... :root[data-theme="dark"] { --color-ink: #e8e8e8; }
    ... ''')
    >>> sorted(t)
    ['dark-os', 'dark-pinned', 'light']
    >>> t['light']['--nav-ink'], t['dark-os']['--nav-ink'], t['dark-pinned']['--color-canvas']
    ('#18211F', '#E8E8E8', '#F6F6F3')
    """
    css = re.sub(r"/\*.*?\*/", "", css, flags=re.S)
    blocks = {}
    for name, selector in THEME_SELECTORS.items():
        m = re.search(r"(?:^|[\s}])" + re.escape(selector) + r"\s*\{([^{}]*)\}", css)
        if not m:
            sys.exit(f"contrast.py: no {selector} block in the stylesheet")
        blocks[name] = dict(re.findall(r"(--[\w-]+)\s*:\s*([^;]+?)\s*;", m.group(1)))
    out = {}
    for name, declared in blocks.items():
        tokens = {**blocks["light"], **declared}
        out[name] = {k: resolve(v, tokens) for k, v in tokens.items()}
    return out


def resolve(value, tokens, depth=0):
    """value with var(--x) replaced by --x's value, and hex upper-cased."""
    m = re.fullmatch(r"var\((--[\w-]+)\)", value)
    if m and depth < 10:
        return resolve(tokens.get(m.group(1), value), tokens, depth + 1)
    return value.upper() if re.fullmatch(r"#[0-9A-Fa-f]{6}", value) else value


def stated(markdown):
    """The contrast ratios markdown's tables state, as (line, theme, mark,
    background, ratio) tuples. Mark and background are a token where the table
    names one, else the hex it gives. A table without a Theme column is about
    the light theme.

    >>> for s in stated('''
    ... | Theme | Ink | Canvas | Surface hover |
    ... | --- | --- | --- | --- |
    ... | Dark | `--color-ink-muted` `#9E9E9E` | 7.32 | 5.03 |
    ...
    ... | Mark | Surface alt | Source |
    ... | --- | --- | --- |
    ... | Input edge, `--color-border-strong` `#7B908C` | 3.01 | `#D5D2BD` |
    ...
    ... | Theme | Mark | On | Ratio |
    ... | --- | --- | --- | --- |
    ... | Light | Chip text `#3B4048` | chip fill `#E9EAEC` | 8.67 |
    ... | Light | Chip edge `#888F97` | surface `#FFFFFF` / surface hover `#FCFCFA` | 3.27 / 3.18 |
    ... | Dark | Tag text `--color-ink` | canvas / surface | 16.01 / 11.99 |
    ... '''): print(s)
    (4, 'dark', '--color-ink-muted', '--color-canvas', '7.32')
    (4, 'dark', '--color-ink-muted', '--color-surface-hover', '5.03')
    (8, 'light', '--color-border-strong', '--color-surface-alt', '3.01')
    (12, 'light', '#3B4048', '#E9EAEC', '8.67')
    (13, 'light', '#888F97', '--color-surface', '3.27')
    (13, 'light', '#888F97', '--color-surface-hover', '3.18')
    (14, 'dark', '--color-ink', '--color-canvas', '16.01')
    (14, 'dark', '--color-ink', '--color-surface', '11.99')
    """
    out = []
    lines = markdown.splitlines()
    header = None
    for n, line in enumerate(lines, 1):
        line = line.strip()
        if not line.startswith("|"):
            header = None
            continue
        cells = [c.strip() for c in line.strip("|").split("|")]
        if header is None:
            header = [c.lower() for c in cells]
            continue
        if all(re.fullmatch(r":?-+:?", c) for c in cells):
            continue
        row = dict(zip(header, cells))
        theme = row.get("theme", "light").lower()
        if "on" in row and "ratio" in row:
            mark = colour(row.get("mark", ""))
            backs = [background(b) for b in row["on"].split("/")]
            values = [v.strip() for v in row["ratio"].split("/")]
            if mark and len(backs) == len(values):
                out += [(n, theme, mark, b, v) for b, v in zip(backs, values) if b and is_ratio(v)]
            continue
        mark = next((colour(c) for h, c in row.items() if h != "theme" and colour(c)), None)
        for h, c in row.items():
            if mark and background(h) in SURFACES and is_ratio(c):
                out.append((n, theme, mark, background(h), c))
    return out


def colour(cell):
    """The token a table cell names, else the hex it gives, else None.

    >>> colour("Yellow mark `--health-y-mark` `#B27B00`"), colour("`#3B4048`"), colour("Home")
    ('--health-y-mark', '#3B4048', None)
    """
    m = re.search(r"`(--(?:color|health)-[\w-]+)`", cell) or re.search(r"(#[0-9A-Fa-f]{6})\b", cell)
    return m and resolve(m.group(1), {})


def background(cell):
    """The surface token a cell names in words ("surface hover"), else the hex
    it gives, else None."""
    name = "--color-" + "-".join(re.sub(r"`[^`]*`", "", cell).lower().split())
    return name if name in SURFACES else colour(cell)


def is_ratio(cell):
    return re.fullmatch(r"\d+\.\d+", cell) is not None


def over(colour, under):
    """colour as it lands on the #RRGGBB under: an rgba() is laid over it,
    a hex stays as it is.

    >>> over("rgba(239,68,68,.10)", "#0F1012"), over("#FEF6F6", "#FFFFFF")
    ('#251517', '#FEF6F6')
    """
    m = re.fullmatch(r"rgba\((\d+),\s*(\d+),\s*(\d+),\s*([\d.]+)\)", colour)
    if not m:
        return colour
    a = float(m.group(4))
    below = [int(under[i:i + 2], 16) for i in (1, 3, 5)]
    mixed = [round(int(c) * a + b * (1 - a)) for c, b in zip(m.group(1, 2, 3), below)]
    return "#" + "".join(f"{c:02X}" for c in mixed)


def ratio(fg, bg):
    """The WCAG 2 contrast ratio of two #RRGGBB colours.

    >>> f"{ratio('#18211F', '#F6F6F3'):.2f}"
    '15.20'
    >>> f"{ratio('#9E9E9E', '#063530'):.2f}"
    '5.03'
    >>> ratio('#FFFFFF', '#000000') == ratio('#000000', '#FFFFFF') == 21
    True
    """
    hi, lo = sorted((luminance(fg), luminance(bg)), reverse=True)
    return (hi + 0.05) / (lo + 0.05)


def luminance(hex_colour):
    """Relative luminance of a #RRGGBB colour, as WCAG 2 defines it."""
    channels = [int(hex_colour[i:i + 2], 16) / 255 for i in (1, 3, 5)]
    r, g, b = [c / 12.92 if c <= 0.03928 else ((c + 0.055) / 1.055) ** 2.4 for c in channels]
    return 0.2126 * r + 0.7152 * g + 0.0722 * b


def main():
    root = Path(__file__).resolve().parent.parent
    tokens = themes((root / "internal/web/static/app.css").read_text())
    low = 0

    width = max(map(len, MARKS)) + len(" (text)")
    for name, theme in tokens.items():
        print(f"{name}: each mark on each surface, ! under 4.5:1 for text or 3:1 for edges, × not for text, — not checked")
        heads = [s.removeprefix("--color-") for s in SURFACES]
        print(" " * width + "".join(f"  {h:>{len(h)}}" for h in heads))
        for mark, minimum in MARKS.items():
            kind = "(text)" if minimum == 4.5 else "(edge)"
            line = f"{mark} {kind}".ljust(width)
            for s, h in zip(SURFACES, heads):
                r = ratio(theme[mark], theme[s])
                f = flag(mark, s, r)
                if f == "—":
                    line += f"  {'—':>{len(h) - 1}} "
                    continue
                low += f == "!"
                line += f"  {r:>{len(h) - 1}.2f}{f}"
            print(line)
        print()
    print("Check-in Health choices: each ink on its tint over the canvas, ! under 4.5:1")
    for name, theme in tokens.items():
        cells = []
        for health, (ink, tint) in TINTS.items():
            back = over(theme[tint], theme[TINT_UNDER])
            r = ratio(theme[ink], back)
            low += r < 4.5
            cells.append(f"{health} {theme[ink]} on {back} {r:.2f}{'!' if r < 4.5 else ''}")
        print(f"  {name}: " + " · ".join(cells))
    print()
    print(f"{low} ratios under the minimum (marked !)." if low else "Every ratio meets its minimum.")
    problems = low

    drift = sorted(k for k in tokens["dark-os"] if tokens["dark-os"][k] != tokens["dark-pinned"].get(k))
    if drift:
        problems += 1
        print("The two dark blocks differ, though app.css says to keep them identical:")
        for k in drift:
            print(f"  {k}: {tokens['dark-os'][k]} (OS) vs {tokens['dark-pinned'].get(k)} (pinned)")
        print()

    claims = stated((root / "DESIGN.md").read_text())
    wrong = []
    for line, theme, mark, back, value in claims:
        for name in [n for n in tokens if n.startswith(theme)]:
            fg, bg = (tokens[name].get(c, c) for c in (mark, back))
            if not (fg.startswith("#") and bg.startswith("#")):
                continue
            got = f"{ratio(fg, bg):.2f}"
            if got != value:
                wrong.append(f"  DESIGN.md:{line} {name} {mark} on {back}: states {value}, computes {got}")
    if wrong:
        problems += len(wrong)
        print("DESIGN.md states a different ratio:")
        print("\n".join(wrong))
    else:
        print(f"DESIGN.md: all {len(claims)} ratios its tables state match.")
    return 1 if problems else 0


if __name__ == "__main__":
    sys.exit(main())
