# === Phase 1: Build & Provision ===
FROM golang:1.26-alpine3.24 AS builder

RUN apk update && apk add --no-cache ca-certificates && update-ca-certificates
RUN adduser -D -g '' -u 10001 redb

WORKDIR /app

# Copy dependency tracking files
COPY go.mod ./

# Copy internal engineering blocks and commands
COPY cmd/ ./cmd/
COPY internal/ ./internal/

# Compile both binaries statically
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o redb ./cmd/redb/main.go
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o recli ./cmd/recli/main.go

# Set up secure storage path for the volume target
RUN mkdir -p /home/redb/data && chown -R 10001:10001 /home/redb/data

# === Phase 2: Ultimate Scratch Runtime ===
FROM scratch

COPY --from=builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/
COPY --from=builder /etc/passwd /etc/passwd

WORKDIR /home/redb/

# Extract the compiled engine and the client tool
COPY --from=builder --chown=10001:10001 /app/redb .
COPY --from=builder --chown=10001:10001 /app/recli .
COPY --from=builder --chown=10001:10001 /home/redb/data ./data

USER 10001

# Production Environment Variables Defaults
ENV REDB_PORT=7800
ENV REDB_DATA_DIR=/home/redb/data

# Expose the correct custom database TCP port
EXPOSE 7800

CMD ["./redb"]