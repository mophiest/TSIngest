#!/usr/bin/env sh
set -eu

ENV_FILE=".env"
ROLE="all"
SKIP_FRONTEND="false"
NO_POSTGRES="false"
POSTGRES_HOST="${POSTGRES_HOST:-127.0.0.1}"
POSTGRES_PORT="${POSTGRES_PORT:-5432}"

usage() {
  cat <<'EOF'
Usage:
  scripts/dev-run.sh [options]

Options:
  --env-file PATH       Read config from PATH. Default: .env
  --role all|web|worker Run both roles or one role. Default: all
  --skip-frontend       Do not rebuild web/dist and internal/ui/dist
  --no-postgres         Do not start Docker Postgres
  --postgres-host HOST  Database host for source-run. Default: 127.0.0.1
  --postgres-port PORT  Database port for source-run. Default: 5432
  -h, --help            Show this help
EOF
}

while [ "$#" -gt 0 ]; do
  case "$1" in
    --env-file) ENV_FILE="$2"; shift 2 ;;
    --role) ROLE="$2"; shift 2 ;;
    --skip-frontend) SKIP_FRONTEND="true"; shift ;;
    --no-postgres) NO_POSTGRES="true"; shift ;;
    --postgres-host) POSTGRES_HOST="$2"; shift 2 ;;
    --postgres-port) POSTGRES_PORT="$2"; shift 2 ;;
    -h|--help) usage; exit 0 ;;
    *) echo "Unknown option: $1" >&2; usage >&2; exit 2 ;;
  esac
done

case "$ROLE" in
  all|web|worker) ;;
  *) echo "--role must be all, web, or worker." >&2; exit 2 ;;
esac

need() {
  if ! command -v "$1" >/dev/null 2>&1; then
    echo "Missing command: $1" >&2
    exit 1
  fi
}

env_value() {
  key="$1"
  fallback="${2:-}"
  if [ ! -f "$ENV_FILE" ]; then
    printf '%s' "$fallback"
    return
  fi
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

urlencode() {
  VALUE="$1" node -e 'process.stdout.write(encodeURIComponent(process.env.VALUE || ""))'
}

need go
need node
need npm
need ffmpeg
need ffprobe

if [ ! -f "$ENV_FILE" ]; then
  echo "$ENV_FILE not found. Run ./scripts/configure-env.sh first." >&2
  exit 1
fi

WEB_PORT="$(env_value WEB_PORT 8080)"
RECORDINGS_PATH="$(env_value RECORDINGS_PATH /data/tsingest/recordings)"
POSTGRES_PASSWORD="$(env_value POSTGRES_PASSWORD)"
ADMIN_USERNAME="$(env_value TSINGEST_ADMIN_USERNAME admin)"
ADMIN_PASSWORD="$(env_value TSINGEST_ADMIN_PASSWORD)"
ENCRYPTION_KEY="$(env_value TSINGEST_ENCRYPTION_KEY)"
WORKER_ID="$(env_value TSINGEST_WORKER_ID recorder-01)"
MAX_ACTIVE="$(env_value TSINGEST_MAX_ACTIVE_RECORDINGS 64)"
MP4_CONCURRENCY="$(env_value TSINGEST_MP4_CONCURRENCY 2)"
COOKIE_SECURE="$(env_value TSINGEST_COOKIE_SECURE false)"
PUBLIC_URL="$(env_value TSINGEST_PUBLIC_URL)"

if [ -z "$POSTGRES_PASSWORD" ] || [ -z "$ADMIN_PASSWORD" ] || [ -z "$ENCRYPTION_KEY" ]; then
  echo ".env is missing POSTGRES_PASSWORD, TSINGEST_ADMIN_PASSWORD, or TSINGEST_ENCRYPTION_KEY." >&2
  exit 1
fi

if [ "$NO_POSTGRES" != "true" ]; then
  need docker
  echo "Starting Postgres on ${POSTGRES_HOST}:${POSTGRES_PORT}..."
  POSTGRES_PASSWORD="$POSTGRES_PASSWORD" \
  TSINGEST_ADMIN_PASSWORD="$ADMIN_PASSWORD" \
  TSINGEST_ENCRYPTION_KEY="$ENCRYPTION_KEY" \
  POSTGRES_HOST="$POSTGRES_HOST" \
  POSTGRES_PORT="$POSTGRES_PORT" \
    docker compose -f compose.yaml -f compose.dev.yaml up -d postgres

  tries=0
  until POSTGRES_PASSWORD="$POSTGRES_PASSWORD" docker compose -f compose.yaml -f compose.dev.yaml exec -T postgres pg_isready -U tsingest -d tsingest >/dev/null 2>&1; do
    tries=$((tries + 1))
    if [ "$tries" -ge 40 ]; then
      echo "Postgres did not become ready. Check: docker compose -f compose.yaml -f compose.dev.yaml logs postgres" >&2
      exit 1
    fi
    sleep 1
  done
fi

if [ "$SKIP_FRONTEND" != "true" ]; then
  echo "Building frontend..."
  (cd web && npm ci --no-audit --no-fund && npm run build)
  mkdir -p internal/ui/dist
  cp -R web/dist/. internal/ui/dist/
fi

mkdir -p .dev
echo "Building Go debug binary..."
go build -o .dev/tsingest ./cmd/tsingest

encoded_password="$(urlencode "$POSTGRES_PASSWORD")"
database_url="postgres://tsingest:${encoded_password}@${POSTGRES_HOST}:${POSTGRES_PORT}/tsingest?sslmode=disable"

export TSINGEST_DATABASE_URL="$database_url"
export TSINGEST_RECORDINGS_ROOT="$RECORDINGS_PATH"
export TSINGEST_ADMIN_USERNAME="$ADMIN_USERNAME"
export TSINGEST_ADMIN_PASSWORD="$ADMIN_PASSWORD"
export TSINGEST_ENCRYPTION_KEY="$ENCRYPTION_KEY"
export TSINGEST_LISTEN_ADDR=":${WEB_PORT}"
export TSINGEST_COOKIE_SECURE="$COOKIE_SECURE"
export TSINGEST_PUBLIC_URL="$PUBLIC_URL"
export TSINGEST_WORKER_ID="$WORKER_ID"
export TSINGEST_MAX_ACTIVE_RECORDINGS="$MAX_ACTIVE"
export TSINGEST_MP4_CONCURRENCY="$MP4_CONCURRENCY"
export TSINGEST_FFMPEG="${TSINGEST_FFMPEG:-ffmpeg}"
export TSINGEST_FFPROBE="${TSINGEST_FFPROBE:-ffprobe}"

run_one() {
  TSINGEST_ROLE="$1" ./.dev/tsingest
}

case "$ROLE" in
  web)
    echo "Starting web on http://0.0.0.0:${WEB_PORT}"
    exec env TSINGEST_ROLE=web ./.dev/tsingest
    ;;
  worker)
    echo "Starting worker..."
    exec env TSINGEST_ROLE=worker ./.dev/tsingest
    ;;
  all)
    echo "Starting web on http://0.0.0.0:${WEB_PORT}"
    run_one web &
    web_pid=$!
    echo "Starting worker..."
    run_one worker &
    worker_pid=$!
    cleanup() {
      kill -INT "$web_pid" "$worker_pid" >/dev/null 2>&1 || true
      wait "$web_pid" "$worker_pid" >/dev/null 2>&1 || true
    }
    trap cleanup INT TERM EXIT
    wait "$web_pid" "$worker_pid"
    ;;
esac
