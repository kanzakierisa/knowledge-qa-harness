# Go 错误处理

> 内容整理自 go.dev 博客《Error handling and Go》与《Effective Go》的 Errors 章节。

## error 接口

Go 的错误就是一个实现了 error 接口的值：

```go
type error interface {
    Error() string
}
```

函数把 error 作为最后一个返回值返回，约定 error 为 nil 表示成功。调用方用 if 判断处理：

```go
f, err := os.Open("data.txt")
if err != nil {
    log.Fatal(err)
}
defer f.Close()
```

这种"显式返回 + 显式检查"是 Go 错误处理的核心风格，没有 try/catch。

## 创建错误

`errors.New` 用固定文本创建错误：

```go
import "errors"

var ErrNotFound = errors.New("record not found")
```

`fmt.Errorf` 支持格式化，可以带上上下文信息：

```go
return fmt.Errorf("open %s: %w", filename, err)
```

`%w` 动词表示包装错误，被包装的错误可以通过 errors.Unwrap、errors.Is、errors.As 逐层取出。

## errors.Is 与 errors.As

判断错误链上是否有某个哨兵错误用 `errors.Is`，不要直接用 ==（错误可能被包装过）：

```go
if errors.Is(err, os.ErrNotExist) {
    // 文件不存在
}
```

需要把错误转成具体类型取字段时用 `errors.As`：

```go
var pathErr *fs.PathError
if errors.As(err, &pathErr) {
    fmt.Println("出问题的路径：", pathErr.Path)
}
```

## 自定义错误类型

任何实现了 Error() 方法的类型都可以当错误用，携带更多上下文：

```go
type QueryError struct {
    Query string
    Err   error
}

func (e *QueryError) Error() string {
    return e.Query + ": " + e.Err.Error()
}

func (e *QueryError) Unwrap() error { return e.Err }
```

实现 Unwrap 方法可以让 errors.Is 和 errors.As 沿着错误链继续向下找。

## panic 与 recover

panic 用于真正不可恢复的错误（比如程序员写错了逻辑、索引越界），普通业务错误不要用 panic。panic 会让当前 goroutine 的调用栈逐层展开并执行 defer，最后程序崩溃。

`recover` 只能在 defer 函数里调用，可以捕获 panic 让程序恢复：

```go
func safeDo() (err error) {
    defer func() {
        if r := recover(); r != nil {
            err = fmt.Errorf("panic: %v", r)
        }
    }()
    // 可能 panic 的代码
    return nil
}
```

服务器程序通常在中间件里 recover 单个请求的 panic，避免一个请求打崩整个进程，Gin 的 Recovery 中间件就是这个思路。

## 错误处理的风格建议

《Effective Go》的建议：错误值得认真对待，不要用 `_` 随手丢弃 error；错误信息应该说明"做什么操作时出了什么错"；底层错误往上层传时逐层添加上下文（用 %w 包装），最终在程序边界统一记录日志。
