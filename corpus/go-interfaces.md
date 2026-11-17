# Go 接口

> 内容整理自 go.dev 官方教程《A Tour of Go》与《Effective Go》的 Interfaces 章节。

## 接口的定义

接口类型是一组方法签名的集合：

```go
type Abser interface {
    Abs() float64
}
```

任何类型只要实现了接口要求的全部方法，就**隐式**实现了该接口，不需要 implements 之类的声明。这是 Go 接口与其他语言最大的不同。

## 隐式实现

```go
type Vertex struct {
    X, Y float64
}

func (v Vertex) Abs() float64 {
    return math.Sqrt(v.X*v.X + v.Y*v.Y)
}

func main() {
    var a Abser
    v := Vertex{3, 4}
    a = v // Vertex 实现了 Abs，自动满足 Abser 接口
    fmt.Println(a.Abs())
}
```

隐式实现让接口的定义方和使用方解耦：接口可以定义在使用方的包里，不需要改动提供方的代码。

## 接口值与 nil

接口值由两部分组成：**具体类型**和**该类型的值**，可以理解为一个二元组 (type, value)。接口值调用方法时，会分派到具体类型的方法上。

一个接口值只有类型和值都为 nil 时才是 nil。如果接口里存的是一个 nil 指针的具体类型，接口本身不是 nil，调用方法时接收者会收到 nil 指针，方法内部需要自行判空。

## 空接口

`interface{}`（Go 1.18 起有别名 `any`）是没有任何方法的接口，**任何类型都实现了空接口**，因此可以装任意值：

```go
func describe(i interface{}) {
    fmt.Printf("(%v, %T)\n", i, i)
}
```

空接口常用于处理未知类型的数据，比如 JSON 解码到 map[string]interface{}。

## 类型断言

类型断言取出接口值底层保存的具体值：

```go
t := i.(string)      // 断言失败会 panic
t, ok := i.(string)  // 双返回值写法：失败时 ok 为 false，不 panic
```

需要按多个类型分支处理时用 type switch：

```go
switch v := i.(type) {
case string:
    fmt.Println("字符串", v)
case int:
    fmt.Println("整数", v)
default:
    fmt.Printf("未知类型 %T\n", v)
}
```

## 常用标准接口

`fmt.Stringer` 是最常见的接口之一，定义了 String 方法，fmt 包打印时会自动调用：

```go
type Stringer interface {
    String() string
}
```

error 接口同样只有一个方法：`Error() string`。小接口是 Go 的推荐风格——接口越小越通用，标准库大量接口只有一个方法。

## 接口的设计建议

《Effective Go》建议：接口应该在**使用方**定义，而不是在实现方预先定义一大套。只有当确实需要多种实现时才引入接口，否则直接用具体类型更简单。
