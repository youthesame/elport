from __future__ import annotations

import datetime as dt
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


# A Tuesday: the last column holds Mon 11-02 and Tue 11-03, the rest is future.
TODAY = dt.date(2026, 11, 3)


def row(created, userid=29, fullname="Yasmin Heller"):
    return {"created_at": created, "userid": userid, "fullname": fullname}


def grid(text: str) -> dict[str, str]:
    """Weekday label -> that row's cells, for the first author's grid."""
    lines = text.splitlines()
    start = next(i for i, line in enumerate(lines) if line.startswith("Mon "))
    labels = ("Mon", "Tue", "Wed", "Thu", "Fri", "Sat", "Sun")
    return {
        label: lines[start + i][4::2].ljust(browse.WEEKS)
        for i, label in enumerate(labels)
    }


def test_each_day_counts_the_entities_created_on_it():
    rows = [
        row("2026-11-02 09:00:00"),
        row("2026-11-02 17:30:00"),
        row("2026-11-03 08:00:00"),
    ]

    cells = grid(browse.activity(rows, "experiments", TODAY))

    assert cells["Mon"][-1] == "▒"
    assert cells["Tue"][-1] == "░"
    assert cells["Wed"][-1] == " "


def test_a_day_without_entities_is_a_dot():
    cells = grid(browse.activity([row("2026-11-03 08:00:00")], "experiments", TODAY))

    assert cells["Mon"][-1] == "·"
    assert cells["Sun"][0] == "·"


@pytest.mark.parametrize(
    ("count", "glyph"), [(1, "░"), (2, "▒"), (3, "▓"), (4, "█"), (9, "█")]
)
def test_the_glyph_darkens_with_the_count_and_caps_at_four(count, glyph):
    rows = [row("2026-11-02 10:00:00")] * count

    assert grid(browse.activity(rows, "experiments", TODAY))["Mon"][-1] == glyph


def test_the_grid_spans_53_weeks_starting_on_a_monday():
    rows = [row("2025-11-03 10:00:00"), row("2025-11-02 10:00:00")]

    text = browse.activity(rows, "experiments", TODAY)

    # 2025-11-03 is the Monday 52 weeks before this week's: the first cell.
    assert grid(text)["Mon"][0] == "░"
    assert "1 experiment " in text
    assert "2025-11-03 → 2026-11-03" in text


def test_month_labels_sit_above_the_week_holding_the_first():
    text = browse.activity([row("2026-11-03 08:00:00")], "experiments", TODAY)

    months = next(line for line in text.splitlines() if "Dec" in line)
    # 2025-12-01 is a Monday, four weeks after the first column.
    assert months.index("Dec") == 4 + 2 * 4
    # 2026-11-01 is a Sunday, so November labels the second-to-last column.
    assert months.index("Nov", 5) == 4 + 2 * (browse.WEEKS - 2)


def test_the_first_week_is_labelled_with_its_month():
    text = browse.activity([row("2026-11-03 08:00:00")], "experiments", TODAY)

    months = next(line for line in text.splitlines() if "Dec" in line)
    assert months.index("Nov") == 4


def test_the_first_weeks_month_gives_way_to_a_label_it_would_overlap():
    # The grid starts on Mon 2025-09-22; October begins in the second column.
    today = dt.date(2026, 9, 21)

    text = browse.activity([row("2026-09-21 08:00:00")], "experiments", today)

    months = next(line for line in text.splitlines() if "Nov" in line)
    assert months.startswith("      Oct")


def test_every_other_weekday_is_labelled_monday_to_sunday():
    text = browse.activity([row("2026-11-03 08:00:00")], "experiments", TODAY)

    lines = text.splitlines()
    start = next(i for i, line in enumerate(lines) if line.startswith("Mon "))
    labels = [line[:4] for line in lines[start : start + 7]]
    assert labels == ["Mon ", "    ", "Wed ", "    ", "Fri ", "    ", "Sun "]


def test_each_author_gets_a_grid_busiest_first():
    rows = [
        row("2026-11-02 10:00:00", 28, "Janiya Cole"),
        row("2026-11-02 10:00:00", 35, "Reynold Turcotte"),
        row("2026-11-03 10:00:00", 35, "Reynold Turcotte"),
    ]

    text = browse.activity(rows, "experiments", TODAY)

    assert text.index("Reynold Turcotte  2 experiments · 2 active days") < text.index(
        "Janiya Cole  1 experiment · 1 active day"
    )
    assert text.count("Mon ") == 2


def test_an_author_without_a_name_is_shown_by_user_id():
    text = browse.activity([row("2026-11-02 10:00:00", 12, None)], "experiments", TODAY)

    assert "user 12  1 experiment" in text


def test_authors_sharing_a_name_keep_separate_grids():
    rows = [
        row("2026-11-02 10:00:00", 28, "Alex Kim"),
        row("2026-11-02 10:00:00", 41, "Alex Kim"),
    ]

    text = browse.activity(rows, "experiments", TODAY)

    assert text.count("Alex Kim  1 experiment · 1 active day") == 2


def test_rows_without_a_creation_date_are_skipped():
    rows = [row("2026-11-02 10:00:00"), row(None), row("")]

    assert "1 experiment " in browse.activity(rows, "experiments", TODAY)


def test_nothing_in_the_window_prints_a_notice():
    text = browse.activity([row("2024-01-01 10:00:00")], "items", TODAY)

    assert text == "no items created since 2025-11-03\n"


def test_plain_output_has_no_escape_codes():
    text = browse.activity([row("2026-11-02 10:00:00")], "experiments", TODAY)

    assert "\033" not in text
    assert "· 0  ░ 1  ▒ 2  ▓ 3  █ 4+" in text


def test_color_output_paints_each_cell_green():
    text = browse.activity(
        [row("2026-11-02 10:00:00")], "experiments", TODAY, color=True
    )

    assert f"\033[38;5;{browse.PALETTE[1]}m░\033[0m" in text
    assert f"\033[38;5;{browse.PALETTE[0]}m·\033[0m" in text


def test_cells_are_spaced_one_column_apart():
    text = browse.activity([row("2026-11-02 08:00:00")], "experiments", TODAY)

    sunday = next(line for line in text.splitlines() if line.startswith("Sun "))
    assert sunday[4:9] == "· · ·"


def test_a_narrow_terminal_keeps_the_most_recent_weeks_that_fit():
    rows = [row("2026-02-23 08:00:00"), row("2026-02-22 08:00:00")]

    text = browse.activity(rows, "experiments", TODAY, width=80)

    # 37 weeks of two columns, plus the label and room for the last month name.
    assert "2026-02-23 → 2026-11-03" in text
    assert "1 experiment " in text
    assert grid(text)["Mon"][0] == "░"
    assert max(len(line) for line in text.splitlines()) <= 80


def test_a_tiny_terminal_still_shows_the_current_week():
    text = browse.activity([row("2026-11-02 08:00:00")], "experiments", TODAY, width=5)

    assert grid(text)["Mon"][0] == "░"
