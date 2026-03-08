"""Smoke tests for the FastAPI application."""

from fastapi.testclient import TestClient

from app.main import app


client = TestClient(app)


def test_root_metadata() -> None:
    response = client.get("/")

    assert response.status_code == 200
    assert response.json() == {
        "name": "AI Web Testing Backend",
        "environment": "development",
        "docs_url": "/docs",
    }


def test_health_endpoint() -> None:
    response = client.get("/api/v1/health")

    assert response.status_code == 200
    assert response.json() == {
        "status": "ok",
        "service": "AI Web Testing Backend",
        "environment": "development",
        "version": "0.1.0",
    }
