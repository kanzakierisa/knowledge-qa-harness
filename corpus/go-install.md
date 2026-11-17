# Go 安装与环境配置

> 内容整理自 go.dev 官方文档《Download and install》《Getting started》。

## 下载与安装

Go 的官方下载地址是 https://go.dev/dl/ 。页面提供 Windows、macOS、Linux 三大平台的安装包。Windows 用户下载 .msi 安装程序，双击按向导完成即可，默认安装到 `C:\Program Files\Go`，安装器会自动把 Go 的 bin 目录加入 PATH 环境变量。

macOS 用户下载 .pkg 安装包，默认安装到 /usr/local/go。Linux 用户下载 tar.gz 压缩包，官方推荐先删除旧版本再解压到 /usr/local：

```bash
rm -rf /usr/local/go && tar -C /usr/local -xzf go1.22.5.linux-amd64.tar.gz
```

解压后需要把 Go 的 bin 目录加入 PATH，在 `$HOME/.profile` 中添加：

```bash
export PATH=$PATH:/usr/local/go/bin
```

对 profile 的修改要到下次登录终端才生效，也可以用 `source $HOME/.profile` 立即生效。

## 验证安装

安装完成后打开一个新的终端窗口，执行：

```bash
go version
```

如果输出类似 `go version go1.22.5 windows/amd64` 的信息，说明安装成功。如果提示找不到命令，先确认终端是在安装之后新开的，再检查 PATH 是否包含 Go 的 bin 目录。

## 编写第一个程序

官方教程建议新建一个目录并初始化模块：

```bash
mkdir hello
cd hello
go mod init example/hello
```

然后创建 hello.go 文件，写入一个最简单的 main 程序：

```go
package main

import "fmt"

func main() {
    fmt.Println("Hello, World!")
}
```

在目录下执行 `go run .` 即可编译并运行程序，输出 Hello, World!。`go build` 则会把程序编译成一个可执行文件，之后可以直接运行该文件而不需要 Go 工具链。

## 常用环境变量

`go env` 命令可以查看 Go 的全部环境配置。最常用的几个：

- `GOROOT`：Go 的安装目录，安装器自动设置，一般无需手动改。
- `GOPATH`：工作区目录，存放下载的第三方模块缓存，默认是用户主目录下的 go 文件夹。
- `GOPROXY`：模块下载代理。国内网络访问默认代理较慢，常设置为国内镜像。

设置 GOPROXY 的命令如下，`-w` 表示写入配置文件持久生效：

```bash
go env -w GOPROXY=https://goproxy.cn,direct
```

## 升级与卸载

升级 Go 的方法就是安装新版本，安装器会覆盖旧版本。卸载时：Windows 在"添加或删除程序"里卸载并删除 Go 目录；Linux 和 macOS 直接删除 /usr/local/go 目录，并移除 profile 里的 PATH 行。需要清理工作区时可再删除 GOPATH 目录。
