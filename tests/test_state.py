from elab import state


def test_load_treats_missing_and_corrupt_state_as_unavailable(tmp_path, monkeypatch):
    monkeypatch.setattr(state, "_dir", lambda create=False: tmp_path)
    path = tmp_path / state.key("https://example.org", "experiments", "42")

    assert state.load("https://example.org", "experiments", "42") is None

    path.write_text("", encoding="utf-8")
    assert state.load("https://example.org", "experiments", "42") is None

    path.write_text("not json", encoding="utf-8")
    assert state.load("https://example.org", "experiments", "42") is None

    expected = {"local_base": "body", "team": 7}
    state.save("https://example.org", "experiments", "42", expected)
    assert state.load("https://example.org", "experiments", "42") == expected
