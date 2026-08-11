from __future__ import annotations

import pytest
from _sync_harness import FakeClient, saved_state, write_doc

from elport import frontmatter, sync


def test_push_folds_declared_permission_bases_into_payload(
    tmp_path, monkeypatch, configured, capsys
):
    doc = tmp_path / "report.md"
    write_doc(doc, "local", read="team", write="owner")
    client = FakeClient(
        remote_doc={
            "body": "remote",
            "tags": "",
            "canread_base": 20,
            "canwrite_base": 20,
            "canread": '{"teams":[3],"users":[],"teamgroups":[]}',
            "canwrite": '{"teams":[],"users":[],"teamgroups":[]}',
        }
    )
    monkeypatch.setattr(sync.state, "load", lambda *args: saved_state())
    monkeypatch.setattr(sync.state, "save", lambda *args: None)

    sync.push(doc, client, {})

    assert client.saved_payload is not None
    assert client.saved_payload["canread_base"] == 30
    assert client.saved_payload["canwrite_base"] == 10
    assert "canread" not in client.saved_payload
    assert "canwrite" not in client.saved_payload
    assert client.calls.count("patch") == 2
    assert "  permissions: read=team write=owner\n" in capsys.readouterr().out


def test_push_applies_narrowing_before_uploads_even_if_later_conflict_aborts(
    tmp_path, monkeypatch, configured
):
    doc = tmp_path / "report.md"
    (tmp_path / "data.csv").write_text("new", encoding="utf-8")
    write_doc(doc, "[data](data.csv)", read="owner", write="owner+admin")
    client = FakeClient(
        gets=[
            {
                "body": "remote",
                "canread_base": 30,
                "canwrite_base": 30,
            },
            {"body": "web edit"},
        ]
    )
    monkeypatch.setattr(sync.state, "load", lambda *args: saved_state())

    with pytest.raises(RuntimeError, match="after uploads"):
        sync.push(doc, client, {})

    assert client.saved_payload == {"canread_base": 10, "canwrite_base": 20}
    assert "canread" not in client.saved_payload
    assert "canwrite" not in client.saved_payload
    assert client.calls.index("patch") < client.calls.index("upload")
    assert client.calls.count("patch") == 1


def test_push_keeps_widening_in_final_patch_after_uploads(
    tmp_path, monkeypatch, configured
):
    doc = tmp_path / "report.md"
    (tmp_path / "data.csv").write_text("new", encoding="utf-8")
    write_doc(doc, "[data](data.csv)", read="public")
    client = FakeClient(remote_doc={"body": "remote", "canread_base": 30, "tags": ""})
    monkeypatch.setattr(sync.state, "load", lambda *args: saved_state())
    monkeypatch.setattr(sync.state, "save", lambda *args: None)

    sync.push(doc, client, {}, assume_yes=True)

    assert client.calls.count("patch") == 1
    assert client.calls.index("upload") < client.calls.index("patch")
    assert client.saved_payload is not None
    assert client.saved_payload["canread_base"] == 50
    assert "canread" not in client.saved_payload


def test_new_entity_applies_narrowing_after_create_before_uploads(
    tmp_path, monkeypatch, configured
):
    doc = tmp_path / "report.md"
    (tmp_path / "data.csv").write_text("new", encoding="utf-8")
    doc.write_text(
        frontmatter.render({"title": "Test", "read": "owner"}, "[data](data.csv)"),
        encoding="utf-8",
    )
    client = FakeClient(identity={"default_read_base": 30})
    monkeypatch.setattr(sync.state, "save", lambda *args: None)

    sync.push(doc, client, {})

    assert client.calls.index("create") < client.calls.index("patch")
    assert client.calls.index("patch") < client.calls.index("upload")
    assert client.calls.count("patch") == 2
    assert client.saved_payload is not None
    assert client.saved_payload["canread_base"] == 10
    assert "canread" not in client.saved_payload


