# --- STEP 1: Build stage ---
FROM golang:1.24-alpine AS builder

# Instaliramo git (neophodan za povlačenje gnark i ostalih eksternih biblioteka)
RUN apk add --no-cache git

WORKDIR /app

# Kopiramo definicije modula
COPY go.mod go.sum ./

# Pokušavamo download (iako go mod tidy ispod to može da popravi, ovo je dobro za keširanje)
RUN go mod download

# Kopiramo sav izvorni kod
COPY . .

# KLJUČNI KORAK: Sređujemo zavisnosti koje su pravile problem (gnark, cleanenv, itd.)
RUN go mod tidy

# Kompajliramo aplikaciju na osnovu tačne putanje sa tvoje slike
RUN CGO_ENABLED=0 GOOS=linux go build -o cloud-server ./cmd/aggregator/main.go

# --- STEP 2: Final stage ---
FROM alpine:latest

# Opciono: dodaj sertifikate ako budeš imao HTTPS pozive kasnije
RUN apk --no-cache add ca-certificates || true

WORKDIR /root/

# Kopiramo izvršni fajl iz builder faze
COPY --from=builder /app/cloud-server .

# Kopiramo tvoj config.yaml (pošto je u root-u, Docker će ga naći)
COPY --from=builder /app/config.yaml ./config.yaml

# Kopiranje foldera sa ključevima
COPY --from=builder /app/keys ./keys

# Izlažemo port (proveri da li tvoja aplikacija sluša na 8080)
EXPOSE 8080

# Pokrećemo server
CMD ["./cloud-server"]