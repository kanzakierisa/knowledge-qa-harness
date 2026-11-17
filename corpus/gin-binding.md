# Gin JSON 绑定与校验

> 内容整理自 gin-gonic.com 官方文档《Model binding and validation》《Bind JSON》。

## 绑定 JSON 请求体

`c.ShouldBindJSON` 把 JSON 请求体解码到结构体，字段用 json 标签对应：

```go
type CreateUserRequest struct {
    Name  string `json:"name" binding:"required"`
    Email string `json:"email" binding:"required,email"`
    Age   int    `json:"age"`
}

r.POST("/users", func(c *gin.Context) {
    var req CreateUserRequest
    if err := c.ShouldBindJSON(&req); err != nil {
        c.JSON(400, gin.H{"error": err.Error()})
        return
    }
    c.JSON(200, gin.H{"name": req.Name})
})
```

绑定失败（JSON 语法错、必填缺失、校验不通过）都会返回 error，务必检查。

## binding 校验标签

Gin 内置 go-playground/validator，常用校验规则：

- `required`：必填，零值视为缺失。
- `email`：必须是合法邮箱格式。
- `min=6`、`max=20`：字符串最小/最大长度，数字最小/最大值。
- `len=11`：长度必须恰好等于。
- `oneof=red green`：值必须在枚举集合内。
- `gte=0`、`lte=150`：大于等于、小于等于。

多个规则用逗号连写：`binding:"required,min=6,max=32"`。

## ShouldBind 与 Bind 的区别

带 Should 前缀的方法（ShouldBindJSON、ShouldBindQuery、ShouldBind）出错时只返回 error，由你自己决定响应。不带 Should 的 `c.BindJSON` 等方法出错时会自动写 400 响应并调用 Abort，处理函数仍会继续执行，容易造成二次写响应，生产代码一律建议用 Should 系列。

## 按 Content-Type 自动选择绑定器

`c.ShouldBind(&obj)` 根据请求的 Content-Type 自动选择 JSON、XML、表单或查询参数绑定器，一个接口想同时支持 JSON 和表单提交时很方便。GET 请求默认绑定查询参数，DELETE 跟随 GET 规则。

## 绑定嵌套结构与时间

结构体可以嵌套，JSON 层级与结构体层级对应。时间字段默认能解析 RFC3339 格式：

```go
type Order struct {
    ID        int       `json:"id"`
    CreatedAt time.Time `json:"created_at" time_format:"2006-01-02"`
}
```

需要自定义日期格式时用 `time_format` 标签指定 layout。

## 响应 JSON

处理成功后用 `c.JSON` 返回数据，响应结构体同样用 json 标签控制字段名：

```go
type UserResponse struct {
    ID   int    `json:"id"`
    Name string `json:"name"`
}

c.JSON(200, UserResponse{ID: 1, Name: "张三"})
```

标签里加 `omitempty` 可以让零值字段不出现在响应里：`json:"nickname,omitempty"`。
