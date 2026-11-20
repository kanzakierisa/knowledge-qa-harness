#!/bin/sh
set -e

is_true() {
  case "$(printf '%s' "$1" | tr '[:upper:]' '[:lower:]')" in
    1|true|yes|on) return 0 ;;
    *) return 1 ;;
  esac
}

if is_true "${AUTO_SEED_DEMO:-false}"; then
  if [ -n "${LLM_BASE_URL:-}" ] && [ -n "${LLM_API_KEY:-}" ] && [ -n "${LLM_CHAT_MODEL:-}" ] && [ -n "${LLM_EMBED_MODEL:-}" ]; then
    echo "[entrypoint] AUTO_SEED_DEMO=true：正在幂等导入多项目多格式演示语料..."
    /app/seed_demo_v2 || echo "[entrypoint] 警告：自动演示灌库未完成（将继续启动 Web 服务），请核对模型凭据或网络连通性。"
  else
    echo "[entrypoint] AUTO_SEED_DEMO=true，但尚未提供完整模型参数（LLM_BASE_URL/LLM_API_KEY/LLM_CHAT_MODEL/LLM_EMBED_MODEL），跳过自动向量化灌库。"
    echo "[entrypoint] 服务将以引导配置模式启动；请在网页端填写模型配置后上传文档，或稍后执行 /app/seed_demo_v2。"
  fi
fi

if is_true "${ALLOW_WEB_SETUP:-false}"; then
  exec /app/knowledge-qa --setup-ui "$@"
fi

exec /app/knowledge-qa "$@"
