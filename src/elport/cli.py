from __future__ import annotations

import argparse
import datetime
import errno
import functools
import getpass
import io
import os
import re
import shutil
import stat
import sys
import unicodedata
from collections import Counter
from collections.abc import Callable
from contextlib import redirect_stderr, redirect_stdout
from importlib import resources
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
SIDECARS = (".remote.md", ".base.md")
TITLE_LIMIT = 50
_WORDS = {"✓": "done", "·": sync.UP_TO_DATE, "*": "to sync", "✗": "failed"}

HELP_TOPICS = {
    "agents": "read this first when you are an agent acting for a user",
    "workflow": "the status, diff, push loop; conflicts; pull and fetch",
    "documents": "front matter, references, attachments, links between documents",
}
_TOPICS = "help topics:\n" + "".join(
    f"  elport help {topic:<10} {summary}\n" for topic, summary in HELP_TOPICS.items()
)

_SIDE_EFFECTS = """\
side effects:
  read-only       list, view (without -o), activity, comments, status, diff,
                  whoami, profile list, push -n
  local writes    pull, fetch, clone, merge, view -o DIR, login, logout,
                  profile use
  remote writes   push (without -n; also writes locally),
                  new (also writes locally), comment

defaults & auth:
  document commands default to report.md (sync commands also accept directories)
  exit codes: 0 success, 1 failure, 2 usage error
  non-interactive auth: set both ELABFTW_BASE_URL and ELABFTW_API_KEY
"""

