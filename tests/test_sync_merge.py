from __future__ import annotations

import hashlib
import shutil
from pathlib import Path

import pytest
from _sync_harness import FakeClient, saved_state, write_doc

from elab import frontmatter, sync


@pytest.mark.parametrize("resolved", [False, True])
def test_merge_requires_both_sidecars(tmp_path, resolved):
    doc = tmp_path / "report.md"
    doc.write_text("local\n", encoding="utf-8")
    (tmp_path / "report.base.md").write_text("base\n", encoding="utf-8")

    with pytest.raises(
        RuntimeError,
        match=r"no conflict to merge; run push/pull first .*report\.base\.md.*report\.remote\.md",
    ):
        sync.merge(doc, {}, resolved=resolved)


@pytest.mark.skipif(shutil.which("git") is None, reason="git is not installed")
def test_merge_cleanly_updates_document_and_deletes_sidecars(tmp_path, capsys):
    doc = tmp_path / "report.md"
    base = tmp_path / "report.base.md"
    remote = tmp_path / "report.remote.md"
    doc.write_text("local first\nmiddle\nlast\n", encoding="utf-8")
    base.write_text("first\nmiddle\nlast\n", encoding="utf-8")
    remote.write_text("first\nmiddle\nremote last\n", encoding="utf-8")

    sync.merge(doc, {})

    assert doc.read_text(encoding="utf-8") == "local first\nmiddle\nremote last\n"
    assert not base.exists()
    assert not remote.exists()
    assert capsys.readouterr().out == (
        f"merged cleanly into {doc}; review and run 'elab push'\n"
    )


@pytest.mark.skipif(shutil.which("git") is None, reason="git is not installed")
def test_clean_merge_promotes_pending_remote_and_allows_regular_push(
    tmp_path, monkeypatch, configured
):
    doc = tmp_path / "report.md"
    base_body = "first\nmiddle\nlast\n"
    remote_body = "first\nmiddle\nremote last\n"
    write_doc(doc, "local first\nmiddle\nlast\n")
    stored_state = saved_state(local=base_body, remote=base_body)
    saved_versions = []

    def load_state(*args):
        assert args == ("https://e.example", "experiments", "1")
        return stored_state

    def save_state(*args):
        assert args[:3] == ("https://e.example", "experiments", "1")
        stored_state.clear()
        stored_state.update(args[3])
        saved_versions.append(dict(stored_state))

    monkeypatch.setattr(sync.state, "load", load_state)
    monkeypatch.setattr(sync.state, "save", save_state)
    client = FakeClient(
        gets=[
            {"body": remote_body},
            {"body": remote_body},
            {"body": remote_body},
            {"body": "stored merged", "content_type": 2},
        ]
    )

    with pytest.raises(RuntimeError, match="remote changed"):
        sync.push(doc, client, {})

    assert saved_versions[-1] == {
        "local_base": base_body,
        "remote_base": base_body,
        "team": 7,
        "pending_remote": remote_body,
    }

    sync.merge(doc, {})

    merged_body = "local first\nmiddle\nremote last\n"
    assert frontmatter.parse(doc.read_text(encoding="utf-8"))[1] == merged_body
    assert stored_state == {
        "local_base": base_body,
        "remote_base": remote_body,
        "team": 7,
    }

    sync.push(doc, client, {})

    assert "patch" in client.calls
    assert client.saved_payload is not None
    assert client.saved_payload["body"] == merged_body


def test_resolved_merge_without_git_promotes_pending_remote_and_allows_regular_push(
    tmp_path, monkeypatch, configured, capsys
):
    doc = tmp_path / "report.md"
    base_body = "base\n"
    remote_body = "remote\n"
    write_doc(doc, "local\n")
    stored_state = saved_state(local=base_body, remote=base_body)

    monkeypatch.setattr(sync.state, "load", lambda *args: stored_state)
    monkeypatch.setattr(
        sync.state,
        "save",
        lambda *args: stored_state.clear() or stored_state.update(args[3]),
    )
    monkeypatch.setattr(sync.shutil, "which", lambda command: None)
    monkeypatch.setattr(
        sync.subprocess,
        "run",
        lambda *args, **kwargs: pytest.fail("git merge-file must not run"),
    )
    client = FakeClient(
        gets=[
            {"body": remote_body},
            {"body": remote_body},
            {"body": remote_body},
            {"body": "resolved", "content_type": 2},
        ]
    )

    with pytest.raises(RuntimeError, match="remote changed"):
        sync.push(doc, client, {})

    write_doc(doc, "resolved\n")
    sync.merge(doc, {}, resolved=True)

    assert stored_state == {
        "local_base": base_body,
        "remote_base": remote_body,
        "team": 7,
    }
    assert not (tmp_path / "report.base.md").exists()
    assert not (tmp_path / "report.remote.md").exists()
    assert capsys.readouterr().out == f"marked resolved: {doc}; run 'elab push'\n"

    sync.push(doc, client, {})

    assert client.saved_payload is not None
    assert client.saved_payload["body"] == "resolved\n"


