from __future__ import annotations

import os
import re
import stat
import tempfile
from pathlib import Path

import yaml
import yaml.constructor
import yaml.resolver

PERMISSION_LEVELS = {
    "owner": 10,
    "owner+admin": 20,
    "team": 30,
    "account": 40,
    "public": 50,
}
ALLOWED = {
    "elab_id",
    "entity",
    "title",
    "tags",
    "category",
    "status",
    "profile",
    "read",
    "write",
}


class _StrictLoader(yaml.SafeLoader):
    pass


def _forbid_duplicate_keys(loader, node):
    mapping = {}
    for key_node, value_node in node.value:
        key = loader.construct_object(key_node, deep=True)
        if key in mapping:
            raise yaml.constructor.ConstructorError(
                None, None, f"found duplicate key {key!r}", key_node.start_mark
            )
        mapping[key] = loader.construct_object(value_node, deep=True)
    return mapping


_StrictLoader.add_constructor(
    yaml.resolver.BaseResolver.DEFAULT_MAPPING_TAG, _forbid_duplicate_keys
)


def validate_elab_id(value: object) -> int:
    if isinstance(value, bool) or not isinstance(value, int) or value <= 0:
        raise ValueError("elab_id must be a positive integer")
    return value


def parse_server_elab_id(value: object) -> int:
    if not isinstance(value, (int, str)) or not str(value).isdigit():
        raise RuntimeError("server did not return a valid entity id")
    try:
        return validate_elab_id(int(value))
    except ValueError as error:
        raise RuntimeError("server did not return a valid entity id") from error


def parse(text: str) -> tuple[dict, str]:
    delimiter = re.compile(r"(?m)^---[ \t]*(?:\r?\n|$)")
    opener = delimiter.match(text)
    if opener is None:
        return {}, text
    closer = delimiter.search(text, opener.end())
    if closer is None:
        return {}, text
    try:
        data = yaml.load(text[opener.end() : closer.start()], Loader=_StrictLoader)
    except yaml.YAMLError as error:
        raise ValueError(f"invalid frontmatter YAML: {error}") from error
    if not isinstance(data, dict):
        return {}, text
    unknown = sorted(set(data) - ALLOWED)
    if unknown:
        raise ValueError(f"unknown front matter key(s): {', '.join(unknown)}")
    if "elab_id" in data:
        data["elab_id"] = validate_elab_id(data["elab_id"])
    for key in ("title", "profile"):
        if key in data and data[key] is not None:
            data[key] = str(data[key])
    for key in ("read", "write"):
        if key in data and (
            not isinstance(data[key], str) or data[key] not in PERMISSION_LEVELS
        ):
            valid = ", ".join(PERMISSION_LEVELS)
            raise ValueError(f"{key} must be one of: {valid}")
    if "tags" in data:
        tags = data["tags"]
        if tags is None:
            data["tags"] = []
        elif not isinstance(tags, list):
            if isinstance(tags, (dict, set)):
                raise ValueError("tags must be a list or scalar")
            data["tags"] = [tags]
    body = text[closer.end() :]
    if body.startswith("\r\n"):
        body = body[2:]
    elif body.startswith(("\r", "\n")):
        body = body[1:]
    return data, body


def render(meta: dict, body: str) -> str:
    return (
        "---\n"
        + yaml.safe_dump(meta, allow_unicode=True, sort_keys=False).rstrip()
        + "\n---\n\n"
        + body
    )


def atomic_write(path: Path, text: str) -> None:
    mode = stat.S_IMODE(path.stat().st_mode) if path.exists() else None
    fd, name = tempfile.mkstemp(prefix=f".{path.name}.", dir=path.parent)
    try:
        with os.fdopen(fd, "w", encoding="utf-8", newline="") as f:
            f.write(text)
            if mode is not None and hasattr(os, "fchmod"):
                os.fchmod(f.fileno(), mode)
        os.replace(name, path)
    finally:
        if os.path.exists(name):
            os.unlink(name)
