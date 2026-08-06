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

env_value() {
  key="$1"
  fallback="${2:-}"
  value="$(awk -v key="$key" '
    $0 ~ "^[[:space:]]*#" { next }
    index($0, key "=") == 1 {
      sub("^[^=]*=", "")
      print
      found=1
      exit
    }
    END {
      if (!found) exit 1
    }
  ' "$ENV_FILE" 2>/dev/null || true)"
  if [ -n "$value" ]; then
    printf '%s' "$value"
  else
    printf '%s' "$fallback"
  fi
}

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

RECORDINGS_PATH="$(env_value RECORDINGS_PATH /data/tsingest/recordings)"
if [ -z "$RECORDINGS_PATH" ] || [ "$RECORDINGS_PATH" = "/" ]; then
  echo "RECORDINGS_PATH is unsafe: ${RECORDINGS_PATH}" >&2
  exit 1
fi

echo "Preparing recordings directory ${RECORDINGS_PATH}"
mkdir -p "$RECORDINGS_PATH"
if command -v chown >/dev/null 2>&1; then
  chown -R 10001:10001 "$RECORDINGS_PATH" 2>/dev/null || echo "Warning: could not chown ${RECORDINGS_PATH}; run as root or execute: sudo chown -R 10001:10001 '${RECORDINGS_PATH}'" >&2
fi

echo "Deploying ${TSINGEST_IMAGE} (app version ${APP_VERSION})"
docker compose pull postgres web worker
docker compose up -d --no-build --force-recreate web worker
docker compose ps
