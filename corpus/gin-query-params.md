# Gin 查询参数

> 内容整理自 gin-gonic.com 官方文档《Querystring parameters》。

## 读取查询参数

查询参数是 URL `?` 后面的键值对，如 `/search?q=gin&page=2`。用 `c.Query` 读取，参数不存在时返回空字符串：

```go
r.GET("/search", func(c *gin.Context) {
    q := c.Query("q")
    page := c.Query("page")
    c.JSON(200, gin.H{"q": q, "page": page})
})
```

## 默认值 DefaultQuery

`c.DefaultQuery` 在参数不存在时返回指定默认值，适合分页这类可省略参数：

```go
r.GET("/users", func(c *gin.Context) {
    page := c.DefaultQuery("page", "1")
    size := c.DefaultQuery("size", "20")
    c.JSON(200, gin.H{"page": page, "size": size})
})
```

## 读取多个值

同一个键出现多次时（如 `?tag=go&tag=web`），用 `c.QueryArray` 取全部值：

```go
r.GET("/filter", func(c *gin.Context) {
    tags := c.QueryArray("tag") // []string{"go", "web"}
    c.JSON(200, gin.H{"tags": tags})
})
```

对应的 `c.DefaultQueryArray` 可以提供默认值。map 形式的查询参数（如 `?ids[a]=1&ids[b]=2`）用 `c.QueryMap("ids")` 读取。

## GetQuery 与存在性判断

`c.GetQuery` 多返回一个 bool 表示参数是否存在，区分"没传"和"传了空值"：

```go
name, exists := c.GetQuery("name")
if !exists {
    c.JSON(400, gin.H{"error": "缺少 name 参数"})
    return
}
```

## 数值转换

查询参数同样是字符串，数值参数要自己转换并处理错误：

```go
page, err := strconv.Atoi(c.DefaultQuery("page", "1"))
if err != nil || page < 1 {
    c.JSON(400, gin.H{"error": "page 参数不合法"})
    return
}
```

参数较多时更推荐用 c.ShouldBindQuery 把查询参数整体绑定到结构体，配合 binding 标签自动校验。
