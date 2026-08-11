from __future__ import annotations

import datetime
import hashlib
import shlex
import shutil
from pathlib import Path

import pytest

from elab import frontmatter, sync


class FakeClient:
    def __init__(self, gets=None, uploads=None):
        self.gets = list(gets or [])
        self.upload_list = list(uploads or [])
        self.calls = []
        self.saved_payload = None

    def me(self):
        self.calls.append("me")
        return {"team": 7}

    def get(self, entity, eid):
        self.calls.append("get")
        return self.gets.pop(0)

    def uploads(self, entity, eid):
        self.calls.append("uploads")
        return self.upload_list

    def upload(self, entity, eid, path):
        self.calls.append("upload")
        uploaded = {
            "id": 8,
            "long_name": "aa/uploaded",
            "real_name": path.name,
            "storage": 1,
        }
        self.upload_list.append(uploaded)
        return uploaded

    def patch(self, entity, eid, payload):
        self.calls.append("patch")
        self.saved_payload = payload
        return {}

    def add_tag(self, entity, eid, tag):
        self.calls.append(f"tag:{tag}")

    def categories(self, team_id, entity):
        self.calls.append("categories")
        return [{"id": 9, "title": "Synthesis"}]

    def create(self, entity, title):
        self.calls.append("create")
        return {"id": 42}

    def download(self, entity, eid, upload_id):
        self.calls.append("download")
        return b"data"


@pytest.fixture
def configured(monkeypatch):
    monkeypatch.setattr(
        sync.config_module,
        "resolve",
        lambda config, profile, meta: ("test", "https://e.example", "secret", True),
    )
    monkeypatch.setattr(
        sync.config_module,
        "base_target",
        lambda config, profile, meta: ("test", "https://e.example", True),
    )


def write_doc(path: Path, body: str, **extra):
    meta = {"elab_id": 1, "entity": "experiments", "title": "Test", **extra}
    path.write_text(frontmatter.render(meta, body))


def saved_state(local="local", remote="remote"):
    return {"local_base": local, "remote_base": remote, "team": 7}


def test_merge_requires_both_sidecars(tmp_path):
    doc = tmp_path / "report.md"
    doc.write_text("local\n", encoding="utf-8")
    (tmp_path / "report.base.md").write_text("base\n", encoding="utf-8")

    with pytest.raises(
        RuntimeError,
        match=r"no conflict to merge; run push/pull first .*report\.base\.md.*report\.remote\.md",
    ):
        sync.merge(doc)


@pytest.mark.skipif(shutil.which("git") is None, reason="git is not installed")
def test_merge_cleanly_updates_document_and_deletes_sidecars(tmp_path, capsys):
    doc = tmp_path / "report.md"
    base = tmp_path / "report.base.md"
    remote = tmp_path / "report.remote.md"
    doc.write_text("local first\nmiddle\nlast\n", encoding="utf-8")
    base.write_text("first\nmiddle\nlast\n", encoding="utf-8")
    remote.write_text("first\nmiddle\nremote last\n", encoding="utf-8")

    sync.merge(doc)

    assert doc.read_text(encoding="utf-8") == "local first\nmiddle\nremote last\n"
    assert not base.exists()
    assert not remote.exists()
    assert capsys.readouterr().out == (
        f"merged cleanly into {doc}; review and run 'elab push'\n"
    )


@pytest.mark.skipif(shutil.which("git") is None, reason="git is not installed")
def test_merge_conflict_keeps_sidecars_and_writes_markers(tmp_path):
    doc = tmp_path / "report.md"
    base = tmp_path / "report.base.md"
    remote = tmp_path / "report.remote.md"
    doc.write_text("local\n", encoding="utf-8")
    base.write_text("base\n", encoding="utf-8")
    remote.write_text("remote\n", encoding="utf-8")

    with pytest.raises(RuntimeError, match=f"1 conflict.*{doc}"):
        sync.merge(doc)

    merged = doc.read_text(encoding="utf-8")
    assert "<<<<<<< " in merged
    assert "=======\n" in merged
    assert ">>>>>>> " in merged
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
        RuntimeError, match=r"merge .*report\.base\.md.*report\.remote\.md manually"
    ):
        sync.merge(doc)

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
        sync.merge(doc)

    assert base.exists()
    assert remote.exists()


def test_push_rejects_unresolved_merge_markers_before_network(tmp_path, configured):
    doc = tmp_path / "report.md"
    write_doc(
        doc, "<<<<<<< report.md\nlocal\n=======\nremote\n>>>>>>> report.remote.md\n"
    )
    client = FakeClient()

    with pytest.raises(RuntimeError, match=f"unresolved merge markers in {doc}"):
        sync.push(doc, client, {})

    assert client.calls == []


def test_push_does_not_treat_separator_alone_as_merge_markers(
    tmp_path, configured, capsys
):
    doc = tmp_path / "report.md"
    doc.write_text("Heading\n=======\n", encoding="utf-8")
    client = FakeClient()

    sync.push(doc, client, {}, dry_run=True)

    assert client.calls == ["me"]
    assert capsys.readouterr().out == "Upload plan:\n"


def test_force_push_bypasses_unresolved_merge_marker_guard(
    tmp_path, configured, capsys
):
    doc = tmp_path / "report.md"
    doc.write_text(
        "<<<<<<< report.md\nlocal\n=======\nremote\n>>>>>>> report.remote.md\n",
        encoding="utf-8",
    )
    client = FakeClient()

    sync.push(doc, client, {}, dry_run=True, force=True)

    assert client.calls == ["me"]
    assert capsys.readouterr().out == "Upload plan:\n"


