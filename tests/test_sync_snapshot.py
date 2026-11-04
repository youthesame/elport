from __future__ import annotations

import pytest
from _sync_harness import FakeClient

from elport import sync

BASE = "https://e.example"
FIG = {"id": 3, "long_name": "aa/fig", "real_name": "fig.png", "storage": 1}
RAW = {"id": 4, "long_name": "bb/raw", "real_name": "raw.csv", "storage": 1}
FIG_URL = f"{BASE}/app/download.php?f=aa%2Ffig&name=fig.png&storage=1"


def test_snapshot_writes_the_body_and_only_the_uploads_it_shows(tmp_path, capsys):
    out = tmp_path / "out"
    client = FakeClient(
        remote_doc={"body": f"![figure]({FIG_URL})\n"}, uploads=[FIG, RAW]
    )

    sync.snapshot(client, "experiments", 42, out, BASE)

    assert (out / "report.md").read_text() == "![figure](fig.png)\n"
    assert (out / "fig.png").read_bytes() == b"data"
    assert sorted(p.name for p in out.iterdir()) == ["fig.png", "report.md"]
    assert capsys.readouterr().out.splitlines() == [
        str(out / "report.md"),
        str(out / "fig.png"),
    ]


def test_snapshot_is_never_a_working_copy(tmp_path, monkeypatch):
    def no_state(*args):
        raise AssertionError("snapshot must not touch state")

    monkeypatch.setattr(sync.state, "load", no_state)
    monkeypatch.setattr(sync.state, "save", no_state)
    client = FakeClient(remote_doc={"body": "text\n"})

    sync.snapshot(client, "experiments", 42, tmp_path / "out", BASE)

    # No front matter: push and pull cannot mistake it for a document.
    assert (tmp_path / "out" / "report.md").read_text() == "text\n"


def test_snapshot_refuses_a_non_empty_directory_before_any_request(tmp_path):
    (tmp_path / "keep.txt").write_text("mine")
    client = FakeClient(remote_doc={"body": "text\n"})

    with pytest.raises(RuntimeError, match="not empty"):
        sync.snapshot(client, "experiments", 42, tmp_path, BASE)

    assert client.calls == []
    assert (tmp_path / "keep.txt").read_text() == "mine"


def test_snapshot_fills_an_existing_empty_directory(tmp_path):
    client = FakeClient(remote_doc={"body": "text\n"})

    sync.snapshot(client, "experiments", 42, tmp_path, BASE)

    assert (tmp_path / "report.md").read_text() == "text\n"


def test_snapshot_never_places_a_hidden_upload(tmp_path):
    hidden = {"id": 5, "long_name": "cc/env", "real_name": ".envrc", "storage": 1}
    url = f"{BASE}/app/download.php?f=cc%2Fenv&name=.envrc&storage=1"
    client = FakeClient(remote_doc={"body": f"[env]({url})\n"}, uploads=[hidden])

    sync.snapshot(client, "experiments", 42, tmp_path / "out", BASE)

    assert not (tmp_path / "out" / ".envrc").exists()
    assert url in (tmp_path / "out" / "report.md").read_text()


def test_snapshot_rejects_an_invalid_id_before_any_request(tmp_path):
    client = FakeClient()

    with pytest.raises(ValueError):
        sync.snapshot(client, "experiments", 0, tmp_path / "out", BASE)

    assert client.calls == []
    assert not (tmp_path / "out").exists()


def test_snapshot_refuses_an_attachment_named_like_the_body(tmp_path):
    note = {"id": 6, "long_name": "dd/note", "real_name": "report.md", "storage": 1}
    url = f"{BASE}/app/download.php?f=dd%2Fnote&name=report.md&storage=1"
    client = FakeClient(remote_doc={"body": f"[prev]({url})\n"}, uploads=[note])

    with pytest.raises(RuntimeError, match="report.md"):
        sync.snapshot(client, "experiments", 42, tmp_path / "out", BASE)

    assert not (tmp_path / "out").exists()


def test_snapshot_refuses_a_body_that_would_read_as_an_elport_document(tmp_path):
    body = "---\nid: 9\nprofile: lab\n---\n\ntext\n"
    client = FakeClient(remote_doc={"body": body})

    with pytest.raises(RuntimeError, match="front matter"):
        sync.snapshot(client, "experiments", 42, tmp_path / "out", BASE)

    assert not (tmp_path / "out").exists()
