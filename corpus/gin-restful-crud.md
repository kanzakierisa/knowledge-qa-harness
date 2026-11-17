# Gin 写 RESTful API 的惯例

## 资源式路由

RESTful 用名词表示资源，用 HTTP 方法表达动作：

```
GET    /users      列表
GET    /users/:id  详情
POST   /users      新建
PUT    /users/:id  全量更新
PATCH  /users/:id  部分更新
DELETE /users/:id  删除
```

## 统一响应结构

常见做法是统一 {code, message, data} 包装，业务码与 HTTP 状态码配合使用。

```
c.JSON(200, gin.H{"code": 0, "message": "ok", "data": user})
```

## 状态码惯例

200 成功，201 创建成功，400 参数错误，401 未认证，403 无权限，404 资源不存在，500 服务器内部错误。

## 参数校验

结构体 tag 配合 ShouldBindJSON 做校验：binding:"required"、binding:"min=6"、binding:"email"。校验失败返回 400 并带上字段错误信息。
