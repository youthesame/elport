from __future__ import annotations

import argparse
import getpass
import os
import re
import sys
from importlib.metadata import version as _pkg_version
from pathlib import Path

from . import config, frontmatter, state
from .client import Client
from .sync import comment, comments, diff, merge, pull, push, status

ENTITIES = ("experiments", "items")

_EXAMPLES = """\
examples:
  elab login                       store credentials for the default profile
  elab new "Cell viability assay"  create a remote entity and report.md
  elab push                        upload report.md (add -n to preview first)
  elab status                      show local/remote sync state
  elab pull                        download the remote body and attachments
"""


def _add_target_options(parser: argparse.ArgumentParser) -> None:
    parser.add_argument("--profile", help="config profile to use")
    parser.add_argument(
        "--entity", choices=ENTITIES, help="target entity type (default: experiments)"
    )


def _parser() -> argparse.ArgumentParser:
    parser = argparse.ArgumentParser(
        prog="elab",
        description="git-like sync CLI for eLabFTW (local is authoritative)",
        epilog=_EXAMPLES,
        formatter_class=argparse.RawDescriptionHelpFormatter,
    )
    parser.add_argument(
        "--version", action="version", version=f"%(prog)s {_pkg_version('elab')}"
    )
    commands = parser.add_subparsers(dest="cmd", required=True)

    push_parser = commands.add_parser(
        "push", help="upload the local document (full-body overwrite)"
    )
    push_parser.add_argument(
        "doc",
        nargs="?",
        default="report.md",
        help="local document (default: report.md)",
    )
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
        help="skip confirmation when widening access to account or public",
    )

    pull_parser = commands.add_parser(
        "pull", help="download the remote body and attachments"
    )
    pull_parser.add_argument(
        "doc",
        nargs="?",
        default="report.md",
        help="local document (default: report.md)",
    )
    _add_target_options(pull_parser)

    status_parser = commands.add_parser("status", help="show local/remote sync state")
    status_parser.add_argument(
        "doc",
        nargs="?",
        default="report.md",
        help="local document (default: report.md)",
    )
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
    diff_parser.add_argument(
        "doc",
        nargs="?",
        default="report.md",
        help="local document (default: report.md)",
    )
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
    created = client.create(args.entity, args.title)
    eid = frontmatter.parse_server_elab_id(created.get("id"))
    meta["elab_id"] = eid
    frontmatter.atomic_write(path, frontmatter.render(meta, ""))
    remote = client.get(args.entity, eid)
    state.save(
        client.root,
        args.entity,
        str(eid),
        {
            "remote_base": remote.get("body", ""),
            "local_base": "",
            "team": client.me().get("team"),
        },
    )
    print(f"created {args.entity}/{eid}: {path}")
    url = remote.get("sharelink")
    if url:
        print(f"  → {url}")


def _color(text: str, code: int) -> str:
    if sys.stdout.isatty() and os.environ.get("NO_COLOR") is None:
        return f"\033[{code}m{text}\033[0m"
    return text


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
            raise ValueError("elab profile use requires a profile name")
        if name not in profiles:
            raise ValueError(f"no such profile: {name}")
        config.set_default(name)
        print(f"default profile is now {name}")
        return
    if not profiles:
        print("no profiles configured; run 'elab login <name>'")
        return
    default = data.get("default_profile")
    for profile_name, values in profiles.items():
        marker = "* " if profile_name == default else "  "
        base_url = values.get("base_url") or ""
        print(f"{marker}{profile_name}  {base_url}".rstrip())


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
        if args.cmd == "whoami":
            _whoami(args.profile)
            return 0
        if args.cmd == "merge":
            path = Path(args.doc)
            data, _ = _target(path, args.profile, args.entity)
            merge(path, data, args.profile, args.resolved)
            return 0

        path = Path(args.doc)
        data, client = _target(path, args.profile, args.entity)
        if args.cmd == "push":
            push(
                path,
                client,
                data,
                args.profile,
                args.dry_run,
                args.force,
                args.yes,
            )
        elif args.cmd == "pull":
            pull(path, client, data, args.profile)
        elif args.cmd == "status":
            status(path, client, data, args.profile)
        elif args.cmd == "comments":
            comments(path, client, data, args.profile)
        elif args.cmd == "comment":
            comment(path, client, data, args.profile, text=args.text)
        elif args.cmd == "diff":
            diff(path, client, data, args.profile, args.base)
        return 0
    except (ValueError, RuntimeError, OSError) as error:
        print(str(error), file=sys.stderr)
        return 1


if __name__ == "__main__":
    raise SystemExit(main())
