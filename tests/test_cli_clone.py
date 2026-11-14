import pytest
from _sync_harness import elport_doc

from elport import cli, frontmatter, sync


class CloneClient:
    def __init__(self, body="remote body\n"):
        self.root = "https://e.example"
        self.body = body

    def get(self, entity, eid):
        return {
            "body": self.body,
            "title": "Remote title",
            "category_title": "Chat",
            "tags": "alpha|beta",
            "canread_base": 30,
            "canwrite_base": 10,
            "sharelink": f"https://e.example/{entity}/{eid}",
        }

    def uploads(self, entity, eid):
        return []

    def me(self):
        return {"team": 7}


def _clone_env(monkeypatch, tmp_path, client):
    monkeypatch.setattr(cli.config, "load", lambda *args: {})
    monkeypatch.setattr(cli, "_resolved_client", lambda *args: ("test", client))
    monkeypatch.setattr(
        sync.config_module,
        "resolve",
        lambda *args: ("test", "https://e.example", "secret", True),
    )
    monkeypatch.setattr(cli.state, "_dir", lambda create=False: tmp_path / "state")
    (tmp_path / "state").mkdir()


def test_clone_scaffolds_frontmatter_and_pulls(tmp_path, monkeypatch):
    output = tmp_path / "report.md"
    client = CloneClient()
    _clone_env(monkeypatch, tmp_path, client)

    assert cli.main(["clone", "357", "-o", str(output)]) == 0

    meta, body = frontmatter.parse(output.read_text())
    assert meta["id"] == 357
    assert meta["entity"] == "experiments"
    assert meta["profile"] == "test"
    assert meta["title"] == "Remote title"
    assert meta["category"] == "Chat"
    assert meta["tags"] == ["alpha", "beta"]
    assert meta["read"] == "team"
    assert meta["write"] == "owner"
    assert body == "remote body\n"
    assert sync.state.load("https://e.example", "experiments", "357") == {
        "remote_base": "remote body\n",
        "local_base": "remote body\n",
        "meta_base": {
            "title": "Remote title",
            "category": "Chat",
            "read": "team",
            "write": "owner",
        },
        "path": str(output.resolve()),
    }


def test_clone_refuses_when_the_entity_already_has_a_working_copy(
    tmp_path, monkeypatch, capsys
):
    output = tmp_path / "second.md"
    _clone_env(monkeypatch, tmp_path, CloneClient())
    saved = {
        "remote_base": "remote body\n",
        "local_base": "old local\n",
        "path": "/notes/exp357/report.md",
    }
    sync.state.save("https://e.example", "experiments", "357", saved)

    assert cli.main(["clone", "357", "-o", str(output)]) == 1

    assert not output.exists()
    err = capsys.readouterr().err
    assert "already has a working copy at /notes/exp357/report.md" in err
    assert sync.state.load("https://e.example", "experiments", "357") == saved


def test_clone_refuses_to_overwrite_an_existing_output(tmp_path, monkeypatch):
    output = tmp_path / "report.md"
    output.write_bytes(b"keep me")
    client = CloneClient()
    _clone_env(monkeypatch, tmp_path, client)

    assert cli.main(["clone", "357", "-o", str(output)]) == 1
    assert output.read_bytes() == b"keep me"


def test_clone_rejects_missing_parent(tmp_path, monkeypatch):
    output = tmp_path / "missing" / "report.md"
    _clone_env(monkeypatch, tmp_path, CloneClient())

    assert cli.main(["clone", "357", "-o", str(output)]) == 1
    assert not output.exists()


def test_clone_rejects_a_non_positive_id(tmp_path, monkeypatch):
    output = tmp_path / "report.md"
    _clone_env(monkeypatch, tmp_path, CloneClient())

    assert cli.main(["clone", "0", "-o", str(output)]) == 1
    assert not output.exists()


def test_clone_removes_the_stub_when_the_pull_fails(tmp_path, monkeypatch):
    output = tmp_path / "report.md"
    _clone_env(monkeypatch, tmp_path, CloneClient())
    monkeypatch.setattr(
        cli,
        "pull",
        lambda *args: (_ for _ in ()).throw(RuntimeError("pull failed")),
    )

    assert cli.main(["clone", "357", "-o", str(output)]) == 1
    assert not output.exists()


def test_clone_keeps_the_document_when_the_pull_fails_after_saving_a_base(
    tmp_path, monkeypatch
):
    output = tmp_path / "report.md"
    _clone_env(monkeypatch, tmp_path, CloneClient())
    real_pull = cli.pull

    def pull_then_fail(*args):
        real_pull(*args)
        raise BrokenPipeError("stdout closed")

    monkeypatch.setattr(cli, "pull", pull_then_fail)

    assert cli.main(["clone", "357", "-o", str(output)]) == 1

    assert frontmatter.parse(output.read_text())[1] == "remote body\n"


