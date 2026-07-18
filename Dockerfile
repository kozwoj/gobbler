# ─── Build stage ─────────────────────────────────────────────────────────────
FROM golang:1.24-alpine AS builder

WORKDIR /build

# Download modules first — cached as a separate layer unless go.mod/go.sum change
COPY go.mod go.sum ./
RUN go mod download

COPY . .

RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o gobbler .

# ─── Runtime stage ────────────────────────────────────────────────────────────
FROM alpine:latest

# CA certificates are required for Azure blob HTTPS connections (blob mode).
RUN apk add --no-cache ca-certificates

WORKDIR /gobbler

COPY --from=builder /build/gobbler .

# Default listen port. Override by passing -port <n> as docker run arguments.
EXPOSE 8080

ENTRYPOINT ["./gobbler"]
CMD ["-port", "8080"]
