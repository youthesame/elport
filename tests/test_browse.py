from __future__ import annotations

import json

import pytest

from elport import browse


class FakeSearchClient:
    def __init__(self, rows=None, entity_doc=None):
        self.rows = list(rows or [])
        self.entity_doc = dict(entity_doc or {})
        self.searches = []
        self.gets = []
        self.root = "https://e.example"

    def search(self, entity, params):
        self.searches.append((entity, dict(params)))
        return self.rows

    def get(self, entity, eid):
        self.gets.append((entity, eid))
        return dict(self.entity_doc)


ROWS = [
    {"id": 42, "date": "2026-09-18", "title": "Cell culture day 3"},
    {"id": 7, "date": "2026-08-01", "title": "Plasmid prep"},
]


def test_list_defaults_to_self_scope():
    client = FakeSearchClient(rows=ROWS)

    browse.entities(client, "experiments")

    entity, params = client.searches[0]
    assert entity == "experiments"
    assert params["scope"] == 1


def test_list_passes_scope_limit_offset_and_query():
    client = FakeSearchClient(rows=ROWS)

    browse.entities(
        client, "items", scope="team", limit=5, offset=10, query="western blot"
    )

    assert client.searches == [
        ("items", {"scope": 2, "limit": 5, "offset": 10, "q": "western blot"}),
    ]


def test_list_omits_query_when_absent():
    client = FakeSearchClient(rows=ROWS)

    browse.entities(client, "experiments", limit=3)

    assert "q" not in client.searches[0][1]


@pytest.mark.parametrize(("scope", "expected"), [("self", 1), ("team", 2), ("all", 3)])
def test_list_maps_scope_names_to_api_integers(scope, expected):
    client = FakeSearchClient(rows=ROWS)

    browse.entities(client, "experiments", scope=scope)

    assert client.searches[0][1]["scope"] == expected


def test_list_prints_id_date_and_title(capsys):
    client = FakeSearchClient(rows=ROWS)

    browse.entities(client, "experiments")

    assert capsys.readouterr().out == (
        "42  2026-09-18  Cell culture day 3\n7   2026-08-01  Plasmid prep\n"
    )


def test_list_prints_nothing_but_a_notice_when_empty(capsys):
    client = FakeSearchClient(rows=[])

    browse.entities(client, "experiments")

    assert capsys.readouterr().out == "no experiments found\n"


def test_list_json_emits_the_rows_verbatim(capsys):
    client = FakeSearchClient(rows=ROWS)

    browse.entities(client, "experiments", as_json=True)

    assert json.loads(capsys.readouterr().out) == ROWS


def test_list_json_emits_an_empty_array_without_a_notice(capsys):
    client = FakeSearchClient(rows=[])

    browse.entities(client, "experiments", as_json=True)

    assert json.loads(capsys.readouterr().out) == []


def test_list_warns_on_stderr_when_the_page_is_full(capsys):
    client = FakeSearchClient(rows=ROWS)

    browse.entities(client, "experiments", limit=2)

    captured = capsys.readouterr()
    assert "--limit" in captured.err
    assert "--limit" not in captured.out


def test_list_does_not_warn_when_the_page_is_not_full(capsys):
    client = FakeSearchClient(rows=ROWS)

    browse.entities(client, "experiments", limit=3)

    assert capsys.readouterr().err == ""


def test_list_rejects_an_unknown_scope():
    client = FakeSearchClient(rows=ROWS)

    with pytest.raises(ValueError, match="^scope must be one of: self, team, all$"):
        browse.entities(client, "experiments", scope="everything")

    assert client.searches == []


@pytest.mark.parametrize("limit", [0, -1])
def test_list_rejects_a_non_positive_limit(limit):
    client = FakeSearchClient(rows=ROWS)

    with pytest.raises(ValueError, match="^limit must be a positive integer$"):
        browse.entities(client, "experiments", limit=limit)

    assert client.searches == []


def test_list_tolerates_rows_missing_a_date_or_title(capsys):
    client = FakeSearchClient(rows=[{"id": 3}])

    browse.entities(client, "experiments")

    assert capsys.readouterr().out == "3\n"


def test_view_prints_the_remote_body_verbatim(capsys):
    client = FakeSearchClient(entity_doc={"body": "# Title\n\nBody text\n"})

    browse.view(client, "experiments", 42)

    assert capsys.readouterr().out == "# Title\n\nBody text\n"
    assert client.gets == [("experiments", 42)]


def test_view_does_not_write_a_base_or_touch_the_working_tree(tmp_path, monkeypatch):
    monkeypatch.chdir(tmp_path)
    client = FakeSearchClient(entity_doc={"body": "text\n"})

    browse.view(client, "experiments", 42)

    assert list(tmp_path.iterdir()) == []


def test_view_prints_an_html_body_without_conversion(capsys):
    client = FakeSearchClient(entity_doc={"body": "<p>written in the web UI</p>"})

    browse.view(client, "experiments", 1)

    assert capsys.readouterr().out == "<p>written in the web UI</p>"


def test_view_does_not_add_a_trailing_newline_the_body_lacks(capsys):
    client = FakeSearchClient(entity_doc={"body": "no final newline"})

    browse.view(client, "experiments", 1)

    assert capsys.readouterr().out == "no final newline"


def test_view_keeps_the_trailing_newlines_the_body_has(capsys):
    client = FakeSearchClient(entity_doc={"body": "text\n\n\n"})

    browse.view(client, "experiments", 1)

    assert capsys.readouterr().out == "text\n\n\n"


def test_view_prints_nothing_for_an_empty_body(capsys):
    client = FakeSearchClient(entity_doc={"body": ""})

    browse.view(client, "experiments", 1)

    assert capsys.readouterr().out == ""


def test_view_rejects_an_invalid_id_before_any_request():
    client = FakeSearchClient()

    with pytest.raises(ValueError):
        browse.view(client, "experiments", 0)

    assert client.gets == []


def test_list_warns_on_stderr_in_json_mode_too(capsys):
    client = FakeSearchClient(rows=ROWS)

    browse.entities(client, "experiments", limit=2, as_json=True)

    captured = capsys.readouterr()
    assert "--limit" in captured.err
    assert json.loads(captured.out) == ROWS


def test_list_does_not_warn_in_json_mode_when_the_page_is_not_full(capsys):
    client = FakeSearchClient(rows=ROWS)

    browse.entities(client, "experiments", limit=3, as_json=True)

    assert capsys.readouterr().err == ""


def test_list_rejects_a_negative_offset():
    client = FakeSearchClient(rows=ROWS)

    with pytest.raises(ValueError, match="^offset must not be negative$"):
        browse.entities(client, "experiments", offset=-1)

    assert client.searches == []


def test_list_accepts_a_zero_offset():
    client = FakeSearchClient(rows=ROWS)

    browse.entities(client, "experiments", offset=0)

    assert client.searches[0][1]["offset"] == 0
