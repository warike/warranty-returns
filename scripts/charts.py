#!/usr/bin/env python3
"""Render the blog post's tables and charts as SVG from data/results.json.

Standard library only. Every figure comes out twice, once per theme, so a
page can show the light one by default and swap to the dark one under
[data-theme="dark"]. Text is real text, not paths, so the numbers stay
searchable and the file stays small.

    python3 scripts/charts.py            # writes charts/*.svg
    python3 scripts/charts.py --out dir  # writes somewhere else
"""

from __future__ import annotations

import argparse
import json
from pathlib import Path
from xml.sax.saxutils import escape

ROOT = Path(__file__).resolve().parent.parent
DATA = ROOT / "data" / "results.json"

THEMES = {
    "light": {"title": "#111827", "text": "#1f2937", "muted": "#4b5563", "line": "#d1d5db", "head": "#f3f4f6"},
    "dark": {"title": "#e5e7eb", "text": "#d1d5db", "muted": "#9ca3af", "line": "#374151", "head": "#262c36"},
}

WIDTH = 760
FONT = 'font-family="system-ui, sans-serif" font-size="13"'


def fmt(n: float) -> str:
    """Trim float noise so the SVG diffs cleanly between runs."""
    s = f"{n:.2f}".rstrip("0").rstrip(".")
    return s if s else "0"


def svg(height: int, title: str, body: list[str]) -> str:
    head = (
        f'<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 {WIDTH} {height}" '
        f'width="{WIDTH}" height="{height}" role="img" {FONT}>\n'
        f"<title>{escape(title)}</title>\n"
    )
    return head + "\n".join(body) + "\n</svg>\n"


def text(x, y, s, fill, **attrs) -> str:
    extra = "".join(f' {k.replace("_", "-")}="{v}"' for k, v in attrs.items())
    return f'<text x="{fmt(x)}" y="{fmt(y)}" fill="{fill}"{extra}>{escape(str(s))}</text>'


# --- tables -----------------------------------------------------------------

ROW = 34
HEAD_Y = 0


def table(t: dict, title: str, columns: list[tuple[str, int, str]], rows: list[list[str]]) -> str:
    """columns: (header, x, anchor). rows: cell strings in column order.

    The title goes in the SVG <title> only, for screen readers. The visible
    caption is the sentence in the post that introduces the figure."""
    out = [f'<rect x="0" y="{HEAD_Y}" width="{WIDTH}" height="{ROW}" fill="{t["head"]}" rx="4"/>']
    for header, x, anchor in columns:
        out.append(text(x, HEAD_Y + 22, header, t["title"], font_weight=600, text_anchor=anchor))
    y = HEAD_Y + ROW
    out.append(f'<line x1="0" x2="{WIDTH}" y1="{y}" y2="{y}" stroke="{t["line"]}"/>')
    for row in rows:
        for i, ((_, x, anchor), cell) in enumerate(zip(columns, row)):
            fill = t["title"] if i == 0 else t["text"]
            out.append(text(x, y + 22, cell, fill, text_anchor=anchor))
        y += ROW
        out.append(f'<line x1="0" x2="{WIDTH}" y1="{y}" y2="{y}" stroke="{t["line"]}"/>')
    return svg(y + 14, title, out)


def cost_table(d: dict, t: dict) -> str:
    rows = []
    for m in d["models"]:
        per_token = float(m["usd_per_m_input"]) / 1_000_000 * m["input_tokens_per_record"]
        rows.append([m["name"], m["usd_per_m_input"], f"{per_token * 1_000:.2f}", f"{per_token * 100_000:.2f}"])
    return table(
        t,
        "Cost per month, input tokens only",
        [("Model", 14, "start"), ("USD per M input", 346, "end"), ("1,000 returns", 546, "end"), ("100,000 returns", 746, "end")],
        rows,
    )


def scenarios_table(d: dict, t: dict) -> str:
    rows = [[s["return"], s["customer"], s["technician"], s["expected"]] for s in d["scenarios"]]
    return table(
        t,
        "Five returns, each built to push on a different corner",
        [("Return", 14, "start"), ("Customer said", 154, "start"), ("Technician found", 384, "start"), ("Expected", 684, "start")],
        rows,
    )


def answers_table(d: dict, t: dict) -> str:
    rows = [[r["return"], f"{r['jev']:.2f}", f"{r['clef']:.2f}", f"{r['clef_flash']:.2f}", r["action"]] for r in d["match"]["rows"]]
    return table(
        t,
        d["match"]["question"],
        [("Return", 14, "start"), ("Jev", 326, "end"), ("Clef", 466, "end"), ("Clef-flash", 606, "end"), ("Action", 634, "start")],
        rows,
    )


# --- charts -----------------------------------------------------------------


