import errno
import io
import re
import stat
import sys
from importlib import resources
from pathlib import Path

import keyring.errors
import pytest

from elport import cli, config, frontmatter


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

    assert calls == [("elport", "lab")]
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


def test_output_is_utf8_whatever_the_locale(monkeypatch):
    # Windows encodes a piped stdout (an agent, a script) with the locale code
    # page, which cannot hold most titles, names or emoji.
    raw = io.BytesIO()
    monkeypatch.setattr(
        sys, "stdout", io.TextIOWrapper(raw, encoding="cp1252", newline="\n")
    )
    monkeypatch.setattr(cli.config, "logout", lambda name: True, raising=False)

    assert cli.main(["logout", "実験🧪"]) == 0

    sys.stdout.flush()
    assert raw.getvalue().decode("utf-8") == "logged out profile 実験🧪\n"


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


def test_status_of_a_directory_without_documents_is_a_friendly_error(
    tmp_path, monkeypatch, capsys
):
    document = tmp_path / "report.md"
    document.mkdir()
    monkeypatch.setattr(cli.config, "load", lambda *args: {})

    assert cli.main(["status", str(document)]) == 1

    error = capsys.readouterr().err
    assert f"no elport documents under {document}" in error
    assert "[Errno 21]" not in error
    assert "Is a directory" not in error


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


def test_status_without_id_does_not_resolve_credentials(tmp_path, monkeypatch, capsys):
    document = tmp_path / "report.md"
    document.write_text("local body\n", encoding="utf-8")
    monkeypatch.setattr(cli.config, "load", lambda *args: {})
    monkeypatch.setattr(
        cli,
        "_client",
        lambda *args: pytest.fail("credentials should not be resolved"),
    )

    assert cli.main(["status", str(document)]) == 0
    assert "remote: no id" in capsys.readouterr().out


def test_base_diff_does_not_resolve_credentials(tmp_path, monkeypatch, capsys):
    document = tmp_path / "report.md"
    document.write_text("---\nid: 42\nprofile: lab\n---\nchanged\n", encoding="utf-8")
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


def test_status_reports_credentials_unavailable_as_cli_error(
    tmp_path, monkeypatch, capsys
):
    document = tmp_path / "report.md"
    document.write_text("---\nid: 42\nprofile: lab\n---\nbody\n", encoding="utf-8")
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

    assert cli.main(["status", str(document)]) == 1
    captured = capsys.readouterr()
    assert "local: clean" in captured.out
    assert "remote: unavailable (offline?)" not in captured.out
    assert captured.err == "credentials unavailable\n"


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


def test_comments_takes_an_entity_id_without_a_local_document(
    tmp_path, monkeypatch, capsys
):
    monkeypatch.chdir(tmp_path)
    calls = []

    class Client:
        def comments(self, entity, eid):
            calls.append((entity, eid))
            return []

    monkeypatch.setattr(config, "load", lambda *args: {})
    monkeypatch.setattr(
        cli, "_resolved_client", lambda data, profile, meta: ("test", Client())
    )

    assert cli.main(["comments", "357", "--entity", "items"]) == 0

    assert calls == [("items", 357)]
    assert capsys.readouterr().out == "no comments\n"
    assert list(tmp_path.iterdir()) == []


def test_comments_accepts_a_matching_entity_url_and_rejects_other_servers(
    tmp_path, monkeypatch, capsys
):
    monkeypatch.chdir(tmp_path)
    calls = []

    class Client:
        root = "https://e.example"

        def comments(self, entity, eid):
            calls.append((entity, eid))
            return []

    monkeypatch.setattr(config, "load", lambda *args: {})
    monkeypatch.setattr(
        cli, "_resolved_client", lambda data, profile, meta: ("test", Client())
    )

    assert (
        cli.main(["comments", "https://e.example/database.php?mode=view&id=357"]) == 0
    )
    assert calls == [("items", 357)]
    assert capsys.readouterr().out == "no comments\n"

    assert (
        cli.main(["comments", "https://other.example/database.php?mode=view&id=357"])
        == 1
    )
    assert calls == [("items", 357)]
    assert "does not match" in capsys.readouterr().err

    for bad in (
        "//u:secretpass@e.example/experiments.php?mode=view&id=357",
        " //u:secretpass@e.example/experiments.php?mode=view&id=357",
    ):
        assert cli.main(["comments", bad]) == 1
        assert calls == [("items", 357)]
        err = capsys.readouterr().err
        assert err != "" and "secretpass" not in err


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


