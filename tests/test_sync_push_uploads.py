from __future__ import annotations

import hashlib

import pytest
from _sync_harness import (
    FakeClient,
    Store,
    saved_state,
    write_doc,
)

from elport import sync


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


def test_push_does_not_upload_file_named_only_in_non_reference_text(
    tmp_path, monkeypatch, configured, capsys
):
    doc = tmp_path / "report.md"
    secret = tmp_path / "secret.txt"
    secret.write_text("local secret", encoding="utf-8")
    real = tmp_path / "real.txt"
    real.write_text("attachment", encoding="utf-8")
    write_doc(
        doc,
        '[public](https://example.test "caption [hidden](secret.txt)") '
        "<textarea>[hidden](secret.txt)</textarea> [real](real.txt)",
    )
    client = FakeClient(gets=[{"body": "remote"}])
    monkeypatch.setattr(sync.state, "load", lambda *args: saved_state())

    sync.push(doc, client, {}, dry_run=True)

    output = capsys.readouterr().out
    assert "real.txt -> UPLOAD_PENDING:real.txt" in output
    assert "secret.txt" not in output


def test_push_without_referenced_files_skips_upload_listing(
    tmp_path, monkeypatch, configured
):
    doc = tmp_path / "report.md"
    write_doc(doc, "local")
    client = FakeClient(
        gets=[
            {"body": "remote"},
            {"body": "stored", "content_type": 2},
        ]
    )
    monkeypatch.setattr(sync.state, "load", lambda *args: saved_state())
    monkeypatch.setattr(sync.state, "save", lambda *args: client.calls.append("save"))

    sync.push(doc, client, {})

    assert client.calls == ["me", "get", "patch", "save"]


FOREIGN_URL = (
    "https://e.example/app/download.php?f=bb%2Ftheirs&name=theirs.png&storage=1"
)


def test_push_stays_silent_on_unmatched_attachment_reference(
    tmp_path, monkeypatch, configured, capsys
):
    doc = tmp_path / "report.md"
    write_doc(doc, f"see [x]({FOREIGN_URL})")
    client = FakeClient(remote_doc={"body": "remote", "content_type": 2})
    monkeypatch.setattr(sync.state, "load", lambda *args: saved_state())

    sync.push(doc, client, {}, dry_run=True)

    assert "kept as a URL" not in capsys.readouterr().err


def test_push_names_new_uploads(tmp_path, monkeypatch, configured, capsys):
    doc = tmp_path / "report.md"
    (tmp_path / "plot.png").write_bytes(b"png")
    write_doc(doc, "![p](plot.png)")
    client = FakeClient(remote_doc={"body": "remote", "content_type": 2})
    Store(monkeypatch, {"1": saved_state()})

    sync.push(doc, client, {})

    assert "uploads: 0 reused, 1 new (plot.png)" in capsys.readouterr().out


def test_push_uploads_a_file_outside_the_document_directory(
    tmp_path, monkeypatch, configured
):
    (tmp_path / "shared").mkdir()
    (tmp_path / "shared" / "raw.csv").write_bytes(b"raw")
    doc = tmp_path / "notes" / "report.md"
    doc.parent.mkdir()
    (doc.parent / "assets").mkdir()
    (doc.parent / "assets" / "fig.png").write_bytes(b"png")
    body = '![f](assets/fig.png) [r](../shared/raw.csv) <img src="assets/fig.png">'
    write_doc(doc, body)
    store = Store(monkeypatch, {"1": saved_state()})
    client = FakeClient(
        gets=[{"body": "remote"}, {"body": "remote"}, {"body": "s", "content_type": 2}]
    )

    sync.push(doc, client, {})

    assert client.calls.count("upload") == 2
    sent = client.saved_payload["body"]
    assert "../shared/raw.csv" not in sent and "assets/fig.png" not in sent
    # pull puts each one back as written (ADR-0007).
    links = store.data["1"]["doc_links"]
    assert sorted(
        (family, written)
        for entry in links.values()
        for family, written in entry.items()
    ) == [
        ("html", "assets/fig.png"),
        ("markdown", "../shared/raw.csv"),
        ("markdown", "assets/fig.png"),
    ]
    assert sync.relink(sent, links) == body


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


def test_large_upload_non_tty_stops_before_mutation(tmp_path, monkeypatch, configured):
    doc = tmp_path / "report.md"
    attachment = tmp_path / "large.bin"
    with attachment.open("wb") as stream:
        stream.truncate(sync.LARGE_UPLOAD_BYTES + 1)
    write_doc(doc, "[large](large.bin)")
    client = FakeClient(gets=[{"body": "remote"}])
    monkeypatch.setattr(sync.state, "load", lambda *args: saved_state())

    with pytest.raises(
        RuntimeError, match="re-run with --yes only if that is intended"
    ):
        sync.push(doc, client, {})

    assert client.calls == ["me", "get", "uploads"]


