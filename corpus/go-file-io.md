# Go 文件与 IO

## os 包读写文件

os.ReadFile 一次读完整个文件，os.WriteFile 一次写入（0644 是常见权限）。

```
data, err := os.ReadFile("a.txt")
err = os.WriteFile("b.txt", data, 0644)
```

os.Open / os.Create 返回 *os.File，可细粒度读写，用完必须 Close，惯例是 defer f.Close()。

## io 包

io.Reader 和 io.Writer 是 Go 最重要的两个接口，各只有一个方法：Read(p []byte) 和 Write(p []byte)。io.Copy(dst, src) 在两者之间搬运数据。io.ReadAll 读完一个 Reader。

## bufio

bufio.NewReader / NewWriter 提供缓冲 IO 和按行读取。bufio.Scanner 按行（或自定义分隔）扫描大文件：

```
sc := bufio.NewScanner(f)
for sc.Scan() {
    line := sc.Text()
}
```

## 目录操作

os.MkdirAll 递归建目录，os.ReadDir 列目录，filepath.Walk 遍历目录树，filepath.Join 拼路径（自动处理平台分隔符）。
