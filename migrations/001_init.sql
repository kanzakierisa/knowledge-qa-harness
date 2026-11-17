CREATE DATABASE IF NOT EXISTS knowledge_qa DEFAULT CHARSET utf8mb4;

USE knowledge_qa;

-- 原始文档
CREATE TABLE IF NOT EXISTS documents (
  id BIGINT AUTO_INCREMENT PRIMARY KEY,
  title VARCHAR(255) NOT NULL,
  filename VARCHAR(255) NOT NULL,
  content MEDIUMTEXT NOT NULL,
  created_at DATETIME DEFAULT CURRENT_TIMESTAMP
);

-- 切片后的段落
CREATE TABLE IF NOT EXISTS chunks (
  id BIGINT AUTO_INCREMENT PRIMARY KEY,
  document_id BIGINT NOT NULL,
  heading_path VARCHAR(500) NOT NULL DEFAULT '',   -- 如 "Gin 文档 > 路由 > 参数"
  content TEXT NOT NULL,
  embedding MEDIUMTEXT,                            -- JSON 数组字符串，如 "[0.012,-0.034,...]"
  created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
  FOREIGN KEY (document_id) REFERENCES documents(id)
);

-- 金标评测题
CREATE TABLE IF NOT EXISTS eval_questions (
  id BIGINT AUTO_INCREMENT PRIMARY KEY,
  question TEXT NOT NULL,
  gold_answer TEXT NOT NULL,        -- 人工写的参考答案
  should_refuse TINYINT(1) NOT NULL DEFAULT 0,  -- 1 = 文档里没答案，正确行为是拒答
  gold_chunk_id BIGINT,             -- 答案所在的标准段落（should_refuse=1 时为 NULL）
  created_at DATETIME DEFAULT CURRENT_TIMESTAMP
);

-- 每次评测运行记录
CREATE TABLE IF NOT EXISTS eval_runs (
  id BIGINT AUTO_INCREMENT PRIMARY KEY,
  question_id BIGINT NOT NULL,
  retrieved_chunk_ids VARCHAR(500),   -- JSON 数组，检索出的段落 id
  answer TEXT,                        -- 模型生成的答案
  refused TINYINT(1) DEFAULT 0,       -- 系统是否拒答了
  hit TINYINT(1) DEFAULT NULL,        -- 检索是否命中 gold_chunk
  citation_ok TINYINT(1) DEFAULT NULL,-- 引用编号是否真实存在于检索结果
  usable TINYINT(1) DEFAULT NULL,     -- 人工标注：答案是否可用
  created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
  FOREIGN KEY (question_id) REFERENCES eval_questions(id)
);
