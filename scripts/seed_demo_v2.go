//go:build ignore

// 阶段 13 多项目实战演示沙箱一键灌库脚本：
//
//	go run ./scripts/seed_demo_v2.go
//	go run ./scripts/seed_demo_v2.go -with-base-eval   # 额外按需导入 base52 / holdout 基准评测集
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"knowledge-qa/config"
	"knowledge-qa/ingest"
	"knowledge-qa/store"
)

type demoDocSpec struct {
	ProjectSlug string
	FilePath    string
	Title       string
	DocLevel    string
	Owner       string
}

func main() {
	withBaseEval := flag.Bool("with-base-eval", false, "若当前库尚未导入 base52/holdout 基准评测集，按需一并导入")
	showGoGin := flag.Bool("show-go-gin", false, "是否在前端界面公开展示 go-gin 基线技术文档（默认仅隐藏用于 52+39 回归评测）")
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

	emb := ingest.NewEmbedder(cfg.Embed.BaseURL, cfg.Embed.APIKey, cfg.Embed.Model)
	conv := ingest.NewConverter(ingest.ConvertOptions{Engine: cfg.ConvertEngine})

	// 1. 幂等初始化内容消费多项目空间；go-gin 默认隐藏（is_hidden=1）保留 52+39 评测基线，不删库不污染产品展示
	if _, err := st.EnsureProject("shared", "【示例】公共规范库", "跨业务线共享的合规红线、研发上线值班与错误码规范", false); err != nil {
		log.Fatalf("初始化 shared 项目失败：%v", err)
	}
	if _, err := st.EnsureProject("go-gin", "Go/Gin 技术基线（评测隐藏）", "V1/V2 52+39 金标评测回归语料（默认隐藏，可通过 -show-go-gin 或评测集调用）", !*showGoGin); err != nil {
		log.Fatalf("初始化 go-gin 项目失败：%v", err)
	}
	if _, err := st.EnsureProject("content-promo", "【示例】内容活动运营", "内容消费创作激励活动规则、排期表与审批规范", false); err != nil {
		log.Fatalf("初始化 content-promo 项目失败：%v", err)
	}
	if _, err := st.EnsureProject("short-drama", "【示例】红果短剧业务线", "短剧版权三件套审核、上线核查与商业分账结算规范", false); err != nil {
		log.Fatalf("初始化 short-drama 项目失败：%v", err)
	}
	if _, err := st.EnsureProject("novel-growth", "【示例】番茄小说创作者增长", "网文作者签约分成、分级全勤激励与版本迭代规范", false); err != nil {
		log.Fatalf("初始化 novel-growth 项目失败：%v", err)
	}

	allProjs, err := st.ListAllProjects()
	if err != nil {
		log.Fatalf("读取项目列表失败：%v", err)
	}
	for _, p := range allProjs {
		if p.Slug == "project-alpha" || p.Slug == "project-beta" {
			if err := st.SetProjectHidden(p.Slug, true); err != nil {
				log.Printf("设置隐藏夹具项目 %s 失败（可忽略）：%v", p.Slug, err)
			} else {
				log.Printf("已将离线盲测夹具项目 %q 标记为隐藏（is_hidden=1），日常界面与 auto 检索不可见", p.Slug)
			}
		}
	}

	// 2. 双模式判断 go-gin：仅当空库时复用 corpus/*.md 导入，已有基线数据时完整保留不动
	existingGoDocs, err := st.ListDocuments("go-gin", "")
	if err != nil {
		log.Fatalf("检查 go-gin 文档失败：%v", err)
	}
	if len(existingGoDocs) == 0 {
		log.Println("检测到 go-gin 尚无文档，正在复用 corpus/*.md 导入基础技术文档...")
		if err := seedGoGinFromCorpus(st, emb); err != nil {
			log.Fatalf("导入 go-gin 语料失败：%v", err)
		}
	} else {
		log.Printf("检测到 go-gin 已存在 %d 篇文档，完整保留基线语料与切片", len(existingGoDocs))
	}

	// 3. 幂等导入 corpus_v2/ 下内容消费多项目多格式演示语料
	demoDocs := []demoDocSpec{
		{
			ProjectSlug: "shared",
			FilePath:    filepath.Join("corpus_v2", "shared", "team-engineering-norms.md"),
			Title:       "团队研发上线与值班通用规范",
			DocLevel:    "spec",
			Owner:       "架构委员会",
		},
		{
			ProjectSlug: "shared",
			FilePath:    filepath.Join("corpus_v2", "shared", "content-compliance-baseline.md"),
			Title:       "内容消费通用合规与审核红线总则",
			DocLevel:    "spec",
			Owner:       "内容安全与合规组",
		},
		{
			ProjectSlug: "content-promo",
			FilePath:    filepath.Join("corpus_v2", "content-promo", "winter-creator-incentive-handbook.docx"),
			Title:       "冬季创作激励活动执行手册",
			DocLevel:    "spec",
			Owner:       "活动运营组",
		},
		{
			ProjectSlug: "content-promo",
			FilePath:    filepath.Join("corpus_v2", "content-promo", "promo-launch-schedule-budget.xlsx"),
			Title:       "活动上线排期与值班预算表",
			DocLevel:    "note",
			Owner:       "运营PMO",
		},
		{
			ProjectSlug: "content-promo",
			FilePath:    filepath.Join("corpus_v2", "content-promo", "promo-reward-and-port-v1.md"),
			Title:       "旧版活动发奖与端口配置规范 v1",
			DocLevel:    "spec",
			Owner:       "历史规范归档",
		},
		{
			ProjectSlug: "content-promo",
			FilePath:    filepath.Join("corpus_v2", "content-promo", "promo-reward-and-approval-v2.md"),
			Title:       "新版活动发奖与上线审批规范 v2",
			DocLevel:    "spec",
			Owner:       "陈敏",
		},
		{
			ProjectSlug: "content-promo",
			FilePath:    filepath.Join("corpus_v2", "content-promo", "promo-risk-control-sla.pdf"),
			Title:       "内容活动风控与接口限流 SLA 指南",
			DocLevel:    "spec",
			Owner:       "风控策略组",
		},
		{
			ProjectSlug: "short-drama",
			FilePath:    filepath.Join("corpus_v2", "short-drama", "short-drama-copyright-settlement.md"),
			Title:       "红果短剧版权审核与分账结算规范",
			DocLevel:    "spec",
			Owner:       "短剧版权与结算组",
		},
		{
			ProjectSlug: "short-drama",
			FilePath:    filepath.Join("corpus_v2", "short-drama", "short-drama-release-checklist.xlsx"),
			Title:       "短剧上线排期与合规检查表",
			DocLevel:    "note",
			Owner:       "剧目运营组",
		},
		{
			ProjectSlug: "novel-growth",
			FilePath:    filepath.Join("corpus_v2", "novel-growth", "novel-signing-incentive-v1.md"),
			Title:       "旧版小说作者保底签约试行办法 v1",
			DocLevel:    "spec",
			Owner:       "历史规范归档",
		},
		{
			ProjectSlug: "novel-growth",
			FilePath:    filepath.Join("corpus_v2", "novel-growth", "novel-signing-incentive-v2.docx"),
			Title:       "番茄小说作者分成与全勤激励规则 v2",
			DocLevel:    "spec",
			Owner:       "作者生态增长组",
		},
	}

	docIDsByFile := make(map[string]int64, len(demoDocs))
	for _, spec := range demoDocs {
		docID, err := ingestDemoDoc(st, emb, conv, spec)
		if err != nil {
			log.Fatalf("导入演示文档 %s 失败：%v", spec.FilePath, err)
		}
		docIDsByFile[filepath.Base(spec.FilePath)] = docID
	}

	// 4. 配置 deprecated -> replaced_by 关系
	v1ID := docIDsByFile["promo-reward-and-port-v1.md"]
	v2ID := docIDsByFile["promo-reward-and-approval-v2.md"]
	if v1ID > 0 && v2ID > 0 {
		depStatus := "deprecated"
		if _, err := st.UpdateDocumentMeta(v1ID, store.DocumentPatch{
			Status:             &depStatus,
			SetReplacedByDocID: true,
			ReplacedByDocID:    &v2ID,
		}); err != nil {
			log.Fatalf("配置废弃替代关系失败：%v", err)
		}
		log.Printf("已配置废弃替代关系：doc#%d (活动v1, deprecated) -> doc#%d (活动v2, active)", v1ID, v2ID)
	}
	novelV1ID := docIDsByFile["novel-signing-incentive-v1.md"]
	novelV2ID := docIDsByFile["novel-signing-incentive-v2.docx"]
	if novelV1ID > 0 && novelV2ID > 0 {
		depStatus := "deprecated"
		if _, err := st.UpdateDocumentMeta(novelV1ID, store.DocumentPatch{
			Status:             &depStatus,
			SetReplacedByDocID: true,
			ReplacedByDocID:    &novelV2ID,
		}); err != nil {
			log.Fatalf("配置小说规范废弃替代关系失败：%v", err)
		}
		log.Printf("已配置废弃替代关系：doc#%d (小说v1, deprecated) -> doc#%d (小说v2, active)", novelV1ID, novelV2ID)
	}

	// 5. 导入演示用回归题（写入 question_set='feedback' 并绑定 source_doc_id，用于 Doc CI/CD 与活评测演示，不污染 base52/holdout）
	if err := seedDemoFeedbackQuestions(st); err != nil {
		log.Fatalf("导入演示回归题失败：%v", err)
	}

	// 6. 清理遗留测试脏盲区并预置 2 条脱敏示例盲区
	for _, dirtyQ := range []string{"1", "怎么做法式红酒炖牛肉？"} {
		if deleted, err := st.DeleteKnowledgeGapByQuestion(dirtyQ); err == nil && deleted > 0 {
			log.Printf("已清理测试遗留盲区记录 %d 条：%q", deleted, dirtyQ)
		}
	}
	if err := seedDemoGaps(st); err != nil {
		log.Fatalf("预置示例盲区失败：%v", err)
	}

	// 7. 可选按需导入 base52 / holdout 基准评测集
	if *withBaseEval {
		if err := ensureBaseAndHoldoutEval(st, emb); err != nil {
			log.Fatalf("导入 base52/holdout 评测集失败：%v", err)
		}
	}

	log.Println("阶段 13 多项目演示沙箱初始化完成！")
}

