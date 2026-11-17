# Go net/http 标准库

## 起服务

http.HandleFunc 注册处理函数，http.ListenAndServe 启动服务器。

```
http.HandleFunc("/hello", func(w http.ResponseWriter, r *http.Request) {
    fmt.Fprintln(w, "hello")
})
http.ListenAndServe(":8080", nil)
```

第二个参数传 nil 表示使用默认的 DefaultServeMux。

## 处理函数签名

处理函数接收 http.ResponseWriter 和 *http.Request。r.URL.Query().Get("key") 读取查询参数，r.FormValue 读取表单字段，r.Method 判断请求方法。

## 发请求（客户端）

http.Get 发 GET 请求，响应体必须关闭：

```
resp, err := http.Get(url)
defer resp.Body.Close()
body, _ := io.ReadAll(resp.Body)
```

需要超时控制时用 http.Client{Timeout: 10 * time.Second}。自定义请求头用 http.NewRequest 配合 client.Do。

## JSON 响应

w.Header().Set("Content-Type", "application/json") 后用 json.NewEncoder(w).Encode(v) 输出。
