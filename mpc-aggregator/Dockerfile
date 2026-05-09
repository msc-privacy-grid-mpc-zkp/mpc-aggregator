# --- STEP 1: Build stage ---
FROM golang:1.24-alpine AS builder

# 1. Instaliramo git, ali i CA sertifikate i timezone podatke
# koje ćemo prekopirati u finalni prazan image
RUN apk add --no-cache git ca-certificates tzdata && update-ca-certificates

# 2. Kreiramo non-root korisnika za maksimalnu bezbednost
ENV USER=appuser
ENV UID=10001
RUN adduser \
    --disabled-password \
    --gecos "" \
    --home "/nonexistent" \
    --shell "/sbin/nologin" \
    --no-create-home \
    --uid "${UID}" \
    "${USER}"

WORKDIR /app

# 3. Kopiramo SAMO definicije modula (Savršeno Docker keširanje)
COPY go.mod go.sum ./
RUN go mod download

# 4. Tek sada kopiramo ostatak koda
COPY . .

# NOTE: Refresh dependencies before building
RUN go mod tidy

# 5. Kompajliramo uz uklanjanje debug simbola (-ldflags="-s -w") za drastično manji fajl
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -ldflags="-s -w" -o mpc-server ./cmd/aggregator/main.go

# --- STEP 2: Final stage ---
# Use a minimal alpine base to allow shell-based startup wrappers (wait-for-files)
FROM alpine:3.18

# Install CA certificates and timezone data
RUN apk add --no-cache ca-certificates tzdata && update-ca-certificates

WORKDIR /app/

# Copy runtime essentials from builder
COPY --from=builder /app/mpc-server .
COPY --from=builder /app/config.yaml ./config.yaml
COPY --from=builder /app/keys ./keys

# Create non-root user (mirror builder user to keep same UID)
RUN addgroup -S appuser && adduser -S -G appuser -u 10001 appuser

# Switch to non-root user
USER appuser:appuser

EXPOSE 8080

CMD ["./mpc-server"]