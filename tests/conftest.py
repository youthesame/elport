from __future__ import annotations

import os

import pytest
from hypothesis import settings

from elport import sync

settings.register_profile("crosshair", backend="crosshair", deadline=None)
if profile := os.getenv("HYPOTHESIS_PROFILE"):
    settings.load_profile(profile)


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
    monkeypatch.setattr(sync.config_module, "load", lambda project, doc_dir: {})