def test_resolved_merge_regular_push_reconflicts_if_remote_changed_again(
    tmp_path, monkeypatch, configured
):
    doc = tmp_path / "report.md"
    base_body = "base\n"
    remote_body = "remote\n"
    newer_remote_body = "newer remote\n"
    write_doc(doc, "local\n")
    stored_state = saved_state(local=base_body, remote=base_body)

    monkeypatch.setattr(sync.state, "load", lambda *args: stored_state)
    monkeypatch.setattr(
        sync.state,
        "save",
        lambda *args: stored_state.clear() or stored_state.update(args[3]),
    )
    monkeypatch.setattr(sync.shutil, "which", lambda command: None)
    monkeypatch.setattr(
        sync.subprocess,
        "run",
        lambda *args, **kwargs: pytest.fail("git merge-file must not run"),
    )
    client = FakeClient(gets=[{"body": remote_body}, {"body": newer_remote_body}])

    with pytest.raises(RuntimeError, match="remote changed"):
        sync.push(doc, client, {})

    write_doc(doc, "resolved\n")
    sync.merge(doc, {}, resolved=True)

    with pytest.raises(RuntimeError, match="remote changed"):
        sync.push(doc, client, {})

    assert stored_state == {
        "local_base": base_body,
        "remote_base": remote_body,
        "team": 7,
        "pending_remote": newer_remote_body,
    }
    assert (tmp_path / "report.base.md").exists()
    remote_meta, sidecar_body = frontmatter.parse(
        (tmp_path / "report.remote.md").read_text(encoding="utf-8")
    )
    assert remote_meta["elab_id"] == 1
    assert sidecar_body == newer_remote_body


@pytest.mark.skipif(shutil.which("git") is None, reason="git is not installed")
def test_conflicted_merge_promotes_pending_remote_and_allows_regular_push(
    tmp_path, monkeypatch, configured
):
    doc = tmp_path / "report.md"
    base_body = "base\n"
    remote_body = "remote\n"
    write_doc(doc, "local\n")
    stored_state = saved_state(local=base_body, remote=base_body)

    monkeypatch.setattr(sync.state, "load", lambda *args: stored_state)
    monkeypatch.setattr(
        sync.state,
        "save",
        lambda *args: stored_state.clear() or stored_state.update(args[3]),
    )
    client = FakeClient(
        gets=[
            {"body": remote_body},
            {"body": remote_body},
            {"body": remote_body},
            {"body": "resolved", "content_type": 2},
        ]
    )

    with pytest.raises(RuntimeError, match="remote changed"):
        sync.push(doc, client, {})

    with pytest.raises(RuntimeError) as exc_info:
        sync.merge(doc, {})

    message = str(exc_info.value)
    assert message == (
        f"1 conflict(s) remain in {doc}; resolve the markers, then run 'elab push'"
    )
    assert "--force" not in message
    merged = doc.read_text(encoding="utf-8")
    assert "<<<<<<< " in merged
    assert "=======\n" in merged
    assert ">>>>>>> " in merged
    base = tmp_path / "report.base.md"
    remote = tmp_path / "report.remote.md"
    assert not base.exists()
    assert not remote.exists()
    assert stored_state == {
        "local_base": base_body,
        "remote_base": remote_body,
        "team": 7,
    }

    write_doc(doc, "resolved\n")
    sync.push(doc, client, {})

    assert client.saved_payload is not None
    assert client.saved_payload["body"] == "resolved\n"


@pytest.mark.skipif(shutil.which("git") is None, reason="git is not installed")
def test_conflicted_merge_regular_push_reconflicts_if_remote_changed_again(
    tmp_path, monkeypatch, configured
):
    doc = tmp_path / "report.md"
    base_body = "base\n"
    remote_body = "remote\n"
    newer_remote_body = "newer remote\n"
    write_doc(doc, "local\n")
    stored_state = saved_state(local=base_body, remote=base_body)

    monkeypatch.setattr(sync.state, "load", lambda *args: stored_state)
    monkeypatch.setattr(
        sync.state,
        "save",
        lambda *args: stored_state.clear() or stored_state.update(args[3]),
    )
    client = FakeClient(gets=[{"body": remote_body}, {"body": newer_remote_body}])

    with pytest.raises(RuntimeError, match="remote changed"):
        sync.push(doc, client, {})
    with pytest.raises(RuntimeError, match="conflict.*run 'elab push'"):
        sync.merge(doc, {})

    assert not (tmp_path / "report.base.md").exists()
    assert not (tmp_path / "report.remote.md").exists()

    write_doc(doc, "resolved\n")
    with pytest.raises(RuntimeError, match="remote changed"):
        sync.push(doc, client, {})

    assert stored_state == {
        "local_base": base_body,
        "remote_base": remote_body,
        "team": 7,
        "pending_remote": newer_remote_body,
    }
    assert (tmp_path / "report.base.md").exists()
    remote_meta, sidecar_body = frontmatter.parse(
        (tmp_path / "report.remote.md").read_text(encoding="utf-8")
    )
    assert remote_meta["elab_id"] == 1
    assert sidecar_body == newer_remote_body


