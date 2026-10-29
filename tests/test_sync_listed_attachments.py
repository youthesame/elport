"""Front matter ``attachments:`` uploads files the body never references (ADR-0006)."""

from __future__ import annotations

import hashlib

import pytest
from _sync_harness import FakeClient, saved_state, write_doc

from elport import frontmatter, sync
from elport.transclude import IgnoreLayer

STORED = {"body": "stored", "content_type": 2}


def test_frontmatter_accepts_attachments_list():
    meta, _ = frontmatter.parse("---\nattachments: [raw.csv, 2024]\n---\nBody\n")

    assert meta["attachments"] == ["raw.csv", "2024"]


def test_frontmatter_wraps_scalar_attachment_and_empties_null():
    assert frontmatter.parse("---\nattachments: raw.csv\n---\n")[0]["attachments"] == [
        "raw.csv"
    ]
    assert frontmatter.parse("---\nattachments:\n---\n")[0]["attachments"] == []


@pytest.mark.parametrize(
    "value", ["{raw: csv}", "[[raw.csv]]", "[{raw: csv}]", "[null]", "[true]"]
)
def test_frontmatter_rejects_non_path_attachments(value):
    with pytest.raises(ValueError, match="attachments must be a list of paths"):
        frontmatter.parse(f"---\nattachments: {value}\n---\nBody\n")


def test_push_uploads_listed_file_without_touching_the_body(
    tmp_path, monkeypatch, configured
):
    doc = tmp_path / "report.md"
    (tmp_path / "data").mkdir()
    (tmp_path / "data" / "raw.csv").write_bytes(b"raw")
    write_doc(doc, "local", attachments=["data/raw.csv"])
    client = FakeClient(gets=[{"body": "remote"}, {"body": "remote"}, STORED])
    monkeypatch.setattr(sync.state, "load", lambda *args: saved_state())

    sync.push(doc, client, {})

    assert client.calls.count("upload") == 1
    assert client.saved_payload["body"] == "local"
    assert frontmatter.parse(doc.read_text())[0]["attachments"] == ["data/raw.csv"]


def test_push_reuses_listed_file_and_skips_the_patch(tmp_path, monkeypatch, configured):
    doc = tmp_path / "report.md"
    (tmp_path / "raw.csv").write_bytes(b"raw")
    write_doc(doc, "local", attachments=["raw.csv"])
    upload = {
        "id": 3,
        "long_name": "aa/raw",
        "real_name": "raw.csv",
        "hash": hashlib.sha256(b"raw").hexdigest(),
        "hash_algorithm": "sha256",
    }
    client = FakeClient(
        remote_doc={"body": "remote", "title": "Test", "content_type": 2},
        uploads=[upload],
    )
    monkeypatch.setattr(
        sync.state,
        "load",
        lambda *args: {**saved_state(), "meta_base": {"title": "Test"}},
    )

    sync.push(doc, client, {})

    assert "upload" not in client.calls
    assert "patch" not in client.calls


@pytest.mark.parametrize(
    "entry",
    [
        "missing.csv",
        "data",  # a directory
        ".env",
        "data/.secret",
        "scratch.csv",  # ignored
        "alias.csv",  # a symlink to an ignored file
        "../outside.csv",
        "/etc/passwd",
        "https://example.com/raw.csv",
        "raw.csv#top",
        "raw.csv?x=1",
    ],
)
def test_push_fails_on_listed_entry_that_would_not_upload(
    tmp_path, monkeypatch, configured, entry
):
    root = tmp_path / "root"
    (root / "data").mkdir(parents=True)
    (root / "data" / ".secret").write_bytes(b"secret")
    (root / ".env").write_bytes(b"KEY=1")
    (root / "scratch.csv").write_bytes(b"scratch")
    (root / "raw.csv").write_bytes(b"raw")
    (root / "alias.csv").symlink_to("scratch.csv")
    (tmp_path / "outside.csv").write_bytes(b"outside")
    doc = root / "report.md"
    write_doc(doc, "local", attachments=[entry])
    client = FakeClient(gets=[{"body": "remote"}])
    monkeypatch.setattr(sync.state, "load", lambda *args: saved_state())

    with pytest.raises(ValueError, match="attachments: cannot upload"):
        sync.push(doc, client, {"ignore": [IgnoreLayer(["scratch.csv"])]})

    assert "upload" not in client.calls
    assert "patch" not in client.calls


