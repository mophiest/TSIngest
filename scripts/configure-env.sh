#!/usr/bin/env sh
set -eu

ENV_FILE=".env"
FORCE="false"
NON_INTERACTIVE="false"

APP_VERSION="${APP_VERSION:-0.1.0}"
WEB_PORT="${WEB_PORT:-8080}"
RECORDINGS_PATH="${RECORDINGS_PATH:-/data/tsingest/recordings}"
TSINGEST_ADMIN_USERNAME="${TSINGEST_ADMIN_USERNAME:-admin}"
TSINGEST_ADMIN_PASSWORD="${TSINGEST_ADMIN_PASSWORD:-}"
POSTGRES_PASSWORD="${POSTGRES_PASSWORD:-}"
TSINGEST_ENCRYPTION_KEY="${TSINGEST_ENCRYPTION_KEY:-}"
TSINGEST_WORKER_ID="${TSINGEST_WORKER_ID:-recorder-01}"
TSINGEST_MAX_ACTIVE_RECORDINGS="${TSINGEST_MAX_ACTIVE_RECORDINGS:-64}"
TSINGEST_MP4_CONCURRENCY="${TSINGEST_MP4_CONCURRENCY:-2}"
TSINGEST_COOKIE_SECURE="${TSINGEST_COOKIE_SECURE:-false}"
TSINGEST_PUBLIC_URL="${TSINGEST_PUBLIC_URL:-}"
TSINGEST_DOCKER_SUBNET="${TSINGEST_DOCKER_SUBNET:-172.31.240.0/24}"
TSINGEST_SECCOMP_PROFILE="${TSINGEST_SECCOMP_PROFILE:-unconfined}"
GOPROXY="${GOPROXY:-https://proxy.golang.org,direct}"
DEBIAN_MIRROR="${DEBIAN_MIRROR:-http://deb.debian.org/debian}"
DEBIAN_SECURITY_MIRROR="${DEBIAN_SECURITY_MIRROR:-http://deb.debian.org/debian-security}"

usage() {
  cat <<'EOF'
Usage:
  scripts/configure-env.sh [options]

Options:
  --env-file PATH                 Write config to PATH. Default: .env
  --app-version VALUE             Docker image tag. Default: 0.1.0
  --web-port PORT                 Host web port. Default: 8080
  --recordings-path PATH          Host recordings directory. Default: /data/tsingest/recordings
  --admin-user VALUE              Admin username. Default: admin
  --admin-password VALUE          Admin password. Random if omitted in non-interactive mode
  --postgres-password VALUE       Database password. Random if omitted in non-interactive mode
  --encryption-key VALUE          Base64 32-byte key. Random if omitted
  --worker-id VALUE               Worker id. Default: recorder-01
  --max-active VALUE              Max active recordings. Default: 64
  --mp4-concurrency VALUE         MP4 conversion concurrency. Default: 2
  --cookie-secure true|false      Secure cookie flag behind HTTPS. Default: false
  --public-url URL                Public URL behind reverse proxy
  --docker-subnet CIDR            Compose bridge subnet. Default: 172.31.240.0/24
  --seccomp-profile VALUE         Worker seccomp profile. Default: unconfined
  --goproxy VALUE                 Go module proxy used while building images
  --debian-mirror URL             Debian package mirror used while building images
  --debian-security-mirror URL    Debian security mirror used while building images
  --non-interactive               Do not prompt; generate missing secrets
  --force                         Overwrite existing env file
  -h, --help                      Show this help
EOF
}

random_secret() {
  openssl rand -base64 32 | tr -d '\n'
}

