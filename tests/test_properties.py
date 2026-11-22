"""Properties the example tests pin down only at a few points.

Run with ``HYPOTHESIS_PROFILE=crosshair`` to explore them symbolically.
"""

from __future__ import annotations

import hashlib
import html
import io
import os
import tempfile
import unicodedata
from contextlib import redirect_stdout
from pathlib import Path

import pytest
from _sync_harness import Store, elport_doc
from hypothesis import assume, example, given
from hypothesis import strategies as st
from hypothesis.stateful import (
    RuleBasedStateMachine,
    initialize,
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


@pytest.mark.symbolic
@given(st.text())
def test_safe_name_is_one_plain_component(name):
    result = transclude.safe_name(name)
    assert "/" not in result and "\\" not in result
    assert result not in ("", ".", "..")


@pytest.mark.symbolic
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


segment = st.text("abc01_-", min_size=1, max_size=5)
rel_path = st.lists(segment, min_size=1, max_size=3).map("/".join)
ignore_part = segment | st.sampled_from(["*", "**", "*.tmp", "a*"])
ignore_pattern = st.builds(
    lambda lead, parts, trail: f"{lead}{'/'.join(parts)}{trail}",
    st.sampled_from(["", "/"]),
    st.lists(ignore_part, min_size=1, max_size=3),
    st.sampled_from(["", "/"]),
)


@pytest.mark.symbolic
@given(rel_path, segment, st.lists(ignore_pattern, max_size=3), segment)
def test_ignore_is_monotonic_and_trailing_slash_matches_dirs_only(
    relative, child, patterns, name
):
    if transclude._matches(relative, patterns):
        assert transclude._matches(f"{relative}/{child}", patterns)

    # Trailing slash excludes descendants of a directory, never a file of that name.
    assert not transclude._matches(name, [f"{name}/"])
    assert not transclude._matches(name, [f"/{name}/"])
    assert not transclude._matches(f"work/{name}", [f"work/{name}/"])
    assert not transclude._matches(f"work/{name}", [f"**/{name}/"])
    assert transclude._matches(f"{name}/{child}", [f"/{name}/"])
    assert transclude._matches(f"work/{name}/{child}", [f"work/{name}/"])
    assert transclude._matches(f"work/{name}/{child}", [f"**/{name}/"])

    layer = transclude.IgnoreLayer(patterns, child)
    assert transclude._ignored(relative, [layer]) == transclude._matches(
        f"{child}/{relative}", patterns
    )


@pytest.mark.symbolic
@given(
    st.sampled_from(["", "a", "a/b"]),
    segment,
    st.sampled_from(["", "x", "../ok", "./ok"]),
    st.sampled_from(["/", "\\"]),
)
def test_hidden_relative_paths_are_always_rejected(before, secret, after, sep):
    doc_dir = Path("/work/notes")
    parts = [p for p in (before, f".{secret}", after) if p]
    assert transclude._hidden_as_written(doc_dir, sep.join(parts))

    plain_parts = [p for p in (before, secret, after) if p]
    assert not transclude._hidden_as_written(doc_dir, sep.join(plain_parts))


@pytest.mark.symbolic
@given(segment, segment, segment)
def test_hidden_ancestor_allows_neighbours_but_rejects_new_hidden_components(
    ancestor, sibling, secret
):
    assume(secret != ancestor)
    # Resolve once so target and doc_dir share a drive/anchor on Windows too.
    root = Path(f"/root/.{ancestor}").resolve()
    doc_dir = root / "notes"
    neighbour = doc_dir / f"{sibling}.txt"
    assert not transclude._exposes_hidden(doc_dir, f"{sibling}.txt", neighbour)
    assert not transclude._exposes_hidden(doc_dir, str(neighbour), neighbour)

    in_hidden = doc_dir / f".{secret}" / f"{sibling}.txt"
    assert transclude._exposes_hidden(doc_dir, f"{sibling}.txt", in_hidden)
    other_hidden = root.parent / f".{secret}" / f"{sibling}.txt"
    assert transclude._exposes_hidden(doc_dir, str(other_hidden), other_hidden)


meta_text = st.sampled_from(["None", "Exp 1", "5", ""])
catalog_value = st.integers(min_value=1, max_value=12) | st.sampled_from(
    ["5", "None", "Synthesis"]
)
synced_meta = st.fixed_dictionaries(
    {},
    optional={
        "id": st.integers(min_value=1, max_value=9),
        "title": meta_text,
        "category": catalog_value,
        "read": st.sampled_from(sorted(frontmatter.PERMISSION_LEVELS)),
        "tags": st.lists(st.sampled_from(["a", "b"]), max_size=2),
    },
)
remote_meta_doc = st.fixed_dictionaries(
    {},
    optional={
        "title": meta_text,
        "category": st.integers(min_value=1, max_value=12),
        "category_title": st.sampled_from(["", "5", "Synthesis", "Analysis"]),
        "canread_base": st.sampled_from(list(frontmatter.PERMISSION_LEVELS.values())),
        "tags": st.sampled_from(["", "a", "a,b", "b|c"]),
    },
)


def _same_value(a, b) -> bool:
    return (a is None and b is None) or (
        a is not None and b is not None and str(a) == str(b)
    )


@pytest.mark.symbolic
@given(synced_meta, synced_meta)
def test_edited_meta_selects_only_changed_synced_keys(local_meta, base_meta):
    assert sync._edited_meta(local_meta, None) == local_meta
    edited = sync._edited_meta(local_meta, {"meta_base": base_meta})
    for key, val in local_meta.items():
        if key not in sync.SYNCED_META:
            assert edited[key] == val
        elif _same_value(val, base_meta.get(key)):
            assert key not in edited
        else:
            assert edited[key] == val


@pytest.mark.symbolic
@given(synced_meta, synced_meta, remote_meta_doc)
@example(
    {"category": 5}, {"category": "5"}, {"category": 9, "category_title": "Analysis"}
)
@example({"category": "5"}, {}, {"category": 5, "category_title": "5"})
def test_merge_meta_preserves_local_edits_and_is_idempotent(
    local_meta, base_meta, remote_doc
):
    merged, new_base = sync._merge_meta(local_meta, base_meta, remote_doc)
    remote_values = sync._remote_meta(remote_doc)
    for key, remote_val in remote_values.items():
        assert new_base.get(key) == remote_val
        if _same_value(local_meta.get(key), base_meta.get(key)):
            assert _same_value(merged.get(key), remote_val)
        else:
            assert merged.get(key) == local_meta.get(key)
    if "tags" in remote_doc:
        expected_tags = sorted(
            sync._remote_tags(remote_doc)
            | {str(t) for t in local_meta.get("tags") or []}
        )
        assert (merged.get("tags") or []) == expected_tags
    # Pulling the same remote again is a no-op and still keeps local edits.
    assert sync._merge_meta(merged, new_base, remote_doc) == (merged, new_base)


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
    converge: storing the stored form changes it again (ELABFTW-API). Old
    instances list uploads without a sha256, leaving name and size to match."""

    def __init__(self, body: str, hashes: bool):
        self.body = body
        self.hashes = hashes
        self.files: dict[int, bytes] = {}
        self.listing: list[dict] = []
        self.patches = 0

    @staticmethod
    def normalize(body: str) -> str:
        return body.replace("&", "&amp;")

    def me(self):
        return {"userid": 1, "team": 7}

    def get(self, entity, eid):
        return {"body": self.body, "content_type": 2, "userid": 1, "team": 7}

    def uploads(self, entity, eid):
        return [dict(upload) for upload in self.listing]

    def add(self, real_name: str, data: bytes) -> dict:
        upload_id = len(self.listing) + 1
        upload = {
            "id": upload_id,
            "long_name": f"xx/{upload_id}",
            "real_name": real_name,
            "storage": 1,
            "filesize": len(data),
        }
        if self.hashes:
            upload["hash"] = hashlib.sha256(data).hexdigest()
            upload["hash_algorithm"] = "sha256"
        self.files[upload_id] = data
        self.listing.append(upload)
        return dict(upload)

    def upload(self, entity, eid, path):
        return self.add(path.name, path.read_bytes())

    def download(self, entity, eid, upload_id):
        return self.files[upload_id]

    def patch(self, entity, eid, payload):
        self.patches += 1
        if "body" in payload:
            self.body = self.normalize(payload["body"])
        return self.get(entity, eid)

    def edit(self, body: str):
        """A Web UI save: the editor shows the decoded body and saves it."""
        self.body = self.normalize(body)

    def served(self, value: str) -> bytes | None:
        """The bytes a download URL serves, if it names one of our uploads."""
        parsed = transclude.parse_download_url(value)
        for upload in self.listing:
            if parsed and upload["long_name"] == parsed[0]:
                return self.files[upload["id"]]
        return None

    def meaning(self) -> str:
        """The body with each upload URL replaced by the bytes it serves."""
        return _meaning(html.unescape(self.body), self.served)


def _meaning(text: str, resolve) -> str:
    out, pos = [], 0
    for reference in transclude.extract(text):
        data = resolve(reference.path)
        if data is not None:
            out += [text[pos : reference.start], f"<{data!r}>"]
            pos = reference.end
    return "".join(out) + text[pos:]


# Same-size contents and names that sanitize alike are what a hashless match
# could confuse.
contents = st.sampled_from([b"1", b"22", b"33"])
local_names = st.sampled_from(["a.txt", "b.txt", "a_b.txt"])
remote_names = st.sampled_from(["a.txt", "a?b.txt", "a_b.txt"])
texts = st.text("ab \n", min_size=1, max_size=4).filter(str.strip)


def test_sync_follows_the_two_form_base(configured):
    """Random local edits, Web edits and uploads, push, pull, and conflict
    resolution keep elport honest: status reports exactly what changed, a
    remote change is never overwritten without --force, push never rewrites
    the local file or deletes an upload, and after every sync each link on
    both sides serves the same bytes."""

    class Sync(RuleBasedStateMachine):
        @initialize(hashes=st.booleans())
        def clone(self, hashes):
            self.directory = tempfile.TemporaryDirectory()
            self.doc = Path(self.directory.name) / "note.md"
            self.monkeypatch = pytest.MonkeyPatch()
            self.store = Store(self.monkeypatch)
            # A local file the user never links: only the remote side plants it,
            # so push must never upload it without attachments: (ADR-0007).
            (self.doc.parent / "secret.txt").write_bytes(b"secret")
            # Cloned: both sides at the same stored form.
            self.server = Server("a", hashes)
            self.local_base = self.remote_base = self.local = "a"
            self.pending = None  # remote body of an unresolved conflict
            self.synced = True  # both sides mean the same, files included
            self.recorded: dict = {}  # push's link record when a pull adopted
            self.changed: set[str] = set()  # files written since the last push
            self.write(self.local)
            sync.pull(self.doc, self.server, {})

        def teardown(self):
            if hasattr(self, "directory"):
                self.monkeypatch.undo()
                self.directory.cleanup()

        def write(self, body):
            elport_doc(self.doc, body)

        def read(self):
            return frontmatter.parse(self.doc.read_text(encoding="utf-8"))[1]

        def local_meaning(self):
            local = self.read()
            # Pair each local link with the remote one it was pulled from: one
            # whose URL push recorded came back as written, not downloaded, and
            # means that upload (ADR-0007); any other is read from disk.
            remote = transclude.extract(html.unescape(self.server.body))
            references = transclude.extract(local)
            if self.recorded:
                assert len(references) == len(remote)
            recorded = {
                r.start: r_remote.path
                for r, r_remote in zip(references, remote)
                if r_remote.path in self.recorded
            }
            out, pos = [], 0
            for r in references:
                if r.start in recorded:
                    written = self.recorded[recorded[r.start]]["markdown"]
                    assert local[r.start : r.end] == written  # restored as written
                    data = self.server.served(recorded[r.start])
                elif r.path == "secret.txt":
                    # Remote-planted path: never uploaded, stays literal on both sides.
                    data = None
                else:
                    file = self.doc.parent / r.path
                    data = file.read_bytes() if file.is_file() else None
                if data is not None:
                    out += [local[pos : r.start], f"<{data!r}>"]
                    pos = r.end
            return "".join(out) + local[pos:]

        def conflicted(self):
            return self.pending is not None

        def resolved(self):
            for leftover in [
                *self.doc.parent.glob("note.*.md"),
                *self.doc.parent.glob("*.remote"),
                *self.doc.parent.glob("*.base"),
            ]:
                leftover.unlink()
            self.pending = None

        @rule(name=local_names, data=contents)
        def write_file(self, name, data):
            (self.doc.parent / name).write_bytes(data)
            self.synced = False
            self.changed.add(name)

        @rule(text=texts, keep_planted=st.booleans(), data=st.data())
        def edit_locally(self, text, keep_planted, data):
            # Only files that exist (never secret.txt): pushing a missing one sends
            # its bare path, which reads as a server-side link from then on (ADR-0007).
            existing = sorted(
                p.name for p in self.doc.parent.glob("*.txt") if p.name != "secret.txt"
            )
            links = (
                data.draw(st.lists(st.sampled_from(existing), max_size=2))
                if existing
                else []
            )
            planted = (
                "\n[r](secret.txt)"
                if keep_planted and "[r](secret.txt)" in self.local
                else ""
            )
            body = text + planted + "".join(f"\n[{name}]({name})" for name in links)
            self.write(body)
            self.local, self.synced = body, False

        @rule(text=texts)
        def edit_on_the_web(self, text):
            self.server.edit(text)
            self.synced = False

        @rule()
        def plant_link_on_the_web(self):
            self.server.edit(f"{html.unescape(self.server.body)}\n[r](secret.txt)")
            self.synced = False

        @rule(name=remote_names, data=contents)
        def attach_on_the_web(self, name, data):
            url = transclude.download_url(BASE, self.server.add(name, data))
            self.server.edit(f"{html.unescape(self.server.body)}\n[f]({url})")
            self.synced = False

        @precondition(lambda self: hasattr(self, "server") and not self.conflicted())
        @rule()
        def push(self):
            self.send(force=False)

        @precondition(lambda self: hasattr(self, "server"))
        @rule()
        def force_push(self):
            self.send(force=True)
            self.resolved()

        def send(self, force):
            before, remote = self.doc.read_bytes(), self.server.body
            patches, uploads = self.server.patches, list(self.server.listing)
            recorded = self.store.data["1"].get("doc_links")
            unchanged = self.local == self.local_base and remote == self.remote_base
            # Without a sha256 every referenced file is uploaded again (ADR-0014).
            clean = unchanged and self.server.hashes and not self.changed
            linkless = unchanged and not recorded and "](" not in self.local
            try:
                sync.push(self.doc, self.server, {}, force=force)
            except RuntimeError as error:
                assert "remote changed" in str(error)
                assert not force and remote != self.remote_base
                assert self.server.body == remote
                self.pending = remote
            else:
                assert force or remote == self.remote_base
                if clean:  # nothing to send: the stored body must not churn
                    assert self.server.body == remote
                if linkless:
                    assert self.server.patches == patches
                self.local_base, self.remote_base = self.local, self.server.body
                self.synced, self.changed, self.recorded = True, set(), {}
            assert self.doc.read_bytes() == before
            assert self.server.listing[: len(uploads)] == uploads  # none deleted

        @precondition(lambda self: hasattr(self, "server") and not self.conflicted())
        @rule()
        def pull(self):
            dirty = self.local != self.local_base
            remote_changed = self.server.body != self.remote_base
            try:
                sync.pull(self.doc, self.server, {})
            except RuntimeError as error:
                if "attachment conflicts" in str(error):
                    # The user takes the server's copy of each, then pulls again.
                    for copy in self.doc.parent.glob("*.remote"):
                        copy.replace(copy.with_suffix(""))
                        self.changed.add(copy.stem)
                else:
                    assert "local changes conflict" in str(error)
                    assert dirty and remote_changed
                    self.pending = self.server.body
            else:
                assert not (dirty and remote_changed)
                if not dirty:
                    self.local = self.local_base = self.read()
                    self.remote_base = self.server.body
                    self.recorded = self.store.data["1"].get("doc_links", {})
                    self.synced = True
            assert self.read() == self.local

        @precondition(lambda self: hasattr(self, "server") and self.conflicted())
        @rule()
        def resolve(self):
            sidecar = self.doc.with_name("note.remote.md").read_text(encoding="utf-8")
            sync.merge(self.doc, {}, resolved=True)
            # Both bases move to the remote side; the local text is the resolution.
            self.local_base = frontmatter.parse(sidecar)[1]
            self.remote_base = self.pending
            self.resolved()

        @invariant()
        def both_sides_agree(self):
            if not hasattr(self, "server"):
                return
            assert b"secret" not in self.server.files.values()
            if self.synced:
                assert self.local_meaning() == self.server.meaning()
            out = io.StringIO()
            with redirect_stdout(out):
                summary = sync.status(self.doc, self.server, {})
            for line in out.getvalue().splitlines():
                if line.startswith(("uploads new:", "uploads reuse:")):
                    assert "secret.txt" not in line
            assert ("local dirty" in summary) == (self.local != self.local_base)
            assert ("remote changed" in summary) == (
                self.server.body != self.remote_base
            )

    run_state_machine_as_test(Sync)
