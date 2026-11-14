import datetime
import json

import pytest

from elport import browse, cli, config


def _stub_browse_client(monkeypatch, client):
    monkeypatch.setattr(config, "load", lambda *args: {})
    monkeypatch.setattr(
        cli, "_resolved_client", lambda data, profile, meta: ("test", client)
    )


class BrowseClient:
    def __init__(self, rows=None, body=""):
        self.rows = list(rows or [])
        self.body = body
        self.searches = []
        self.root = "https://e.example"

    def search(self, entity, params):
        self.searches.append((entity, dict(params)))
        return self.rows

    def get(self, entity, eid):
        return {"body": self.body}

    def uploads(self, entity, eid):
        return []


def test_list_command_defaults_to_self_scoped_experiments(monkeypatch, capsys):
    client = BrowseClient(rows=[{"id": 4, "date": "2026-09-18", "title": "Note"}])
    _stub_browse_client(monkeypatch, client)

    assert cli.main(["list"]) == 0

    entity, params = client.searches[0]
    assert entity == "experiments"
    assert params["scope"] == 1
    assert capsys.readouterr().out == "4  2026-09-18  Note\n"


def test_list_command_forwards_its_options(monkeypatch):
    client = BrowseClient(rows=[])
    _stub_browse_client(monkeypatch, client)

    assert (
        cli.main(
            [
                "list",
                "--entity",
                "items",
                "--scope",
                "team",
                "--limit",
                "3",
                "--offset",
                "6",
                "-q",
                "blot",
            ]
        )
        == 0
    )

    assert client.searches == [
        ("items", {"scope": 2, "limit": 3, "offset": 6, "q": "blot"}),
    ]


def test_list_command_needs_no_local_document(tmp_path, monkeypatch):
    monkeypatch.chdir(tmp_path)
    client = BrowseClient(rows=[])
    _stub_browse_client(monkeypatch, client)

    assert cli.main(["list"]) == 0
    assert list(tmp_path.iterdir()) == []


def test_list_command_all_walks_every_page(monkeypatch, capsys):
    rows = [{"id": n, "date": "", "title": f"t{n}"} for n in range(150)]
    client = BrowseClient()
    client.search = lambda entity, params: rows[
        params["offset"] : params["offset"] + params["limit"]
    ]
    _stub_browse_client(monkeypatch, client)

    assert cli.main(["list", "--all", "--json"]) == 0

    assert json.loads(capsys.readouterr().out) == rows


def test_list_command_forwards_order_sort_and_repeated_tags(monkeypatch):
    client = BrowseClient(rows=[])
    _stub_browse_client(monkeypatch, client)

    assert (
        cli.main(
            [
                "list",
                "--order",
                "comment",
                "--sort",
                "asc",
                "--tag",
                "a",
                "--tag",
                "b c",
            ]
        )
        == 0
    )

    params = client.searches[0][1]
    assert (params["order"], params["sort"], params["tags[]"]) == (
        "comment",
        "asc",
        ["a", "b c"],
    )


def test_list_command_rejects_an_order_the_server_would_silently_ignore(monkeypatch):
    # eLabFTW answers an unknown order with its default order, not an error.
    _stub_browse_client(monkeypatch, BrowseClient(rows=[]))

    with pytest.raises(SystemExit):
        cli.main(["list", "--order", "newest"])


def test_list_command_rejects_all_with_limit(monkeypatch):
    _stub_browse_client(monkeypatch, BrowseClient(rows=[]))

    with pytest.raises(SystemExit):
        cli.main(["list", "--all", "--limit", "5"])


def test_list_command_honors_the_entity_config_key(monkeypatch):
    client = BrowseClient(rows=[])
    monkeypatch.setattr(config, "load", lambda *args: {"entity": "items"})
    monkeypatch.setattr(
        cli, "_resolved_client", lambda data, profile, meta: ("test", client)
    )

    assert cli.main(["list"]) == 0

    assert client.searches[0][0] == "items"


