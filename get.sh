#!/usr/bin/env bash
# schedule-lite 一鍵安裝:抓 repo → (必要時生 go.sum) → docker compose 起服務。
#
# 乾淨的 Linux 只要裝好 Docker,貼這行即可:
#   curl -fsSL https://raw.githubusercontent.com/Graylee0128/schedule-lite/main/get.sh | bash
#
# 想先看內容再跑(較安全,建議):
#   curl -fsSL https://raw.githubusercontent.com/Graylee0128/schedule-lite/main/get.sh -o get.sh
#   less get.sh && bash get.sh
set -euo pipefail

REPO="Graylee0128/schedule-lite"
BRANCH="main"
WORKDIR="${WORKDIR:-$HOME/schedule-lite}"
BASE_URL="http://localhost:8080"

say() { printf '\033[1;36m→ %s\033[0m\n' "$*"; }
ok()  { printf '\033[1;32m✓ %s\033[0m\n' "$*"; }
die() { printf '\033[1;31m✗ %s\033[0m\n' "$*" >&2; exit 1; }

# --- 1. 前置檢查(乾淨環境最少需求:curl / tar / docker)---
command -v curl   >/dev/null 2>&1 || die "需要 curl"
command -v tar    >/dev/null 2>&1 || die "需要 tar"
command -v docker >/dev/null 2>&1 || die "需要 Docker(安裝:https://docs.docker.com/engine/install/)"
docker compose version >/dev/null 2>&1 || die "需要 docker compose v2(較新的 Docker 內建)"
docker info >/dev/null 2>&1 || die "Docker daemon 沒在跑,或當前帳號無權限(試 sudo,或把帳號加入 docker 群組)"

# --- 2. 抓 repo(用 tarball,免裝 git)---
say "下載 ${REPO}@${BRANCH} → ${WORKDIR}"
rm -rf "${WORKDIR}"
mkdir -p "${WORKDIR}"
curl -fsSL "https://github.com/${REPO}/archive/refs/heads/${BRANCH}.tar.gz" \
  | tar -xz -C "${WORKDIR}" --strip-components=1
cd "${WORKDIR}"

# --- 3. 確保 go.sum(乾淨環境可能沒裝 Go → 用丟棄式 golang 容器產生)---
if [ ! -f go.sum ]; then
  if command -v go >/dev/null 2>&1; then
    say "產生 go.sum(go mod tidy)"
    go mod tidy
  else
    say "未裝 Go,改用 golang:1.22 容器產生 go.sum"
    docker run --rm -v "$PWD":/src -w /src golang:1.22-alpine go mod tidy
  fi
fi

# --- 4. build 並背景啟動 app + postgres ---
say "docker compose up --build -d(首次會 build,稍等)"
docker compose up --build -d

# --- 5. 輪詢 /healthz 等服務起來(最多 60 秒)---
say "等待 ${BASE_URL}/healthz ..."
for i in $(seq 1 60); do
  if curl -fsS "${BASE_URL}/healthz" >/dev/null 2>&1; then
    ok "起好了!"
    echo "   管理台:    ${BASE_URL}/"
    echo "   員工填班頁:${BASE_URL}/a/{token}(管理台發連結後取得)"
    echo "   看資料庫:  docker compose --profile tools up -d adminer  → http://localhost:8081"
    echo "   收掉:      cd ${WORKDIR} && docker compose down        (加 -v 連資料清)"
    exit 0
  fi
  sleep 1
done
die "60 秒內未就緒。app log:$(cd "${WORKDIR}" && docker compose logs app 2>&1 | tail -20)"