def test_push_omits_undeclared_permissions(tmp_path, monkeypatch, configured):
    doc = tmp_path / "report.md"
    write_doc(doc, "local")
    client = FakeClient(
        remote_doc={
            "body": "remote",
            "canread_base": 50,
            "canwrite_base": 40,
            "canread": '{"teams":[3],"users":[],"teamgroups":[]}',
            "canwrite": '{"teams":[],"users":[4],"teamgroups":[]}',
        }
    )
    monkeypatch.setattr(sync.state, "load", lambda *args: saved_state())
    monkeypatch.setattr(sync.state, "save", lambda *args: None)

    sync.push(doc, client, {})

    assert client.saved_payload is not None
    assert (
        not {
            "canread_base",
            "canwrite_base",
            "canread",
            "canwrite",
        }
        & client.saved_payload.keys()
    )


@pytest.mark.parametrize("field", ["read", "write"])
@pytest.mark.parametrize("immutable_key", ["is_immutable", "base_is_immutable"])
def test_locked_declared_permission_at_current_level_is_omitted(
    tmp_path, monkeypatch, configured, field, immutable_key
):
    doc = tmp_path / "report.md"
    write_doc(doc, "updated", **{field: "team"})
    client = FakeClient(
        remote_doc={
            "body": "remote",
            f"can{field}_base": 30,
            f"can{field}_{immutable_key}": True,
        }
    )
    monkeypatch.setattr(sync.state, "load", lambda *args: saved_state())
    monkeypatch.setattr(sync.state, "save", lambda *args: None)

    sync.push(doc, client, {})

    assert client.saved_payload is not None
    assert client.saved_payload["body"] == "updated"
    assert f"can{field}_base" not in client.saved_payload
    assert f"can{field}" not in client.saved_payload
    assert client.calls.count("patch") == 1


@pytest.mark.parametrize("field", ["read", "write"])
@pytest.mark.parametrize("immutable_key", ["is_immutable", "base_is_immutable"])
def test_locked_declared_permission_change_stops_before_upload_or_patch(
    tmp_path, monkeypatch, configured, field, immutable_key
):
    doc = tmp_path / "report.md"
    attachment = tmp_path / "data.csv"
    attachment.write_text("new", encoding="utf-8")
    write_doc(doc, "[data](data.csv)", **{field: "team"})
    client = FakeClient(
        remote_doc={
            "body": "remote",
            f"can{field}_base": 20,
            f"can{field}_{immutable_key}": True,
        }
    )
    monkeypatch.setattr(sync.state, "load", lambda *args: saved_state())
    monkeypatch.setattr(
        sync.state, "save", lambda *args: pytest.fail("state must not change")
    )

    with pytest.raises(
        RuntimeError,
        match=rf"^{field} permission is locked on this entity \(admin-set\); "
        rf"remove '{field}:' or ask an admin$",
    ):
        sync.push(doc, client, {})

    assert client.calls == ["me", "get"]
    assert client.saved_payload is None


@pytest.mark.parametrize("field", ["read", "write"])
def test_non_owner_cannot_narrow_existing_entity_to_owner(
    tmp_path, monkeypatch, configured, field
):
    doc = tmp_path / "report.md"
    (tmp_path / "data.csv").write_text("new", encoding="utf-8")
    write_doc(doc, "[data](data.csv)", **{field: "owner"})
    client = FakeClient(
        remote_doc={
            "body": "remote",
            "userid": 2,
            f"can{field}_base": 30,
        },
        identity={"userid": 1},
    )
    monkeypatch.setattr(sync.state, "load", lambda *args: saved_state())

    with pytest.raises(
        RuntimeError,
        match=rf"^{field}: owner would revoke your own access "
        rf"\(you are not the entity owner\); use 'team' or ask the owner$",
    ):
        sync.push(doc, client, {})

    assert client.calls == ["me", "get"]
    assert "patch" not in client.calls
    assert "upload" not in client.calls


@pytest.mark.parametrize("field", ["read", "write"])
def test_non_owner_can_retain_existing_owner_base(
    tmp_path, monkeypatch, configured, field
):
    doc = tmp_path / "report.md"
    write_doc(doc, "updated", **{field: "owner"})
    client = FakeClient(
        remote_doc={
            "body": "remote",
            "userid": 2,
            f"can{field}_base": 10,
            f"can{field}": '{"teams":[],"users":[1],"teamgroups":[]}',
        },
        identity={"userid": 1},
    )
    monkeypatch.setattr(sync.state, "load", lambda *args: saved_state())
    monkeypatch.setattr(sync.state, "save", lambda *args: None)

    sync.push(doc, client, {})

    assert client.saved_payload is not None
    assert client.saved_payload["body"] == "updated"
    assert f"can{field}_base" not in client.saved_payload
    assert f"can{field}" not in client.saved_payload
    assert client.calls.count("patch") == 1


