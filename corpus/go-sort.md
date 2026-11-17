# Go sort 排序

## 基础类型排序

sort.Ints、sort.Float64s、sort.Strings 对基础类型切片原地升序排序。sort.IntsAreSorted 判断是否已排序。

## 自定义排序

sort.Slice 用比较函数排序任意切片：

```
sort.Slice(people, func(i, j int) bool {
    return people[i].Age < people[j].Age
})
```

sort.SliceStable 是稳定排序（相等元素保持原顺序）。

## sort.Interface

实现 Len / Less / Swap 三个方法的类型可以用 sort.Sort 排序，适合需要复用排序逻辑的场景。

## 查找

sort.SearchInts / SearchStrings 在已排序切片里二分查找插入位置。

## 反向排序

sort.Sort(sort.Reverse(data)) 把已有排序器反转成降序。