@pytest.mark.parametrize("resolved", [False, True])
def test_merge_profile_mismatch_stops_before_git_and_keeps_sidecars(
    tmp_path, monkeypatch, resolved
):
    doc = tmp_path / "report.md"
    base = tmp_path / "report.base.md"
    remote = tmp_path / "report.remote.md"
    write_doc(doc, "local\n", profile="labA")
    base.write_text("base\n", encoding="utf-8")
    remote.write_text("remote\n", encoding="utf-8")
    config = {
        "profiles": {
            "labA": {"base_url": "https://a.example"},
            "labB": {"base_url": "https://b.example"},
        }
    }
    monkeypatch.setattr(
        sync.subprocess,
        "run",
        lambda *args, **kwargs: pytest.fail("git merge-file must not run"),
    )

    with pytest.raises(
        ValueError, match="profile mismatch between frontmatter and CLI"
    ):
        sync.merge(doc, config, profile="labB", resolved=resolved)

    assert frontmatter.parse(doc.read_text(encoding="utf-8"))[1] == "local\n"
    assert base.exists()
    assert remote.exists()


def test_merge_without_git_leaves_document_untouched(tmp_path, monkeypatch, capsys):
    doc = tmp_path / "report.md"
    base = tmp_path / "report.base.md"
    remote = tmp_path / "report.remote.md"
    doc.write_text("local\n", encoding="utf-8")
    base.write_text("base\n", encoding="utf-8")
    remote.write_text("remote\n", encoding="utf-8")
    monkeypatch.setattr(shutil, "which", lambda command: None)

    with pytest.raises(
        RuntimeError,
        match=(
            r"merge .*report\.base\.md.*report\.remote\.md by hand, then run "
            r"'elab merge --resolved'"
        ),
    ):
        sync.merge(doc, {})

    assert doc.read_text(encoding="utf-8") == "local\n"
    assert base.exists()
    assert remote.exists()
    assert "Traceback" not in capsys.readouterr().err


@pytest.mark.parametrize("returncode", [-9, 128])
def test_merge_reports_git_execution_errors(tmp_path, monkeypatch, returncode):
    doc = tmp_path / "report.md"
    base = tmp_path / "report.base.md"
    remote = tmp_path / "report.remote.md"
    doc.write_text("local\n", encoding="utf-8")
    base.write_text("base\n", encoding="utf-8")
    remote.write_text("remote\n", encoding="utf-8")
    monkeypatch.setattr(shutil, "which", lambda command: "/usr/bin/git")
    monkeypatch.setattr(
        sync.subprocess,
        "run",
        lambda *args, **kwargs: sync.subprocess.CompletedProcess(
            args[0], returncode, stderr="git failed"
        ),
    )

    with pytest.raises(RuntimeError, match="git failed"):
        sync.merge(doc, {})

    assert base.exists()
    assert remote.exists()


@pytest.mark.parametrize("sidecar", ["report.remote.md", "report.base.md"])
def test_merge_sidecar_collision_stops_before_attachment_download(
    tmp_path, monkeypatch, configured, sidecar
):
    doc = tmp_path / "report.md"
    write_doc(doc, "local")
    upload = {
        "id": 3,
        "long_name": "aa/existing",
        "real_name": sidecar,
        "storage": 1,
    }
    url = sync.download_url("https://e.example", upload)
    remote_body = f"web edit [file]({url})" if ".remote." in sidecar else "web edit"
    base_body = "base" if ".remote." in sidecar else f"base [file]({url})"
    client = FakeClient(gets=[{"body": remote_body}], uploads=[upload])
    monkeypatch.setattr(sync.state, "load", lambda *args: saved_state(remote=base_body))

    with pytest.raises(RuntimeError, match=f"sidecar.*{sidecar}"):
        sync.push(doc, client, {})

    assert client.calls == ["me", "get", "uploads"]
    assert not (tmp_path / sidecar).exists()


