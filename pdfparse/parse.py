#!/usr/bin/env python3
"""Experimental: parse an ITPEC Subject-A question PDF into JSON.

Single pass over `pdftotext -layout`. Per question it extracts the number,
stem text, and textual options, and flags whether the stem and/or answers
contain non-textual content (tables, diagrams, graphs).

Everything "visual" in these PDFs is vector line-art (no raster images), which
no image tool reports — but `-layout` preserves its shape in the text grid:
  * a TABLE row becomes a line split into 3+ cells by runs of 2+ spaces;
  * a DIAGRAM (automaton, flowchart, circuit) becomes deeply-indented lines of
    short scattered tokens (labels like "S1", "0/0") with no left-margin prose.
So classification is per-line on the layout grid.

Flags per question:
  stemVisual    : the stem region has table/diagram lines.
  answersVisual : options aren't four clean text strings, or the answer region
                  has table/diagram lines (fraction/graph/circuit options).
  visualKind    : best-effort ["table"] / ["diagram"] labels.

Usage: parse.py <questions.pdf> <examId>   Writes: out/<examId>.json
Needs: pdftotext (poppler).
"""

import json
import re
import subprocess
import sys
from pathlib import Path

Q_START = re.compile(r"^\s*Q(\d+)\.\s*(.*)$")
FOOTER = re.compile(r"^\s*[–-]\s*\d+\s*[–-]\s*$")
OPT_MARK = re.compile(r"(?<![A-Za-z0-9])([a-d])\)\s*")
OPT_LINE = re.compile(r"^\s*a\)")
PREAMBLE_END = "Do not open the exam booklet"

# A run of 2+ spaces separates columns/cells in layout mode.
CELL_SPLIT = re.compile(r" {2,}")
TABLE_WIDE_CELLS = 3    # one line with this many cells is already a table row
TABLE_2COL_LINES = 3    # ...or this many lines each split into 2 cells (a 2-col table)
DIAGRAM_INDENT = 18     # a diagram-label line is indented at least this much
DIAGRAM_MAX_LEN = 45    # ...and its trimmed content is short (scattered labels)
DIAGRAM_MIN_LINES = 2   # need a few such lines to call it a diagram


def collapse(text: str) -> str:
    return re.sub(r"\s+", " ", text).strip()


def split_options(region: str) -> dict[str, str]:
    parts = OPT_MARK.split(region)
    opts: dict[str, str] = {}
    it = iter(parts[1:])
    for letter, text in zip(it, it):
        opts[letter] = collapse(text)
    return opts


def line_cells(line: str) -> int:
    """Number of column-cells on a layout line (tokens split by 2+ spaces)."""
    return len([c for c in CELL_SPLIT.split(line.strip()) if c])


def is_diagram_line(line: str) -> bool:
    """A deeply-indented line of short scattered content — a diagram label."""
    stripped = line.strip()
    indent = len(line) - len(line.lstrip())
    return bool(stripped) and indent >= DIAGRAM_INDENT and len(stripped) <= DIAGRAM_MAX_LEN


def region_kinds(lines: list[str], detect_tables: bool = True) -> set[str]:
    """Visual kinds present in a block of layout lines. In the answer region,
    detect_tables is False: the four a-d options are themselves laid out in
    columns, so a columnar line there is not a real table — only genuinely
    diagram-like (sparse, indented) options count as visual."""
    kinds = set()
    if detect_tables:
        cells = [line_cells(ln) for ln in lines]
        wide = any(c >= TABLE_WIDE_CELLS for c in cells)
        two_col = sum(1 for c in cells if c >= 2) >= TABLE_2COL_LINES
        if wide or two_col:
            kinds.add("table")
    if sum(1 for ln in lines if is_diagram_line(ln)) >= DIAGRAM_MIN_LINES:
        kinds.add("diagram")
    return kinds


def parse(pdf: Path) -> list[dict]:
    raw = subprocess.run(["pdftotext", "-layout", str(pdf), "-"],
                         capture_output=True, text=True, check=True).stdout
    lines = raw.splitlines()
    for i, ln in enumerate(lines):
        if PREAMBLE_END in ln:
            lines = lines[i + 1:]
            break
    lines = [ln for ln in lines if not FOOTER.match(ln)]

    blocks: list[tuple[int, list[str]]] = []
    cur: int | None = None
    buf: list[str] = []
    for ln in lines:
        m = Q_START.match(ln)
        if m:
            if cur is not None:
                blocks.append((cur, buf))
            cur, buf = int(m.group(1)), [ln]
        elif cur is not None:
            buf.append(ln)
    if cur is not None:
        blocks.append((cur, buf))

    questions = []
    for num, blk in blocks:
        # Split the block into the stem region and the answer region at the
        # first "a)" option line.
        opt_idx = next((i for i, ln in enumerate(blk) if OPT_LINE.match(ln)), None)
        stem_lines = blk[:opt_idx] if opt_idx is not None else blk
        ans_lines = blk[opt_idx:] if opt_idx is not None else []

        block_text = "\n".join(blk)
        m = OPT_MARK.search(block_text)
        stem = collapse(block_text[: m.start()] if m else block_text)
        # strip the leading "Q<n>." off the stem
        stem = re.sub(r"^Q\d+\.\s*", "", stem)
        options = split_options(block_text[m.start():]) if m else {}

        clean_opts = len(options) == 4 and all(options.get(l) for l in "abcd")
        stem_kinds = region_kinds(stem_lines)
        ans_kinds = region_kinds(ans_lines, detect_tables=False)

        questions.append({
            "number": num,
            "stem": stem,
            "options": options,
            "stemVisual": bool(stem_kinds),
            "answersVisual": (not clean_opts) or bool(ans_kinds),
            "visualKind": sorted(stem_kinds | ans_kinds),
        })
    return questions


def main() -> None:
    if len(sys.argv) != 3:
        sys.exit(f"usage: {sys.argv[0]} <questions.pdf> <examId>")
    pdf, exam_id = Path(sys.argv[1]), sys.argv[2]

    questions = parse(pdf)
    for q in questions:
        q["globalId"] = f"{exam_id}#{q['number']}"
    # put globalId first for readability
    questions = [
        {"globalId": q.pop("globalId"), **q} for q in questions
    ]

    result = {
        "examId": exam_id,
        "source": pdf.name,
        "questionCount": len(questions),
        "questions": questions,
    }
    out_dir = Path(__file__).parent / "out"
    out_dir.mkdir(exist_ok=True)
    out_path = out_dir / f"{exam_id}.json"
    out_path.write_text(json.dumps(result, indent=2, ensure_ascii=False))
    nv = sum(1 for q in questions if q["stemVisual"] or q["answersVisual"])
    print(f"wrote {out_path} ({len(questions)} questions, {nv} with visual content)")


if __name__ == "__main__":
    main()
