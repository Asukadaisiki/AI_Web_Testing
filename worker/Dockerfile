FROM ghcr.io/astral-sh/uv:0.7-python3.13-bookworm

ENV PLAYWRIGHT_BROWSERS_PATH=/ms-playwright

WORKDIR /app/worker

COPY worker/pyproject.toml worker/uv.lock ./
RUN uv sync --frozen --no-dev
RUN uv run --no-sync playwright install --with-deps chromium

COPY worker/loop_worker ./loop_worker
