// 入口：装配依赖，起 HTTP 服务。
package main

import (
	"log"

	"knowledge-qa/api"
	"knowledge-qa/config"
	"knowledge-qa/generate"
	"knowledge-qa/ingest"
	"knowledge-qa/store"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		log.Fatal("配置错误：", err)
	}
	log.Printf("对话模型：%s @ %s | embedding：%s @ %s",
		cfg.Chat.Model, cfg.Chat.BaseURL, cfg.Embed.Model, cfg.Embed.BaseURL)

	st, err := store.New(cfg.MySQLDSN)
	if err != nil {
		log.Fatal(err)
	}
	defer st.Close()

	var vlm ingest.VLMTranscriber
	if cfg.VLM.Model != "" {
		vlm = ingest.NewVLMClient(cfg.VLM.BaseURL, cfg.VLM.APIKey, cfg.VLM.Model)
	}
	h := &api.Handler{
		Store:     st,
		Embedder:  ingest.NewEmbedder(cfg.Embed.BaseURL, cfg.Embed.APIKey, cfg.Embed.Model),
		Chat:      generate.NewChatClient(cfg.Chat.BaseURL, cfg.Chat.APIKey, cfg.Chat.Model),
		Converter: ingest.NewConverter(ingest.ConvertOptions{Engine: cfg.ConvertEngine, VLM: vlm}),
	}

	r := api.NewRouter(h)
	log.Printf("knowledge-qa 服务启动，监听：%s", cfg.HTTPAddr)
	if err := r.Run(cfg.HTTPAddr); err != nil {
		log.Fatal(err)
	}
}
