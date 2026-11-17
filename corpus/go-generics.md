# Go 泛型（Go 1.18+）

## 类型参数

函数名后用方括号声明类型参数，any 表示任意类型。

```
func Max[T comparable](a, b T) T { ... }
```

## 约束

约束写在类型参数后面。内置约束：any、comparable（可比较）。constraints 思路是用接口表达约束，例如：

```
type Number interface {
    ~int | ~float64
}
```

~ 表示底层类型匹配，| 表示并集。

## 泛型类型

类型本身也可以泛型化：

```
type Stack[T any] struct {
    items []T
}
func (s *Stack[T]) Push(v T) { s.items = append(s.items, v) }
```

## 使用建议

泛型适合容器和通用算法，不要为泛型而泛型；简单场景用具体类型或接口更直观。