def test_large_upload_non_tty_proceeds_with_yes(tmp_path, monkeypatch, configured):
    doc = tmp_path / "report.md"
    attachment = tmp_path / "large.bin"
    with attachment.open("wb") as stream:
        stream.truncate(sync.LARGE_UPLOAD_BYTES + 1)
    write_doc(doc, "[large](large.bin)")
    client = FakeClient(
        gets=[
            {"body": "remote", "tags": ""},
            {"body": "remote"},
            {"body": "stored", "content_type": 2},
        ]
    )
    monkeypatch.setattr(sync.state, "load", lambda *args: saved_state())
    monkeypatch.setattr(sync.state, "save", lambda *args: None)
    monkeypatch.setattr(sync.sys.stdin, "isatty", lambda: False)

    sync.push(doc, client, {}, assume_yes=True)

    assert "upload" in client.calls


class _IdOnlyUploadClient(FakeClient):
    """Like eLabFTW: the upload POST answers with an id only."""

    def __init__(self, listed=True, **kwargs):
        super().__init__(**kwargs)
        self.listed = listed

    def upload(self, entity, eid, path):
        self.calls.append("upload")
        uid = 100 + len(self.upload_list)
        if self.listed:
            self.upload_list.append(
                {
                    "id": uid,
                    "long_name": f"aa/{uid}",
                    "real_name": path.name,
                    "storage": 1,
                }
            )
        return {"id": uid}


def test_id_only_upload_responses_are_completed_with_one_listing(
    tmp_path, monkeypatch, configured
):
    doc = tmp_path / "report.md"
    for name in ("a.csv", "b.csv", "c.csv"):
        (tmp_path / name).write_text(name)
    write_doc(doc, "[a](a.csv) [b](b.csv) [c](c.csv)")
    client = _IdOnlyUploadClient(
        gets=[
            {"body": "remote"},
            {"body": "remote"},
            {"body": "stored", "content_type": 2},
        ]
    )
    monkeypatch.setattr(sync.state, "load", lambda *args: saved_state())
    monkeypatch.setattr(sync.state, "save", lambda *args: None)

    sync.push(doc, client, {})

    assert client.calls == [
        "me",
        "get",
        "uploads",
        "upload",
        "upload",
        "upload",
        "uploads",
        "get",
        "patch",
    ]
    urls = [sync.download_url("https://e.example", u) for u in client.upload_list]
    assert client.saved_payload is not None
    assert client.saved_payload["body"] == "[a]({}) [b]({}) [c]({})".format(*urls)


def test_upload_missing_from_the_listing_stops_before_the_body_patch(
    tmp_path, monkeypatch, configured
):
    doc = tmp_path / "report.md"
    (tmp_path / "a.csv").write_text("a")
    write_doc(doc, "[a](a.csv)")
    client = _IdOnlyUploadClient(listed=False, gets=[{"body": "remote"}])
    monkeypatch.setattr(sync.state, "load", lambda *args: saved_state())

    with pytest.raises(RuntimeError, match="metadata could not be retrieved"):
        sync.push(doc, client, {})

    assert "patch" not in client.calls


def test_push_without_attachments_skips_the_recheck(tmp_path, monkeypatch, configured):
    # Nothing slow runs between the first read and the PATCH, so a second read
    # would only cost a round trip.
    doc = tmp_path / "report.md"
    write_doc(doc, "edited")
    client = FakeClient(
        gets=[{"body": "remote"}, {"body": "stored", "content_type": 2}]
    )
    monkeypatch.setattr(sync.state, "load", lambda *args: saved_state())
    monkeypatch.setattr(sync.state, "save", lambda *args: client.calls.append("save"))

    sync.push(doc, client, {})

    assert client.calls == ["me", "get", "patch", "save"]


def test_push_rechecks_after_listing_and_hashing_reused_attachments(
    tmp_path, monkeypatch, configured
):
    # Hashing a large local file leaves time for a Web edit before the PATCH.
    doc = tmp_path / "report.md"
    (tmp_path / "data.csv").write_bytes(b"same")
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
        gets=[{"body": "remote"}, {"body": "web edit"}], uploads=[upload]
    )
    Store(monkeypatch, {"1": saved_state()})

    with pytest.raises(RuntimeError, match="remote changed"):
        sync.push(doc, client, {})

    assert "patch" not in client.calls


