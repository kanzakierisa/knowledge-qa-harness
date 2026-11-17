// 语料导入命令行：扫描 corpus/ 下所有 .md → 存 documents → 切片存 chunks → 逐条算 embedding 入库。
// 运行方式：在项目根目录执行 go run ./scripts          （追加导入）
//
//	go run ./scripts -clean    （先清空 documents/chunks 再重灌，切片器或语料变更后用这个）
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"

	"knowledge-qa/config"
	"knowledge-qa/ingest"
	"knowledge-qa/store"
)

func main() {
	clean := flag.Bool("clean", false, "导入前清空 documents 与 chunks 两表")
	flag.Parse()

	cfg, err := config.Load()
	if err != nil {
		log.Fatal("配置错误：", err)
	}

	st, err := store.New(cfg.MySQLDSN)
	if err != nil {
		log.Fatal(err)
	}
	defer st.Close()

	// -clean：清空旧数据，保证重灌后库里只有当前语料+当前切片器的结果
	if *clean {
		if err := st.ClearAll(); err != nil {
			log.Fatal(err)
		}
		log.Println("已清空 documents 与 chunks，开始重新导入...")
	}

	emb := ingest.NewEmbedder(cfg.Embed.BaseURL, cfg.Embed.APIKey, cfg.Embed.Model)

	// 扫描 corpus/ 下所有 .md 文件
	files, err := filepath.Glob("corpus/*.md")
	if err != nil || len(files) == 0 {
		log.Fatal("corpus/ 下没有找到 .md 文件")
	}

	// 先统计总切片数，用于打印 [n/total] 进度
	type docChunks struct {
		file   string
		title  string
		raw    string
		pieces []ingest.ChunkPiece
	}
	var docs []docChunks
	total := 0
	for _, f := range files {
		// STATS.md 是切片统计报告，不是语料，跳过
		if strings.EqualFold(filepath.Base(f), "STATS.md") {
			continue
		}
		raw, err := os.ReadFile(f)
		if err != nil {
			log.Fatalf("读取 %s 失败：%v", f, err)
		}
		pieces := ingest.SplitMarkdownV2(string(raw)) // 阶段 2 起使用切片器 v2
		if len(pieces) == 0 {
			continue
		}
		title := strings.TrimSuffix(filepath.Base(f), ".md")
		docs = append(docs, docChunks{file: filepath.Base(f), title: title, raw: string(raw), pieces: pieces})
		total += len(pieces)
	}
	log.Printf("共 %d 篇文档，%d 个切片，开始入库（embedding: %s @ %s）...", len(docs), total, cfg.Embed.Model, cfg.Embed.BaseURL)

	n := 0
	for _, d := range docs {
		docID, err := st.InsertDocument(d.title, d.file, d.raw)
		if err != nil {
			log.Fatalf("文档 %s 入库失败：%v", d.file, err)
		}
		for _, p := range d.pieces {
			// embedding 输入带上标题路径，检索效果更好
			vec, err := emb.Embed(p.HeadingPath + "\n" + p.Content)
			if err != nil {
				log.Fatalf("切片 embedding 失败：%v", err)
			}
			vecJSON, _ := json.Marshal(vec)
			if _, err := st.InsertChunk(docID, p.HeadingPath, p.Content, string(vecJSON)); err != nil {
				log.Fatalf("切片入库失败：%v", err)
			}
			n++
			fmt.Printf("[%d/%d] chunk saved\n", n, total)
		}
	}
	log.Printf("导入完成：%d 个切片", n)
}
