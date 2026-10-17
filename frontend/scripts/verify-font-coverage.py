#!/usr/bin/env python3
"""Verify actual self-hosted cmaps; --update refreshes the official UI supplement.

Tool-only dependency: fonttools[woff]==4.60.1 (install in an isolated environment).
The application and offline Vitest regression do not need Python dependencies.
"""

import argparse
import hashlib
import io
import json
import re
import urllib.parse
import urllib.request
from pathlib import Path

from fontTools.ttLib import TTFont


FRONTEND = Path(__file__).resolve().parents[1]
FONT_DIR = FRONTEND / "src/assets/fonts"
OUTPUT_DIR = FONT_DIR / "noto-sans-ui"
CSS_PATH = FRONTEND / "src/styles/fonts.css"
FAMILY = "Ren2NotoSansSC"
MARKER = "/* Generated UI supplement: scripts/verify-font-coverage.py */"
SOURCE_EXTENSIONS = {".ts", ".vue", ".css", ".html", ".json", ".svg"}
CJK = re.compile(
    "[\u2e80-\u303f\u31c0-\u31ef\u3400-\u9fff\uf900-\ufaff\uff00-\uffef"
    "\U00020000-\U000323af]"
)
USER_AGENT = (
    "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 "
    "(KHTML, like Gecko) Chrome/131.0.0.0 Safari/537.36"
)


def digest(data):
    return hashlib.sha256(data).hexdigest()


def corpus():
    """Conservative superset: all production source, including local messages.

    Comments are included deliberately. No handpicked screenshot text, route
    list or translation-key allowlist can conceal a missing production glyph.
    """
    result = set()
    files = []
    for path in sorted((FRONTEND / "src").rglob("*")):
        if not path.is_file() or path.suffix not in SOURCE_EXTENSIONS:
            continue
        if "__tests__" in path.parts or path.name.endswith((".spec.ts", ".test.ts")):
            continue
        # Provenance data is a verification artifact, not interface text.
        if path.is_relative_to(FONT_DIR):
            continue
        text = path.read_text(encoding="utf-8-sig")
        # Include characters written as JavaScript/JSON Unicode escapes.
        text = re.sub(
            r"\\u(?:\{([0-9a-fA-F]+)\}|([0-9a-fA-F]{4}))",
            lambda match: chr(int(match.group(1) or match.group(2), 16)),
            text,
        )
        found = {ord(char) for char in CJK.findall(text)}
        if found:
            files.append(str(path.relative_to(FRONTEND)))
            result.update(found)
    return result, files


def ranges(points):
    output = []
    for point in sorted(points):
        if output and output[-1][1] + 1 == point:
            output[-1][1] = point
        else:
            output.append([point, point])
    return output


def css_ranges(points):
    return ", ".join(
        f"U+{start:04X}" if start == end else f"U+{start:04X}-{end:04X}"
        for start, end in ranges(points)
    )


def declared_points(block):
    declaration = re.search(r"unicode-range:\s*([^;]+);", block)
    if declaration is None:
        return None
    result = set()
    for start, end in re.findall(r"U\+([0-9a-f]+)(?:-([0-9a-f]+))?", declaration[1], re.I):
        result.update(range(int(start, 16), int(end or start, 16) + 1))
    return result


def inspect_font(path):
    data = path.read_bytes()
    font = TTFont(io.BytesIO(data))
    points = {
        point for point, glyph in font.getBestCmap().items()
        if font.getGlyphID(glyph) != 0
    }
    axes = {
        axis.axisTag: [axis.minValue, axis.maxValue]
        for axis in font["fvar"].axes
    } if "fvar" in font else {}
    return points, {
        "file": str(path.relative_to(FRONTEND)),
        "sha256": digest(data),
        "bytes": len(data),
        "cmap_ranges": ranges(points),
        "axes": axes,
    }


def coverage(css, weight=None):
    font_records = {}
    covered = set()
    for block in re.findall(r"@font-face\s*\{([^}]+)\}", css):
        if f"font-family: '{FAMILY}'" not in block:
            continue
        descriptor = re.search(r"font-weight:\s*([^;]+);", block)[1].strip()
        if weight is not None and descriptor != weight:
            continue
        relative = re.search(r"src:\s*url\(([^)]+)\)", block)[1]
        path = (CSS_PATH.parent / relative).resolve()
        points, record = inspect_font(path)
        declared = declared_points(block)
        usable = points if declared is None else points & declared
        covered.update(usable)
        font_records[record["file"]] = record
    return covered, list(font_records.values())


def fetch(url):
    request = urllib.request.Request(url, headers={"User-Agent": USER_AGENT})
    with urllib.request.urlopen(request, timeout=60) as response:
        return response.read()


