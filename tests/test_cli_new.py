import pytest

from elport import cli, frontmatter


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
    assert meta["id"] == 42
    assert meta["title"] == "Experiment"
    assert meta["profile"] == "test"
    assert body == ""
    assert saved[-1] == (
        "https://e.example",
        "experiments",
        "42",
        {
            "remote_base": "server empty\n",
            "local_base": "",
            "path": str(output.resolve()),
        },
    )
    assert capsys.readouterr().out == (
        f"created experiments/42: {output}\n  → https://e.example/experiments/42\n"
    )


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
    assert "id" not in meta


def test_new_persists_created_id_and_resumable_base_on_followup_failure(
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
    saved = []
    monkeypatch.setattr(cli.state, "save", lambda *args: saved.append(args))

    assert cli.main(["new", "Experiment", "-o", str(output)]) == 1

    meta, body = frontmatter.parse(output.read_text(encoding="utf-8"))
    assert meta["id"] == 42
    assert meta["profile"] == "test"
    assert body == ""
    # Issue #13: a provisional base lets a plain `elport push` resume this entity.
    assert [args[-1] for args in saved] == [
        {
            "remote_base": "",
            "local_base": "",
            "meta_base": {},
            "pending_create": True,
            "body_sent": False,
        }
    ]


def test_new_rejects_missing_parent_before_remote_creation(tmp_path, monkeypatch):
    output = tmp_path / "missing" / "report.md"
    client = NewClient()
    monkeypatch.setattr(cli.config, "load", lambda *args: {})
    monkeypatch.setattr(cli, "_resolved_client", lambda *args: ("test", client))

    assert cli.main(["new", "Experiment", "-o", str(output)]) == 1
    assert client.created == []
    assert not output.exists()


def test_new_rejects_existing_output_before_remote_creation(
    tmp_path, monkeypatch, capsys
):
    output = tmp_path / "report.md"
    output.write_bytes(b"keep me")
    client = NewClient()
    monkeypatch.setattr(cli.config, "load", lambda *args: {})
    monkeypatch.setattr(cli, "_resolved_client", lambda *args: ("test", client))

    assert cli.main(["new", "Experiment", "-o", str(output)]) == 1
    assert client.created == []
    assert output.read_bytes() == b"keep me"
    # An existing note becomes an entity through push, so the error names it.
    assert f"elport push {output}" in capsys.readouterr().err


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


def test_new_help_points_an_existing_note_to_push(capsys):
    with pytest.raises(SystemExit):
        cli.main(["new", "--help"])

    assert "elport push <file>" in capsys.readouterr().out
