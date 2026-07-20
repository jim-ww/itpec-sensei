#!/usr/bin/env python3
"""Rewrites pdfparse/tags.json globalIds from pdfparse's own exam-ID scheme to
the production exam-ID scheme used by data/questions/*.json (and thus by
core.Bank.GlobalID()).

pdfparse names exams by season ("A" = Autumn, "S" = Spring), taken from the
ITPEC source PDF filenames. Production names exams by chronological
sitting-within-year ("A" = first sitting, "B" = second; "S" is a one-off
label used only for the COVID-era 2020/2021 makeup sittings, which have no
"A" sitting at all that year). The two schemes don't line up letter-for-letter,
so this script maps by season instead of guessing:

  - pdfparse "A" (Autumn) always maps to production's "B" sitting.
  - pdfparse "S" (Spring) maps to production's "A" sitting, or "S" for the
    exam-less-that-lettering years (2020, 2021) where "A" doesn't exist.

The mapping was verified (not assumed) by word-overlap scoring between each
pdfparse question's stem and the corresponding production question's
explanation across all 80 questions per exam, for every year 2018-2025. See
git history for the verification script if this ever needs re-checking.

Run from repo root: python3 pdfparse/normalize_ids.py
"""
import json
import re
import sys
from pathlib import Path

REPO_ROOT = Path(__file__).resolve().parent.parent
TAGS_PATH = REPO_ROOT / "pdfparse" / "tags.json"
QUESTIONS_DIR = REPO_ROOT / "data" / "questions"

GLOBAL_ID_RE = re.compile(r"^(\d{4})([AS])_FE[_-](AM|A)#(\d+)$")


def production_exam_ids() -> set[str]:
    return {p.stem for p in QUESTIONS_DIR.glob("*.json")}


def normalize_key(key: str, known_exam_ids: set[str]) -> str:
    exam_id, _, _ = key.partition("#")
    if exam_id in known_exam_ids:
        return key  # already in production form

    m = GLOBAL_ID_RE.match(key)
    if not m:
        raise ValueError(f"unrecognized globalId format: {key!r}")
    year, season, part, num = m.groups()

    if season == "A":
        prod_letter = "B"
    else:
        prod_letter = "A" if f"{year}A_FE-{part}" in known_exam_ids else "S"

    new_exam_id = f"{year}{prod_letter}_FE-{part}"
    if new_exam_id not in known_exam_ids:
        raise ValueError(
            f"{key!r} -> {new_exam_id!r}#{num}, but {new_exam_id} "
            f"has no file in {QUESTIONS_DIR}"
        )
    return f"{new_exam_id}#{num}"


def main() -> None:
    known_exam_ids = production_exam_ids()
    if not known_exam_ids:
        sys.exit(f"no exam files found under {QUESTIONS_DIR}")

    tags = json.loads(TAGS_PATH.read_text())

    already_normalized = 0
    remapped = {}
    errors = []
    for key, value in tags.items():
        try:
            new_key = normalize_key(key, known_exam_ids)
        except ValueError as e:
            errors.append(str(e))
            continue
        if new_key == key:
            already_normalized += 1
        if new_key in remapped:
            errors.append(f"collision: both map to {new_key!r}")
            continue
        remapped[new_key] = value

    if errors:
        for e in errors:
            print(f"error: {e}", file=sys.stderr)
        sys.exit(f"{len(errors)} error(s), no changes written")

    if len(remapped) != len(tags):
        sys.exit(f"lost entries: {len(tags)} in, {len(remapped)} out")

    if already_normalized == len(tags):
        print("tags.json is already normalized; no changes needed")
        return

    ordered = dict(sorted(remapped.items()))
    TAGS_PATH.write_text(json.dumps(ordered, indent=2, ensure_ascii=False) + "\n")
    print(f"normalized {len(tags)} globalIds -> {TAGS_PATH}")


if __name__ == "__main__":
    main()