def test_merge_sidecar_collision_is_case_insensitive(tmp_path, monkeypatch, configured):
    doc = tmp_path / "report.md"
    write_doc(doc, "local")
    upload = {
        "id": 3,
        "long_name": "aa/existing",
        "real_name": "REPORT.REMOTE.MD",
        "storage": 1,
    }
    url = sync.download_url("https://e.example", upload)
    client = FakeClient(gets=[{"body": f"web edit [file]({url})"}], uploads=[upload])
    monkeypatch.setattr(sync.state, "load", lambda *args: saved_state())

    with pytest.raises(RuntimeError, match="sidecar.*REPORT.REMOTE.MD"):
        sync.push(doc, client, {})

    assert client.calls == ["me", "get", "uploads"]


def test_merge_command_preserves_and_quotes_relative_document_path(
    tmp_path, monkeypatch, configured, capsys
):
    monkeypatch.chdir(tmp_path)
    doc = Path("notes") / "my report.md"
    doc.parent.mkdir()
    write_doc(doc, "edited")
    client = FakeClient(gets=[{"body": "web edit"}])
    monkeypatch.setattr(sync.state, "load", lambda *args: saved_state())

    with pytest.raises(RuntimeError, match="local changes conflict"):
        sync.pull(doc, client, {})

    assert (
        "git merge-file -- 'notes/my report.md' 'notes/my report.base.md' "
        "'notes/my report.remote.md'"
    ) in capsys.readouterr().err


def test_merge_preserves_different_same_name_remote_and_base_attachments(
    tmp_path, monkeypatch, configured
):
    doc = tmp_path / "report.md"
    write_doc(doc, "edited")
    (tmp_path / "figure.png").write_bytes(b"local")
    remote_upload = {
        "id": 3,
        "long_name": "aa/remote",
        "real_name": "figure.png",
        "storage": 1,
    }
    base_upload = {
        "id": 4,
        "long_name": "aa/base",
        "real_name": "figure.png",
        "storage": 1,
    }
    remote_url = sync.download_url("https://e.example", remote_upload)
    base_url = sync.download_url("https://e.example", base_upload)

    class DifferentAttachmentsClient(FakeClient):
        def download(self, entity, eid, upload_id):
            self.calls.append(f"download:{upload_id}")
            return {3: b"remote", 4: b"base"}[upload_id]

    client = DifferentAttachmentsClient(
        gets=[{"body": f"remote [figure]({remote_url})"}],
        uploads=[remote_upload, base_upload],
    )
    monkeypatch.setattr(
        sync.state,
        "load",
        lambda *args: saved_state(remote=f"base [figure]({base_url})"),
    )

    with pytest.raises(RuntimeError, match="local changes conflict"):
        sync.pull(doc, client, {})

    assert (tmp_path / "figure.png").read_bytes() == b"local"
    assert (tmp_path / "figure.png.remote").read_bytes() == b"remote"
    assert (tmp_path / "figure.png.base").read_bytes() == b"base"


def test_merge_rejects_different_same_basename_uploads_in_remote_body(
    tmp_path, monkeypatch, configured
):
    doc = tmp_path / "report.md"
    write_doc(doc, "edited")
    uploads = [
        {
            "id": upload_id,
            "long_name": f"aa/{upload_id}",
            "real_name": "figure.png",
            "storage": 1,
            "hash": hashlib.sha256(content).hexdigest(),
            "hash_algorithm": "sha256",
        }
        for upload_id, content in ((3, b"first"), (4, b"second"))
    ]
    urls = [sync.download_url("https://e.example", upload) for upload in uploads]
    client = FakeClient(
        gets=[{"body": f"[first]({urls[0]}) [second]({urls[1]})"}],
        uploads=uploads,
    )
    monkeypatch.setattr(sync.state, "load", lambda *args: saved_state())

    with pytest.raises(RuntimeError, match="basename collision.*figure.png"):
        sync.pull(doc, client, {})

    assert "download" not in client.calls
    assert not (tmp_path / "report.remote.md").exists()
    assert not (tmp_path / "report.base.md").exists()


@pytest.mark.skipif(shutil.which("git") is None, reason="git is not installed")
def test_merge_handles_document_name_starting_with_dash(tmp_path, monkeypatch):
    monkeypatch.chdir(tmp_path)
    (tmp_path / "-note.md").write_text("local first\nmiddle\nlast\n", encoding="utf-8")
    (tmp_path / "-note.base.md").write_text("first\nmiddle\nlast\n", encoding="utf-8")
    (tmp_path / "-note.remote.md").write_text(
        "first\nmiddle\nremote last\n", encoding="utf-8"
    )

    sync.merge(Path("-note.md"), {})

    assert (tmp_path / "-note.md").read_text(encoding="utf-8") == (
        "local first\nmiddle\nremote last\n"
    )
    assert not (tmp_path / "-note.base.md").exists()
    assert not (tmp_path / "-note.remote.md").exists()
