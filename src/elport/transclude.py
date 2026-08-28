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
    r"(?<!\\)\]\(\s*(<([^>\n]*)>)\s*"
    r"""(?:"(?:\\.|[^"\\])*"|'(?:\\.|[^'\\])*'|\((?:\\.|[^)\\])*\))?\)"""
)
_HTML_TAG = re.compile(
    r"""</?[A-Za-z][A-Za-z0-9:-]*(?=[\s/>])(?:"[^"]*"|'[^']*'|[^'">])*>"""
)
_HTML_TAG_NAME = re.compile(r"</?([A-Za-z][A-Za-z0-9:-]*)")
_REFERENCE_DEFINITION = re.compile(
    r"(?m)^ {0,3}\[[^\]\n]+\]:[ \t]*(?:<([^>\n]*)>|(\S+))"
)


@dataclass
class Reference:
    path: str
    start: int
    end: int
    file: Path | None = None
    fragment: str = ""


@dataclass
class _MarkdownLink:
    reference: Reference
    label_start: int
    destination_start: int
    full_end: int


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
    current = "".join(mask)
    angle_spans = {match.span(1) for match in _ANGLE_DESTINATION.finditer(current)}
    tags = list(_HTML_TAG.finditer(current))
    covered_until = 0
    for index, tag in enumerate(tags):
        if tag.start() < covered_until or tag.span() in angle_spans:
            continue
        name_match = _HTML_TAG_NAME.match(tag.group())
        if name_match is None or tag.group().startswith("</"):
            continue
        name = name_match.group(1).lower()
        if name not in {"pre", "code", "script", "style", "textarea", "title"}:
            continue
        if tag.group().rstrip().endswith("/>"):
            continue
        closer = next(
            (
                candidate
                for candidate in tags[index + 1 :]
                if candidate.group().startswith("</")
                and (match := _HTML_TAG_NAME.match(candidate.group())) is not None
                and match.group(1).lower() == name
            ),
            None,
        )
        if name in {"pre", "code"}:
            start = tag.start()
            end = closer.end() if closer else len(text)
        else:
            start = tag.end()
            end = closer.start() if closer else len(text)
        blank(start, end)
        covered_until = closer.end() if closer else len(text)
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


def _is_escaped(text: str, pos: int) -> bool:
    backslashes = 0
    pos -= 1
    while pos >= 0 and text[pos] == "\\":
        backslashes += 1
        pos -= 1
    return backslashes % 2 == 1


_TITLE_AND_CLOSE = re.compile(
    r"""[ \t\r\n]+(?:"(?:\\.|[^"\\])*"|'(?:\\.|[^'\\])*'|"""
    r"\((?:\\.|[^)\\])*\))?[ \t\r\n]*\)"
)


def _parse_markdown_destination(
    text: str, masked: str, label_start: int, close_bracket: int
) -> _MarkdownLink | None:
    pos = close_bracket + 2
    while pos < len(masked) and masked[pos] in " \t\r\n":
        pos += 1
    if pos >= len(masked):
        return None
    destination_start = pos
    if masked[pos] == "<":
        close_angle = pos + 1
        while close_angle < len(masked) and masked[close_angle] not in ">\n":
            close_angle += 1
        if close_angle == len(masked) or masked[close_angle] != ">":
            return None
        suffix = close_angle + 1
        if suffix < len(masked) and masked[suffix] == ")":
            full_end = suffix + 1
        else:
            title = _TITLE_AND_CLOSE.match(masked, suffix)
            if title is None:
                return None
            full_end = title.end()
        path = text[pos + 1 : close_angle].strip()
        reference = Reference(path, pos, close_angle + 1)
        return _MarkdownLink(reference, label_start, destination_start, full_end)

    start = pos
    depth = 0
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
                reference = Reference(_unescape_markdown_destination(raw), start, pos)
                return _MarkdownLink(reference, label_start, destination_start, pos + 1)
            depth -= 1
        elif char in "<>\n":
            return None
        elif char.isspace() and depth == 0:
            title = _TITLE_AND_CLOSE.match(masked, pos)
            if title is None:
                return None
            raw = text[start:pos]
            reference = Reference(_unescape_markdown_destination(raw), start, pos)
            return _MarkdownLink(reference, label_start, destination_start, title.end())
        pos += 1
    return None