@pytest.mark.parametrize("field", ["read", "write"])
def test_owner_can_narrow_existing_entity_to_owner(
    tmp_path, monkeypatch, configured, field
):
    doc = tmp_path / "report.md"
    write_doc(doc, "local", **{field: "owner"})
    client = FakeClient(
        remote_doc={
            "body": "remote",
            "userid": 1,
            f"can{field}_base": 30,
        },
        identity={"userid": 1},
    )
    monkeypatch.setattr(sync.state, "load", lambda *args: saved_state())
    monkeypatch.setattr(sync.state, "save", lambda *args: None)

    sync.push(doc, client, {})

    assert client.saved_payload is not None
    assert client.saved_payload[f"can{field}_base"] == 10
    assert f"can{field}" not in client.saved_payload
    assert client.calls.count("patch") == 2


@pytest.mark.parametrize("field", ["read", "write"])
def test_non_admin_cannot_narrow_existing_entity_to_owner_admin(
    tmp_path, monkeypatch, configured, field
):
    doc = tmp_path / "report.md"
    (tmp_path / "data.csv").write_text("new", encoding="utf-8")
    write_doc(doc, "[data](data.csv)", **{field: "owner+admin"})
    client = FakeClient(
        remote_doc={
            "body": "remote",
            "team": 7,
            "userid": 2,
            f"can{field}_base": 30,
        },
        identity={
            "userid": 1,
            "teams": [
                {"id": 7, "is_admin": False},
                {"id": 8, "is_admin": True},
            ],
        },
    )
    monkeypatch.setattr(sync.state, "load", lambda *args: saved_state())

    with pytest.raises(
        RuntimeError,
        match=rf"^{field}: owner\+admin would revoke your own access "
        rf"\(you are not an owner or admin of this team\); "
        rf"use 'team' or ask an admin$",
    ):
        sync.push(doc, client, {})

    assert client.calls == ["me", "get"]
    assert "patch" not in client.calls
    assert "upload" not in client.calls


@pytest.mark.parametrize("field", ["read", "write"])
@pytest.mark.parametrize(
    ("remote_userid", "identity", "current", "permission_changes"),
    [
        pytest.param(
            2,
            {"userid": 1, "teams": [{"id": 7, "is_admin": True}]},
            30,
            True,
            id="team-admin",
        ),
        pytest.param(
            1,
            {"userid": 1, "teams": []},
            30,
            True,
            id="owner",
        ),
        pytest.param(
            2,
            {"userid": 1, "teams": [{"id": 7, "is_admin": False}]},
            20,
            False,
            id="no-op",
        ),
    ],
)
def test_owner_admin_narrowing_allowed_for_authorized_or_unchanged_base(
    tmp_path,
    monkeypatch,
    configured,
    field,
    remote_userid,
    identity,
    current,
    permission_changes,
):
    doc = tmp_path / "report.md"
    write_doc(doc, "updated", **{field: "owner+admin"})
    client = FakeClient(
        remote_doc={
            "body": "remote",
            "team": 7,
            "userid": remote_userid,
            f"can{field}_base": current,
        },
        identity=identity,
    )
    monkeypatch.setattr(sync.state, "load", lambda *args: saved_state())
    monkeypatch.setattr(sync.state, "save", lambda *args: None)

    sync.push(doc, client, {})

    assert client.saved_payload is not None
    assert client.saved_payload["body"] == "updated"
    assert (f"can{field}_base" in client.saved_payload) is permission_changes
    if permission_changes:
        assert client.saved_payload[f"can{field}_base"] == 20
    assert f"can{field}" not in client.saved_payload
    assert client.calls.count("patch") == (2 if permission_changes else 1)


