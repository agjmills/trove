FROM debian:bookworm-slim AS css-builder

ARG TARGETARCH

RUN apt-get update && apt-get install -y curl && rm -rf /var/lib/apt/lists/*

WORKDIR /css

# Download the correct Tailwind CLI binary for the target architecture
RUN case ${TARGETARCH} in \
    "amd64")  TWARCH=x64  ;; \
    "arm64")  TWARCH=arm64  ;; \
    *)        echo "Unsupported architecture: ${TARGETARCH}" && exit 1  ;; \
    esac && \
    curl -sLO https://github.com/tailwindlabs/tailwindcss/releases/latest/download/tailwindcss-linux-${TWARCH} && \
    mv tailwindcss-linux-${TWARCH} tailwindcss && \
    chmod +x tailwindcss

COPY web/static/css/input.css ./web/static/css/input.css
COPY web/templates ./web/templates
COPY tailwind.config.js ./

RUN ./tailwindcss \
    -i ./web/static/css/input.css \
    -o ./web/static/css/style.css \
    --minify

# Stage 2: Build Go binary
FROM golang:1.25-alpine AS builder

RUN apk add --no-cache git ca-certificates tzdata

WORKDIR /build

COPY go.mod go.sum ./
RUN go mod download

COPY . .

RUN CGO_ENABLED=0 GOOS=linux go build \
    -ldflags="-s -w" \
    -o trove \
    ./cmd/server

FROM scratch

COPY --from=builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/
COPY --from=builder /usr/share/zoneinfo /usr/share/zoneinfo

WORKDIR /app

COPY --from=builder /build/trove /app/trove
COPY --from=builder /build/web /app/web

COPY --from=css-builder /css/web/static/css/style.css /app/web/static/css/style.css

EXPOSE 8080

ENTRYPOINT ["/app/trove"]
