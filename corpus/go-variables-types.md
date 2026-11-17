# Go 变量与基本类型

> 内容整理自 go.dev 官方教程《A Tour of Go》的 Basics 章节。

## 变量的声明

`var` 语句声明一个变量，类型写在变量名之后：

```go
var name string
var age int
var ok bool
```

var 可以一次声明多个变量，也可以出现在函数内或包级别。声明时可以直接初始化：

```go
var i, j int = 1, 2
var s = "hello" // 有初始化值时类型可以省略，由编译器推导
```

## 短变量声明

在函数内部，可以用 `:=` 短声明代替 var，类型自动推导，这是最常用的写法：

```go
func main() {
    x := 42        // int
    pi := 3.14     // float64
    name := "gopher" // string
}
```

注意：`:=` 只能用在函数内部，包级别的变量必须用 var。

## 基本类型

Go 的基本类型分为几类：

- 布尔：`bool`
- 字符串：`string`
- 有符号整数：`int`、`int8`、`int16`、`int32`、`int64`
- 无符号整数：`uint`、`uint8`（即 byte）、`uint16`、`uint32`、`uint64`
- 浮点数：`float32`、`float64`
- 复数：`complex64`、`complex128`

`int` 的大小随平台而定，32 位系统上是 32 位，64 位系统上是 64 位。除非有明确的位数或大小要求，一般直接用 int。

## 零值

变量声明后没有显式初始化时，会被赋予该类型的零值：

- 数值类型零值是 `0`
- 布尔类型零值是 `false`
- 字符串零值是 `""`（空字符串）

```go
var i int     // 0
var f float64 // 0
var b bool    // false
var s string  // ""
```

Go 没有"未初始化的变量"，这避免了很多其他语言里常见的脏数据 bug。

## 类型转换

Go 不会做隐式类型转换，不同类型的值之间运算必须显式转换，写法是 `T(v)`：

```go
var i int = 42
var f float64 = float64(i)
var u uint = uint(f)
```

把 int 和 float64 直接相加会编译报错，必须先转成同一类型。这一点和 C、Java 的隐式提升不同。

## 常量

常量用 `const` 声明，可以是字符、字符串、布尔或数值：

```go
const Pi = 3.14
const World = "世界"
```

常量不能用 `:=` 声明。数值常量是高精度的，只有在赋值给变量时才确定具体类型。无类型常量可以根据上下文自动适配：

```go
const Big = 1 << 100 // 一个很大的整数常量，超出 int64 也能先存在
```

## 类型推导的小结

有初始化值时，编译器按值的类型推导变量类型：整数默认 int、小数默认 float64、字符串默认 string。需要其他类型时必须显式写出，例如 `var f float32 = 3.14`。
