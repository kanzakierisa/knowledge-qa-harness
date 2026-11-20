// 入口：装配依赖，起 HTTP 服务。
package main

import (
	"flag"
	"log"

	"knowledge-qa/api"
	"knowledge-qa/config"
	"knowledge-qa/store"
)

func main() {
	setupUI := flag.Bool("setup-ui", false, "允许在缺少模型环境变量时启动服务并在浏览器完成内存运行态配置（不落盘）")
	flag.Parse()

	allowSetup := *setupUI || config.IsTruthyEnv("ALLOW_WEB_SETUP")
	cfg, missingModels, err := config.LoadWithOptions(allowSetup)
	if err != nil {
		log.Fatal("配置错误：", err)
	}

	if len(missingModels) > 0 {
		log.Printf("[setup] 未检测到完整模型环境变量（缺少：%v），已进入 Web 引导配置态（仅存内存，不落盘）", missingModels)
	} else {
		log.Printf("对话模型：%s @ %s | embedding：%s @ %s",
			cfg.Chat.Model, cfg.Chat.BaseURL, cfg.Embed.Model, cfg.Embed.BaseURL)
		if cfg.Rerank.BaseURL != "" {
			log.Printf("重排模型：%s @ %s", cfg.Rerank.Model, cfg.Rerank.BaseURL)
		}
	}

	st, err := store.New(cfg.MySQLDSN)
	if err != nil {
		log.Fatal(err)
	}
	defer st.Close()

	h := &api.Handler{Store: st}
	h.InitSetupFromConfig(cfg, allowSetup, missingModels)

	r := api.NewRouter(h)
	log.Printf("knowledge-qa 服务启动，监听：%s", cfg.HTTPAddr)
	if err := r.Run(cfg.HTTPAddr); err != nil {
		log.Fatal(err)
	}
}