def test_push_order_includes_post_upload_recheck(
    tmp_path, monkeypatch, configured, capsys
):
    doc = tmp_path / "report.md"
    attachment = tmp_path / "data.csv"
    attachment.write_text("new")
    write_doc(doc, "[data](data.csv)")
    client = FakeClient(
        gets=[
            {"body": "remote", "tags": ""},
            {"body": "remote"},
            {
                "body": "stored",
                "content_type": 2,
                "sharelink": "https://e.example/experiments/1",
            },
        ]
    )
    monkeypatch.setattr(sync.state, "load", lambda *args: saved_state())
    monkeypatch.setattr(sync.state, "save", lambda *args: client.calls.append("save"))

    sync.push(doc, client, {})

    assert client.calls == [
        "me",
        "get",
        "uploads",
        "upload",
        "get",
        "patch",
        "get",
        "save",
    ]
    assert capsys.readouterr().out == (
        "pushed experiments/1: Test\n"
        "  body updated (markdown)\n"
        "  uploads: 0 reused, 1 new\n"
        "  → https://e.example/experiments/1\n"
    )


def test_push_summary_omits_empty_sharelink(tmp_path, monkeypatch, configured, capsys):
    doc = tmp_path / "report.md"
    write_doc(doc, "local")
    client = FakeClient(
        gets=[
            {"body": "remote", "tags": ""},
            {"body": "remote"},
            {"body": "stored", "content_type": 2, "sharelink": ""},
        ]
    )
    monkeypatch.setattr(sync.state, "load", lambda *args: saved_state())
    monkeypatch.setattr(sync.state, "save", lambda *args: None)

    sync.push(doc, client, {})

    assert capsys.readouterr().out == (
        "pushed experiments/1: Test\n"
        "  body unchanged (markdown)\n"
        "  uploads: 0 reused, 0 new\n"
    )


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


def test_push_stops_before_target_reads_when_team_changed(
    tmp_path, monkeypatch, configured
):
    doc = tmp_path / "report.md"
    write_doc(doc, "local", category="Synthesis")

    class OtherTeamClient(FakeClient):
        def me(self):
            self.calls.append("me")
            return {"team": 8}

    client = OtherTeamClient()
    monkeypatch.setattr(sync.state, "load", lambda *args: saved_state())

    with pytest.raises(RuntimeError, match="active team differs"):
        sync.push(doc, client, {})

    assert client.calls == ["me"]


def test_push_stops_if_remote_changes_after_uploads(tmp_path, monkeypatch, configured):
    doc = tmp_path / "report.md"
    (tmp_path / "data.csv").write_text("new")
    write_doc(doc, "[data](data.csv)")
    client = FakeClient(gets=[{"body": "remote"}, {"body": "web edit"}])
    monkeypatch.setattr(sync.state, "load", lambda *args: saved_state())
    monkeypatch.setattr(sync.state, "save", lambda *args: pytest.fail("state changed"))

    with pytest.raises(RuntimeError, match="after uploads"):
        sync.push(doc, client, {})

    assert "upload" in client.calls
    assert "patch" not in client.calls


def test_push_stops_on_initial_conflict_with_merge_inputs(
    tmp_path, monkeypatch, configured, capsys
):
    doc = tmp_path / "report.md"
    write_doc(doc, "local")
    upload = {
        "id": 3,
        "long_name": "aa/existing",
        "real_name": "figure.png",
        "storage": 1,
    }
    url = sync.download_url("https://e.example", upload)
    client = FakeClient(gets=[{"body": f"web edit [figure]({url})"}], uploads=[upload])
    monkeypatch.setattr(
        sync.state,
        "load",
        lambda *args: saved_state(remote=f"base [figure]({url})"),
    )

    with pytest.raises(RuntimeError, match="remote changed"):
        sync.push(doc, client, {})

    assert client.calls == ["me", "get", "uploads", "download"]
    assert (tmp_path / "figure.png").read_bytes() == b"data"
    remote_meta, remote_body = frontmatter.parse(
        (tmp_path / "report.remote.md").read_text(encoding="utf-8")
    )
    base_meta, base_body = frontmatter.parse(
        (tmp_path / "report.base.md").read_text(encoding="utf-8")
    )
    local_meta, _ = frontmatter.parse(doc.read_text(encoding="utf-8"))
    assert remote_meta == base_meta == local_meta
    assert remote_body == "web edit [figure](figure.png)"
    assert base_body == "base [figure](figure.png)"
    assert (
        shlex.join(
            [
                "git",
                "merge-file",
                str(doc),
                str(doc.with_name("report.base.md")),
                str(doc.with_name("report.remote.md")),
            ]
        )
        in capsys.readouterr().err
    )


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


def test_post_upload_conflict_refreshes_merge_inputs(
    tmp_path, monkeypatch, configured, capsys
):
    doc = tmp_path / "report.md"
    (tmp_path / "data.csv").write_text("new", encoding="utf-8")
    write_doc(doc, "[data](data.csv)")
    uploaded = {
        "id": 8,
        "long_name": "aa/uploaded",
        "real_name": "data.csv",
        "storage": 1,
    }
    url = sync.download_url("https://e.example", uploaded)

    class SameDownloadClient(FakeClient):
        def download(self, entity, eid, upload_id):
            self.calls.append("download")
            return b"new"

    client = SameDownloadClient(
        gets=[{"body": "remote"}, {"body": f"web edit [data]({url})"}]
    )
    monkeypatch.setattr(sync.state, "load", lambda *args: saved_state())

    with pytest.raises(RuntimeError, match="after uploads"):
        sync.push(doc, client, {})

    remote_meta, remote_body = frontmatter.parse(
        (tmp_path / "report.remote.md").read_text(encoding="utf-8")
    )
    base_meta, base_body = frontmatter.parse(
        (tmp_path / "report.base.md").read_text(encoding="utf-8")
    )
    local_meta, _ = frontmatter.parse(doc.read_text(encoding="utf-8"))
    assert remote_meta == base_meta == local_meta
    assert remote_body == "web edit [data](data.csv)"
    assert base_body == "remote"
    assert "download" in client.calls
    assert not (tmp_path / "data.csv.remote").exists()
    assert (
        shlex.join(
            [
                "git",
                "merge-file",
                str(doc),
                str(doc.with_name("report.base.md")),
                str(doc.with_name("report.remote.md")),
            ]
        )
        in capsys.readouterr().err
    )


