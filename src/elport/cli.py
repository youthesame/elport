from __future__ import annotations

import argparse
import getpass
import io
import os
import re
import sys
import unicodedata
from collections import Counter
from collections.abc import Callable
from contextlib import redirect_stderr, redirect_stdout
from importlib.metadata import version as _pkg_version
from pathlib import Path

from . import browse, config, frontmatter, state, sync
from .client import Client
from .sync import (
    comment,
    comments,
    diff,
    fetch,
    mark_created,
    merge,
    pull,
    push,
    status,
)

ENTITIES = ("experiments", "items")
SEARCH_PAGE = 100
SIDECARS = (".remote.md", ".base.md")
TITLE_LIMIT = 50
_WORDS = {"✓": "done", "·": sync.UP_TO_DATE, "*": "to sync", "✗": "failed"}

_EXAMPLES = """\
examples:
  elport login                       store credentials for the default profile
  elport new "Cell viability assay"  create a remote entity and report.md
  elport clone 357                  start report.md from an existing entity
  elport list                        list your own entities on the server
  elport view 357                    print a remote body without cloning it
  elport push                        upload report.md (add -n to preview first)
  elport status                      show local/remote sync state
  elport pull                        download the remote body and attachments
  elport push notes/                 push every document under notes/
  elport clone --all backup/         back up all your entities (re-run to add new ones)
"""


def _add_target_options(parser: argparse.ArgumentParser) -> None:
    parser.add_argument("--profile", help="config profile to use")
    parser.add_argument(
        "--entity", choices=ENTITIES, help="target entity type (default: experiments)"
    )


def _add_docs_argument(parser: argparse.ArgumentParser) -> None:
    parser.add_argument(
        "doc",
        nargs="*",
        default=["report.md"],
        help="documents or directories (default: report.md); "
        "a directory means every elport document under it",
    )


