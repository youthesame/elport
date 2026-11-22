from __future__ import annotations

import pytest
from _sync_harness import FakeClient, write_doc

from elport import sync


def test_comments_prints_thread_without_writing_files(tmp_path, configured, capsys):
    doc = tmp_path / "report.md"
    write_doc(doc, "Local body\n")
    before = doc.read_bytes()
    existing = set(tmp_path.iterdir())
    client = FakeClient(
        comments=[
            {
                "fullname": "Ada Lovelace",
                "created_at": "2026-08-11 09:00:00",
                "modified_at": "2026-08-11 09:00:00",
                "comment": "First comment",
            },
            {
                "fullname": "Grace Hopper",
                "created_at": "2026-08-11 10:00:00",
                "modified_at": "2026-08-11 10:05:00",
                "comment": "Edited comment",
            },
        ]
    )

    sync.comments(doc, client, {})

    assert capsys.readouterr().out == (
        "Ada Lovelace 2026-08-11 09:00:00\n"
        "First comment\n\n"
        "Grace Hopper 2026-08-11 10:00:00 (edited)\n"
        "Edited comment\n"
    )
    assert client.calls == ["comments"]
    assert doc.read_bytes() == before
    assert set(tmp_path.iterdir()) == existing


def test_comments_prints_empty_thread(tmp_path, configured, capsys):
    doc = tmp_path / "report.md"
    write_doc(doc, "Local body\n")
    client = FakeClient()

    sync.comments(doc, client, {})

    assert capsys.readouterr().out == "no comments\n"


def test_comments_requires_id(tmp_path, configured):
    doc = tmp_path / "report.md"
    doc.write_text("Local body\n", encoding="utf-8")
    client = FakeClient()

    with pytest.raises(RuntimeError, match="^id is required$"):
        sync.comments(doc, client, {})

    assert client.calls == []


def test_show_comments_reads_the_thread_by_entity_and_id(capsys):
    client = FakeClient()

    sync.show_comments(client, "items", 17)

    assert client.calls == ["comments"]
    assert capsys.readouterr().out == "no comments\n"


def test_comment_posts_to_document_target(tmp_path, configured, capsys):
    doc = tmp_path / "report.md"
    write_doc(doc, "Local body\n", id=17, entity="items")
    client = FakeClient()

    sync.comment(doc, client, {}, text="A useful note")

    assert client.comment_posts == [("items", 17, {"comment": "A useful note"})]
    assert capsys.readouterr().out == "commented on items/17\n"


@pytest.mark.parametrize("text", ["", "   ", "\t\n"])
def test_comment_rejects_empty_text_before_network(tmp_path, configured, text):
    doc = tmp_path / "report.md"
    write_doc(doc, "Local body\n")
    client = FakeClient()

    with pytest.raises(RuntimeError, match="^comment text is empty$"):
        sync.comment(doc, client, {}, text=text)

    assert client.calls == []


def test_comment_requires_id(tmp_path, configured):
    doc = tmp_path / "report.md"
    doc.write_text("Local body\n", encoding="utf-8")
    client = FakeClient()

    with pytest.raises(RuntimeError, match="^id is required$"):
        sync.comment(doc, client, {}, text="hello")

    assert client.calls == []


def test_post_comment_posts_by_entity_and_id(capsys):
    client = FakeClient()

    sync.post_comment(client, "items", 17, "Checked")

    assert client.comment_posts == [("items", 17, {"comment": "Checked"})]
    assert capsys.readouterr().out == "commented on items/17\n"


def test_post_comment_rejects_empty_text_and_invalid_id_before_network():
    client = FakeClient()

    with pytest.raises(RuntimeError, match="^comment text is empty$"):
        sync.post_comment(client, "experiments", 17, "   ")
    with pytest.raises(ValueError, match="positive integer"):
        sync.post_comment(client, "experiments", 0, "Checked")

    assert client.calls == []
