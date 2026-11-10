from __future__ import annotations

import pytest
from _sync_harness import FakeClient

from elport import sync

BASE = "https://e.example"
FIG = {"id": 3, "long_name": "aa/fig", "real_name": "fig.png", "storage": 1}
RAW = {"id": 4, "long_name": "bb/raw", "real_name": "raw.csv", "storage": 1}
FIG_URL = f"{BASE}/app/download.php?f=aa%2Ffig&name=fig.png&storage=1"
HTML_BODY = (
    '<p style="line-height:1;"><img src="app/download.php?f=aa/fig" alt="x"></p>\n\n'
    '<p>see <a href="experiments.php?mode=view&amp;id=19">BEN</a></p>'
)
HTML_READABLE = (
    '<p><img src="fig.png"></p>\n'
    f'<p>see <a href="{BASE}/experiments.php?mode=view&amp;id=19">BEN</a></p>'
)


def test_snapshot_writes_the_body_and_only_the_uploads_it_shows(tmp_path, capsys):
    out = tmp_path / "out"
    client = FakeClient(
        remote_doc={"body": f"![figure]({FIG_URL})\n"}, uploads=[FIG, RAW]
    )

    sync.view(client, "experiments", 42, BASE, out)

    assert (out / "report.md").read_text() == "![figure](fig.png)\n"
    assert (out / "fig.png").read_bytes() == b"data"
    assert sorted(p.name for p in out.iterdir()) == ["fig.png", "report.md"]
    assert capsys.readouterr().out.splitlines() == [
        str(out / "report.md"),
        str(out / "fig.png"),
    ]


def test_snapshot_of_an_html_entity_is_its_readable_body(tmp_path):
    out = tmp_path / "out"
    client = FakeClient(
        remote_doc={"body": HTML_BODY, "content_type": 1}, uploads=[FIG, RAW]
    )

    assert sync.snapshot(client, "experiments", 42, out, BASE) == [
        out / "report.md",
        out / "fig.png",
    ]

    assert (out / "report.md").read_text() == HTML_READABLE
    assert (out / "fig.png").read_bytes() == b"data"


def test_snapshot_is_never_a_working_copy(tmp_path, monkeypatch):
    def no_state(*args):
        raise AssertionError("snapshot must not touch state")

    monkeypatch.setattr(sync.state, "load", no_state)
    monkeypatch.setattr(sync.state, "save", no_state)
    client = FakeClient(remote_doc={"body": "text\n"})

    sync.snapshot(client, "experiments", 42, tmp_path / "out", BASE)

    # No front matter: push and pull cannot mistake it for a document.
    assert (tmp_path / "out" / "report.md").read_text() == "text\n"


def test_snapshot_refuses_a_non_empty_directory_before_any_request(tmp_path):
    (tmp_path / "keep.txt").write_text("mine")
    client = FakeClient(remote_doc={"body": "text\n"})

    with pytest.raises(RuntimeError, match="not empty"):
        sync.snapshot(client, "experiments", 42, tmp_path, BASE)

    assert client.calls == []
    assert (tmp_path / "keep.txt").read_text() == "mine"


def test_snapshot_fills_an_existing_empty_directory(tmp_path):
    client = FakeClient(remote_doc={"body": "text\n"})

    sync.snapshot(client, "experiments", 42, tmp_path, BASE)

    assert (tmp_path / "report.md").read_text() == "text\n"


def test_snapshot_never_places_a_hidden_upload(tmp_path):
    hidden = {"id": 5, "long_name": "cc/env", "real_name": ".envrc", "storage": 1}
    url = f"{BASE}/app/download.php?f=cc%2Fenv&name=.envrc&storage=1"
    client = FakeClient(remote_doc={"body": f"[env]({url})\n"}, uploads=[hidden])

    sync.snapshot(client, "experiments", 42, tmp_path / "out", BASE)

    assert not (tmp_path / "out" / ".envrc").exists()
    assert url in (tmp_path / "out" / "report.md").read_text()


def test_snapshot_rejects_an_invalid_id_before_any_request(tmp_path):
    client = FakeClient()

    with pytest.raises(ValueError):
        sync.snapshot(client, "experiments", 0, tmp_path / "out", BASE)

    assert client.calls == []
    assert not (tmp_path / "out").exists()


def test_snapshot_refuses_an_attachment_named_like_the_body(tmp_path):
    note = {"id": 6, "long_name": "dd/note", "real_name": "report.md", "storage": 1}
    url = f"{BASE}/app/download.php?f=dd%2Fnote&name=report.md&storage=1"
    client = FakeClient(remote_doc={"body": f"[prev]({url})\n"}, uploads=[note])

    with pytest.raises(RuntimeError, match="report.md"):
        sync.snapshot(client, "experiments", 42, tmp_path / "out", BASE)

    assert not (tmp_path / "out").exists()


