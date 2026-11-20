#!/bin/sh
set -e

TARGET_DB="${MYSQL_DATABASE:-knowledge_qa_v3}"
echo "[docker-init] 正在初始化 V3 数据库: ${TARGET_DB} ..."

run_sql() {
  if command -v docker_process_sql >/dev/null 2>&1; then
    docker_process_sql --database="${TARGET_DB}"
  else
    MYSQL_PWD="${MYSQL_ROOT_PASSWORD}" mysql -u root --default-character-set=utf8mb4 "${TARGET_DB}"
  fi
}

# migrations/001_init.sql 保持 V1 原样，容器首次初始化时自动导入到 V3 目标库（同时剥离 Windows CRLF 回车符）
tr -d '\r' < /migrations/001_init.sql | grep -viE '^(CREATE DATABASE|USE )' | run_sql
tr -d '\r' < /migrations/002_v2_schema.sql | run_sql
echo "[docker-init] 数据库 ${TARGET_DB} 初始化完成。"
