import os

import keyring.errors
import pytest

from elab import config


def test_load_merges_ignore_across_all_layers(tmp_path, monkeypatch):
    user = tmp_path / "user.toml"
    project = tmp_path / "project"
    document = project / "notes"
    project.mkdir()
    document.mkdir()
    user.write_text('entity = "experiments"\nignore = ["*.key"]\n', encoding="utf-8")
    (project / ".elab.toml").write_text(
        'entity = "items"\nignore = ["*.zip"]\n', encoding="utf-8"
    )
    (document / ".elab.toml").write_text(
        'entity = "experiments"\nignore = ["scratch/**"]\n', encoding="utf-8"
    )
    monkeypatch.setattr(config, "config_path", lambda: user)

    loaded = config.load(project, document)

    assert loaded["ignore"] == ["*.key", "*.zip", "scratch/**"]
    assert loaded["entity"] == "experiments"


def test_load_does_not_apply_same_project_and_document_layer_twice(
    tmp_path, monkeypatch
):
    user = tmp_path / "user.toml"
    user.write_text('ignore = ["user"]\n', encoding="utf-8")
    (tmp_path / ".elab.toml").write_text('ignore = ["local"]\n', encoding="utf-8")
    monkeypatch.setattr(config, "config_path", lambda: user)

    assert config.load(tmp_path, tmp_path)["ignore"] == ["user", "local"]


def test_load_ignores_protected_user_profile_overrides(tmp_path, monkeypatch, capsys):
    user = tmp_path / "user.toml"
    project = tmp_path / "project"
    document = project / "notes"
    document.mkdir(parents=True)
    user.write_text(
        '[profiles.lab]\nbase_url = "https://e.example"\nverify_ssl = true\n',
        encoding="utf-8",
    )
    (project / ".elab.toml").write_text(
        '[profiles.lab]\nbase_url = "https://attacker.example"\nverify_ssl = false\n',
        encoding="utf-8",
    )
    (document / ".elab.toml").write_text(
        '[profiles.lab]\nbase_url = "https://other-attacker.example"\n'
        "verify_ssl = false\n",
        encoding="utf-8",
    )
    monkeypatch.setattr(config, "config_path", lambda: user)
    monkeypatch.setattr(config.keyring, "get_password", lambda *args: "secret")
    monkeypatch.delenv("ELABFTW_BASE_URL", raising=False)
    monkeypatch.delenv("ELABFTW_API_KEY", raising=False)

    loaded = config.load(project, document)

    assert loaded["profiles"]["lab"] == {
        "base_url": "https://e.example",
        "verify_ssl": True,
    }
    assert config.resolve(loaded, "lab", {}) == (
        "lab",
        "https://e.example",
        "secret",
        True,
    )
    warning = capsys.readouterr().err
    assert warning.count("profiles.lab.base_url") == 2
    assert warning.count("profiles.lab.verify_ssl") == 2


def test_load_allows_new_profile_in_project_and_document_layers(
    tmp_path, monkeypatch, capsys
):
    user = tmp_path / "user.toml"
    project = tmp_path / "project"
    document = project / "notes"
    document.mkdir(parents=True)
    user.write_text('[profiles.lab]\nbase_url = "https://e.example"\n')
    (project / ".elab.toml").write_text(
        '[profiles.new]\nbase_url = "https://new.example"\nverify_ssl = true\n'
    )
    (document / ".elab.toml").write_text("[profiles.new]\nverify_ssl = false\n")
    monkeypatch.setattr(config, "config_path", lambda: user)

    loaded = config.load(project, document)

    assert loaded["profiles"]["new"] == {
        "base_url": "https://new.example",
        "verify_ssl": False,
    }
    assert capsys.readouterr().err == ""


def test_resolve_uses_home_plaintext_key_when_keyring_is_unavailable(
    tmp_path, monkeypatch, capsys
):
    user = tmp_path / "config.toml"
    user.write_text(
        '[profiles.lab]\nbase_url = "https://e.example"\napi_key = "secret"\n',
        encoding="utf-8",
    )
    monkeypatch.setattr(config, "config_path", lambda: user)
    monkeypatch.setattr(
        config.keyring,
        "get_password",
        lambda *args: (_ for _ in ()).throw(keyring.errors.NoKeyringError()),
    )
    monkeypatch.delenv("ELABFTW_BASE_URL", raising=False)
    monkeypatch.delenv("ELABFTW_API_KEY", raising=False)

    resolved = config.resolve(config._read(user), "lab", {})

    assert resolved == ("lab", "https://e.example", "secret", True)
    assert "平文 api_key を使用中（600 推奨）" in capsys.readouterr().err


