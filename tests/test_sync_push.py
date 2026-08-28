from __future__ import annotations

import datetime
import shlex

import pytest
from _sync_harness import FakeClient, saved_state, write_doc

from elport import frontmatter, sync


@pytest.mark.parametrize(
    "body",
    [
        "<<<<<<< report.md\nlocal\n",
        ">>>>>>> report.remote.md\nremote\n",
        "<<<<<<< report.md\nlocal\n=======\nremote\n>>>>>>> report.remote.md\n",
    ],
)
def test_push_rejects_unresolved_merge_markers_before_network(
    tmp_path, configured, body
):
    doc = tmp_path / "report.md"
    write_doc(doc, body)
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


def test_push_rejects_empty_body_unless_forced(tmp_path, monkeypatch, configured):
    doc = tmp_path / "report.md"
    write_doc(doc, "   \n")
    client = FakeClient()

    with pytest.raises(
        RuntimeError,
        match="refusing to push an empty body; use --force to overwrite",
    ):
        sync.push(doc, client, {})

    assert client.calls == []

    dry_run_client = FakeClient()
    with pytest.raises(
        RuntimeError,
        match="refusing to push an empty body; use --force to overwrite",
    ):
        sync.push(doc, dry_run_client, {}, dry_run=True)

    assert dry_run_client.calls == []

    forced_client = FakeClient(
        gets=[
            {"body": "remote"},
            {"body": "", "content_type": 2},
        ]
    )
    monkeypatch.setattr(sync.state, "load", lambda *args: saved_state())
    monkeypatch.setattr(sync.state, "save", lambda *args: None)

    sync.push(doc, forced_client, {}, force=True)

    assert forced_client.calls.count("patch") == 1


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
    stored_state = saved_state()
    monkeypatch.setattr(sync.state, "load", lambda *args: stored_state)
    monkeypatch.setattr(sync.state, "save", lambda *args: stored_state.update(args[-1]))

    with pytest.raises(RuntimeError, match="after uploads"):
        sync.push(doc, client, {})

    assert "upload" in client.calls
    assert "patch" not in client.calls
    assert stored_state["remote_base"] == "remote"
    assert stored_state["pending_remote"] == "web edit"


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
                "--",
                str(doc),
                str(doc.with_name("report.base.md")),
                str(doc.with_name("report.remote.md")),
            ]
        )
        in capsys.readouterr().err
    )


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
                "--",
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


def test_dry_run_has_no_mutating_calls(tmp_path, monkeypatch, configured):
    doc = tmp_path / "report.md"
    (tmp_path / "data.csv").write_text("new")
    write_doc(doc, "[data](data.csv)")
    client = FakeClient(gets=[{"body": "remote"}])
    monkeypatch.setattr(sync.state, "load", lambda *args: saved_state())
    monkeypatch.setattr(sync.state, "save", lambda *args: pytest.fail("state changed"))

    sync.push(doc, client, {}, dry_run=True)

    assert client.calls == ["me", "get", "uploads"]


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


def test_push_does_not_upload_file_named_only_inside_attribute_value(
    tmp_path, monkeypatch, configured, capsys
):
    doc = tmp_path / "report.md"
    figure = tmp_path / "figure.png"
    figure.write_bytes(b"image")
    secret = tmp_path / "secret.md"
    secret.write_text("local secret", encoding="utf-8")
    write_doc(doc, '<img alt=\'literal src="secret.md"\' src="figure.png">')
    client = FakeClient(gets=[{"body": "remote"}])
    monkeypatch.setattr(sync.state, "load", lambda *args: saved_state())

    sync.push(doc, client, {}, dry_run=True)

    output = capsys.readouterr().out
    assert "figure.png -> UPLOAD_PENDING:figure.png" in output
    assert "secret.md" not in output


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
    assert meta["id"] == 42
    assert meta["entity"] == "items"
    assert meta["profile"] == "lab"


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


