import stat
from pathlib import Path

import keyring.errors
import pytest

from elab import cli, config, frontmatter, sync


class NewClient:
    def __init__(self):
        self.created = []
        self.root = "https://e.example"

    def create(self, entity, title):
        self.created.append((entity, title))
        return {"id": 42}

    def get(self, entity, eid):
        return {
            "body": "server empty\n",
            "sharelink": "https://e.example/experiments/42",
        }

    def me(self):
        return {"team": 7}


def test_logout_removes_plaintext_api_key_and_calls_keyring(tmp_path, monkeypatch):
    user = tmp_path / "config.toml"
    user.write_text(
        '[profiles.lab]\nbase_url = "https://e.example"\napi_key = "secret"\n',
        encoding="utf-8",
    )
    calls = []
    monkeypatch.setattr(config, "config_path", lambda: user)
    monkeypatch.setattr(
        config.keyring,
        "delete_password",
        lambda *args: calls.append(args),
    )

    assert config.logout("lab") is True

    assert calls == [("elab", "lab")]
    assert config._read(user)["profiles"]["lab"] == {"base_url": "https://e.example"}


def test_logout_returns_false_when_no_credentials_are_stored(tmp_path, monkeypatch):
    user = tmp_path / "config.toml"
    user.write_text(
        '[profiles.lab]\nbase_url = "https://e.example"\n', encoding="utf-8"
    )
    monkeypatch.setattr(config, "config_path", lambda: user)
    monkeypatch.setattr(
        config.keyring,
        "delete_password",
        lambda *args: (_ for _ in ()).throw(keyring.errors.PasswordDeleteError()),
    )

    assert config.logout("lab") is False
    assert config._read(user)["profiles"]["lab"] == {"base_url": "https://e.example"}


def test_logout_removes_plaintext_key_when_keyring_is_unavailable(
    tmp_path, monkeypatch
):
    user = tmp_path / "config.toml"
    user.write_text(
        '[profiles.lab]\nbase_url = "https://e.example"\napi_key = "secret"\n',
        encoding="utf-8",
    )
    monkeypatch.setattr(config, "config_path", lambda: user)
    monkeypatch.setattr(
        config.keyring,
        "delete_password",
        lambda *args: (_ for _ in ()).throw(keyring.errors.KeyringError()),
    )

    assert config.logout("lab") is True
    assert config._read(user)["profiles"]["lab"] == {"base_url": "https://e.example"}


@pytest.mark.parametrize(
    ("argv", "removed", "expected"),
    [
        (["logout", "labX"], True, "logged out profile labX\n"),
        (["logout"], False, "no stored credentials for profile default\n"),
    ],
)
def test_logout_reports_result(argv, removed, expected, monkeypatch, capsys):
    calls = []
    monkeypatch.setattr(
        cli.config,
        "logout",
        lambda name: calls.append(name) or removed,
        raising=False,
    )

    assert cli.main(argv) == 0
    assert calls == [argv[1] if len(argv) > 1 else "default"]
    assert capsys.readouterr().out == expected


def test_new_creates_remote_entity_and_local_document(tmp_path, monkeypatch, capsys):
    output = tmp_path / "report.md"
    client = NewClient()
    saved = []
    monkeypatch.setattr(cli.config, "load", lambda *args: {})
    monkeypatch.setattr(
        cli.config,
        "resolve",
        lambda *args: ("test", "https://e.example", "secret", True),
    )
    monkeypatch.setattr(cli, "_resolved_client", lambda *args: ("test", client))
    monkeypatch.setattr(cli.state, "save", lambda *args: saved.append(args))

    assert cli.main(["new", "Experiment", "-o", str(output)]) == 0

    meta, body = frontmatter.parse(output.read_text())
    assert client.created == [("experiments", "Experiment")]
    assert meta["elab_id"] == 42
    assert meta["title"] == "Experiment"
    assert meta["profile"] == "test"
    assert body == ""
    assert saved == [
        (
            "https://e.example",
            "experiments",
            "42",
            {"remote_base": "server empty\n", "local_base": "", "team": 7},
        )
    ]
    assert capsys.readouterr().out == (
        f"created experiments/42: {output}\n  → https://e.example/experiments/42\n"
    )


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
def test_frontmatter_rejects_non_positive_integer_elab_id(value):
    with pytest.raises(ValueError, match="elab_id must be a positive integer"):
        frontmatter.parse(f"---\nelab_id: {value}\n---\nBody\n")


