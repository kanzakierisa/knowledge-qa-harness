# Gin 静态文件服务

> 内容整理自 gin-gonic.com 官方文档《Serving static files》。

## 服务整个目录

`r.Static` 把一个 URL 前缀映射到本地目录，目录下的文件都能通过 URL 访问：

```go
r.Static("/assets", "./static")
```

此后 `./static/css/style.css` 可以通过 `/assets/css/style.css` 访问。Static 适合放 CSS、JS、图片等前端资源。

## StaticFS 与 StaticFile

`r.StaticFS` 用自定义的 http.FileSystem 提供服务，可以配合 embed 等虚拟文件系统：

```go
r.StaticFS("/more_static", http.Dir("my_file_system"))
```

`r.StaticFile` 只暴露单个文件，常用于 favicon 这类固定资源：

```go
r.StaticFile("/favicon.ico", "./resources/favicon.ico")
```

## 单文件与嵌入文件

Go 1.16 引入的 embed 可以把静态文件编译进二进制，配合 StaticFS 使用：

```go
//go:embed static/*
var staticFS embed.FS

func main() {
    r := gin.Default()
    r.StaticFS("/assets", http.FS(staticFS))
    r.Run()
}
```

这样部署时只需要一个可执行文件，不用带上静态目录。

## 首页与模板

静态服务只处理文件下载。要渲染 HTML 页面，用 `r.LoadHTMLGlob` 加载模板后用 c.HTML 渲染：

```go
r.LoadHTMLGlob("templates/*")
r.GET("/", func(c *gin.Context) {
    c.HTML(200, "index.tmpl", gin.H{"title": "首页"})
})
```

模板使用 Go 标准库 html/template 的语法：`{{ .title }}`。

## 注意事项

Static 注册的路径前缀不要和 API 路由冲突，例如都用 /api 开头会产生路由 panic（参数冲突）。生产环境流量大时，静态文件更推荐交给 Nginx 或 CDN，Gin 专注处理动态接口。Static 服务没有缓存控制头，需要缓存策略时自行用中间件设置 Cache-Control。
