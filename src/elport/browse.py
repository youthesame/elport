from __future__ import annotations

import json
import sys

from .frontmatter import validate_id

SCOPES = {"self": 1, "team": 2, "all": 3}
DEFAULT_LIMIT = 50


def entities(
    client,
    entity: str,
    scope: str = "self",
    limit: int = DEFAULT_LIMIT,
    offset: int = 0,
    query: str | None = None,
    as_json: bool = False,
) -> None:
    if scope not in SCOPES:
        raise ValueError("scope must be one of: self, team, all")
    if limit < 1:
        raise ValueError("limit must be a positive integer")
    if offset < 0:
        raise ValueError("offset must not be negative")
    params = {"scope": SCOPES[scope], "limit": limit, "offset": offset}
    if query:
        params["q"] = query
    rows = client.search(entity, params)

    if as_json:
        print(json.dumps(rows, indent=2, ensure_ascii=False))
    elif not rows:
        print(f"no {entity} found")
    else:
        width = max(len(str(row.get("id", ""))) for row in rows)
        for row in rows:
            date = row.get("date") or ""
            title = row.get("title") or ""
            print(f"{row.get('id', '')!s:<{width}}  {date}  {title}".rstrip())
    # The notice goes to stderr, so it reaches a --json pipeline too: a silently
    # truncated id list would make a scripted clone-everything miss entities.
    if len(rows) >= limit:
        print(
            f"showing {limit}; raise --limit or page with --offset for more",
            file=sys.stderr,
        )


def view(client, entity: str, eid: object) -> None:
    # Write, not print: the body goes to stdout byte for byte, so redirecting it
    # to a file reproduces what the server stores.
    sys.stdout.write(client.get(entity, validate_id(eid)).get("body") or "")
