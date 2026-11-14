from __future__ import annotations

from _sync_harness import (
    EXP12,
    LinkClient,
    Store,
    elport_doc,
    patch_returning,
    saved_state,
)

from elport import frontmatter, sync


def test_push_sends_a_document_link_as_the_entity_url_and_registers_it(
    tmp_path, monkeypatch, configured
):
    elport_doc(tmp_path / "exp12" / "report.md", id=12)
    doc = elport_doc(
        tmp_path / "exp13" / "report.md", "[prev](../exp12/report.md)", id=13
    )
    store = Store(monkeypatch, {"13": saved_state(local="old", remote="")})
    client = LinkClient(gets=[{"body": ""}, {"body": ""}], remote_doc={})
    client.patch = patch_returning(client, "stored")

    sync.push(doc, client, {})

    assert client.saved_payload["body"] == f"[prev]({EXP12})"
    assert client.links == [("experiments", 13, "experiments", 12)]
    saved = store.data["13"]
    assert saved["doc_links"] == {EXP12: {"markdown": "../exp12/report.md"}}
    assert saved["links"] == [["experiments", 12]]
    assert saved["path"] == str(doc.resolve())


def test_push_records_each_syntax_of_a_repeated_link_separately(
    tmp_path, monkeypatch, configured
):
    elport_doc(tmp_path / "other.md", id=12)
    body = '<a href="other.md">a</a> [x](<other.md>)'
    doc = elport_doc(tmp_path / "report.md", body, id=13)
    store = Store(monkeypatch, {"13": saved_state(local="old", remote="")})
    client = LinkClient(gets=[{"body": ""}, {"body": ""}])
    client.patch = patch_returning(client, "stored")

    sync.push(doc, client, {})

    links = store.data["13"]["doc_links"]
    assert links == {EXP12: {"html": "other.md", "markdown": "<other.md>"}}
    sent = client.saved_payload["body"]
    assert sync.relink(sent, links) == body


def test_push_registers_each_link_once_and_never_removes_one(
    tmp_path, monkeypatch, configured
):
    elport_doc(tmp_path / "exp12" / "report.md", id=12)
    doc = elport_doc(tmp_path / "exp13" / "report.md", "no links now", id=13)
    previous = {**saved_state(local="old", remote=""), "links": [["experiments", 12]]}
    store = Store(monkeypatch, {"13": previous})
    client = LinkClient(gets=[{"body": ""}, {"body": ""}])
    client.patch = patch_returning(client, "stored")

    sync.push(doc, client, {})

    assert client.links == []
    assert store.data["13"]["links"] == [["experiments", 12]]


def test_push_links_items_to_their_database_page(tmp_path, monkeypatch, configured):
    elport_doc(tmp_path / "item.md", entity="items", id=5)
    doc = elport_doc(tmp_path / "report.md", "[i](item.md)", id=13)
    Store(monkeypatch, {"13": saved_state(local="old", remote="")})
    client = LinkClient(gets=[{"body": ""}, {"body": ""}])
    client.patch = patch_returning(client, "stored")

    sync.push(doc, client, {})

    assert client.saved_payload["body"] == (
        "[i](https://e.example/database.php?mode=view&id=5)"
    )
    assert client.links == [("experiments", 13, "items", 5)]


def test_push_does_not_register_a_link_on_another_server(
    tmp_path, monkeypatch, configured
):
    elport_doc(tmp_path / "other.md", id=12, profile="labB")
    doc = elport_doc(tmp_path / "report.md", "[o](other.md)", id=13)
    Store(monkeypatch, {"13": saved_state(local="old", remote="")})
    monkeypatch.setattr(
        sync.config_module,
        "base_target",
        lambda config, profile, meta: (
            "x",
            "https://b.example"
            if meta.get("profile") == "labB"
            else "https://e.example",
            True,
        ),
    )
    client = LinkClient(gets=[{"body": ""}, {"body": ""}])
    client.patch = patch_returning(client, "stored")

    sync.push(doc, client, {})

    assert client.saved_payload["body"] == (
        "[o](https://b.example/experiments.php?mode=view&id=12)"
    )
    assert client.links == []


def test_push_resolves_a_link_target_under_its_own_config(
    tmp_path, monkeypatch, configured
):
    target = tmp_path / "inventory" / "buffer.md"
    target.parent.mkdir()
    target.write_text(frontmatter.render({"id": 5, "profile": "test"}, "x"))
    doc = elport_doc(tmp_path / "report.md", "[b](inventory/buffer.md)", id=13)
    Store(monkeypatch, {"13": saved_state(local="old", remote="")})
    monkeypatch.setattr(
        sync.config_module,
        "load",
        lambda project, doc_dir: (
            {"entity": "items"} if doc_dir.name == "inventory" else {}
        ),
    )
    client = LinkClient(gets=[{"body": ""}, {"body": ""}])
    client.patch = patch_returning(client, "stored")

    sync.push(doc, client, {})

    assert client.links == [("experiments", 13, "items", 5)]
