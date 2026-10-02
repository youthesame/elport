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


def test_fetch_skips_download_when_local_file_matches_server_hash(tmp_path, configured):
    doc = tmp_path / "report.md"
    write_doc(doc, "body")
    (tmp_path / "raw_data.csv").write_bytes(b"data")
    client = FakeClient(uploads=[_upload(3, "raw_data.csv")])

    sync.fetch(doc, client, {})

    assert client.calls == ["uploads"]


@pytest.mark.parametrize(
    "hash_fields",
    [
        {},
        {"hash": hashlib.md5(b"data").hexdigest(), "hash_algorithm": "md5"},
        {"hash": hashlib.sha256(b"data").hexdigest()},
        {"hash": "not-a-sha256-digest", "hash_algorithm": "sha256"},
    ],
)
def test_fetch_downloads_when_server_hash_is_unknown(hash_fields, tmp_path, configured):
    doc = tmp_path / "report.md"
    write_doc(doc, "body")
    (tmp_path / "raw_data.csv").write_bytes(b"data")
    upload = {k: v for k, v in _upload(3, "raw_data.csv").items() if "hash" not in k}
    client = FakeClient(uploads=[{**upload, **hash_fields}])

    sync.fetch(doc, client, {})

    assert client.calls == ["uploads", "download"]


def test_fetch_downloads_when_local_file_differs_from_server_hash(tmp_path, configured):
    doc = tmp_path / "report.md"
    write_doc(doc, "body")
    (tmp_path / "raw_data.csv").write_bytes(b"local-different")
    client = FakeClient(uploads=[_upload(3, "raw_data.csv")])

    sync.fetch(doc, client, {})

    assert client.calls == ["uploads", "download"]
    assert (tmp_path / "raw_data.csv.remote").read_bytes() == b"data"


def test_fetch_rejects_symlink_even_when_its_content_matches(tmp_path, configured):
    doc = tmp_path / "report.md"
    write_doc(doc, "body")
    (tmp_path / "linked.csv").write_bytes(b"data")
    (tmp_path / "raw_data.csv").symlink_to(tmp_path / "linked.csv")
    client = FakeClient(uploads=[_upload(3, "raw_data.csv")])

    with pytest.raises(RuntimeError, match="unsafe attachment destination"):
        sync.fetch(doc, client, {})

    assert "download" not in client.calls


def test_fetch_same_name_collision_is_rejected_even_when_one_matches_local(
    tmp_path, configured
):
    doc = tmp_path / "report.md"
    write_doc(doc, "body")
    (tmp_path / "raw_data.csv").write_bytes(b"data")
    client = FakeClient(
        uploads=[
            _upload(3, "raw_data.csv"),
            _upload(4, "Raw_Data.csv", b"other"),
        ]
    )

    with pytest.raises(RuntimeError, match="basename collision"):
        sync.fetch(doc, client, {})


def test_fetch_matching_local_file_leaves_stale_conflict_copy_alone(
    tmp_path, configured, capsys
):
    doc = tmp_path / "report.md"
    write_doc(doc, "body")
    (tmp_path / "raw_data.csv").write_bytes(b"data")
    (tmp_path / "raw_data.csv.remote").write_bytes(b"older remote")
    client = FakeClient(uploads=[_upload(3, "raw_data.csv")])

    sync.fetch(doc, client, {})

    assert client.calls == ["uploads"]
    assert (tmp_path / "raw_data.csv.remote").read_bytes() == b"older remote"
    assert "conflicts written to" not in capsys.readouterr().out


def test_fetch_identical_conflict_copy_does_not_hide_a_differing_local_file(
    tmp_path, configured, capsys
):
    doc = tmp_path / "report.md"
    write_doc(doc, "body")
    (tmp_path / "raw_data.csv").write_bytes(b"local-different")
    (tmp_path / "raw_data.csv.remote").write_bytes(b"data")
    client = FakeClient(uploads=[_upload(3, "raw_data.csv")])

    sync.fetch(doc, client, {})

    assert client.calls == ["uploads", "download"]
    assert "conflicts written to: raw_data.csv.remote" in capsys.readouterr().out
