# syntax=docker/dockerfile:1.7
FROM node:22-alpine AS web-build
WORKDIR /src/web
COPY web/package.json web/package-lock.json ./
RUN npm ci --no-audit --no-fund
COPY web/ ./
RUN mkdir -p public
COPY 产品logo.png ./public/logo.png
RUN npm run build

FROM golang:1.24-bookworm AS go-build
WORKDIR /src
COPY go.mod go.sum* ./
RUN go mod download
COPY cmd ./cmd
COPY internal ./internal
COPY --from=web-build /src/web/dist ./internal/ui/dist
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/tsingest ./cmd/tsingest

FROM debian:bookworm-slim
ARG APP_VERSION=0.1.0
ARG FFMPEG_DEB_VERSION=7:5.1.9-0+deb12u1
ARG DEBIAN_MIRROR=http://deb.debian.org/debian
ARG DEBIAN_SECURITY_MIRROR=http://deb.debian.org/debian-security
RUN rm -f /etc/apt/apt.conf.d/docker-clean \
    && sed -i "s|http://deb.debian.org/debian-security|${DEBIAN_SECURITY_MIRROR}|g; s|http://deb.debian.org/debian|${DEBIAN_MIRROR}|g" /etc/apt/sources.list.d/debian.sources \
    && apt-get -o Acquire::Retries=5 update \
    && DEBIAN_FRONTEND=noninteractive apt-get -o Acquire::Retries=5 install -y --no-install-recommends "ffmpeg=${FFMPEG_DEB_VERSION}" ca-certificates curl tzdata \
    && apt-get clean \
    && rm -rf /var/lib/apt/lists/* /var/cache/apt/archives/*.deb /var/cache/apt/archives/partial/*.deb /var/cache/apt/*.bin \
    && ffmpeg -hide_banner -protocols 2>&1 | grep -qE '^[[:space:]]+srt$' \
    && ffmpeg -hide_banner -formats 2>&1 | grep -qE '^[[:space:]]*DE[[:space:]]+mpegts[[:space:]]' \
    && ffmpeg -hide_banner -formats 2>&1 | grep -qE '^[[:space:]]*E[[:space:]]+mp4[[:space:]]' \
    && ffmpeg -hide_banner -decoders 2>&1 | grep -qE '^[[:space:]]*V.*[[:space:]]h264[[:space:]]' \
    && ffmpeg -hide_banner -encoders 2>&1 | grep -qE '^[[:space:]]*A.*[[:space:]]aac[[:space:]]'
RUN groupadd --gid 10001 tsingest && useradd --uid 10001 --gid 10001 --create-home --shell /usr/sbin/nologin tsingest
WORKDIR /app
COPY --from=go-build /out/tsingest /app/tsingest
RUN mkdir -p /data/recordings && chown -R tsingest:tsingest /data
USER tsingest
ENV TSINGEST_LISTEN_ADDR=:8080 \
    TSINGEST_RECORDINGS_ROOT=/data/recordings
EXPOSE 8080/tcp 9000-9099/udp
ENTRYPOINT ["/app/tsingest"]