func seedGoGinFromCorpus(st *store.Store, emb *ingest.Embedder) error {
	files, err := filepath.Glob(filepath.Join("corpus", "*.md"))
	if err != nil || len(files) == 0 {
		return fmt.Errorf("corpus/ 下未找到 .md 文件")
	}
	sort.Strings(files)
	for _, f := range files {
		base := filepath.Base(f)
		if strings.EqualFold(base, "STATS.md") {
			continue
		}
		raw, err := os.ReadFile(f)
		if err != nil {
			return err
		}
		hash := store.SHA256Hex(raw)
		if _, _, dup, err := st.CheckDuplicateDocument("go-gin", base, hash); err == nil && dup {
			continue
		}
		md := strings.ReplaceAll(strings.ReplaceAll(string(raw), "\r\n", "\n"), "\r", "\n")
		pieces := ingest.SplitMarkdownV2(md)
		if len(pieces) == 0 {
			continue
		}
		chunkInputs := make([]store.ChunkInput, 0, len(pieces))
		for _, p := range pieces {
			embText := p.Content
			if p.HeadingPath != "" {
				embText = p.HeadingPath + "\n" + p.Content
			}
			vec, err := emb.Embed(embText)
			if err != nil {
				return fmt.Errorf("%s embedding 失败：%w", base, err)
			}
			vecJSON, _ := json.Marshal(vec)
			chunkInputs = append(chunkInputs, store.ChunkInput{
				HeadingPath: p.HeadingPath,
				Content:     p.Content,
				Embedding:   string(vecJSON),
			})
		}
		if _, err := st.InsertOrReplaceDocument(store.InsertDocParams{
			ProjectSlug: "go-gin",
			Title:       strings.TrimSuffix(base, ".md"),
			Filename:    base,
			Content:     md,
			SourceType:  "md",
			ContentHash: hash,
			DocLevel:    "spec",
			Owner:       "Go/Gin 团队",
			Chunks:      chunkInputs,
		}); err != nil {
			return fmt.Errorf("%s 入库失败：%w", base, err)
		}
	}
	return nil
}

