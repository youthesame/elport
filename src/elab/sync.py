from __future__ import annotations

import difflib
import errno
import hashlib
import os
import shlex
import sys
import tempfile
from dataclasses import dataclass
from pathlib import Path
from typing import Any

from . import config as config_module
from . import frontmatter, state
from .transclude import download_url, plan, replace_spans, reverse, safe_name

LARGE_UPLOAD_BYTES = 25 * 1024 * 1024
CONTROL_FILENAMES = {".elab.toml", ".elabignore"}


@dataclass
class Remote:
    """Binds an entity's identity (client, entity, eid, base_url) so helpers and
    call sites stop threading those four values through every signature."""

    client: Any
    entity: str
    eid: object
    base_url: str

    def get(self):
        return self.client.get(self.entity, self.eid)

    def uploads(self):
        return self.client.uploads(self.entity, self.eid)

    def upload(self, path):
        return self.client.upload(self.entity, self.eid, path)

    def download(self, uid):
        return self.client.download(self.entity, self.eid, uid)

    def patch(self, payload):
        return self.client.patch(self.entity, self.eid, payload)


def _upload_name(upload: dict) -> str:
    return safe_name(str(upload.get("real_name", "attachment")))


def _is_control_upload(upload: dict) -> bool:
    return _upload_name(upload).casefold() in CONTROL_FILENAMES


def _reversible_uploads(uploads: list[dict]) -> list[dict]:
    return [upload for upload in uploads if not _is_control_upload(upload)]


def ignore_patterns(doc_dir: Path, config: dict) -> list[str]:
    ignore_file = doc_dir / ".elabignore"
    file_patterns = (
        ignore_file.read_text(encoding="utf-8").splitlines()
        if ignore_file.exists()
        else []
    )
    return [
        pattern.strip()
        for pattern in file_patterns + config.get("ignore", [])
        if pattern.strip() and not pattern.startswith("#")
    ]


def _sha256(path: Path) -> str:
    digest = hashlib.sha256()
    with path.open("rb") as stream:
        for chunk in iter(lambda: stream.read(1024 * 1024), b""):
            digest.update(chunk)
    return digest.hexdigest()


def _remote_sha256(upload: dict) -> str | None:
    if str(upload.get("hash_algorithm", "")).lower() != "sha256":
        return None
    value = upload.get("hash") or upload.get("sha256")
    return str(value).lower() if value else None


def _matching_upload(path: Path, uploads: list[dict]) -> dict | None:
    candidates = [u for u in uploads if u.get("real_name") == path.name]
    local_hash = None
    for upload in candidates:
        remote_hash = _remote_sha256(upload)
        if remote_hash:
            local_hash = local_hash or _sha256(path)
            if remote_hash == local_hash:
                return upload
    for upload in candidates:
        if (
            _remote_sha256(upload) is None
            and upload.get("filesize") == path.stat().st_size
        ):
            return upload
    return None


def _complete_upload(remote: Remote, uploaded: dict) -> dict:
    if all(key in uploaded for key in ("long_name", "real_name", "storage")):
        return uploaded
    upload_id = str(uploaded.get("id", ""))
    refreshed = remote.uploads()
    match = next((u for u in refreshed if str(u.get("id", "")) == upload_id), None)
    if match is None:
        raise RuntimeError("uploaded file metadata could not be retrieved")
    return match


def _confirm_large_uploads(paths: list[Path]) -> None:
    large = [path for path in paths if path.stat().st_size > LARGE_UPLOAD_BYTES]
    if not large:
        return
    names = ", ".join(f"{path.name} ({path.stat().st_size} bytes)" for path in large)
    if not sys.stdin.isatty():
        raise RuntimeError(f"large upload requires interactive confirmation: {names}")
    answer = input(f"Upload large file(s) {names}? [y/N] ").strip().lower()
    if answer not in ("y", "yes"):
        raise RuntimeError("large upload cancelled")


def _resolve_category(client, entity: str, team_id, category):
    if category is None:
        return None
    if isinstance(category, int) or (isinstance(category, str) and category.isdigit()):
        return int(category)
    categories = client.categories(team_id, entity)
    match = next((item for item in categories if item.get("title") == category), None)
    if match is None:
        raise RuntimeError(f"category not found: {category}")
    return match["id"]


