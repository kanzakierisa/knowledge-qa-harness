// Package static 内嵌阶段 7 单页 Web 界面资源，确保不依赖运行工作目录即可服务。
package static

import _ "embed"

// IndexHTML 是单页 Web 界面的完整 HTML 内容。
//
//go:embed index.html
var IndexHTML []byte
