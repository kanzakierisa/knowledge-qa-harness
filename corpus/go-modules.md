# Go 包与模块管理

> 内容整理自 go.dev 官方文档《Tutorial: Create a module》《Managing dependencies》。

## 包（package）的基本概念

Go 代码按包组织，每个目录是一个包，目录内所有 .go 文件开头声明同一个包名：

```go
package main // 可执行程序的入口包
```

`package main` 加 `func main()` 构成可执行程序；其他包名的是库，被别的代码 import 使用。包内首字母大写的标识符（函数、类型、变量、常量）是**导出**的，包外可见；小写开头的是包内私有。

## import 导入

import 用模块路径加包路径导入：

```go
import (
    "fmt"
    "strings"
    "github.com/gin-gonic/gin"
)
```

导入后未使用的包会编译报错，这是 Go 强制的整洁性检查。导入路径里的最后一段默认就是包名，可以用别名覆盖。

## 模块（module）与 go.mod

module 是 Go 的依赖管理单元，一组相关的包构成一个 module，由根目录的 go.mod 文件定义：

```bash
go mod init example.com/hello
```

生成的 go.mod 记录模块路径、Go 版本和依赖要求：

```go
module example.com/hello

go 1.22

require github.com/gin-gonic/gin v1.10.0
```

模块路径应是一个可以被 go 工具下载到的路径，通常对应代码仓库地址；不发布的本地项目可以用 example.com/hello 这类占位名。

## 添加与升级依赖

最常用的方式是直接写代码 import，然后执行：

```bash
go mod tidy
```

go mod tidy 会扫描全部源码，自动添加缺失依赖、移除未用依赖，并生成/更新 go.sum（记录每个依赖的加密哈希，保证下载内容没被篡改）。

也可以显式获取指定版本：

```bash
go get github.com/gin-gonic/gin@v1.10.0
go get -u ./... # 升级所有依赖到最新次版本/修订版本
```

## 语义化版本

Go 模块遵循语义化版本（SemVer）：v主版本.次版本.修订号。修订号升级是 bug 修复，次版本升级是向后兼容的新功能，主版本升级允许不兼容。主版本 ≥2 时模块路径必须以 /v2 等后缀结尾，这是 Go 模块特有的"语义化导入版本"规则。

## replace 与本地开发

调试本地未发布的依赖时，在 go.mod 里用 replace 把模块路径替换成本地目录：

```go
replace example.com/mypkg => ../mypkg
```

replace 只影响当前主模块，随 go.mod 提交后团队成员都生效，发布前要确认是否需要保留。

## vendoring

执行 `go mod vendor` 会把所有依赖复制到项目根的 vendor/ 目录，构建时自动优先使用它。vendor 可以让构建完全不依赖网络，代价是仓库体积变大。CI 环境网络受限时常用这种方式。
