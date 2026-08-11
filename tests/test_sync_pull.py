from __future__ import annotations

import hashlib
import shlex

import pytest
from _sync_harness import FakeClient, saved_state, write_doc

from elab import frontmatter, sync


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
                "--",
                str(doc),
                str(doc.with_name("report.base.md")),
                str(doc.with_name("report.remote.md")),
            ]
        )
        in capsys.readouterr().err
    )


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
    assert capsys.readouterr().out == (
        "remote: unchanged (local differs from the last sync; nothing to pull)\n"
    )


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