func ingestDemoDoc(st *store.Store, emb *ingest.Embedder, conv ingest.Converter, spec demoDocSpec) (int64, error) {
	raw, err := os.ReadFile(spec.FilePath)
	if err != nil {
		return 0, err
	}
	filename := filepath.Base(spec.FilePath)
	hash := store.SHA256Hex(raw)
	if existingID, chunkCnt, isDup, err := st.CheckDuplicateDocument(spec.ProjectSlug, filename, hash); err != nil {
		return 0, err
	} else if isDup {
		log.Printf("[skip] %s/%s 内容未变更（doc#%d, %d 段）", spec.ProjectSlug, filename, existingID, chunkCnt)
		return existingID, nil
	}

	md, sourceType, err := conv.Convert(filename, raw)
	if err != nil {
		return 0, err
	}
	pieces := ingest.SplitMarkdownV3(md)
	if len(pieces) == 0 {
		return 0, fmt.Errorf("%s 未切出有效段落", filename)
	}
	chunkInputs := make([]store.ChunkInput, 0, len(pieces))
	for _, p := range pieces {
		embText := p.Content
		if p.HeadingPath != "" {
			embText = p.HeadingPath + "\n" + p.Content
		}
		vec, err := emb.Embed(embText)
		if err != nil {
			return 0, err
		}
		vecJSON, _ := json.Marshal(vec)
		chunkInputs = append(chunkInputs, store.ChunkInput{
			HeadingPath: p.HeadingPath,
			Content:     p.Content,
			Embedding:   string(vecJSON),
		})
	}
	res, err := st.InsertOrReplaceDocument(store.InsertDocParams{
		ProjectSlug: spec.ProjectSlug,
		Title:       spec.Title,
		Filename:    filename,
		Content:     md,
		SourceType:  sourceType,
		ContentHash: hash,
		DocLevel:    spec.DocLevel,
		Owner:       spec.Owner,
		Overwrite:   true,
		Chunks:      chunkInputs,
	})
	if err != nil {
		return 0, err
	}
	log.Printf("[import] %s/%s (%s) -> doc#%d (%d 段)", spec.ProjectSlug, filename, sourceType, res.DocumentID, res.ChunkCount)
	return res.DocumentID, nil
}

