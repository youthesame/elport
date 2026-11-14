import stat
import sys

import pytest

from elport import frontmatter


@pytest.mark.parametrize(
    "text",
    [
        "---\nIntroduction\n---\nBody\n",
        "---\n- one\n- two\n---\nBody\n",
        "---\nnull\n---\nBody\n",
        "----\nBody\n",
        "---not-frontmatter\nBody\n",
        "---not-frontmatter\nkey: value\n---\nBody\n",
        "---\nkey: value\n----\nBody\n",
    ],
)
def test_non_mapping_or_non_delimiter_frontmatter_preserves_full_body(text):
    assert frontmatter.parse(text) == ({}, text)


@pytest.mark.parametrize("body", ["", "Body\n", "\nBody\n", "\n\nBody\n"])
def test_frontmatter_render_parse_round_trip_preserves_body(body):
    meta = {"title": "Test", "tags": ["one"]}

    assert frontmatter.parse(frontmatter.render(meta, body)) == (meta, body)


@pytest.mark.parametrize(
    ("yaml_value", "expected"),
    [("PCR", "PCR"), ("42", 42), ("true", True)],
)
def test_scalar_frontmatter_tag_is_normalized_to_one_item(yaml_value, expected):
    meta, body = frontmatter.parse(f"---\ntags: {yaml_value}\n---\nBody\n")

    assert meta["tags"] == [expected]
    assert body == "Body\n"


@pytest.mark.parametrize(
    "tags_yaml",
    ["tags:\n  assay: PCR", "tags: !!set {PCR: null}"],
)
def test_non_list_non_scalar_frontmatter_tags_are_rejected(tags_yaml):
    with pytest.raises(ValueError, match="tags must be a list or scalar"):
        frontmatter.parse(f"---\n{tags_yaml}\n---\nBody\n")


@pytest.mark.parametrize(
    "value",
    [
        "'../items/42'",
        "'42'",
        "0",
        "-1",
        "true",
        "5.0",
        "null",
        "[]",
        "{value: 42}",
    ],
)
def test_frontmatter_rejects_non_positive_integer_id(value):
    with pytest.raises(ValueError, match="id must be a positive integer"):
        frontmatter.parse(f"---\nid: {value}\n---\nBody\n")


def test_frontmatter_accepts_positive_integer_id():
    meta, body = frontmatter.parse("---\nid: 42\n---\nBody\n")

    assert meta["id"] == 42
    assert body == "Body\n"


def test_frontmatter_rejects_unknown_keys():
    with pytest.raises(ValueError, match="unknown front matter key"):
        frontmatter.parse("---\ncatetory: x\n---\nBody\n")


def test_frontmatter_accepts_only_allowed_keys():
    text = """---
id: 42
entity: experiments
title: Test
tags: [one]
category: 3
status: 2
profile: lab
read: team
write: owner
attachments: [raw.csv]
---
Body
"""

    meta, body = frontmatter.parse(text)

    assert set(meta) == frontmatter.ALLOWED
    assert body == "Body\n"


def test_frontmatter_rejects_duplicate_yaml_keys():
    with pytest.raises(
        ValueError, match="invalid frontmatter YAML: found duplicate key"
    ):
        frontmatter.parse("---\nid: 1\nid: 2\n---\nBody\n")


@pytest.mark.parametrize("key", ["read", "write"])
def test_frontmatter_rejects_invalid_permission_keyword(key):
    with pytest.raises(
        ValueError,
        match=(rf"{key} must be one of: owner, owner\+admin, team, account, public"),
    ):
        frontmatter.parse(f"---\n{key}: laboratory\n---\nBody\n")


@pytest.mark.parametrize(
    ("key", "value"), [("read", "[team]"), ("write", "{level: team}")]
)
def test_frontmatter_rejects_non_string_permission_value(key, value):
    with pytest.raises(
        ValueError,
        match=(rf"{key} must be one of: owner, owner\+admin, team, account, public"),
    ):
        frontmatter.parse(f"---\n{key}: {value}\n---\nBody\n")


def test_frontmatter_allows_permission_keys():
    meta, body = frontmatter.parse("---\nread: team\nwrite: owner+admin\n---\nBody\n")

    assert {"read", "write"} <= frontmatter.ALLOWED
    assert meta["read"] == "team"
    assert meta["write"] == "owner+admin"
    assert body == "Body\n"


@pytest.mark.skipif(sys.platform == "win32", reason="Windows has no POSIX file modes")
def test_atomic_write_preserves_existing_permissions(tmp_path):
    path = tmp_path / "report.md"
    path.write_text("before", encoding="utf-8")
    path.chmod(0o640)

    frontmatter.atomic_write(path, "after")

    assert path.read_text(encoding="utf-8") == "after"
    assert stat.S_IMODE(path.stat().st_mode) == 0o640


def test_atomic_write_works_without_fchmod(tmp_path, monkeypatch):
    path = tmp_path / "report.md"
    path.write_text("before", encoding="utf-8")
    monkeypatch.delattr(frontmatter.os, "fchmod", raising=False)

    frontmatter.atomic_write(path, "after")

    assert path.read_text(encoding="utf-8") == "after"