while [ "$#" -gt 0 ]; do
  case "$1" in
    --env-file) ENV_FILE="$2"; shift 2 ;;
    --app-version) APP_VERSION="$2"; shift 2 ;;
    --web-port) WEB_PORT="$2"; shift 2 ;;
    --recordings-path) RECORDINGS_PATH="$2"; shift 2 ;;
    --admin-user) TSINGEST_ADMIN_USERNAME="$2"; shift 2 ;;
    --admin-password) TSINGEST_ADMIN_PASSWORD="$2"; shift 2 ;;
    --postgres-password) POSTGRES_PASSWORD="$2"; shift 2 ;;
    --encryption-key) TSINGEST_ENCRYPTION_KEY="$2"; shift 2 ;;
    --worker-id) TSINGEST_WORKER_ID="$2"; shift 2 ;;
    --max-active) TSINGEST_MAX_ACTIVE_RECORDINGS="$2"; shift 2 ;;
    --mp4-concurrency) TSINGEST_MP4_CONCURRENCY="$2"; shift 2 ;;
    --cookie-secure) TSINGEST_COOKIE_SECURE="$2"; shift 2 ;;
    --public-url) TSINGEST_PUBLIC_URL="$2"; shift 2 ;;
    --docker-subnet) TSINGEST_DOCKER_SUBNET="$2"; shift 2 ;;
    --seccomp-profile) TSINGEST_SECCOMP_PROFILE="$2"; shift 2 ;;
    --goproxy) GOPROXY="$2"; shift 2 ;;
    --debian-mirror) DEBIAN_MIRROR="$2"; shift 2 ;;
    --debian-security-mirror) DEBIAN_SECURITY_MIRROR="$2"; shift 2 ;;
    --non-interactive) NON_INTERACTIVE="true"; shift ;;
    --force) FORCE="true"; shift ;;
    -h|--help) usage; exit 0 ;;
    *) echo "Unknown option: $1" >&2; usage >&2; exit 2 ;;
  esac
done

if [ -f "$ENV_FILE" ] && [ "$FORCE" != "true" ]; then
  echo "$ENV_FILE already exists. Use --force to overwrite." >&2
  exit 1
fi

prompt_value() {
  name="$1"
  current="$2"
  label="$3"
  if [ "$NON_INTERACTIVE" = "true" ]; then
    printf '%s' "$current"
    return
  fi
  printf '%s [%s]: ' "$label" "$current" >&2
  IFS= read -r value || value=""
  if [ -n "$value" ]; then
    printf '%s' "$value"
  else
    printf '%s' "$current"
  fi
}

prompt_secret() {
  current="$1"
  label="$2"
  if [ -n "$current" ]; then
    printf '%s' "$current"
    return
  fi
  if [ "$NON_INTERACTIVE" = "true" ]; then
    random_secret
    return
  fi
  printf '%s [auto-generate]: ' "$label" >&2
  IFS= read -r value || value=""
  if [ -n "$value" ]; then
    printf '%s' "$value"
  else
    random_secret
  fi
}

APP_VERSION="$(prompt_value APP_VERSION "$APP_VERSION" "Image tag")"
WEB_PORT="$(prompt_value WEB_PORT "$WEB_PORT" "Web port")"
RECORDINGS_PATH="$(prompt_value RECORDINGS_PATH "$RECORDINGS_PATH" "Recordings path")"
TSINGEST_ADMIN_USERNAME="$(prompt_value TSINGEST_ADMIN_USERNAME "$TSINGEST_ADMIN_USERNAME" "Admin username")"
TSINGEST_ADMIN_PASSWORD="$(prompt_secret "$TSINGEST_ADMIN_PASSWORD" "Admin password")"
POSTGRES_PASSWORD="$(prompt_secret "$POSTGRES_PASSWORD" "Postgres password")"
TSINGEST_ENCRYPTION_KEY="$(prompt_secret "$TSINGEST_ENCRYPTION_KEY" "Encryption key")"
TSINGEST_WORKER_ID="$(prompt_value TSINGEST_WORKER_ID "$TSINGEST_WORKER_ID" "Worker id")"
TSINGEST_MAX_ACTIVE_RECORDINGS="$(prompt_value TSINGEST_MAX_ACTIVE_RECORDINGS "$TSINGEST_MAX_ACTIVE_RECORDINGS" "Max active recordings")"
TSINGEST_MP4_CONCURRENCY="$(prompt_value TSINGEST_MP4_CONCURRENCY "$TSINGEST_MP4_CONCURRENCY" "MP4 concurrency")"
TSINGEST_COOKIE_SECURE="$(prompt_value TSINGEST_COOKIE_SECURE "$TSINGEST_COOKIE_SECURE" "Secure cookie true/false")"
TSINGEST_PUBLIC_URL="$(prompt_value TSINGEST_PUBLIC_URL "$TSINGEST_PUBLIC_URL" "Public URL, empty for LAN HTTP")"
TSINGEST_DOCKER_SUBNET="$(prompt_value TSINGEST_DOCKER_SUBNET "$TSINGEST_DOCKER_SUBNET" "Docker bridge subnet")"
TSINGEST_SECCOMP_PROFILE="$(prompt_value TSINGEST_SECCOMP_PROFILE "$TSINGEST_SECCOMP_PROFILE" "Worker seccomp profile")"
GOPROXY="$(prompt_value GOPROXY "$GOPROXY" "Go module proxy")"
DEBIAN_MIRROR="$(prompt_value DEBIAN_MIRROR "$DEBIAN_MIRROR" "Debian mirror")"
DEBIAN_SECURITY_MIRROR="$(prompt_value DEBIAN_SECURITY_MIRROR "$DEBIAN_SECURITY_MIRROR" "Debian security mirror")"

