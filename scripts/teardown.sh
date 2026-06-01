#!/usr/bin/env bash
# 收掉 schedule-lite compose。
# 用法(從任何位置都可):
#   bash scripts/teardown.sh       # 停容器,保留 DB 資料
#   bash scripts/teardown.sh -v    # 連 DB volume 一起刪(資料清空)
set -euo pipefail

# 解析專案根(絕對路徑,抗 symlink / 任意 CWD)
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(cd "${SCRIPT_DIR}/.." && pwd)"
cd "${PROJECT_ROOT}"

command -v docker >/dev/null 2>&1 || { echo "✗ 需要 'docker'" >&2; exit 1; }

# docker compose(v2)優先,退回 docker-compose(v1)
if docker compose version >/dev/null 2>&1; then
  COMPOSE=(docker compose)
elif command -v docker-compose >/dev/null 2>&1; then
  COMPOSE=(docker-compose)
else
  echo "✗ 找不到 'docker compose' 或 'docker-compose'" >&2
  exit 1
fi

if [ "${1:-}" = "-v" ]; then
  echo "→ ${COMPOSE[*]} down -v(連資料一起刪)"
  "${COMPOSE[@]}" down -v
else
  echo "→ ${COMPOSE[*]} down(保留 DB 資料;要清資料加 -v)"
  "${COMPOSE[@]}" down
fi
