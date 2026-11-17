# Go 并发：channel

> 内容整理自 go.dev 官方教程《A Tour of Go》的 Concurrency 章节。

## channel 的创建与收发

channel 是带类型的管道，用 `make` 创建，用 `<-` 操作符收发：

```go
ch := make(chan int) // 创建 int 类型的 channel

go func() {
    ch <- 42 // 发送：把值放进 channel
}()

v := <-ch // 接收：从 channel 取出值
fmt.Println(v)
```

默认情况下收发都是**阻塞**的：发送方等到有接收方就绪才完成，接收方等到有值可读才继续。这种同步机制让 goroutine 之间无需显式加锁就能安全交换数据。Go 的格言是：不要通过共享内存来通信，而要通过通信来共享内存。

## 带缓冲的 channel

创建时给出缓冲容量，缓冲未满时发送不阻塞，缓冲取空后接收才阻塞：

```go
ch := make(chan int, 100) // 容量 100

ch <- 1 // 缓冲没满，立即返回
ch <- 2
fmt.Println(<-ch) // 1
fmt.Println(<-ch) // 2
```

缓冲写满后再发送会阻塞，可能造成死锁，容量选择要合理。

## close 与 range

发送方可以关闭 channel 表示"不会再有值了"：

```go
close(ch)
```

接收方用双返回值判断是否已关闭且取尽：

```go
v, ok := <-ch
if !ok {
    // channel 已关闭且没有剩余值
}
```

用 range 可以一直接收到 channel 关闭为止：

```go
for v := range ch {
    fmt.Println(v)
}
```

注意：**只有发送方应该关闭 channel**，接收方关闭会向发送方制造 panic；向已关闭的 channel 发送也会 panic。

## select 多路复用

`select` 让 goroutine 同时等待多个 channel 操作，哪个就绪执行哪个；多个同时就绪时随机选一个：

```go
select {
case v := <-ch1:
    fmt.Println("从 ch1 收到", v)
case ch2 <- 10:
    fmt.Println("向 ch2 发送成功")
default:
    fmt.Println("都没有就绪，不阻塞")
}
```

default 分支让 select 变成非阻塞轮询。select 配合 time.After 可以实现超时控制：

```go
select {
case v := <-ch:
    fmt.Println(v)
case <-time.After(time.Second):
    fmt.Println("超时了")
}
```

## 单向 channel

函数签名里可以把 channel 声明为只发或只收，编译器会强制约束：

```go
func producer(out chan<- int) { // 只能发送
    out <- 1
}

func consumer(in <-chan int) { // 只能接收
    fmt.Println(<-in)
}
```

## 常见模式：worker pool

多个 goroutine 从同一个 jobs channel 取任务、往同一个 results channel 写结果，是 channel 的经典用法：

```go
jobs := make(chan int, 100)
results := make(chan int, 100)

for w := 0; w < 3; w++ {
    go func() {
        for j := range jobs {
            results <- j * 2
        }
    }()
}

for j := 0; j < 5; j++ {
    jobs <- j
}
close(jobs) // 所有任务发完，关闭通知 worker
```
