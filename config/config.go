// Package config 集中管理配置：从环境变量读取 5 个必填项并校验。
// 遵循总体规格 §3：不存在任何硬编码的 key / 地址 / 模型名，缺任何一个变量直接报错退出。
package config

import (
	"fmt"
	"os"
	"strings"
)

// Endpoint 描述一类调用（对话或 embedding）的完整接入信息。
type Endpoint struct {
	BaseURL string
	APIKey  string
	Model   string
}

// Config 是全局配置。
type Config struct {
	Chat     Endpoint // 对话模型接入
	Embed    Endpoint // embedding 模型接入（可与对话不同服务商）
	MySQLDSN string
	HTTPAddr string // 可选监听地址，便于与本机其他服务共存。
}

// requiredVars 5 个必需环境变量（总体规格 §3）。
var requiredVars = []string{
	"LLM_BASE_URL",
	"LLM_API_KEY",
	"LLM_CHAT_MODEL",
	"LLM_EMBED_MODEL",
	"MYSQL_DSN",
}

// Load 读取环境变量并生成配置。
// 必需：LLM_BASE_URL、LLM_API_KEY、LLM_CHAT_MODEL、LLM_EMBED_MODEL、MYSQL_DSN，
// 缺任何一个都报错并列出缺哪些。
// 可选扩展：EMBED_BASE_URL、EMBED_API_KEY（embedding 走另一家平台时指定，
// 缺省回落到 LLM_BASE_URL / LLM_API_KEY）。
func Load() (*Config, error) {
	missing := make([]string, 0, len(requiredVars))
	for _, k := range requiredVars {
		if strings.TrimSpace(os.Getenv(k)) == "" {
			missing = append(missing, k)
		}
	}
	if len(missing) > 0 {
		return nil, fmt.Errorf("缺少必需的环境变量：%s", strings.Join(missing, ", "))
	}

	c := &Config{MySQLDSN: os.Getenv("MYSQL_DSN"), HTTPAddr: getenv("HTTP_ADDR", ":8080")}
	c.Chat = Endpoint{
		BaseURL: os.Getenv("LLM_BASE_URL"),
		APIKey:  os.Getenv("LLM_API_KEY"),
		Model:   os.Getenv("LLM_CHAT_MODEL"),
	}
	// embedding 端点：默认跟随对话端点，可用 EMBED_* 单独指定另一家服务商
	c.Embed = Endpoint{
		BaseURL: getenv("EMBED_BASE_URL", c.Chat.BaseURL),
		APIKey:  getenv("EMBED_API_KEY", c.Chat.APIKey),
		Model:   os.Getenv("LLM_EMBED_MODEL"),
	}
	return c, nil
}

func getenv(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
