from __future__ import annotations

import pytest
from _sync_harness import (
    FakeClient,
    Store,
    saved_state,
    write_doc,
)

from elport import sync


def _pulled_link_doc(tmp_path, monkeypatch, link="private.txt"):
    """A Web-side editor linked a local file; pull brought it into the note."""
    doc = tmp_path / "notes" / "report.md"
    doc.parent.mkdir()
    write_doc(doc, "local")
    (doc.parent / "private.txt").write_text("secret")
    (tmp_path / "private.txt").write_text("secret")
    pulled = f"see [x]({link})"
    Store(monkeypatch, {"1": saved_state()})
    sync.pull(doc, FakeClient(gets=[{"body": pulled}]), {})
    return doc, pulled


REMOTE_LINKED = "linked from the remote body"


@pytest.mark.parametrize("assume_yes", [False, True])
@pytest.mark.parametrize(
    "link", ["private.txt", "../private.txt", "{root}/private.txt"]
)
def test_push_never_uploads_a_file_the_remote_body_linked(
    tmp_path, monkeypatch, configured, capsys, link, assume_yes
):
    link = link.format(root=tmp_path)
    doc, pulled = _pulled_link_doc(tmp_path, monkeypatch, link)
    write_doc(doc, f"{pulled}\n\nmy edit")
    client = FakeClient(remote_doc={"body": pulled, "content_type": 2})

    sync.push(doc, client, {}, assume_yes=assume_yes)

    assert "upload" not in client.calls
    assert f"[x]({link})" in client.saved_payload["body"]
    err = capsys.readouterr().err
    assert REMOTE_LINKED in err
    assert "written on the server side" in err and "on purpose" in err


def test_push_uploads_a_remote_linked_file_listed_under_attachments(
    tmp_path, monkeypatch, configured
):
    doc, pulled = _pulled_link_doc(tmp_path, monkeypatch)
    write_doc(doc, pulled, attachments=["private.txt"])
    client = FakeClient(remote_doc={"body": pulled, "content_type": 2})

    sync.push(doc, client, {})

    assert "upload" in client.calls
    assert "download.php" in client.saved_payload["body"]


def test_status_does_not_count_a_remote_linked_file_as_an_upload(
    tmp_path, monkeypatch, configured, capsys
):
    doc, pulled = _pulled_link_doc(tmp_path, monkeypatch)
    client = FakeClient(remote_doc={"body": pulled, "content_type": 2})

    sync.status(doc, client, {})

    out, err = capsys.readouterr()
    assert "uploads new: none" in out
    assert REMOTE_LINKED in err


def test_dry_run_reports_a_remote_linked_file_as_not_uploaded(
    tmp_path, monkeypatch, configured, capsys
):
    doc, pulled = _pulled_link_doc(tmp_path, monkeypatch)
    client = FakeClient(remote_doc={"body": pulled})

    sync.push(doc, client, {}, dry_run=True)

    out, err = capsys.readouterr()
    assert REMOTE_LINKED in err
    assert "private.txt ->" not in out


def test_push_skips_a_remote_link_hidden_behind_an_escaped_comment(
    tmp_path, monkeypatch, configured
):
    doc, pulled = _pulled_link_doc(tmp_path, monkeypatch, "x")
    pulled = "&lt;!-- [x](private.txt) --&gt;"
    sync.pull(doc, FakeClient(gets=[{"body": pulled}]), {})
    client = FakeClient(remote_doc={"body": pulled, "content_type": 2})

    sync.push(doc, client, {})

    assert "upload" not in client.calls


def test_force_push_still_skips_after_the_web_side_removed_the_link(
    tmp_path, monkeypatch, configured
):
    doc, _ = _pulled_link_doc(tmp_path, monkeypatch)
    client = FakeClient(remote_doc={"body": "the link is gone", "content_type": 2})

    sync.push(doc, client, {}, force=True)

    assert "upload" not in client.calls


@pytest.mark.parametrize("spelling", ["./private.txt", "private.txt#top"])
def test_push_skips_a_remote_link_respelled_locally(
    spelling, tmp_path, monkeypatch, configured
):
    doc, pulled = _pulled_link_doc(tmp_path, monkeypatch)
    write_doc(doc, f"see [x]({spelling})")
    client = FakeClient(remote_doc={"body": pulled, "content_type": 2})

    sync.push(doc, client, {})

    assert "upload" not in client.calls
    assert client.saved_payload["body"] == f"see [x]({spelling})"


def test_force_push_skips_a_link_from_an_unmerged_conflicting_remote(
    tmp_path, monkeypatch, configured
):
    doc = tmp_path / "report.md"
    (tmp_path / "private.txt").write_text("secret")
    write_doc(doc, "local edit")
    Store(monkeypatch, {"1": saved_state()})
    with pytest.raises(RuntimeError, match="local changes conflict"):
        sync.pull(doc, FakeClient(gets=[{"body": "see [x](private.txt)"}]), {})
    write_doc(doc, "local edit, merged: see [x](private.txt)")
    client = FakeClient(remote_doc={"body": "the link is gone", "content_type": 2})

    sync.push(doc, client, {}, force=True)

    assert "upload" not in client.calls
