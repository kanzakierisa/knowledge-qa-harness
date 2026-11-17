# Go regexp 正则

## 编译

regexp.MustCompile 编译正则，失败时 panic，适合包级变量的静态正则；regexp.Compile 返回 error，适合动态正则。

```
var numRe = regexp.MustCompile(`\d+`)
```

## 常用方法

MatchString 判断是否匹配，FindString 找第一个匹配，FindAllString 找全部，ReplaceAllString 替换，Split 按正则切分。

## 子匹配捕获

FindStringSubmatch 返回完整匹配及各捕获组，索引 0 是整体，1 起是各分组。

```
re := regexp.MustCompile(`(\w+)@(\w+)`)
m := re.FindStringSubmatch("user@host")
```

## 性能提示

正则编译较慢，应在初始化时编译一次复用，不要在循环里反复 Compile。反引号原始字符串可避免反斜杠双重转义。
