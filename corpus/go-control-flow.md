# Go 控制流：for、if、switch

> 内容整理自 go.dev 官方教程《A Tour of Go》的 Flow control 章节。

## for 循环

Go 只有一种循环结构：`for`。它的三个部分用分号隔开，但**条件表达式不需要括号**，循环体的大括号是必须的：

```go
sum := 0
for i := 0; i < 10; i++ {
    sum += i
}
```

初始化语句和后置语句是可选的，去掉分号后 for 就等价于其他语言的 while：

```go
sum := 1
for sum < 1000 {
    sum += sum
}
```

如果连条件也省略，就是无限循环，用 `break` 退出：

```go
for {
    // 无限循环，直到 break 或 return
}
```

## if 语句

if 同样不需要括号，大括号必须：

```go
if x < 0 {
    return -x
}
```

if 可以在条件之前写一条短语句，声明的变量只在 if 及其 else 块内有效：

```go
if v := math.Pow(x, n); v < lim {
    return v
} else {
    fmt.Println(v) // v 在 else 里也能用
}
```

## switch 语句

Go 的 switch 比 C 家族简洁：case 不需要常量，匹配后**自动 break**，不会穿透到下一个 case：

```go
switch os := runtime.GOOS; os {
case "darwin":
    fmt.Println("macOS")
case "linux":
    fmt.Println("Linux")
default:
    fmt.Println(os)
}
```

case 从上到下求值，命中即停。需要穿透时显式写 `fallthrough`，但很少使用。

没有条件的 switch 等价于 `switch true`，是写长 if-else 链的清爽替代：

```go
switch {
case t.Hour() < 12:
    fmt.Println("早上好")
case t.Hour() < 17:
    fmt.Println("下午好")
default:
    fmt.Println("晚上好")
}
```

## defer 延迟调用

`defer` 把函数调用推迟到外层函数返回之前执行，常用于释放资源：

```go
f, err := os.Open("data.txt")
if err != nil {
    return err
}
defer f.Close() // 函数无论从哪里返回都会执行
```

defer 的参数在 defer 语句处就完成求值，只是调用被延迟。多个 defer 按**后进先出**（栈）的顺序执行：

```go
for i := 0; i < 5; i++ {
    defer fmt.Println(i) // 输出 4 3 2 1 0
}
```

## goto 与标签

Go 保留了 goto 和标签，但官方风格指南不鼓励使用。日常代码里用 for、if、switch、defer 已经足够表达所有控制流。