def _remote_tags(remote: dict) -> set[str]:
    value = remote.get("tags") or ""
    if isinstance(value, str):
        return {tag.strip() for tag in value.split(",") if tag.strip()}
    return {str(tag).strip() for tag in value if str(tag).strip()}


def _document(path: Path, config: dict) -> tuple[dict, str, str, object]:
    meta, body = frontmatter.parse(path.read_text(encoding="utf-8"))
    entity = meta.get("entity", config.get("entity", "experiments"))
    return meta, body, entity, meta.get("elab_id")


def _check_team_match(saved: dict | None, identity: dict) -> None:
    if (
        saved
        and saved.get("team") is not None
        and identity.get("team") != saved.get("team")
    ):
        raise RuntimeError("active team differs from saved state")


def push(
    path: Path,
    client,
    config: dict,
    profile=None,
    dry_run: bool = False,
    force: bool = False,
) -> None:
    meta, body, entity, eid = _document(path, config)
    for key in ("entity", "category"):
        if key in meta and meta[key] is not None:
            meta[key] = str(meta[key])
    if "tags" in meta:
        meta["tags"] = [str(tag) for tag in meta["tags"]]
    resolved_profile, base_url, _, _ = config_module.resolve(config, profile, meta)

    refs = plan(body, path.parent, ignore_patterns(path.parent, config))
    files = sorted({ref.file for ref in refs if ref.file})

    remote = Remote(client, entity, eid, base_url) if eid else None
    saved = state.load(base_url, entity, str(eid)) if eid else None
    identity = client.me()
    _check_team_match(saved, identity)
    category = _resolve_category(
        client, entity, identity.get("team"), meta.get("category")
    )
    remote_doc = remote.get() if remote is not None else {}
    if eid and saved is None and not force:
        raise RuntimeError("base unavailable; run pull first or use --force")
    if (
        remote is not None
        and saved
        and remote_doc.get("body", "") != saved.get("remote_base", "")
        and not force
    ):
        message = "remote changed; use pull or --force"
        if dry_run:
            raise RuntimeError(message)
        _raise_conflict(
            path,
            remote_doc.get("body", ""),
            saved,
            remote.uploads(),
            remote,
            message,
        )

    uploads = remote.uploads() if remote is not None else []
    reused = {path: _matching_upload(path, uploads) for path in files}
    new_uploads = [path for path, upload in reused.items() if upload is None]
    if dry_run:
        print("Upload plan:", *(path.name for path in files), sep="\n  ")
        for ref in refs:
            if ref.file is None:
                continue
            file_path = ref.file
            upload = reused[file_path]
            if upload is not None:
                target = download_url(base_url, upload)
            else:
                target = f"UPLOAD_PENDING:{file_path.name}"
            target += ref.fragment
            print(f"{ref.path} -> {target}")
        return

    _confirm_large_uploads(new_uploads)

    if not eid:
        created = client.create(entity, meta.get("title", path.stem))
        eid = frontmatter.parse_server_elab_id(created.get("id"))
        meta["elab_id"] = eid
        meta["entity"] = entity
        meta["profile"] = resolved_profile
        frontmatter.atomic_write(path, frontmatter.render(meta, body))
    remote = Remote(client, entity, eid, base_url)

    urls: dict[Path, str] = {}
    for file_path in files:
        upload = reused[file_path]
        if upload is None:
            upload = _complete_upload(remote, remote.upload(file_path))
            uploads.append(upload)
        urls[file_path] = download_url(remote.base_url, upload)

    sent = replace_spans(body, refs, urls)
    payload = {"body": sent, "content_type": 2}
    if meta.get("title"):
        payload["title"] = meta["title"]
    if category is not None:
        payload["category"] = category

    if saved and not force:
        latest = remote.get()
        if latest.get("body", "") != saved.get("remote_base", ""):
            _raise_conflict(
                path,
                latest.get("body", ""),
                saved,
                remote.uploads(),
                remote,
                "remote changed after uploads; body was not updated and uploads remain",
            )

    remote.patch(payload)
    stored = remote.get()
    if stored.get("content_type") != 2:
        raise RuntimeError("remote entity is not in markdown mode")

    state.save(
        base_url,
        entity,
        str(eid),
        {
            "remote_base": stored.get("body", ""),
            "local_base": body,
            "team": identity.get("team"),
        },
    )

    existing_tags = _remote_tags(remote_doc)
    for tag in meta.get("tags", []) or []:
        if tag not in existing_tags:
            client.add_tag(entity, eid, tag)
            existing_tags.add(tag)

    title = meta.get("title") or path.stem
    body_changed = saved is None or body != saved.get("local_base", "")
    reused_count = len(files) - len(new_uploads)
    new_count = len(new_uploads)
    print(f"pushed {entity}/{eid}: {title}")
    print(f"  body {'updated' if body_changed else 'unchanged'} (markdown)")
    print(f"  uploads: {reused_count} reused, {new_count} new")
    url = stored.get("sharelink")
    if url:
        print(f"  → {url}")


