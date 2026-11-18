-- V2 增量迁移脚本（严格遵循《00_V2总体规格.md》§4）
-- 前置条件：目标库（默认 knowledge_qa_v2，严禁在原 knowledge_qa 库执行）已创建 001_init.sql 的基础 4 张表。
-- 本脚本完全幂等，可重复执行；不修改 001_init.sql 已有列的类型与语义。

SET NAMES utf8mb4;

-- 1. 项目空间表（含默认 shared 公共域与默认 go-gin 项目）
CREATE TABLE IF NOT EXISTS projects (
  id BIGINT AUTO_INCREMENT PRIMARY KEY,
  slug VARCHAR(64) NOT NULL UNIQUE,
  name VARCHAR(120) NOT NULL,
  description VARCHAR(500) NOT NULL DEFAULT '',
  created_at DATETIME DEFAULT CURRENT_TIMESTAMP
);

INSERT IGNORE INTO projects (id, slug, name, description) VALUES
  (1, 'shared', '公共规范库', '跨项目共享的团队通用规范与基础文档'),
  (2, 'go-gin', 'Go/Gin 技术文档', 'V1 原始 Go 与 Gin 官方文档语料');

-- 2. 幂等扩展 documents 与 eval_questions 字段及外键（兼容 MySQL 8 无 ADD COLUMN IF NOT EXISTS）
DROP PROCEDURE IF EXISTS v2_apply_schema_alter;
DELIMITER $$
CREATE PROCEDURE v2_apply_schema_alter()
BEGIN
  -- documents.project_id
  IF NOT EXISTS (
    SELECT 1 FROM INFORMATION_SCHEMA.COLUMNS
    WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'documents' AND COLUMN_NAME = 'project_id'
  ) THEN
    ALTER TABLE documents ADD COLUMN project_id BIGINT NOT NULL DEFAULT 2;
  END IF;

  -- documents.source_type
  IF NOT EXISTS (
    SELECT 1 FROM INFORMATION_SCHEMA.COLUMNS
    WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'documents' AND COLUMN_NAME = 'source_type'
  ) THEN
    ALTER TABLE documents ADD COLUMN source_type VARCHAR(32) NOT NULL DEFAULT 'md';
  END IF;

  -- documents.content_hash
  IF NOT EXISTS (
    SELECT 1 FROM INFORMATION_SCHEMA.COLUMNS
    WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'documents' AND COLUMN_NAME = 'content_hash'
  ) THEN
    ALTER TABLE documents ADD COLUMN content_hash CHAR(64) NOT NULL DEFAULT '';
  END IF;

  -- documents.doc_level
  IF NOT EXISTS (
    SELECT 1 FROM INFORMATION_SCHEMA.COLUMNS
    WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'documents' AND COLUMN_NAME = 'doc_level'
  ) THEN
    ALTER TABLE documents ADD COLUMN doc_level ENUM('spec', 'rfc', 'note') NOT NULL DEFAULT 'spec';
  END IF;

  -- documents.status
  IF NOT EXISTS (
    SELECT 1 FROM INFORMATION_SCHEMA.COLUMNS
    WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'documents' AND COLUMN_NAME = 'status'
  ) THEN
    ALTER TABLE documents ADD COLUMN status ENUM('active', 'deprecated') NOT NULL DEFAULT 'active';
  END IF;

  -- documents.replaced_by_doc_id
  IF NOT EXISTS (
    SELECT 1 FROM INFORMATION_SCHEMA.COLUMNS
    WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'documents' AND COLUMN_NAME = 'replaced_by_doc_id'
  ) THEN
    ALTER TABLE documents ADD COLUMN replaced_by_doc_id BIGINT NULL;
  END IF;

  -- documents.owner
  IF NOT EXISTS (
    SELECT 1 FROM INFORMATION_SCHEMA.COLUMNS
    WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'documents' AND COLUMN_NAME = 'owner'
  ) THEN
    ALTER TABLE documents ADD COLUMN owner VARCHAR(100) NOT NULL DEFAULT '';
  END IF;

  -- documents.updated_at
  IF NOT EXISTS (
    SELECT 1 FROM INFORMATION_SCHEMA.COLUMNS
    WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'documents' AND COLUMN_NAME = 'updated_at'
  ) THEN
    ALTER TABLE documents ADD COLUMN updated_at DATETIME DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP;
  END IF;

  -- 外键 fk_documents_project
  IF NOT EXISTS (
    SELECT 1 FROM INFORMATION_SCHEMA.TABLE_CONSTRAINTS
    WHERE CONSTRAINT_SCHEMA = DATABASE() AND TABLE_NAME = 'documents' AND CONSTRAINT_NAME = 'fk_documents_project'
  ) THEN
    ALTER TABLE documents
      ADD CONSTRAINT fk_documents_project FOREIGN KEY (project_id) REFERENCES projects(id);
  END IF;

  -- eval_questions.project_slug
  IF NOT EXISTS (
    SELECT 1 FROM INFORMATION_SCHEMA.COLUMNS
    WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'eval_questions' AND COLUMN_NAME = 'project_slug'
  ) THEN
    ALTER TABLE eval_questions ADD COLUMN project_slug VARCHAR(64) NOT NULL DEFAULT '';
  END IF;

  -- eval_questions.question_set
  IF NOT EXISTS (
    SELECT 1 FROM INFORMATION_SCHEMA.COLUMNS
    WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'eval_questions' AND COLUMN_NAME = 'question_set'
  ) THEN
    ALTER TABLE eval_questions ADD COLUMN question_set VARCHAR(32) NOT NULL DEFAULT 'base52';
  END IF;

  -- eval_questions.source_doc_id
  IF NOT EXISTS (
    SELECT 1 FROM INFORMATION_SCHEMA.COLUMNS
    WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'eval_questions' AND COLUMN_NAME = 'source_doc_id'
  ) THEN
    ALTER TABLE eval_questions ADD COLUMN source_doc_id BIGINT NULL;
  END IF;

  -- eval_questions.category
  IF NOT EXISTS (
    SELECT 1 FROM INFORMATION_SCHEMA.COLUMNS
    WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'eval_questions' AND COLUMN_NAME = 'category'
  ) THEN
    ALTER TABLE eval_questions ADD COLUMN category VARCHAR(64) NOT NULL DEFAULT '';
  END IF;

  -- eval_runs.batch_id（阶段 11：精确隔离 base52 / holdout / feedback / all 评测批次）
  IF NOT EXISTS (
    SELECT 1 FROM INFORMATION_SCHEMA.COLUMNS
    WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'eval_runs' AND COLUMN_NAME = 'batch_id'
  ) THEN
    ALTER TABLE eval_runs ADD COLUMN batch_id VARCHAR(64) NOT NULL DEFAULT '';
  END IF;
