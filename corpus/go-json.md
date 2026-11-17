# Go 处理 JSON（encoding/json）

## 序列化

json.Marshal 把结构体编码为 JSON 字节串。只有大写开头的导出字段会被序列化。

```
type User struct {
    Name string `json:"name"`
    Age  int    `json:"age,omitempty"`
}
b, err := json.Marshal(u)
```

struct tag 里 json:"name" 指定字段名，omitempty 表示零值时省略该字段，json:"-" 表示完全忽略。

## 反序列化

json.Unmarshal 把 JSON 解析进结构体，传入指针。

```
var u User
err := json.Unmarshal(data, &u)
```

未知字段默认被忽略；类型不匹配会报错。

## 流式编解码

json.NewEncoder(w).Encode(v) 直接写入 io.Writer，json.NewDecoder(r).Decode(&v) 从 io.Reader 读取，适合 HTTP 请求响应等大流量场景。

## map[string]interface{}

结构不确定的 JSON 可以解析到 map[string]interface{}，数字会变成 float64，需要自己类型断言。
