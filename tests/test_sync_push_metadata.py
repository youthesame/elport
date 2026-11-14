from __future__ import annotations

import datetime

import pytest
from _sync_harness import (
    FakeClient,
    saved_state,
    write_doc,
)

from elport import sync


def test_dry_run_previews_title_category_and_only_new_tags(
    tmp_path, monkeypatch, configured, capsys
):
    doc = tmp_path / "report.md"
    write_doc(
        doc,
        "local",
        title="Dry run",
        category="Synthesis",
        tags=["PCR", "new"],
    )
    client = FakeClient(gets=[{"body": "remote", "tags": "PCR|existing"}])
    monkeypatch.setattr(sync.state, "load", lambda *args: saved_state())

    sync.push(doc, client, {}, dry_run=True)

    output = capsys.readouterr().out
    assert "title: Dry run\n" in output
    assert "category: 9\n" in output
    assert "tags +: new\n" in output
    assert "tags +: PCR" not in output
    assert not {"create", "upload", "patch", "tag:PCR", "tag:new"} & set(client.calls)


@pytest.mark.parametrize("title", [datetime.date(2026, 8, 8), 20260808])
def test_push_normalizes_yaml_scalar_metadata_to_strings(
    tmp_path, monkeypatch, configured, title
):
    doc = tmp_path / "report.md"
    write_doc(doc, "local", title=title, tags=[123, datetime.date(2026, 8, 8)])
    client = FakeClient(
        gets=[
            {"body": "remote", "tags": ""},
            {"body": "stored", "content_type": 2},
        ]
    )
    monkeypatch.setattr(sync.state, "load", lambda *args: saved_state())
    monkeypatch.setattr(sync.state, "save", lambda *args: None)

    sync.push(doc, client, {})

    assert client.saved_payload is not None
    assert client.saved_payload["title"] == str(title)
    assert "tag:123" in client.calls
    assert "tag:2026-08-08" in client.calls


def test_push_saves_metadata_base(tmp_path, monkeypatch, configured):
    doc = tmp_path / "report.md"
    write_doc(doc, "local", category=9, tags=["x"])
    client = FakeClient(gets=[{"body": "remote"}])
    stored_state = saved_state()
    monkeypatch.setattr(sync.state, "load", lambda *args: stored_state)
    monkeypatch.setattr(sync.state, "save", lambda *args: stored_state.update(args[-1]))

    sync.push(doc, client, {})

    assert stored_state["meta_base"] == {"title": "Test", "category": "9"}


def test_existing_tags_are_not_posted_again(tmp_path, monkeypatch, configured):
    doc = tmp_path / "report.md"
    write_doc(doc, "local", tags=["PCR", "new"])
    client = FakeClient(
        gets=[
            {"body": "remote", "tags": "PCR, existing"},
            {"body": "stored", "content_type": 2},
        ]
    )
    monkeypatch.setattr(sync.state, "load", lambda *args: saved_state())
    monkeypatch.setattr(sync.state, "save", lambda *args: None)

    sync.push(doc, client, {})

    assert "tag:PCR" not in client.calls
    assert client.calls.count("tag:new") == 1


def test_pipe_separated_existing_tags_are_not_posted_again(
    tmp_path, monkeypatch, configured
):
    doc = tmp_path / "report.md"
    write_doc(doc, "local", tags=["PCR", "new"])
    client = FakeClient(
        gets=[
            {"body": "remote", "tags": "PCR|existing"},
            {"body": "stored", "content_type": 2},
        ]
    )
    monkeypatch.setattr(sync.state, "load", lambda *args: saved_state())
    monkeypatch.setattr(sync.state, "save", lambda *args: None)

    sync.push(doc, client, {})

    assert "tag:PCR" not in client.calls
    assert client.calls.count("tag:new") == 1


def test_verified_state_is_saved_before_tag_failure(tmp_path, monkeypatch, configured):
    doc = tmp_path / "report.md"
    write_doc(doc, "local", tags=["new"])

    class FailingTagClient(FakeClient):
        def add_tag(self, entity, eid, tag):
            self.calls.append(f"tag:{tag}")
            raise RuntimeError("tag failed")

    client = FailingTagClient(
        gets=[
            {"body": "remote", "tags": ""},
            {"body": "stored", "content_type": 2},
        ]
    )
    monkeypatch.setattr(sync.state, "load", lambda *args: saved_state())
    saved = []
    monkeypatch.setattr(sync.state, "save", lambda *args: saved.append(args))

    with pytest.raises(RuntimeError, match="tag failed"):
        sync.push(doc, client, {})

    assert saved == [
        (
            "https://e.example",
            "experiments",
            "1",
            {
                "remote_base": "stored",
                "local_base": "local",
                "meta_base": {"title": "Test"},
                "path": str(doc.resolve()),
            },
        )
    ]


