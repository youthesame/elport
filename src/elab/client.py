from __future__ import annotations

from pathlib import Path

import requests

from .frontmatter import validate_elab_id

REQUEST_TIMEOUT = (10, 120)


def _entity_path(entity: str, eid: object, suffix: str = "") -> str:
    return f"/{entity}/{validate_elab_id(eid)}{suffix}"


class Client:
    def __init__(self, base_url: str, api_key: str, verify_ssl: bool = True):
        self.base = base_url.rstrip("/") + "/api/v2"
        self.root = base_url.rstrip("/")
        self.key = api_key
        self.verify = verify_ssl

    def request(self, method, path, **kwargs):
        headers = kwargs.pop("headers", {})
        headers["Authorization"] = self.key
        try:
            r = requests.request(
                method,
                self.base + path,
                headers=headers,
                verify=self.verify,
                timeout=REQUEST_TIMEOUT,
                **kwargs,
            )
        except requests.exceptions.RequestException as exc:
            raise OSError(f"could not reach {self.root} (connection failed)") from exc
        if not r.ok:
            raise RuntimeError(f"API {r.status_code}: {r.text[:300]}")
        return r

    def me(self):
        return self.request("GET", "/users/me").json()

    def get(self, entity, eid):
        data = self.request("GET", _entity_path(entity, eid)).json()
        # eLabFTW returns null for an empty body; normalize at the API boundary.
        if data.get("body") is None:
            data["body"] = ""
        return data

    def create(self, entity, title):
        r = self.request(
            "POST", f"/{entity}", json={"title": title, "body": "", "content_type": 2}
        )
        try:
            result = r.json()
        except ValueError:
            result = {}
        if "id" not in result and r.headers.get("Location"):
            result["id"] = r.headers["Location"].rstrip("/").split("/")[-1]
        return result

    def patch(self, entity, eid, payload):
        response = self.request("PATCH", _entity_path(entity, eid), json=payload)
        try:
            return response.json()
        except ValueError:
            return {}

    def uploads(self, entity, eid):
        return self.request("GET", _entity_path(entity, eid, "/uploads")).json()

    def comments(self, entity, eid):
        return self.request("GET", _entity_path(entity, eid, "/comments")).json()

    def upload(self, entity, eid, path: Path):
        with path.open("rb") as stream:
            r = self.request(
                "POST",
                _entity_path(entity, eid, "/uploads"),
                files={"file": (path.name, stream)},
            )
        try:
            return r.json()
        except ValueError:
            return {"id": r.headers["Location"].rstrip("/").split("/")[-1]}

    def download(self, entity, eid, uid):
        return self.request(
            "GET", _entity_path(entity, eid, f"/uploads/{uid}?format=binary")
        ).content

    def add_tag(self, entity, eid, tag):
        self.request("POST", _entity_path(entity, eid, "/tags"), json={"tag": tag})

    def add_comment(self, entity, eid, text):
        self.request(
            "POST",
            _entity_path(entity, eid, "/comments"),
            json={"comment": text},
        )

    def categories(self, team_id, entity):
        resource = (
            "experiments_categories"
            if entity == "experiments"
            else "resources_categories"
        )
        return self.request("GET", f"/teams/{team_id}/{resource}").json()
