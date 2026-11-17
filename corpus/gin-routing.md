# Gin 路由

> 内容整理自 gin-gonic.com 官方文档《Routing》《Using HTTP method》。

## 基本路由方法

Gin 为每种 HTTP 方法提供同名注册函数：GET、POST、PUT、PATCH、DELETE、HEAD、OPTIONS：

```go
r := gin.Default()
r.GET("/users", listUsers)
r.POST("/users", createUser)
r.PUT("/users/:id", updateUser)
r.DELETE("/users/:id", deleteUser)
```

每个方法接收路径和一个或多个处理函数，处理函数签名是 `func(*gin.Context)`。

## Any 与 NoRoute

`r.Any` 把同一个处理函数注册到所有 HTTP 方法：

```go
r.Any("/health", func(c *gin.Context) {
    c.String(200, "ok")
})
```

`r.NoRoute` 设置没有匹配路由时的处理函数，常用来统一 404 响应：

```go
r.NoRoute(func(c *gin.Context) {
    c.JSON(404, gin.H{"code": 404, "message": "页面不存在"})
})
```

## 路由组

`Group` 把拥有共同前缀的路由归为一组，还能整组挂中间件：

```go
v1 := r.Group("/v1")
{
    v1.GET("/users", listUsersV1)
    v1.POST("/users", createUserV1)
}

v2 := r.Group("/v2")
{
    v2.GET("/users", listUsersV2)
}
```

最终路径是组前缀加组内路径，如 `/v1/users`。路由组可以嵌套：`admin := v1.Group("/admin")`。

## 路由冲突规则

Gin 使用 httprouter 风格的基数树路由。同一个位置只能注册一种参数名，例如 `/user/:id` 和 `/user/:name` 冲突，注册第二个会 panic。通配符 `*` 和具体段位也有优先级限制，设计 API 路径时保持一致风格可以避免这类冲突。

## 常用请求方法对照表

RESTful 风格里方法与语义的习惯搭配：

- GET：查询资源，不应该有副作用。
- POST：创建资源。
- PUT：全量更新资源。
- PATCH：部分更新资源。
- DELETE：删除资源。

Gin 不强制语义，但遵循惯例让 API 更易理解。

## 自定义 HTTP 方法

`r.Handle(method, path, handlers...)` 可以注册任意自定义方法名：

```go
r.Handle("REPORT", "/stats", reportHandler)
```
