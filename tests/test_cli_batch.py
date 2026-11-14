import io
import sys
from contextlib import redirect_stdout

import pytest
from _sync_harness import elport_doc

from elport import cli, frontmatter, sync

# --- discovery -------------------------------------------------------------


def test_documents_finds_only_elport_documents(tmp_path):
    a = elport_doc(tmp_path / "a" / "report.md")
    b = elport_doc(tmp_path / "b.md", id=2)
    (tmp_path / "README.md").write_text("# readme\n")
    (tmp_path / "hugo.md").write_text("---\nlayout: post\n---\nx\n")
    (tmp_path / "draft.md").write_text(frontmatter.render({"title": "t"}, "x"))
    (tmp_path / "noprofile.md").write_text(frontmatter.render({"id": 3}, "x"))
    elport_doc(tmp_path / ".hidden" / "report.md")
    elport_doc(tmp_path / "a" / "report.remote.md")
    elport_doc(tmp_path / "a" / "report.base.md")

    found, skipped = cli._documents(tmp_path)

    assert found == [a, b]
    assert skipped == 4


def test_expand_keeps_order_and_drops_duplicates(tmp_path):
    a = elport_doc(tmp_path / "x" / "a.md")
    b = elport_doc(tmp_path / "b.md")

    found, skipped = cli._expand([b, tmp_path / "x", a])

    assert found == [b, a]
    assert skipped == 0


# --- batch commands --------------------------------------------------------


@pytest.fixture
def stub_target(monkeypatch):
    monkeypatch.setattr(cli.config, "load", lambda *args: {})
    monkeypatch.setattr(cli, "_LazyClient", lambda *args: object())


def test_push_of_a_directory_prints_one_line_per_document_and_keeps_going(
    tmp_path, monkeypatch, capsys, stub_target
):
    a = elport_doc(tmp_path / "a.md", id=1)
    b = elport_doc(tmp_path / "b.md", id=2)
    (tmp_path / "notes.md").write_text("plain\n")

    def fake_push(path, client, data, profile, dry_run, force, yes):
        if path == b:
            print("warning: something odd", file=sys.stderr)
            raise RuntimeError("remote changed; use pull or --force")
        print("verbose single-document output")
        return "pushed (body updated)"

    monkeypatch.setattr(cli, "push", fake_push)

    assert cli.main(["push", str(tmp_path)]) == 1

    captured = capsys.readouterr()
    assert captured.out.splitlines() == [
        f"✓ {a}  experiments/1  pushed (body updated)",
        f"✗ {b}  experiments/2  remote changed; use pull or --force",
        "1 done, 1 failed, 1 skipped",
    ]
    assert "    warning: something odd" in captured.err


def test_pull_batch_marks_unchanged_documents(
    tmp_path, monkeypatch, capsys, stub_target
):
    a = elport_doc(tmp_path / "a.md")
    monkeypatch.setattr(cli, "pull", lambda *args: "up to date")

    assert cli.main(["pull", str(a), str(a)]) == 0

    assert capsys.readouterr().out.splitlines() == [
        f"· {a}  experiments/1  up to date",
        "1 up to date",
    ]


def test_status_batch_marks_documents_with_something_to_sync(
    tmp_path, monkeypatch, capsys, stub_target
):
    a = elport_doc(tmp_path / "a.md", id=1)
    b = elport_doc(tmp_path / "b.md", id=2)
    monkeypatch.setattr(
        cli,
        "status",
        lambda path, *args: "up to date" if path == a else "local dirty",
    )

    assert cli.main(["status", str(tmp_path)]) == 0

    assert capsys.readouterr().out.splitlines() == [
        f"· {a}  experiments/1  up to date",
        f"* {b}  experiments/2  local dirty",
        "1 up to date, 1 to sync",
    ]


def test_diff_batch_prints_each_non_empty_diff_under_a_header(
    tmp_path, monkeypatch, capsys, stub_target
):
    a = elport_doc(tmp_path / "a.md", id=1)
    elport_doc(tmp_path / "b.md", id=2)

    def fake_diff(path, *args):
        if path == a:
            print("-old\n+new")

    monkeypatch.setattr(cli, "diff", fake_diff)

    assert cli.main(["diff", str(tmp_path)]) == 0

    assert capsys.readouterr().out == f"=== {a}\n-old\n+new\n"


def test_dry_run_batch_shows_each_plan(tmp_path, monkeypatch, capsys, stub_target):
    a = elport_doc(tmp_path / "a.md")

    def fake_push(*args):
        print("Upload plan:\n  fig.png")
        return "dry run"

    monkeypatch.setattr(cli, "push", fake_push)

    assert cli.main(["push", "-n", str(tmp_path)]) == 0

    assert capsys.readouterr().out.splitlines() == [
        f"✓ {a}  experiments/1  dry run",
        f"=== {a}",
        "Upload plan:",
        "  fig.png",
        "1 done",
    ]


def test_batch_refuses_force_before_touching_anything(
    tmp_path, monkeypatch, capsys, stub_target
):
    elport_doc(tmp_path / "a.md")
    monkeypatch.setattr(cli, "push", lambda *args: pytest.fail("pushed"))

    assert cli.main(["push", "-f", str(tmp_path)]) == 1

    assert "--force works on one document at a time" in capsys.readouterr().err


def test_a_directory_without_documents_is_an_error(tmp_path, capsys, stub_target):
    (tmp_path / "README.md").write_text("x")

    assert cli.main(["status", str(tmp_path)]) == 1

    assert f"no elport documents under {tmp_path} (1 skipped)" in (
        capsys.readouterr().err
    )


def test_a_batch_never_prompts(monkeypatch):
    monkeypatch.setattr(sync.sys.stdin, "isatty", lambda: True)
    with (
        redirect_stdout(io.StringIO()),
        pytest.raises(RuntimeError, match="re-run with --yes"),
    ):
        sync._confirm_permission_widening([("read", "public", 50, 30)], False)


def test_a_batch_shares_one_client_per_server(tmp_path, monkeypatch, capsys):
    monkeypatch.delenv("ELABFTW_BASE_URL", raising=False)
    monkeypatch.delenv("ELABFTW_API_KEY", raising=False)
    monkeypatch.setattr(
        cli.config,
        "load",
        lambda *args: {
            "default_profile": "test",
            "profiles": {"test": {"base_url": "https://e.example"}},
        },
    )
    monkeypatch.setattr(cli.config.keyring, "get_password", lambda *args: "secret")
    elport_doc(tmp_path / "a.md", id=1)
    elport_doc(tmp_path / "b.md", id=2)
    sessions = []

    def fake_push(path, client, data, profile, dry_run, force, yes):
        sessions.append(client.session)
        return "pushed (body unchanged)"

    monkeypatch.setattr(cli, "push", fake_push)

    assert cli.main(["push", str(tmp_path)]) == 0
    assert len(sessions) == 2 and sessions[0] is sessions[1]
