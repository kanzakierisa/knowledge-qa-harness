# Gin 中间件

> 内容整理自 gin-gonic.com 官方文档《Middleware》《Custom Middleware》。

## 什么是中间件

中间件是在请求到达业务处理函数前后执行的函数，签名与处理函数相同：`func(*gin.Context)`。多个中间件串成一条链，请求依次穿过每个中间件的前半段，到达处理函数后再逆序穿过每个中间件的后半段。`gin.Default()` 自带 Logger（记录请求日志）和 Recovery（捕获 panic 返回 500）两个中间件。

## 编写自定义中间件

中间件就是一个返回 gin.HandlerFunc 的普通函数。下面是一个统计请求耗时的例子：

```go
func CostTime() gin.HandlerFunc {
    return func(c *gin.Context) {
        start := time.Now()

        c.Next() // 执行链上后续的处理函数

        cost := time.Since(start)
        log.Printf("%s %s 耗时 %v", c.Request.Method, c.Request.URL.Path, cost)
    }
}
```

`c.Next()` 之前是"请求阶段"，之后是"响应阶段"。在中间件里调用 `c.Abort()` 可以阻止链继续往下走（处理函数不会执行），常用于鉴权失败。

## 全局中间件

`r.Use()` 注册的中间件对该引擎上的所有路由生效：

```go
r := gin.New()
r.Use(CostTime())
r.Use(gin.Recovery())
```

Use 的顺序就是执行顺序，先注册的先执行请求阶段。

## 路由组与单路由中间件

中间件也可以只挂在某个路由组或单个路由上：

```go
admin := r.Group("/admin", AuthRequired()) // 组内全部路由都要求登录
{
    admin.GET("/stats", statsHandler)
}

// 单个路由挂多个处理函数，中间件写在处理函数前面
r.GET("/profile", AuthRequired(), profileHandler)
```

## 在中间件与处理函数之间传值

`c.Set` / `c.Get` 在请求上下文里存取键值对，是中间件向处理函数传递数据的官方方式：

```go
func AuthRequired() gin.HandlerFunc {
    return func(c *gin.Context) {
        token := c.GetHeader("Authorization")
        if token == "" {
            c.AbortWithStatusJSON(401, gin.H{"error": "未登录"})
            return
        }
        c.Set("userID", parseUserID(token)) // 写入上下文
        c.Next()
    }
}

func profileHandler(c *gin.Context) {
    userID := c.MustGet("userID").(int) // 取出使用
    c.JSON(200, gin.H{"user_id": userID})
}
```

注意 c.Set 只在当前请求内有效，不要把它当全局变量用。

## Recovery 中间件的行为

Recovery 捕获处理链上的 panic，打印堆栈并返回 500，保证单个请求的崩溃不会打挂整个服务。gin.New() 不带它，生产环境务必自己加上：`r.Use(gin.Recovery()`。

## 常见第三方中间件

社区常用的有：cors（跨域）、gzip（响应压缩）、jwt（令牌鉴权）、rate limiter（限流）。它们都遵循同一个 gin.HandlerFunc 形态，用 Use 挂上即可。
