from __future__ import annotations

import hashlib
import json
import os
import tempfile
from pathlib import Path


def _dir(create: bool = False) -> Path:
    p = Path.home() / ".config/elport/state"
    if create:
        p.mkdir(parents=True, exist_ok=True)
        p.chmod(0o700)
    return p


def key(base: str, entity: str, eid: str) -> str:
    return hashlib.sha256(f"{base.rstrip('/')}|{entity}|{eid}".encode()).hexdigest()


def load(base: str, entity: str, eid: str) -> dict | None:
    try:
        return json.loads((_dir() / key(base, entity, eid)).read_text(encoding="utf-8"))
    except (FileNotFoundError, json.JSONDecodeError):
        return None


def save(base: str, entity: str, eid: str, data: dict) -> None:
    p = _dir(create=True) / key(base, entity, eid)
    fd, temporary = tempfile.mkstemp(prefix=f".{p.name}.", dir=p.parent)
    try:
        with os.fdopen(fd, "w", encoding="utf-8") as stream:
            stream.write(json.dumps(data, ensure_ascii=False))
        os.chmod(temporary, 0o600)
        os.replace(temporary, p)
    finally:
        if os.path.exists(temporary):
            os.unlink(temporary)