case "$TSINGEST_COOKIE_SECURE" in
  true|false) ;;
  *) echo "TSINGEST_COOKIE_SECURE must be true or false." >&2; exit 1 ;;
esac

case "$APP_VERSION" in
  ""|*/*|*\\*|*:*|*" "*)
    echo "APP_VERSION must be a Docker tag such as 0.1.0, not: $APP_VERSION" >&2
    exit 1
    ;;
esac

case "$WEB_PORT" in
  *[!0-9]*|"") echo "WEB_PORT must be a number." >&2; exit 1 ;;
esac

case "$TSINGEST_ENCRYPTION_KEY" in
  replace-with-*|"") echo "TSINGEST_ENCRYPTION_KEY is invalid." >&2; exit 1 ;;
esac

mkdir -p "$(dirname "$ENV_FILE")"
tmp_file="${ENV_FILE}.tmp.$$"
umask 077
cat > "$tmp_file" <<EOF
APP_VERSION=$APP_VERSION
WEB_PORT=$WEB_PORT
RECORDINGS_PATH=$RECORDINGS_PATH

POSTGRES_PASSWORD=$POSTGRES_PASSWORD
TSINGEST_ADMIN_USERNAME=$TSINGEST_ADMIN_USERNAME
TSINGEST_ADMIN_PASSWORD=$TSINGEST_ADMIN_PASSWORD
TSINGEST_ENCRYPTION_KEY=$TSINGEST_ENCRYPTION_KEY

TSINGEST_WORKER_ID=$TSINGEST_WORKER_ID
TSINGEST_MAX_ACTIVE_RECORDINGS=$TSINGEST_MAX_ACTIVE_RECORDINGS
TSINGEST_MP4_CONCURRENCY=$TSINGEST_MP4_CONCURRENCY
TSINGEST_COOKIE_SECURE=$TSINGEST_COOKIE_SECURE
TSINGEST_PUBLIC_URL=$TSINGEST_PUBLIC_URL
TSINGEST_DOCKER_SUBNET=$TSINGEST_DOCKER_SUBNET
TSINGEST_SECCOMP_PROFILE=$TSINGEST_SECCOMP_PROFILE
GOPROXY=$GOPROXY
DEBIAN_MIRROR=$DEBIAN_MIRROR
DEBIAN_SECURITY_MIRROR=$DEBIAN_SECURITY_MIRROR
EOF
mv "$tmp_file" "$ENV_FILE"

echo "Wrote $ENV_FILE"
echo "Admin username: $TSINGEST_ADMIN_USERNAME"
echo "Recordings path: $RECORDINGS_PATH"
echo "Next:"
echo "  sudo mkdir -p '$RECORDINGS_PATH'"
echo "  sudo chown -R 10001:10001 '$RECORDINGS_PATH'"
echo "  docker compose up -d"