def _parser() -> argparse.ArgumentParser:
    parser = argparse.ArgumentParser(
        prog="elport",
        description="git-like sync CLI for eLabFTW (local is authoritative)",
        epilog=_EXAMPLES,
        formatter_class=argparse.RawDescriptionHelpFormatter,
    )
    parser.add_argument(
        "--version", action="version", version=f"%(prog)s {_pkg_version('elport')}"
    )
    commands = parser.add_subparsers(dest="cmd", required=True)

    push_parser = commands.add_parser(
        "push", help="upload the local document (full-body overwrite)"
    )
    _add_docs_argument(push_parser)
    _add_target_options(push_parser)
    push_parser.add_argument(
        "-n",
        "--dry-run",
        action="store_true",
        help="show what would change without uploading",
    )
    push_parser.add_argument(
        "-f",
        "--force",
        action="store_true",
        help="overwrite even if the remote has diverged",
    )
    push_parser.add_argument(
        "-y",
        "--yes",
        action="store_true",
        help="skip confirmations (access widening, large uploads)",
    )

    pull_parser = commands.add_parser(
        "pull", help="download the remote body and attachments"
    )
    _add_docs_argument(pull_parser)
    _add_target_options(pull_parser)

    fetch_parser = commands.add_parser(
        "fetch", help="download all remote attachments (read-only)"
    )
    _add_docs_argument(fetch_parser)
    _add_target_options(fetch_parser)

    status_parser = commands.add_parser("status", help="show local/remote sync state")
    _add_docs_argument(status_parser)
    _add_target_options(status_parser)

    comments_parser = commands.add_parser(
        "comments", help="show the remote comment thread"
    )
    comments_parser.add_argument(
        "doc",
        nargs="?",
        default="report.md",
        help="local document (default: report.md)",
    )
    _add_target_options(comments_parser)

    comment_parser = commands.add_parser("comment", help="post a remote comment")
    comment_parser.add_argument(
        "doc",
        nargs="?",
        default="report.md",
        help="local document (default: report.md)",
    )
    comment_parser.add_argument("text", help="comment text")
    _add_target_options(comment_parser)

    diff_parser = commands.add_parser("diff", help="show changes against the base")
    _add_docs_argument(diff_parser)
    _add_target_options(diff_parser)
    diff_parser.add_argument(
        "--base",
        action="store_true",
        help="diff against the recorded base instead of the remote",
    )

    merge_parser = commands.add_parser(
        "merge", help="merge local and remote changes using git merge-file"
    )
    merge_parser.add_argument(
        "doc",
        nargs="?",
        default="report.md",
        help="local document (default: report.md)",
    )
    _add_target_options(merge_parser)
    merge_parser.add_argument(
        "--resolved",
        action="store_true",
        help="mark a hand-merged conflict as resolved",
    )

    new_parser = commands.add_parser(
        "new", help="create a new remote entity and local document"
    )
    new_parser.add_argument("title", help="title of the new entity")
    new_parser.add_argument(
        "--entity",
        choices=ENTITIES,
        default="experiments",
        help="entity type to create (default: experiments)",
    )
    new_parser.add_argument("--profile", help="config profile to use")
    new_parser.add_argument(
        "-o",
        "--output",
        default="report.md",
        help="local document to create (default: report.md)",
    )

    list_parser = commands.add_parser("list", help="list remote entities (read-only)")
    list_parser.add_argument(
        "--entity",
        choices=ENTITIES,
        help="entity type to list (default: experiments)",
    )
    list_parser.add_argument(
        "--scope",
        choices=tuple(browse.SCOPES),
        default="self",
        help="whose entities to list (default: self)",
    )
    list_parser.add_argument(
        "-q", "--query", help="search term matched against title, body and elabid"
    )
    list_parser.add_argument(
        "--limit",
        type=int,
        default=browse.DEFAULT_LIMIT,
        help=f"maximum number of results (default: {browse.DEFAULT_LIMIT})",
    )
    list_parser.add_argument(
        "--offset", type=int, default=0, help="skip this many results"
    )
    list_parser.add_argument(
        "--json", action="store_true", help="emit the raw API rows as JSON"
    )
    list_parser.add_argument("--profile", help="config profile to use")

    view_parser = commands.add_parser(
        "view", help="print a remote body to stdout (read-only)"
    )
    view_parser.add_argument("id", type=int, help="id of the remote entity")
    view_parser.add_argument(
        "--entity",
        choices=ENTITIES,
        help="entity type to view (default: experiments)",
    )
    view_parser.add_argument("--profile", help="config profile to use")

    clone_parser = commands.add_parser(
        "clone", help="start a local document from an existing remote entity"
    )
    clone_parser.add_argument("id", type=int, nargs="?", help="id of the remote entity")
    clone_parser.add_argument(
        "--all",
        dest="all_root",
        metavar="ROOT",
        help="clone every entity not yet under ROOT into "
        "ROOT/<entity>/<id>-<title>/report.md",
    )
    clone_parser.add_argument(
        "--entity",
        choices=ENTITIES,
        help="entity type to clone (default: experiments; both with --all)",
    )
    clone_parser.add_argument(
        "--scope",
        choices=tuple(browse.SCOPES),
        default="self",
        help="whose entities --all clones (default: self)",
    )
    clone_parser.add_argument("--profile", help="config profile to use")
    clone_parser.add_argument(
        "-o",
        "--output",
        default="report.md",
        help="local document to create (default: report.md)",
    )

    whoami_parser = commands.add_parser(
        "whoami", help="show the authenticated user and active team"
    )
    whoami_parser.add_argument("--profile", help="config profile to use")

    login_parser = commands.add_parser(
        "login", help="store base_url and api_key for a profile"
    )
    login_parser.add_argument(
        "profile", nargs="?", default="default", help="profile name (default: default)"
    )
    logout_parser = commands.add_parser(
        "logout", help="remove the stored api_key for a profile"
    )
    logout_parser.add_argument(
        "profile", nargs="?", default="default", help="profile name (default: default)"
    )
    profile_parser = commands.add_parser(
        "profile", help="list profiles or select the default profile"
    )
    profile_parser.add_argument(
        "action", nargs="?", choices=["list", "use"], default="list"
    )
    profile_parser.add_argument("name", nargs="?")
    return parser


