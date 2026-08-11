from __future__ import annotations

import pytest

from elab import sync


@pytest.fixture
def configured(monkeypatch):
    monkeypatch.setattr(
        sync.config_module,
        "resolve",
        lambda config, profile, meta: ("test", "https://e.example", "secret", True),
    )
    monkeypatch.setattr(
        sync.config_module,
        "base_target",
        lambda config, profile, meta: ("test", "https://e.example", True),
    )
    monkeypatch.setattr(sync.state, "save", lambda *args: None)