func seedDemoFeedbackQuestions(st *store.Store) error {
	specs := []struct {
		projectSlug string
		filename    string
		heading     string
		snippet     string
		question    string
		goldAnswer  string
		category    string
	}{
		{
			projectSlug: "content-promo",
			filename:    "promo-reward-and-approval-v2.md",
			heading:     "新版活动发奖与上线审批规范 v2 > 新版活动发奖周期",
			snippet:     "T+5 个工作日",
			question:    "新版活动发奖规则中，活动结束后几个工作日内完成对账发奖？",
			goldAnswer:  "活动结束后 T+5 个工作日内由结算系统自动完成对账并触发发奖，单账号单月激励封顶 1200 元。",
			category:    "promo_demo",
		},
		{
			projectSlug: "content-promo",
			filename:    "promo-reward-and-approval-v2.md",
			heading:     "新版活动发奖与上线审批规范 v2 > 服务默认监听端口",
			snippet:     ":8082",
			question:    "内容活动运营项目的服务默认监听端口和灰度端口分别是多少？",
			goldAnswer:  "核心服务默认监听端口为 :8082，本地调试与灰度验证环境默认使用 :8083。",
			category:    "promo_demo",
		},
		{
			projectSlug: "content-promo",
			filename:    "winter-creator-incentive-handbook.docx",
			heading:     "冬季创作激励活动执行手册 > 阶梯激励档位表",
			snippet:     "1000 人",
			question:    "冬季创作激励活动的创作者基础参与门槛是什么？",
			goldAnswer:  "账号粉丝数不少于 1000 人、近 30 天公开发布原创内容不少于 3 篇、且近 90 天内无社区违规处罚记录。",
			category:    "promo_demo",
		},
		{
			projectSlug: "content-promo",
			filename:    "promo-risk-control-sla.pdf",
			heading:     "内容活动风控与接口限流 SLA 指南",
			snippet:     "42901",
			question:    "内容活动报名与领奖接口的限流阈值和触发后的业务错误码是什么？",
			goldAnswer:  "单个账号每分钟最多发起 20 次报名校验请求，超出阈值将统一返回业务错误码 42901 并触发滑动窗口冷却 60 秒。",
			category:    "promo_demo",
		},
		{
			projectSlug: "short-drama",
			filename:    "short-drama-copyright-settlement.md",
			heading:     "红果短剧版权审核与分账结算规范 > 短剧分账结算周期",
			snippet:     "T+10 个工作日",
			question:    "红果短剧商业分成多久结算一次，达到多少有效完播可以提升分成比例？",
			goldAnswer:  "每月结束后 T+10 个工作日内出具上月分账账单并触发打款；累计有效完播达到 50 万次起激活阶梯加成（基础 45%），达到 200 万次提升至 55%。",
			category:    "drama_demo",
		},
		{
			projectSlug: "novel-growth",
			filename:    "novel-signing-incentive-v2.docx",
			heading:     "番茄小说作者分成与全勤激励规则 v2 > 分级全勤激励表",
			snippet:     "800 元/月",
			question:    "新版番茄小说作者全勤激励规则中，基础全勤档和高产激励档的月更新字数与奖励分别是多少？",
			goldAnswer:  "基础全勤档要求每日不少于 4000 字且月累计 12 万字及以上，奖励 800 元/月；高产激励档要求每日不少于 6000 字且月累计 18 万字及以上，奖励 1500 元/月。",
			category:    "novel_demo",
		},
	}

	questions := make([]store.EvalQuestion, 0, len(specs))
	for _, sp := range specs {
		chunkID, docID, err := st.FindGoldChunkWithDoc(sp.projectSlug, sp.filename, sp.heading, sp.snippet)
		if err != nil {
			return fmt.Errorf("定位演示金标切片失败 (%s): %w", sp.question, err)
		}
		cID := chunkID
		dID := docID
		questions = append(questions, store.EvalQuestion{
			Question:    sp.question,
			GoldAnswer:  sp.goldAnswer,
			GoldChunkID: &cID,
			ProjectSlug: sp.projectSlug,
			QuestionSet: "feedback",
			SourceDocID: &dID,
			Category:    sp.category,
		})
	}
	inserted, err := st.AppendEvalQuestions(questions)
	if err != nil {
		return err
	}
	log.Printf("演示回归题校验完成：共 %d 题（question_set=feedback，本次新增 %d 题）", len(questions), inserted)
	return nil
}

