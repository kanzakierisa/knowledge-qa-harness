# Go 字符串与 fmt 格式化

## 字符串本质

Go 字符串是只读的字节序列，按 UTF-8 编码。len(s) 返回字节数而不是字符数。遍历字符串用 for range，每次迭代得到一个 rune（Unicode 码点）。

## strings 包

strings.Contains 判断是否包含子串，strings.HasPrefix / HasSuffix 判断前后缀，strings.Split 分割，strings.Join 连接，strings.Replace 替换，strings.ToUpper / ToLower 大小写转换，strings.TrimSpace 去除首尾空白。

## strconv 包

字符串与数字互转：strconv.Atoi 字符串转 int，strconv.Itoa int 转字符串，ParseFloat / ParseBool 同理。

## fmt 格式化动词

%v 默认格式，%+v 带字段名，%#v Go 语法表示，%T 打印类型，%d 整数，%s 字符串，%f 浮点，%.2f 保留两位小数，%t 布尔，%q 带引号字符串。Sprintf 返回字符串，Printf 输出到标准输出，Fprintf 写入指定 writer。
