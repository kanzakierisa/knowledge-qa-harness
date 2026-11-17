# 语料切片统计（切片器 v2）

- 文档数：39
- 总段数：179
- 平均段长：221 字符
- 最长段：581 字符（gin-binding.md · Gin JSON 绑定与校验 > 绑定 JSON 请求体）
- 最短段：37 字符（go-generics.md · Go 泛型（Go 1.18+） > 使用建议）

## 按文档分布

| 文档 | 段数 | 总字符数 | 平均段长 |
|---|---|---|---|
| gin-binding.md | 6 | 1600 | 266 |
| gin-cookies.md | 5 | 1705 | 341 |
| gin-error-handling.md | 5 | 1504 | 300 |
| gin-forms.md | 5 | 1984 | 396 |
| gin-middleware.md | 7 | 1682 | 240 |
| gin-query-params.md | 5 | 1208 | 241 |
| gin-quickstart.md | 5 | 1478 | 295 |
| gin-restful-crud.md | 3 | 463 | 154 |
| gin-route-params.md | 5 | 1139 | 227 |
| gin-routing.md | 6 | 1212 | 202 |
| gin-static-files.md | 5 | 1074 | 214 |
| go-channels.md | 6 | 1718 | 286 |
| go-context.md | 4 | 538 | 134 |
| go-control-flow.md | 5 | 1359 | 271 |
| go-database-sql.md | 4 | 643 | 160 |
| go-effective-errors.md | 3 | 396 | 132 |
| go-errors.md | 6 | 1665 | 277 |
| go-file-io.md | 4 | 563 | 140 |
| go-flag-cli.md | 3 | 482 | 160 |
| go-functions.md | 6 | 1337 | 222 |
| go-generics.md | 3 | 377 | 125 |
| go-goroutines.md | 6 | 1746 | 291 |
| go-http-server.md | 4 | 636 | 159 |
| go-install.md | 5 | 1409 | 281 |
| go-interfaces.md | 7 | 1431 | 204 |
| go-json.md | 4 | 521 | 130 |
| go-modules.md | 7 | 1306 | 186 |
| go-pointers.md | 3 | 275 | 91 |
| go-regex.md | 3 | 406 | 135 |
| go-sort.md | 3 | 384 | 128 |
| go-strings-fmt.md | 2 | 475 | 237 |
| go-structs-methods.md | 6 | 1397 | 232 |
| go-testing.md | 6 | 1816 | 302 |
| go-time.md | 3 | 462 | 154 |
| go-tour-basics.md | 3 | 488 | 162 |
| go-tour-concurrency.md | 4 | 583 | 145 |
| go-tour-slices.md | 2 | 440 | 220 |
| go-tour-structs.md | 3 | 451 | 150 |
| go-variables-types.md | 7 | 1368 | 195 |

## 最长段预览（前 200 字符）

```
> 内容整理自 gin-gonic.com 官方文档《Model binding and validation》《Bind JSON》。

`c.ShouldBindJSON` 把 JSON 请求体解码到结构体，字段用 json 标签对应：

```go
type CreateUserRequest struct {
    Name  string `json:"name" binding:"r...
```