def test_ignore_rules_match_the_file_a_symlink_resolves_to(
    tmp_path, monkeypatch, configured
):
    # The rules guard file contents, which this file's own path already exposes.
    doc = tmp_path / "report.md"
    (tmp_path / "raw.csv").write_bytes(b"raw")
    (tmp_path / "scratch.csv").symlink_to("raw.csv")
    write_doc(doc, "local", attachments=["scratch.csv"])
    client = FakeClient(gets=[{"body": "remote"}, {"body": "remote"}, STORED])
    monkeypatch.setattr(sync.state, "load", lambda *args: saved_state())

    sync.push(doc, client, {"ignore": [IgnoreLayer(["scratch.csv"])]})

    assert client.calls.count("upload") == 1


def test_listed_file_colliding_with_a_referenced_basename_fails(
    tmp_path, monkeypatch, configured
):
    doc = tmp_path / "report.md"
    (tmp_path / "a").mkdir()
    (tmp_path / "a" / "raw.csv").write_bytes(b"one")
    (tmp_path / "raw.csv").write_bytes(b"two")
    write_doc(doc, "[raw](raw.csv)", attachments=["a/raw.csv"])
    client = FakeClient(gets=[{"body": "remote"}])
    monkeypatch.setattr(sync.state, "load", lambda *args: saved_state())

    with pytest.raises(ValueError, match="basename collision: raw.csv"):
        sync.push(doc, client, {})

    assert "upload" not in client.calls


def test_listed_file_also_referenced_uploads_once(tmp_path, monkeypatch, configured):
    doc = tmp_path / "report.md"
    (tmp_path / "raw.csv").write_bytes(b"raw")
    write_doc(doc, "[raw](raw.csv)", attachments=["raw.csv", "./raw.csv"])
    client = FakeClient(gets=[{"body": "remote"}, {"body": "remote"}, STORED])
    monkeypatch.setattr(sync.state, "load", lambda *args: saved_state())

    sync.push(doc, client, {})

    assert client.calls.count("upload") == 1
    assert "aa%2Fuploaded" in client.saved_payload["body"]


def test_dry_run_lists_the_listed_file(tmp_path, monkeypatch, configured, capsys):
    doc = tmp_path / "report.md"
    (tmp_path / "raw.csv").write_bytes(b"raw")
    write_doc(doc, "local", attachments=["raw.csv"])
    client = FakeClient(gets=[{"body": "remote"}])
    monkeypatch.setattr(sync.state, "load", lambda *args: saved_state())

    sync.push(doc, client, {}, dry_run=True)

    assert "Upload plan:\n  raw.csv\n" in capsys.readouterr().out
    assert "upload" not in client.calls


def test_status_reports_listed_file_as_new_upload(
    tmp_path, monkeypatch, configured, capsys
):
    doc = tmp_path / "report.md"
    (tmp_path / "raw.csv").write_bytes(b"raw")
    write_doc(doc, "local", attachments=["raw.csv"])
    client = FakeClient(remote_doc={"body": "remote", "content_type": 2})
    monkeypatch.setattr(sync.state, "load", lambda *args: saved_state())

    summary = sync.status(doc, client, {})

    output = capsys.readouterr().out
    assert "uploads local: raw.csv" in output
    assert "uploads new: raw.csv" in output
    assert "1 new upload(s)" in summary


def test_status_fails_on_listed_entry_that_would_not_upload(
    tmp_path, monkeypatch, configured
):
    doc = tmp_path / "report.md"
    write_doc(doc, "local", attachments=["missing.csv"])
    monkeypatch.setattr(sync.state, "load", lambda *args: saved_state())

    with pytest.raises(ValueError, match="attachments: cannot upload missing.csv"):
        sync.status(doc, FakeClient(), {})


def test_pull_keeps_the_attachments_list(tmp_path, monkeypatch, configured):
    doc = tmp_path / "report.md"
    write_doc(doc, "local", attachments=["raw.csv"])
    client = FakeClient(gets=[{"body": "remote", "content_type": 2}])
    monkeypatch.setattr(sync.state, "load", lambda *args: saved_state())

    sync.pull(doc, client, {})

    assert frontmatter.parse(doc.read_text())[0]["attachments"] == ["raw.csv"]