def test_snapshot_refuses_a_body_that_would_read_as_an_elport_document(tmp_path):
    body = "---\nid: 9\nprofile: lab\n---\n\ntext\n"
    client = FakeClient(remote_doc={"body": body})

    with pytest.raises(RuntimeError, match="front matter"):
        sync.snapshot(client, "experiments", 42, tmp_path / "out", BASE)

    assert not (tmp_path / "out").exists()


# view without -o prints what the snapshot's report.md would hold (ADR-0009).


def test_view_prints_what_the_snapshot_would_write(capsys):
    client = FakeClient(
        remote_doc={"body": HTML_BODY, "content_type": 1}, uploads=[FIG, RAW]
    )

    sync.view(client, "experiments", 42, BASE)

    out, err = capsys.readouterr()
    assert out == HTML_READABLE
    assert "1 attachment" in err and "-o" in err
    assert "download" not in client.calls


def test_view_prints_a_markdown_body_as_stored(capsys):
    client = FakeClient(remote_doc={"body": "# Title\n\n<b>Body</b> text\n\n\n"})

    sync.view(client, "experiments", 42, BASE)

    assert capsys.readouterr() == ("# Title\n\n<b>Body</b> text\n\n\n", "")


def test_view_does_not_add_a_trailing_newline_the_body_lacks(capsys):
    client = FakeClient(remote_doc={"body": "no final newline"})

    sync.view(client, "experiments", 1, BASE)

    assert capsys.readouterr().out == "no final newline"


def test_view_prints_nothing_for_an_empty_body(capsys):
    client = FakeClient(remote_doc={"body": None})

    sync.view(client, "experiments", 1, BASE)

    assert capsys.readouterr().out == ""


def test_view_does_not_write_a_base_or_touch_the_working_tree(tmp_path, monkeypatch):
    monkeypatch.chdir(tmp_path)

    def no_state(*args):
        raise AssertionError("view must not touch state")

    monkeypatch.setattr(sync.state, "load", no_state)
    monkeypatch.setattr(sync.state, "save", no_state)
    client = FakeClient(remote_doc={"body": f"![figure]({FIG_URL})\n"}, uploads=[FIG])

    sync.view(client, "experiments", 42, BASE)

    assert list(tmp_path.iterdir()) == []


def test_view_rejects_an_invalid_id_before_any_request():
    client = FakeClient()

    with pytest.raises(ValueError):
        sync.view(client, "experiments", 0, BASE)

    assert client.calls == []


# What the entity holds outside its body is named on stderr, never written (#64).

ASIDE = {
    "compounds_links": [{"id": 7, "name": "Benzene", "smiles": "c1ccccc1"}],
    "steps": [{"id": 1}, {"id": 2}],
    "comments": [{"id": 1}, {"id": 2}, {"id": 3}],
}
ASIDE_LINE = (
    "not shown in the body: 1 attachment(s) (raw.csv), 1 compound(s), 2 step(s), "
    f"3 comment(s); see {BASE}/experiments.php?mode=view&id=42\n"
)


def test_view_names_what_the_body_does_not_show(capsys):
    client = FakeClient(
        remote_doc={"body": HTML_BODY, "content_type": 1, **ASIDE}, uploads=[FIG, RAW]
    )

    sync.view(client, "experiments", 42, BASE)

    out, err = capsys.readouterr()
    assert out == HTML_READABLE
    assert ASIDE_LINE in err


def test_snapshot_names_what_it_leaves_on_the_server(tmp_path, capsys):
    out = tmp_path / "out"
    client = FakeClient(
        remote_doc={"body": f"![figure]({FIG_URL})\n", **ASIDE}, uploads=[FIG, RAW]
    )

    sync.view(client, "experiments", 42, BASE, out)

    assert sorted(p.name for p in out.iterdir()) == ["fig.png", "report.md"]
    assert capsys.readouterr().err == ASIDE_LINE


def test_view_names_an_attachment_by_its_link_in_any_form(capsys):
    body = f'<img src="app/download.php?f=aa/fig">\n[raw]({BASE}/app/download.php?f=bb%2Fraw)\n'
    client = FakeClient(remote_doc={"body": body}, uploads=[FIG, RAW])

    sync.view(client, "experiments", 42, BASE)

    assert "not shown in the body" not in capsys.readouterr().err


def test_view_names_an_image_it_cannot_show(capsys):
    foreign = f"{BASE}/app/download.php?f=bb%2Ftheirs&name=theirs.png&storage=1"
    client = FakeClient(remote_doc={"body": f"![x]({foreign})\n"})

    sync.view(client, "experiments", 42, BASE)

    assert "kept as a URL (not downloaded, not localized): theirs.png" in (
        capsys.readouterr().err
    )
