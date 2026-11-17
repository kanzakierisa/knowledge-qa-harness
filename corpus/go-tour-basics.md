# Go 基础语法

## 包与导入

每个 Go 文件都以 package 声明开头，程序从 main 包的 main 函数开始执行。import 导入其他包。

```
package main

import "fmt"

func main() {
    fmt.Println("hello")
}
```

## 变量声明

var 声明变量，函数内可用 := 短声明自动推断类型。声明未使用的局部变量会编译报错。

```
var name string = "gopher"
age := 10
```

## 基本类型

bool、string、int、int64、float64、byte（uint8 别名）、rune（int32 别名，表示 Unicode 码点）。数值类型默认零值为 0，字符串零值为空串，布尔零值为 false。

## 常量

const 声明常量，不能用 := 语法。iota 在常量组中从 0 开始自增，常用于枚举。

## 控制流

if 无需括号，条件前可写短语句。for 是唯一的循环关键字，可充当 while。switch 自动 break，无需显式书写。defer 把函数调用推迟到外层函数返回前执行，多个 defer 后进先出。
