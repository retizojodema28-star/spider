FROM golang:1.25.0 AS builder

WORKDIR /src
COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod go mod download
COPY . .
RUN --mount=type=cache,target=/go/pkg/mod --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/site ./cmd/site && \
    CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/worker ./cmd/worker

FROM alpine:3.22
WORKDIR /app
RUN addgroup -S app && adduser -S -G app app && mkdir -p /app/storage && chown app:app /app/storage
COPY --from=builder /out/site /app/site
COPY --from=builder /out/worker /app/worker
COPY assets /app/assets
COPY data/seed /app/data/seed
COPY data/lan /app/data/lan
USER app
EXPOSE 8080
ENTRYPOINT ["/app/site"]
