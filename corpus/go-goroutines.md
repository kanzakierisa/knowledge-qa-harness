# Go 并发：goroutine

> 内容整理自 go.dev 官方教程《A Tour of Go》的 Concurrency 章节。

## 什么是 goroutine

goroutine 是 Go 运行时管理的轻量级线程。启动一个 goroutine 只需在函数调用前加 `go` 关键字：

```go
go f(x, y, z) // 在新 goroutine 中执行 f，当前 goroutine 继续往下走
```

goroutine 的初始栈很小（只有几 KB），可以按需增长，因此一个程序轻松运行成千上万个 goroutine。它和操作系统线程不是一回事：Go 运行时把大量 goroutine 调度到少量系统线程上执行。

## 一个完整的例子

```go
package main

import (
    "fmt"
    "time"
)

func say(s string) {
    for i := 0; i < 3; i++ {
        time.Sleep(100 * time.Millisecond)
        fmt.Println(s)
    }
}

func main() {
    go say("world") // 并发执行
    say("hello")    // 当前 goroutine 执行
}
```

两个 say 交替输出。注意：main 函数返回时所有 goroutine 都会被强制结束，不管有没有跑完。

## 等待 goroutine 结束：sync.WaitGroup

要等待一组 goroutine 完成，标准做法是 `sync.WaitGroup`：Add 计数、每个 goroutine 结束时 Done、主函数 Wait：

```go
var wg sync.WaitGroup

for i := 0; i < 5; i++ {
    wg.Add(1)
    go func(id int) {
        defer wg.Done()
        fmt.Println("worker", id)
    }(i)
}
wg.Wait() // 阻塞直到计数归零
```

循环里启动 goroutine 时要把循环变量作为参数传进去，否则所有 goroutine 可能共享同一个变量值（Go 1.22 起循环变量每次迭代新建，此问题已缓解，但显式传参仍是清晰写法）。

## 共享内存与互斥锁

goroutine 之间通信首选 channel，但有时直接共享内存更简单。这时必须用 `sync.Mutex` 保护临界区，否则会产生数据竞争：

```go
type SafeCounter struct {
    mu sync.Mutex
    v  map[string]int
}

func (c *SafeCounter) Inc(key string) {
    c.mu.Lock()
    defer c.mu.Unlock() // defer 保证函数返回前一定解锁
    c.v[key]++
}

func (c *SafeCounter) Value(key string) int {
    c.mu.Lock()
    defer c.mu.Unlock()
    return c.v[key]
}
```

Go 竞态检测器可以发现数据竞争：`go run -race main.go` 或 `go test -race`。

## sync.Once 与单次执行

`sync.Once` 保证某个函数只执行一次，无论多少 goroutine 调用，常用于懒初始化：

```go
var once sync.Once
var config *Config

func GetConfig() *Config {
    once.Do(func() {
        config = loadConfig()
    })
    return config
}
```

## goroutine 泄漏

启动了却永远无法退出的 goroutine 叫泄漏，会持续占用内存。常见原因是 goroutine 阻塞在永远不会就绪的 channel 收发上。避免方法：给阻塞操作加超时、用 context 传递取消信号、确保 channel 的发送方最终会关闭或停止发送。
