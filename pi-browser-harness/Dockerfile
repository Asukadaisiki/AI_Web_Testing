# syntax=docker/dockerfile:1.6
# Multi-stage runtime image: builds the pi-browser-harness with Node.js and
# provides a Go + Node runtime for the agenttrace-runtime consumer. The runtime
# spawns the harness as a subprocess, so both toolchains must be present.

# ---- Stage 1: build the harness ----
FROM node:22-bookworm AS harness-build

WORKDIR /build/pi-browser-harness

COPY pi-browser-harness/package.json pi-browser-harness/package-lock.json ./
RUN npm ci --no-audit --no-fund

COPY pi-browser-harness/ ./
RUN npm run build \
    && npm prune --omit=dev

# ---- Stage 2: runtime with Go + Node.js + built harness ----
FROM golang:1.24-bookworm AS runtime

# Install Node.js 22 (>= 22.19.0 required by the harness engines field).
RUN apt-get update \
    && apt-get install -y --no-install-recommends ca-certificates curl gnupg \
    && mkdir -p /etc/apt/keyrings \
    && curl -fsSL https://deb.nodesource.com/gpgkey/nodesource-repo.gpg.key \
        | gpg --dearmor -o /etc/apt/keyrings/nodesource.gpg \
    && echo "deb [signed-by=/etc/apt/keyrings/nodesource.gpg] https://deb.nodesource.com/node_22.x nodistro main" \
        > /etc/apt/sources.list.d/nodesource.list \
    && apt-get update \
    && apt-get install -y --no-install-recommends nodejs \
    && rm -rf /var/lib/apt/lists/*

# Verify the Node.js version meets the harness requirement.
RUN node --version | grep -E '^v(22\.(1[9-9]|[2-9][0-9])|2[3-9]|[3-9][0-9])\.' \
    || (echo "Node.js >= 22.19.0 is required" && exit 1)

WORKDIR /app

# Copy the built harness.
COPY --from=harness-build /build/pi-browser-harness/dist /app/pi-browser-harness/dist
COPY --from=harness-build /build/pi-browser-harness/resources /app/pi-browser-harness/resources
COPY --from=harness-build /build/pi-browser-harness/package.json /app/pi-browser-harness/package.json
COPY --from=harness-build /build/pi-browser-harness/node_modules /app/pi-browser-harness/node_modules

# Copy the Go source so the runtime can run the consumer.
COPY backend /app/backend

# Pre-fetch Go modules for faster container startup.
WORKDIR /app/backend
RUN go mod download

WORKDIR /app/backend
