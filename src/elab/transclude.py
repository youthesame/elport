from __future__ import annotations

import filecmp
import html
import re
import string
import sys
from dataclasses import dataclass
from fnmatch import fnmatchcase
from pathlib import Path
from urllib.parse import parse_qs, urlencode, urlparse

_ANGLE_DESTINATION = re.compile(
    r"!?(?<!\\)(?:\[[^\]]*\])\(\s*(<([^>\n]*)>)\s*"
    r"""(?:"(?:\\.|[^"\\])*"|'(?:\\.|[^'\\])*'|\((?:\\.|[^)\\])*\))?\)"""
)
_HTML_TAG = re.compile(
    r"""</?[A-Za-z][A-Za-z0-9:-]*(?=[\s/>])(?:"[^"]*"|'[^']*'|[^'">])*>"""
)


@dataclass
class Reference:
    path: str
    start: int
    end: int
    file: Path | None = None
    fragment: str = ""


def _masked(text: str) -> str:
    mask = list(text)

    def blank(a: int, b: int) -> None:
        mask[a:b] = [" "] * (b - a)

    opener_pattern = re.compile(r"(?m)^ {0,3}(`{3,}|~{3,})[^\n]*(?:\n|$)")
    pos = 0
    while opener := opener_pattern.search(text, pos):
        marker = opener.group(1)
        closer_pattern = re.compile(
            rf"(?m)^ {{0,3}}{re.escape(marker[0])}{{{len(marker)},}}[ \t]*(?:\r?\n|$)"
        )
        closer = closer_pattern.search(text, opener.end())
        end = closer.end() if closer else len(text)
        blank(opener.start(), end)
        pos = end
    for m in re.finditer(r"(?<!`)(`+)(?!`)[\s\S]*?(?<!`)\1(?!`)", "".join(mask)):
        blank(*m.span())
    for m in re.finditer(r"<!--[\s\S]*?(?:-->|\Z)", "".join(mask)):
        blank(*m.span())
    angle_spans = {
        match.span(1) for match in _ANGLE_DESTINATION.finditer("".join(mask))
    }
    for m in re.finditer(
        r"<(pre|code)\b[^>]*>[\s\S]*?(?:</\1\s*>|\Z)",
        "".join(mask),
        re.IGNORECASE,
    ):
        opener_end = m.start() + m.group().index(">") + 1
        if (m.start(), opener_end) in angle_spans:
            continue
        blank(*m.span())
    return "".join(mask)


def _path_matches(relative: str, pattern: str) -> bool:
    pattern_parts = tuple(pattern.split("/"))
    path_parts = tuple(relative.split("/"))

    def matches(pattern_index: int, path_index: int) -> bool:
        if pattern_index == len(pattern_parts):
            return path_index == len(path_parts)
        if pattern_parts[pattern_index] == "**":
            return matches(pattern_index + 1, path_index) or (
                path_index < len(path_parts) and matches(pattern_index, path_index + 1)
            )
        return (
            path_index < len(path_parts)
            and fnmatchcase(path_parts[path_index], pattern_parts[pattern_index])
            and matches(pattern_index + 1, path_index + 1)
        )

    return matches(0, 0)


def _ignored(relative: str, patterns: list[str]) -> bool:
    """Match a small gitignore-like subset against a relative POSIX path.
    Patterns use fnmatch syntax; ``**/`` may span zero or more directories.
    A trailing slash matches that directory and every file below it.
    """
    for pattern in patterns:
        pattern = pattern.replace("\\", "/")
        rooted = pattern.startswith("/")
        pattern = pattern.removeprefix("/")
        directory_only = pattern.endswith("/")
        if directory_only:
            pattern = pattern.removesuffix("/")
            if not rooted and "/" not in pattern:
                if any(
                    fnmatchcase(segment, pattern)
                    for segment in relative.split("/")[:-1]
                ):
                    return True
                continue
            pattern += "/**"

        if not rooted and "/" not in pattern:
            if any(fnmatchcase(segment, pattern) for segment in relative.split("/")):
                return True
            continue

        if _path_matches(relative, pattern):
            return True
    return False


