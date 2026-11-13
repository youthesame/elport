from __future__ import annotations

import datetime as dt
import json
import sys
from collections import Counter, defaultdict

SCOPES = {"self": 1, "team": 2, "all": 3}
DEFAULT_LIMIT = 50
SEARCH_PAGE = 100
# eLabFTW's own order names. It answers an unknown one with its default order
# rather than an error, so only these are offered.
ORDERS = (
    "cat", "comment", "customid", "date", "id",
    "lastchange", "rating", "status", "title", "user",
)  # fmt: skip
# The column an order sorts by, shown in place of the date so the order reads.
ORDER_COLUMNS = {"lastchange": "modified_at", "comment": "recent_comment"}


def every(
    client,
    entity: str,
    scope: str,
    query: str | None = None,
    offset: int = 0,
    **filters,
):
    # Page until an empty answer: a server that caps the page size below ours
    # must not end the walk early.
    params = {"scope": SCOPES[scope], **({"q": query} if query else {}), **filters}
    while True:
        rows = client.search(entity, {**params, "limit": SEARCH_PAGE, "offset": offset})
        if not rows:
            return
        yield from rows
        offset += len(rows)


def entities(
    client,
    entity: str,
    scope: str = "self",
    limit: int | None = DEFAULT_LIMIT,
    offset: int = 0,
    query: str | None = None,
    as_json: bool = False,
    order: str | None = None,
    sort: str | None = None,
    tags: list[str] | None = None,
) -> None:
    if scope not in SCOPES:
        raise ValueError("scope must be one of: self, team, all")
    if limit is not None and limit < 1:
        raise ValueError("limit must be a positive integer")
    if offset < 0:
        raise ValueError("offset must not be negative")
    filters = {
        key: value
        for key, value in {"order": order, "sort": sort, "tags[]": tags}.items()
        if value
    }
    if limit is None:
        rows = list(every(client, entity, scope, query, offset, **filters))
    else:
        params = {"scope": SCOPES[scope], "limit": limit, "offset": offset, **filters}
        if query:
            params["q"] = query
        rows = client.search(entity, params)

    if as_json:
        print(json.dumps(rows, indent=2, ensure_ascii=False))
    elif not rows:
        print(f"no {entity} found")
    else:
        column = ORDER_COLUMNS.get(order, "date")
        width = max(len(str(row.get("id", ""))) for row in rows)
        when = max(len(row.get(column) or "") for row in rows)
        for row in rows:
            date = row.get(column) or ""
            title = row.get("title") or ""
            print(f"{row.get('id', '')!s:<{width}}  {date:<{when}}  {title}".rstrip())
    # The notice goes to stderr, so it reaches a --json pipeline too: a silently
    # truncated id list would make a scripted clone-everything miss entities.
    if limit is not None and len(rows) >= limit:
        print(
            f"showing {limit}; raise --limit or page with --offset for more",
            file=sys.stderr,
        )


WEEKS = 53
GLYPHS = "·░▒▓█"
# xterm-256 greys and greens close to GitHub's dark theme; 256 colours work in
# every terminal elport targets, unlike 24-bit colour (macOS Terminal.app).
PALETTE = (240, 22, 28, 34, 40)
MONTHS = (
    "Jan", "Feb", "Mar", "Apr", "May", "Jun",
    "Jul", "Aug", "Sep", "Oct", "Nov", "Dec",
)  # fmt: skip
LABELS = ("Mon", "", "Wed", "", "Fri", "", "Sun")


def activity(
    rows, entity: str, today: dt.date, color: bool = False, width: int = 2 * WEEKS + 5
) -> str:
    # Two columns a week after the 4-column label, and room for a month name
    # over the last one; a narrow terminal drops the oldest weeks.
    weeks = max(1, min(WEEKS, (width - 5) // 2))
    start = today - dt.timedelta(days=today.weekday(), weeks=weeks - 1)
    # Keyed by user id as well as name: two members may share a name.
    days: defaultdict[tuple[str, object], Counter[dt.date]] = defaultdict(Counter)
    for row in rows:
        created = (row.get("created_at") or "")[:10]
        if not created:
            continue
        day = dt.date.fromisoformat(created)
        if start <= day <= today:
            userid = row.get("userid")
            days[row.get("fullname") or f"user {userid}", userid][day] += 1
    if not days:
        return f"no {entity} created since {start}\n"

    noun = entity.rstrip("s")
    lines = [f"{entity} created per day · {start} → {today}"]
    by_total = sorted(days.items(), key=lambda a: (-a[1].total(), a[0][0]))
    for (author, _), counts in by_total:
        total, active = counts.total(), len(counts)
        heading = (
            f"{author}  {total} {noun}{'s' * (total != 1)}"
            f" · {active} active day{'s' * (active != 1)}"
        )
        lines += ["", heading, _months(start, weeks)]
        for weekday, label in enumerate(LABELS):
            cells = []
            for week in range(weeks):
                day = start + dt.timedelta(weeks=week, days=weekday)
                cells.append(" " if day > today else _cell(counts[day], color))
            lines.append(f"{label:<4}{' '.join(cells)}".rstrip())
    legend = "  ".join(
        f"{_cell(level, color)} {level}{'+' * (level == 4)}" for level in range(5)
    )
    lines += ["", f"    {legend}"]
    return "\n".join(lines) + "\n"


def _cell(count: int, color: bool) -> str:
    level = min(count, 4)
    glyph = GLYPHS[level]
    return f"\033[38;5;{PALETTE[level]}m{glyph}\033[0m" if color else glyph


def _months(start: dt.date, weeks: int) -> str:
    """Each month's name above the week that holds its first day, and the first
    week's month unless the next name starts too close to fit beside it."""
    labels = [(0, start.month)]
    for week in range(weeks):
        for weekday in range(7):
            day = start + dt.timedelta(weeks=week, days=weekday)
            if day.day == 1:
                labels.append((week, day.month))
    if len(labels) > 1 and labels[1][0] < 2:
        labels.pop(0)
    line = [" "] * (2 * weeks + 1)
    for week, month in labels:
        line[2 * week : 2 * week + 3] = MONTHS[month - 1]
    return ("    " + "".join(line)).rstrip()
