# Go 并发：goroutine 与 channel

## goroutine

go f(x) 启动一个 goroutine，即轻量级线程，由 Go 运行时调度，初始栈很小且可伸缩。

```
go func() {
    fmt.Println("在后台运行")
}()
```

## channel

channel 是带类型的管道，用于 goroutine 之间传递数据，用 make(chan T) 创建。

```
ch := make(chan int)
ch <- 42      // 发送
v := <-ch     // 接收
```

默认发送和接收都会阻塞，直到另一端就绪。带缓冲的 channel make(chan int, 100) 在缓冲满之前不阻塞发送方。

## close 与 range

close(ch) 关闭 channel，表示不再发送。for v := range ch 可持续接收直到 channel 关闭。只有发送方才应关闭 channel。

## select

select 同时等待多个 channel 操作，哪个就绪执行哪个，多个同时就绪则随机选择。

```
select {
case v := <-ch1:
case ch2 <- 1:
default:
}
```

## sync 包

sync.Mutex 提供互斥锁，Lock/Unlock 保护共享数据。sync.WaitGroup 用于等待一组 goroutine 完成：Add 计数、Done 减一、Wait 阻塞等待归零。
