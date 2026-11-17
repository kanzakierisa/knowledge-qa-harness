# Go 错误处理惯例（Effective Go）

## 多返回值返回错误

Go 函数通常把 error 作为最后一个返回值。调用方应立即检查 err，不要忽略。

```
f, err := os.Open("a.txt")
if err != nil {
    return err
}
```

## 自定义错误

errors.New 创建简单错误，fmt.Errorf 配合 %w 包装错误以保留错误链。

```
return fmt.Errorf("打开配置失败: %w", err)
```

## errors.Is 与 errors.As

判断错误链中是否包含某个哨兵错误用 errors.Is(err, target)；提取特定类型的错误用 errors.As(err, &target)。不要再用 == 直接比较包装过的错误。

## panic 与 recover

panic 用于真正的不可恢复错误（如程序内部矛盾），普通错误不要 panic。recover 只能在 defer 函数中捕获 panic。库代码通常不应 panic，而应返回 error。