def _sidecar_path(path: Path) -> Path:
    return path.with_name(path.stem + ".remote.md")


def _base_sidecar_path(path: Path) -> Path:
    return path.with_name(path.stem + ".base.md")


def _reject_sidecar_attachment_collisions(
    uploads: list[dict], sidecars: tuple[Path, ...]
) -> None:
    sidecar_names = {sidecar.name.casefold() for sidecar in sidecars}
    for upload in uploads:
        name = _upload_name(upload)
        if name.casefold() in sidecar_names:
            raise RuntimeError(f"sidecar conflicts with attachment: {name}")


def _reject_attachment_conflict_name_collisions(
    planned: list[dict], suffixed: list[tuple[dict, str]]
) -> None:
    planned_names = {_upload_name(upload).casefold() for upload in planned}
    for upload, suffix in suffixed:
        conflict_name = _upload_name(upload) + suffix
        if conflict_name.casefold() in planned_names:
            raise RuntimeError(f"attachment {conflict_name} conflict-name collision")


def _raise_conflict(
    path: Path,
    remote_body: str,
    saved: dict,
    uploads: list[dict],
    remote: Remote,
    message: str,
) -> None:
    """Write git merge-file inputs (remote + base sidecars, attachments) and raise."""
    uploads = _reversible_uploads(uploads)
    remote_source, remote_used = reverse(remote_body, uploads, remote.base_url)
    base_source, base_used = reverse(
        saved.get("remote_base", ""), uploads, remote.base_url
    )
    used = []
    for upload in remote_used + base_used:
        if upload not in used:
            used.append(upload)
    _reject_sidecar_attachment_collisions(
        used, (_sidecar_path(path), _base_sidecar_path(path))
    )
    base_only = [upload for upload in base_used if upload not in remote_used]
    _reject_attachment_conflict_name_collisions(
        used,
        [(upload, ".remote") for upload in remote_used]
        + [(upload, ".base") for upload in base_only],
    )
    _place_attachments(path, remote, remote_used)
    _place_attachments(path, remote, base_only, conflict_suffix=".base")
    meta = frontmatter.parse(path.read_text(encoding="utf-8"))[0]
    frontmatter.atomic_write(
        _sidecar_path(path), frontmatter.render(meta, remote_source)
    )
    frontmatter.atomic_write(
        _base_sidecar_path(path), frontmatter.render(meta, base_source)
    )
    print(
        "git merge-file "
        f"{shlex.quote(str(path))} "
        f"{shlex.quote(str(_base_sidecar_path(path)))} "
        f"{shlex.quote(str(_sidecar_path(path)))}",
        file=sys.stderr,
    )
    raise RuntimeError(message)


def _validate_attachment_target(target: Path, doc_dir: Path) -> None:
    root = doc_dir.resolve()
    if target.is_symlink():
        raise RuntimeError(f"unsafe attachment destination: {target.name}")
    try:
        target.resolve().relative_to(root)
    except ValueError:
        raise RuntimeError(f"unsafe attachment destination: {target.name}") from None


def _write_attachment(target: Path, data: bytes, doc_dir: Path) -> None:
    _validate_attachment_target(target, doc_dir)
    nofollow = getattr(os, "O_NOFOLLOW", None)
    if nofollow is None or os.open not in os.supports_dir_fd:
        try:
            with target.open("xb") as stream:
                stream.write(data)
        except FileExistsError:
            if target.is_symlink():
                raise RuntimeError(
                    f"unsafe attachment destination: {target.name}"
                ) from None
            raise
        return
    directory_fd = os.open(
        doc_dir.resolve(), os.O_RDONLY | getattr(os, "O_DIRECTORY", 0)
    )
    try:
        flags = os.O_WRONLY | os.O_CREAT | os.O_EXCL | nofollow
        try:
            fd = os.open(target.name, flags, 0o666, dir_fd=directory_fd)
        except OSError as exc:
            if exc.errno == errno.ELOOP or target.is_symlink():
                raise RuntimeError(
                    f"unsafe attachment destination: {target.name}"
                ) from None
            raise
        with os.fdopen(fd, "wb") as stream:
            stream.write(data)
    finally:
        os.close(directory_fd)


