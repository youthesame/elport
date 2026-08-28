from __future__ import annotations

import hashlib

import pytest
from _sync_harness import FakeClient, write_doc

from elport import frontmatter, sync


def _upload(uid: int, real_name: str, content: bytes = b"data") -> dict:
    return {
        "id": uid,
        "long_name": f"aa/{uid}",
        "real_name": real_name,
        "storage": 1,
        "hash": hashlib.sha256(content).hexdigest(),
        "hash_algorithm": "sha256",
    }


def test_fetch_downloads_unreferenced_attachments(tmp_path, configured, capsys):
    doc = tmp_path / "report.md"
    write_doc(doc, "body with no attachment links")
    client = FakeClient(uploads=[_upload(3, "raw_data.csv"), _upload(4, "notes.pdf")])

    sync.fetch(doc, client, {})

    assert (tmp_path / "raw_data.csv").read_bytes() == b"data"
    assert (tmp_path / "notes.pdf").read_bytes() == b"data"
    # the body is never parsed and never rewritten
    assert frontmatter.parse(doc.read_text())[1] == "body with no attachment links"
    out = capsys.readouterr().out
    assert "fetched experiments/1: 2 attachment(s)" in out


def test_fetch_skips_control_file_attachments(tmp_path, configured, capsys):
    doc = tmp_path / "report.md"
    write_doc(doc, "body")
    client = FakeClient(
        uploads=[_upload(3, ".elport.toml"), _upload(4, "raw_data.csv")]
    )

    sync.fetch(doc, client, {})

    assert not (tmp_path / ".elport.toml").exists()
    assert (tmp_path / "raw_data.csv").exists()
    assert "control file" in capsys.readouterr().err


def test_fetch_never_touches_base_state(tmp_path, monkeypatch, configured):
    doc = tmp_path / "report.md"
    write_doc(doc, "body")
    client = FakeClient(uploads=[_upload(3, "raw_data.csv")])
    touched = []
    monkeypatch.setattr(sync.state, "save", lambda *args: touched.append("save"))
    monkeypatch.setattr(sync.state, "load", lambda *args: touched.append("load"))

    sync.fetch(doc, client, {})

    assert touched == []


def test_fetch_requires_id(tmp_path, configured):
    doc = tmp_path / "report.md"
    doc.write_text(frontmatter.render({"entity": "experiments", "title": "T"}, "body"))
    client = FakeClient()

    with pytest.raises(RuntimeError, match="id is required"):
        sync.fetch(doc, client, {})


@pytest.mark.parametrize("name", ["report.base.md", "report.remote.md"])
def test_fetch_rejects_merge_sidecar_named_attachments(name, tmp_path, configured):
    # An attachment named like a merge sidecar must not be placed: a later
    # `elport merge` would treat it as saved conflict state and delete it.
    doc = tmp_path / "report.md"
    write_doc(doc, "body")
    client = FakeClient(uploads=[_upload(3, name)])

    with pytest.raises(RuntimeError, match="sidecar conflicts with attachment"):
        sync.fetch(doc, client, {})

    assert not (tmp_path / name).exists()
    assert "download" not in client.calls


def test_fetch_writes_remote_suffix_when_local_differs(tmp_path, configured, capsys):
    doc = tmp_path / "report.md"
    write_doc(doc, "body")
    (tmp_path / "raw_data.csv").write_bytes(b"local-different")
    client = FakeClient(uploads=[_upload(3, "raw_data.csv")])

    sync.fetch(doc, client, {})

    assert (tmp_path / "raw_data.csv").read_bytes() == b"local-different"
    assert (tmp_path / "raw_data.csv.remote").read_bytes() == b"data"
    assert "conflicts written to: raw_data.csv.remote" in capsys.readouterr().out


def test_fetch_leaves_identical_local_file_untouched(tmp_path, configured, capsys):
    doc = tmp_path / "report.md"
    write_doc(doc, "body")
    (tmp_path / "raw_data.csv").write_bytes(b"data")
    client = FakeClient(uploads=[_upload(3, "raw_data.csv")])

    sync.fetch(doc, client, {})

    assert (tmp_path / "raw_data.csv").read_bytes() == b"data"
    assert not (tmp_path / "raw_data.csv.remote").exists()
    assert "conflicts written to" not in capsys.readouterr().out