def _resolved_client(data: dict, profile: str | None, meta: dict) -> tuple[str, Client]:
    name, base_url, api_key, verify_ssl = config.resolve(data, profile, meta)
    return name, Client(base_url, api_key, verify_ssl)


def _client(data: dict, profile: str | None, meta: dict) -> Client:
    return _resolved_client(data, profile, meta)[1]


class _LazyClient:
    def __init__(self, data: dict, profile: str | None, meta: dict):
        self._args = (data, profile, meta)
        self._client = None

    def __getattr__(self, name):
        if self._client is None:
            self._client = _client(*self._args)
        return getattr(self._client, name)


def _target(path: Path, profile: str | None, entity: str | None):
    data = config.load(Path.cwd(), path.parent)
    if not path.is_file():
        raise RuntimeError(f"document not found: {path}")
    meta = frontmatter.parse(path.read_text(encoding="utf-8"))[0]
    if "entity" in meta:
        selected_entity = meta["entity"]
    elif entity is not None:
        selected_entity = entity
    else:
        selected_entity = data.get("entity", "experiments")
    if selected_entity not in ENTITIES:
        raise ValueError("entity must be one of: experiments, items")
    if entity and meta.get("entity") and entity != meta["entity"]:
        raise ValueError("entity mismatch between frontmatter and CLI")
    if entity and not meta.get("entity"):
        data["entity"] = entity
    return data, _LazyClient(data, profile, meta)


def _new(args) -> None:
    path = Path(args.output)
    if path.exists():
        raise RuntimeError(f"output already exists: {path}")
    if not path.parent.is_dir():
        raise RuntimeError(f"output parent does not exist: {path.parent}")
    meta = {"title": args.title, "entity": args.entity}
    data = config.load(Path.cwd(), path.parent)
    resolved_profile, client = _resolved_client(data, args.profile, meta)
    meta["profile"] = resolved_profile
    frontmatter.atomic_write(path, frontmatter.render(meta, ""))
    team = client.me().get("team")
    created = client.create(args.entity, args.title)
    eid = frontmatter.parse_server_id(created.get("id"))
    meta["id"] = eid
    frontmatter.atomic_write(path, frontmatter.render(meta, ""))
    mark_created(client.root, args.entity, eid, team)
    remote = client.get(args.entity, eid)
    state.save(
        client.root,
        args.entity,
        str(eid),
        {
            "remote_base": remote.get("body", ""),
            "local_base": "",
            "team": team,
            "path": str(path.resolve()),
        },
    )
    print(f"created {args.entity}/{eid}: {path}")
    url = remote.get("sharelink")
    if url:
        print(f"  → {url}")


def _clone(args) -> int:
    if (args.id is None) == (args.all_root is None):
        raise ValueError("clone needs exactly one of <id> or --all")
    if args.all_root is not None:
        return _clone_all(args)
    _clone_to(args.id, args.entity or "experiments", Path(args.output), args.profile)
    return 0


def _every(client, entity: str, scope: str):
    # Page until an empty answer: a server that caps the page size below ours
    # must not end the walk early.
    offset = 0
    while True:
        rows = client.search(
            entity,
            {"scope": browse.SCOPES[scope], "limit": SEARCH_PAGE, "offset": offset},
        )
        if not rows:
            return
        yield from rows
        offset += len(rows)


