# syntax=docker/dockerfile:1

# ---- Build Stage ----
FROM golang:1.25-alpine AS builder

ARG BIN=api
ARG VERSION=1.0.0
ARG BUILD_TIME

WORKDIR /app

# Install certificates and timezone data required for runtime HTTPS & temporal operations
RUN apk add --no-cache git ca-certificates tzdata

# Cache Go modules
COPY go.mod go.sum ./
RUN go mod download && go mod verify

# Copy source tree
COPY . .

# Compile static binary without cgo
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build \
    -trimpath \
    -ldflags="-w -s -X 'main.Version=${VERSION}' -X 'main.BuildTime=${BUILD_TIME:-$(date -u +%Y-%m-%dT%H:%M:%SZ)}'" \
    -o /app/bin/doki-app ./cmd/${BIN}

# ---- Runtime Stage ----
FROM gcr.io/distroless/static-debian12:nonroot

WORKDIR /

# Explicitly copy CA certificates and timezone database into distroless image
COPY --from=builder /usr/share/zoneinfo /usr/share/zoneinfo
COPY --from=builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/
COPY --from=builder /app/bin/doki-app /doki-app

USER nonroot:nonroot

EXPOSE 8080 9090

ENTRYPOINT ["/doki-app"]
