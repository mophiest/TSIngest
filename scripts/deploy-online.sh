#!/usr/bin/env sh
set -eu

ENV_FILE="${ENV_FILE:-.env}"

usage() {
  cat <<'EOF'
Usage:
  scripts/deploy-online.sh [options]

Options:
  --env-file PATH       Env file. Default: .env
  --image IMAGE         TSIngest image to deploy
  --app-version VALUE   App version. Used when --image is omitted
  -h, --help            Show this help

This script deploys prebuilt images. It does not build on the production host.
EOF
}

APP_VERSION="${APP_VERSION:-0.1.0}"
TSINGEST_IMAGE="${TSINGEST_IMAGE:-}"

while [ "$#" -gt 0 ]; do
  case "$1" in
    --env-file) ENV_FILE="$2"; shift 2 ;;
    --image) TSINGEST_IMAGE="$2"; shift 2 ;;
    --app-version) APP_VERSION="$2"; shift 2 ;;
    -h|--help) usage; exit 0 ;;
    *) echo "Unknown option: $1" >&2; usage >&2; exit 2 ;;
  esac
done

if [ ! -f "$ENV_FILE" ]; then
  echo "$ENV_FILE not found. Run ./scripts/configure-env.sh first." >&2
  exit 1
fi

if [ -z "$TSINGEST_IMAGE" ]; then
  TSINGEST_IMAGE="ghcr.io/mophiest/tsingest:${APP_VERSION}"
else
  image_tag="${TSINGEST_IMAGE##*:}"
  if [ -n "$image_tag" ] && [ "$image_tag" != "$TSINGEST_IMAGE" ] && [ "$image_tag" != "latest" ]; then
    APP_VERSION="$image_tag"
  fi
fi

set_env_value() {
  key="$1"
  value="$2"
  if grep -q "^${key}=" "$ENV_FILE"; then
    tmp_file="${ENV_FILE}.tmp.$$"
    sed "s|^${key}=.*|${key}=${value}|" "$ENV_FILE" > "$tmp_file"
    mv "$tmp_file" "$ENV_FILE"
  else
    printf '\n%s=%s\n' "$key" "$value" >> "$ENV_FILE"
  fi
}

set_env_value TSINGEST_IMAGE "$TSINGEST_IMAGE"
set_env_value APP_VERSION "$APP_VERSION"

echo "Deploying ${TSINGEST_IMAGE} (app version ${APP_VERSION})"
docker compose pull postgres web worker
docker compose up -d --no-build --force-recreate web worker
docker compose ps