def latency_chart(d: dict, t: dict) -> str:
    x0, px_per_ms, top, step = 150, 0.46, 8, 56
    lat = d["latency_ms"]
    out = []
    order = ["jev", "clef_flash", "clef"]
    models = {m["id"]: m for m in d["models"]}
    for i, mid in enumerate(order):
        m, v = models[mid], lat[mid]
        y = top + 10 + step * i
        med, lo, hi = (x0 + v[k] * px_per_ms for k in ("median", "min", "max"))
        c = m["color"]
        out.append(text(x0 - 10, y + 15, m["label"], t["text"], text_anchor="end"))
        out.append(f'<rect x="{x0}" y="{y}" width="{fmt(med - x0)}" height="20" fill="{c}" rx="3"/>')
        out.append(f'<line x1="{fmt(lo)}" x2="{fmt(hi)}" y1="{y + 10}" y2="{y + 10}" stroke="{c}" stroke-width="2" opacity="0.7"/>')
        for x in (lo, hi):
            out.append(f'<line x1="{fmt(x)}" x2="{fmt(x)}" y1="{y + 4}" y2="{y + 16}" stroke="{c}" stroke-width="2"/>')
        out.append(text(hi + 8, y + 15, f"{v['median']} ms", t["title"], font_weight=600))
        out.append(text(hi + 8, y + 30, f"{v['min']} to {v['max']}", t["muted"], font_size=11))
    bottom = top + 176
    for ms in range(0, 1001, 200):
        x = x0 + ms * px_per_ms
        out.append(f'<line x1="{fmt(x)}" x2="{fmt(x)}" y1="{top}" y2="{bottom}" stroke="{t["line"]}" stroke-dasharray="3 4"/>')
        out.append(text(x, bottom + 16, ms, t["muted"], text_anchor="middle"))
    out.append(text(380, bottom + 34, "milliseconds per call", t["muted"], text_anchor="middle"))
    title = (
        f"Latency per call, three decision models, {lat['calls_per_model']} calls each. "
        "Median is the solid bar, the thin line is min to max."
    )
    return svg(bottom + 44, title, out)


def scale_chart(d: dict, t: dict) -> str:
    x0, x1, base, px_per_ms, ymax = 70, 610, 240, 0.1712, 1250
    sc = d["scale_ms"]
    words = sc["words"]
    xs = [x0 + (x1 - x0) * i / (len(words) - 1) for i in range(len(words))]
    out = []
    for ms in range(0, ymax + 1, 250):
        y = base - ms * px_per_ms
        out.append(f'<line x1="{x0}" x2="{x1}" y1="{fmt(y)}" y2="{fmt(y)}" stroke="{t["line"]}" stroke-dasharray="3 4"/>')
        out.append(text(x0 - 8, y + 4, ms, t["muted"], text_anchor="end"))
    for x, w in zip(xs, words):
        out.append(text(x, base + 18, f"{w:,}", t["muted"], text_anchor="middle"))
    out.append(text((x0 + x1) / 2, base + 36, "words in the record", t["muted"], text_anchor="middle"))
    out.append(text(x0 - 8, 16, "ms", t["muted"], text_anchor="end", font_size=11))

    models = {m["id"]: m for m in d["models"]}
    legend = []
    for mid in ("clef", "clef_flash", "jev"):
        m, series = models[mid], sc[mid]
        ys = [base - v * px_per_ms for v in series]
        pts = " ".join(f"{fmt(x)},{fmt(y)}" for x, y in zip(xs, ys))
        out.append(f'<polyline points="{pts}" fill="none" stroke="{m["color"]}" stroke-width="3" stroke-linejoin="round"/>')
        for x, y in zip(xs, ys):
            out.append(f'<circle cx="{fmt(x)}" cy="{fmt(y)}" r="4.5" fill="{m["color"]}"/>')
        legend.append([ys[-1] + 5, m])
    # Legend labels sit beside the last point; push any that would overlap down.
    legend.sort(key=lambda e: e[0])
    for i in range(1, len(legend)):
        legend[i][0] = max(legend[i][0], legend[i - 1][0] + 36)
    for y, m in legend:
        out.append(text(x1 + 12, y, m["label"], m["color"], font_weight=600))
        out.append(text(x1 + 12, y + 15, " / ".join(str(v) for v in sc[m["id"]]) + " ms", t["muted"], font_size=11))
    title = (
        f"Median of {sc['calls_per_point']} calls per point as the input grows from {words[0]} to {words[-1]:,} words. "
        "Jev stays flat, Clef stays slow, Clef-flash sits between."
    )
    return svg(base + 50, title, out)


FIGURES = {
    "cost": cost_table,
    "scenarios": scenarios_table,
    "answers": answers_table,
    "latency": latency_chart,
    "scale": scale_chart,
}


def main() -> None:
    ap = argparse.ArgumentParser(description=__doc__.splitlines()[0])
    ap.add_argument("--out", type=Path, default=ROOT / "charts", help="output directory (default: charts/)")
    ap.add_argument("--prefix", default="decision-model-", help="file name prefix")
    args = ap.parse_args()

    data = json.loads(DATA.read_text())
    args.out.mkdir(parents=True, exist_ok=True)
    for name, render in FIGURES.items():
        for theme, palette in THEMES.items():
            path = args.out / f"{args.prefix}{name}-{theme}.svg"
            path.write_text(render(data, palette))
            print(path.relative_to(ROOT) if path.is_relative_to(ROOT) else path)


if __name__ == "__main__":
    main()
