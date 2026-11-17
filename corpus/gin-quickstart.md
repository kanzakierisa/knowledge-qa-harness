# Gin 安装与快速开始

> 内容整理自 gin-gonic.com 官方文档《Quickstart》《Installation》。

## 安装要求

Gin 要求 Go 1.16 及以上版本。先在项目目录初始化模块，再用 go get 安装 Gin：

```bash
go mod init example.com/demo
go get -u github.com/gin-gonic/gin
```

安装后在代码里导入：

```go
import "github.com/gin-gonic/gin"
```

执行 `go mod tidy` 可以把依赖写进 go.mod 并生成 go.sum。

## 第一个 Gin 程序

官方 Quickstart 的最小例子：创建一个默认引擎、注册一个 GET 路由、启动服务：

```go
package main

import "github.com/gin-gonic/gin"

func main() {
    r := gin.Default()
    r.GET("/ping", func(c *gin.Context) {
        c.JSON(200, gin.H{
            "message": "pong",
        })
    })
    r.Run() // 默认监听 :8080
}
```

运行 `go run .` 后访问 http://localhost:8080/ping ，返回 `{"message":"pong"}`。

## gin.Default 与 gin.New

`gin.Default()` 返回一个自带 Logger 和 Recovery 两个中间件的引擎实例，开发期最常用。`gin.New()` 返回不带任何中间件的空引擎，想完全自己控制中间件时用它，例如：

```go
r := gin.New()
r.Use(gin.Logger())
r.Use(gin.Recovery())
```

## 端口与监听地址

`r.Run()` 不传参数时监听 `:8080`。可以显式指定地址：

```go
r.Run(":3000")           // 所有网卡的 3000 端口
r.Run("127.0.0.1:8080")  // 只监听本机回环
```

Run 内部调用的是标准库的 http.ListenAndServe。需要更精细控制（读写超时、TLS）时，可以把 gin 引擎作为 Handler 塞进标准库的 http.Server：

```go
srv := &http.Server{
    Addr:    ":8080",
    Handler: r,
}
srv.ListenAndServe()
```

## 响应的常用写法

处理函数收到 `*gin.Context`，常用响应方法：

- `c.JSON(code, obj)`：返回 JSON，最常用。
- `c.String(code, format, args...)`：返回纯文本。
- `c.HTML(code, name, data)`：渲染 HTML 模板。
- `c.Data(code, contentType, bytes)`：返回任意字节。
- `c.Redirect(code, url)`：重定向。

`gin.H` 是 `map[string]interface{}` 的别名，用来快速拼 JSON 对象：

```go
c.JSON(200, gin.H{"code": 0, "data": gin.H{"name": "张三"}})
```