func seedDemoGaps(st *store.Store) error {
	c1, _, err := st.FindGoldChunkWithDoc("content-promo", "winter-creator-incentive-handbook.docx", "冬季创作激励活动执行手册 > 阶梯激励档位表", "1000 人")
	if err != nil {
		return err
	}
	c2, _, err := st.FindGoldChunkWithDoc("shared", "team-engineering-norms.md", "团队研发上线与值班通用规范 > 统一业务错误码规范", "40301")
	if err != nil {
		return err
	}
	if err := st.EnsureDemoKnowledgeGap(store.RecordGapParams{
		ProjectSlug:    "content-promo",
		Question:       "海外创作者激励活动税务代扣比例是多少？",
		RefuseReason:   "threshold",
		Top1Final:      0.276,
		NearestChunkID: &c1,
		NearestHeading: "冬季创作激励活动执行手册 > 阶梯激励档位表",
	}, 3); err != nil {
		return err
	}
	if err := st.EnsureDemoKnowledgeGap(store.RecordGapParams{
		ProjectSlug:    "auto",
		Question:       "活动物料版权侵权申诉表单在哪里提交？",
		RefuseReason:   "threshold",
		Top1Final:      0.241,
		NearestChunkID: &c2,
		NearestHeading: "团队研发上线与值班通用规范 > 统一业务错误码规范",
	}, 2); err != nil {
		return err
	}
	log.Println("已幂等预置 2 条脱敏示例知识盲区记录")
	return nil
}

