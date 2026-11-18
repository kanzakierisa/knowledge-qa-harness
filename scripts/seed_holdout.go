//go:build ignore

// 阶段 11 独立对抗盲测集导入脚本：go run ./scripts/seed_holdout.go
// 审阅模式（只读解析 chunk_id 并打印盲测题审阅表，不写入数据库）：go run ./scripts/seed_holdout.go -review
// 只增量追加 question_set='holdout'，绝不修改原 base52（id=1..52）及历史 eval_runs 记录。
package main

import (
	"flag"
	"fmt"
	"log"
	"strings"

	"knowledge-qa/config"
	"knowledge-qa/store"
)

type holdoutQuestion struct {
	category     string // paraphrase_correction | near_miss_refusal | cross_para_compare | project_governance
	projectSlug  string // 空串表示默认 auto 全局路由（类 1~3）；非空表示绑定特定项目隔离（类 4）
	lookupProj   string // 仅用于定位 gold_chunk_id 的项目过滤，为空时不限项目
	question     string
	goldAnswer   string
	shouldRefuse bool
	document     string
	headingPath  string
	snippet      string
}

// 39 道独立对抗盲测题（question_set='holdout'）：
//   - 类 1：换问法 / 隐含前提纠偏可作答题（11 题，避开 base52 q_id=33/37 知识点）
//   - 类 2：“高分陷阱”边界拒答题（13 题，刻意使用真实 API 名但问语料未收录细节）
//   - 类 3：多实体跨段落对比题（7 题，覆盖多个独立切片的多术语联合检索与对比）
//   - 类 4：多项目与文档治理测试题（8 题，绑定特定 project_slug 验证硬隔离与废弃/现行文档）
var holdoutQuestions = []holdoutQuestion{
	// ===== 类 1：换问法 / 隐含前提纠偏可作答题（11 题） =====
	{
		category:    "paraphrase_correction",
		lookupProj:  "go-gin",
		question:    "用 gin.Default() 创建的引擎不想自带 Logger 和 Recovery 中间件时，应该改用哪个函数从零组装？",
		goldAnswer:  "改用 gin.New()，它会返回不带任何中间件的空引擎，之后可用 r.Use() 按需自行挂载中间件。",
		document:    "gin-quickstart.md",
		headingPath: "Gin 安装与快速开始 > gin.Default 与 gin.New",
		snippet:     "想完全自己控制中间件时用它",
	},
	{
		category:    "paraphrase_correction",
		lookupProj:  "go-gin",
		question:    "把结构不确定的 JSON 反序列化进 map[string]interface{} 后，为什么其中的数值字段不能直接按 int 类型断言使用？",
		goldAnswer:  "因为解析到 map[string]interface{} 后数字会统一变成 float64，不能直接按 int 断言，需要自己进行类型断言处理。",
		document:    "go-json.md",
		headingPath: "Go 处理 JSON（encoding/json） > map[string]interface{}",
		snippet:     "数字会变成 float64，需要自己类型断言。",
	},
	{
		category:    "paraphrase_correction",
		lookupProj:  "go-gin",
		question:    "在 Gin 处理函数里调用 c.BindJSON 失败后，如果后面又写了响应语句，会产生什么后果？",
		goldAnswer:  "c.BindJSON 出错时已自动写入 400 响应并调用 Abort，但当前处理函数仍会继续执行，后面再写响应容易造成二次写响应。",
		document:    "gin-binding.md",
		headingPath: "Gin JSON 绑定与校验 > ShouldBind 与 Bind 的区别",
		snippet:     "处理函数仍会继续执行，容易造成二次写响应",
	},
	{
		category:    "paraphrase_correction",
		lookupProj:  "go-gin",
		question:    "在 Gin 中间件调用 c.AbortWithStatusJSON(403, ...) 后如果不写 return，同一函数体后面的语句会跳过吗？",
		goldAnswer:  "不会跳过。c.Abort() 只阻止后续处理函数执行，当前函数内的代码仍会继续，因此 Abort 之后通常必须紧跟 return。",
		document:    "gin-error-handling.md",
		headingPath: "Gin 错误处理 > 主动终止请求链",
		snippet:     "当前函数内的代码仍会继续，所以 Abort 之后通常紧跟 return",
	},
	{
		category:    "paraphrase_correction",
		lookupProj:  "go-gin",
		question:    "在 Gin 里如果已经注册了 /user/:id，还能再注册同位置不同参数名的 /user/:name 路由吗？",
		goldAnswer:  "不能。同一个位置只能注册一种参数名，/user/:id 和 /user/:name 冲突，注册第二个会 panic。",
		document:    "gin-routing.md",
		headingPath: "Gin 路由 > 路由冲突规则",
		snippet:     "同一个位置只能注册一种参数名",
	},
	{
		category:    "paraphrase_correction",
		lookupProj:  "go-gin",
		question:    "能否把 r.Static 注册的静态资源路径前缀也设成与业务 API 相同的 /api 开头？",
		goldAnswer:  "不能。Static 注册的路径前缀不要和 API 路由冲突，都用 /api 开头会产生路由 panic（参数冲突）。",
		document:    "gin-static-files.md",
		headingPath: "Gin 静态文件服务 > 注意事项",
		snippet:     "Static 注册的路径前缀不要和 API 路由冲突",
	},
	{
		category:    "paraphrase_correction",
		lookupProj:  "go-gin",
		question:    "在 Go 并发编程中，消费者（接收方）从 channel 读完数据后可以顺手调用 close(ch) 吗？",
		goldAnswer:  "不可以。只有发送方才应该关闭 channel，接收方关闭会向发送方制造 panic。",
		document:    "go-channels.md",
		headingPath: "Go 并发：channel > close 与 range",
		snippet:     "接收方关闭会向发送方制造 panic",
	},
	{
		category:    "paraphrase_correction",
		lookupProj:  "go-gin",
		question:    "把一个值为 nil 的具体类型指针赋值给 Go 接口变量后，判断该接口变量 == nil 会得到 true 吗？",
		goldAnswer:  "不会。一个接口值只有具体类型和值都为 nil 时才是 nil；若存的是 nil 指针的具体类型，接口本身不是 nil。",
		document:    "go-interfaces.md",
		headingPath: "Go 接口 > 接口值与 nil",
		snippet:     "如果接口里存的是一个 nil 指针的具体类型",
	},
	{
		category:    "paraphrase_correction",
		lookupProj:  "go-gin",
		question:    "在 Go 源文件包级别声明全局变量时，可以用 := 短变量声明替代 var 吗？",
		goldAnswer:  "不能。:= 只能用在函数内部，包级别的变量必须用 var 声明。",
		document:    "go-variables-types.md",
		headingPath: "Go 变量与基本类型 > 短变量声明",
		snippet:     "包级别的变量必须用 var",
	},
	{
		category:    "paraphrase_correction",
		lookupProj:  "go-gin",
		question:    "Go 的 switch 语句在命中某个 case 后如果不写 break，会自动继续执行下一个 case 吗？",
		goldAnswer:  "不会。Go 的 switch 匹配后会自动 break，不会穿透到下一个 case（除非显式写 fallthrough）。",
		document:    "go-control-flow.md",
		headingPath: "Go 控制流：for、if、switch > switch 语句",
		snippet:     "不会穿透到下一个 case",
	},
	{
		category:    "paraphrase_correction",
		lookupProj:  "go-gin",
		question:    "按照 Go 官方约定，context.Context 适合直接存放在结构体字段里跨方法复用吗？",
		goldAnswer:  "不适合。约定将 ctx context.Context 作为函数的第一个参数传递，不要存在结构体字段里。",
		document:    "go-context.md",
		headingPath: "Go context 包 > 创建",
		snippet:     "不要存在结构体字段里",
	},

	// ===== 类 2：“高分陷阱”边界拒答题（13 题，should_refuse=1） =====
	{
		category:     "near_miss_refusal",
		question:     "gin.Default() 自带的 Logger 中间件默认会把访问日志同时写入哪个本地磁盘路径？",
		goldAnswer:   "文档中没有依据",
		shouldRefuse: true,
	},
	{
		category:     "near_miss_refusal",
		question:     "c.ShouldBindJSON 内部默认使用 Go 标准库 encoding/json 还是 goccy/go-json 来解码请求体？",
		goldAnswer:   "文档中没有依据",
		shouldRefuse: true,
	},
	{
		category:     "near_miss_refusal",
		question:     "filepath.Join 在拼接文件路径时会自动校验目标路径在磁盘上是否存在吗？",
		goldAnswer:   "文档中没有依据",
		shouldRefuse: true,
	},
	{
		category:     "near_miss_refusal",
		question:     "sync.WaitGroup 可以按值传参进子 goroutine 吗，它内部最多支持记录多少个计数？",
		goldAnswer:   "文档中没有依据",
		shouldRefuse: true,
	},
	{
		category:     "near_miss_refusal",
		question:     "regexp.MustCompile 支持 PCRE 负向环视（lookahead/lookbehind）正则语法吗？",
		goldAnswer:   "文档中没有依据",
		shouldRefuse: true,
	},
	{
		category:     "near_miss_refusal",
		question:     "c.SetCookie 的 domain 参数传入通配符 *.example.com 时，Gin 内部会自动转义成哪种格式？",
		goldAnswer:   "文档中没有依据",
		shouldRefuse: true,
	},
	{
		category:     "near_miss_refusal",
		question:     "r.MaxMultipartMemory 超出上限写到临时文件时，Gin 默认把临时文件保存在哪个系统目录，请求结束后会自动删除吗？",
		goldAnswer:   "文档中没有依据",
		shouldRefuse: true,
	},
	{
		category:     "near_miss_refusal",
		question:     "errors.Is 在沿着 Unwrap() 链查找哨兵错误时，最大允许递归多少层错误包装？",
		goldAnswer:   "文档中没有依据",
		shouldRefuse: true,
	},
	{
		category:     "near_miss_refusal",
		question:     "go-playground/validator 的 binding:\"email\" 校验标签在 Gin 中会发 DNS MX 记录查询来验证邮箱域名真实存在吗？",
		goldAnswer:   "文档中没有依据",
		shouldRefuse: true,
	},
	{
		category:     "near_miss_refusal",
		question:     "c.AbortWithStatusJSON 写入的错误响应支持自动开启 Gzip 压缩吗，默认压缩级别是多少？",
		goldAnswer:   "文档中没有依据",
		shouldRefuse: true,
	},
	{
		category:     "near_miss_refusal",
		question:     "Go 的 append 在对容量超过 1024 的切片扩容时，具体按百分之多少的倍率增长？",
		goldAnswer:   "文档中没有依据",
		shouldRefuse: true,
	},
	{
		category:     "near_miss_refusal",
		question:     "未显式调用 db.SetConnMaxLifetime 时，Go database/sql 连接池默认的连接最大存活时间是多少分钟？",
		goldAnswer:   "文档中没有依据",
		shouldRefuse: true,
	},
	{
		category:     "near_miss_refusal",
		question:     "json.Marshal 序列化含循环引用的结构体指针时，默认会截断到第几层递归深度？",
		goldAnswer:   "文档中没有依据",
		shouldRefuse: true,
	},

	// ===== 类 3：多实体跨段落对比题（7 题） =====
	{
		category:    "cross_para_compare",
		lookupProj:  "go-gin",
		question:    "在 Gin 中 c.Query 与 c.Param 分别用于读取哪种参数？找不到参数时 c.Query 会返回什么？",
		goldAnswer:  "c.Query 用于读取 URL ? 后面的查询参数（参数不存在时返回空字符串），c.Param 用于读取路径中的 :name 路径参数（如 /user/:name）。",
		document:    "gin-route-params.md",
		headingPath: "Gin 路由参数 > 路径参数 :name",
		snippet:     "处理函数里用 `c.Param(\"name\")` 取值",
	},
	{
		category:    "cross_para_compare",
		lookupProj:  "go-gin",
		question:    "在 Go 并发编程中，sync.Mutex 与 sync.Once 各自的用途和核心调用方法有什么区别？",
		goldAnswer:  "sync.Mutex 是互斥锁，通过 Lock/Unlock 保护临界区共享内存防止数据竞争；sync.Once 通过 once.Do 保证某个函数无论被多少 goroutine 调用都只执行一次，常用于懒初始化。",
		document:    "go-goroutines.md",
		headingPath: "Go 并发：goroutine > sync.Once 与单次执行",
		snippet:     "`sync.Once` 保证某个函数只执行一次",
	},
	{
		category:    "cross_para_compare",
		lookupProj:  "go-gin",
		question:    "errors.Is 与 errors.As 各自解决什么问题？自定义错误类型实现哪个方法才能让它们沿着错误链向下查找？",
		goldAnswer:  "errors.Is 用于判断错误链上是否包含某个哨兵错误，errors.As 用于把错误转为具体错误类型以读取其字段；自定义错误类型需要实现 Unwrap 方法让它们沿错误链向下查找。",
		document:    "go-errors.md",
		headingPath: "Go 错误处理 > 自定义错误类型",
		snippet:     "实现 Unwrap 方法可以让 errors.Is 和 errors.As 沿着错误链继续向下找。",
	},
	{
		category:    "cross_para_compare",
		lookupProj:  "go-gin",
		question:    "Go 结构体方法中值接收者与指针接收者在修改字段时有何差异？同一类型的方法集通常建议如何保持风格？",
		goldAnswer:  "指针接收者可以修改接收者指向的原结构体字段，而值接收者操作的是原值的副本、修改不会影响原结构体；同一类型的方法集应保持一致风格，不要指针接收者和值接收者混用。",
		document:    "go-structs-methods.md",
		headingPath: "Go 结构体与方法 > 指针接收者与值接收者",
		snippet:     "不要指针接收者和值接收者混用。",
	},
	{
		category:    "cross_para_compare",
		lookupProj:  "go-gin",
		question:    "在 Gin 静态文件服务中，r.Static、r.StaticFS 与 r.StaticFile 各自适用于什么场景？",
		goldAnswer:  "r.Static 把一个 URL 前缀映射到本地目录以提供目录下文件访问；r.StaticFS 使用自定义的 http.FileSystem（可配合 embed 等虚拟文件系统）提供服务；r.StaticFile 只暴露单个固定文件（如 favicon.ico）。",
		document:    "gin-static-files.md",
		headingPath: "Gin 静态文件服务 > StaticFS 与 StaticFile",
		snippet:     "`r.StaticFile` 只暴露单个文件",
	},
	{
		category:    "cross_para_compare",
		lookupProj:  "go-gin",
		question:    "Go 单元测试中 t.Error 与 t.Fatal 的执行行为有何区别？只跑某个子测试和跑基准测试分别用 go test 的哪个参数？",
		goldAnswer:  "t.Error 标记失败但继续执行后续代码，t.Fatal 标记失败并立即终止当前测试函数；只跑某个子测试用 go test -run（如 -run TestAdd/正数），跑基准测试用 go test -bench=.",
		document:    "go-testing.md",
		headingPath: "Go 测试 > 第一个测试",
		snippet:     "t.Error 标记失败但继续执行后续代码；t.Fatal 标记失败并立即终止当前测试函数。",
	},
	{
		category:    "cross_para_compare",
		lookupProj:  "go-gin",
		question:    "在 Gin 查询参数读取中，c.QueryArray 与 c.QueryMap 分别用于处理什么形式的 URL 参数，c.DefaultQuery 起什么作用？",
		goldAnswer:  "c.QueryArray 用于读取同一键出现多次的数组参数（如 ?tag=go&tag=web），c.QueryMap 用于读取 map 形式的查询参数（如 ?ids[a]=1&ids[b]=2），c.DefaultQuery 用于在参数不存在时返回指定默认值。",
		document:    "gin-query-params.md",
		headingPath: "Gin 查询参数 > 读取多个值",
		snippet:     "map 形式的查询参数（如 `?ids[a]=1&ids[b]=2`）用 `c.QueryMap(\"ids\")` 读取。",
	},

	// ===== 类 4：多项目与文档治理测试题（8 题，绑定特定 project_slug） =====
	{
		category:    "project_governance",
		projectSlug: "project-alpha",
		lookupProj:  "project-alpha",
		question:    "当前项目的服务默认监听端口是多少？",
		goldAnswer:  "Alpha 项目的服务默认监听端口为 8091，用于对外提供 REST API，禁止擅自修改为其他端口。",
		document:    "alpha-port.md",
		headingPath: "Alpha 服务配置 > 服务默认监听端口",
		snippet:     "Alpha 项目的服务默认监听端口为 8091",
	},
	{
		category:    "project_governance",
		projectSlug: "project-beta",
		lookupProj:  "project-beta",
		question:    "当前项目的网关服务默认监听端口是多少，能否擅自修改？",
		goldAnswer:  "Beta 项目的服务默认监听端口为 9090，用于对外提供网关服务，禁止擅自修改为其他端口。",
		document:    "beta-port.md",
		headingPath: "Beta 服务配置 > 服务默认监听端口",
		snippet:     "Beta 项目的服务默认监听端口为 9090",
	},
	{
		category:    "project_governance",
		projectSlug: "go-gin",
		lookupProj:  "go-gin",
		question:    "不传参数直接调用 r.Run() 时，服务默认监听哪个端口？",
		goldAnswer:  "r.Run() 不传参数时默认监听 :8080。",
		document:    "gin-quickstart.md",
		headingPath: "Gin 安装与快速开始 > 端口与监听地址",
		snippet:     "`r.Run()` 不传参数时监听 `:8080`。",
	},
	{
		category:     "project_governance",
		projectSlug:  "project-alpha",
		question:     "创作者历史激励补贴的结算周期和单月封顶金额是多少？",
		goldAnswer:   "文档中没有依据",
		shouldRefuse: true,
	},
	{
		category:     "project_governance",
		projectSlug:  "project-beta",
		question:     "Gin 中怎么使用 c.ShouldBindJSON 绑定 JSON 请求体？",
		goldAnswer:   "文档中没有依据",
		shouldRefuse: true,
	},
	{
		category:     "project_governance",
		projectSlug:  "go-gin",
		question:     "Alpha 项目和 Beta 项目的默认网关监听端口分别有什么用途？",
		goldAnswer:   "文档中没有依据",
		shouldRefuse: true,
	},
	{
		category:    "project_governance",
		projectSlug: "project-beta",
		lookupProj:  "project-beta",
		question:    "旧版激励活动规则 v1 中，创作者历史补贴的结算周期和单月封顶是多少？",
		goldAnswer:  "在旧版激励活动规则 v1 中，创作者历史补贴按 T+7 工作日统一结算，单月补贴封顶 300 元。",
		document:    "beta-legacy-reward.md",
		headingPath: "旧版激励活动规则 v1 > 历史激励补贴结算规则",
		snippet:     "按 T+7 工作日统一结算，单月补贴封顶 300 元",
	},
	{
		category:    "project_governance",
		projectSlug: "project-beta",
		lookupProj:  "project-beta",
		question:    "新版激励活动 v2 的现行积分兑换规范是如何规定的？",
		goldAnswer:  "新版激励活动 v2 全面改为季度积分商城兑换，不再沿用月度现金补贴模式。",
		document:    "beta-new-reward.md",
		headingPath: "新版激励活动规则 v2 > 现行积分兑换规范",
		snippet:     "新版激励活动 v2 全面改为季度积分商城兑换",
	},
}