def test_comment_posts_by_id_or_matching_url_without_a_local_document(
    tmp_path, monkeypatch, capsys
):
    monkeypatch.chdir(tmp_path)
    posts = []

    class Client:
        root = "https://e.example"

        def add_comment(self, entity, eid, text):
            posts.append((entity, eid, text))

    monkeypatch.setattr(config, "load", lambda *args: {})
    monkeypatch.setattr(
        cli, "_resolved_client", lambda data, profile, meta: ("test", Client())
    )

    assert cli.main(["comment", "357", "looks good", "--entity", "items"]) == 0
    assert (
        cli.main(
            [
                "comment",
                "https://e.example/database.php?mode=view&id=42",
                "checked",
            ]
        )
        == 0
    )

    assert posts == [("items", 357, "looks good"), ("items", 42, "checked")]
    assert capsys.readouterr().out == "commented on items/357\ncommented on items/42\n"
    assert list(tmp_path.iterdir()) == []


@pytest.mark.parametrize(
    "argv",
    [
        ["comment", "0", "hi"],
        ["comment", "357", "   "],
        ["comment", "https://other.example/experiments.php?mode=view&id=1", "hi"],
        [
            "comment",
            "https://e.example/database.php?mode=view&id=1",
            "hi",
            "--entity",
            "experiments",
        ],
        ["comment", "//u:secretpass@e.example/experiments.php?mode=view&id=1", "hi"],
        ["comment", " //u:secretpass@e.example/experiments.php?mode=view&id=1", "hi"],
        [
            "comment",
            "https:/u:secretpass@e.example/experiments.php?mode=view&id=1",
            "hi",
        ],
        [
            "comment",
            "http:/u:secretpass@e.example/experiments.php?mode=view&id=1",
            "hi",
        ],
    ],
)
def test_comment_rejects_invalid_remote_targets_before_posting(
    tmp_path, monkeypatch, capsys, argv
):
    monkeypatch.chdir(tmp_path)
    posts = []

    class Client:
        root = "https://e.example"

        def add_comment(self, entity, eid, text):
            posts.append((entity, eid, text))

    monkeypatch.setattr(config, "load", lambda *args: {})
    monkeypatch.setattr(
        cli, "_resolved_client", lambda data, profile, meta: ("test", Client())
    )

    assert cli.main(argv) == 1

    assert posts == []
    err = capsys.readouterr().err
    assert err != "" and "secretpass" not in err


def test_comment_rejects_blank_text_before_resolving_client(monkeypatch, capsys):
    resolved = []
    monkeypatch.setattr(
        cli, "_resolved_client", lambda *args: resolved.append(args) or ("test", None)
    )

    assert cli.main(["comment", "357", "   "]) == 1

    assert resolved == []
    assert capsys.readouterr().err == "comment text is empty\n"


@pytest.mark.parametrize("name", ["357", "http:doc.md"])
def test_comment_prefers_existing_local_document_over_remote_target(
    tmp_path, monkeypatch, name
):
    monkeypatch.chdir(tmp_path)
    if name == "357":
        (tmp_path / name).write_text(
            "---\nid: 42\nentity: items\n---\n\nbody\n", encoding="utf-8"
        )
    else:
        # Windows forbids colons in real filenames; simulate a POSIX file.
        class _RegStat:
            st_mode = stat.S_IFREG

        monkeypatch.setattr(
            Path,
            "stat",
            lambda self, *args, **kwargs: (
                _RegStat()
                if self == Path(name)
                else (_ for _ in ()).throw(FileNotFoundError())
            ),
        )
    comment_calls = []
    client = object()
    monkeypatch.setattr(cli, "_target", lambda *args: ({}, client))
    monkeypatch.setattr(
        cli, "comment", lambda *args, **kwargs: comment_calls.append((args, kwargs))
    )

    assert cli.main(["comment", name, "hello"]) == 0
    assert comment_calls == [((Path(name), client, {}, None), {"text": "hello"})]


