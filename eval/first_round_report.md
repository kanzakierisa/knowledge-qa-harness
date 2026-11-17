# 本批评测结果

| 指标 | 比例 |
|---|---|
| 检索命中率 | 100.00% |
| 引用准确率 | 100.00% |
| 答案可用率 | 100.00% |
| 拒答正确率 | 100.00% |

共 25 题（可作答 18 题，误拒 0 题）；系统非拒答 18 条；人工已标注 18 条、待标注 0 条。答案可用率按已标注回答计算，未标注时为 NULL。

| run_id | 问题 | 应拒答 | gold_chunk_id | 检索 Top 5 | 实际拒答 | hit | citation_ok | usable | 回答 |
|---|---|---|---|---|---|---|---|---|---|
| 1 | 在 Gin 中，如何让服务只监听本机回环地址的 8080 端口？ | false | 325 | [325,323,344,303,345] | false | true | true | true | 根据资料 [1]，可以显式指定地址为 `127.0.0.1:8080` 来实现只监听本机回环地址的 8080 端口，示例为 `r.Run("127.0.0.1:8080")` [1]。 |
| 2 | 在 v1 := r.Group("/v1") 组内注册 v1.GET("/users", listUsersV1) 后，最终请求路径是什么？ | false | 337 | [337,335,313,312,330] | false | true | true | true | 最终路径是组前缀加组内路径，即 `/v1/users` [1]。 |
| 3 | Gin 的 * 通配符参数能匹配哪些路径内容，在位置上有什么限制？ | false | 332 | [332,338,330,331,333] | false | true | true | true | `*` 通配符匹配剩余所有路径（可以包含斜杠）[1]。它只能出现在路径末尾 [1]。 |
| 4 | 在读取查询参数时，c.GetQuery 如何帮助区分“没传”和“传了空值”？ | false | 320 | [320,317,319,334,397] | false | true | true | true | `c.GetQuery` 多返回一个 bool 表示参数是否存在，从而区分“没传”和“传了空值” [1]。示例中通过 `name, exists := c.GetQuery("name")` 获取参数，并用 `if !exists` 判断参数不存在时返回错误 [1]。 |
| 5 | Gin 中带 Should 前缀的绑定方法与不带 Should 的 c.BindJSON 在出错时有何区别？为什么生产代码建议使用 Should 系列？ | false | 291 | [291,289,292,309,290] | false | true | true | true | 带 Should 前缀的方法（ShouldBindJSON、ShouldBindQuery、ShouldBind）出错时只返回 error，由你自己决定响应 [1]。不带 Should 的 `c.BindJSON` 等方法出错时会自动写 400 响应并调用 Abort，处理函数仍会继续执行，容易造成二次写响应 [1]。因此生产代码一律建议用 Should 系列 [1]。 |
| 6 | 在自定义 Gin 中间件中，c.Next() 前后的代码分别属于什么阶段？调用 c.Abort() 有什么效果？ | false | 311 | [311,304,310,301,312] | false | true | true | true | `c.Next()` 之前是"请求阶段"，之后是"响应阶段" [1]。在中间件里调用 `c.Abort()` 可以阻止链继续往下走（处理函数不会执行），常用于鉴权失败 [1]。`c.Abort()` 阻止后续处理函数执行，但当前函数内的代码仍会继续，所以 Abort 之后通常紧跟 return [2]。 |
| 7 | Windows 的 Go 安装器会自动如何配置 PATH 环境变量？ | false | 400 | [400,403,404,401,379] | false | true | true | true | Windows 用户下载 .msi 安装程序，双击按向导完成即可，默认安装到 `C:\Program Files\Go`，安装器会自动把 Go 的 bin 目录加入 PATH 环境变量 [1]。 |
| 8 | 在 for i := 0; i < 5; i++ 循环中执行 defer fmt.Println(i)，会按什么顺序输出？ | false | 359 | [359,392,391,386,356] | false | true | true | true | 按后进先出（栈）的顺序执行 [1][4]，输出为 4 3 2 1 0 [1]。 |
| 9 | 在 Go 语言中，函数可变参数在类型前加什么符号？在函数内部它是什么类型，调用时如何把切片展开传入？ | false | 384 | [384,462,387,461,381] | false | true | true | true | 参数类型前加 `...` 表示可变参数 [1]。函数内它是一个切片 [1]。调用时可以用 `切片...` 展开传入 [1]。 |
| 10 | 一个接口值在什么条件下才是 nil？ | false | 407 | [407,460,409,368,375] | false | true | true | true | 一个接口值只有类型和值都为 nil 时才是 nil [1]。如果接口里存的是一个 nil 指针的具体类型，接口本身不是 nil [1]。 |
| 11 | main 函数返回时，尚未运行完的 goroutine 会怎样？ | false | 391 | [391,390,452,393,392] | false | true | true | true | main 函数返回时所有 goroutine 都会被强制结束，不管有没有跑完 [1]。 |
| 12 | 当 select 等待的多个 channel 操作同时就绪时，会选择执行哪一个？ | false | 349 | [349,454,354,351,455] | false | true | true | true | 当 select 等待的多个 channel 操作同时就绪时，会随机选一个执行 [1][2]。 |
| 13 | 将结构不确定的 JSON 解析到 map[string]interface{} 时，数字会变成什么类型，之后需要如何处理？ | false | 415 | [415,413,333,294,412] | false | true | true | true | 数字会变成 float64 [1]，需要自己类型断言 [1]。 |
| 14 | 调用 http.ListenAndServe 时，第二个参数传 nil 表示什么？ | false | 396 | [396,325,407,424,338] | false | true | true | true | 第二个参数传 nil 表示使用默认的 DefaultServeMux [1]。 |
| 15 | context.WithCancel 返回的 cancel 函数有什么作用，为什么使用后必须调用它？ | false | 353 | [353,355,352,354,383] | false | true | true | true | cancel() 用于通知所有监听者退出 [1]。使用后必须调用 cancel 以防止泄漏 [1]。 |
| 16 | 如何只运行 TestAdd 中名为“正数”的子测试？ | false | 442 | [442,441,440,443,445] | false | true | true | true | 只跑某个子测试用 `go test -run TestAdd/正数` [1]。 |
| 17 | 为什么判断错误链上是否有某个哨兵错误时应使用 errors.Is，而不要直接使用 ==？ | false | 370 | [370,366,371,300,368] | false | true | true | true | 判断错误链上是否有某个哨兵错误应使用 `errors.Is`，不要直接用 `==`，因为错误可能被包装过 [1]。`errors.New` 创建简单错误，`fmt.Errorf` 配合 `%w` 包装错误以保留错误链 [2]。不要再用 `==` 直接比较包装过的错误 [2]。 |
| 18 | 在泛型约束 ~int &#124; ~float64 中，~ 和 &#124; 分别表示什么？ | false | 387 | [387,389,465,388,450] | false | true | true | true | ~ 表示底层类型匹配 [1]，&#124; 表示并集 [1]。 |
| 19 | 今天天气怎么样？ | true | NULL | [446,296,333,447,294] | true | NULL | NULL | NULL | 文档中没有依据 |
| 20 | Gin 怎么做 WebSocket 长连接？ | true | NULL | [325,323,295,335,322] | true | NULL | NULL | NULL | 文档中没有依据 |
| 21 | Gin 作者最喜欢的颜色是什么？ | true | NULL | [383,310,324,373,322] | true | NULL | NULL | NULL | 文档中没有依据 |
| 22 | Go 语言作者早餐爱吃什么？ | true | NULL | [373,382,390,464,405] | true | NULL | NULL | NULL | 文档中没有依据 |
| 23 | Gin 如何自动申请和续期 Let's Encrypt 证书？ | true | NULL | [331,330,292,311,332] | true | NULL | NULL | NULL | 文档中没有依据 |
| 24 | Go 1.27 新增了哪些语法特性？ | true | NULL | [441,451,444,449,450] | true | NULL | NULL | NULL | 文档中没有依据 |
| 25 | 怎么做法式红酒炖牛肉？ | true | NULL | [325,295,335,317,298] | true | NULL | NULL | NULL | 文档中没有依据 |