def test_push_looks_a_category_up_in_the_entitys_team(
    tmp_path, monkeypatch, configured
):
    # Categories belong to a team; the active one may be another of the user's.
    doc = tmp_path / "report.md"
    write_doc(doc, "local", category="Synthesis")
    looked_up = []

    class OtherTeamClient(FakeClient):
        def me(self):
            return {**super().me(), "team": 8}

        def categories(self, team_id, entity):
            looked_up.append(team_id)
            return super().categories(team_id, entity)

    client = OtherTeamClient(
        gets=[{"body": "remote", "team": 7}, {"body": "stored", "content_type": 2}]
    )
    monkeypatch.setattr(sync.state, "load", lambda *args: saved_state())
    monkeypatch.setattr(sync.state, "save", lambda *args: None)

    sync.push(doc, client, {})

    assert looked_up == [7]


def test_category_name_is_resolved(tmp_path, monkeypatch, configured):
    doc = tmp_path / "report.md"
    write_doc(doc, "local", category="Synthesis")
    client = FakeClient(
        gets=[
            {"body": "remote"},
            {"body": "stored", "content_type": 2},
        ]
    )
    monkeypatch.setattr(sync.state, "load", lambda *args: saved_state())
    monkeypatch.setattr(sync.state, "save", lambda *args: None)

    sync.push(doc, client, {})

    assert client.saved_payload is not None
    assert client.saved_payload["category"] == 9


def test_category_valid_numeric_id_is_validated_and_sent(
    tmp_path, monkeypatch, configured
):
    doc = tmp_path / "report.md"
    write_doc(doc, "local", category=9)
    client = FakeClient(
        gets=[
            {"body": "remote"},
            {"body": "stored", "content_type": 2},
        ]
    )
    monkeypatch.setattr(sync.state, "load", lambda *args: saved_state())
    monkeypatch.setattr(sync.state, "save", lambda *args: None)

    sync.push(doc, client, {})

    assert "categories" in client.calls
    assert client.saved_payload is not None
    assert client.saved_payload["category"] == 9


def test_category_unknown_numeric_id_is_rejected_before_mutation(tmp_path, configured):
    doc = tmp_path / "report.md"
    write_doc(doc, "local", category=99)
    client = FakeClient()

    with pytest.raises(RuntimeError, match="category not found: 99"):
        sync.push(doc, client, {})

    assert client.calls == ["me", "get", "categories"]


@pytest.mark.parametrize("status", ["Running", 5])
def test_status_name_or_valid_numeric_id_is_resolved_and_sent(
    tmp_path, monkeypatch, configured, status
):
    doc = tmp_path / "report.md"
    write_doc(doc, "local", status=status)
    client = FakeClient(
        gets=[
            {"body": "remote"},
            {"body": "stored", "content_type": 2},
        ]
    )
    monkeypatch.setattr(sync.state, "load", lambda *args: saved_state())
    monkeypatch.setattr(sync.state, "save", lambda *args: None)

    sync.push(doc, client, {})

    assert "statuses" in client.calls
    assert client.saved_payload is not None
    assert client.saved_payload["status"] == 5


@pytest.mark.parametrize("status", ["Missing", 99])
def test_unknown_status_is_rejected_before_mutation(tmp_path, configured, status):
    doc = tmp_path / "report.md"
    write_doc(doc, "local", status=status)
    client = FakeClient()

    with pytest.raises(RuntimeError, match=f"status not found: {status}"):
        sync.push(doc, client, {})

    assert client.calls == ["me", "get", "statuses"]


def test_push_without_status_does_not_send_status(tmp_path, monkeypatch, configured):
    doc = tmp_path / "report.md"
    write_doc(doc, "local")
    client = FakeClient(
        gets=[
            {"body": "remote"},
            {"body": "stored", "content_type": 2},
        ]
    )
    monkeypatch.setattr(sync.state, "load", lambda *args: saved_state())
    monkeypatch.setattr(sync.state, "save", lambda *args: None)

    sync.push(doc, client, {})

    assert client.saved_payload is not None
    assert "status" not in client.saved_payload
    assert "statuses" not in client.calls


def test_dry_run_previews_declared_status_without_mutation(
    tmp_path, monkeypatch, configured, capsys
):
    doc = tmp_path / "report.md"
    write_doc(doc, "local", status="Running")
    client = FakeClient(gets=[{"body": "remote"}])
    monkeypatch.setattr(sync.state, "load", lambda *args: saved_state())
    monkeypatch.setattr(sync.state, "save", lambda *args: pytest.fail("state changed"))

    sync.push(doc, client, {}, dry_run=True)

    assert "status: Running\n" in capsys.readouterr().out
    assert client.calls == ["me", "get", "statuses"]
