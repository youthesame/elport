from __future__ import annotations

import hashlib

from _sync_harness import FakeClient, saved_state, write_doc

from elab import frontmatter, sync


def test_identical_files_with_same_basename_share_one_upload(
    tmp_path, monkeypatch, configured
):
    doc = tmp_path / "report.md"
    (tmp_path / "a").mkdir()
    (tmp_path / "b").mkdir()
    (tmp_path / "a" / "data.csv").write_bytes(b"same")
    (tmp_path / "b" / "data.csv").write_bytes(b"same")
    write_doc(doc, "[a](a/data.csv) [b](b/data.csv)")
    client = FakeClient(
        gets=[
            {"body": "remote"},
            {"body": "remote"},
            {"body": "stored", "content_type": 2},
        ]
    )
    monkeypatch.setattr(sync.state, "load", lambda *args: saved_state())
    monkeypatch.setattr(sync.state, "save", lambda *args: None)

    sync.push(doc, client, {})

    assert client.calls.count("upload") == 1
    assert client.saved_payload is not None
    assert client.saved_payload["body"].count("aa%2Fuploaded") == 2


def test_hash_match_reuses_upload(tmp_path, monkeypatch, configured):
    doc = tmp_path / "report.md"
    attachment = tmp_path / "data.csv"
    attachment.write_bytes(b"same")
    write_doc(doc, "[data](data.csv)")
    upload = {
        "id": 3,
        "long_name": "aa/existing",
        "real_name": "data.csv",
        "storage": 1,
        "hash": hashlib.sha256(b"same").hexdigest(),
        "hash_algorithm": "sha256",
    }
    client = FakeClient(
        gets=[
            {"body": "remote"},
            {"body": "remote"},
            {"body": "stored", "content_type": 2},
        ],
        uploads=[upload],
    )
    monkeypatch.setattr(sync.state, "load", lambda *args: saved_state())
    monkeypatch.setattr(sync.state, "save", lambda *args: None)

    sync.push(doc, client, {})

    assert "upload" not in client.calls
    assert client.saved_payload is not None
    assert "aa%2Fexisting" in client.saved_payload["body"]


def test_unsupported_remote_hash_falls_back_to_matching_size(tmp_path):
    attachment = tmp_path / "data.csv"
    attachment.write_bytes(b"same")
    upload = {
        "real_name": "data.csv",
        "filesize": 4,
        "hash": "unsupported-digest",
        "hash_algorithm": "md5",
    }

    assert sync._matching_upload(attachment, [upload]) is upload

    upload["filesize"] = 5
    assert sync._matching_upload(attachment, [upload]) is None


def test_hash_without_algorithm_falls_back_to_matching_size(tmp_path):
    attachment = tmp_path / "data.csv"
    attachment.write_bytes(b"same")
    upload = {
        "real_name": "data.csv",
        "filesize": 4,
        "hash": "legacy-digest",
    }

    assert sync._matching_upload(attachment, [upload]) is upload

    upload.update(
        {
            "filesize": 4,
            "hash": hashlib.sha256(b"different").hexdigest(),
            "hash_algorithm": "sha256",
        }
    )
    assert sync._matching_upload(attachment, [upload]) is None


def test_pull_then_push_reuses_upload_with_markdown_unsafe_name(
    tmp_path, monkeypatch, configured
):
    doc = tmp_path / "report.md"
    write_doc(doc, "local")
    real_name = "図 1 (テスト)#1.png"
    upload = {
        "id": 3,
        "long_name": "aa/existing",
        "real_name": real_name,
        "storage": 1,
        "filesize": 4,
    }
    url = sync.download_url("https://e.example", upload)
    remote = {"body": f"[file]({url})", "tags": ""}
    client = FakeClient(
        gets=[remote, remote, remote, {**remote, "content_type": 2}],
        uploads=[upload],
    )
    stored_state = saved_state()
    monkeypatch.setattr(sync.state, "load", lambda *args: stored_state)

    def save_state(*args):
        stored_state.clear()
        stored_state.update(args[-1])

    monkeypatch.setattr(sync.state, "save", save_state)

    sync.pull(doc, client, {})
    sync.push(doc, client, {})

    assert frontmatter.parse(doc.read_text(encoding="utf-8"))[1] == (
        f"[file](<{real_name}>)"
    )
    assert client.saved_payload is not None
    assert client.saved_payload["body"] == f"[file]({url})"
    assert "upload" not in client.calls
