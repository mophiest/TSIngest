#!/usr/bin/env sh
set -eu

APP_VERSION="${APP_VERSION:-0.1.0}"
IMAGE_TAR="${IMAGE_TAR:-tsingest-images-${APP_VERSION}.tar}"

if [ ! -f "$IMAGE_TAR" ]; then
  found="$(ls tsingest-images-*.tar 2>/dev/null | head -n 1 || true)"
  if [ -n "$found" ]; then
    IMAGE_TAR="$found"
  else
    echo "Cannot find image tar. Expected ${IMAGE_TAR}." >&2
    exit 1
  fi
fi

echo "Loading Docker images from ${IMAGE_TAR}..."
docker load -i "$IMAGE_TAR"
echo "Images loaded."

if [ ! -f .env ]; then
  echo ".env not found. Run ./scripts/configure-env.sh next."
else
  echo ".env already exists. You can start with: docker compose up -d"
fi
