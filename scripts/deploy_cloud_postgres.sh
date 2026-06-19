#!/usr/bin/env bash

set -euo pipefail

REPO_URL="${REPO_URL:-https://github.com/psmtnhljs/npd_fixsql.git}"
BRANCH="${BRANCH:-main}"
INSTALL_DIR="${INSTALL_DIR:-/opt/npd_fixsql}"
APP_PORT="${APP_PORT:-3000}"
POSTGRES_DB="${POSTGRES_DB:-nodepassdash}"
POSTGRES_USER="${POSTGRES_USER:-nodepass}"
POSTGRES_PASSWORD="${POSTGRES_PASSWORD:-}"
DB_SSLMODE="${DB_SSLMODE:-disable}"
DB_TIMEZONE="${DB_TIMEZONE:-Asia/Shanghai}"
LOG_LEVEL="${LOG_LEVEL:-INFO}"

if [[ -z "${POSTGRES_PASSWORD}" ]]; then
  POSTGRES_PASSWORD="$(tr -dc 'A-Za-z0-9' </dev/urandom | head -c 24)"
fi

log() {
  echo "[deploy] $*"
}

install_base_packages() {
  if command -v apt-get >/dev/null 2>&1; then
    export DEBIAN_FRONTEND=noninteractive
    apt-get update
    apt-get install -y ca-certificates curl git gnupg lsb-release
  elif command -v dnf >/dev/null 2>&1; then
    dnf install -y ca-certificates curl git
  elif command -v yum >/dev/null 2>&1; then
    yum install -y ca-certificates curl git
  else
    echo "Unsupported Linux distribution. Please install git and docker manually." >&2
    exit 1
  fi
}

install_docker_if_missing() {
  if command -v docker >/dev/null 2>&1; then
    log "Docker already installed"
    return
  fi

  log "Installing Docker"
  curl -fsSL https://get.docker.com | sh
  systemctl enable docker
  systemctl start docker
}

ensure_compose() {
  if docker compose version >/dev/null 2>&1; then
    return
  fi
  echo "Docker Compose plugin is required but not available." >&2
  exit 1
}

prepare_repo() {
  mkdir -p "$(dirname "$INSTALL_DIR")"
  if [[ -d "$INSTALL_DIR/.git" ]]; then
    log "Updating existing repository"
    git -C "$INSTALL_DIR" fetch --all --prune
    git -C "$INSTALL_DIR" checkout "$BRANCH"
    git -C "$INSTALL_DIR" pull --ff-only origin "$BRANCH"
  else
    log "Cloning repository"
    git clone --branch "$BRANCH" "$REPO_URL" "$INSTALL_DIR"
  fi
}

write_env_file() {
  cat > "$INSTALL_DIR/.env" <<EOF
APP_PORT=${APP_PORT}
POSTGRES_DB=${POSTGRES_DB}
POSTGRES_USER=${POSTGRES_USER}
POSTGRES_PASSWORD=${POSTGRES_PASSWORD}
DB_SSLMODE=${DB_SSLMODE}
DB_TIMEZONE=${DB_TIMEZONE}
LOG_LEVEL=${LOG_LEVEL}
EOF
}

start_stack() {
  mkdir -p "$INSTALL_DIR/logs"
  docker compose -f "$INSTALL_DIR/docker-compose.cloud.yml" --env-file "$INSTALL_DIR/.env" up -d --build
}

show_result() {
  local host_ip
  host_ip="$(curl -fsSL https://api.ipify.org 2>/dev/null || hostname -I | awk '{print $1}')"
  log "Deployment completed"
  log "Install dir: $INSTALL_DIR"
  log "App URL: http://${host_ip}:${APP_PORT}"
  log "PostgreSQL database: ${POSTGRES_DB}"
  log "PostgreSQL user: ${POSTGRES_USER}"
  log "PostgreSQL password: ${POSTGRES_PASSWORD}"
  log "To view logs: docker compose -f ${INSTALL_DIR}/docker-compose.cloud.yml --env-file ${INSTALL_DIR}/.env logs -f"
}

main() {
  if [[ "${EUID}" -ne 0 ]]; then
    echo "Please run this script as root or with sudo." >&2
    exit 1
  fi

  install_base_packages
  install_docker_if_missing
  ensure_compose
  prepare_repo
  write_env_file
  start_stack
  show_result
}

main "$@"
