//go:build ignore

// 阶段 6 增量金标评测集导入脚本：go run ./scripts/seed_eval_stage6.go
// 审阅模式（只读不写库）：go run ./scripts/seed_eval_stage6.go -review
// 只增量追加 id=26..52，完整保留阶段 5 原 25 题与历史 eval_runs 记录。
package main

import (
	"flag"
	"fmt"
	"log"
	"strings"

	"knowledge-qa/config"
	"knowledge-qa/store"
)

type stage6Question struct {
	question     string
	goldAnswer   string
	shouldRefuse bool
	document     string
	headingPath  string
	snippet      string
}

// 27 道阶段 6 增量金标题：21 篇未覆盖语料各 1 题 + 1 题无原词语义改写（Gin 全局中间件）+ 5 题含 Gin/Go 字样的边界拒答题。
var stage6Questions = []stage6Question{
	{
		question:   "怎么让客户端删除已保存的登录凭证？",
		goldAnswer: "把 Cookie 的有效期设为负数（如 -1），且 name、path、domain 必须与设置时一致，浏览器才能正确匹配删除。",
		document:   "gin-cookies.md", headingPath: "Gin Cookie 处理 > 删除 Cookie",
		snippet: "name、path、domain 必须与设置时一致，浏览器才能正确匹配删除。",
	},
	{
		question:   "用 gin.New() 启动服务后，业务代码发生 panic 如何避免整个进程崩溃？",
		goldAnswer: "手动添加 Recovery 中间件（r.Use(gin.Recovery()) 或 gin.CustomRecovery(...)），它会捕获 panic、打印堆栈并返回 500，进程不会退出。",
		document:   "gin-error-handling.md", headingPath: "Gin 错误处理 > panic 的恢复：Recovery",
		snippet: "用 gin.New() 时务必手动添加：",
	},
	{
		question:   "Gin 上传文件时默认多大会写到临时文件？",
		goldAnswer: "默认 multipart 表单内存上限是 32MB，超出 32MB 的部分会落临时文件（不是拒绝请求），可用 r.MaxMultipartMemory 调整。",
		document:   "gin-forms.md", headingPath: "Gin 表单处理 > 文件上传（单文件）",
		snippet: "默认 multipart 表单内存上限是 32MB，可用 `r.MaxMultipartMemory` 调整（超出部分落临时文件，不是拒绝请求）。",
	},
	{
		question:   "更新已有数据时，整体替换和局部修改分别对应哪个 HTTP 动作？",
		goldAnswer: "全量（整体）更新对应 PUT，部分（局部）更新对应 PATCH。",
		document:   "gin-restful-crud.md", headingPath: "Gin 写 RESTful API 的惯例 > 资源式路由",
		snippet: "PUT    /users/:id  全量更新",
	},
	{
		question:   "不想额外挂载目录，如何把前端静态文件直接打包进 Go 编译出的单文件二进制里由 Gin 提供访问？",
		goldAnswer: "使用 Go 1.16 引入的 embed（如 //go:embed static/* 声明 embed.FS 变量），再配合 r.StaticFS(\"/assets\", http.FS(staticFS)) 提供服务。",
		document:   "gin-static-files.md", headingPath: "Gin 静态文件服务 > 单文件与嵌入文件",
		snippet: "Go 1.16 引入的 embed 可以把静态文件编译进二进制，配合 StaticFS 使用",
	},
	{
		question:   "调用 sql.Open 没有返回错误，能说明数据库已经连接成功吗？如何验证连通性？",
		goldAnswer: "不能。sql.Open 只是建立连接池配置，不会真正连接；需要调用 db.Ping() 验证连通性。",
		document:   "go-database-sql.md", headingPath: "Go database/sql 操作 MySQL > 打开连接",
		snippet: "sql.Open 只是建立连接池配置，不会真正连接；用 db.Ping() 验证连通性。",
	},
	{
		question:   "编写公共库时，遇到一般异常应该直接终止流程还是交给调用方处理？捕获崩溃的调用写在哪里才有效？",
		goldAnswer: "普通错误不要 panic，库代码通常不应 panic 而应返回 error 交给调用方处理；recover 只能在 defer 函数中捕获 panic。",
		document:   "go-effective-errors.md", headingPath: "Go 错误处理惯例（Effective Go） > panic 与 recover",
		snippet: "库代码通常不应 panic，而应返回 error。",
	},
	{
		question:   "在 Go 中为什么推荐用 filepath.Join 来拼接文件路径，而不是手动用斜杠拼接？",
		goldAnswer: "filepath.Join 在拼接路径时会自动处理平台分隔符，从而兼容不同操作系统。",
		document:   "go-file-io.md", headingPath: "Go 文件与 IO > 目录操作",
		snippet: "filepath.Join 拼路径（自动处理平台分隔符）。",
	},
	{
		question:   "使用 os.Exit(1) 强制退出进程时，函数里用 defer 注册的清理代码还会运行吗？",
		goldAnswer: "不会运行。os.Exit(1) 会立即退出且不执行 defer。",
		document:   "go-flag-cli.md", headingPath: "Go 命令行与 flag 包 > 退出",
		snippet: "os.Exit(1) 立即退出且不执行 defer",
	},
	{
		question:   "Go 模块主版本号升级到 v2 及以上时，模块路径必须遵守什么约定？",
		goldAnswer: "主版本 ≥2 时，模块路径必须以 /v2 等主版本后缀结尾（语义化导入版本规则）。",
		document:   "go-modules.md", headingPath: "Go 包与模块管理 > 语义化版本",
		snippet: "主版本 ≥2 时模块路径必须以 /v2 等后缀结尾",
	},
	{
		question:   "为什么在 Go 函数里把局部变量的指针返回给调用方是安全的？",
		goldAnswer: "Go 指针安全且有垃圾回收，返回局部变量的地址是合法的，变量会自动逃逸到堆上。",
		document:   "go-pointers.md", headingPath: "Go 指针 > 与 C 的区别",
		snippet: "返回局部变量的地址是合法的（变量会逃逸到堆上）。",
	},
	{
		question:   "Go 语言中如果传入非法正则表达式，regexp.MustCompile 会返回什么值？",
		goldAnswer: "regexp.MustCompile 编译正则失败时会直接 panic，不会返回 error；需要返回 error 时应使用 regexp.Compile。",
		document:   "go-regex.md", headingPath: "Go regexp 正则 > 编译",
		snippet: "regexp.MustCompile 编译正则，失败时 panic",
	},
	{
		question:   "对切片排序时希望相等元素保持原来的前后顺序，应该调用 sort 包的哪个函数？",
		goldAnswer: "应调用 sort.SliceStable，它是稳定排序，会让相等元素保持原顺序。",
		document:   "go-sort.md", headingPath: "Go sort 排序 > 自定义排序",
		snippet: "sort.SliceStable 是稳定排序（相等元素保持原顺序）。",
	},
	{
		question:   "为什么说在 Go 里遍历带中文的字符串时不能只用 len(s) 当作字符数？",
		goldAnswer: "Go 字符串是按 UTF-8 编码的只读字节序列，len(s) 返回字节数而不是字符数；逐个字符遍历应使用 for range 获取 rune（Unicode 码点）。",
		document:   "go-strings-fmt.md", headingPath: "Go 字符串与 fmt 格式化 > strings 包",
		snippet: "len(s) 返回字节数而不是字符数。",
	},
	{
		question:   "Go 语言不支持类继承，怎样让一个结构体自动拥有另一个结构体的属性和行为？",
		goldAnswer: "通过结构体嵌套（嵌入）实现组合复用：将目标类型作为嵌入字段放入外层结构体，其字段和方法会被“提升”到外层结构体直接访问。",
		document:   "go-structs-methods.md", headingPath: "Go 结构体与方法 > 组合代替继承",
		snippet: "嵌入类型的字段和方法会被\"提升\"到外层结构体",
	},
	{
		question:   "Go 解析和格式化时间用的参考模板时间点是哪一个？",
		goldAnswer: "Mon Jan 2 15:04:05 MST 2006（即 01/02 03:04:05PM '06 -0700，常用格式如 \"2006-01-02 15:04:05\"）。",
		document:   "go-time.md", headingPath: "Go time 包 > 格式化",
		snippet: "Mon Jan 2 15:04:05 MST 2006",
	},
	{
		question:   "在 Go 常量组里如何自动生成从 0 递增的枚举值？",
		goldAnswer: "在 const 常量组中使用 iota，它会从 0 开始自增。",
		document:   "go-tour-basics.md", headingPath: "Go 基础语法 > 控制流",
		snippet: "iota 在常量组中从 0 开始自增，常用于枚举。",
	},
	{
		question:   "在 Go 中，应该由哪一端关闭 channel？用 for v := range ch 遍历 channel 会在什么时候结束？",
		goldAnswer: "只有发送方才应关闭 channel；for v := range ch 会持续接收直到 channel 被关闭。",
		document:   "go-tour-concurrency.md", headingPath: "Go 并发：goroutine 与 channel > select",
		snippet: "只有发送方才应关闭 channel。",
	},
	{
		question:   "在 Go 中 map 的零值是什么？刚声明但没用 make 初始化的 map 能直接写入数据吗？",
		goldAnswer: "map 的零值是 nil；不能直接写入，必须先用 make 初始化后才能写入。",
		document:   "go-tour-slices.md", headingPath: "Go 数组、切片与 map > map",
		snippet: "map 的零值是 nil，必须用 make 初始化后才能写入。",
	},
	{
		question:   "结构体方法要修改内部字段时，接收者应该写成哪种形式？",
		goldAnswer: "应该写成指针接收者；值接收者操作的是副本，无法修改原结构体内容。",
		document:   "go-tour-structs.md", headingPath: "Go 结构体与方法 > 方法",
		snippet: "指针接收者才能修改结构体内容；值接收者操作的是副本。",
	},
	{
		question:   "为什么 Go 里把 int 和 float64 直接相加会编译失败？",
		goldAnswer: "Go 不会做隐式类型转换，不同类型的值之间运算必须先用 T(v) 显式转换成同一类型。",
		document:   "go-variables-types.md", headingPath: "Go 变量与基本类型 > 类型转换",
		snippet: "把 int 和 float64 直接相加会编译报错，必须先转成同一类型。",
	},
	{
		question:   "在 Gin 中怎么让多条路由或整个服务复用同一段前置处理逻辑？",
		goldAnswer: "使用中间件：通过 r.Use() 注册全局中间件对所有路由生效，或通过 r.Group(..., 中间件) 为路由组挂载中间件，在中间件 c.Next() 之前执行前置逻辑。",
		document:   "gin-middleware.md", headingPath: "Gin 中间件 > 全局中间件",
		snippet: "`r.Use()` 注册的中间件对该引擎上的所有路由生效：",
	},
	{question: "Gin 的 Hot Reload（热重载）怎么做？", goldAnswer: "文档中没有依据", shouldRefuse: true},
	{question: "Gin 怎么接入 Swagger 自动生成 API 文档？", goldAnswer: "文档中没有依据", shouldRefuse: true},
	{question: "Go 怎么用 sync.Pool 复用临时对象来减少 GC 压力？", goldAnswer: "文档中没有依据", shouldRefuse: true},
	{question: "Go 1.22 的 range over func 自定义迭代器怎么写？", goldAnswer: "文档中没有依据", shouldRefuse: true},
	{question: "Gin 怎么使用 r.RunTLS 直接传入证书和私钥文件开启 HTTPS？", goldAnswer: "文档中没有依据", shouldRefuse: true},
}