def test_post_upload_conflict_refreshes_web_attachment_metadata(
    tmp_path, monkeypatch, configured
):
    doc = tmp_path / "report.md"
    (tmp_path / "data.csv").write_text("new", encoding="utf-8")
    write_doc(doc, "[data](data.csv)")
    web_upload = {
        "id": 9,
        "long_name": "aa/web",
        "real_name": "web.png",
        "storage": 1,
    }
    url = sync.download_url("https://e.example", web_upload)

    class WebEditClient(FakeClient):
        def uploads(self, entity, eid):
            self.calls.append("uploads")
            if self.calls.count("uploads") > 1:
                return [*self.upload_list, web_upload]
            return self.upload_list

    client = WebEditClient(
        gets=[{"body": "remote"}, {"body": f"web edit [image]({url})"}]
    )
    monkeypatch.setattr(sync.state, "load", lambda *args: saved_state())

    with pytest.raises(RuntimeError, match="after uploads"):
        sync.push(doc, client, {})

    assert (tmp_path / "web.png").read_bytes() == b"data"
    assert "web edit [image](web.png)" in (tmp_path / "report.remote.md").read_text(
        encoding="utf-8"
    )


def test_push_without_base_stops_before_upload_listing(
    tmp_path, monkeypatch, configured
):
    doc = tmp_path / "report.md"
    write_doc(doc, "local")
    client = FakeClient(gets=[{"body": "web edit"}])
    monkeypatch.setattr(sync.state, "load", lambda *args: None)

    with pytest.raises(RuntimeError, match="base unavailable"):
        sync.push(doc, client, {})

    assert client.calls == ["me", "get"]


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


def test_dry_run_has_no_mutating_calls(tmp_path, monkeypatch, configured):
    doc = tmp_path / "report.md"
    (tmp_path / "data.csv").write_text("new")
    write_doc(doc, "[data](data.csv)")
    client = FakeClient(gets=[{"body": "remote"}])
    monkeypatch.setattr(sync.state, "load", lambda *args: saved_state())
    monkeypatch.setattr(sync.state, "save", lambda *args: pytest.fail("state changed"))

    sync.push(doc, client, {}, dry_run=True)

    assert client.calls == ["me", "get", "uploads"]


def test_dry_run_large_upload_skips_confirmation_and_prints_preview(
    tmp_path, monkeypatch, configured, capsys
):
    doc = tmp_path / "report.md"
    attachment = tmp_path / "large.bin"
    with attachment.open("wb") as stream:
        stream.truncate(sync.LARGE_UPLOAD_BYTES + 1)
    write_doc(doc, "[large](large.bin)")
    client = FakeClient(gets=[{"body": "remote"}])
    monkeypatch.setattr(sync.state, "load", lambda *args: saved_state())

    sync.push(doc, client, {}, dry_run=True)

    assert "large.bin -> UPLOAD_PENDING:large.bin" in capsys.readouterr().out
    assert client.calls == ["me", "get", "uploads"]


def test_dry_run_prints_each_original_reference_path(
    tmp_path, monkeypatch, configured, capsys
):
    doc = tmp_path / "report.md"
    assets = tmp_path / "assets"
    assets.mkdir()
    (assets / "figure.png").write_bytes(b"image")
    write_doc(
        doc,
        "![one](assets/figure.png) ![two](./assets/figure.png)",
    )
    upload = {
        "id": 3,
        "long_name": "aa/existing",
        "real_name": "figure.png",
        "storage": 1,
        "filesize": 5,
    }
    client = FakeClient(gets=[{"body": "remote"}], uploads=[upload])
    monkeypatch.setattr(sync.state, "load", lambda *args: saved_state())

    sync.push(doc, client, {}, dry_run=True)

    output = capsys.readouterr().out
    url = sync.download_url("https://e.example", upload)
    assert f"assets/figure.png -> {url}" in output
    assert f"./assets/figure.png -> {url}" in output


def test_dry_run_remote_conflict_writes_no_merge_inputs(
    tmp_path, monkeypatch, configured
):
    doc = tmp_path / "report.md"
    write_doc(doc, "local")
    upload = {
        "id": 3,
        "long_name": "aa/web",
        "real_name": "web.png",
        "storage": 1,
    }
    url = sync.download_url("https://e.example", upload)
    client = FakeClient(gets=[{"body": f"web edit [image]({url})"}], uploads=[upload])
    monkeypatch.setattr(sync.state, "load", lambda *args: saved_state())

    with pytest.raises(RuntimeError, match="remote changed"):
        sync.push(doc, client, {}, dry_run=True)

    assert client.calls == ["me", "get"]
    assert not (tmp_path / "web.png").exists()
    assert not (tmp_path / "report.remote.md").exists()
    assert not (tmp_path / "report.base.md").exists()