_EXAMPLES = """\
examples:
  elport login                       store credentials for the default profile
  elport new "Cell viability assay"  create a remote entity and report.md
  elport list                        list your own entities on the server
  elport view 357                    read an entity without cloning it
  elport view 357 -o e357/           read it with all its attachments, in a new dir
  elport clone 357                   start report.md to edit and push an existing entity
  elport activity                    heatmap of the experiments you created this year
  elport push                        upload report.md (add -n to preview first)
  elport status                      show local/remote sync state
  elport pull                        download the remote body and attachments
  elport push experiments/           push every document under experiments/
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
        description=(
            "git-like sync CLI for eLabFTW (local is authoritative).\n"
            "Agents: run `elport help agents` before acting."
        ),
        epilog=_SIDE_EFFECTS + "\n" + _TOPICS + "\n" + _EXAMPLES,
        formatter_class=argparse.RawDescriptionHelpFormatter,
    )
    parser.add_argument(
        "--version", action="version", version=f"%(prog)s {_pkg_version('elport')}"
    )
    commands = parser.add_subparsers(dest="cmd", required=True)

    help_parser = commands.add_parser(
        "help",
        help="print a help topic",
        description="print a bundled help topic (read-only)",
        epilog="example: elport help agents",
    )
    help_parser.add_argument("topic", nargs="?", choices=HELP_TOPICS)

    push_parser = commands.add_parser(
        "push",
        help="upload the local document (-n/--dry-run plans without sending)",
        description=(
            "upload referenced files and overwrite the remote body. "
            "-n/--dry-run prints the plan without sending or running confirmations"
        ),
        epilog=(
            "example: elport push -n && elport push. "
            "on conflict, run `elport diff`, then `elport pull` before `elport merge`; "
            "agents: see `elport help agents`"
        ),
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
        "pull",
        help="download the remote body and attachments",
        description=(
            "fetch the remote body, metadata, and referenced attachments into "
            "the local document (remote read-only)"
        ),
        epilog=(
            "example: elport pull. on conflict, run `elport merge`; "
            "to download unreferenced attachments too, run `elport fetch`"
        ),
    )
    _add_docs_argument(pull_parser)
    _add_target_options(pull_parser)

    fetch_parser = commands.add_parser(
        "fetch",
        help="download all remote attachments locally",
        description=(
            "download every attachment on the entity without changing the body "
            "or base (remote read-only, writes locally)"
        ),
        epilog=(
            "example: elport fetch. to read an entity and its attachments "
            "without a working copy, run `elport view <id> -o DIR`"
        ),
    )
    _add_docs_argument(fetch_parser)
    _add_target_options(fetch_parser)

    status_parser = commands.add_parser(
        "status",
        help="show local/remote sync state",
        description="show local/remote sync state, uploads, mode, and locks (read-only)",
        epilog="example: elport status. inspect changes with `elport diff` or `elport push -n`",
    )
    _add_docs_argument(status_parser)
    _add_target_options(status_parser)

    comments_parser = commands.add_parser(
        "comments",
        help="show the remote comment thread",
        description="print the remote comment thread to the terminal (read-only)",
        epilog="example: elport comments 357. post a reply with `elport comment`",
    )
    comments_parser.add_argument(
        "doc",
        nargs="?",
        default="report.md",
        help="local document, or the id or eLabFTW URL of a remote entity (default: report.md)",
    )
    _add_target_options(comments_parser)

    comment_parser = commands.add_parser(
        "comment",
        help="post a remote comment",
        description=(
            "post one comment to a document's or remote entity's thread "
            "(edit or delete in the Web UI)"
        ),
        epilog='example: elport comment "checked calibration" (or: elport comment 357 "text")',
    )
    comment_parser.add_argument(
        "doc",
        nargs="?",
        default="report.md",
        help="local document, or the id or eLabFTW URL of a remote entity (default: report.md)",
    )
    comment_parser.add_argument("text", help="comment text")
    _add_target_options(comment_parser)

    diff_parser = commands.add_parser(
        "diff",
        help="show changes against remote or base",
        description="diff the local document against remote, or against the last sync with --base (read-only)",
        epilog="example: elport diff --base. preview upload changes with `elport push -n`",
    )
    _add_docs_argument(diff_parser)
    _add_target_options(diff_parser)
    diff_parser.add_argument(
        "--base",
        action="store_true",
        help="diff against the recorded base instead of the remote",
    )

    merge_parser = commands.add_parser(
        "merge",
        help="merge local and remote sidecars using git merge-file",
        description=(
            "3-way merge .base.md and .remote.md into the local document "
            "using git merge-file (local-only; never pushes)"
        ),
        epilog=(
            "example: elport merge. resolve any conflict markers, then run `elport push`; "
            "for a hand-merged file, run `elport merge --resolved`"
        ),
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
        "new",
        help="create a new remote entity and local document",
        description=(
            "create a new remote entity (with your server's default visibility) "
            "and write a local document scaffold"
        ),
        epilog="to upload an existing document as a new entity, run: elport push <file>",
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

    list_parser = commands.add_parser(
        "list",
        help="list remote entities",
        description="list remote entities (read-only)",
        epilog="what moved in the team: --scope team --order lastchange, then "
        "--order comment (a new comment does not count as a change). "
        "--json rows carry modified_at and recent_comment (server time); "
        "read a thread with: elport comments <id>",
    )
    list_parser.add_argument(
        "--entity",
        choices=ENTITIES,
        help="entity type to list (default: experiments)",
    )
    list_parser.add_argument(
        "--scope",
        choices=tuple(browse.SCOPES),
        default="self",
        help="whose entities to list: self (yours), team (active team), "
        "all (every visible team) (default: self)",
    )
    list_parser.add_argument(
        "-q",
        "--query",
        help="search term matched against title, body and elabid, or fields such as "
        '\'author:"Jane Doe" category:"Assay" status:"Success"\'',
    )
    list_parser.add_argument(
        "--tag",
        action="append",
        dest="tags",
        metavar="TAG",
        help="only entities with this tag (repeat to require several)",
    )
    list_parser.add_argument(
        "--order",
        choices=browse.ORDERS,
        help="sort by this field (lastchange and comment show their time)",
    )
    list_parser.add_argument(
        "--sort", choices=("asc", "desc"), help="sort direction (default: desc)"
    )
    size = list_parser.add_mutually_exclusive_group()
    size.add_argument(
        "--limit",
        type=int,
        default=browse.DEFAULT_LIMIT,
        help=f"maximum number of results (default: {browse.DEFAULT_LIMIT})",
    )
    size.add_argument("--all", action="store_true", help="page through every result")
    list_parser.add_argument(
        "--offset", type=int, default=0, help="skip this many results"
    )
    list_parser.add_argument(
        "--json", action="store_true", help="emit the raw API rows as JSON"
    )
    list_parser.add_argument("--profile", help="config profile to use")

    activity_parser = commands.add_parser(
        "activity",
        help="heatmap of entities created per day, up to a year",
        description="print a terminal heatmap of entities created per day, up to a year (read-only)",
        epilog="raw rows as JSON: elport list --all --json [--scope team]",
    )
    activity_parser.add_argument(
        "--team", action="store_true", help="one heatmap per author in your team"
    )
    activity_parser.add_argument(
        "--entity",
        choices=ENTITIES,
        help="entity type to count (default: experiments)",
    )
    activity_parser.add_argument("--profile", help="config profile to use")

    view_parser = commands.add_parser(
        "view",
        help="read a remote entity (-o DIR saves a local snapshot)",
        description=(
            "read a remote entity (read-only; -o DIR writes a local snapshot, "
            "not a working copy)"
        ),
        epilog=(
            "stdout holds the body (or saved paths with -o DIR); "
            "links and items outside the body print to stderr. "
            "example: elport view 357 -o e357/"
        ),
    )
    view_parser.add_argument(
        "id",
        metavar="ID_OR_URL",
        help="id or eLabFTW URL of the remote entity",
    )
    view_parser.add_argument(
        "--entity",
        choices=ENTITIES,
        help="entity type to view (default: experiments)",
    )
    view_parser.add_argument(
        "-o",
        "--output",
        metavar="DIR",
        help="write the body and all its attachments into DIR (new or empty) "
        "instead of printing it",
    )
    view_parser.add_argument("--profile", help="config profile to use")

    clone_parser = commands.add_parser(
        "clone",
        help="start a local document from an existing entity (to read, use view)",
        description=(
            "start a local document to edit and push an existing remote entity "
            "(writes locally; to only read one, use view)"
        ),
        epilog=(
            "example: elport clone 357 (or: elport clone --all backup/ to mirror). "
            "HTML entities are read-only; read them with `elport view <id> -o DIR`"
        ),
    )
    clone_parser.add_argument(
        "id",
        nargs="?",
        metavar="ID_OR_URL",
        help="id or eLabFTW URL of the remote entity",
    )
    clone_parser.add_argument(
        "--all",
        dest="all_root",
        metavar="ROOT",
        help="clone every entity not yet under ROOT into "
        "ROOT/<entity>/<id>-<title>/report.md (HTML entities become read-only snapshots)",
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
        "whoami",
        help="show the authenticated user and active team",
        description="check authentication, team, key permissions, server version, and scopes (read-only)",
        epilog="if unauthenticated, run `elport login` or set ELABFTW_BASE_URL and ELABFTW_API_KEY",
    )
    whoami_parser.add_argument("--profile", help="config profile to use")

    login_parser = commands.add_parser(
        "login",
        help="store base_url and api_key for a profile",
        description=(
            "interactively store base_url in config.toml and api_key in the OS "
            "keyring (falling back to config.toml without a keyring)"
        ),
        epilog="for non-interactive runs, set both ELABFTW_BASE_URL and ELABFTW_API_KEY instead",
    )
    login_parser.add_argument(
        "profile", nargs="?", default="default", help="profile name (default: default)"
    )
    logout_parser = commands.add_parser(
        "logout",
        help="remove the stored api_key for a profile",
        description=(
            "remove the stored api_key from the OS keyring and config.toml, "
            "clearing default_profile if it names this profile (keeps base_url)"
        ),
        epilog="example: elport logout labA",
    )
    logout_parser.add_argument(
        "profile", nargs="?", default="default", help="profile name (default: default)"
    )
    profile_parser = commands.add_parser(
        "profile",
        help="list profiles or select the default with 'use'",
        description="list configured profiles (read-only), or set the default profile with `profile use <name>`",
        epilog="example: elport profile use labA",
    )
    profile_parser.add_argument(
        "action", nargs="?", choices=["list", "use"], default="list"
    )
    profile_parser.add_argument("name", nargs="?")
    return parser


@functools.cache
def _resolve_profile(
    name: str,
    base_url: str | None,
    verify_ssl: bool,
    env_url: str | None,
    env_key: str | None,
) -> tuple[str, Client]:
    prof: dict = {"verify_ssl": verify_ssl}
    if base_url is not None:
        prof["base_url"] = base_url
    resolved_name, url, api_key, verify = config.resolve(
        {"profiles": {name: prof}}, name, {}
    )
    return resolved_name, _connect(url, api_key, verify)


def _resolved_client(data: dict, profile: str | None, meta: dict) -> tuple[str, Client]:
    name = config._profile_name(data, profile, meta)
    prof = data.get("profiles", {}).get(name, {})
    return _resolve_profile(
        name,
        prof.get("base_url"),
        prof.get("verify_ssl", True),
        os.getenv("ELABFTW_BASE_URL"),
        os.getenv("ELABFTW_API_KEY"),
    )


@functools.cache
def _connect(base_url: str, api_key: str, verify_ssl: bool) -> Client:
    # One client per server for the whole command, so a folder of documents
    # shares its connection and identity. main() clears it per command.
    return Client(base_url, api_key, verify_ssl)


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
        raise RuntimeError(
            f"output already exists: {path}\n"
            f"to create an entity from an existing note, run: elport push {path}"
        )
    if not path.parent.is_dir():
        raise RuntimeError(f"output parent does not exist: {path.parent}")
    meta = {"title": args.title, "entity": args.entity}
    data = config.load(Path.cwd(), path.parent)
    resolved_profile, client = _resolved_client(data, args.profile, meta)
    meta["profile"] = resolved_profile
    frontmatter.atomic_write(path, frontmatter.render(meta, ""))
    created = client.create(args.entity, args.title)
    eid = frontmatter.parse_server_id(created.get("id"))
    meta["id"] = eid
    frontmatter.atomic_write(path, frontmatter.render(meta, ""))
    mark_created(client.root, args.entity, eid)
    remote = client.get(args.entity, eid)
    state.save(
        client.root,
        args.entity,
        str(eid),
        {
            "remote_base": remote.get("body", ""),
            "local_base": "",
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
    _clone_to(args.id, args.entity, Path(args.output), args.profile)
    return 0


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
        # A snapshot has no front matter, so its directory is what marks it as
        # present (ADR-0010); the name starts with the id, whatever the title.
        folder = root / entity
        taken = (
            {p.name for p in folder.iterdir() if p.is_dir()}
            if folder.is_dir()
            else set()
        )
        for row in browse.every(client, entity, args.scope):
            eid = frontmatter.parse_server_id(row.get("id"))
            if (profile_name, entity, eid) in present or any(
                name == str(eid) or name.startswith(f"{eid}-") for name in taken
            ):
                report.counts["·"] += 1
                continue
            saved = state.load(client.root, entity, str(eid))
            if saved is not None:
                report.line("·", f"{entity}/{eid}  working copy at {_where(saved)}")
                continue
            directory = root / entity / _directory_name(eid, row.get("title") or "")
            path = directory / "report.md"

            def work(eid=eid, entity=entity, directory=directory, path=path):
                # New by construction: a directory named by this id was skipped.
                directory.mkdir(parents=True)
                try:
                    try:
                        return _clone_to(eid, entity, path, profile_name)
                    except sync.HtmlEntityError:
                        sync.snapshot(client, entity, eid, directory, client.root)
                        return "snapshot (HTML entity, read-only)"
                except BaseException:
                    # Without a base nothing here is a working copy, only a partial
                    # download whose directory would count as present on re-runs.
                    if not state.path(client.root, entity, str(eid)).exists():
                        shutil.rmtree(directory)
                    raise

            report.run(str(path), f"{entity}/{eid}", work)
    return report.finish(0)


def _where(saved: dict) -> str:
    return saved.get("path") or "an unrecorded path"


def _is_existing_file(text: str) -> bool:
    try:
        return stat.S_ISREG(Path(text).stat().st_mode)
    except OSError as error:
        if error.errno in {
            errno.ENOENT,
            errno.ENOTDIR,
            errno.ENAMETOOLONG,
            errno.EINVAL,
        }:
            return False
        raise OSError("cannot inspect local target path") from None


def _is_remote_target(text: str) -> bool:
    stripped = text.lstrip()
    return (
        text.isdigit()
        or "://" in text
        or stripped.startswith("//")
        or stripped.casefold().startswith(("http:", "https:"))
    )


def _parse_remote_target(
    raw: object, explicit_entity: str | None, default_entity: str, client
) -> tuple[str, int]:
    text = str(raw)
    if "://" in text:
        entity, eid = sync.parse_entity_url(text, client.root)
        if explicit_entity and explicit_entity != entity:
            raise ValueError(
                f"entity mismatch between URL ({entity}) and --entity ({explicit_entity})"
            )
        return entity, eid
    try:
        eid = frontmatter.validate_id(int(text))
    except ValueError as error:
        if text.lstrip("-").isdigit():
            raise
        raise ValueError("invalid entity id or URL") from error
    return explicit_entity or default_entity, eid


def _clone_to(
    target, explicit_entity: str | None, path: Path, profile: str | None
) -> str:
    if path.exists():
        raise RuntimeError(f"output already exists: {path}")
    if not path.parent.is_dir():
        raise RuntimeError(f"output parent does not exist: {path.parent}")
    data = config.load(Path.cwd(), path.parent)
    resolved_profile, client = _resolved_client(data, profile, {})
    entity, eid = _parse_remote_target(target, explicit_entity, "experiments", client)
    meta = {"id": eid, "entity": entity, "profile": resolved_profile}
    # The base is keyed by entity, not by file: a second clone would reset the
    # base of whatever working copy already holds it, so refuse instead.
    base = state.path(client.root, entity, str(eid))
    if base.exists():
        saved = state.load(client.root, entity, str(eid)) or {}
        raise RuntimeError(
            f"{entity}/{eid} already has a working copy at {_where(saved)}; "
            f"pull there instead (to start over, delete its base: {base})"
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


def _colors() -> bool:
    return sys.stdout.isatty() and os.environ.get("NO_COLOR") is None


def _color(text: str, code: int) -> str:
    return f"\033[{code}m{text}\033[0m" if _colors() else text


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


_MIGRATION_NOTICE = (
    "notice: elport v0.2.0+ is now a standalone Go binary "
    "(this Python package is maintenance-only). "
    "Upgrade: https://github.com/youthesame/elport#install"
)


def _maybe_emit_migration_notice() -> None:
    if os.environ.get("ELPORT_NO_MIGRATION_NOTICE") == "1":
        return
    if sys.stderr is not None and sys.stderr.isatty():
        print(_MIGRATION_NOTICE, file=sys.stderr)


def main(argv=None) -> int:
    # Windows encodes a piped stdout with the locale code page, which cannot hold
    # most titles, names or emoji; whoever reads elport's output gets UTF-8.
    for stream in (sys.stdout, sys.stderr):
        if isinstance(stream, io.TextIOWrapper):
            stream.reconfigure(encoding="utf-8")
    args = _parser().parse_args(argv)
    _connect.cache_clear()
    _resolve_profile.cache_clear()
    if args.cmd == "help":
        if args.topic is None:
            print(_TOPICS, end="")
        else:
            topic = resources.files("elport") / "help" / f"{args.topic}.md"
            sys.stdout.write(topic.read_text("utf-8"))
        return 0
    _maybe_emit_migration_notice()
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
                limit=None if args.all else args.limit,
                offset=args.offset,
                query=args.query,
                as_json=args.json,
                order=args.order,
                sort=args.sort,
                tags=args.tags,
            )
            return 0
        if args.cmd == "activity":
            entity, client = _browse_target(args)
            rows = browse.every(client, entity, "team" if args.team else "self")
            today = datetime.datetime.now().astimezone().date()
            width = shutil.get_terminal_size().columns
            print(browse.activity(rows, entity, today, _colors(), width), end="")
            return 0
        if args.cmd == "view":
            default_entity, client = _browse_target(args)
            entity, eid = _parse_remote_target(
                args.id, args.entity, default_entity, client
            )
            output = Path(args.output) if args.output else None
            sync.view(client, entity, eid, client.root, output)
            return 0
        if args.cmd == "whoami":
            _whoami(args.profile)
            return 0
        if args.cmd == "merge":
            path = Path(args.doc)
            data, _ = _target(path, args.profile, args.entity)
            merge(path, data, args.profile, args.resolved)
            return 0

        if (
            args.cmd in ("comments", "comment")
            and _is_remote_target(args.doc)
            and not (args.cmd == "comment" and _is_existing_file(args.doc))
        ):
            if args.cmd == "comment" and not args.text.strip():
                raise RuntimeError("comment text is empty")
            default_entity, client = _browse_target(args)
            entity, eid = _parse_remote_target(
                args.doc, args.entity, default_entity, client
            )
            if args.cmd == "comments":
                sync.show_comments(client, entity, eid)
            else:
                sync.post_comment(client, entity, eid, args.text)
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
