# Go 结构体与方法

> 内容整理自 go.dev 官方教程《A Tour of Go》的 Methods and interfaces 章节。

## 结构体的定义

结构体（struct）是字段的集合，用 `type` 和 `struct` 关键字定义：

```go
type Vertex struct {
    X int
    Y int
}
```

字段名首字母大写表示导出（包外可见），小写表示包内私有。创建结构体并访问字段用点号：

```go
v := Vertex{1, 2}      // 按字段顺序初始化
v2 := Vertex{X: 1}     // 按字段名初始化，未列出的字段取零值
v.X = 4                // 修改字段
fmt.Println(v.X)
```

## 结构体指针

结构体可以通过指针访问，用 `&` 取地址：

```go
v := Vertex{1, 2}
p := &v
p.X = 1e9 // 等价于 (*p).X = 1e9，Go 自动解引用
```

通过指针修改字段会影响原结构体。结构体较大时传指针可以避免整体拷贝。

## 方法的定义

Go 没有类，但可以为类型定义方法。方法是带**接收者**参数的函数，接收者写在 func 和方法名之间：

```go
type Vertex struct {
    X, Y float64
}

func (v Vertex) Abs() float64 {
    return math.Sqrt(v.X*v.X + v.Y*v.Y)
}
```

调用方式和其他语言的对象方法一样：`v.Abs()`。方法只能定义在同一个包的类型上，不能为 int、string 等内置类型定义方法（但可以先定义命名类型再挂方法）。

## 指针接收者与值接收者

接收者可以是指针。指针接收者的方法可以修改接收者指向的值：

```go
func (v *Vertex) Scale(f float64) {
    v.X = v.X * f
    v.Y = v.Y * f
}
```

值接收者操作的是原值的副本，方法内对字段的修改不会影响原结构体。选择指针接收者的两个理由：一是需要修改接收者；二是避免每次调用拷贝整个结构体。同一个类型的方法集应保持一致风格，不要指针接收者和值接收者混用。

## 组合代替继承

Go 没有继承，通过结构体**嵌套（嵌入）**实现组合复用：

```go
type Animal struct {
    Name string
}

func (a Animal) Speak() string { return "..." }

type Dog struct {
    Animal // 嵌入字段，Dog 自动获得 Name 字段和 Speak 方法
    Age    int
}
```

嵌入类型的字段和方法会被"提升"到外层结构体，可以直接 `dog.Name`、`dog.Speak()`。这是 Go 实现代码复用的主要方式。

## 构造函数惯例

Go 没有构造函数关键字，惯例是写一个 `NewXxx` 函数返回初始化好的指针：

```go
func NewVertex(x, y float64) *Vertex {
    return &Vertex{X: x, Y: y}
}
```

需要校验参数或设置默认值时，都收拢在这个工厂函数里。
