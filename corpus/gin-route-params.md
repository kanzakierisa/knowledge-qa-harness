# Gin 路由参数

> 内容整理自 gin-gonic.com 官方文档《Parameters in path》。

## 路径参数 :name

路径中用 `:name` 声明一段参数，处理函数里用 `c.Param("name")` 取值：

```go
r.GET("/user/:name", func(c *gin.Context) {
    name := c.Param("name")
    c.String(200, "Hello %s", name)
})
```

请求 `/user/张三` 时 name 为"张三"。路径参数只匹配**一段**（不含斜杠）。

## 多段参数组合

一个路径里可以有多个参数，也可以和固定段混合：

```go
r.GET("/user/:name/*action", func(c *gin.Context) {
    name := c.Param("name")
    action := c.Param("action")
    c.String(200, "%s is %s", name, action)
})
```

## 通配符参数 *action

`*` 通配符匹配**剩余所有路径**（可以包含斜杠），且只能出现在路径末尾：

```go
r.GET("/files/*filepath", func(c *gin.Context) {
    filepath := c.Param("filepath") // 以 / 开头
    c.String(200, "file: %s", filepath)
})
```

请求 `/files/css/main.css` 时 filepath 为 `/css/main.css`。

## 参数的类型转换

c.Param 返回的永远是字符串，需要数字时自行转换：

```go
import "strconv"

r.GET("/users/:id", func(c *gin.Context) {
    id, err := strconv.Atoi(c.Param("id"))
    if err != nil {
        c.JSON(400, gin.H{"error": "id 必须是数字"})
        return
    }
    c.JSON(200, gin.H{"id": id})
})
```

这是新手常踩的坑：路径参数没有类型系统，校验和转换都要自己做。

## 与查询参数的区别

路径参数是 URL 路径的一部分，如 `/users/42`，适合标识资源本身；查询参数在 `?` 之后，如 `/users?page=2`，适合过滤、分页等可选条件。两者可以同时在一条路由里使用，查询参数用 c.Query 或 c.DefaultQuery 读取。
