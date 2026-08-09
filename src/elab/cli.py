from __future__ import annotations

import argparse
import getpass
import sys
from pathlib import Path

from . import config, frontmatter, state
from .client import Client
from .sync import diff, pull, push, status

ENTITIES = ("experiments", "items")


def _add_target_options(parser: argparse.ArgumentParser) -> None:
    parser.add_argument("--profile")
    parser.add_argument("--entity", choices=ENTITIES)


def _parser() -> argparse.ArgumentParser:
    parser = argparse.ArgumentParser(prog="elab")
    commands = parser.add_subparsers(dest="cmd", required=True)

    push_parser = commands.add_parser("push")
    push_parser.add_argument("doc", nargs="?", default="report.md")
    _add_target_options(push_parser)
    push_parser.add_argument("-n", "--dry-run", action="store_true")
    push_parser.add_argument("-f", "--force", action="store_true")

    pull_parser = commands.add_parser("pull")
    pull_parser.add_argument("doc", nargs="?", default="report.md")
    _add_target_options(pull_parser)

    status_parser = commands.add_parser("status")
    status_parser.add_argument("doc", nargs="?", default="report.md")
    _add_target_options(status_parser)

    diff_parser = commands.add_parser("diff")
    diff_parser.add_argument("doc", nargs="?", default="report.md")
    _add_target_options(diff_parser)
    diff_parser.add_argument("--base", action="store_true")

    new_parser = commands.add_parser("new")
    new_parser.add_argument("title")
    new_parser.add_argument("--entity", choices=ENTITIES, default="experiments")
    new_parser.add_argument("--profile")
    new_parser.add_argument("-o", "--output", default="report.md")

    whoami_parser = commands.add_parser("whoami")
    whoami_parser.add_argument("--profile")

    login_parser = commands.add_parser("login")
    login_parser.add_argument("profile", nargs="?", default="default")
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


def _whoami(profile: str | None) -> None:
    data = config.load(Path.cwd(), Path.cwd())
    identity = _client(data, profile, {}).me()
    name = identity.get("fullname")
    if not name:
        name = " ".join(
            part
            for part in (identity.get("firstname"), identity.get("lastname"))
            if part
        )
    name = name or identity.get("email") or identity.get("userid") or "unknown"
    team_id = identity.get("team")
    teams = identity.get("teams") or []
    team = next((item for item in teams if str(item.get("id")) == str(team_id)), {})
    team_name = team.get("name") or team.get("title")
    team_display = f"{team_name} ({team_id})" if team_name else str(team_id)
    print(f"user: {name}")
    print(f"active team: {team_display}")


def main(argv=None) -> int:
    args = _parser().parse_args(argv)
    try:
        if args.cmd == "login":
            base_url = input("base_url: ").strip()
            api_key = getpass.getpass("api_key: ")
            config.login(args.profile, base_url, api_key)
            return 0
        if args.cmd == "new":
            _new(args)
            return 0
        if args.cmd == "whoami":
            _whoami(args.profile)
            return 0

        path = Path(args.doc)
        data, client = _target(path, args.profile, args.entity)
        if args.cmd == "push":
            push(path, client, data, args.profile, args.dry_run, args.force)
        elif args.cmd == "pull":
            pull(path, client, data, args.profile)
        elif args.cmd == "status":
            status(path, client, data, args.profile)
        elif args.cmd == "diff":
            diff(path, client, data, args.profile, args.base)
        return 0
    except (ValueError, RuntimeError, OSError) as error:
        print(str(error), file=sys.stderr)
        return 1


if __name__ == "__main__":
    raise SystemExit(main())
