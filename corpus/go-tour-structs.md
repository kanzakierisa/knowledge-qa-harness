# Go 结构体与方法

## 结构体

struct 是字段的集合，用点号访问字段。

```
type Point struct {
    X int
    Y int
}
```

结构体字面量可以按字段名初始化：Point{X: 1, Y: 2}，也可以按顺序：Point{1, 2}。

## 方法

方法是带接收者（receiver）的函数。接收者可以是值也可以是指针。

```
func (p Point) Abs() float64 { ... }
func (p *Point) Scale(f float64) { p.X = p.X * f }
```

指针接收者才能修改结构体内容；值接收者操作的是副本。

## 接口

interface 是一组方法签名。类型实现了接口的所有方法即自动满足该接口，无需显式声明 implements。空接口 interface{} 可以装任意值。

## 错误处理

error 是内置接口，约定返回值的最后一个为 error，nil 表示成功。

```
if err != nil {
    return err
}
```
