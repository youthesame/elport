import pytest
import requests

from elport.client import REQUEST_TIMEOUT, Client


class _Response:
    ok = True

    def __init__(self, data):
        self._data = data

    def json(self):
        return self._data


@pytest.mark.parametrize(
    ("method", "path", "data"),
    [
        ("info", "/info", {"elabftw_version": "5.6.12"}),
        ("apikeys", "/apikeys", [{"id": 12, "can_write": 1}]),
    ],
)
def test_account_metadata_methods_use_expected_get_endpoint(
    monkeypatch, method, path, data
):
    client = Client("https://example.org", "key")
    calls = []
    monkeypatch.setattr(
        client,
        "request",
        lambda *args, **kwargs: calls.append((args, kwargs)) or _Response(data),
    )

    assert getattr(client, method)() == data
    assert calls == [(("GET", path), {})]


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

    monkeypatch.setattr("elport.client.requests.request", request)

    Client("https://example.org", "key").request("GET", "/users/me")

    assert captured["timeout"] == REQUEST_TIMEOUT == (10, 120)


def test_request_reports_connection_failure_without_internal_details(monkeypatch):
    def request(*args, **kwargs):
        raise requests.exceptions.ConnectionError("boom")

    monkeypatch.setattr("elport.client.requests.request", request)

    with pytest.raises(OSError) as error:
        Client("https://example.org", "key").request("GET", "/users/me")

    message = str(error.value)
    assert "could not reach" in message
    assert "ConnectionError" not in message
    assert "boom" not in message


@pytest.mark.parametrize(
    "call",
    [
        lambda client, path: client.get("experiments", "../items/42"),
        lambda client, path: client.patch("experiments", "../items/42", {}),
        lambda client, path: client.uploads("experiments", "../items/42"),
        lambda client, path: client.upload("experiments", "../items/42", path),
        lambda client, path: client.download("experiments", "../items/42", 3),
        lambda client, path: client.add_tag("experiments", "../items/42", "tag"),
        lambda client, path: client.comments("experiments", "../items/42"),
        lambda client, path: client.add_comment(
            "experiments", "../items/42", "comment"
        ),
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

    with pytest.raises(ValueError, match="id must be a positive integer"):
        call(client, attachment)


@pytest.mark.parametrize("eid", ["42", 0, -1, True, 5.0])
def test_entity_api_paths_reject_other_non_positive_integer_ids(monkeypatch, eid):
    client = Client("https://example.org", "key")
    monkeypatch.setattr(
        client,
        "request",
        lambda *args, **kwargs: pytest.fail("request should not be sent"),
    )

    with pytest.raises(ValueError, match="id must be a positive integer"):
        client.get("experiments", eid)


def test_comments_lists_entity_comments(monkeypatch):
    client = Client("https://example.org", "key")
    calls = []
    monkeypatch.setattr(
        client,
        "request",
        lambda *args, **kwargs: calls.append((args, kwargs)) or _Response([{"id": 5}]),
    )

    assert client.comments("experiments", 42) == [{"id": 5}]
    assert calls == [(("GET", "/experiments/42/comments"), {})]


def test_add_comment_posts_comment_text(monkeypatch):
    client = Client("https://example.org", "key")
    calls = []
    monkeypatch.setattr(
        client,
        "request",
        lambda *args, **kwargs: calls.append((args, kwargs)) or _Response({}),
    )

    client.add_comment("items", 9, "hello")

    assert calls == [(("POST", "/items/9/comments"), {"json": {"comment": "hello"}})]


@pytest.mark.parametrize(
    ("entity", "resource"),
    [
        ("items", "resources_categories"),
        ("experiments", "experiments_categories"),
    ],
)
def test_categories_uses_entity_category_endpoint(monkeypatch, entity, resource):
    client = Client("https://example.org", "key")
    calls = []
    monkeypatch.setattr(
        client,
        "request",
        lambda *args, **kwargs: calls.append((args, kwargs)) or _Response([]),
    )

    assert client.categories(7, entity) == []
    assert calls == [(("GET", f"/teams/7/{resource}"), {})]


@pytest.mark.parametrize(
    ("entity", "resource"),
    [
        ("items", "items_status"),
        ("experiments", "experiments_status"),
    ],
)
def test_statuses_uses_entity_status_endpoint(monkeypatch, entity, resource):
    client = Client("https://example.org", "key")
    calls = []
    monkeypatch.setattr(
        client,
        "request",
        lambda *args, **kwargs: calls.append((args, kwargs)) or _Response([]),
    )

    assert client.statuses(7, entity) == []
    assert calls == [(("GET", f"/teams/7/{resource}"), {})]
