#!/usr/bin/env bash
set -Eeuo pipefail

readonly DEPLOY_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
readonly STATE_DIR="${DEPLOY_DIR}/.deploy"
readonly BACKUP_DIR="${DEPLOY_DIR}/backups"
readonly CURRENT_IMAGE_FILE="${STATE_DIR}/current-image"
readonly PREVIOUS_IMAGE_FILE="${STATE_DIR}/previous-image"
readonly LOCK_FILE="${STATE_DIR}/update.lock"

cd "${DEPLOY_DIR}"
mkdir -p "${STATE_DIR}" "${BACKUP_DIR}"
chmod 700 "${STATE_DIR}" "${BACKUP_DIR}"
exec 9>"${LOCK_FILE}"
flock -n 9 || { echo "已有更新任务正在运行" >&2; exit 1; }

[[ -f .env ]] || { echo "缺少 ${DEPLOY_DIR}/.env" >&2; exit 1; }
set -a
# shellcheck disable=SC1091
source .env
set +a

UPDATE_VERSION="${XIANYU_UPDATE_VERSION:-}"
SOURCE_IMAGE="${XIANYU_IMAGE_SOURCE:-ghcr.io/haojundeveloper/ydisks-xianyu-helper:main}"
if [[ -n "${UPDATE_VERSION}" ]]; then
  SOURCE_IMAGE="${XIANYU_IMAGE_REPOSITORY:-ghcr.io/haojundeveloper/ydisks-xianyu-helper}:${UPDATE_VERSION#v}"
fi
readonly SOURCE_IMAGE UPDATE_VERSION
readonly HEALTH_URL="http://127.0.0.1:${XIANYU_HTTP_PORT:-59188}/health"
readonly TIMESTAMP="$(date -u +%Y%m%dT%H%M%SZ)"
BACKUP_FILE=""
OLD_IMAGE=""
NEW_IMAGE=""

compose() {
  docker compose --env-file .env "$@"
}

resolve_image() {
  docker pull "${SOURCE_IMAGE}" >/dev/null
  docker image inspect "${SOURCE_IMAGE}" --format '{{index .RepoDigests 0}}'
}

wait_for_health() {
  local attempt
  for attempt in $(seq 1 30); do
    if curl --fail --silent --show-error --max-time 5 "${HEALTH_URL}" >/dev/null; then
      return 0
    fi
    sleep 5
  done
  return 1
}

backup_database() {
  if ! compose ps --status running --services | grep -qx postgres; then
    return 0
  fi

  BACKUP_FILE="${BACKUP_DIR}/postgres-${TIMESTAMP}.dump"
  compose exec -T postgres pg_dump \
    --username "${POSTGRES_USER}" \
    --dbname "${POSTGRES_DB}" \
    --format=custom \
    --clean \
    --if-exists >"${BACKUP_FILE}"
  chmod 600 "${BACKUP_FILE}"
}

restore_database() {
  [[ -n "${BACKUP_FILE}" && -s "${BACKUP_FILE}" ]] || return 0
  compose stop app >/dev/null 2>&1 || true
  compose up -d postgres
  compose exec -T postgres pg_restore \
    --username "${POSTGRES_USER}" \
    --dbname "${POSTGRES_DB}" \
    --clean \
    --if-exists \
    --no-owner <"${BACKUP_FILE}"
}

rollback() {
  local exit_code=$?
  trap - ERR
  echo "更新失败，正在回滚到 ${OLD_IMAGE}" >&2
  if [[ -n "${OLD_IMAGE}" ]]; then
    printf '%s\n' "${OLD_IMAGE}" >"${CURRENT_IMAGE_FILE}"
    XIANYU_IMAGE="${OLD_IMAGE}" compose up -d postgres
    restore_database
    XIANYU_IMAGE="${OLD_IMAGE}" compose up -d
    wait_for_health || echo "回滚完成，但健康检查仍失败，请查看 docker compose logs" >&2
  else
    rm -f "${CURRENT_IMAGE_FILE}"
    compose stop app >/dev/null 2>&1 || true
  fi
  exit "${exit_code}"
}

trap rollback ERR

OLD_IMAGE="$(cat "${CURRENT_IMAGE_FILE}" 2>/dev/null || true)"

backup_database
NEW_IMAGE="$(resolve_image)"

if [[ "${NEW_IMAGE}" == "${OLD_IMAGE}" ]] && wait_for_health; then
  echo "当前已是最新版本：${NEW_IMAGE}"
  exit 0
fi

[[ -n "${OLD_IMAGE}" ]] && printf '%s\n' "${OLD_IMAGE}" >"${PREVIOUS_IMAGE_FILE}"
printf '%s\n' "${NEW_IMAGE}" >"${CURRENT_IMAGE_FILE}"
XIANYU_IMAGE="${NEW_IMAGE}" compose up -d
wait_for_health
trap - ERR

find "${BACKUP_DIR}" -maxdepth 1 -type f -name 'postgres-*.dump' -mtime +14 -delete
echo "更新成功：${NEW_IMAGE}"