func main() {
	review := flag.Bool("review", false, "只解析真实 chunk_id 并打印增量题审阅表，不写入数据库")
	flag.Parse()

	if len(stage6Questions) != 27 {
		log.Fatalf("阶段 6 增量金标题应为 27 条，当前为 %d", len(stage6Questions))
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

	questions := make([]store.EvalQuestion, 0, len(stage6Questions))
	answerable, refusals := 0, 0
	for i, item := range stage6Questions {
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
	if answerable != 22 || refusals != 5 {
		log.Fatalf("应为 22 条作答题、5 条拒答题，当前为 %d、%d", answerable, refusals)
	}

	if *review {
		fmt.Println("# 阶段 6 增量 27 条金标题审阅表")
		fmt.Println("| 序号 | should_refuse | 问题 | gold_chunk_id | 来源 |")
		fmt.Println("|---|---|---|---|---|")
		for i, item := range stage6Questions {
			idText := "NULL"
			src := "—"
			if q := questions[i]; q.GoldChunkID != nil {
				idText = fmt.Sprintf("%d", *q.GoldChunkID)
				src = item.document + " · " + item.headingPath
			}
			fmt.Printf("| %d | %t | %s | %s | %s |\n",
				i+26, item.shouldRefuse, strings.ReplaceAll(item.question, "|", "&#124;"), idText, src)
		}
		return
	}

	inserted, err := st.AppendEvalQuestions(questions)
	if err != nil {
		log.Fatal("阶段 6 增量金标题入库失败：", err)
	}
	log.Printf("阶段 6 增量金标题入库完成：本次新增 %d 条（共校验 %d 条，22 作答 + 5 拒答）", inserted, len(questions))
}
