# Go time 包

## 获取时间

time.Now() 返回当前本地时间 time.Time。time.Unix(sec, nsec) 从 Unix 时间戳构造。

## 格式化

Go 用参考时间 Mon Jan 2 15:04:05 MST 2006（即 01/02 03:04:05PM '06 -0700）做格式模板：

```
t.Format("2006-01-02 15:04:05")
t.Format("2006-01-02")
```

解析字符串用 time.Parse("2006-01-02", s)。

## Duration

time.Duration 是纳秒级整数，常用常量：time.Second、time.Minute、time.Hour。加减时间用 t.Add(d)，比较用 t.Before / t.After / t.Equal，差值用 t.Sub(u)。

## Timer 与 Ticker

time.Sleep 暂停当前 goroutine。time.NewTimer 触发一次，time.NewTicker 周期触发，Ticker 用 C channel 接收时间，不再使用时必须 Stop。
