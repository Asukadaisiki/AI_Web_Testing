FROM ghcr.io/astral-sh/uv:0.7-python3.13-bookworm

ENV PLAYWRIGHT_BROWSERS_PATH=/ms-playwright

WORKDIR /app/worker

ARG UV_PACKAGE_BASE=https://files.pythonhosted.org
ARG DEBIAN_MIRROR=http://deb.debian.org/debian
ARG DEBIAN_SECURITY_MIRROR=http://deb.debian.org/debian-security

COPY worker/pyproject.toml worker/uv.lock ./
RUN if [ "$UV_PACKAGE_BASE" != "https://files.pythonhosted.org" ]; then \
        sed -i "s#https://files.pythonhosted.org#${UV_PACKAGE_BASE%/}#g" uv.lock; \
    fi \
    && UV_HTTP_TIMEOUT=120 UV_HTTP_RETRIES=5 uv sync --frozen --no-dev
RUN sed -i \
        -e "s#http://deb.debian.org/debian-security#${DEBIAN_SECURITY_MIRROR%/}#g" \
        -e "s#http://deb.debian.org/debian#${DEBIAN_MIRROR%/}#g" \
        /etc/apt/sources.list.d/debian.sources \
    && uv run --no-sync playwright install --with-deps chromium

COPY worker/loop_worker ./loop_worker

# Supply private provider token, stable profile key/root and shared artifact mount.
CMD ["uv", "run", "--no-sync", "uvicorn", "loop_worker.main:app", "--host", "0.0.0.0", "--port", "8110"]
