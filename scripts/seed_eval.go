//go:build ignore

// 金标评测集导入脚本：go run ./scripts/seed_eval.go
// 审阅模式（只读不写库）：go run ./scripts/seed_eval.go -review
// 使用 ignore 构建标签避免与同目录 ingest.go 的 main 函数冲突；显式 go run 本文件不受影响。
package main

import (
	"flag"
	"fmt"
	"log"
	"strings"

	"knowledge-qa/config"
	"knowledge-qa/store"
)

type seedQuestion struct {
	question     string
	goldAnswer   string
	shouldRefuse bool
	document     string
	headingPath  string
	snippet      string
}

// 18 条可作答题覆盖 18 篇不同文档；7 条拒答题覆盖无关、沾边未收录和诱导编造。
// 切片以文档、标题和原文片段定位，执行时写入真实 gold_chunk_id，重灌语料后仍可复现。
var goldQuestions = []seedQuestion{
	{
		question:   "在 Gin 中，如何让服务只监听本机回环地址的 8080 端口？",
		goldAnswer: "调用 r.Run(\"127.0.0.1:8080\")。",
		document:   "gin-quickstart.md", headingPath: "Gin 安装与快速开始 > 端口与监听地址",
		snippet: "r.Run(\"127.0.0.1:8080\")  // 只监听本机回环",
	},
	{
		question:   "在 v1 := r.Group(\"/v1\") 组内注册 v1.GET(\"/users\", listUsersV1) 后，最终请求路径是什么？",
		goldAnswer: "最终请求路径是 /v1/users，即组前缀 /v1 加组内路径 /users。",
		document:   "gin-routing.md", headingPath: "Gin 路由 > 路由组",
		snippet: "最终路径是组前缀加组内路径，如 `/v1/users`。",
	},
	{
		question:   "Gin 的 * 通配符参数能匹配哪些路径内容，在位置上有什么限制？",
		goldAnswer: "* 通配符匹配剩余所有路径，可以包含斜杠，并且只能出现在路径末尾。",
		document:   "gin-route-params.md", headingPath: "Gin 路由参数 > 通配符参数 *action",
		snippet: "通配符匹配**剩余所有路径**（可以包含斜杠），且只能出现在路径末尾",
	},
	{
		question:   "在读取查询参数时，c.GetQuery 如何帮助区分“没传”和“传了空值”？",
		goldAnswer: "c.GetQuery 除返回参数值外，还多返回一个 bool 表示参数是否存在，因此可以区分未传参和传了空值。",
		document:   "gin-query-params.md", headingPath: "Gin 查询参数 > GetQuery 与存在性判断",
		snippet: "多返回一个 bool 表示参数是否存在，区分\"没传\"和\"传了空值\"",
	},
	{
		question:   "Gin 中带 Should 前缀的绑定方法与不带 Should 的 c.BindJSON 在出错时有何区别？为什么生产代码建议使用 Should 系列？",
		goldAnswer: "Should 系列出错时只返回 error，由开发者决定响应；c.BindJSON 等会自动写入 400 响应并调用 Abort，但处理函数仍会继续执行，容易造成二次写响应，因此生产代码建议使用 Should 系列。",
		document:   "gin-binding.md", headingPath: "Gin JSON 绑定与校验 > ShouldBind 与 Bind 的区别",
		snippet: "出错时会自动写 400 响应并调用 Abort，处理函数仍会继续执行",
	},
	{
		question:   "在自定义 Gin 中间件中，c.Next() 前后的代码分别属于什么阶段？调用 c.Abort() 有什么效果？",
		goldAnswer: "c.Next() 之前是请求阶段，之后是响应阶段；调用 c.Abort() 会阻止处理链继续执行，使后续业务处理函数不会执行。",
		document:   "gin-middleware.md", headingPath: "Gin 中间件 > 编写自定义中间件",
		snippet: "`c.Next()` 之前是\"请求阶段\"，之后是\"响应阶段\"。",
	},
	{
		question:   "Windows 的 Go 安装器会自动如何配置 PATH 环境变量？",
		goldAnswer: "安装器会自动把 Go 的 bin 目录加入 PATH 环境变量。",
		document:   "go-install.md", headingPath: "Go 安装与环境配置 > 下载与安装",
		snippet: "安装器会自动把 Go 的 bin 目录加入 PATH 环境变量。",
	},
	{
		question:   "在 for i := 0; i < 5; i++ 循环中执行 defer fmt.Println(i)，会按什么顺序输出？",
		goldAnswer: "依次输出 4、3、2、1、0，因为多个 defer 按后进先出顺序执行。",
		document:   "go-control-flow.md", headingPath: "Go 控制流：for、if、switch > defer 延迟调用",
		snippet: "defer fmt.Println(i) // 输出 4 3 2 1 0",
	},
	{
		question:   "在 Go 语言中，函数可变参数在类型前加什么符号？在函数内部它是什么类型，调用时如何把切片展开传入？",
		goldAnswer: "在类型前加 ...，函数内部它是一个切片；调用时使用 切片...（如 sum(nums...)）展开传入。",
		document:   "go-functions.md", headingPath: "Go 函数 > 可变参数",
		snippet: "参数类型前加 `...` 表示可变参数，函数内它是一个切片：",
	},
	{
		question:   "一个接口值在什么条件下才是 nil？",
		goldAnswer: "只有接口值的具体类型和值都为 nil 时，该接口值才是 nil。",
		document:   "go-interfaces.md", headingPath: "Go 接口 > 接口值与 nil",
		snippet: "一个接口值只有类型和值都为 nil 时才是 nil。",
	},
	{
		question:   "main 函数返回时，尚未运行完的 goroutine 会怎样？",
		goldAnswer: "所有 goroutine 都会被强制结束，无论是否已经运行完。",
		document:   "go-goroutines.md", headingPath: "Go 并发：goroutine > 一个完整的例子",
		snippet: "main 函数返回时所有 goroutine 都会被强制结束，不管有没有跑完。",
	},
	{
		question:   "当 select 等待的多个 channel 操作同时就绪时，会选择执行哪一个？",
		goldAnswer: "会从同时就绪的操作中随机选择一个执行。",
		document:   "go-channels.md", headingPath: "Go 并发：channel > select 多路复用",
		snippet: "哪个就绪执行哪个；多个同时就绪时随机选一个：",
	},
	{
		question:   "将结构不确定的 JSON 解析到 map[string]interface{} 时，数字会变成什么类型，之后需要如何处理？",
		goldAnswer: "数字会变成 float64，之后需要自行进行类型断言。",
		document:   "go-json.md", headingPath: "Go 处理 JSON（encoding/json） > map[string]interface{}",
		snippet: "数字会变成 float64，需要自己类型断言。",
	},
	{
		question:   "调用 http.ListenAndServe 时，第二个参数传 nil 表示什么？",
		goldAnswer: "表示使用默认的 DefaultServeMux。",
		document:   "go-http-server.md", headingPath: "Go net/http 标准库 > 起服务",
		snippet: "第二个参数传 nil 表示使用默认的 DefaultServeMux。",
	},
	{
		question:   "context.WithCancel 返回的 cancel 函数有什么作用，为什么使用后必须调用它？",
		goldAnswer: "调用 cancel 会通知所有监听者退出；使用后必须调用它以防止泄漏。",
		document:   "go-context.md", headingPath: "Go context 包 > 取消与超时",
		snippet: "调用 cancel() 通知所有监听者退出，使用后必须调用 cancel 防止泄漏。",
	},
	{
		question:   "如何只运行 TestAdd 中名为“正数”的子测试？",
		goldAnswer: "执行命令 go test -run TestAdd/正数。",
		document:   "go-testing.md", headingPath: "Go 测试 > 子测试",
		snippet: "只跑某个子测试：`go test -run TestAdd/正数`。",
	},
	{
		question:   "为什么判断错误链上是否有某个哨兵错误时应使用 errors.Is，而不要直接使用 ==？",
		goldAnswer: "因为错误可能被包装过，errors.Is 能判断错误链，直接使用 == 可能无法识别被包装的哨兵错误。",
		document:   "go-errors.md", headingPath: "Go 错误处理 > errors.Is 与 errors.As",
		snippet: "用 `errors.Is`，不要直接用 ==（错误可能被包装过）",
	},
	{
		question:   "在泛型约束 ~int | ~float64 中，~ 和 | 分别表示什么？",
		goldAnswer: "~ 表示底层类型匹配，| 表示类型并集。",
		document:   "go-generics.md", headingPath: "Go 泛型（Go 1.18+） > 约束",
		snippet: "~ 表示底层类型匹配，| 表示并集。",
	},
	{question: "今天天气怎么样？", goldAnswer: "文档中没有依据", shouldRefuse: true},
	{question: "Gin 怎么做 WebSocket 长连接？", goldAnswer: "文档中没有依据", shouldRefuse: true},
	{question: "Gin 作者最喜欢的颜色是什么？", goldAnswer: "文档中没有依据", shouldRefuse: true},
	{question: "Go 语言作者早餐爱吃什么？", goldAnswer: "文档中没有依据", shouldRefuse: true},
	{question: "Gin 如何自动申请和续期 Let's Encrypt 证书？", goldAnswer: "文档中没有依据", shouldRefuse: true},
	{question: "Go 1.27 新增了哪些语法特性？", goldAnswer: "文档中没有依据", shouldRefuse: true},
	{question: "怎么做法式红酒炖牛肉？", goldAnswer: "文档中没有依据", shouldRefuse: true},
}

