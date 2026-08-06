#!/usr/bin/env sh
set -eu

APP_VERSION="${APP_VERSION:-0.1.0}"
OUT_DIR="${OUT_DIR:-release}"
TARGET_PLATFORM="${TARGET_PLATFORM:-linux/amd64}"
GOPROXY="${GOPROXY:-https://goproxy.cn,direct}"
DEBIAN_MIRROR="${DEBIAN_MIRROR:-http://mirrors.aliyun.com/debian}"
DEBIAN_SECURITY_MIRROR="${DEBIAN_SECURITY_MIRROR:-http://mirrors.aliyun.com/debian-security}"
APP_IMAGE="tsingest:${APP_VERSION}"
POSTGRES_IMAGE="${POSTGRES_IMAGE:-postgres:17-alpine}"
BUNDLE_PLATFORM="$(printf '%s' "$TARGET_PLATFORM" | tr '/:' '--')"
BUNDLE_DIR="${OUT_DIR}/tsingest-${APP_VERSION}-${BUNDLE_PLATFORM}"
BUNDLE_TAR="${OUT_DIR}/tsingest-${APP_VERSION}-${BUNDLE_PLATFORM}-offline.tar.gz"
IMAGES_TAR="tsingest-images-${APP_VERSION}-${BUNDLE_PLATFORM}.tar"

usage() {
  cat <<'EOF'
Usage:
  scripts/package-offline.sh [options]

Options:
  --app-version VALUE     Image tag. Default: APP_VERSION env or 0.1.0
  --out-dir PATH          Output directory. Default: release
  --platform PLATFORM     Target platform. Default: linux/amd64
  --goproxy VALUE         Go module proxy. Default: https://goproxy.cn,direct
  --debian-mirror URL     Debian mirror. Default: http://mirrors.aliyun.com/debian
  --debian-security URL   Debian security mirror. Default: http://mirrors.aliyun.com/debian-security
  --postgres-image IMAGE  Database image. Default: postgres:17-alpine
  -h, --help              Show this help
EOF
}

refresh_names() {
  APP_IMAGE="tsingest:${APP_VERSION}"
  BUNDLE_PLATFORM="$(printf '%s' "$TARGET_PLATFORM" | tr '/:' '--')"
  BUNDLE_DIR="${OUT_DIR}/tsingest-${APP_VERSION}-${BUNDLE_PLATFORM}"
  BUNDLE_TAR="${OUT_DIR}/tsingest-${APP_VERSION}-${BUNDLE_PLATFORM}-offline.tar.gz"
  IMAGES_TAR="tsingest-images-${APP_VERSION}-${BUNDLE_PLATFORM}.tar"
}

while [ "$#" -gt 0 ]; do
  case "$1" in
    --app-version) APP_VERSION="$2"; refresh_names; shift 2 ;;
    --out-dir) OUT_DIR="$2"; refresh_names; shift 2 ;;
    --platform) TARGET_PLATFORM="$2"; refresh_names; shift 2 ;;
    --goproxy) GOPROXY="$2"; shift 2 ;;
    --debian-mirror) DEBIAN_MIRROR="$2"; shift 2 ;;
    --debian-security) DEBIAN_SECURITY_MIRROR="$2"; shift 2 ;;
    --postgres-image) POSTGRES_IMAGE="$2"; shift 2 ;;
    -h|--help) usage; exit 0 ;;
    *) echo "Unknown option: $1" >&2; usage >&2; exit 2 ;;
  esac
done

case "$OUT_DIR" in
  ""|"/"|".") echo "Refusing unsafe output directory: ${OUT_DIR}" >&2; exit 1 ;;
esac

export APP_VERSION
export POSTGRES_PASSWORD="${POSTGRES_PASSWORD:-build-only-postgres-password}"
export TSINGEST_ADMIN_PASSWORD="${TSINGEST_ADMIN_PASSWORD:-build-only-admin-password}"
export TSINGEST_ENCRYPTION_KEY="${TSINGEST_ENCRYPTION_KEY:-MDEyMzQ1Njc4OWFiY2RlZjAxMjM0NTY3ODlhYmNkZWY=}"

rm -rf "$BUNDLE_DIR"
mkdir -p "$BUNDLE_DIR/scripts"

echo "Building ${APP_IMAGE} for ${TARGET_PLATFORM}..."
docker buildx build \
  --platform "$TARGET_PLATFORM" \
  --build-arg "APP_VERSION=${APP_VERSION}" \
  --build-arg "GOPROXY=${GOPROXY}" \
  --build-arg "DEBIAN_MIRROR=${DEBIAN_MIRROR}" \
  --build-arg "DEBIAN_SECURITY_MIRROR=${DEBIAN_SECURITY_MIRROR}" \
  --load \
  -t "$APP_IMAGE" \
  .

echo "Pulling ${POSTGRES_IMAGE} for ${TARGET_PLATFORM}..."
docker pull --platform "$TARGET_PLATFORM" "$POSTGRES_IMAGE"

echo "Image architectures:"
docker image inspect "$APP_IMAGE" --format '  {{.RepoTags}} {{.Os}}/{{.Architecture}}'
docker image inspect "$POSTGRES_IMAGE" --format '  {{.RepoTags}} {{.Os}}/{{.Architecture}}'

echo "Saving Docker images..."
docker save -o "${BUNDLE_DIR}/${IMAGES_TAR}" "$APP_IMAGE" "$POSTGRES_IMAGE"

cp compose.yaml "$BUNDLE_DIR/compose.yaml"
cp .env.example "$BUNDLE_DIR/.env.example"
cp README.md "$BUNDLE_DIR/README.md"
cp scripts/configure-env.sh "$BUNDLE_DIR/scripts/configure-env.sh"
cp scripts/load-offline.sh "$BUNDLE_DIR/load-offline.sh"
chmod +x "$BUNDLE_DIR/scripts/configure-env.sh" "$BUNDLE_DIR/load-offline.sh"

cat > "${BUNDLE_DIR}/OFFLINE_DEPLOY.md" <<EOF
# TSIngest Offline Deploy

Target platform: ${TARGET_PLATFORM}

1. Copy this directory to the production server.
2. Run:

   \`\`\`sh
   ./load-offline.sh
   ./scripts/configure-env.sh
   sudo mkdir -p /data/tsingest/recordings
   sudo chown -R 10001:10001 /data/tsingest/recordings
   docker compose up -d
   \`\`\`

3. Open http://SERVER_IP:8080.

SRT listener UDP ports: 9000-9099.
EOF

tar -C "$OUT_DIR" -czf "$BUNDLE_TAR" "tsingest-${APP_VERSION}-${BUNDLE_PLATFORM}"

echo "Wrote ${BUNDLE_TAR}"
echo "Copy it to the production server and extract it."