def test_non_owner_can_narrow_existing_entity_read_to_team(
    tmp_path, monkeypatch, configured
):
    doc = tmp_path / "report.md"
    write_doc(doc, "local", read="team")
    client = FakeClient(
        remote_doc={"body": "remote", "userid": 2, "canread_base": 40},
        identity={"userid": 1},
    )
    monkeypatch.setattr(sync.state, "load", lambda *args: saved_state())
    monkeypatch.setattr(sync.state, "save", lambda *args: None)

    sync.push(doc, client, {})

    assert client.saved_payload is not None
    assert client.saved_payload["canread_base"] == 30
    assert "canread" not in client.saved_payload


def test_permission_widening_interactive_yes_applies_once(
    tmp_path, monkeypatch, configured
):
    doc = tmp_path / "report.md"
    write_doc(doc, "local", read="public", write="account")
    client = FakeClient(
        remote_doc={"body": "remote", "canread_base": 30, "canwrite_base": 20}
    )
    prompts = []
    monkeypatch.setattr(sync.state, "load", lambda *args: saved_state())
    monkeypatch.setattr(sync.state, "save", lambda *args: None)
    monkeypatch.setattr(sync.sys.stdin, "isatty", lambda: True)
    monkeypatch.setattr(
        "builtins.input", lambda prompt: prompts.append(prompt) or "YES"
    )

    sync.push(doc, client, {})

    assert len(prompts) == 1
    assert "read → public" in prompts[0]
    assert "anonymous" in prompts[0]
    assert "no login required" in prompts[0]
    assert "write → account" in prompts[0]
    assert client.saved_payload is not None
    assert client.saved_payload["canread_base"] == 50
    assert client.saved_payload["canwrite_base"] == 40


def test_permission_widening_interactive_no_stops_before_uploads(
    tmp_path, monkeypatch, configured
):
    doc = tmp_path / "report.md"
    (tmp_path / "data.csv").write_text("new", encoding="utf-8")
    write_doc(doc, "[data](data.csv)", read="account")
    client = FakeClient(remote_doc={"body": "remote", "canread_base": 30})
    monkeypatch.setattr(sync.state, "load", lambda *args: saved_state())
    monkeypatch.setattr(sync.sys.stdin, "isatty", lambda: True)
    monkeypatch.setattr("builtins.input", lambda prompt: "n")
    monkeypatch.setattr(
        sync,
        "_confirm_large_uploads",
        lambda paths: pytest.fail("large-upload confirmation must run later"),
    )

    with pytest.raises(RuntimeError, match="permission widening cancelled"):
        sync.push(doc, client, {})

    assert "patch" not in client.calls
    assert "upload" not in client.calls


def test_permission_widening_noninteractive_requires_yes(
    tmp_path, monkeypatch, configured
):
    doc = tmp_path / "report.md"
    write_doc(doc, "local", write="account")
    client = FakeClient(remote_doc={"body": "remote", "canwrite_base": 20})
    monkeypatch.setattr(sync.state, "load", lambda *args: saved_state())
    monkeypatch.setattr(sync.sys.stdin, "isatty", lambda: False)
    monkeypatch.setattr(
        "builtins.input", lambda prompt: pytest.fail("input must not be called")
    )

    with pytest.raises(RuntimeError, match=r"re-run with --yes"):
        sync.push(doc, client, {})

    assert "patch" not in client.calls
    assert "upload" not in client.calls


def test_permission_widening_yes_applies_without_prompt(
    tmp_path, monkeypatch, configured
):
    doc = tmp_path / "report.md"
    write_doc(doc, "local", read="public")
    client = FakeClient(remote_doc={"body": "remote", "canread_base": 30})
    monkeypatch.setattr(sync.state, "load", lambda *args: saved_state())
    monkeypatch.setattr(sync.state, "save", lambda *args: None)
    monkeypatch.setattr(
        "builtins.input", lambda prompt: pytest.fail("input must not be called")
    )

    sync.push(doc, client, {}, assume_yes=True)

    assert client.saved_payload is not None
    assert client.saved_payload["canread_base"] == 50


