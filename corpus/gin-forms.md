# Gin 表单处理

> 内容整理自 gin-gonic.com 官方文档《Form parameters》《Multipart form》。

## 读取表单字段

POST 请求 Content-Type 为 `application/x-www-form-urlencoded` 时，用 `c.PostForm` 读取字段，字段不存在返回空字符串：

```go
r.POST("/login", func(c *gin.Context) {
    username := c.PostForm("username")
    password := c.PostForm("password")
    c.JSON(200, gin.H{"username": username})
})
```

对应的有 `c.DefaultPostForm`（提供默认值）和 `c.GetPostForm`（返回存在性），用法与查询参数的 Query 系列一致。同名字段多个值用 `c.PostFormArray`，map 形式用 `c.PostFormMap`。

## 同时有查询参数和表单

PostForm 只读请求体里的表单字段，Query 只读 URL 上的查询参数，两者互不干扰，可以在同一个请求里混用：

```go
// POST /save?draft=true  body: title=hello&content=world
r.POST("/save", func(c *gin.Context) {
    draft := c.Query("draft")
    title := c.PostForm("title")
    c.JSON(200, gin.H{"draft": draft, "title": title})
})
```

## 文件上传（单文件）

`c.FormFile` 读取 multipart 表单中的文件，`c.SaveUploadedFile` 保存到本地：

```go
r.POST("/upload", func(c *gin.Context) {
    file, err := c.FormFile("file")
    if err != nil {
        c.JSON(400, gin.H{"error": "缺少文件"})
        return
    }
    c.SaveUploadedFile(file, "./uploads/"+file.Filename)
    c.JSON(200, gin.H{"filename": file.Filename, "size": file.Size})
})
```

默认 multipart 表单内存上限是 32MB，可用 `r.MaxMultipartMemory` 调整（超出部分落临时文件，不是拒绝请求）。

## 文件上传（多文件）

同一字段传多个文件时，从 `c.MultipartForm()` 里取文件列表：

```go
r.POST("/uploads", func(c *gin.Context) {
    form, _ := c.MultipartForm()
    files := form.File["files"]
    for _, f := range files {
        c.SaveUploadedFile(f, "./uploads/"+f.Filename)
    }
    c.JSON(200, gin.H{"count": len(files)})
})
```

## 表单绑定到结构体

字段多时用 `c.ShouldBind` 把表单整体绑定到结构体，配合 form 标签和校验规则：

```go
type LoginForm struct {
    Username string `form:"username" binding:"required"`
    Password string `form:"password" binding:"required,min=6"`
}

r.POST("/login", func(c *gin.Context) {
    var form LoginForm
    if err := c.ShouldBind(&form); err != nil {
        c.JSON(400, gin.H{"error": err.Error()})
        return
    }
    c.JSON(200, gin.H{"welcome": form.Username})
})
```

ShouldBind 会根据 Content-Type 自动选择绑定器：表单、JSON、XML 都能绑到同一个结构体。