def test_resolve_does_not_use_project_plaintext_key(tmp_path, monkeypatch):
    user = tmp_path / "config.toml"
    user.write_text(
        '[profiles.lab]\nbase_url = "https://e.example"\n', encoding="utf-8"
    )
    monkeypatch.setattr(config, "config_path", lambda: user)
    monkeypatch.setattr(
        config.keyring,
        "get_password",
        lambda *args: (_ for _ in ()).throw(keyring.errors.NoKeyringError()),
    )
    monkeypatch.delenv("ELABFTW_BASE_URL", raising=False)
    monkeypatch.delenv("ELABFTW_API_KEY", raising=False)
    merged = {
        "profiles": {
            "lab": {"base_url": "https://e.example", "api_key": "project-secret"}
        }
    }

    with pytest.raises(ValueError, match="ELABFTW_BASE_URL.*ELABFTW_API_KEY"):
        config.resolve(merged, "lab", {})


def test_login_saves_plaintext_key_when_keyring_is_unavailable(
    tmp_path, monkeypatch, capsys
):
    user = tmp_path / "config.toml"
    user.write_text(
        'default_profile = "lab"\n\n[profiles.lab]\nverify_ssl = false\n'
        '\n[profiles.other]\nbase_url = "https://other.example"\n',
        encoding="utf-8",
    )
    monkeypatch.setattr(config, "config_path", lambda: user)
    monkeypatch.setattr(
        config.keyring,
        "set_password",
        lambda *args: (_ for _ in ()).throw(keyring.errors.NoKeyringError()),
    )

    config.login("lab", "https://e.example/", "secret")

    assert config._read(user)["profiles"]["lab"] == {
        "base_url": "https://e.example",
        "verify_ssl": False,
        "api_key": "secret",
    }
    assert config._read(user)["profiles"]["other"] == {
        "base_url": "https://other.example"
    }
    assert user.stat().st_mode & 0o777 == 0o600
    assert "平文 api_key を使用中（600 推奨）" in capsys.readouterr().err


def test_login_replaces_config_from_mode_0600_temporary_file(tmp_path, monkeypatch):
    user = tmp_path / "config.toml"
    monkeypatch.setattr(config, "config_path", lambda: user)
    monkeypatch.setattr(
        config.keyring,
        "set_password",
        lambda *args: (_ for _ in ()).throw(keyring.errors.NoKeyringError()),
    )
    real_replace = os.replace
    observed = {}

    def inspect_replace(source, destination):
        source = config.Path(source)
        observed["mode"] = source.stat().st_mode & 0o777
        observed["text"] = source.read_text(encoding="utf-8")
        real_replace(source, destination)

    monkeypatch.setattr(config.os, "replace", inspect_replace)

    config.login("lab", "https://e.example", "secret")

    assert observed["mode"] == 0o600
    assert 'api_key = "secret"' in observed["text"]
    assert user.stat().st_mode & 0o777 == 0o600


def test_login_replace_failure_preserves_existing_config(tmp_path, monkeypatch):
    user = tmp_path / "config.toml"
    original = b'[profiles.lab]\nbase_url = "https://old.example"\n'
    user.write_bytes(original)
    monkeypatch.setattr(config, "config_path", lambda: user)
    keyring_calls = []
    monkeypatch.setattr(
        config.keyring,
        "set_password",
        lambda *args: keyring_calls.append(args),
    )
    monkeypatch.setattr(
        config.os,
        "replace",
        lambda *args: (_ for _ in ()).throw(OSError("replace failed")),
    )

    with pytest.raises(OSError, match="replace failed"):
        config.login("lab", "https://new.example", "secret")

    assert user.read_bytes() == original
    assert list(tmp_path.iterdir()) == [user]
    assert keyring_calls == []


def test_login_writes_config_before_setting_keyring(tmp_path, monkeypatch):
    user = tmp_path / "config.toml"
    monkeypatch.setattr(config, "config_path", lambda: user)
    observed = {}

    def inspect_config(*args):
        observed.update(config._read(user)["profiles"]["lab"])

    monkeypatch.setattr(
        config.keyring,
        "set_password",
        inspect_config,
    )

    config.login("lab", "https://e.example", "secret")

    assert observed == {"base_url": "https://e.example"}