def _markdown_destinations(text: str, masked: str) -> list[Reference]:
    brackets = []
    links = []
    for pos, char in enumerate(masked):
        if char in "\r\n":
            brackets.clear()
            continue
        if char not in "[]" or _is_escaped(masked, pos):
            continue
        if char == "[":
            brackets.append(pos)
            continue
        if not brackets:
            continue
        label_start = brackets.pop()
        if pos + 1 >= len(masked) or masked[pos + 1] != "(":
            continue
        link = _parse_markdown_destination(text, masked, label_start, pos)
        if link is not None:
            links.append(link)

    links = [
        link
        for link in links
        if not any(
            outer is not link
            and outer.destination_start <= link.label_start < outer.full_end
            for outer in links
        )
    ]
    return sorted((link.reference for link in links), key=lambda ref: ref.start)


def _html_attributes(tag: str) -> list[tuple[str, int, int]]:
    """Walk a well-formed HTML tag and yield ``(name, value_start, value_end)``
    for each attribute that carries a value. ``value_start``/``value_end`` bound
    the raw value inside the tag (quote characters excluded). Boolean attributes
    and the tag name are skipped, so a ``src=``/``href=`` sequence that merely
    appears inside another attribute's quoted or unquoted value is never
    mistaken for a real attribute.
    """
    name_match = _HTML_TAG_NAME.match(tag)
    if name_match is None:
        return []
    pos = name_match.end()
    n = len(tag)
    attributes: list[tuple[str, int, int]] = []
    while pos < n:
        while pos < n and (tag[pos].isspace() or tag[pos] == "/"):
            pos += 1
        if pos >= n or tag[pos] == ">":
            break
        name_start = pos
        while pos < n and not tag[pos].isspace() and tag[pos] not in "=/>":
            pos += 1
        name = tag[name_start:pos]
        while pos < n and tag[pos].isspace():
            pos += 1
        if pos >= n or tag[pos] != "=":
            continue  # boundary or boolean attribute; re-enter loop at pos
        pos += 1
        while pos < n and tag[pos].isspace():
            pos += 1
        if pos < n and tag[pos] in "\"'":
            quote = tag[pos]
            pos += 1
            value_start = pos
            while pos < n and tag[pos] != quote:
                pos += 1
            attributes.append((name, value_start, pos))
            if pos < n:
                pos += 1  # closing quote
        else:
            value_start = pos
            while pos < n and not tag[pos].isspace() and tag[pos] != ">":
                pos += 1
            attributes.append((name, value_start, pos))
    return attributes


def extract(text: str) -> list[Reference]:
    masked = _masked(text)
    preliminary = _markdown_destinations(text, masked)
    angle_spans = {
        (reference.start, reference.end)
        for reference in preliminary
        if text[reference.start : reference.start + 1] == "<"
    }
    all_tags = list(_HTML_TAG.finditer(masked))
    tags = [tag for tag in all_tags if tag.span() not in angle_spans]
    markdown_mask = list(masked)
    for tag in tags:
        markdown_mask[tag.start() : tag.end()] = [" "] * (tag.end() - tag.start())
    markdown_masked = "".join(markdown_mask)

    out = _markdown_destinations(text, markdown_masked)

    for tag in tags:
        for name, value_start, value_end in _html_attributes(tag.group()):
            if name.lower() not in ("src", "href"):
                continue
            raw = tag.group()[value_start:value_end]
            value = raw.strip()
            if not value:
                continue
            start = tag.start() + value_start + len(raw) - len(raw.lstrip())
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
    if any(
        part.startswith(".") and part not in (".", "..")
        for part in re.split(r"[\\/]+", raw)
    ):
        return None, True, ""
    if any(part.startswith(".") for part in relative.parts):
        return None, True, ""
    if _ignored(relative.as_posix(), ignore):
        return None, False, ""
    return p, False, fragment


def _warn_reference_definitions(text: str, doc_dir: Path, ignore: list[str]) -> None:
    for match in _REFERENCE_DEFINITION.finditer(_masked(text)):
        destination = (match.group(1) or match.group(2) or "").strip()
        if not destination:
            continue
        candidate, _, _ = _candidate(doc_dir, destination, ignore)
        if candidate is not None:
            print(
                "warning: reference-style link not uploaded "
                f"(elport rewrites inline links only): {destination}",
                file=sys.stderr,
            )


def plan(text: str, doc_dir: Path, ignore: list[str] | None = None) -> list[Reference]:
    ignore = ignore or []
    _warn_reference_definitions(text, doc_dir, ignore)
    refs = extract(text)
    names: dict[str, Path] = {}
    for ref in refs:
        p, warn, fragment = _candidate(doc_dir, ref.path, ignore)
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
    markdown_destinations = [
        reference
        for reference in _markdown_destinations(text, masked)
        if text[reference.start : reference.start + 1] != "<"
    ]
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