def _unescape_markdown_destination(value: str) -> str:
    out = []
    pos = 0
    while pos < len(value):
        if (
            value[pos] == "\\"
            and pos + 1 < len(value)
            and value[pos + 1] in string.punctuation
        ):
            pos += 1
        out.append(value[pos])
        pos += 1
    return "".join(out)


def _markdown_destinations(text: str, masked: str) -> list[Reference]:
    opener_pattern = re.compile(r"!?(?<!\\)(?:\[[^\]\n]*\])\(\s*")
    references = []
    for opener in opener_pattern.finditer(masked):
        start = opener.end()
        if start == len(masked) or masked[start] == "<":
            continue
        depth = 0
        pos = start
        while pos < len(masked):
            char = masked[pos]
            if char == "\\" and pos + 1 < len(masked):
                pos += 2
                continue
            if char == "(":
                depth += 1
            elif char == ")":
                if depth == 0:
                    raw = text[start:pos]
                    references.append(
                        Reference(_unescape_markdown_destination(raw), start, pos)
                    )
                    break
                depth -= 1
            elif char in "<>\n" or (char.isspace() and depth == 0):
                title = re.match(
                    r"""\s+(?:"(?:\\.|[^"\\])*"|'(?:\\.|[^'\\])*'|"""
                    r"\((?:\\.|[^)\\])*\))\s*\)",
                    masked[pos:],
                )
                if title:
                    raw = text[start:pos]
                    references.append(
                        Reference(_unescape_markdown_destination(raw), start, pos)
                    )
                break
            pos += 1
    return references


def extract(text: str) -> list[Reference]:
    masked = _masked(text)
    angle_matches = list(_ANGLE_DESTINATION.finditer(masked))
    all_tags = list(_HTML_TAG.finditer(masked))
    angle_matches = [
        match
        for match in angle_matches
        if not any(
            tag.start() <= match.start() and match.end() <= tag.end()
            for tag in all_tags
        )
    ]
    angle_spans = {match.span(1) for match in angle_matches}
    tags = [tag for tag in all_tags if tag.span() not in angle_spans]
    markdown_mask = list(masked)
    for tag in tags:
        markdown_mask[tag.start() : tag.end()] = [" "] * (tag.end() - tag.start())
    markdown_masked = "".join(markdown_mask)

    out = _markdown_destinations(text, markdown_masked)
    for match in angle_matches:
        start, end = match.span(1)
        out.append(Reference(match.group(2).strip(), start, end))

    attribute = re.compile(
        r"(?<![-\w])(?:src|href)\s*=\s*([\"'])(.*?)\1",
        re.IGNORECASE,
    )
    for tag in tags:
        for match in attribute.finditer(tag.group()):
            raw = match.group(2)
            value = raw.strip()
            start = tag.start() + match.start(2) + len(raw) - len(raw.lstrip())
            end = start + len(value)
            out.append(Reference(html.unescape(value), start, end))
    return sorted(out, key=lambda x: x.start)


def _candidate(
    doc_dir: Path, value: str, ignore: list[str]
) -> tuple[Path | None, bool, str]:
    if not value or value.startswith("#"):
        return None, False, ""
    if value.startswith(("/", "\\")) or re.match(r"^[A-Za-z]:[\\/]", value):
        return None, True, ""
    if re.match(r"^[A-Za-z][A-Za-z0-9+.-]*:", value):
        return None, False, ""
    raw = value
    if "?" in raw:
        return None, True, ""
    if not raw:
        return None, False, ""
    p = (doc_dir / raw).resolve()
    root = doc_dir.resolve()
    try:
        relative = p.relative_to(root)
    except ValueError:
        return None, True, ""
    fragment = ""
    if not p.is_file() and "#" in raw:
        raw, suffix = raw.split("#", 1)
        fragment = "#" + suffix
        p = (doc_dir / raw).resolve()
        try:
            relative = p.relative_to(root)
        except ValueError:
            return None, True, ""
    if not p.is_file():
        return None, True, ""
    if _ignored(relative.as_posix(), ignore):
        return None, False, ""
    return p, False, fragment