def update(required):
    css = CSS_PATH.read_text(encoding="utf-8-sig").split(MARKER)[0].rstrip()
    covered, _ = coverage(css)
    missing = required - covered
    if not missing:
        raise SystemExit("No supplemental glyphs are needed; inspect existing assets first.")
    text = "".join(map(chr, sorted(missing)))
    css_url = "https://fonts.googleapis.com/css2?" + urllib.parse.urlencode({
        "family": "Noto Sans SC:wght@100..900", "display": "swap", "text": text,
    })
    source_css = fetch(css_url)
    face = source_css.decode()
    if len(re.findall("@font-face", face)) != 1 or "font-weight: 100 900" not in face:
        raise SystemExit("Official response is not the expected single variable font.")
    font_url = re.search(r"url\((https://fonts.gstatic.com/[^)]+)\)", face)[1]
    data = fetch(font_url)
    if data[:4] != b"wOF2":
        raise SystemExit("Official response is not WOFF2.")
    OUTPUT_DIR.mkdir(exist_ok=True)
    path = OUTPUT_DIR / "ui-supplement.woff2"
    path.write_bytes(data)
    points, record = inspect_font(path)
    if not missing <= points or record["axes"].get("wght") != [100, 900]:
        raise SystemExit("Official supplement has missing glyphs or wrong weight coverage.")
    license_url = "https://raw.githubusercontent.com/google/fonts/main/ofl/notosanssc/OFL.txt"
    license_data = fetch(license_url)
    if b"SIL OPEN FONT LICENSE Version 1.1" not in license_data:
        raise SystemExit("Unexpected upstream font license.")
    (OUTPUT_DIR / "OFL.txt").write_bytes(license_data)
    public_license = FRONTEND / "public/licenses/noto-sans-sc-OFL.txt"
    public_license.parent.mkdir(exist_ok=True)
    public_license.write_bytes(license_data)
    (OUTPUT_DIR / "source-response.txt").write_bytes(source_css)
    (OUTPUT_DIR / "source.json").write_text(json.dumps({
        "family": "Noto Sans SC", "css_url": css_url, "css_sha256": digest(source_css),
        "font_url": font_url, "font_sha256": digest(data), "user_agent": USER_AGENT,
        "license_url": license_url, "license_sha256": digest(license_data),
        "requested_codepoints": ranges(missing),
    }, ensure_ascii=False, indent=2) + "\n")
    # All subsets of one family must expose the same weight descriptors.
    # A range face beside existing static faces splits the browser's composite
    # family at weight matching, even when their Unicode ranges are disjoint.
    faces = []
    for weight in (400, 500, 700):
        faces.append("\n".join([
            "@font-face {", f"  font-family: '{FAMILY}';", "  font-style: normal;",
            f"  font-weight: {weight};", "  font-display: swap;",
            "  src: url(../assets/fonts/noto-sans-ui/ui-supplement.woff2) format('woff2');",
            f"  unicode-range: {css_ranges(missing)};", "}", "",
        ]))
    CSS_PATH.write_text(css + "\n\n" + MARKER + "\n" + "\n".join(faces))


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--update", action="store_true", help="download only missing UI glyphs")
    parser.add_argument("--write-manifest", action="store_true", help="write cmap attestation after verification")
    args = parser.parse_args()
    required, files = corpus()
    if args.update:
        update(required)
    css = CSS_PATH.read_text()
    covered, fonts = coverage(css)
    missing = required - covered
    weights = {
        re.search(r"font-weight:\s*([^;]+);", block)[1].strip()
        for block in re.findall(r"@font-face\s*\{([^}]+)\}", css)
        if f"font-family: '{FAMILY}'" in block
    }
    weight_coverage = {}
    for weight in sorted(weights):
        usable, _ = coverage(css, weight)
        weight_coverage[weight] = len(required & usable)
        missing.update(required - usable)
    result = {
        "success": not missing, "corpus_files": len(files), "required_codepoints": len(required),
        "covered_codepoints": len(required - missing), "coverage_by_weight": weight_coverage,
        "missing": "".join(map(chr, sorted(missing))),
    }
    print(json.dumps(result, ensure_ascii=False))
    if missing:
        raise SystemExit(1)
    if args.update or args.write_manifest:
        (OUTPUT_DIR / "coverage.json").write_text(json.dumps({
            "generator": "scripts/verify-font-coverage.py (fonttools 4.60.1)",
            "scope": "All production src Chinese/Han/fullwidth punctuation, including local messages and comments",
            "fonts": fonts,
        }, ensure_ascii=False, indent=2) + "\n")


if __name__ == "__main__":
    main()
