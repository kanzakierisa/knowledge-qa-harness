# Go 数组、切片与 map

## 数组

数组长度是类型的一部分，[10]int 和 [20]int 是不同类型，因此数组很少直接用。

## 切片

切片是对数组的动态视图，用 []T 表示。len 是长度，cap 是底层数组容量。

```
s := []int{1, 2, 3}
s = append(s, 4)
```

append 追加元素，容量不足时会自动扩容（通常翻倍）并可能更换底层数组。切片可以用 s[low:high] 再切分，多个切片可共享底层数组。

make 用于创建切片：make([]int, 0, 10)。

## range

for i, v := range s 遍历切片或 map，i 是下标（或键），v 是值。用 _ 忽略不需要的部分。

## map

map 是键值对：map[string]int。用 m[k] 取值，m[k] = v 赋值，delete(m, k) 删除。取值可用双返回值判断键是否存在：

```
v, ok := m["key"]
```

map 的零值是 nil，必须用 make 初始化后才能写入。