def test_permission_already_at_target_does_not_prompt_noninteractive(
    tmp_path, monkeypatch, configured
):
    doc = tmp_path / "report.md"
    write_doc(doc, "local", read="public", write="account")
    client = FakeClient(
        remote_doc={"body": "remote", "canread_base": 50, "canwrite_base": 40}
    )
    monkeypatch.setattr(sync.state, "load", lambda *args: saved_state())
    monkeypatch.setattr(sync.state, "save", lambda *args: None)
    monkeypatch.setattr(sync.sys.stdin, "isatty", lambda: False)
    monkeypatch.setattr(
        "builtins.input", lambda prompt: pytest.fail("input must not be called")
    )

    sync.push(doc, client, {})

    assert client.saved_payload is not None
    assert "canread_base" not in client.saved_payload
    assert "canwrite_base" not in client.saved_payload


@pytest.mark.parametrize(
    ("field", "keyword", "base"),
    [
        ("read", "owner", 10),
        ("write", "owner+admin", 20),
    ],
)
@pytest.mark.parametrize(
    "grants",
    [
        '{"teams":[3],"users":[],"teamgroups":[]}',
        '{"teams":[],"users":[4],"teamgroups":[]}',
        '{"teams":[],"users":[],"teamgroups":[5]}',
    ],
)
def test_permission_narrowing_warns_when_individual_grants_remain(
    tmp_path, monkeypatch, configured, capsys, field, keyword, base, grants
):
    doc = tmp_path / "report.md"
    write_doc(doc, "local", **{field: keyword})
    client = FakeClient(
        remote_doc={
            "body": "remote",
            f"can{field}_base": 30,
            f"can{field}": grants,
        }
    )
    monkeypatch.setattr(sync.state, "load", lambda *args: saved_state())
    monkeypatch.setattr(sync.state, "save", lambda *args: None)

    sync.push(doc, client, {})

    error = capsys.readouterr().err
    assert f"warning: report.md declares {field}: {keyword}" in error
    assert "individual grants remain" in error
    assert "effective access is wider" in error
    assert client.saved_payload is not None
    assert client.saved_payload[f"can{field}_base"] == base


@pytest.mark.parametrize("field", ["read", "write"])
def test_permission_narrowing_with_empty_grants_does_not_warn(
    tmp_path, monkeypatch, configured, capsys, field
):
    doc = tmp_path / "report.md"
    write_doc(doc, "local", **{field: "owner"})
    client = FakeClient(
        remote_doc={
            "body": "remote",
            f"can{field}_base": 30,
            f"can{field}": '{"teams":[],"users":[],"teamgroups":[]}',
        }
    )
    monkeypatch.setattr(sync.state, "load", lambda *args: saved_state())
    monkeypatch.setattr(sync.state, "save", lambda *args: None)

    sync.push(doc, client, {})

    assert "individual grants remain" not in capsys.readouterr().err


def test_permission_dry_run_prints_plan_without_prompt_or_patch(
    tmp_path, monkeypatch, configured, capsys
):
    doc = tmp_path / "report.md"
    write_doc(doc, "local", read="public", write="owner")
    client = FakeClient(
        remote_doc={"body": "remote", "canread_base": 30, "canwrite_base": 30}
    )
    monkeypatch.setattr(sync.state, "load", lambda *args: saved_state())
    monkeypatch.setattr(sync.sys.stdin, "isatty", lambda: False)
    monkeypatch.setattr(
        "builtins.input", lambda prompt: pytest.fail("input must not be called")
    )

    sync.push(doc, client, {}, dry_run=True)

    assert "permissions: read→public, write→owner" in capsys.readouterr().out
    assert "patch" not in client.calls


def test_new_entity_widening_uses_identity_default(tmp_path, monkeypatch, configured):
    doc = tmp_path / "report.md"
    doc.write_text(
        frontmatter.render({"title": "Test", "read": "account"}, "local"),
        encoding="utf-8",
    )
    client = FakeClient(identity={"default_read_base": 30})
    monkeypatch.setattr(sync.sys.stdin, "isatty", lambda: False)

    with pytest.raises(RuntimeError, match=r"re-run with --yes"):
        sync.push(doc, client, {})

    assert "create" not in client.calls
    assert "patch" not in client.calls