@pytest.mark.parametrize(
    ("target", "error"),
    [
        (
            "https:/u:secretpass@e.example/experiments.php?mode=view&id=1",
            OSError(errno.ENAMETOOLONG, "File name too long", "secretpass"),
        ),
        (
            "357",
            PermissionError(errno.EACCES, "Permission denied", "357"),
        ),
        (
            "https:/u:secretpass@e.example/experiments.php?mode=view&id=1",
            PermissionError(errno.EACCES, "Permission denied", "secretpass"),
        ),
    ],
)
def test_comment_handles_file_probe_oserror_safely(monkeypatch, capsys, target, error):
    posts = []

    class Client:
        root = "https://e.example"

        def add_comment(self, entity, eid, text):
            posts.append((entity, eid, text))

    def fail_probe(self, *args, **kwargs):
        raise error

    monkeypatch.setattr(Path, "stat", fail_probe)
    monkeypatch.setattr(config, "load", lambda *args: {})
    monkeypatch.setattr(
        cli, "_resolved_client", lambda data, profile, meta: ("test", Client())
    )

    assert cli.main(["comment", target, "hi"]) == 1

    assert posts == []
    err = capsys.readouterr().err
    assert err != "" and "secretpass" not in err


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
    document.write_text("---\nid: 42\n---\nbody\n", encoding="utf-8")
    data = {}
    calls = []
    monkeypatch.setattr(cli.config, "load", lambda *args: data)
    monkeypatch.setattr(cli, "merge", lambda *args: calls.append(args))

    assert cli.main(["merge", str(document), "--entity", "items"]) == 0
    assert data["entity"] == "items"
    assert calls == [(document, data, None, False)]


def test_merge_rejects_entity_mismatch_as_cli_error(tmp_path, monkeypatch, capsys):
    document = tmp_path / "report.md"
    document.write_text("---\nid: 42\nentity: items\n---\nbody\n", encoding="utf-8")
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
    monkeypatch.setenv("PATH", "")

    assert cli.main(["merge", str(document)]) == 1
    error = capsys.readouterr().err
    assert str(base) in error
    assert str(remote) in error
    assert "by hand" in error
    assert "elport merge --resolved" in error
    assert "Traceback" not in error
    assert document.read_text(encoding="utf-8") == "local\n"


@pytest.mark.parametrize(
    ("can_write", "key_label"), [(1, "read-write"), (0, "read-only")]
)
def test_whoami_prints_auth_status_block(monkeypatch, capsys, can_write, key_label):
    class IdentityClient:
        root = "https://lab-a.example.org"
        key = "42-secret"

        def me(self):
            return {
                "fullname": "Ada Lovelace",
                "userid": 9,
                "email": "ada@example.org",
                "team": 7,
                "is_sysadmin": 0,
                "teams": [
                    {
                        "id": 7,
                        "name": "Lab A",
                        "usergroup": None,
                        "is_admin": 0,
                        "is_owner": 1,
                    }
                ],
                "scope_experiments": 2,
                "scope_items": 3,
            }

        def info(self):
            return {"elabftw_version": "5.6.12"}

        def apikeys(self):
            return [{"id": 42, "can_write": can_write}]

    monkeypatch.setattr(cli.config, "load", lambda *args: {})
    monkeypatch.setattr(
        cli, "_resolved_client", lambda *args: ("labA", IdentityClient())
    )

    assert cli.main(["whoami"]) == 0
    output = capsys.readouterr().out
    assert output == (
        "✓ lab-a.example.org — Ada Lovelace (uid 9)\n"
        "  Profile   labA\n"
        "  Email     ada@example.org\n"
        "  Team      Lab A (#7) · owner\n"
        f"  API key   {key_label}\n"
        "  Server    eLabFTW 5.6.12\n"
        "  Scopes    experiments=team · items=everything\n"
    )
    assert "\x1b[" not in output


def test_whoami_omits_missing_optional_fields(monkeypatch, capsys):
    class IdentityClient:
        root = "https://lab-a.example.org"
        key = "unprefixed"

        def me(self):
            return {"firstname": "Ada", "lastname": "Lovelace"}

        def info(self):
            return {}

        def apikeys(self):
            return []

    monkeypatch.setattr(cli.config, "load", lambda *args: {})
    monkeypatch.setattr(
        cli, "_resolved_client", lambda *args: ("labA", IdentityClient())
    )

    assert cli.main(["whoami"]) == 0
    assert capsys.readouterr().out == (
        "✓ lab-a.example.org — Ada Lovelace\n  Profile   labA\n"
    )