def _clone_all(args) -> int:
    root = Path(args.all_root)
    data = config.load(Path.cwd(), root)
    profile_name, client = _resolved_client(data, args.profile, {})
    present = set()
    for path in _documents(root)[0] if root.is_dir() else []:
        meta = frontmatter.document_meta(path) or {}
        present.add(
            (meta.get("profile"), meta.get("entity", "experiments"), meta.get("id"))
        )
    report = _Report()
    for entity in [args.entity] if args.entity else ENTITIES:
        for row in _every(client, entity, args.scope):
            eid = frontmatter.parse_server_id(row.get("id"))
            if (profile_name, entity, eid) in present:
                report.counts["·"] += 1
                continue
            saved = state.load(client.root, entity, str(eid))
            if saved is not None:
                where = saved.get("path") or "an unrecorded path"
                report.line("·", f"{entity}/{eid}  working copy at {where}")
                continue
            directory = root / entity / _directory_name(eid, row.get("title") or "")
            path = directory / "report.md"

            def work(eid=eid, entity=entity, directory=directory, path=path):
                directory.mkdir(parents=True, exist_ok=True)
                try:
                    return _clone_to(eid, entity, path, profile_name)
                except BaseException:
                    if not any(directory.iterdir()):
                        directory.rmdir()
                    raise

            report.run(str(path), f"{entity}/{eid}", work)
    return report.finish(0)


def _clone_to(eid, entity: str, path: Path, profile: str | None) -> str:
    eid = frontmatter.validate_id(eid)
    if path.exists():
        raise RuntimeError(f"output already exists: {path}")
    if not path.parent.is_dir():
        raise RuntimeError(f"output parent does not exist: {path.parent}")
    meta = {"id": eid, "entity": entity}
    data = config.load(Path.cwd(), path.parent)
    resolved_profile, client = _resolved_client(data, profile, meta)
    meta["profile"] = resolved_profile
    # The base is keyed by entity, not by file: a second clone would reset the
    # base of whatever working copy already holds it, so refuse instead.
    base = state.path(client.root, entity, str(eid))
    if base.exists():
        raise RuntimeError(
            f"{entity}/{eid} already has a working copy; pull there instead "
            f"(to start over, delete its base: {base})"
        )
    frontmatter.atomic_write(path, frontmatter.render(meta, ""))
    try:
        pull(path, client, data, profile)
    except BaseException:
        # Drop the scaffold so the clone can be retried, unless the pull got far
        # enough to record a base: then the document is real, whatever failed after.
        if not base.exists():
            path.unlink(missing_ok=True)
        raise
    return "cloned"


def _color(text: str, code: int) -> str:
    if sys.stdout.isatty() and os.environ.get("NO_COLOR") is None:
        return f"\033[{code}m{text}\033[0m"
    return text


def _browse_target(args) -> tuple[str, Client]:
    data = config.load(Path.cwd(), Path.cwd())
    entity = args.entity or data.get("entity", "experiments")
    if entity not in ENTITIES:
        raise ValueError("entity must be one of: experiments, items")
    return entity, _resolved_client(data, args.profile, {})[1]


def _whoami(profile: str | None) -> None:
    data = config.load(Path.cwd(), Path.cwd())
    profile_name, client = _resolved_client(data, profile, {})
    identity = client.me()
    server = client.info()
    api_keys = client.apikeys()

    name = identity.get("fullname")
    if not name:
        name = " ".join(
            part
            for part in (identity.get("firstname"), identity.get("lastname"))
            if part
        )
    name = name or identity.get("email") or identity.get("userid")
    userid = identity.get("userid")

    host = client.root.split("://", 1)[-1]
    heading = f"{_color('✓', 32)} {host}"
    if name:
        heading += f" — {name}"
    if userid is not None:
        heading += f" (uid {userid})"
    print(heading)
    print(f"  Profile   {profile_name}")

    email = identity.get("email")
    if email:
        print(f"  Email     {email}")

    team_id = identity.get("team")
    teams = identity.get("teams") or []
    team = next((item for item in teams if str(item.get("id")) == str(team_id)), {})
    team_name = team.get("name")
    if team_name and team_id is not None:
        roles = {1: "sysadmin", 2: "admin", 4: "user"}
        role = roles.get(team.get("usergroup"))
        if role is None:
            if team.get("is_owner"):
                role = "owner"
            elif team.get("is_admin"):
                role = "admin"
            else:
                role = "user"
        role_colors = {"sysadmin": 31, "owner": 36, "admin": 35, "user": 32}
        role_display = _color(role, role_colors[role])
        if identity.get("is_sysadmin") and role != "sysadmin":
            role_display += f" · {_color('sysadmin', 31)}"
        print(f"  Team      {team_name} (#{team_id}) · {role_display}")

    match = re.match(r"^(\d+)-", client.key)
    if match:
        key_id = match.group(1)
        api_key = next(
            (item for item in api_keys if str(item.get("id")) == key_id), None
        )
        can_write = api_key.get("can_write") if api_key else None
        if can_write is not None:
            key_label = "read-write" if can_write else "read-only"
            print(f"  API key   {_color(key_label, 32 if can_write else 31)}")

    server_version = server.get("elabftw_version")
    if server_version:
        print(f"  Server    eLabFTW {server_version}")

    scope_experiments = identity.get("scope_experiments")
    scope_items = identity.get("scope_items")
    if scope_experiments is not None and scope_items is not None:
        scopes = {1: "self", 2: "team", 3: "everything"}
        experiments_label = scopes.get(scope_experiments, str(scope_experiments))
        items_label = scopes.get(scope_items, str(scope_items))
        print(f"  Scopes    experiments={experiments_label} · items={items_label}")


