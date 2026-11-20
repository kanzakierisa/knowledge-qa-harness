#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT_DIR"

RUN_SEED=false
ALLOW_SETUP=false

for arg in "$@"; do
  case "$arg" in
    --seed) RUN_SEED=true ;;
    --setup-ui) ALLOW_SETUP=true ;;
    *)
      echo "未知参数: $arg"
      echo "用法: ./scripts/quickstart.sh [--seed] [--setup-ui]"
      exit 1
      ;;
  esac
done

if ! command -v go >/dev/null 2>&1; then
  echo "未检测到 Go 运行环境，请先安装 Go 1.26+。"
  exit 1
fi

if [ -f ".env" ]; then
  echo "[quickstart] 正在加载当前目录 .env 环境变量..."
  set -a
  # shellcheck disable=SC1091
  . ./.env
  set +a
elif [ -z "${MYSQL_DSN:-}" ]; then
  echo "[quickstart] 未检测到 .env 且 MYSQL_DSN 未设置。"
  echo "请先执行: cp .env.example .env 并填写 MYSQL_DSN（模型 Key 可选，缺失时可用 --setup-ui 在网页填）。"
  exit 1
fi

if [ -z "${MYSQL_DSN:-}" ]; then
  echo "[quickstart] 错误：MYSQL_DSN 不能为空。"
  exit 1
fi

HAS_MODEL_VARS=true
for var in LLM_BASE_URL LLM_API_KEY LLM_CHAT_MODEL LLM_EMBED_MODEL; do
  eval val="\${$var:-}"
  if [ -z "$val" ]; then
    HAS_MODEL_VARS=false
  fi
done

if [ "$RUN_SEED" = true ]; then
  if [ "$HAS_MODEL_VARS" != true ]; then
    echo "[quickstart] 错误：执行 --seed 需要先配置 LLM_BASE_URL / LLM_API_KEY / LLM_CHAT_MODEL / LLM_EMBED_MODEL。"
    exit 1
  fi
  echo "[quickstart] 正在幂等导入多项目多格式演示语料与示例数据..."
  go run ./scripts/seed_demo_v2.go
fi

if [ "$HAS_MODEL_VARS" != true ] || [ "$ALLOW_SETUP" = true ] || [ "${ALLOW_WEB_SETUP:-}" = "true" ]; then
  echo "[quickstart] 启动服务（Web 引导配置态 --setup-ui，密钥仅存内存，监听 ${HTTP_ADDR:-:8080}）..."
  exec go run . --setup-ui
fi

echo "[quickstart] 启动服务（监听 ${HTTP_ADDR:-:8080}）..."
exec go run .
