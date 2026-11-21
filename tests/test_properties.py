"""Properties the example tests pin down only at a few points.

Run with ``HYPOTHESIS_PROFILE=crosshair`` to explore them symbolically.
"""

from __future__ import annotations

import html
import os
import tempfile
import unicodedata
from pathlib import Path

import pytest
from _sync_harness import Store, elport_doc
from hypothesis import assume, example, given
from hypothesis import strategies as st
from hypothesis.stateful import (
    RuleBasedStateMachine,
    invariant,
    precondition,
    rule,
    run_state_machine_as_test,
)

from elport import frontmatter, sync, transclude

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


class Server:
    """One entity on an eLabFTW that normalizes every saved body, and does not
    converge: storing the stored form changes it again (ELABFTW-API)."""

    def __init__(self, body: str):
        self.body = body
        self.patches = 0

    @staticmethod
    def normalize(body: str) -> str:
        return body.replace("&", "&amp;")

    def me(self):
        return {"userid": 1, "team": 7}

    def get(self, entity, eid):
        return {"body": self.body, "content_type": 2, "userid": 1, "team": 7}

    def uploads(self, entity, eid):
        return []

    def patch(self, entity, eid, payload):
        self.patches += 1
        if "body" in payload:
            self.body = self.normalize(payload["body"])
        return self.get(entity, eid)


bodies = st.text("ab& \n", min_size=1, max_size=6).filter(str.strip)


def test_sync_follows_the_two_form_base(configured):
    """Random local edits, Web edits, push, pull, and conflict resolution keep
    elport honest: status reports exactly what changed, a remote change is
    never overwritten without --force, and push never rewrites the local file."""

    class Sync(RuleBasedStateMachine):
        def __init__(self):
            super().__init__()
            self.directory = tempfile.TemporaryDirectory()
            self.doc = Path(self.directory.name) / "note.md"
            self.monkeypatch = pytest.MonkeyPatch()
            Store(self.monkeypatch)
            # Cloned: both sides at the same stored form.
            self.server = Server("a")
            self.local_base = self.remote_base = self.local = "a"
            self.pending = None  # remote body of an unresolved conflict
            self.write(self.local)
            sync.pull(self.doc, self.server, {})

        def teardown(self):
            self.monkeypatch.undo()
            self.directory.cleanup()

        def write(self, body):
            elport_doc(self.doc, body)

        def read(self):
            return frontmatter.parse(self.doc.read_text(encoding="utf-8"))[1]

        def conflicted(self):
            return self.pending is not None

        @rule(body=bodies)
        def edit_locally(self, body):
            self.write(body)
            self.local = body

        @rule(body=bodies)
        def edit_on_the_web(self, body):
            self.server.body = Server.normalize(body)

        @precondition(lambda self: not self.conflicted())
        @rule()
        def push(self):
            self.send(force=False)

        @rule()
        def force_push(self):
            self.send(force=True)
            for sidecar in self.doc.parent.glob("note.*.md"):
                sidecar.unlink()
            self.pending = None

        def send(self, force):
            before, remote = self.doc.read_bytes(), self.server.body
            patches = self.server.patches
            clean = self.local == self.local_base and remote == self.remote_base
            try:
                sync.push(self.doc, self.server, {}, force=force)
            except RuntimeError as error:
                assert "remote changed" in str(error)
                assert not force and remote != self.remote_base
                assert self.server.body == remote
                self.pending = remote
            else:
                assert force or remote == self.remote_base
                if clean:
                    assert self.server.patches == patches  # nothing to send
                else:
                    assert self.server.body == Server.normalize(self.local)
                self.local_base, self.remote_base = self.local, self.server.body
            assert self.doc.read_bytes() == before

        @precondition(lambda self: not self.conflicted())
        @rule()
        def pull(self):
            dirty = self.local != self.local_base
            remote_changed = self.server.body != self.remote_base
            try:
                sync.pull(self.doc, self.server, {})
            except RuntimeError as error:
                assert "local changes conflict" in str(error)
                assert dirty and remote_changed
                self.pending = self.server.body
            else:
                assert not (dirty and remote_changed)
                if not dirty:
                    self.local = self.local_base = self.server.body
                    self.remote_base = self.server.body
            assert self.read() == self.local

        @precondition(lambda self: self.conflicted())
        @rule()
        def resolve(self):
            sync.merge(self.doc, {}, resolved=True)
            self.remote_base, self.pending = self.pending, None

        @invariant()
        def status_reports_both_sides(self):
            summary = sync.status(self.doc, self.server, {})
            assert ("local dirty" in summary) == (self.local != self.local_base)
            assert ("remote changed" in summary) == (
                self.server.body != self.remote_base
            )

    run_state_machine_as_test(Sync)
