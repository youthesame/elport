from __future__ import annotations

import os
import sys
import tempfile
from pathlib import Path

if sys.version_info >= (3, 11):
    import tomllib
else:
    import tomli as tomllib

import keyring
import tomli_w
from keyring.errors import KeyringError

PLAINTEXT_WARNING = "warning: using plaintext api_key (chmod 600 recommended)"


def config_path() -> Path:
    return Path.home() / ".config/elport/config.toml"


def _read(path: Path) -> dict:
    try:
        return tomllib.loads(path.read_text(encoding="utf-8"))
    except FileNotFoundError:
        return {}


def _atomic_write(path: Path, text: str) -> None:
    path.parent.mkdir(parents=True, exist_ok=True)
    fd, temporary = tempfile.mkstemp(dir=path.parent, prefix=f".{path.name}.")
    try:
        with os.fdopen(fd, "w", encoding="utf-8") as stream:
            stream.write(text)
        os.replace(temporary, path)
    finally:
        Path(temporary).unlink(missing_ok=True)


def load(project: Path, doc_dir: Path) -> dict:
    """Load layered config while protecting credential-bound profile fields.
    Project/document layers cannot override user profile base_url or verify_ssl.
    """
    data = _read(config_path())
    user_profile_names = set(data.get("profiles", {}))
    seen: set[Path] = set()
    for p in (project / ".elport.toml", doc_dir / ".elport.toml"):
        resolved = p.resolve()
        if resolved in seen:
            continue
        seen.add(resolved)
        layer = _read(p)
        profiles = data.setdefault("profiles", {})
        for name, values in layer.get("profiles", {}).items():
            profile = profiles.setdefault(name, {})
            for key, value in values.items():
                if name in user_profile_names and key in {"base_url", "verify_ssl"}:
                    print(
                        f"warning: ignoring {p} override of profiles.{name}.{key}",
                        file=sys.stderr,
                    )
                    continue
                profile[key] = value
        for k, v in layer.items():
            if k == "ignore":
                data.setdefault("ignore", []).extend(v)
            elif k != "profiles":
                data[k] = v
    return data


def _profile_name(data: dict, profile: str | None, meta: dict) -> str:
    mp, cp = meta.get("profile"), profile
    if mp and cp and mp != cp:
        raise ValueError("profile mismatch between frontmatter and CLI")
    selected = mp or cp or data.get("default_profile")
    if selected:
        return selected
    profiles = data.get("profiles", {})
    if not profiles or "default" in profiles:
        return "default"
    raise ValueError("no default profile set; run 'elport profile use <name>'")


def base_target(data: dict, profile: str | None, meta: dict) -> tuple[str, str, bool]:
    name = _profile_name(data, profile, meta)
    prof = data.get("profiles", {}).get(name, {})
    url = os.getenv("ELABFTW_BASE_URL") or prof.get("base_url")
    if not url:
        raise ValueError(f"base_url unavailable for profile {name}; run elport login")
    return name, url.rstrip("/"), prof.get("verify_ssl", True)


def resolve(data: dict, profile: str | None, meta: dict) -> tuple[str, str, str, bool]:
    name = _profile_name(data, profile, meta)
    env_url, env_key = os.getenv("ELABFTW_BASE_URL"), os.getenv("ELABFTW_API_KEY")
    if bool(env_url) != bool(env_key):
        raise ValueError("ELABFTW_BASE_URL and ELABFTW_API_KEY must be set together")
    if env_url and env_key:
        return name, env_url.rstrip("/"), env_key, True
    prof = data.get("profiles", {}).get(name, {})
    url = prof.get("base_url")
    try:
        key = keyring.get_password("elport", name)
    except KeyringError:
        fallback = _read(config_path()).get("profiles", {}).get(name, {})
        url = fallback.get("base_url")
        key = fallback.get("api_key")
        if not url or not key:
            raise ValueError(
                "keyring unavailable; set ELABFTW_BASE_URL and "
                "ELABFTW_API_KEY together, or run elport login"
            ) from None
        print(PLAINTEXT_WARNING, file=sys.stderr)
        return name, url.rstrip("/"), key, fallback.get("verify_ssl", True)
    if not url or not key:
        raise ValueError(
            f"credentials unavailable for profile {name}; run elport login"
        )
    return name, url.rstrip("/"), key, prof.get("verify_ssl", True)


def login(name: str, url: str, api_key: str) -> None:
    p = config_path()
    p.parent.mkdir(parents=True, exist_ok=True)
    data = _read(p)
    profile = data.setdefault("profiles", {}).setdefault(name, {})
    profile["base_url"] = url.rstrip("/")
    profile.pop("api_key", None)
    if not data.get("default_profile"):
        data["default_profile"] = name
    _atomic_write(p, tomli_w.dumps(data))
    try:
        keyring.set_password("elport", name, api_key)
    except KeyringError:
        profile["api_key"] = api_key
        _atomic_write(p, tomli_w.dumps(data))
        print(PLAINTEXT_WARNING, file=sys.stderr)


def logout(name: str) -> bool:
    """Remove the stored api_key for a profile from keyring and config.toml.

    Returns True if anything was removed. Leaves base_url intact.
    """
    removed = False
    try:
        keyring.delete_password("elport", name)
        removed = True
    except KeyringError:
        pass

    p = config_path()
    data = _read(p)
    profile = data.get("profiles", {}).get(name, {})
    config_changed = False
    if "api_key" in profile:
        del profile["api_key"]
        removed = True
        config_changed = True
    if data.get("default_profile") == name:
        del data["default_profile"]
        config_changed = True
    if config_changed:
        _atomic_write(p, tomli_w.dumps(data))
    return removed


def set_default(name: str) -> None:
    p = config_path()
    data = _read(p)
    data["default_profile"] = name
    _atomic_write(p, tomli_w.dumps(data))
