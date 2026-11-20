package config

import (
	"strings"
	"testing"
)

func TestLoadWithOptionsSetupMode(t *testing.T) {
	for _, k := range []string{
		"LLM_BASE_URL", "LLM_API_KEY", "LLM_CHAT_MODEL", "LLM_EMBED_MODEL",
		"EMBED_BASE_URL", "EMBED_API_KEY", "MYSQL_DSN", "ALLOW_WEB_SETUP",
	} {
		t.Setenv(k, "")
	}

	// 即使开启 setup 模式，缺少 MYSQL_DSN 也必须直接报错
	if _, _, err := LoadWithOptions(true); err == nil || !strings.Contains(err.Error(), "MYSQL_DSN") {
		t.Fatalf("expected MYSQL_DSN error in setup mode, got %v", err)
	}

	// 提供 MYSQL_DSN 后，允许缺少模型变量进入 Web 引导态
	t.Setenv("MYSQL_DSN", "root:pwd@tcp(127.0.0.1:3306)/knowledge_qa_v3?charset=utf8mb4&parseTime=true")
	cfg, missing, err := LoadWithOptions(true)
	if err != nil {
		t.Fatalf("unexpected error in setup mode: %v", err)
	}
	if len(missing) != 4 {
		t.Fatalf("expected 4 missing model vars, got %v", missing)
	}
	if cfg.ConvertEngine != "native" || cfg.HTTPAddr != ":8080" {
		t.Fatalf("unexpected defaults: %+v", cfg)
	}

	// 补齐 4 项模型变量后 missing 应为空
	t.Setenv("LLM_BASE_URL", "https://api.example.com/v1")
	t.Setenv("LLM_API_KEY", "sk-placeholder")
	t.Setenv("LLM_CHAT_MODEL", "chat-model")
	t.Setenv("LLM_EMBED_MODEL", "embed-model")
	cfg2, missing2, err := LoadWithOptions(true)
	if err != nil || len(missing2) != 0 {
		t.Fatalf("expected configured setup mode, missing=%v err=%v", missing2, err)
	}
	if cfg2.Embed.BaseURL != "https://api.example.com/v1" || cfg2.Embed.APIKey != "sk-placeholder" {
		t.Fatalf("embed fallback mismatch: %+v", cfg2.Embed)
	}

	// IsTruthyEnv 检查
	t.Setenv("ALLOW_WEB_SETUP", "true")
	if !IsTruthyEnv("ALLOW_WEB_SETUP") {
		t.Fatal("expected IsTruthyEnv true")
	}
	t.Setenv("ALLOW_WEB_SETUP", "false")
	if IsTruthyEnv("ALLOW_WEB_SETUP") {
		t.Fatal("expected IsTruthyEnv false")
	}
}