def test_frontmatter_accepts_positive_integer_elab_id():
    meta, body = frontmatter.parse("---\nelab_id: 42\n---\nBody\n")

    assert meta["elab_id"] == 42
    assert body == "Body\n"


def test_frontmatter_rejects_unknown_keys():
    with pytest.raises(ValueError, match="unknown front matter key"):
        frontmatter.parse("---\ncatetory: x\n---\nBody\n")


def test_frontmatter_accepts_only_allowed_keys():
    text = """---
elab_id: 42
entity: experiments
title: Test
tags: [one]
category: 3
status: 2
profile: lab
read: team
write: owner
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
        frontmatter.parse("---\nelab_id: 1\nelab_id: 2\n---\nBody\n")


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


def test_new_rejects_invalid_server_id_before_followup_requests(tmp_path, monkeypatch):
    class InvalidIdClient(NewClient):
        def create(self, entity, title):
            self.created.append((entity, title))
            return {"id": "../items/42"}

        def get(self, entity, eid):
            pytest.fail("invalid id must not reach an entity request")

    output = tmp_path / "report.md"
    client = InvalidIdClient()
    monkeypatch.setattr(cli.config, "load", lambda *args: {})
    monkeypatch.setattr(cli, "_resolved_client", lambda *args: ("test", client))
    monkeypatch.setattr(
        cli.state,
        "save",
        lambda *args: pytest.fail("invalid id must not be saved to state"),
    )

    assert cli.main(["new", "Experiment", "-o", str(output)]) == 1

    meta, _ = frontmatter.parse(output.read_text(encoding="utf-8"))
    assert "elab_id" not in meta


def test_invalid_frontmatter_yaml_is_reported_as_cli_error(tmp_path, capsys):
    document = tmp_path / "report.md"
    document.write_text("---\ntags: [\n---\nBody\n", encoding="utf-8")

    assert cli.main(["status", str(document)]) == 1
    error = capsys.readouterr().err
    assert "invalid frontmatter YAML:" in error
    assert "Traceback" not in error


def test_push_missing_document_reports_friendly_error(tmp_path, monkeypatch, capsys):
    monkeypatch.chdir(tmp_path)
    monkeypatch.setattr(cli.config, "load", lambda *args: {})

    assert cli.main(["push", "no_such_doc.md"]) == 1

    error = capsys.readouterr().err
    assert "document not found: no_such_doc.md" in error
    assert "[Errno 2]" not in error


def test_status_directory_reports_friendly_error(tmp_path, monkeypatch, capsys):
    document = tmp_path / "report.md"
    document.mkdir()
    monkeypatch.setattr(cli.config, "load", lambda *args: {})

    assert cli.main(["status", str(document)]) == 1

    error = capsys.readouterr().err
    assert f"document not found: {document}" in error
    assert "[Errno 21]" not in error
    assert "Is a directory" not in error


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


def test_new_persists_created_id_before_state_initialization_failure(
    tmp_path, monkeypatch
):
    class FailingGetClient(NewClient):
        def get(self, entity, eid):
            raise RuntimeError("GET failed")

    output = tmp_path / "nested" / "report.md"
    output.parent.mkdir()
    monkeypatch.setattr(cli.config, "load", lambda *args: {})
    monkeypatch.setattr(
        cli.config,
        "resolve",
        lambda *args: ("test", "https://e.example", "secret", True),
    )
    monkeypatch.setattr(
        cli,
        "_resolved_client",
        lambda *args: ("test", FailingGetClient()),
    )
    monkeypatch.setattr(
        cli.state,
        "save",
        lambda *args: pytest.fail("state should not be saved after GET failure"),
    )

    assert cli.main(["new", "Experiment", "-o", str(output)]) == 1

    meta, body = frontmatter.parse(output.read_text(encoding="utf-8"))
    assert meta["elab_id"] == 42
    assert meta["profile"] == "test"
    assert body == ""


def test_new_rejects_missing_parent_before_remote_creation(tmp_path, monkeypatch):
    output = tmp_path / "missing" / "report.md"
    client = NewClient()
    monkeypatch.setattr(cli.config, "load", lambda *args: {})
    monkeypatch.setattr(cli, "_resolved_client", lambda *args: ("test", client))

    assert cli.main(["new", "Experiment", "-o", str(output)]) == 1
    assert client.created == []
    assert not output.exists()


def test_new_rejects_existing_output_before_remote_creation(tmp_path, monkeypatch):
    output = tmp_path / "report.md"
    output.write_bytes(b"keep me")
    client = NewClient()
    monkeypatch.setattr(cli.config, "load", lambda *args: {})
    monkeypatch.setattr(cli, "_resolved_client", lambda *args: ("test", client))

    assert cli.main(["new", "Experiment", "-o", str(output)]) == 1
    assert client.created == []
    assert output.read_bytes() == b"keep me"


def test_new_preflights_atomic_output_write_before_remote_creation(
    tmp_path, monkeypatch
):
    output = tmp_path / "report.md"
    client = NewClient()
    monkeypatch.setattr(cli.config, "load", lambda *args: {})
    monkeypatch.setattr(cli, "_resolved_client", lambda *args: ("test", client))
    monkeypatch.setattr(
        cli.frontmatter,
        "atomic_write",
        lambda *args: (_ for _ in ()).throw(OSError("output unwritable")),
    )

    assert cli.main(["new", "Experiment", "-o", str(output)]) == 1
    assert client.created == []
    assert not output.exists()


def test_target_loads_project_from_cwd_and_document_from_parent(tmp_path, monkeypatch):
    project = tmp_path / "project"
    document_dir = project / "notes" / "one"
    document_dir.mkdir(parents=True)
    document = document_dir / "report.md"
    document.write_text("body", encoding="utf-8")
    calls = []
    monkeypatch.chdir(project)
    monkeypatch.setattr(cli.config, "load", lambda *args: calls.append(args) or {})
    cli._target(document, None, None)

    assert calls == [(project, document_dir)]


@pytest.mark.parametrize("selected_profile", [None, "2026-08-08"])
def test_target_normalizes_yaml_profile_before_lazy_client_resolution(
    tmp_path, monkeypatch, selected_profile
):
    document = tmp_path / "report.md"
    document.write_text("---\nprofile: 2026-08-08\n---\nbody\n", encoding="utf-8")
    monkeypatch.delenv("ELABFTW_BASE_URL", raising=False)
    monkeypatch.delenv("ELABFTW_API_KEY", raising=False)
    monkeypatch.setattr(cli.config.keyring, "get_password", lambda *args: "secret")
    monkeypatch.setattr(
        cli.config,
        "load",
        lambda *args: {"profiles": {"2026-08-08": {"base_url": "https://e.example"}}},
    )

    _, client = cli._target(document, selected_profile, None)

    assert client.root == "https://e.example"


@pytest.mark.parametrize("source", ["frontmatter", "config"])
@pytest.mark.parametrize("value", ["users", "", None, ["items"]])
def test_target_rejects_invalid_entity_before_client_use(
    tmp_path, monkeypatch, capsys, source, value
):
    document = tmp_path / "report.md"
    meta = {"entity": value} if source == "frontmatter" else {}
    document.write_text(frontmatter.render(meta, "body"), encoding="utf-8")
    config_data = {"entity": value} if source == "config" else {}
    monkeypatch.setattr(cli.config, "load", lambda *args: config_data)
    monkeypatch.setattr(
        cli,
        "_client",
        lambda *args: pytest.fail("client should not be used"),
    )

    assert cli.main(["status", str(document)]) == 1
    assert "entity must be one of: experiments, items" in capsys.readouterr().err


def test_status_without_elab_id_does_not_resolve_credentials(
    tmp_path, monkeypatch, capsys
):
    document = tmp_path / "report.md"
    document.write_text("local body\n", encoding="utf-8")
    monkeypatch.setattr(cli.config, "load", lambda *args: {})
    monkeypatch.setattr(
        cli,
        "_client",
        lambda *args: pytest.fail("credentials should not be resolved"),
    )

    assert cli.main(["status", str(document)]) == 0
    assert "remote: no elab_id" in capsys.readouterr().out


def test_base_diff_does_not_resolve_credentials(tmp_path, monkeypatch, capsys):
    document = tmp_path / "report.md"
    document.write_text(
        "---\nelab_id: 42\nprofile: lab\n---\nchanged\n", encoding="utf-8"
    )
    monkeypatch.setattr(
        cli.config,
        "load",
        lambda *args: {"profiles": {"lab": {"base_url": "https://e.example"}}},
    )
    monkeypatch.setattr(
        cli.state,
        "load",
        lambda *args: {"local_base": "original\n"},
    )
    monkeypatch.setattr(
        cli,
        "_client",
        lambda *args: pytest.fail("credentials should not be resolved"),
    )

    assert cli.main(["diff", "--base", str(document)]) == 0
    assert "-original" in capsys.readouterr().out


def test_status_prints_local_result_when_credentials_are_unavailable(
    tmp_path, monkeypatch, capsys
):
    document = tmp_path / "report.md"
    document.write_text("---\nelab_id: 42\nprofile: lab\n---\nbody\n", encoding="utf-8")
    monkeypatch.setattr(
        cli.config,
        "load",
        lambda *args: {"profiles": {"lab": {"base_url": "https://e.example"}}},
    )
    monkeypatch.setattr(
        cli.state,
        "load",
        lambda *args: {"local_base": "body\n"},
    )
    monkeypatch.setattr(
        cli,
        "_client",
        lambda *args: (_ for _ in ()).throw(ValueError("credentials unavailable")),
    )

    assert cli.main(["status", str(document)]) == 0
    output = capsys.readouterr().out
    assert "local: clean" in output
    assert "remote: unavailable (offline?)" in output


def test_selected_profile_verify_ssl_is_forwarded(monkeypatch):
    monkeypatch.delenv("ELABFTW_BASE_URL", raising=False)
    monkeypatch.delenv("ELABFTW_API_KEY", raising=False)
    monkeypatch.setattr(cli.config.keyring, "get_password", lambda *args: "secret")

    client = cli._client(
        {
            "default_profile": "lab",
            "profiles": {"lab": {"base_url": "https://e.example", "verify_ssl": False}},
        },
        None,
        {},
    )

    assert client.verify is False


@pytest.mark.parametrize(
    "argv",
    [
        ["pull", "--force"],
        ["pull", "--dry-run"],
        ["status", "--base"],
        ["status", "--force"],
        ["diff", "--dry-run"],
    ],
)
def test_irrelevant_flags_are_rejected(argv):
    with pytest.raises(SystemExit):
        cli._parser().parse_args(argv)


@pytest.mark.parametrize("flag", ["-y", "--yes"])
def test_push_yes_is_threaded_to_sync(monkeypatch, flag):
    client = object()
    calls = []
    monkeypatch.setattr(cli, "_target", lambda *args: ({}, client))
    monkeypatch.setattr(cli, "push", lambda *args: calls.append(args))

    assert cli.main(["push", "report.md", flag]) == 0
    assert calls == [(Path("report.md"), client, {}, None, False, False, True)]


def test_comments_defaults_to_report_and_dispatches_through_target(monkeypatch):
    target_calls = []
    comment_calls = []
    client = object()
    monkeypatch.setattr(
        cli,
        "_target",
        lambda *args: target_calls.append(args) or ({"entity": "items"}, client),
    )
    monkeypatch.setattr(cli, "comments", lambda *args: comment_calls.append(args))

    assert cli.main(["comments", "--profile", "lab", "--entity", "items"]) == 0
    assert target_calls == [(Path("report.md"), "lab", "items")]
    assert comment_calls == [(Path("report.md"), client, {"entity": "items"}, "lab")]


@pytest.mark.parametrize(
    ("argv", "expected_path"),
    [
        (["comment", "hello"], Path("report.md")),
        (["comment", "note.md", "hello"], Path("note.md")),
    ],
)
def test_comment_parses_doc_first_and_dispatches(argv, expected_path, monkeypatch):
    target_calls = []
    comment_calls = []
    client = object()
    monkeypatch.setattr(
        cli,
        "_target",
        lambda *args: target_calls.append(args) or ({}, client),
    )
    monkeypatch.setattr(
        cli, "comment", lambda *args, **kwargs: comment_calls.append((args, kwargs))
    )

    assert cli.main(argv) == 0
    assert target_calls == [(expected_path, None, None)]
    assert comment_calls == [((expected_path, client, {}, None), {"text": "hello"})]


def test_comment_text_is_required():
    with pytest.raises(SystemExit):
        cli._parser().parse_args(["comment"])


@pytest.mark.parametrize(
    ("extra_args", "resolved"), [([], False), (["--resolved"], True)]
)
def test_merge_loads_config_without_creating_client(
    tmp_path, monkeypatch, extra_args, resolved
):
    monkeypatch.chdir(tmp_path)
    Path("report.md").write_text("body\n", encoding="utf-8")
    data = {"profiles": {"lab": {"base_url": "https://e.example"}}}
    load_calls = []
    calls = []
    monkeypatch.setattr(
        cli.config,
        "load",
        lambda *args: load_calls.append(args) or data,
    )
    monkeypatch.setattr(cli, "merge", lambda *args: calls.append(args))
    monkeypatch.setattr(
        cli,
        "_client",
        lambda *args: pytest.fail("merge must not create a client"),
    )

    assert cli.main(["merge", "--profile", "lab", *extra_args]) == 0
    assert load_calls == [(tmp_path, Path("."))]
    assert calls == [(Path("report.md"), data, "lab", resolved)]


def test_merge_threads_cli_entity_override(tmp_path, monkeypatch):
    document = tmp_path / "report.md"
    document.write_text("---\nelab_id: 42\n---\nbody\n", encoding="utf-8")
    data = {}
    calls = []
    monkeypatch.setattr(cli.config, "load", lambda *args: data)
    monkeypatch.setattr(cli, "merge", lambda *args: calls.append(args))

    assert cli.main(["merge", str(document), "--entity", "items"]) == 0
    assert data["entity"] == "items"
    assert calls == [(document, data, None, False)]


def test_merge_rejects_entity_mismatch_as_cli_error(tmp_path, monkeypatch, capsys):
    document = tmp_path / "report.md"
    document.write_text(
        "---\nelab_id: 42\nentity: items\n---\nbody\n", encoding="utf-8"
    )
    monkeypatch.setattr(cli.config, "load", lambda *args: {})
    monkeypatch.setattr(
        cli,
        "merge",
        lambda *args: pytest.fail("merge should not be called"),
    )

    assert cli.main(["merge", str(document), "--entity", "experiments"]) == 1
    error = capsys.readouterr().err
    assert "entity mismatch between frontmatter and CLI" in error
    assert "Traceback" not in error


def test_merge_missing_sidecars_is_reported_as_cli_error(tmp_path, capsys):
    document = tmp_path / "note.md"
    document.write_text("local\n", encoding="utf-8")

    assert cli.main(["merge", str(document)]) == 1
    error = capsys.readouterr().err
    assert "no conflict to merge; run push/pull first" in error
    assert "note.base.md" in error
    assert "note.remote.md" in error
    assert "Traceback" not in error


def test_merge_without_git_is_reported_as_cli_error(tmp_path, monkeypatch, capsys):
    document = tmp_path / "note.md"
    base = tmp_path / "note.base.md"
    remote = tmp_path / "note.remote.md"
    document.write_text("local\n", encoding="utf-8")
    base.write_text("base\n", encoding="utf-8")
    remote.write_text("remote\n", encoding="utf-8")
    monkeypatch.setattr(sync.shutil, "which", lambda command: None)

    assert cli.main(["merge", str(document)]) == 1
    error = capsys.readouterr().err
    assert str(base) in error
    assert str(remote) in error
    assert "by hand" in error
    assert "elab merge --resolved" in error
    assert "Traceback" not in error
    assert document.read_text(encoding="utf-8") == "local\n"


def test_whoami_prints_user_and_active_team(monkeypatch, capsys):
    class IdentityClient:
        def me(self):
            return {
                "fullname": "Ada Lovelace",
                "team": 7,
                "teams": [{"id": 7, "name": "Lab A"}],
            }

    monkeypatch.setattr(cli.config, "load", lambda *args: {})
    monkeypatch.setattr(cli, "_client", lambda *args: IdentityClient())

    assert cli.main(["whoami"]) == 0
    assert capsys.readouterr().out == "user: Ada Lovelace\nactive team: Lab A (7)\n"