END$$
DELIMITER ;

CALL v2_apply_schema_alter();
DROP PROCEDURE IF EXISTS v2_apply_schema_alter;

-- 3. 团队知识盲区雷达（仅记录真实问答拒答，评测跑批不写入）
CREATE TABLE IF NOT EXISTS knowledge_gaps (
  id BIGINT AUTO_INCREMENT PRIMARY KEY,
  project_slug VARCHAR(64) NOT NULL DEFAULT 'auto',
  question TEXT NOT NULL,
  normalized_key VARCHAR(255) NOT NULL,
  refuse_reason VARCHAR(64) NOT NULL,
  top1_final DOUBLE NOT NULL DEFAULT 0,
  nearest_chunk_id BIGINT NULL,
  nearest_heading VARCHAR(500) NOT NULL DEFAULT '',
  hit_count INT NOT NULL DEFAULT 1,
  status ENUM('open', 'resolved', 'ignored') NOT NULL DEFAULT 'open',
  last_asked_at DATETIME DEFAULT CURRENT_TIMESTAMP,
  created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
  UNIQUE KEY uk_gap_proj_key (project_slug, normalized_key)
);

-- 4. 用户回答反馈与活评测集待审队列
CREATE TABLE IF NOT EXISTS eval_feedback (
  id BIGINT AUTO_INCREMENT PRIMARY KEY,
  project_slug VARCHAR(64) NOT NULL DEFAULT 'auto',
  question TEXT NOT NULL,
  retrieved_chunk_ids VARCHAR(500) NOT NULL DEFAULT '[]',
  answer TEXT NOT NULL,
  refused TINYINT(1) NOT NULL DEFAULT 0,
  vote ENUM('up', 'down') NOT NULL,
  comment VARCHAR(500) NOT NULL DEFAULT '',
  promoted_question_id BIGINT NULL,
  created_at DATETIME DEFAULT CURRENT_TIMESTAMP
);