def _replace_attachment(target: Path, data: bytes, doc_dir: Path) -> None:
    _validate_attachment_target(target, doc_dir)
    fd, temporary = tempfile.mkstemp(dir=doc_dir, prefix=f".{target.name}.")
    try:
        with os.fdopen(fd, "wb") as stream:
            stream.write(data)
        _validate_attachment_target(target, doc_dir)
        os.replace(temporary, target)
    finally:
        Path(temporary).unlink(missing_ok=True)


def _coalesce_attachments(remote: Remote, uploads: list[dict]):
    groups: dict[str, list[dict]] = {}
    for upload in uploads:
        name = _upload_name(upload)
        groups.setdefault(name.casefold(), []).append(upload)

    result = []
    for group in groups.values():
        if len(group) == 1:
            result.append((group[0], None))
            continue
        candidates = []
        for upload in group:
            digest = _remote_sha256(upload)
            data = None
            if digest is None:
                data = remote.download(upload["id"])
                digest = hashlib.sha256(data).hexdigest()
            candidates.append((upload, data, digest))
        if any(item[2] != candidates[0][2] for item in candidates[1:]):
            name = _upload_name(group[0])
            raise RuntimeError(
                f"attachment basename collision with different content: {name}"
            )
        upload, data, _ = next(
            (item for item in candidates if item[1] is not None), candidates[0]
        )
        result.append((upload, data))
    return result


def _place_attachments(
    path: Path,
    remote: Remote,
    uploads: list[dict],
    conflict_suffix: str = ".remote",
) -> list[str]:
    conflicts = []
    for upload, prefetched in _coalesce_attachments(remote, uploads):
        name = _upload_name(upload)
        if _is_control_upload(upload):
            print(
                f"warning: refusing to place control file attachment: {name}",
                file=sys.stderr,
            )
            continue
        target = path.parent / name
        _validate_attachment_target(target, path.parent)
        data = prefetched if prefetched is not None else remote.download(upload["id"])
        _validate_attachment_target(target, path.parent)
        try:
            _write_attachment(target, data, path.parent)
            continue
        except FileExistsError:
            _validate_attachment_target(target, path.parent)
        if target.read_bytes() != data:
            remote_target = target.with_name(target.name + conflict_suffix)
            _replace_attachment(remote_target, data, path.parent)
            conflicts.append(remote_target.name)
    return conflicts


def pull(path: Path, client, config: dict, profile=None) -> None:
    meta, body, entity, eid = _document(path, config)
    if not eid:
        raise RuntimeError("elab_id is required")
    _, base_url, _, _ = config_module.resolve(config, profile, meta)
    remote = Remote(client, entity, eid, base_url)
    saved = state.load(base_url, entity, str(eid))
    identity = client.me()
    _check_team_match(saved, identity)
    remote_doc = remote.get()
    local_dirty = saved is not None and body != saved.get("local_base", "")
    if local_dirty and remote_doc.get("body", "") == saved.get("remote_base", ""):
        print("remote: unchanged")
        return

    uploads = remote.uploads()
    for upload in uploads:
        if _is_control_upload(upload):
            print(
                "warning: refusing to place control file attachment: "
                f"{_upload_name(upload)}",
                file=sys.stderr,
            )
    source, used = reverse(
        remote_doc.get("body", ""),
        _reversible_uploads(uploads),
        remote.base_url,
    )

    no_base_conflict = saved is None and bool(body.strip()) and body != source
    if local_dirty:
        _raise_conflict(
            path,
            remote_doc.get("body", ""),
            saved,
            uploads,
            remote,
            f"local changes conflict; merge {_sidecar_path(path).name}",
        )
    if no_base_conflict:
        sidecar = _sidecar_path(path)
        _reject_sidecar_attachment_collisions(used, (sidecar,))
        _reject_attachment_conflict_name_collisions(
            used, [(upload, ".remote") for upload in used]
        )
        attachment_conflicts = _place_attachments(path, remote, used)
        frontmatter.atomic_write(sidecar, source)
        message = f"base unavailable; remote written to {sidecar.name}"
        if attachment_conflicts:
            message += "; attachment conflicts written to: " + ", ".join(
                attachment_conflicts
            )
        raise RuntimeError(message)

    _reject_attachment_conflict_name_collisions(
        used, [(upload, ".remote") for upload in used]
    )
    attachment_conflicts = _place_attachments(path, remote, used)
    if attachment_conflicts:
        raise RuntimeError(
            "attachment conflicts written to: " + ", ".join(attachment_conflicts)
        )

    frontmatter.atomic_write(path, frontmatter.render(meta, source))
    state.save(
        base_url,
        entity,
        str(eid),
        {
            "remote_base": remote_doc.get("body", ""),
            "local_base": source,
            "team": identity.get("team"),
        },
    )
    title = meta.get("title") or remote_doc.get("title") or path.stem
    print(f"pulled {entity}/{eid}: {title}")
    print(f"  wrote {path.name}")
    url = remote_doc.get("sharelink")
    if url:
        print(f"  → {url}")


