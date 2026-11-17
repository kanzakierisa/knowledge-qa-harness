// 纯向量 Top1 对照工具（阶段 3 验收用）：对给定问题只走向量检索，
// 打印 Top 5 的 (chunk_id, heading, 余弦分)，与 /api/ask?debug 的混合结果对比。
// 运行方式：在项目根目录执行 go run ./scripts/veccmp "问题文本"
package main

import (
	"fmt"
	"log"
	"os"

	"knowledge-qa/config"
	"knowledge-qa/ingest"
	"knowledge-qa/retrieve"
	"knowledge-qa/store"
)

func main() {
	if len(os.Args) < 2 {
		log.Fatal("用法：go run ./scripts/veccmp \"问题文本\"")
	}
	question := os.Args[1]

	cfg, err := config.Load()
	if err != nil {
		log.Fatal("配置错误：", err)
	}
	st, err := store.New(cfg.MySQLDSN)
	if err != nil {
		log.Fatal(err)
	}
	defer st.Close()

	emb := ingest.NewEmbedder(cfg.Embed.BaseURL, cfg.Embed.APIKey, cfg.Embed.Model)
	qVec, err := emb.Embed(question)
	if err != nil {
		log.Fatal("embedding 失败：", err)
	}
	chunks, err := st.AllChunks()
	if err != nil {
		log.Fatal(err)
	}

	fmt.Printf("纯向量 Top5 | 问题：%s\n", question)
	for i, sc := range retrieve.TopK(qVec, chunks, 5) {
		fmt.Printf("  [%d] chunk_id=%d  cos=%.4f  %s\n", i+1, sc.Chunk.ID, sc.Score, sc.Chunk.HeadingPath)
	}
}