def plan(text: str, doc_dir: Path, ignore: list[str] | None = None) -> list[Reference]:
    refs = extract(text)
    names: dict[str, Path] = {}
    for ref in refs:
        p, warn, fragment = _candidate(doc_dir, ref.path, ignore or [])
        if warn:
            print(
                f"warning: local reference not uploaded: {ref.path}",
                file=sys.stderr,
            )
        if p is not None:
            name_key = p.name.casefold()
            previous = names.get(name_key)
            if previous is not None and previous != p:
                if not filecmp.cmp(previous, p, shallow=False):
                    raise ValueError(f"basename collision: {p.name}")
                p = previous
            names[name_key] = p
            ref.file = p
            ref.fragment = fragment
    return refs


def replace_spans(text: str, refs: list[Reference], urls: dict[Path, str]) -> str:
    normalized = {p.resolve(): value for p, value in urls.items()}
    pieces: list[str] = []
    pos = 0
    for r in refs:
        if r.file is None or r.file.resolve() not in normalized:
            continue
        pieces.append(text[pos : r.start])
        pieces.append(normalized[r.file.resolve()] + r.fragment)
        pos = r.end
    pieces.append(text[pos:])
    return "".join(pieces)


def download_url(base_url: str, upload: dict) -> str:
    q = urlencode(
        {
            "f": upload.get("long_name", ""),
            "name": upload.get("real_name", ""),
            "storage": upload.get("storage", 1),
        }
    )
    return base_url.rstrip("/") + "/app/download.php?" + q


def parse_download_url(value: str) -> tuple[str, str, str] | None:
    q = parse_qs(urlparse(html.unescape(value)).query)
    if not all(k in q and q[k] for k in ("f", "name", "storage")):
        return None
    return q["f"][0], q["name"][0], q["storage"][0]


def reverse(text: str, uploads: list[dict], base_url: str) -> tuple[str, list[dict]]:
    masked = _masked(text)
    markdown_destinations = _markdown_destinations(text, masked)
    by_key = {
        (
            str(upload.get("long_name", "")),
            str(upload.get("real_name", "")),
            str(upload.get("storage", 1)),
        ): upload
        for upload in uploads
    }
    prefix = re.escape(base_url.rstrip("/") + "/app/download.php?")
    url_pattern = re.compile(prefix + r"[^\s\"'<>)]*")
    replacements: list[tuple[int, int, str]] = []
    placed: list[dict] = []
    canonical_names: dict[str, str] = {}
    for match in url_pattern.finditer(masked):
        parsed = parse_download_url(text[match.start() : match.end()])
        if parsed is None:
            continue
        upload = by_key.get(parsed)
        if upload is None:
            continue
        value = text[match.start() : match.end()]
        fragment = "#" + value.split("#", 1)[1] if "#" in value else ""
        safe = safe_name(parsed[1])
        name = canonical_names.setdefault(safe.casefold(), safe)
        in_bare_markdown = any(
            reference.start <= match.start() and match.end() <= reference.end
            for reference in markdown_destinations
        )
        if in_bare_markdown and re.search(r"[\s()#]", name):
            name = f"<{name}{fragment}>"
        else:
            name += fragment
        replacements.append((match.start(), match.end(), name))
        if upload not in placed:
            placed.append(upload)
    result = text
    for start, end, name in reversed(replacements):
        result = result[:start] + name + result[end:]
    return result, placed


def safe_name(name: str) -> str:
    cleaned = Path(name.replace("\\", "/")).name.lstrip("~")
    return cleaned if cleaned not in ("", ".", "..") else "attachment"