func main() {
	review := flag.Bool("review", false, "只解析真实 chunk_id 并打印对抗盲测审阅表，不写入数据库")
	flag.Parse()

	if len(holdoutQuestions) < 35 {
		log.Fatalf("阶段 11 对抗盲测集应不少于 35 题，当前为 %d", len(holdoutQuestions))
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

	questions := make([]store.EvalQuestion, 0, len(holdoutQuestions))
	catCounts := map[string]int{}
	answerable, refusals := 0, 0
	for i, item := range holdoutQuestions {
		catCounts[item.category]++
		q := store.EvalQuestion{
			Question:     item.question,
			GoldAnswer:   item.goldAnswer,
			ShouldRefuse: item.shouldRefuse,
			ProjectSlug:  item.projectSlug,
			QuestionSet:  "holdout",
			Category:     item.category,
		}
		if item.shouldRefuse {
			refusals++
		} else {
			answerable++
			chunkID, docID, err := st.FindGoldChunkWithDoc(item.lookupProj, item.document, item.headingPath, item.snippet)
			if err != nil {
				log.Fatalf("第 %d 题（%s）金标切片定位失败：%v", i+1, item.category, err)
			}
			q.GoldChunkID = &chunkID
			q.SourceDocID = &docID
		}
		questions = append(questions, q)
	}

	if catCounts["paraphrase_correction"] < 10 ||
		catCounts["near_miss_refusal"] < 12 ||
		catCounts["cross_para_compare"] < 6 ||
		catCounts["project_governance"] < 7 {
		log.Fatalf("分类题数未满足阶段 11 规格要求：%+v", catCounts)
	}

	if *review {
		fmt.Printf("# 阶段 11 独立对抗盲测集（holdout）审阅表（共 %d 题：可作答 %d 题，应拒答 %d 题）\n\n",
			len(questions), answerable, refusals)
		fmt.Printf("分类分布：paraphrase_correction=%d | near_miss_refusal=%d | cross_para_compare=%d | project_governance=%d\n\n",
			catCounts["paraphrase_correction"], catCounts["near_miss_refusal"],
			catCounts["cross_para_compare"], catCounts["project_governance"])
		fmt.Println("| 序号 | category | project_slug | should_refuse | 问题 | 参考答案 | gold_chunk_id | 来源 |")
		fmt.Println("|---|---|---|---|---|---|---|---|")
		for i, item := range holdoutQuestions {
			idText := "NULL"
			src := "—"
			proj := item.projectSlug
			if proj == "" {
				proj = "auto"
			}
			if q := questions[i]; q.GoldChunkID != nil {
				idText = fmt.Sprintf("%d", *q.GoldChunkID)
				src = item.document + " · " + item.headingPath
			}
			qCell := strings.ReplaceAll(item.question, "|", "&#124;")
			aCell := strings.ReplaceAll(item.goldAnswer, "|", "&#124;")
			fmt.Printf("| %d | %s | %s | %t | %s | %s | %s | %s |\n",
				i+1, item.category, proj, item.shouldRefuse, qCell, aCell, idText, src)
		}
		return
	}

	inserted, err := st.AppendEvalQuestions(questions)
	if err != nil {
		log.Fatal("阶段 11 对抗盲测集入库失败：", err)
	}
	log.Printf("阶段 11 对抗盲测集入库完成：本次新增 %d 条（共校验 %d 条，可作答 %d + 拒答 %d，question_set=holdout）",
		inserted, len(questions), answerable, refusals)
}