def test_profile_list_marks_default(monkeypatch, capsys):
    monkeypatch.setattr(
        cli.config,
        "load",
        lambda *args: {
            "default_profile": "labA",
            "profiles": {
                "labA": {"base_url": "https://lab-a.example.org"},
                "labB": {"base_url": "https://lab-b.example.org"},
            },
        },
    )

    assert cli.main(["profile"]) == 0
    assert capsys.readouterr().out == (
        "* labA  https://lab-a.example.org\n  labB  https://lab-b.example.org\n"
    )


def test_profile_list_reports_when_empty(monkeypatch, capsys):
    monkeypatch.setattr(cli.config, "load", lambda *args: {})

    assert cli.main(["profile", "list"]) == 0
    assert capsys.readouterr().out == (
        "no profiles configured; run 'elport login <name>'\n"
    )


def test_profile_use_sets_default(monkeypatch, capsys):
    selected = []
    monkeypatch.setattr(
        cli.config,
        "load",
        lambda *args: {"profiles": {"labA": {"base_url": "https://e.example"}}},
    )
    monkeypatch.setattr(cli.config, "set_default", selected.append, raising=False)

    assert cli.main(["profile", "use", "labA"]) == 0
    assert selected == ["labA"]
    assert capsys.readouterr().out == "default profile is now labA\n"


def test_profile_use_creates_user_config_for_project_profile(
    tmp_path, monkeypatch, capsys
):
    project = tmp_path / "project"
    project.mkdir()
    (project / ".elport.toml").write_text(
        '[profiles.lab]\nbase_url = "https://e.example"\n', encoding="utf-8"
    )
    user = tmp_path / "home" / ".config" / "elport" / "config.toml"
    monkeypatch.chdir(project)
    monkeypatch.setattr(cli.config, "config_path", lambda: user)

    assert cli.main(["profile", "use", "lab"]) == 0
    assert config._read(user)["default_profile"] == "lab"
    assert capsys.readouterr().out == "default profile is now lab\n"


@pytest.mark.parametrize(
    ("argv", "message"),
    [
        (["profile", "use"], "elport profile use requires a profile name"),
        (["profile", "use", "missing"], "no such profile: missing"),
    ],
)
def test_profile_use_reports_invalid_name(argv, message, monkeypatch, capsys):
    monkeypatch.setattr(
        cli.config,
        "load",
        lambda *args: {"profiles": {"labA": {"base_url": "https://e.example"}}},
    )

    assert cli.main(argv) == 1
    assert capsys.readouterr().err == f"{message}\n"


def test_top_level_help_lists_the_help_topics(capsys):
    with pytest.raises(SystemExit):
        cli.main(["--help"])

    out = capsys.readouterr().out
    for topic in cli.HELP_TOPICS:
        assert f"elport help {topic}" in out


@pytest.mark.parametrize("topic", list(cli.HELP_TOPICS))
def test_help_prints_the_topic_file(topic, capsys):
    text = (resources.files("elport") / "help" / f"{topic}.md").read_text("utf-8")

    assert cli.main(["help", topic]) == 0

    assert capsys.readouterr().out == text
    assert text.startswith("# ")


def test_help_topics_match_the_files_that_ship():
    shipped = {
        entry.name.removesuffix(".md")
        for entry in (resources.files("elport") / "help").iterdir()
        if entry.name.endswith(".md")
    }

    assert shipped == set(cli.HELP_TOPICS)


def test_help_without_a_topic_lists_them(capsys):
    assert cli.main(["help"]) == 0

    out = capsys.readouterr().out
    for topic, summary in cli.HELP_TOPICS.items():
        assert f"elport help {topic}" in out and summary in out


def test_help_rejects_an_unknown_topic(capsys):
    with pytest.raises(SystemExit) as exit_info:
        cli.main(["help", "nope"])

    assert exit_info.value.code == 2