def _profile(action: str, name: str | None) -> None:
    data = config.load(Path.cwd(), Path.cwd())
    profiles = data.get("profiles", {})
    if action == "use":
        if not name:
            raise ValueError("elport profile use requires a profile name")
        if name not in profiles:
            raise ValueError(f"no such profile: {name}")
        config.set_default(name)
        print(f"default profile is now {name}")
        return
    if not profiles:
        print("no profiles configured; run 'elport login <name>'")
        return
    default = data.get("default_profile")
    for profile_name, values in profiles.items():
        marker = "* " if profile_name == default else "  "
        base_url = values.get("base_url") or ""
        print(f"{marker}{profile_name}  {base_url}".rstrip())


def _run(args, path: Path) -> str | None:
    data, client = _target(path, args.profile, args.entity)
    if args.cmd == "push":
        return push(
            path, client, data, args.profile, args.dry_run, args.force, args.yes
        )
    if args.cmd == "pull":
        return pull(path, client, data, args.profile)
    if args.cmd == "fetch":
        return fetch(path, client, data, args.profile)
    if args.cmd == "status":
        return status(path, client, data, args.profile)
    return diff(path, client, data, args.profile, args.base)


def _documents(root: Path) -> tuple[list[Path], int]:
    """elport documents under ``root``, and how many other Markdown files were
    passed over. Hidden paths and conflict sidecars are not counted at all."""
    found, skipped = [], 0
    for path in sorted(root.rglob("*.md")):
        relative = path.relative_to(root)
        if any(part.startswith(".") for part in relative.parts):
            continue
        if path.name.endswith(SIDECARS) or not path.is_file():
            continue
        if frontmatter.document_meta(path) is None:
            skipped += 1
        else:
            found.append(path)
    return found, skipped


def _expand(paths: list[Path]) -> tuple[list[Path], int]:
    """A file stays as given; a directory becomes the documents under it."""
    found, skipped, seen = [], 0, set()
    for path in paths:
        if path.is_dir():
            inside, passed = _documents(path)
            skipped += passed
        else:
            inside = [path]
        for document in inside:
            if document.resolve() not in seen:
                seen.add(document.resolve())
                found.append(document)
    return found, skipped


def _directory_name(eid: int, title: str) -> str:
    name = unicodedata.normalize("NFC", title)
    name = re.sub(r'[/\\:*?"<>|\x00-\x1f\x7f]', "_", name)
    name = name.strip().lstrip(".").strip()[:TITLE_LIMIT].rstrip(" .")
    return f"{eid}-{name}" if name else str(eid)


