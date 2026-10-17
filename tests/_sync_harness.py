from __future__ import annotations

from pathlib import Path

from elport import frontmatter, sync


class FakeClient:
    def __init__(
        self, gets=None, uploads=None, comments=None, remote_doc=None, identity=None
    ):
        self.gets = list(gets or [])
        self.upload_list = list(uploads or [])
        self.comment_list = list(comments or [])
        self.remote_doc = dict(remote_doc or {})
        self.identity = {
            "team": 7,
            "default_read_base": 30,
            "default_write_base": 20,
            **(identity or {}),
        }
        self.calls = []
        self.saved_payload = None
        self.comment_posts = []

    def me(self):
        self.calls.append("me")
        return self.identity

    def get(self, entity, eid):
        self.calls.append("get")
        return self.gets.pop(0) if self.gets else dict(self.remote_doc)

    def uploads(self, entity, eid):
        self.calls.append("uploads")
        return self.upload_list

    def upload(self, entity, eid, path):
        self.calls.append("upload")
        uploaded = {
            "id": 8,
            "long_name": "aa/uploaded",
            "real_name": path.name,
            "storage": 1,
        }
        self.upload_list.append(uploaded)
        return uploaded

    def patch(self, entity, eid, payload):
        self.calls.append("patch")
        self.saved_payload = payload
        self.remote_doc.update(payload)
        # The body PATCH answers with the stored entity (it replaced the re-GET),
        # so it takes the next queued remote form.
        if "body" in payload and self.gets:
            return self.gets.pop(0)
        return dict(self.remote_doc)

    def add_tag(self, entity, eid, tag):
        self.calls.append(f"tag:{tag}")

    def comments(self, entity, eid):
        self.calls.append("comments")
        return self.comment_list

    def add_comment(self, entity, eid, text):
        self.calls.append("add_comment")
        self.comment_posts.append((entity, eid, {"comment": text}))

    def categories(self, team_id, entity):
        self.calls.append("categories")
        return [{"id": 9, "title": "Synthesis"}]

    def statuses(self, team_id, entity):
        self.calls.append("statuses")
        return [{"id": 5, "title": "Running"}]

    def create(self, entity, title):
        self.calls.append("create")
        return {"id": 42}

    def download(self, entity, eid, upload_id):
        self.calls.append("download")
        return b"data"


def write_doc(path: Path, body: str, **extra):
    meta = {"id": 1, "entity": "experiments", "title": "Test", **extra}
    path.write_text(frontmatter.render(meta, body))


def saved_state(local="local", remote="remote"):
    return {"local_base": local, "remote_base": remote, "team": 7}


def elport_doc(path, body="body", **meta):
    path.parent.mkdir(parents=True, exist_ok=True)
    values = {"id": 1, "entity": "experiments", "profile": "test", **meta}
    path.write_text(frontmatter.render(values, body), encoding="utf-8")
    return path


class Store:
    """In-memory state keyed like the real one."""

    def __init__(self, monkeypatch, initial=None):
        self.data = dict(initial or {})
        monkeypatch.setattr(
            sync.state, "load", lambda base, entity, eid: self.data.get(eid)
        )
        monkeypatch.setattr(
            sync.state,
            "save",
            lambda base, entity, eid, value: self.data.__setitem__(eid, value),
        )


class LinkClient(FakeClient):
    def __init__(self, **kwargs):
        super().__init__(**kwargs)
        self.links = []

    def add_link(self, entity, eid, target_entity, target_id):
        self.calls.append("link")
        self.links.append((entity, eid, target_entity, target_id))


EXP12 = "https://e.example/experiments.php?mode=view&id=12"


def patch_returning(client, body):
    def patch(entity, eid, payload):
        client.calls.append("patch")
        client.saved_payload = payload
        return {"body": body, "content_type": 2}

    return patch