func ensureBaseAndHoldoutEval(st *store.Store, emb *ingest.Embedder) error {
	baseQs, err := st.EvalQuestionsBySet("base52")
	if err != nil {
		return err
	}
	if len(baseQs) == 0 {
		log.Println("正在按需导入 base52 基准评测集（25 + 27 题）...")
		if err := runGoScript("./scripts/seed_eval.go"); err != nil {
			return err
		}
		if err := runGoScript("./scripts/seed_eval_stage6.go"); err != nil {
			return err
		}
	} else {
		log.Printf("base52 评测集已存在（%d 题），跳过导入", len(baseQs))
	}

	holdoutQs, err := st.EvalQuestionsBySet("holdout")
	if err != nil {
		return err
	}
	if len(holdoutQs) == 0 {
		log.Println("正在准备隐藏评测夹具并导入 holdout 对抗盲测集（39 题）...")
		if err := ensureHiddenHoldoutFixtures(st, emb); err != nil {
			return err
		}
		if err := runGoScript("./scripts/seed_holdout.go"); err != nil {
			return err
		}
	} else {
		log.Printf("holdout 评测集已存在（%d 题），跳过导入", len(holdoutQs))
	}
	return nil
}

func ensureHiddenHoldoutFixtures(st *store.Store, emb *ingest.Embedder) error {
	if _, err := st.EnsureProject("project-alpha", "项目 Alpha", "阶段 11 隐藏评测夹具", true); err != nil {
		return err
	}
	if _, err := st.EnsureProject("project-beta", "项目 Beta", "阶段 11 隐藏评测夹具", true); err != nil {
		return err
	}
	embedMD := func(proj, filename, title, md, owner string) (int64, error) {
		hash := store.SHA256Hex([]byte(md))
		if id, _, dup, err := st.CheckDuplicateDocument(proj, filename, hash); err == nil && dup {
			return id, nil
		}
		pieces := ingest.SplitMarkdownV3(md)
		inputs := make([]store.ChunkInput, 0, len(pieces))
		for _, p := range pieces {
			vec, err := emb.Embed(p.HeadingPath + "\n" + p.Content)
			if err != nil {
				return 0, err
			}
			vj, _ := json.Marshal(vec)
			inputs = append(inputs, store.ChunkInput{HeadingPath: p.HeadingPath, Content: p.Content, Embedding: string(vj)})
		}
		res, err := st.InsertOrReplaceDocument(store.InsertDocParams{
			ProjectSlug: proj, Title: title, Filename: filename, Content: md,
			SourceType: "md", ContentHash: hash, DocLevel: "spec", Owner: owner, Overwrite: true, Chunks: inputs,
		})
		if err != nil {
			return 0, err
		}
		return res.DocumentID, nil
	}

	if _, err := embedMD("project-alpha", "alpha-port.md", "Alpha 服务端口规范",
		"# Alpha 服务配置\n\n## 服务默认监听端口\n\nAlpha 项目的服务默认监听端口为 8091，用于对外提供 REST API，禁止擅自修改为其他端口。", "张三"); err != nil {
		return err
	}
	if _, err := embedMD("project-beta", "beta-port.md", "Beta 服务端口规范",
		"# Beta 服务配置\n\n## 服务默认监听端口\n\nBeta 项目的服务默认监听端口为 9090，用于对外提供网关服务，禁止擅自修改为其他端口。", "李四"); err != nil {
		return err
	}
	oldID, err := embedMD("project-beta", "beta-legacy-reward.md", "旧版激励活动规则 v1",
		"# 旧版激励活动规则 v1\n\n## 历史激励补贴结算规则\n\n在旧版激励活动规则 v1 中，创作者历史补贴按 T+7 工作日统一结算，单月补贴封顶 300 元。", "李四")
	if err != nil {
		return err
	}
	newID, err := embedMD("project-beta", "beta-new-reward.md", "新版激励活动规则 v2",
		"# 新版激励活动规则 v2\n\n## 现行积分兑换规范\n\n新版激励活动 v2 全面改为季度积分商城兑换，不再沿用月度现金补贴模式。", "王五")
	if err != nil {
		return err
	}
	dep := "deprecated"
	_, err = st.UpdateDocumentMeta(oldID, store.DocumentPatch{Status: &dep, SetReplacedByDocID: true, ReplacedByDocID: &newID})
	return err
}

func runGoScript(scriptPath string) error {
	if _, err := exec.LookPath("go"); err != nil {
		return fmt.Errorf("未检测到 Go 环境，无法执行 %s（无 Go 环境下请不要传入 -with-base-eval 参数）", scriptPath)
	}
	cmd := exec.Command("go", "run", scriptPath)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}