class _Report:
    """One line per document, a tally at the end; failures never stop the run."""

    def __init__(self):
        self.counts: Counter[str] = Counter()

    def line(self, mark: str, text: str) -> None:
        self.counts[mark] += 1
        print(f"{mark} {text}")

    def run(
        self, label: str, ident: str, work: Callable[[], str | None], mark: str = "✓"
    ) -> str:
        """Run one document's command with its own output captured, so a prompt
        can never wait unseen (sync refuses to prompt without a terminal).
        Returns the captured stdout; a None summary prints no line."""
        out, err = io.StringIO(), io.StringIO()
        try:
            with redirect_stdout(out), redirect_stderr(err):
                summary = work()
        except (ValueError, RuntimeError, OSError) as error:
            self.line("✗", f"{label}  {ident}  {error}")
        else:
            if summary is not None:
                self.line(
                    "·" if summary == sync.UP_TO_DATE else mark,
                    f"{label}  {ident}  {summary}",
                )
        for text in err.getvalue().splitlines():
            print(f"    {text}", file=sys.stderr)
        return out.getvalue()

    def finish(self, skipped: int) -> int:
        parts = [
            f"{self.counts[mark]} {word}"
            for mark, word in _WORDS.items()
            if self.counts[mark]
        ]
        if skipped:
            parts.append(f"{skipped} skipped")
        if parts:
            print(", ".join(parts))
        return 1 if self.counts["✗"] else 0


def _ident(path: Path) -> str:
    try:
        meta = frontmatter.parse(path.read_text(encoding="utf-8"))[0]
    except (OSError, UnicodeDecodeError, ValueError):
        meta = {}
    return f"{meta.get('entity', 'experiments')}/{meta.get('id', '-')}"


def _batch(args, paths: list[Path]) -> int:
    if getattr(args, "force", False):
        # --force discards Web-side edits; that is never a bulk decision.
        raise ValueError("--force works on one document at a time")
    documents, skipped = _expand(paths)
    if not documents:
        where = ", ".join(str(path) for path in paths)
        raise RuntimeError(f"no elport documents under {where} ({skipped} skipped)")
    report = _Report()
    mark = "*" if args.cmd == "status" else "✓"
    for path in documents:
        output = report.run(
            str(path), _ident(path), lambda path=path: _run(args, path), mark
        )
        # diff and a dry run exist to be read; everything else is the one line.
        if output and (args.cmd == "diff" or getattr(args, "dry_run", False)):
            print(f"=== {path}")
            print(output, end="")
    return report.finish(skipped)


def main(argv=None) -> int:
    args = _parser().parse_args(argv)
    try:
        if args.cmd == "login":
            base_url = input("base_url: ").strip()
            api_key = getpass.getpass("api_key: ")
            config.login(args.profile, base_url, api_key)
            return 0
        if args.cmd == "logout":
            removed = config.logout(args.profile)
            if removed:
                print(f"logged out profile {args.profile}")
            else:
                print(f"no stored credentials for profile {args.profile}")
            return 0
        if args.cmd == "profile":
            _profile(args.action, args.name)
            return 0
        if args.cmd == "new":
            _new(args)
            return 0
        if args.cmd == "clone":
            return _clone(args)
        if args.cmd == "list":
            entity, client = _browse_target(args)
            browse.entities(
                client,
                entity,
                scope=args.scope,
                limit=args.limit,
                offset=args.offset,
                query=args.query,
                as_json=args.json,
            )
            return 0
        if args.cmd == "view":
            entity, client = _browse_target(args)
            browse.view(client, entity, args.id)
            return 0
        if args.cmd == "whoami":
            _whoami(args.profile)
            return 0
        if args.cmd == "merge":
            path = Path(args.doc)
            data, _ = _target(path, args.profile, args.entity)
            merge(path, data, args.profile, args.resolved)
            return 0

        if args.cmd in ("comments", "comment"):
            path = Path(args.doc)
            data, client = _target(path, args.profile, args.entity)
            if args.cmd == "comments":
                comments(path, client, data, args.profile)
            else:
                comment(path, client, data, args.profile, text=args.text)
            return 0

        paths = [Path(doc) for doc in args.doc]
        if len(paths) == 1 and not paths[0].is_dir():
            _run(args, paths[0])
            return 0
        return _batch(args, paths)
    except (ValueError, RuntimeError, OSError) as error:
        print(str(error), file=sys.stderr)
        return 1


if __name__ == "__main__":
    raise SystemExit(main())
