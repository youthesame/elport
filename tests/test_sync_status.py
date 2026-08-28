from __future__ import annotations

import pytest
from _sync_harness import FakeClient, saved_state, write_doc

from elport import sync


def test_status_distinguishes_missing_base(tmp_path, monkeypatch, configured, capsys):
    doc = tmp_path / "report.md"
    write_doc(doc, "local")
    client = FakeClient(gets=[{"body": "remote", "content_type": 2}])
    monkeypatch.setattr(sync.state, "load", lambda *args: None)

    sync.status(doc, client, {})

    output = capsys.readouterr().out
    assert "local: base unavailable (comparison unavailable)" in output
    assert "remote: base unavailable (comparison unavailable)" in output
    assert "remote: changed" not in output


def test_status_degrades_remote_fields_when_offline(
    tmp_path, monkeypatch, configured, capsys
):
    doc = tmp_path / "report.md"
    attachment = tmp_path / "data.csv"
    attachment.write_text("data", encoding="utf-8")
    write_doc(doc, "[data](data.csv)")

    class OfflineClient(FakeClient):
        def uploads(self, entity, eid):
            raise ConnectionError("offline")

    client = OfflineClient()
    monkeypatch.setattr(sync.state, "load", lambda *args: saved_state())

    sync.status(doc, client, {})

    lines = capsys.readouterr().out.splitlines()
    assert lines[:2] == [
        'local: dirty (use "elport push")',
        "uploads local: data.csv",
    ]
    assert "uploads reuse: unavailable (offline?)" in lines
    assert "remote: unavailable (offline?)" in lines
    assert "mode: unavailable (offline?)" in lines


def test_status_prints_local_plan_before_remote_get_failure(
    tmp_path, monkeypatch, configured, capsys
):
    doc = tmp_path / "report.md"
    write_doc(doc, "local")
    observed = []

    class OfflineClient(FakeClient):
        def get(self, entity, eid):
            observed.extend(capsys.readouterr().out.splitlines())
            raise ConnectionError("offline")

    client = OfflineClient()
    monkeypatch.setattr(sync.state, "load", lambda *args: saved_state())

    sync.status(doc, client, {})

    assert observed == ["local: clean", "uploads local: none"]
    assert capsys.readouterr().out.splitlines() == [
        "uploads reuse: unavailable (offline?)",
        "remote: unavailable (offline?)",
        "mode: unavailable (offline?)",
    ]


def test_status_does_not_treat_value_errors_as_offline(
    tmp_path, monkeypatch, configured, capsys
):
    doc = tmp_path / "report.md"
    write_doc(doc, "local")

    class InvalidClient(FakeClient):
        def uploads(self, entity, eid):
            raise ValueError("credentials unavailable")

    client = InvalidClient()
    monkeypatch.setattr(sync.state, "load", lambda *args: saved_state())

    with pytest.raises(ValueError, match="credentials unavailable"):
        sync.status(doc, client, {})

    assert capsys.readouterr().out.splitlines() == [
        "local: clean",
        "uploads local: none",
    ]


def test_status_suggests_actions_for_local_and_remote_changes(
    tmp_path, monkeypatch, configured, capsys
):
    doc = tmp_path / "report.md"
    write_doc(doc, "local change")
    client = FakeClient(gets=[{"body": "remote change", "content_type": 2}])
    monkeypatch.setattr(sync.state, "load", lambda *args: saved_state())

    sync.status(doc, client, {})

    lines = capsys.readouterr().out.splitlines()
    assert 'local: dirty (use "elport push")' in lines
    assert 'remote: changed (use "elport pull")' in lines


def test_status_prints_pending_declared_permission_change(
    tmp_path, monkeypatch, configured, capsys
):
    doc = tmp_path / "report.md"
    write_doc(doc, "local", read="team")
    client = FakeClient(
        gets=[{"body": "remote", "content_type": 2, "canread_base": 10}]
    )
    monkeypatch.setattr(sync.state, "load", lambda *args: saved_state())

    sync.status(doc, client, {})

    assert "permissions: read owner→team\n" in capsys.readouterr().out


def test_status_prints_unchanged_for_matching_declared_permissions(
    tmp_path, monkeypatch, configured, capsys
):
    doc = tmp_path / "report.md"
    write_doc(doc, "local", read="team", write="owner")
    client = FakeClient(
        gets=[
            {
                "body": "remote",
                "content_type": 2,
                "canread_base": 30,
                "canwrite_base": 10,
            }
        ]
    )
    monkeypatch.setattr(sync.state, "load", lambda *args: saved_state())

    sync.status(doc, client, {})

    assert "permissions: read unchanged, write unchanged\n" in capsys.readouterr().out


def test_status_omits_permissions_when_none_are_declared(
    tmp_path, monkeypatch, configured, capsys
):
    doc = tmp_path / "report.md"
    write_doc(doc, "local")
    client = FakeClient(gets=[{"body": "remote", "content_type": 2}])
    monkeypatch.setattr(sync.state, "load", lambda *args: saved_state())

    sync.status(doc, client, {})

    assert "permissions:" not in capsys.readouterr().out


def test_remote_diff_ignores_line_endings_and_trailing_newlines(
    tmp_path, monkeypatch, configured, capsys
):
    doc = tmp_path / "report.md"
    write_doc(doc, "same\n")
    client = FakeClient(gets=[{"body": "same\r\n\r\n"}])
    monkeypatch.setattr(sync.state, "load", lambda *args: saved_state())

    sync.diff(doc, client, {})

    captured = capsys.readouterr()
    assert captured.out == ""
    assert "server normalization noise" not in captured.err


def test_remote_diff_prints_normalization_note_for_real_difference(
    tmp_path, monkeypatch, configured, capsys
):
    doc = tmp_path / "report.md"
    write_doc(doc, "local\n")
    client = FakeClient(gets=[{"body": "remote\n"}])
    monkeypatch.setattr(sync.state, "load", lambda *args: saved_state())

    sync.diff(doc, client, {})

    captured = capsys.readouterr()
    assert "@@" in captured.out
    assert "-remote" in captured.out
    assert "+local" in captured.out
    assert "server normalization noise" in captured.err


def test_remote_tags_handles_null():
    assert sync._remote_tags({"tags": None}) == set()
