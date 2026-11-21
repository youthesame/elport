"""Properties the example tests pin down only at a few points.

Run with ``HYPOTHESIS_PROFILE=crosshair`` to explore them symbolically.
"""

from __future__ import annotations

import html
import os
import tempfile
import unicodedata
from pathlib import Path

from hypothesis import assume, example, given
from hypothesis import strategies as st

from elport import frontmatter, transclude

BASE = "https://e.example"
nonempty = st.text(min_size=1)
plain = st.text(st.characters(categories=["L", "N"]) | st.sampled_from(" \n"))
path = st.from_regex(
    r"[a-z0-9_-]{1,8}(/[a-z0-9_-]{1,8}){0,2}\.[a-z]{1,4}", fullmatch=True
)


@given(nonempty, nonempty, st.integers(min_value=1, max_value=10**6))
def test_download_url_round_trips(long_name, real_name, storage):
    upload = {"long_name": long_name, "real_name": real_name, "storage": storage}
    url = transclude.download_url("https://e.example/", upload)
    assert transclude.parse_download_url(url) == (long_name, real_name, str(storage))


# PyYAML writes NEL (U+0085) as a raw line break, which reads back as a space.
value = st.text(st.characters(exclude_characters="\x85"))
meta = st.fixed_dictionaries(
    {},
    optional={
        "id": st.integers(min_value=1, max_value=2**31),
        "title": value,
        "profile": value,
        "tags": st.lists(value),
        "read": st.sampled_from(sorted(frontmatter.PERMISSION_LEVELS)),
        "write": st.sampled_from(sorted(frontmatter.PERMISSION_LEVELS)),
        "attachments": st.lists(value.filter(bool)),
    },
)


@given(meta, st.text(), st.booleans())
@example({"title": "実験\u3000"}, "", False)
def test_frontmatter_round_trips(data, body, bom):
    text = frontmatter.render(data, body)
    assert frontmatter.parse("\ufeff" * bom + text) == (data, body)


@given(st.text())
def test_safe_name_is_one_plain_component(name):
    result = transclude.safe_name(name)
    assert "/" not in result and "\\" not in result
    assert result not in ("", ".", "..")


@given(st.text())
def test_masking_only_blanks(text):
    masked = transclude._masked(text)
    assert len(masked) == len(text)
    assert all(m == t or m == " " for m, t in zip(masked, text))


@given(
    plain,
    plain,
    path,
    st.sampled_from(["```\n{}\n```\n", "~~~\n{}\n~~~\n", "`{}`", "<!-- {} -->"]),
)
def test_code_and_comments_hide_references(before, after, target, wrapper):
    link = wrapper.format(f"![x]({target}) <img src='{target}'>")
    assert transclude.extract(f"{before}\n\n{link}\n\n{after}") == []


# Markdown/HTML built from the tokens the parsers care about, so generated text
# actually exercises links, tags, fences, and code spans.
markup = st.lists(
    st.sampled_from(
        [
            "[x](",
            "![x](",
            "](",
            "(",
            ")",
            "<",
            ">",
            "<img src=",
            "<a href=",
            '"',
            "'",
            "`",
            "```\n",
            "~~~\n",
            "<!--",
            "-->",
            "\\",
            " ",
            "\n",
            "#s",
            "a.png",
            "b c.txt",
            "&amp;",
            "> ",
            "- ",
        ]
    )
    | st.text(max_size=3),
).map("".join)


@given(markup)
@example('<img src="a<!--x-->b">')
def test_references_point_at_unmasked_destinations(text):
    masked = transclude._masked(text)
    references = transclude.extract(text)
    assert [r.start for r in references] == sorted(r.start for r in references)
    for r in references:
        assert 0 <= r.start <= r.end <= len(text)
        assert masked[r.start : r.end] == text[r.start : r.end]
        raw = text[r.start : r.end]
        if r.kind == "html":
            assert r.path == html.unescape(raw)
        elif r.kind == "angle":
            assert raw[0] + raw[-1] == "<>" and r.path == raw[1:-1].strip()
        else:
            assert r.path == transclude._unescape_markdown_destination(raw)


# Remote angle destinations are left out: push writes a plain destination, so
# that notation changes once, harmlessly.
notation = st.sampled_from(
    ["[f]({})", "![f]({})", '<img src="{}">', "<a href='{}'>f</a>"]
)


@given(
    st.text(
        st.characters(exclude_categories=["Cs", "Cn"]),
        min_size=1,
        max_size=12,
    ),
    notation,
    st.sampled_from(["", "#sec"]),
    plain,
    plain,
)
def test_pull_then_push_then_pull_is_stable(real_name, form, fragment, before, after):
    """A pulled body pushes back to the same URLs, so pulling again changes
    nothing, and every localized reference names the downloaded file."""
    name = transclude.safe_name(real_name)
    assume(not name.startswith("."))  # pull refuses hidden attachments
    assume(not ("#" in name and fragment))
    upload = {"id": 4, "long_name": "aa/bb.png", "real_name": real_name, "storage": 1}
    url = transclude.download_url(BASE, upload)
    remote = f"{before}\n\n{form.format(html.escape(url) + fragment)}\n\n{after}"

    local, placed = transclude.reverse(remote, [upload], BASE)
    assert placed == [upload]
    with tempfile.TemporaryDirectory() as directory:
        doc_dir = Path(directory)
        file = doc_dir / name
        file.write_bytes(b"data")
        references = [r for r in transclude.plan(local, doc_dir) if r.file]
        assert [(r.file, r.fragment) for r in references] == [
            (file.resolve(), fragment)
        ]
        pushed = transclude.replace_spans(local, references, {file: url})
    assert transclude.reverse(pushed, [upload], BASE) == (local, [upload])


uploads = st.lists(
    st.fixed_dictionaries(
        {
            "real_name": st.sampled_from(
                # Names a case- and normalization-insensitive disk (APFS, NTFS)
                # may hold as one file.
                [
                    "image.png",
                    "Image.PNG",
                    "data",
                    "é",
                    "e\u0301",
                    "ß",
                    "SS",
                    "\u212a",
                    "k",
                ]
            )
            | st.text(st.characters(exclude_categories=["Cs", "Cn"]), max_size=8)
        }
    ),
    max_size=8,
).map(lambda items: [{**item, "id": i + 1} for i, item in enumerate(items)])


def _names(items: list[dict]) -> dict[int, str]:
    # Fresh copies: name_uploads stores its answer on the dicts it is given.
    named = transclude.name_uploads([dict(u) for u in items])
    return {u["id"]: u["elport_name"] for u in named}


@given(uploads, st.randoms())
def test_upload_names_are_distinct_files_and_stable(items, random):
    """Every upload gets its own file beside the note, whatever the listing
    order, and a newer upload never renames an older one's file."""
    names = _names(items)
    assert sorted(names) == [u["id"] for u in items]
    folded = {unicodedata.normalize("NFC", n).casefold() for n in names.values()}
    assert len(folded) == len(items)
    assert _names(random.sample(items, len(items))) == names
    assert _names(items[:-1]) == {u["id"]: names[u["id"]] for u in items[:-1]}
    with tempfile.TemporaryDirectory() as directory:
        for upload_id, name in names.items():
            (Path(directory) / name).write_text(str(upload_id))
        assert len(os.listdir(directory)) == len(items)
