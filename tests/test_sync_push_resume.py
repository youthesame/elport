from __future__ import annotations

import pytest
from _sync_harness import (
    FakeClient,
    write_doc,
)

from elport import frontmatter, sync


@pytest.mark.parametrize("fail_at", ["upload", "patch", "verify"])
def test_new_push_resumes_after_failure_without_force(
    fail_at, tmp_path, monkeypatch, configured
):
    """Issue #13: once the entity exists, a plain retry must finish the push."""

    class FlakyClient(FakeClient):
        def __init__(self):
            super().__init__(remote_doc={"body": ""})
            self.creates = 0
            self.broken = True

        def create(self, entity, title):
            self.creates += 1
            return super().create(entity, title)

        def _break(self, step):
            if self.broken and fail_at == step:
                self.broken = False
                raise RuntimeError(f"{step} failed")

        def upload(self, entity, eid, path):
            self._break("upload")
            return super().upload(entity, eid, path)

        def patch(self, entity, eid, payload):
            self._break("patch")
            stored = super().patch(entity, eid, payload)
            # Applied server-side, but the response (the stored form) is lost.
            self._break("verify")
            return stored

    doc = tmp_path / "report.md"
    (tmp_path / "data.csv").write_text("x", encoding="utf-8")
    doc.write_text(
        frontmatter.render({"title": "Test"}, "[data](data.csv)"), encoding="utf-8"
    )
    store = {}
    monkeypatch.setattr(sync.state, "load", lambda *args: store.get("state"))
    monkeypatch.setattr(
        sync.state, "save", lambda *args: store.__setitem__("state", args[-1])
    )
    client = FlakyClient()

    with pytest.raises(RuntimeError, match=f"{fail_at} failed"):
        sync.push(doc, client, {})

    assert frontmatter.parse(doc.read_text(encoding="utf-8"))[0]["id"] == 42
    assert store["state"]["pending_create"] is True

    sync.push(doc, client, {})

    assert client.creates == 1
    assert store["state"].get("pending_create") is None
    assert store["state"]["local_base"] == "[data](data.csv)"
    assert store["state"]["remote_base"] == client.remote_doc["body"]


def test_resumed_push_keeps_conflict_detection_after_it_completes(
    tmp_path, monkeypatch, configured
):
    doc = tmp_path / "report.md"
    doc.write_text(frontmatter.render({"title": "Test"}, "local"), encoding="utf-8")
    store = {}
    monkeypatch.setattr(sync.state, "load", lambda *args: store.get("state"))
    monkeypatch.setattr(
        sync.state, "save", lambda *args: store.__setitem__("state", args[-1])
    )
    client = FakeClient(remote_doc={"body": ""})

    sync.push(doc, client, {})

    client.remote_doc["body"] = "web edit"
    with pytest.raises(RuntimeError, match="remote changed"):
        sync.push(doc, client, {})


def test_resumed_create_still_detects_a_remote_written_before_the_first_push(
    tmp_path, monkeypatch, configured
):
    """Nothing was stored yet, so a non-empty remote is somebody else's write."""
    doc = tmp_path / "report.md"
    write_doc(doc, "local")
    client = FakeClient(gets=[{"body": "web edit"}])
    monkeypatch.setattr(
        sync.state,
        "load",
        lambda *args: {
            "remote_base": "",
            "local_base": "",
            "meta_base": {},
            "pending_create": True,
            "body_sent": False,
        },
    )

    with pytest.raises(RuntimeError, match="remote changed"):
        sync.push(doc, client, {})


def test_resumed_create_detects_a_remote_edit_made_during_the_retry(
    tmp_path, monkeypatch, configured
):
    doc = tmp_path / "report.md"
    (tmp_path / "data.csv").write_text("x", encoding="utf-8")
    write_doc(doc, "[data](data.csv)")
    client = FakeClient(gets=[{"body": "ours"}, {"body": "web edit"}])
    monkeypatch.setattr(
        sync.state,
        "load",
        lambda *args: {
            "remote_base": "",
            "local_base": "",
            "meta_base": {},
            "pending_create": True,
            "body_sent": True,
        },
    )

    with pytest.raises(RuntimeError, match="remote changed after uploads"):
        sync.push(doc, client, {})

    assert "patch" not in client.calls


def test_resume_conflict_is_not_overwritten_by_the_next_push(
    tmp_path, monkeypatch, configured
):
    doc = tmp_path / "report.md"
    # A new upload opens the window the recheck guards.
    (tmp_path / "d.csv").write_text("d")
    write_doc(doc, "[d](d.csv)")
    stored_state = {
        "remote_base": "",
        "local_base": "",
        "meta_base": {},
        "pending_create": True,
        "body_sent": True,
    }
    monkeypatch.setattr(sync.state, "load", lambda *args: stored_state)
    monkeypatch.setattr(
        sync.state,
        "save",
        lambda *args: (stored_state.clear(), stored_state.update(args[3])),
    )
    client = FakeClient(gets=[{"body": "ours"}, {"body": "web edit"}])

    with pytest.raises(RuntimeError, match="remote changed after uploads"):
        sync.push(doc, client, {})

    assert stored_state["pending_remote"] == "web edit"

    client.gets = [{"body": "web edit"}]
    with pytest.raises(RuntimeError, match="remote changed"):
        sync.push(doc, client, {})

    assert "patch" not in client.calls
    assert stored_state["pending_remote"] == "web edit"


def test_resumed_create_warns_when_it_overwrites_a_nonempty_remote(
    tmp_path, monkeypatch, configured, capsys
):
    doc = tmp_path / "report.md"
    write_doc(doc, "local")
    monkeypatch.setattr(
        sync.state,
        "load",
        lambda *args: {
            "remote_base": "",
            "local_base": "",
            "meta_base": {},
            "pending_create": True,
            "body_sent": True,
        },
    )
    client = FakeClient(remote_doc={"body": "interrupted", "content_type": 2})

    sync.push(doc, client, {})

    assert "resuming an interrupted creation" in capsys.readouterr().err