func main() {
	review := flag.Bool("review", false, "只解析真实 chunk_id 并打印人工审阅表，不写入数据库")
	flag.Parse()

	if len(goldQuestions) != 25 {
		log.Fatalf("金标题数量必须为 25，当前为 %d", len(goldQuestions))
	}
	cfg, err := config.Load()
	if err != nil {
		log.Fatal("配置错误：", err)
	}
	st, err := store.New(cfg.MySQLDSN)
	if err != nil {
		log.Fatal(err)
	}
	defer st.Close()

	questions := make([]store.EvalQuestion, 0, len(goldQuestions))
	answerable, refusals := 0, 0
	for i, item := range goldQuestions {
		q := store.EvalQuestion{
			Question: item.question, GoldAnswer: item.goldAnswer, ShouldRefuse: item.shouldRefuse,
		}
		if item.shouldRefuse {
			refusals++
		} else {
			answerable++
			chunkID, err := st.FindGoldChunk(item.document, item.headingPath, item.snippet)
			if err != nil {
				log.Fatalf("第 %d 题金标切片定位失败：%v", i+1, err)
			}
			q.GoldChunkID = &chunkID
		}
		questions = append(questions, q)
	}
	if answerable != 18 || refusals != 7 {
		log.Fatalf("应为 18 条作答题、7 条拒答题，当前为 %d、%d", answerable, refusals)
	}

	if *review {
		chunks, err := st.AllChunks()
		if err != nil {
			log.Fatal(err)
		}
		byID := make(map[int64]store.Chunk, len(chunks))
		for _, c := range chunks {
			byID[c.ID] = c
		}
		fmt.Println("# 25 条金标题人工审阅表（与 scripts/seed_eval.go 及当前数据库切片实时同步）")
		fmt.Println("\n请逐条核对问题、参考答案（gold_answer）、是否应拒答（should_refuse）与来源切片正文（gold_chunk_id）。\n")
		fmt.Println("| 序号 | should_refuse | 问题 | 参考答案 | gold_chunk_id | 来源文档与标题 | 原文片段 |")
		fmt.Println("|---|---|---|---|---|---|---|")
		for i, item := range goldQuestions {
			idText := "NULL"
			src := "—"
			excerpt := "—"
			if q := questions[i]; q.GoldChunkID != nil {
				idText = fmt.Sprintf("%d", *q.GoldChunkID)
				src = item.document + " · " + item.headingPath
				if c, ok := byID[*q.GoldChunkID]; ok {
					excerpt = strings.ReplaceAll(strings.ReplaceAll(c.Content, "\n", "<br>"), "|", "&#124;")
				}
			}
			qCell := strings.ReplaceAll(item.question, "|", "&#124;")
			aCell := strings.ReplaceAll(item.goldAnswer, "|", "&#124;")
			fmt.Printf("| %d | %t | %s | %s | %s | %s | %s |\n",
				i+1, item.shouldRefuse, qCell, aCell, idText, src, excerpt)
		}
		return
	}

	if err := st.SeedEvalQuestions(questions); err != nil {
		log.Fatal("金标题入库失败：", err)
	}
	log.Printf("金标题入库完成：共 25 条（18 条作答题，7 条拒答题）；18 个 gold_chunk_id 均已解析并校验")
}