@pytest.mark.parametrize("argv", [["clone"], ["clone", "3", "--all", "x"]])
def test_clone_needs_exactly_one_of_id_and_all(argv, capsys):
    assert cli.main(argv) == 1
    assert "exactly one of <id> or --all" in capsys.readouterr().err


class HtmlCloneClient(CloneClient):
    def get(self, entity, eid):
        return {**super().get(entity, eid), "content_type": 1}

    def download(self, entity, eid, uid):
        return b"data"


def test_clone_refuses_an_html_entity_and_points_to_view(tmp_path, monkeypatch, capsys):
    output = tmp_path / "report.md"
    _clone_env(monkeypatch, tmp_path, HtmlCloneClient(body="<p>web UI</p>"))

    assert cli.main(["clone", "357", "-o", str(output)]) == 1

    assert not output.exists()
    assert "view 357 -o" in capsys.readouterr().err
    assert sync.state.load("https://e.example", "experiments", "357") is None


# --- clone --all -----------------------------------------------------------


@pytest.mark.parametrize(
    ("title", "expected"),
    [
        ("CRISPR滴定実験", "42-CRISPR滴定実験"),
        ('a/b\\c:d*e?f"g<h>i|j', "42-a_b_c_d_e_f_g_h_i_j"),
        ("..hidden ", "42-hidden"),
        ("", "42"),
        ("x" * 80, "42-" + "x" * 50),
        ("ｶﾞ", "42-ｶﾞ"),
        ("ガ", "42-ガ"),
    ],
)
def test_directory_name_keeps_unicode_and_replaces_unsafe_characters(title, expected):
    assert cli._directory_name(42, title) == expected


class MirrorClient:
    root = "https://e.example"

    def __init__(self, rows):
        self.rows = rows
        self.searches = []

    def search(self, entity, params):
        self.searches.append((entity, dict(params)))
        rows = self.rows.get(entity, [])
        return rows[params["offset"] : params["offset"] + params["limit"]]


@pytest.fixture
def mirror_env(tmp_path, monkeypatch):
    monkeypatch.setattr(cli.config, "load", lambda *args: {})
    monkeypatch.setattr(cli.state, "_dir", lambda create=False: tmp_path / "state")
    (tmp_path / "state").mkdir()
    cloned = []

    def fake_clone(eid, entity, path, profile):
        cloned.append((entity, eid, path))
        elport_doc(path, id=eid, entity=entity)
        return "cloned"

    monkeypatch.setattr(cli, "_clone_to", fake_clone)
    return cloned


def test_clone_all_lays_out_every_entity_and_skips_ones_already_present(
    tmp_path, monkeypatch, capsys, mirror_env
):
    root = tmp_path / "backup"
    elport_doc(root / "experiments" / "1-old" / "report.md", id=1)
    client = MirrorClient(
        {
            "experiments": [
                {"id": 1, "title": "old"},
                {"id": 2, "title": "滴定/実験"},
            ],
            "items": [{"id": 7, "title": "Buffer"}],
        }
    )
    monkeypatch.setattr(cli, "_resolved_client", lambda *args: ("test", client))

    assert cli.main(["clone", "--all", str(root)]) == 0

    exp = root / "experiments" / "2-滴定_実験" / "report.md"
    item = root / "items" / "7-Buffer" / "report.md"
    assert mirror_env == [("experiments", 2, exp), ("items", 7, item)]
    assert capsys.readouterr().out.splitlines() == [
        f"✓ {exp}  experiments/2  cloned",
        f"✓ {item}  items/7  cloned",
        "2 done, 1 up to date",
    ]
    assert {params["scope"] for _, params in client.searches} == {1}


def test_clone_all_pages_through_every_result_in_the_chosen_scope(
    tmp_path, monkeypatch, mirror_env
):
    rows = [{"id": n, "title": f"t{n}"} for n in range(1, 251)]
    client = MirrorClient({"experiments": rows})
    monkeypatch.setattr(cli, "_resolved_client", lambda *args: ("test", client))
    argv = ["clone", "--all", str(tmp_path / "b"), "--entity", "experiments"]

    assert cli.main([*argv, "--scope", "team"]) == 0

    assert len(mirror_env) == 250
    assert {params["scope"] for _, params in client.searches} == {2}


def test_clone_all_skips_an_entity_with_a_working_copy_elsewhere(
    tmp_path, monkeypatch, capsys, mirror_env
):
    elsewhere = elport_doc(tmp_path / "work" / "report.md", id=3)
    sync.state.save(
        "https://e.example",
        "experiments",
        "3",
        {"remote_base": "", "local_base": "", "path": str(elsewhere)},
    )
    client = MirrorClient({"experiments": [{"id": 3, "title": "x"}]})
    monkeypatch.setattr(cli, "_resolved_client", lambda *args: ("test", client))

    assert (
        cli.main(["clone", "--all", str(tmp_path / "b"), "--entity", "experiments"])
        == 0
    )

    assert mirror_env == []
    assert f"· experiments/3  working copy at {elsewhere}" in capsys.readouterr().out


