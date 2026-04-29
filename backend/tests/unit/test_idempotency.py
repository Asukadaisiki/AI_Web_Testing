"""Unit tests for IdempotencyMiddleware."""

import time

from fastapi import FastAPI, Response
from fastapi.testclient import TestClient

from app.core.idempotency import IdempotencyMiddleware, IdempotencyStore


def _make_app(store: IdempotencyStore | None = None) -> tuple[FastAPI, TestClient]:
    app = FastAPI()
    call_count = {"value": 0}

    @app.post("/echo")
    def echo():
        call_count["value"] += 1
        return {"count": call_count["value"]}

    @app.post("/error-500")
    def error_500():
        call_count["value"] += 1
        return Response(content=b"internal error", status_code=500)

    @app.post("/sse")
    def sse():
        call_count["value"] += 1
        return Response(
            content=b"data: hello\n\n",
            status_code=200,
            media_type="text/event-stream",
        )

    @app.get("/get-echo")
    def get_echo():
        return {"ok": True}

    app.add_middleware(IdempotencyMiddleware, store=store)
    client = TestClient(app)
    return app, client


class TestIdempotencyMiddleware:
    def test_no_header_passes_through(self):
        _, client = _make_app()
        r1 = client.post("/echo")
        r2 = client.post("/echo")
        assert r1.json()["count"] == 1
        assert r2.json()["count"] == 2

    def test_first_request_executes_normally(self):
        _, client = _make_app()
        r = client.post("/echo", headers={"Idempotency-Key": "k1"})
        assert r.status_code == 200
        assert r.json()["count"] == 1

    def test_duplicate_key_returns_cached_response(self):
        _, client = _make_app()
        r1 = client.post("/echo", headers={"Idempotency-Key": "k2"})
        r2 = client.post("/echo", headers={"Idempotency-Key": "k2"})
        assert r1.json() == r2.json()
        assert r2.json()["count"] == 1

    def test_ttl_expiry_reexecutes(self):
        store = IdempotencyStore(ttl=0.01)
        _, client = _make_app(store)
        r1 = client.post("/echo", headers={"Idempotency-Key": "k3"})
        time.sleep(0.02)
        r2 = client.post("/echo", headers={"Idempotency-Key": "k3"})
        assert r1.json()["count"] == 1
        assert r2.json()["count"] == 2

    def test_non_post_methods_pass_through(self):
        _, client = _make_app()
        r = client.get("/get-echo", headers={"Idempotency-Key": "k4"})
        assert r.status_code == 200

    def test_sse_response_not_cached(self):
        _, client = _make_app()
        r1 = client.post("/sse", headers={"Idempotency-Key": "k5"})
        r2 = client.post("/sse", headers={"Idempotency-Key": "k5"})
        assert r1.text == r2.text
        # SSE is not cached, so the handler runs twice
        assert r2.status_code == 200

    def test_5xx_not_cached(self):
        _, client = _make_app()
        r1 = client.post("/error-500", headers={"Idempotency-Key": "k6"})
        r2 = client.post("/error-500", headers={"Idempotency-Key": "k6"})
        assert r1.status_code == 500
        assert r2.status_code == 500
        # 5xx is not cached, handler runs twice


class TestIdempotencyStore:
    def test_store_and_lookup(self):
        store = IdempotencyStore()
        store.store("a", 200, {"content-type": "application/json"}, b'{"ok":true}')
        entry = store.lookup("a")
        assert entry is not None
        assert entry.status_code == 200
        assert entry.body == b'{"ok":true}'

    def test_missing_key_returns_none(self):
        store = IdempotencyStore()
        assert store.lookup("nonexistent") is None

    def test_expired_entry_returns_none(self):
        store = IdempotencyStore(ttl=0.01)
        store.store("b", 200, {}, b"")
        time.sleep(0.02)
        assert store.lookup("b") is None