def test_unchanged_push_patches_when_a_reused_upload_replaced_the_linked_one(
    tmp_path, monkeypatch, configured
):
    # The Web side deleted the attachment and uploaded it again: the body still
    # points at the deleted one, and only a PATCH sends the new URL.
    doc = tmp_path / "report.md"
    (tmp_path / "data.csv").write_bytes(b"same")
    write_doc(doc, "[data](data.csv)")
    old = {"id": 3, "long_name": "aa/deleted", "real_name": "data.csv", "storage": 1}
    new = {
        "id": 4,
        "long_name": "bb/again",
        "real_name": "data.csv",
        "storage": 1,
        "hash": hashlib.sha256(b"same").hexdigest(),
        "hash_algorithm": "sha256",
    }
    remote_body = f"[data]({sync.download_url('https://e.example', old)})"
    remote_doc = {"body": remote_body, "title": "Test", "content_type": 2}
    client = FakeClient(remote_doc=remote_doc, uploads=[new])
    Store(
        monkeypatch,
        {"1": saved_state(local="[data](data.csv)", remote=remote_body)},
    )

    sync.push(doc, client, {})

    assert "patch" in client.calls
    assert "bb%2Fagain" in client.saved_payload["body"]


def test_push_reuses_uploads_that_share_a_name(tmp_path, monkeypatch, configured):
    # image-5.png is the name pull gave the second upload called image.png.
    doc = tmp_path / "report.md"
    (tmp_path / "image.png").write_bytes(b"second")
    (tmp_path / "image-5.png").write_bytes(b"first")
    write_doc(doc, "![a](image.png) ![b](image-5.png)")
    uploads = [
        {
            "id": upload_id,
            "long_name": f"aa/{upload_id}",
            "real_name": "image.png",
            "storage": 1,
            "hash": hashlib.sha256(content).hexdigest(),
            "hash_algorithm": "sha256",
        }
        for upload_id, content in ((3, b"old"), (5, b"first"), (7, b"second"))
    ]
    client = FakeClient(
        remote_doc={"body": "remote", "content_type": 2}, uploads=uploads
    )
    Store(monkeypatch, {"1": saved_state()})

    sync.push(doc, client, {})

    assert "upload" not in client.calls
    assert "aa%2F7" in client.saved_payload["body"]
    assert "aa%2F5" in client.saved_payload["body"]


def test_push_keeps_identical_files_with_different_names_apart(
    tmp_path, monkeypatch, configured
):
    # Matching by content alone would point both links at one upload, and the
    # next pull would rewrite both to one file.
    doc = tmp_path / "report.md"
    (tmp_path / "a.csv").write_bytes(b"same")
    (tmp_path / "b.csv").write_bytes(b"same")
    write_doc(doc, "[a](a.csv) [b](b.csv)")
    digest = hashlib.sha256(b"same").hexdigest()
    uploads = [
        {
            "id": upload_id,
            "long_name": f"aa/{upload_id}",
            "real_name": name,
            "storage": 1,
            "hash": digest,
            "hash_algorithm": "sha256",
        }
        for upload_id, name in ((3, "a.csv"), (4, "b.csv"))
    ]
    client = FakeClient(
        remote_doc={"body": "remote", "content_type": 2}, uploads=uploads
    )
    Store(monkeypatch, {"1": saved_state()})

    sync.push(doc, client, {})

    assert "upload" not in client.calls
    assert "aa%2F3" in client.saved_payload["body"]
    assert "aa%2F4" in client.saved_payload["body"]


def test_unchanged_push_with_reused_attachments_reads_the_remote_once(
    tmp_path, monkeypatch, configured
):
    # With no PATCH to come there is nothing to overwrite, so nothing to recheck.
    doc = tmp_path / "report.md"
    (tmp_path / "data.csv").write_bytes(b"same")
    write_doc(doc, "[data](data.csv)")
    upload = {
        "id": 3,
        "long_name": "aa/existing",
        "real_name": "data.csv",
        "storage": 1,
        "hash": hashlib.sha256(b"same").hexdigest(),
        "hash_algorithm": "sha256",
    }
    remote_body = f"[data]({sync.download_url('https://e.example', upload)})"
    client = FakeClient(
        remote_doc={"body": remote_body, "title": "Test", "content_type": 2},
        uploads=[upload],
    )
    url = sync.download_url("https://e.example", upload)
    Store(
        monkeypatch,
        {
            "1": {
                **saved_state(local="[data](data.csv)", remote=remote_body),
                "doc_links": {url: {"markdown": "data.csv"}},
            }
        },
    )

    sync.push(doc, client, {})

    assert client.calls == ["me", "get", "uploads"]
