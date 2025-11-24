#!/bin/bash
set -e

echo "Building Tailwind CSS..."

# Download and use the standalone Tailwind CLI
docker run --rm \
  -v "$(pwd):/work" \
  -w /work \
  debian:bookworm-slim \
  sh -c "apt-get update && apt-get install -y curl && \
         curl -sLO https://github.com/tailwindlabs/tailwindcss/releases/latest/download/tailwindcss-linux-x64 && \
         chmod +x tailwindcss-linux-x64 && \
         ./tailwindcss-linux-x64 -i ./web/static/css/input.css -o ./web/static/css/style.css --minify && \
         rm tailwindcss-linux-x64"

echo "✓ Tailwind CSS built successfully to web/static/css/style.css"
