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

# DODATO: Neka Docker osveži zavisnosti pre bildovanja
RUN go mod tidy

# 5. Kompajliramo uz uklanjanje debug simbola (-ldflags="-s -w") za drastično manji fajl
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -ldflags="-s -w" -o mpc-server ./cmd/aggregator/main.go

# --- STEP 2: Final stage (Scratch) ---
# Koristimo 'scratch' (apsolutno prazan image, 0 MB overhead-a)
FROM scratch

WORKDIR /app/

# 6. Kopiramo sistemske esencijale iz buildera u prazan prostor
COPY --from=builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/
COPY --from=builder /usr/share/zoneinfo /usr/share/zoneinfo
COPY --from=builder /etc/passwd /etc/passwd
COPY --from=builder /etc/group /etc/group

# 7. Kopiramo tvoj kod i fajlove
COPY --from=builder /app/mpc-server .
COPY --from=builder /app/config.yaml ./config.yaml
COPY --from=builder /app/keys ./keys

# 8. Forsiramo izvršavanje pod non-root korisnikom
USER appuser:appuser

EXPOSE 8080

CMD ["./mpc-server"]