def test_agents_topic_names_what_needs_the_users_approval():
    text = (resources.files("elport") / "help" / "agents.md").read_text("utf-8")

    for flag in ("--force", "--yes", "attachments:"):
        assert flag in text


def test_agents_topic_says_remote_text_is_not_an_instruction():
    text = (resources.files("elport") / "help" / "agents.md").read_text("utf-8")

    assert "instructions" in text and "someone else" in text


def test_top_level_help_lists_side_effects_defaults_and_auth(capsys):
    with pytest.raises(SystemExit):
        cli.main(["--help"])

    out = capsys.readouterr().out
    for expected in (
        "elport help agents",
        "side effects:",
        "read-only",
        "local writes",
        "remote writes",
        "view -o",
        "push -n",
        "profile use",
        "report.md",
        "ELABFTW_BASE_URL",
        "ELABFTW_API_KEY",
        "0 success, 1 failure, 2 usage error",
    ):
        assert expected in out


def test_subcommand_help_names_guidance_for_push_and_view(capsys):
    with pytest.raises(SystemExit):
        cli.main(["push", "--help"])
    push_help = " ".join(capsys.readouterr().out.split())
    assert "elport pull" in push_help and "elport merge" in push_help
    assert "elport help agents" in push_help

    with pytest.raises(SystemExit):
        cli.main(["view", "--help"])
    view_help = " ".join(capsys.readouterr().out.split())
    assert "saved paths with -o DIR" in view_help and "stderr" in view_help


def test_agents_topic_covers_workflows_remote_writes_and_env_auth():
    text = (resources.files("elport") / "help" / "agents.md").read_text("utf-8")

    for expected in (
        "elport list",
        "elport view",
        "elport new",
        "elport comment",
        "elport clone --all",
        "ELABFTW_BASE_URL",
        "ELABFTW_API_KEY",
        "stderr",
    ):
        assert expected in text


def test_documents_help_and_readmes_note_elabftw_6_html_behavior():
    repo_root = Path(__file__).resolve().parent.parent
    docs_help = (resources.files("elport") / "help" / "documents.md").read_text("utf-8")
    readme = (repo_root / "README.md").read_text("utf-8")
    readme_ja = (repo_root / "README_JA.md").read_text("utf-8")

    assert "6.x" in docs_help and "![alt](" in docs_help
    assert "6.x" in readme
    assert "6.x" in readme_ja


def test_help_topics_and_top_help_avoid_note_terminology(capsys):
    with pytest.raises(SystemExit):
        cli.main(["--help"])
    out = capsys.readouterr().out
    assert not re.search(r"\bnotes?\b", out, flags=re.IGNORECASE)

    for topic in cli.HELP_TOPICS:
        text = (resources.files("elport") / "help" / f"{topic}.md").read_text("utf-8")
        assert not re.search(r"\bnotes?\b", text, flags=re.IGNORECASE), topic


def test_migration_notice_prints_only_on_tty_stderr_for_commands(
    monkeypatch, tmp_path, capsys
):
    monkeypatch.setenv("HOME", str(tmp_path))
    monkeypatch.delenv("ELPORT_NO_MIGRATION_NOTICE", raising=False)

    # Non-TTY stderr stays silent.
    assert cli.main(["profile", "list"]) == 0
    captured = capsys.readouterr()
    assert captured.err == ""

    # TTY stderr prints one notice line on stderr, never stdout.
    monkeypatch.setattr(sys.stderr, "isatty", lambda: True)
    assert cli.main(["profile", "list"]) == 0
    captured = capsys.readouterr()
    assert "v0.2.0+" in captured.err
    assert "v0.2.0+" not in captured.out

    # ELPORT_NO_MIGRATION_NOTICE=1 silences the notice even on a TTY.
    monkeypatch.setenv("ELPORT_NO_MIGRATION_NOTICE", "1")
    assert cli.main(["profile", "list"]) == 0
    assert capsys.readouterr().err == ""

    # --version, --help, and help stay silent even on a TTY.
    monkeypatch.delenv("ELPORT_NO_MIGRATION_NOTICE", raising=False)
    for argv in (["--version"], ["--help"]):
        with pytest.raises(SystemExit):
            cli.main(argv)
        assert capsys.readouterr().err == ""

    assert cli.main(["help"]) == 0
    assert capsys.readouterr().err == ""
