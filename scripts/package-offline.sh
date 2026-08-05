#!/usr/bin/env sh
set -eu

APP_VERSION="${APP_VERSION:-0.1.0}"
OUT_DIR="${OUT_DIR:-release}"
APP_IMAGE="tsingest:${APP_VERSION}"
POSTGRES_IMAGE="${POSTGRES_IMAGE:-postgres:17-alpine}"
BUNDLE_DIR="${OUT_DIR}/tsingest-${APP_VERSION}"
BUNDLE_TAR="${OUT_DIR}/tsingest-${APP_VERSION}-offline.tar.gz"
IMAGES_TAR="tsingest-images-${APP_VERSION}.tar"

usage() {
  cat <<'EOF'
Usage:
  scripts/package-offline.sh [options]

Options:
  --app-version VALUE     Image tag. Default: APP_VERSION env or 0.1.0
  --out-dir PATH          Output directory. Default: release
  --postgres-image IMAGE  Database image. Default: postgres:17-alpine
  -h, --help              Show this help
EOF
}

while [ "$#" -gt 0 ]; do
  case "$1" in
    --app-version) APP_VERSION="$2"; APP_IMAGE="tsingest:${APP_VERSION}"; BUNDLE_DIR="${OUT_DIR}/tsingest-${APP_VERSION}"; BUNDLE_TAR="${OUT_DIR}/tsingest-${APP_VERSION}-offline.tar.gz"; IMAGES_TAR="tsingest-images-${APP_VERSION}.tar"; shift 2 ;;
    --out-dir) OUT_DIR="$2"; BUNDLE_DIR="${OUT_DIR}/tsingest-${APP_VERSION}"; BUNDLE_TAR="${OUT_DIR}/tsingest-${APP_VERSION}-offline.tar.gz"; shift 2 ;;
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

echo "Building ${APP_IMAGE}..."
docker compose build

echo "Pulling ${POSTGRES_IMAGE}..."
docker pull "$POSTGRES_IMAGE"

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

tar -C "$OUT_DIR" -czf "$BUNDLE_TAR" "tsingest-${APP_VERSION}"

echo "Wrote ${BUNDLE_TAR}"
echo "Copy it to the production server and extract it."
