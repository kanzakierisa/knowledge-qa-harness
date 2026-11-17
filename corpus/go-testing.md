# Go 测试

> 内容整理自 go.dev 官方文档《Testing》《Tutorial: Add a test》与 testing 包文档。

## 第一个测试

测试代码写在与被测代码同目录、以 `_test.go` 结尾的文件里。测试函数以 `Test` 开头，参数是 `*testing.T`：

```go
// reverse.go 同目录的 reverse_test.go
package main

import "testing"

func TestReverse(t *testing.T) {
    got := Reverse("hello")
    want := "olleh"
    if got != want {
        t.Errorf("Reverse(%q) = %q, want %q", "hello", got, want)
    }
}
```

运行测试用 `go test`：

```bash
go test        # 当前包
go test ./...  # 递归所有包
go test -v     # 显示每个测试的名字和结果
```

t.Error 标记失败但继续执行后续代码；t.Fatal 标记失败并立即终止当前测试函数。

## 表驱动测试

Go 社区的主流风格是表驱动：把多组输入和期望放进切片，循环执行断言：

```go
func TestAdd(t *testing.T) {
    cases := []struct {
        a, b, want int
    }{
        {1, 2, 3},
        {0, 0, 0},
        {-1, 1, 0},
    }
    for _, c := range cases {
        if got := Add(c.a, c.b); got != c.want {
            t.Errorf("Add(%d, %d) = %d, want %d", c.a, c.b, got, c.want)
        }
    }
}
```

表驱动让新增用例只需加一行数据，配合 t.Run 还能给每组用例起名字，失败时精确定位。

## 子测试

`t.Run` 把一组断言包成命名子测试，可以单独运行、单独报告：

```go
for _, c := range cases {
    t.Run(c.name, func(t *testing.T) {
        if got := Add(c.a, c.b); got != c.want {
            t.Errorf("got %d, want %d", got, c.want)
        }
    })
}
```

只跑某个子测试：`go test -run TestAdd/正数`。

## 基准测试

以 `Benchmark` 开头的函数是基准测试，b.N 由 testing 框架自动调整：

```go
func BenchmarkReverse(b *testing.B) {
    for i := 0; i < b.N; i++ {
        Reverse("hello")
    }
}
```

运行 `go test -bench=.` 输出每次操作的耗时（ns/op）和内存分配，用于性能回归对比。

## 测试覆盖率

`go test -cover` 显示语句覆盖率：

```bash
go test -cover ./...
go test -coverprofile=cover.out ./...
go tool cover -html=cover.out # 浏览器里看哪些行没覆盖
```

覆盖率是参考指标而非目标本身，100% 覆盖不代表没有 bug。

## Example 示例函数

以 `Example` 开头的函数既是文档也是测试：如果函数体末尾有 `// Output:` 注释，go test 会校验实际输出是否一致：

```go
func ExampleReverse() {
    fmt.Println(Reverse("hello"))
    // Output: olleh
}
```

Example 会出现在 godoc/pkg.go.dev 文档页上，是写库文档的推荐方式。