def test_pipe_separated_existing_tags_are_not_posted_again(
    tmp_path, monkeypatch, configured
):
    doc = tmp_path / "report.md"
    write_doc(doc, "local", tags=["PCR", "new"])
    client = FakeClient(
        gets=[
            {"body": "remote", "tags": "PCR|existing"},
            {"body": "remote"},
            {"body": "stored", "content_type": 2},
        ]
    )
    monkeypatch.setattr(sync.state, "load", lambda *args: saved_state())
    monkeypatch.setattr(sync.state, "save", lambda *args: None)

    sync.push(doc, client, {})

    assert "tag:PCR" not in client.calls
    assert client.calls.count("tag:new") == 1


@pytest.mark.parametrize(
    ("remote_body", "dry_run", "expect_warning"),
    [("web edit", False, True), ("remote", False, False), ("web edit", True, False)],
)
def test_force_push_warns_only_when_discarding_remote_change(
    tmp_path,
    monkeypatch,
    configured,
    capsys,
    remote_body,
    dry_run,
    expect_warning,
):
    doc = tmp_path / "report.md"
    write_doc(doc, "local")
    client = FakeClient(
        gets=[
            {"body": remote_body},
            {"body": "stored", "content_type": 2},
        ]
    )
    monkeypatch.setattr(sync.state, "load", lambda *args: saved_state())
    monkeypatch.setattr(sync.state, "save", lambda *args: None)

    sync.push(doc, client, {}, force=True, dry_run=dry_run)

    warning = "warning: --force is discarding a remote change"
    assert (warning in capsys.readouterr().err) is expect_warning


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


def test_category_valid_numeric_id_is_validated_and_sent(
    tmp_path, monkeypatch, configured
):
    doc = tmp_path / "report.md"
    write_doc(doc, "local", category=9)
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

    assert "categories" in client.calls
    assert client.saved_payload is not None
    assert client.saved_payload["category"] == 9


def test_category_unknown_numeric_id_is_rejected_before_mutation(tmp_path, configured):
    doc = tmp_path / "report.md"
    write_doc(doc, "local", category=99)
    client = FakeClient()

    with pytest.raises(RuntimeError, match="category not found: 99"):
        sync.push(doc, client, {})

    assert client.calls == ["me", "categories"]


@pytest.mark.parametrize("status", ["Running", 5])
def test_status_name_or_valid_numeric_id_is_resolved_and_sent(
    tmp_path, monkeypatch, configured, status
):
    doc = tmp_path / "report.md"
    write_doc(doc, "local", status=status)
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

    assert client.calls == ["me", "statuses"]


def test_push_without_status_does_not_send_status(tmp_path, monkeypatch, configured):
    doc = tmp_path / "report.md"
    write_doc(doc, "local")
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
    assert client.calls == ["me", "statuses", "get", "uploads"]


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
    assert "id" not in meta
    assert client.calls == ["me", "create"]


def test_successful_push_preserves_unrelated_sidecar_named_documents(
    tmp_path, monkeypatch, configured
):
    doc = tmp_path / "report.md"
    write_doc(doc, "local\n")
    base_sidecar = tmp_path / "report.base.md"
    remote_sidecar = tmp_path / "report.remote.md"
    base_sidecar.write_text("unrelated base document\n", encoding="utf-8")
    remote_sidecar.write_text("unrelated remote document\n", encoding="utf-8")
    monkeypatch.setattr(
        sync.state, "load", lambda *args: saved_state(local="old\n", remote="R\n")
    )
    monkeypatch.setattr(sync.state, "save", lambda *args: None)
    client = FakeClient(
        gets=[
            {"body": "R\n"},
            {"body": "R\n"},
            {"body": "R\n", "content_type": 2},
        ]
    )

    sync.push(doc, client, {})

    assert client.saved_payload is not None
    assert base_sidecar.read_text(encoding="utf-8") == "unrelated base document\n"
    assert remote_sidecar.read_text(encoding="utf-8") == "unrelated remote document\n"
