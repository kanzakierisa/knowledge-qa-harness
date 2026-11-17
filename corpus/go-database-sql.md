# Go database/sql 操作 MySQL

## 打开连接

database/sql 是通用接口，需要搭配具体驱动，MySQL 用 github.com/go-sql-driver/mysql（匿名导入注册驱动）。

```
import _ "github.com/go-sql-driver/mysql"

db, err := sql.Open("mysql", "user:pass@tcp(127.0.0.1:3306)/dbname?charset=utf8mb4&parseTime=true")
```

sql.Open 只是建立连接池配置，不会真正连接；用 db.Ping() 验证连通性。

## 查询

db.Query 返回多行，必须 rows.Close()；db.QueryRow 返回单行，Scan 时无行会返回 sql.ErrNoRows。

```
var name string
err := db.QueryRow("SELECT name FROM users WHERE id = ?", id).Scan(&name)
```

## 写入

db.Exec 执行 INSERT/UPDATE/DELETE，返回 sql.Result，可取 LastInsertId() 和 RowsAffected()。

## 占位符防注入

永远用 ? 占位符传参，不要字符串拼接 SQL，防止 SQL 注入。

## 连接池

db.SetMaxOpenConns 限制最大连接数，SetConnMaxLifetime 限制连接复用时间（MySQL 默认会踢掉 8 小时无活动的连接）。