def status(path: Path, client, config: dict, profile=None) -> None:
    meta, body, entity, eid = _document(path, config)
    saved = None
    remote = None
    if eid:
        _, base_url, _ = config_module.base_target(config, profile, meta)
        remote = Remote(client, entity, eid, base_url)
        saved = state.load(base_url, entity, str(eid))
    refs = plan(body, path.parent, ignore_patterns(path.parent, config))
    files = sorted({ref.file for ref in refs if ref.file})
    if saved is None:
        print("local: base unavailable (comparison unavailable)")
    elif body == saved.get("local_base", ""):
        print("local: clean")
    else:
        print('local: dirty (use "elab push")')
    print("uploads local:", ", ".join(path.name for path in files) or "none")

    if remote is None:
        print("uploads new:", ", ".join(path.name for path in files) or "none")
        print("uploads reuse: none")
        print("remote: no elab_id (comparison unavailable)")
        return
    try:
        uploads = remote.uploads()
        remote_doc = remote.get()
    except (OSError, ValueError):
        print("uploads reuse: unavailable (offline?)")
        print("remote: unavailable (offline?)")
        print("mode: unavailable (offline?)")
        return

    reused = [path.name for path in files if _matching_upload(path, uploads)]
    new = [path.name for path in files if path.name not in reused]
    print("uploads new:", ", ".join(new) or "none")
    print("uploads reuse:", ", ".join(reused) or "none")
    print(
        "mode:",
        "markdown"
        if remote_doc.get("content_type") == 2
        else remote_doc.get("content_type", "unknown"),
    )
    if saved is None:
        print("remote: base unavailable (comparison unavailable)")
    elif remote_doc.get("body", "") == saved.get("remote_base", ""):
        print("remote: unchanged")
    else:
        print('remote: changed (use "elab pull")')


def _normalize_remote_diff(text: str) -> str:
    return text.replace("\r\n", "\n").replace("\r", "\n").rstrip("\n") + "\n"


def diff(path: Path, client, config: dict, profile=None, base_only=False) -> None:
    meta, body, entity, eid = _document(path, config)
    saved = None
    remote = None
    if eid:
        _, base_url, _ = config_module.base_target(config, profile, meta)
        remote = Remote(client, entity, eid, base_url)
        saved = state.load(base_url, entity, str(eid))

    if base_only:
        if saved is None:
            raise RuntimeError("base unavailable; cannot show --base diff")
        other = saved.get("local_base", "")
        local = body
        fromfile = "base"
    else:
        if remote is None:
            raise RuntimeError("elab_id is required")
        remote_doc = remote.get()
        other = reverse(remote_doc.get("body", ""), remote.uploads(), remote.base_url)[
            0
        ]
        other = _normalize_remote_diff(other)
        local = _normalize_remote_diff(body)
        fromfile = "remote"
        print(
            "note: server normalization noise may remain in this diff",
            file=sys.stderr,
        )

    print(
        "".join(
            difflib.unified_diff(
                other.splitlines(True),
                local.splitlines(True),
                fromfile=fromfile,
                tofile="local",
            )
        ),
        end="",
    )
