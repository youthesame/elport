import pytest

from elab.client import REQUEST_TIMEOUT, Client


class _Response:
    ok = True

    def __init__(self, data):
        self._data = data

    def json(self):
        return self._data


def test_get_normalizes_null_body(monkeypatch):
    client = Client("https://example.org", "key")
    monkeypatch.setattr(
        client, "request", lambda *args, **kwargs: _Response({"body": None})
    )
    assert client.get("experiments", 1)["body"] == ""


def test_request_uses_bounded_timeout(monkeypatch):
    captured = {}

    def request(*args, **kwargs):
        captured.update(kwargs)
        return _Response({})

    monkeypatch.setattr("elab.client.requests.request", request)

    Client("https://example.org", "key").request("GET", "/users/me")

    assert captured["timeout"] == REQUEST_TIMEOUT == (10, 120)


@pytest.mark.parametrize(
    "call",
    [
        lambda client, path: client.get("experiments", "../items/42"),
        lambda client, path: client.patch("experiments", "../items/42", {}),
        lambda client, path: client.uploads("experiments", "../items/42"),
        lambda client, path: client.upload("experiments", "../items/42", path),
        lambda client, path: client.download("experiments", "../items/42", 3),
        lambda client, path: client.add_tag("experiments", "../items/42", "tag"),
    ],
)
def test_entity_api_paths_reject_traversal_id_before_request(
    tmp_path, monkeypatch, call
):
    client = Client("https://example.org", "key")
    attachment = tmp_path / "data.txt"
    attachment.write_text("data", encoding="utf-8")
    monkeypatch.setattr(
        client,
        "request",
        lambda *args, **kwargs: pytest.fail("request should not be sent"),
    )

    with pytest.raises(ValueError, match="elab_id must be a positive integer"):
        call(client, attachment)


@pytest.mark.parametrize("eid", ["42", 0, -1, True, 5.0])
def test_entity_api_paths_reject_other_non_positive_integer_ids(monkeypatch, eid):
    client = Client("https://example.org", "key")
    monkeypatch.setattr(
        client,
        "request",
        lambda *args, **kwargs: pytest.fail("request should not be sent"),
    )

    with pytest.raises(ValueError, match="elab_id must be a positive integer"):
        client.get("experiments", eid)
