# Go 函数

> 内容整理自 go.dev 官方教程《A Tour of Go》与《Effective Go》的 Functions 章节。

## 函数的定义

函数用 `func` 关键字声明，参数类型写在参数名之后，返回值类型写在参数列表之后：

```go
func add(x int, y int) int {
    return x + y
}
```

相邻参数类型相同时可以合并，只写最后一个类型：

```go
func add(x, y int) int {
    return x + y
}
```

没有返回值的函数省略返回类型。函数是 Go 的一等公民：可以赋值给变量、作为参数传递、作为返回值返回。

## 多返回值

Go 函数可以返回任意数量的值，这是 Go 区别于多数语言的特点：

```go
func swap(x, y string) (string, string) {
    return y, x
}

func main() {
    a, b := swap("hello", "world")
    fmt.Println(a, b) // world hello
}
```

多返回值最常见的用法是"结果 + error"组合，调用方先检查 error 再使用结果。

## 命名返回值

返回值可以像变量一样命名，命名返回值在函数开头就初始化为零值：

```go
func split(sum int) (x, y int) {
    x = sum * 4 / 9
    y = sum - x
    return // 裸返回：自动返回 x 和 y
}
```

裸返回（naked return）只应在短函数中使用，长函数里用裸返回会损害可读性。命名返回值更大的价值是文档作用：看签名就知道每个返回值是什么意思。

## 可变参数

参数类型前加 `...` 表示可变参数，函数内它是一个切片：

```go
func sum(nums ...int) int {
    total := 0
    for _, n := range nums {
        total += n
    }
    return total
}
```

调用时可以直接传若干值，也可以用 `切片...` 展开传入：

```go
sum(1, 2, 3)
nums := []int{1, 2, 3, 4}
sum(nums...)
```

## 匿名函数与闭包

Go 支持匿名函数，可以捕获外层变量形成闭包：

```go
func adder() func(int) int {
    sum := 0
    return func(x int) int {
        sum += x // 捕获并修改外层变量 sum
        return sum
    }
}
```

闭包捕获的是变量本身而不是值的副本，adder 返回的函数拥有自己的 sum，多次调用会持续累加。

## defer 与函数

defer 语句把函数调用推迟到外层函数返回前执行，常用于关闭文件、释放锁等资源清理。多个 defer 按后进先出顺序执行。defer 声明时参数立即求值，调用延迟发生，这个细节在循环里使用 defer 时尤其要注意。
