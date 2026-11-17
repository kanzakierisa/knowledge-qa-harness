# Go 命令行与 flag 包

## flag 包

标准库 flag 解析命令行参数。定义方式有两种：flag.String 返回指针，或用 Var 绑定已有变量。

```
port := flag.Int("port", 8080, "监听端口")
name := flag.String("name", "world", "名字")
flag.Parse()
```

调用 flag.Parse() 后解析 os.Args[1:]。支持 -port=9090 和 -port 9090 两种写法。

## 位置参数

flag.Args() 返回 flag 之后的剩余位置参数，flag.NArg() 是数量。

## 环境变量

os.Getenv("KEY") 读取环境变量，os.Setenv 设置，os.LookupEnv 可判断变量是否存在（区分空值与未设置）。

```
if v := os.Getenv("MYSQL_DSN"); v == "" {
    log.Fatal("缺少 MYSQL_DSN")
}
```

## 退出

os.Exit(1) 立即退出且不执行 defer；log.Fatal 打印消息后调用 os.Exit(1)。
