# Go context 包

## 作用

context 用于跨 API 边界传递截止时间、取消信号和请求级数据。约定：函数的第一个参数是 ctx context.Context，不要存在结构体字段里。

## 创建

context.Background() 是根 context，通常在 main 或请求入口处调用。context.TODO() 用于还没决定用哪个的占位场景。

## 取消与超时

ctx, cancel := context.WithCancel(parent) 返回可取消的 context，调用 cancel() 通知所有监听者退出，使用后必须调用 cancel 防止泄漏。

ctx, cancel := context.WithTimeout(parent, 5*time.Second) 到时间自动取消；WithDeadline 指定绝对时间点。

## 监听取消

ctx.Done() 返回一个 channel，取消时关闭，常配合 select：

```
select {
case <-ctx.Done():
    return ctx.Err()
case result := <-work:
}
```

## 传值

context.WithValue 存请求级数据（如 trace id），key 应使用自定义类型避免冲突。不要用它传可选参数。