def test_new_push_persists_resolved_target(tmp_path, monkeypatch):
    doc = tmp_path / "report.md"
    doc.write_text(frontmatter.render({"title": "Test"}, "local"), encoding="utf-8")
    client = FakeClient(gets=[{"body": "local", "content_type": 2}])
    monkeypatch.setattr(
        sync.config_module,
        "resolve",
        lambda *args: ("lab", "https://e.example", "secret", True),
    )
    monkeypatch.setattr(sync.state, "save", lambda *args: None)

    sync.push(doc, client, {"entity": "items"})

    meta, _ = frontmatter.parse(doc.read_text(encoding="utf-8"))
    assert meta["elab_id"] == 42
    assert meta["entity"] == "items"
    assert meta["profile"] == "lab"


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


@pytest.mark.parametrize("title", [datetime.date(2026, 8, 8), 20260808])
def test_push_normalizes_yaml_scalar_metadata_to_strings(
    tmp_path, monkeypatch, configured, title
):
    doc = tmp_path / "report.md"
    write_doc(doc, "local", title=title, tags=[123, datetime.date(2026, 8, 8)])
    client = FakeClient(
        gets=[
            {"body": "remote", "tags": ""},
            {"body": "remote"},
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


def test_existing_tags_are_not_posted_again(tmp_path, monkeypatch, configured):
    doc = tmp_path / "report.md"
    write_doc(doc, "local", tags=["PCR", "new"])
    client = FakeClient(
        gets=[
            {"body": "remote", "tags": "PCR, existing"},
            {"body": "remote"},
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
            {"body": "remote"},
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
            {"remote_base": "stored", "local_base": "local", "team": 7},
        )
    ]


def test_category_name_is_resolved(tmp_path, monkeypatch, configured):
    doc = tmp_path / "report.md"
    write_doc(doc, "local", category="Synthesis")
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

    assert client.saved_payload is not None
    assert client.saved_payload["category"] == 9


def test_content_type_must_be_exactly_markdown(tmp_path, monkeypatch, configured):
    doc = tmp_path / "report.md"
    write_doc(doc, "local")
    client = FakeClient(
        gets=[{"body": "remote"}, {"body": "remote"}, {"body": "stored"}]
    )
    monkeypatch.setattr(sync.state, "load", lambda *args: saved_state())
    monkeypatch.setattr(sync.state, "save", lambda *args: pytest.fail("state changed"))

    with pytest.raises(RuntimeError, match="not in markdown mode"):
        sync.push(doc, client, {})


def test_large_upload_non_tty_stops_before_mutation(tmp_path, monkeypatch, configured):
    doc = tmp_path / "report.md"
    attachment = tmp_path / "large.bin"
    with attachment.open("wb") as stream:
        stream.truncate(sync.LARGE_UPLOAD_BYTES + 1)
    write_doc(doc, "[large](large.bin)")
    client = FakeClient(gets=[{"body": "remote"}])
    monkeypatch.setattr(sync.state, "load", lambda *args: saved_state())

    with pytest.raises(RuntimeError, match="interactive confirmation"):
        sync.push(doc, client, {})

    assert client.calls == ["me", "get", "uploads"]


def test_pull_clean_updates_document(tmp_path, monkeypatch, configured, capsys):
    doc = tmp_path / "report.md"
    write_doc(doc, "local")
    client = FakeClient(
        gets=[
            {
                "body": "remote",
                "sharelink": "https://e.example/experiments/1",
            }
        ]
    )
    monkeypatch.setattr(sync.state, "load", lambda *args: saved_state())
    monkeypatch.setattr(sync.state, "save", lambda *args: None)

    sync.pull(doc, client, {})

    assert frontmatter.parse(doc.read_text())[1] == "remote"
    assert not (tmp_path / "report.remote.md").exists()
    assert capsys.readouterr().out == (
        "pulled experiments/1: Test\n"
        "  wrote report.md\n"
        "  → https://e.example/experiments/1\n"
    )


def test_pull_rejects_same_basename_uploads_with_different_content(
    tmp_path, monkeypatch, configured
):
    doc = tmp_path / "report.md"
    write_doc(doc, "local")
    uploads = [
        {
            "id": 3,
            "long_name": "aa/first",
            "real_name": "nested/figure.png",
            "storage": 1,
            "hash": hashlib.sha256(b"first").hexdigest(),
            "hash_algorithm": "sha256",
        },
        {
            "id": 4,
            "long_name": "aa/second",
            "real_name": "figure.png",
            "storage": 1,
            "hash": hashlib.sha256(b"second").hexdigest(),
            "hash_algorithm": "sha256",
        },
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
    assert frontmatter.parse(doc.read_text())[1] == "local"
    assert not (tmp_path / "figure.png").exists()


def test_pull_coalesces_same_basename_uploads_with_identical_content(
    tmp_path, monkeypatch, configured
):
    doc = tmp_path / "report.md"
    write_doc(doc, "local")
    digest = hashlib.sha256(b"data").hexdigest()
    uploads = [
        {
            "id": upload_id,
            "long_name": f"aa/{upload_id}",
            "real_name": "nested/Figure.png" if upload_id == 3 else "figure.png",
            "storage": 1,
            "hash": digest,
            "hash_algorithm": "sha256",
        }
        for upload_id in (3, 4)
    ]
    urls = [sync.download_url("https://e.example", upload) for upload in uploads]
    client = FakeClient(
        gets=[{"body": f"[first]({urls[0]}) [second]({urls[1]})"}],
        uploads=uploads,
    )
    monkeypatch.setattr(sync.state, "load", lambda *args: saved_state())
    monkeypatch.setattr(sync.state, "save", lambda *args: None)

    sync.pull(doc, client, {})

    assert client.calls.count("download") == 1
    assert (tmp_path / "Figure.png").read_bytes() == b"data"
    assert frontmatter.parse(doc.read_text())[1] == (
        "[first](Figure.png) [second](Figure.png)"
    )


def test_pull_compares_same_basename_upload_content_when_hashes_are_missing(
    tmp_path, monkeypatch, configured
):
    doc = tmp_path / "report.md"
    write_doc(doc, "local")
    uploads = [
        {
            "id": upload_id,
            "long_name": f"aa/{upload_id}",
            "real_name": "figure.png",
            "storage": 1,
        }
        for upload_id in (3, 4)
    ]
    urls = [sync.download_url("https://e.example", upload) for upload in uploads]

    class DifferentAttachmentsClient(FakeClient):
        def download(self, entity, eid, upload_id):
            self.calls.append(f"download:{upload_id}")
            return {3: b"first", 4: b"second"}[upload_id]

    client = DifferentAttachmentsClient(
        gets=[{"body": f"[first]({urls[0]}) [second]({urls[1]})"}],
        uploads=uploads,
    )
    monkeypatch.setattr(sync.state, "load", lambda *args: saved_state())

    with pytest.raises(RuntimeError, match="basename collision.*figure.png"):
        sync.pull(doc, client, {})

    assert "download:3" in client.calls
    assert "download:4" in client.calls
    assert frontmatter.parse(doc.read_text())[1] == "local"
    assert not (tmp_path / "figure.png").exists()


def test_pull_stops_before_entity_read_when_team_changed(
    tmp_path, monkeypatch, configured
):
    doc = tmp_path / "report.md"
    write_doc(doc, "local")

    class OtherTeamClient(FakeClient):
        def me(self):
            self.calls.append("me")
            return {"team": 8}

    client = OtherTeamClient()
    monkeypatch.setattr(sync.state, "load", lambda *args: saved_state())

    with pytest.raises(RuntimeError, match="active team differs"):
        sync.pull(doc, client, {})

    assert client.calls == ["me"]


def test_pull_dirty_writes_three_way_sidecars(
    tmp_path, monkeypatch, configured, capsys
):
    doc = tmp_path / "report.md"
    write_doc(doc, "edited")
    original = doc.read_text()
    upload = {
        "id": 3,
        "long_name": "aa/existing",
        "real_name": "figure.png",
        "storage": 1,
    }
    url = sync.download_url("https://e.example", upload)
    client = FakeClient(gets=[{"body": f"remote [figure]({url})"}], uploads=[upload])
    monkeypatch.setattr(
        sync.state,
        "load",
        lambda *args: saved_state(remote=f"base [figure]({url})"),
    )

    with pytest.raises(RuntimeError, match="local changes conflict"):
        sync.pull(doc, client, {})

    assert doc.read_text() == original
    assert (tmp_path / "figure.png").read_bytes() == b"data"
    remote_meta, remote_body = frontmatter.parse(
        (tmp_path / "report.remote.md").read_text()
    )
    base_meta, base_body = frontmatter.parse((tmp_path / "report.base.md").read_text())
    local_meta, _ = frontmatter.parse(original)
    assert remote_meta == base_meta == local_meta
    assert remote_body == "remote [figure](figure.png)"
    assert base_body == "base [figure](figure.png)"
    assert (
        shlex.join(
            [
                "git",
                "merge-file",
                str(doc),
                str(doc.with_name("report.base.md")),
                str(doc.with_name("report.remote.md")),
            ]
        )
        in capsys.readouterr().err
    )


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
        "git merge-file 'notes/my report.md' 'notes/my report.base.md' "
        "'notes/my report.remote.md'"
    ) in capsys.readouterr().err


@pytest.mark.parametrize(
    "real_name",
    [".elab.toml", ".ELAB.TOML", ".elabignore", "nested/.ELABIGNORE"],
)
def test_pull_skips_control_file_attachment(
    tmp_path, monkeypatch, configured, capsys, real_name
):
    doc = tmp_path / "report.md"
    write_doc(doc, "local")
    upload = {
        "id": 3,
        "long_name": "aa/existing",
        "real_name": real_name,
        "storage": 1,
    }
    url = sync.download_url("https://e.example", upload)
    client = FakeClient(gets=[{"body": f"[file]({url})"}], uploads=[upload])
    monkeypatch.setattr(sync.state, "load", lambda *args: saved_state())
    monkeypatch.setattr(sync.state, "save", lambda *args: None)

    sync.pull(doc, client, {})

    assert not (tmp_path / sync.safe_name(real_name)).exists()
    assert frontmatter.parse(doc.read_text())[1] == f"[file]({url})"
    assert "download" not in client.calls
    assert (
        f"refusing to place control file attachment: {sync.safe_name(real_name)}"
        in (capsys.readouterr().err)
    )


def test_pull_dirty_remote_unchanged_is_noop(tmp_path, monkeypatch, configured, capsys):
    doc = tmp_path / "report.md"
    write_doc(doc, "edited")
    original = doc.read_bytes()
    client = FakeClient(gets=[{"body": "remote"}])
    monkeypatch.setattr(sync.state, "load", lambda *args: saved_state())
    monkeypatch.setattr(
        sync.state,
        "save",
        lambda *args: pytest.fail("state should not be saved"),
    )

    sync.pull(doc, client, {})

    assert doc.read_bytes() == original
    assert client.calls == ["me", "get"]
    assert not (tmp_path / "report.remote.md").exists()
    assert not (tmp_path / "report.base.md").exists()
    assert capsys.readouterr().out == "remote: unchanged\n"


def test_pull_dirty_control_file_urls_remain_in_three_way_sidecars(
    tmp_path, monkeypatch, configured
):
    doc = tmp_path / "report.md"
    write_doc(doc, "edited")
    upload = {
        "id": 3,
        "long_name": "aa/existing",
        "real_name": ".elab.toml",
        "storage": 1,
    }
    url = sync.download_url("https://e.example", upload)
    client = FakeClient(gets=[{"body": f"remote [file]({url})"}], uploads=[upload])
    monkeypatch.setattr(
        sync.state,
        "load",
        lambda *args: saved_state(remote=f"base [file]({url})"),
    )

    with pytest.raises(RuntimeError, match="local changes conflict"):
        sync.pull(doc, client, {})

    assert frontmatter.parse((tmp_path / "report.remote.md").read_text())[1] == (
        f"remote [file]({url})"
    )
    assert frontmatter.parse((tmp_path / "report.base.md").read_text())[1] == (
        f"base [file]({url})"
    )
    assert "download" not in client.calls
    assert not (tmp_path / ".elab.toml").exists()


def test_pull_without_base_keeps_control_file_url_in_sidecar(
    tmp_path, monkeypatch, configured
):
    doc = tmp_path / "report.md"
    write_doc(doc, "local")
    upload = {
        "id": 3,
        "long_name": "aa/existing",
        "real_name": ".elabignore",
        "storage": 1,
    }
    url = sync.download_url("https://e.example", upload)
    client = FakeClient(gets=[{"body": f"remote [file]({url})"}], uploads=[upload])
    monkeypatch.setattr(sync.state, "load", lambda *args: None)

    with pytest.raises(RuntimeError, match="base unavailable"):
        sync.pull(doc, client, {})

    assert (tmp_path / "report.remote.md").read_text() == f"remote [file]({url})"
    assert "download" not in client.calls
    assert not (tmp_path / ".elabignore").exists()


def test_push_rejects_invalid_server_id_before_entity_requests(
    tmp_path, monkeypatch, configured
):
    class InvalidIdClient(FakeClient):
        def create(self, entity, title):
            self.calls.append("create")
            return {"id": "../items/42"}

    doc = tmp_path / "report.md"
    doc.write_text(frontmatter.render({"title": "Test"}, "local"))
    client = InvalidIdClient()
    monkeypatch.setattr(
        sync.state,
        "save",
        lambda *args: pytest.fail("invalid id must not be saved to state"),
    )

    with pytest.raises(RuntimeError, match="server did not return a valid entity id"):
        sync.push(doc, client, {})

    meta, _ = frontmatter.parse(doc.read_text(encoding="utf-8"))
    assert "elab_id" not in meta
    assert client.calls == ["me", "create"]


def test_pull_dirty_preserves_differing_remote_attachment(
    tmp_path, monkeypatch, configured
):
    doc = tmp_path / "report.md"
    write_doc(doc, "edited")
    target = tmp_path / "figure.png"
    target.write_bytes(b"local")
    upload = {
        "id": 3,
        "long_name": "aa/existing",
        "real_name": target.name,
        "storage": 1,
    }
    url = sync.download_url("https://e.example", upload)
    client = FakeClient(gets=[{"body": f"remote [figure]({url})"}], uploads=[upload])
    monkeypatch.setattr(
        sync.state,
        "load",
        lambda *args: saved_state(remote=f"base [figure]({url})"),
    )

    with pytest.raises(RuntimeError, match="local changes conflict"):
        sync.pull(doc, client, {})

    assert target.read_bytes() == b"local"
    assert (tmp_path / "figure.png.remote").read_bytes() == b"data"


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


def test_pull_without_base_uses_two_way_sidecar(tmp_path, monkeypatch, configured):
    doc = tmp_path / "report.md"
    write_doc(doc, "local")
    original = doc.read_text()
    (tmp_path / "figure.png").write_bytes(b"data")
    upload = {
        "id": 3,
        "long_name": "aa/existing",
        "real_name": "figure.png",
        "storage": 1,
    }
    url = sync.download_url("https://e.example", upload)
    client = FakeClient(gets=[{"body": f"remote [figure]({url})"}], uploads=[upload])
    monkeypatch.setattr(sync.state, "load", lambda *args: None)

    with pytest.raises(RuntimeError, match="base unavailable"):
        sync.pull(doc, client, {})

    assert doc.read_text() == original
    assert (tmp_path / "report.remote.md").read_text() == (
        "remote [figure](figure.png)"
    )
    assert (tmp_path / "figure.png").read_bytes() == b"data"
    assert not (tmp_path / "figure.png.remote").exists()
    assert "download" in client.calls
    assert not (tmp_path / "report.base.md").exists()


def test_pull_without_base_rejects_sidecar_attachment_before_download(
    tmp_path, monkeypatch, configured
):
    doc = tmp_path / "report.md"
    write_doc(doc, "local")
    upload = {
        "id": 3,
        "long_name": "aa/existing",
        "real_name": "report.remote.md",
        "storage": 1,
    }
    url = sync.download_url("https://e.example", upload)
    client = FakeClient(gets=[{"body": f"[file]({url})"}], uploads=[upload])
    monkeypatch.setattr(sync.state, "load", lambda *args: None)

    with pytest.raises(RuntimeError, match="sidecar.*report.remote.md"):
        sync.pull(doc, client, {})

    assert client.calls == ["me", "get", "uploads"]
    assert not (tmp_path / "report.remote.md").exists()


def test_pull_without_base_reports_attachment_conflicts(
    tmp_path, monkeypatch, configured
):
    doc = tmp_path / "report.md"
    write_doc(doc, "local")
    target = tmp_path / "attachment.bin"
    target.write_bytes(b"local")
    upload = {
        "id": 3,
        "long_name": "aa/existing",
        "real_name": target.name,
        "storage": 1,
    }
    url = sync.download_url("https://e.example", upload)
    client = FakeClient(gets=[{"body": f"remote [file]({url})"}], uploads=[upload])
    monkeypatch.setattr(sync.state, "load", lambda *args: None)

    with pytest.raises(RuntimeError) as error:
        sync.pull(doc, client, {})

    message = str(error.value)
    assert "base unavailable; remote written to report.remote.md" in message
    assert "attachment conflicts written to: attachment.bin.remote" in message
    assert target.read_bytes() == b"local"
    assert (tmp_path / "attachment.bin.remote").read_bytes() == b"data"
    assert (tmp_path / "report.remote.md").read_text() == (
        "remote [file](attachment.bin)"
    )


@pytest.mark.parametrize("target_kind", ["outside", "dangling", "inside"])
def test_pull_rejects_symlink_attachment_target(
    tmp_path, monkeypatch, configured, target_kind
):
    doc = tmp_path / "report.md"
    write_doc(doc, "local")
    if target_kind == "inside":
        outside = tmp_path / "linked.bin"
    else:
        outside = tmp_path.parent / f"{tmp_path.name}-outside.bin"
    if target_kind != "dangling":
        outside.write_bytes(b"outside")
    target = tmp_path / "attachment.bin"
    target.symlink_to(outside)
    upload = {
        "id": 3,
        "long_name": "aa/existing",
        "real_name": "attachment.bin",
        "storage": 1,
    }
    url = sync.download_url("https://e.example", upload)
    client = FakeClient(gets=[{"body": f"[file]({url})"}], uploads=[upload])
    monkeypatch.setattr(sync.state, "load", lambda *args: saved_state())
    monkeypatch.setattr(
        sync.state, "save", lambda *args: pytest.fail("state must not be saved")
    )

    with pytest.raises(RuntimeError, match="unsafe attachment destination"):
        sync.pull(doc, client, {})

    if target_kind == "dangling":
        assert not outside.exists()
    else:
        assert outside.read_bytes() == b"outside"


@pytest.mark.parametrize("dangling", [False, True])
def test_pull_rejects_symlink_attachment_conflict_sidecar(
    tmp_path, monkeypatch, configured, dangling
):
    doc = tmp_path / "report.md"
    write_doc(doc, "local")
    target = tmp_path / "attachment.bin"
    target.write_bytes(b"local")
    outside = tmp_path.parent / f"{tmp_path.name}-outside.bin"
    if not dangling:
        outside.write_bytes(b"outside")
    (tmp_path / "attachment.bin.remote").symlink_to(outside)
    upload = {
        "id": 3,
        "long_name": "aa/existing",
        "real_name": "attachment.bin",
        "storage": 1,
    }
    url = sync.download_url("https://e.example", upload)
    client = FakeClient(gets=[{"body": f"[file]({url})"}], uploads=[upload])
    monkeypatch.setattr(sync.state, "load", lambda *args: saved_state())

    with pytest.raises(RuntimeError, match="unsafe attachment destination"):
        sync.pull(doc, client, {})

    assert target.read_bytes() == b"local"
    if dangling:
        assert not outside.exists()
    else:
        assert outside.read_bytes() == b"outside"


def test_pull_checks_attachment_destination_after_download(
    tmp_path, monkeypatch, configured
):
    doc = tmp_path / "report.md"
    write_doc(doc, "local")
    target = tmp_path / "attachment.bin"
    outside = tmp_path.parent / f"{tmp_path.name}-outside.bin"
    upload = {
        "id": 3,
        "long_name": "aa/existing",
        "real_name": target.name,
        "storage": 1,
    }
    url = sync.download_url("https://e.example", upload)

    class RacingClient(FakeClient):
        def download(self, entity, eid, upload_id):
            self.calls.append("download")
            target.symlink_to(outside)
            return b"data"

    client = RacingClient(gets=[{"body": f"[file]({url})"}], uploads=[upload])
    monkeypatch.setattr(sync.state, "load", lambda *args: saved_state())
    monkeypatch.setattr(
        sync.state, "save", lambda *args: pytest.fail("state must not be saved")
    )

    with pytest.raises(RuntimeError, match="unsafe attachment destination"):
        sync.pull(doc, client, {})

    assert not outside.exists()


def test_attachment_write_does_not_follow_symlink_swapped_after_validation(
    tmp_path, monkeypatch
):
    target = tmp_path / "attachment.bin"
    outside = tmp_path.parent / f"{tmp_path.name}-outside.bin"
    validate = sync._validate_attachment_target

    def swap_after_validation(path, doc_dir):
        validate(path, doc_dir)
        path.symlink_to(outside)

    monkeypatch.setattr(sync, "_validate_attachment_target", swap_after_validation)

    with pytest.raises(RuntimeError, match="unsafe attachment destination"):
        sync._write_attachment(target, b"data", tmp_path)

    assert not outside.exists()


def test_attachment_write_does_not_clobber_existing_hard_link(tmp_path):
    outside = tmp_path.parent / f"{tmp_path.name}-outside.bin"
    outside.write_bytes(b"outside")
    target = tmp_path / "attachment.bin"
    sync.os.link(outside, target)

    with pytest.raises(FileExistsError):
        sync._write_attachment(target, b"remote", tmp_path)

    assert outside.read_bytes() == b"outside"


def test_attachment_write_works_without_dir_fd_support(tmp_path, monkeypatch):
    target = tmp_path / "attachment.bin"
    monkeypatch.setattr(sync.os, "supports_dir_fd", set())

    sync._write_attachment(target, b"data", tmp_path)

    assert target.read_bytes() == b"data"


def test_pull_writes_regular_attachment_target(tmp_path, monkeypatch, configured):
    doc = tmp_path / "report.md"
    write_doc(doc, "local")
    upload = {
        "id": 3,
        "long_name": "aa/existing",
        "real_name": "attachment.bin",
        "storage": 1,
    }
    url = sync.download_url("https://e.example", upload)
    client = FakeClient(gets=[{"body": f"[file]({url})"}], uploads=[upload])
    monkeypatch.setattr(sync.state, "load", lambda *args: saved_state())
    monkeypatch.setattr(sync.state, "save", lambda *args: None)

    sync.pull(doc, client, {})

    assert (tmp_path / "attachment.bin").read_bytes() == b"data"


def test_pull_regular_attachment_conflict_writes_remote(
    tmp_path, monkeypatch, configured
):
    doc = tmp_path / "report.md"
    write_doc(doc, "local")
    target = tmp_path / "attachment.bin"
    target.write_bytes(b"local")
    upload = {
        "id": 3,
        "long_name": "aa/existing",
        "real_name": target.name,
        "storage": 1,
    }
    url = sync.download_url("https://e.example", upload)
    client = FakeClient(gets=[{"body": f"[file]({url})"}], uploads=[upload])
    monkeypatch.setattr(sync.state, "load", lambda *args: saved_state())

    with pytest.raises(RuntimeError, match="attachment conflicts written"):
        sync.pull(doc, client, {})

    assert target.read_bytes() == b"local"
    assert (tmp_path / "attachment.bin.remote").read_bytes() == b"data"


def test_pull_rejects_remote_conflict_name_collision_before_download(
    tmp_path, monkeypatch, configured
):
    doc = tmp_path / "report.md"
    write_doc(doc, "local")
    (tmp_path / "foo").write_bytes(b"local foo")
    (tmp_path / "foo.remote").write_bytes(b"local remote")
    foo = {
        "id": 3,
        "long_name": "aa/foo",
        "real_name": "foo",
        "storage": 1,
    }
    foo_remote = {
        "id": 4,
        "long_name": "aa/foo-remote",
        "real_name": "FOO.REMOTE",
        "storage": 1,
    }
    foo_url = sync.download_url("https://e.example", foo)
    foo_remote_url = sync.download_url("https://e.example", foo_remote)
    client = FakeClient(
        gets=[{"body": f"[foo]({foo_url}) [remote]({foo_remote_url})"}],
        uploads=[foo, foo_remote],
    )
    monkeypatch.setattr(sync.state, "load", lambda *args: saved_state())
    before = {item.name: item.read_bytes() for item in tmp_path.iterdir()}

    with pytest.raises(RuntimeError, match=r"attachment.*foo\.remote.*collision"):
        sync.pull(doc, client, {})

    assert "download" not in client.calls
    assert {item.name: item.read_bytes() for item in tmp_path.iterdir()} == before


def test_pull_rejects_base_conflict_name_collision_before_download(
    tmp_path, monkeypatch, configured
):
    doc = tmp_path / "report.md"
    write_doc(doc, "edited")
    (tmp_path / "foo").write_bytes(b"local foo")
    (tmp_path / "foo.base").write_bytes(b"local base")
    remote_upload = {
        "id": 3,
        "long_name": "aa/foo-base",
        "real_name": "FOO.BASE",
        "storage": 1,
    }
    base_upload = {
        "id": 4,
        "long_name": "aa/foo",
        "real_name": "foo",
        "storage": 1,
    }
    remote_url = sync.download_url("https://e.example", remote_upload)
    base_url = sync.download_url("https://e.example", base_upload)
    client = FakeClient(
        gets=[{"body": f"remote [file]({remote_url})"}],
        uploads=[remote_upload, base_upload],
    )
    monkeypatch.setattr(
        sync.state,
        "load",
        lambda *args: saved_state(remote=f"base [file]({base_url})"),
    )
    before = {item.name: item.read_bytes() for item in tmp_path.iterdir()}

    with pytest.raises(RuntimeError, match=r"attachment.*foo\.base.*collision"):
        sync.pull(doc, client, {})

    assert "download" not in client.calls
    assert {item.name: item.read_bytes() for item in tmp_path.iterdir()} == before


def test_pull_refreshes_existing_regular_attachment_remote(
    tmp_path, monkeypatch, configured
):
    doc = tmp_path / "report.md"
    write_doc(doc, "local")
    (tmp_path / "attachment.bin").write_bytes(b"local")
    remote_target = tmp_path / "attachment.bin.remote"
    remote_target.write_bytes(b"stale")
    upload = {
        "id": 3,
        "long_name": "aa/existing",
        "real_name": "attachment.bin",
        "storage": 1,
    }
    url = sync.download_url("https://e.example", upload)
    client = FakeClient(gets=[{"body": f"[file]({url})"}], uploads=[upload])
    monkeypatch.setattr(sync.state, "load", lambda *args: saved_state())

    with pytest.raises(RuntimeError, match="attachment conflicts written"):
        sync.pull(doc, client, {})

    assert remote_target.read_bytes() == b"data"


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
        'local: dirty (use "elab push")',
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


def test_status_suggests_actions_for_local_and_remote_changes(
    tmp_path, monkeypatch, configured, capsys
):
    doc = tmp_path / "report.md"
    write_doc(doc, "local change")
    client = FakeClient(gets=[{"body": "remote change", "content_type": 2}])
    monkeypatch.setattr(sync.state, "load", lambda *args: saved_state())

    sync.status(doc, client, {})

    lines = capsys.readouterr().out.splitlines()
    assert 'local: dirty (use "elab push")' in lines
    assert 'remote: changed (use "elab pull")' in lines


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
    assert "server normalization noise" in captured.err


def test_remote_tags_handles_null():
    assert sync._remote_tags({"tags": None}) == set()