def test_clone_all_removes_the_empty_directory_when_a_clone_fails(
    tmp_path, monkeypatch, capsys, mirror_env
):
    client = MirrorClient({"experiments": [{"id": 4, "title": "x"}]})
    monkeypatch.setattr(cli, "_resolved_client", lambda *args: ("test", client))
    monkeypatch.setattr(
        cli,
        "_clone_to",
        lambda *args: (_ for _ in ()).throw(RuntimeError("API 403: forbidden")),
    )

    assert (
        cli.main(["clone", "--all", str(tmp_path / "b"), "--entity", "experiments"])
        == 1
    )

    assert not (tmp_path / "b" / "experiments" / "4-x").exists()
    assert "✗" in capsys.readouterr().out


def _lists_one_experiment(monkeypatch, client):
    def search(entity, params):
        first_page = entity == "experiments" and params["offset"] == 0
        return [{"id": 5, "title": "Web"}] if first_page else []

    monkeypatch.setattr(client, "search", search, raising=False)


def test_clone_all_writes_an_html_entity_as_a_snapshot_once(
    tmp_path, monkeypatch, capsys
):
    root = tmp_path / "backup"
    client = HtmlCloneClient(body='<p style="x">web <span>UI</span></p>')
    _clone_env(monkeypatch, tmp_path, client)
    _lists_one_experiment(monkeypatch, client)
    snapshot_dir = root / "experiments" / "5-Web"

    assert cli.main(["clone", "--all", str(root)]) == 0

    assert (snapshot_dir / "report.md").read_text() == "<p>web UI</p>"
    assert (
        f"✓ {snapshot_dir / 'report.md'}  experiments/5  snapshot (HTML entity, read-only)"
        in capsys.readouterr().out
    )
    assert sync.state.load("https://e.example", "experiments", "5") is None

    (snapshot_dir / "report.md").write_text("kept")
    assert cli.main(["clone", "--all", str(root)]) == 0

    assert (snapshot_dir / "report.md").read_text() == "kept"
    assert "1 up to date" in capsys.readouterr().out


@pytest.mark.parametrize("name", ["5-renamed", "5"])
def test_clone_all_counts_a_directory_named_by_the_id_as_present(
    tmp_path, monkeypatch, capsys, mirror_env, name
):
    root = tmp_path / "backup"
    (root / "experiments" / name).mkdir(parents=True)
    (root / "experiments" / "55-other").mkdir()
    client = MirrorClient({"experiments": [{"id": 5, "title": "Web"}]})
    monkeypatch.setattr(cli, "_resolved_client", lambda *args: ("test", client))

    assert cli.main(["clone", "--all", str(root), "--entity", "experiments"]) == 0

    assert mirror_env == []


def test_clone_all_removes_a_snapshot_whose_download_fails(
    tmp_path, monkeypatch, capsys
):
    root = tmp_path / "backup"
    body = '<p><img src="app/download.php?f=aa/fig"></p>'
    client = HtmlCloneClient(body=body)
    _clone_env(monkeypatch, tmp_path, client)
    _lists_one_experiment(monkeypatch, client)
    monkeypatch.setattr(
        client,
        "uploads",
        lambda entity, eid: [
            {"id": 3, "long_name": "aa/fig", "real_name": "fig.png", "storage": 1}
        ],
    )
    monkeypatch.setattr(
        client,
        "download",
        lambda *args: (_ for _ in ()).throw(OSError("connection reset")),
    )

    assert cli.main(["clone", "--all", str(root)]) == 1

    # Left in place, a partial snapshot would count as present on every re-run.
    assert not (root / "experiments" / "5-Web").exists()
    assert "connection reset" in capsys.readouterr().out


def test_clone_all_removes_what_a_failed_clone_downloaded(
    tmp_path, monkeypatch, capsys, mirror_env
):
    def clone_then_fail(eid, entity, path, profile):
        (path.parent / "fig.png").write_bytes(b"half")
        raise OSError("connection reset")

    client = MirrorClient({"experiments": [{"id": 4, "title": "x"}]})
    monkeypatch.setattr(cli, "_resolved_client", lambda *args: ("test", client))
    monkeypatch.setattr(cli, "_clone_to", clone_then_fail)

    argv = ["clone", "--all", str(tmp_path / "b"), "--entity", "experiments"]
    assert cli.main(argv) == 1

    assert not (tmp_path / "b" / "experiments" / "4-x").exists()
    assert "connection reset" in capsys.readouterr().out


def test_clone_all_does_not_count_a_file_named_by_the_id(
    tmp_path, monkeypatch, mirror_env
):
    root = tmp_path / "backup"
    (root / "experiments").mkdir(parents=True)
    (root / "experiments" / "5-notes.txt").write_text("mine")
    client = MirrorClient({"experiments": [{"id": 5, "title": "Web"}]})
    monkeypatch.setattr(cli, "_resolved_client", lambda *args: ("test", client))

    assert cli.main(["clone", "--all", str(root), "--entity", "experiments"]) == 0

    assert [eid for _, eid, _ in mirror_env] == [5]
