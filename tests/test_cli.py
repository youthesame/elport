import stat
from pathlib import Path

import pytest

from elab import cli, frontmatter, sync


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


def test_merge_defaults_to_report_without_loading_config(tmp_path, monkeypatch):
    monkeypatch.chdir(tmp_path)
    calls = []
    monkeypatch.setattr(cli, "merge", lambda path: calls.append(path))
    monkeypatch.setattr(
        cli.config,
        "load",
        lambda *args: pytest.fail("merge must not load config"),
    )
    monkeypatch.setattr(
        cli,
        "_client",
        lambda *args: pytest.fail("merge must not create a client"),
    )

    assert cli.main(["merge"]) == 0
    assert calls == [Path("report.md")]


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
    assert "manually" in error
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
