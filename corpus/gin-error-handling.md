# Gin 错误处理

> 内容整理自 gin-gonic.com 官方文档《Error handling》与 Recovery 中间件文档。

## c.Error 收集错误

处理函数里遇到错误时，可以用 `c.Error(err)` 把错误附加到当前请求的上下文里，请求结束后由统一的中间件处理：

```go
r.GET("/users/:id", func(c *gin.Context) {
    user, err := findUser(c.Param("id"))
    if err != nil {
        c.Error(err) // 只记录，不终止
        c.JSON(404, gin.H{"error": "用户不存在"})
        return
    }
    c.JSON(200, user)
})
```

请求完成后，在后续中间件里通过 `c.Errors` 读取全部已记录的错误。

## 统一错误日志中间件

官方文档推荐的用法：写一个靠后执行的中间件，集中处理 c.Errors：

```go
func ErrorLogger() gin.HandlerFunc {
    return func(c *gin.Context) {
        c.Next() // 先让请求跑完

        for _, e := range c.Errors {
            log.Printf("请求 %s 出错：%v", c.Request.URL.Path, e.Err)
        }
    }
}

r.Use(ErrorLogger())
```

这样业务代码只管 c.Error 记录，日志格式和上报逻辑收敛在一处。

## 错误的类型标记

`c.Error` 返回的 *gin.Error 可以带类型和元数据，用于区分错误级别：

```go
c.Error(err).SetType(gin.ErrorTypePrivate) // 私有错误：不对外暴露细节
c.Error(err).SetType(gin.ErrorTypePublic)  // 公开错误：可以返回给客户端
c.Error(err).SetMeta(map[string]string{"trace_id": tid})
```

私有类型适合数据库错误等内部细节，公开类型适合参数校验失败这类应该告诉调用方的信息。

## panic 的恢复：Recovery

业务代码里 panic 会被 Recovery 中间件捕获，返回 500 并打印堆栈，进程不会退出。gin.Default() 自带 Recovery；用 gin.New() 时务必手动添加：

```go
r := gin.New()
r.Use(gin.Recovery())
```

需要自定义 panic 响应时用 `gin.CustomRecovery`：

```go
r.Use(gin.CustomRecovery(func(c *gin.Context, err any) {
    c.JSON(500, gin.H{"error": "服务器内部错误"})
}))
```

## 主动终止请求链

`c.Abort()` 阻止后续处理函数执行，但当前函数内的代码仍会继续，所以 Abort 之后通常紧跟 return。带响应的便捷方法：

```go
c.AbortWithStatus(401)
c.AbortWithStatusJSON(403, gin.H{"error": "没有权限"})
return
```

忘记写 return 是新手常见 bug：响应已经发出，代码却又执行了一遍后续逻辑。