def test_list_command_lets_the_cli_entity_win_over_the_config_key(monkeypatch):
    client = BrowseClient(rows=[])
    monkeypatch.setattr(config, "load", lambda *args: {"entity": "items"})
    monkeypatch.setattr(
        cli, "_resolved_client", lambda data, profile, meta: ("test", client)
    )

    assert cli.main(["list", "--entity", "experiments"]) == 0

    assert client.searches[0][0] == "experiments"


def test_list_command_rejects_dash_n_so_it_cannot_be_read_as_dry_run(monkeypatch):
    client = BrowseClient(rows=[])
    _stub_browse_client(monkeypatch, client)

    with pytest.raises(SystemExit):
        cli.main(["list", "-n", "5"])


def test_list_command_reports_a_negative_offset_as_a_cli_error(monkeypatch, capsys):
    client = BrowseClient(rows=[])
    _stub_browse_client(monkeypatch, client)

    assert cli.main(["list", "--offset", "-1"]) == 1
    assert capsys.readouterr().err != ""
    assert client.searches == []


def test_view_command_prints_the_remote_body(monkeypatch, capsys):
    client = BrowseClient(body="remote text\n")
    _stub_browse_client(monkeypatch, client)

    assert cli.main(["view", "42"]) == 0

    assert capsys.readouterr().out == "remote text\n"


def test_view_command_writes_into_the_output_directory(tmp_path, monkeypatch, capsys):
    client = BrowseClient(body="remote text\n")
    _stub_browse_client(monkeypatch, client)
    out = tmp_path / "out"

    assert cli.main(["view", "42", "-o", str(out)]) == 0

    assert (out / "report.md").read_text() == "remote text\n"
    assert capsys.readouterr().out == f"{out / 'report.md'}\n"


def test_view_command_reports_a_non_empty_output_directory(
    tmp_path, monkeypatch, capsys
):
    (tmp_path / "keep.txt").write_text("mine")
    _stub_browse_client(monkeypatch, BrowseClient(body="text\n"))

    assert cli.main(["view", "42", "-o", str(tmp_path)]) == 1
    assert "not empty" in capsys.readouterr().err


def test_view_command_reports_an_invalid_id_as_a_cli_error(monkeypatch, capsys):
    client = BrowseClient()
    _stub_browse_client(monkeypatch, client)

    assert cli.main(["view", "0"]) == 1
    assert capsys.readouterr().err != ""


def test_view_command_honors_the_entity_config_key(monkeypatch):
    client = BrowseClient(body="text\n")
    gets = []
    monkeypatch.setattr(config, "load", lambda *args: {"entity": "items"})
    monkeypatch.setattr(
        cli, "_resolved_client", lambda data, profile, meta: ("test", client)
    )
    monkeypatch.setattr(client, "get", lambda entity, eid: gets.append(entity) or {})

    assert cli.main(["view", "5"]) == 0

    assert gets == ["items"]


class ActivityClient:
    def __init__(self, rows):
        self.rows = rows
        self.searches = []

    def search(self, entity, params):
        self.searches.append((entity, dict(params)))
        return self.rows if params["offset"] == 0 else []


@pytest.mark.parametrize(("flags", "scope"), [([], 1), (["--team"], 2)])
def test_cli_activity_reads_own_or_team_entities(monkeypatch, capsys, flags, scope):
    # Created today, so the row is inside the grid whatever the date and width.
    today = datetime.datetime.now().astimezone().date()
    client = ActivityClient(
        [{"created_at": f"{today} 10:00:00", "fullname": "Yasmin Heller"}]
    )
    monkeypatch.setattr(cli.config, "load", lambda *args: {})
    monkeypatch.setattr(cli, "_resolved_client", lambda *args: ("demo", client))

    assert cli.main(["activity", *flags]) == 0

    assert client.searches[0] == (
        "experiments",
        {"scope": scope, "limit": browse.SEARCH_PAGE, "offset": 0},
    )
    assert "Yasmin Heller" in capsys.readouterr().out


def test_activity_help_points_to_the_raw_rows(capsys):
    with pytest.raises(SystemExit):
        cli.main(["activity", "--help"])

    assert "elport list --all --json" in capsys.readouterr().out
