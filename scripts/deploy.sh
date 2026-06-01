#!/usr/bin/env bash
# schedule-lite 部署腳本:build → 起 compose(app + postgres)→ 驗證探針。
#
# 不論從哪裡執行都安全:
#   bash scripts/deploy.sh          # 從專案根
#   cd scripts && bash deploy.sh    # 從 scripts/ 裡
#   /abs/path/scripts/deploy.sh     # 絕對路徑
# 可用 BASE_URL 覆寫驗證位址:
#   BASE_URL=http://<tailscale-ip>:8080 bash scripts/deploy.sh
set -euo pipefail

# --- 解析專案根(用 BASH_SOURCE + pwd 取絕對路徑,可抗 symlink / 任意 CWD)---
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(cd "${SCRIPT_DIR}/.." && pwd)"
cd "${PROJECT_ROOT}"

BASE_URL="${BASE_URL:-http://localhost:8080}"

# --- 前置檢查:需要的工具在不在 ---
need() { command -v "$1" >/dev/null 2>&1 || { echo "✗ 需要 '$1',但找不到" >&2; exit 1; }; }
need docker
need curl

# docker compose(v2,空格)優先;退回 docker-compose(v1)
if docker compose version >/dev/null 2>&1; then
  COMPOSE=(docker compose)
elif command -v docker-compose >/dev/null 2>&1; then
  COMPOSE=(docker-compose)
else
  echo "✗ 找不到 'docker compose' 或 'docker-compose'" >&2
  exit 1
fi

# --- 1. 首次(或改過 deps):確保 go.sum 存在 ---
if [ ! -f go.sum ]; then
  echo "→ 找不到 go.sum,需要 go mod tidy"
  need go
  go mod tidy
fi

# --- 2. build 並背景啟動 app + postgres ---
echo "→ ${COMPOSE[*]} up --build -d"
"${COMPOSE[@]}" up --build -d

# --- 3. 輪詢 /healthz,等服務起來(最多 30 秒)---
echo "→ 等待 ${BASE_URL}/healthz ..."
for i in $(seq 1 30); do
  if curl -fsS "${BASE_URL}/healthz" >/dev/null 2>&1; then
    break
  fi
  if [ "${i}" -eq 30 ]; then
    echo "✗ /healthz 30 秒內未就緒,印出 app log:" >&2
    "${COMPOSE[@]}" logs app >&2 || true
    exit 1
  fi
  sleep 1
done

# --- 4. 顯示兩個探針結果(readyz 不加 -f,讓 503 也印得出來)---
echo "→ /healthz: $(curl -sS "${BASE_URL}/healthz")"
echo "→ /readyz : $(curl -sS "${BASE_URL}/readyz")"
echo "✓ 部署完成。收掉:bash scripts/teardown.sh"
