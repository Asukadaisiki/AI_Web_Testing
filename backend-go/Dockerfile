FROM golang:1.23-bookworm AS builder

WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/agentservice ./cmd/agentservice \
    && CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/execution-worker ./cmd/execution-worker \
    && CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/migrate ./cmd/migrate

FROM alpine:3.22 AS runtime

RUN apk add --no-cache ca-certificates \
    && addgroup -S app \
    && adduser -S -G app app

COPY --from=builder /out/agentservice /usr/local/bin/agentservice
COPY --from=builder /out/execution-worker /usr/local/bin/execution-worker
COPY --from=builder /out/migrate /usr/local/bin/migrate

USER app

EXPOSE 8081

ENTRYPOINT ["/usr/local/bin/agentservice"]
