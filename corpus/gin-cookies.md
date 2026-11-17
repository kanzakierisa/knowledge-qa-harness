# Gin Cookie 处理

> 内容整理自 gin-gonic.com 官方文档《Set and get a cookie》。

## 设置 Cookie

`c.SetCookie` 向客户端写 Cookie：

```go
r.GET("/login", func(c *gin.Context) {
    c.SetCookie(
        "session_id",      // 名称
        "abc123",          // 值
        3600,              // 有效期（秒），0 表示会话 Cookie
        "/",               // path：Cookie 生效的路径
        "localhost",       // domain：生效域名
        false,             // secure：true 时仅 HTTPS 发送
        true,              // httpOnly：true 时 JS 无法读取，防 XSS
    )
    c.JSON(200, gin.H{"msg": "cookie 已设置"})
})
```

httpOnly 设为 true 是安全基线，可以防止脚本窃取会话。

## 读取 Cookie

`c.Cookie` 读取请求带来的 Cookie，返回 (值, error)，不存在时 error 非空：

```go
r.GET("/profile", func(c *gin.Context) {
    sessionID, err := c.Cookie("session_id")
    if err != nil {
        c.JSON(401, gin.H{"error": "未登录"})
        return
    }
    c.JSON(200, gin.H{"session": sessionID})
})
```

## 删除 Cookie

删除 Cookie 的方法是把有效期设为负数：

```go
r.GET("/logout", func(c *gin.Context) {
    c.SetCookie("session_id", "", -1, "/", "localhost", false, true)
    c.JSON(200, gin.H{"msg": "已退出登录"})
})
```

name、path、domain 必须与设置时一致，浏览器才能正确匹配删除。

## 完整示例：登录态判断

把 Cookie 读写组合进中间件，就是一个简化的登录态检查：

```go
func AuthByCookie() gin.HandlerFunc {
    return func(c *gin.Context) {
        sessionID, err := c.Cookie("session_id")
        if err != nil || !validSession(sessionID) {
            c.AbortWithStatusJSON(401, gin.H{"error": "请先登录"})
            return
        }
        c.Set("session_id", sessionID)
        c.Next()
    }
}

r.GET("/dashboard", AuthByCookie(), func(c *gin.Context) {
    c.JSON(200, gin.H{"welcome": c.GetString("session_id")})
})
```

## 安全注意事项

Cookie 里不要直接存用户 ID、权限等敏感明文——客户端可以任意查看和篡改。标准做法是只存一个无意义的会话 ID，真实数据保存在服务端（内存或 Redis），用会话 ID 做索引。涉及安全场景时把 secure 设为 true 只走 HTTPS，并考虑 SameSite 属性防 CSRF。
