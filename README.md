# Go 知识问答 Agent Harness

基于 Go / Gin 官方文档的可溯源问答系统，带金标评测集与四个量化指标。

## 为什么做这个

- 知识散落、AI 答案不敢直接用 → 强制每句结论标注来源 `[n]`，无足够依据时明确拒答
- 优化不能靠感觉 → 以 25 题金标评测集起步（阶段 6 增量扩充至 52 题），用 4 个量化指标驱动单变量迭代
- 不做重型编排 → 纯 Go 直线流水线 + 代码级硬护栏，每一步可观察、可回归、可复现

## 架构

```
                 ┌─────────────── 离线：语料入库 ───────────────┐
                 │                                              │
  corpus/*.md → 切片器(按标题层级) → chunks → embedding API → 向量存 MySQL

                 ┌─────────────── 在线：问答链路 ───────────────┐
                 │                                              │
  用户问题 → embedding → 向量检索 ──┐
                          关键词检索 ┤→ 融合排序取 Top 5
                                    ↓
                    最高分 < 阈值？ → 是 → 直接拒答（不调 LLM）
                                    ↓ 否
                    拼 prompt（问题 + 5 段带编号原文 + 引用约束）
                                    ↓
                              LLM 生成答案
                                    ↓
                    校验：答案里的 [n] 编号是否都存在？
                    不存在 → 重试一次 → 仍失败 → 返回拒答
                                    ↓
                    返回 {答案, 引用编号→原文段落映射}
```

## 评测结果

- **阶段 5 首轮基线（25 题：18 作答 + 7 拒答）**：四项指标均达到 `100.00%`（明细见 [eval_first_round.json](eval_first_round.json) 与 [eval/first_round_report.md](eval/first_round_report.md)）。
- **阶段 6 控制变量迭代（52 题：40 作答 + 12 拒答）**：从扩充基线（迭代 0）出发，每次只改一个变量并重跑全量评测，经 4 轮有效迭代达到最终基线（另保留 1 轮 Prompt 退化回滚记录），完整归因与参数仿真见 [docs/iterations.md](docs/iterations.md)。

| 指标 | 阶段 5 首轮 (25 题) | 阶段 6 迭代 0 基线 (52 题) | 阶段 6 最终基线 (迭代 4, 52 题) | 计算口径 |
|---|---|---|---|---|
| 检索命中率 | 100.00% (18/18) | 95.00% (38/40) | **100.00% (40/40)** | `gold_chunk` 落入 Top 5 数 / 可作答题数 |
| 引用准确率 | 100.00% (18/18) | 100.00% (35/35) | **100.00% (38/38)** | 句级引用合法数 / 实际非拒答回答数 |
| 答案可用率 | 100.00% (18/18) | 97.14% (34/35) | **100.00% (38/38)** | 人工标注可用数 / 已标注非拒答回答数 |
| 拒答正确率 | 100.00% (7/7) | 100.00% (12/12) | **100.00% (12/12)** | 正确拒答数 / 应拒答题数 |

> 最终基线 52 题中，2 题模型保守自拒答（`误拒数 = 2`，不编造答案），所有实际作答 38 题全部通过句级引用校验与人工可用性核对；`usable` 始终由人工调用 `/api/eval/mark` 标注，未标注时 `usable_rate` 返回 `null`。

## 快速开始

### 环境要求

- Go 1.22+
- MySQL 8
- OpenAI 兼容 API（对话 + Embedding）

### 运行步骤

1. **准备 MySQL，执行建库建表脚本**：
   ```bash
   mysql -u root -p < migrations/001_init.sql
   ```
   （如已在目标库中运行，也可在选定库后执行建表语句。）

2. **设置环境变量**（参考 [.env.example](.env.example)，程序不自动读取 `.env` 文件，缺任一必填变量启动即报错退出）：
   ```bash
   # ===== 5 个必需变量 =====
   export LLM_BASE_URL=https://api.deepseek.com/v1
   export LLM_API_KEY=sk-xxx
   export LLM_CHAT_MODEL=deepseek-chat
   export LLM_EMBED_MODEL=text-embedding-v3
   export MYSQL_DSN='root:password@tcp(127.0.0.1:3306)/knowledge_qa?charset=utf8mb4&parseTime=true'

   # ===== 可选：Embedding 走独立平台时补充（缺省回落到 LLM_BASE_URL / LLM_API_KEY）=====
   export EMBED_BASE_URL=https://dashscope.aliyuncs.com/compatible-mode/v1
   export EMBED_API_KEY=sk-yyy

   # ===== 可选：自定义监听地址（默认 :8080）=====
   # export HTTP_ADDR=:8080
   ```
   Windows PowerShell 可用 `$env:KEY="VALUE"` 或 CMD `set KEY=VALUE`。

3. **灌入语料**（扫描 `corpus/*.md`，按切片器 v2 切出 179 段并计算向量入库）：
   ```bash
   go run scripts/ingest.go
   # 语料或切片策略修改后可先清空重灌：
   # go run scripts/ingest.go -clean
   ```

4. **导入金标评测题（可选，运行 `/api/eval/run` 前需要）**：
   ```bash
   go run ./scripts/seed_eval.go          # 导入阶段 5 原 25 题（-review 只看不写）
   go run ./scripts/seed_eval_stage6.go   # 增量追加阶段 6 的 27 题至 52 题
   ```

5. **启动服务并打开 Web 界面**：
   ```bash
   go run .
   ```
   浏览器访问 **http://localhost:8080** 即可提问并点击 `[n]` 跳转高亮参考段落。

## 目录结构

```
knowledge-qa/
├── main.go         # 服务入口：校验环境变量、连接 MySQL、装配模块并启动 Gin
├── config/         # 环境变量加载与 5 项必填校验（支持 EMBED_* 独立端点）
├── store/          # MySQL 读写层（documents、chunks、eval_questions、eval_runs）
├── ingest/         # Markdown 切片器 v2（代码块保护/长段再切/短段合并）与 Embedding 客户端
├── retrieve/       # 向量余弦检索、简化 BM25 关键词检索与两路融合排序（0.75/0.25）
├── generate/       # 强约束 Prompt v3、对话客户端、句级引用校验与重试兜底
├── eval/           # 52 题金标评测引擎、四项指标统计与历史评测报告快照
├── api/            # HTTP 接口路由（/api/ask、/api/chunks、/api/eval/run、/api/eval/mark）
├── static/         # 原生单页 Web 界面（无框架/无 CDN，go:embed 内嵌打包）
├── corpus/         # 39 篇 Go / Gin 官方文档中文整理语料与切片统计 STATS.md
├── scripts/        # 语料入库（ingest.go）、金标导入（seed_eval*.go）及评测分析工具
├── migrations/     # MySQL 建库建表 SQL（001_init.sql）
├── docs/           # 阶段 6 迭代归因文档（iterations.md）与 3 分钟录屏脚本（demo-script.md）
├── .env.example    # 环境变量说明模板（不含真实凭据）
└── README.md       # 项目说明与复现指南
```

## 设计取舍

- **为什么不用向量数据库**：当前语料规模 39 篇共 179 段（≤500 段），全量向量放进 MySQL 并在 Go 内存手写余弦相似度耗时毫秒级，少一个外部组件，部署与调试成本更低。
- **为什么不用 LangChain**：RAG 问答链路本质是“检索 → 阈值拦截 → Prompt 约束 → 引用校验”的直线流水线，不涉及多步工具规划；手写流水线让每一步都可独立单测、打日志和量化归因，Harness 的核心是约束而非编排。
- **为什么混合检索**：纯向量懂语义改写但对 `c.Query`、`http.StatusOK`、`sync.WaitGroup` 等精确符号不够敏感；BM25（中文 2-gram + 英文/代码标识符切分）精准补齐术语信号，配合软下限 `KeywordNormFloor=45.0` 与 `0.75/0.25` 权重避免泛词压制语义首位。

## 护栏设计

1. **阈值拒答**：混合检索 Top 1 融合分低于 `0.35` 时直接拒答并固定返回 `文档中没有依据`，不调用 LLM，既杜绝无据编造又节省 token。
2. **引用校验**：对模型回答先剥离代码块与行内代码，校验正文所有 `[n]` 均落在检索段落 `1..N` 范围内，且每个实质性陈述句与结尾句均标注来源编号，完全无引用或漏标均判为校验失败。
3. **重试兜底**：校验失败时将具体错误原因追加进 Prompt 触发一次纠错重试；若重试后仍不合法则整题兜底拒答（`refused: true`），同时识别模型主动输出的拒答文案，并通过 `retried` 字段记录重试状态。

## HTTP 接口与日常验证

| 方法 | 路径 | 入参 | 说明 |
|---|---|---|---|
| `GET` | `/` | — | 单页 Web 问答界面 |
| `GET` | `/api/chunks` | — | 健康检查，返回 `{"total": 179}` |
| `POST` | `/api/ask` | `{"question": "...", "debug": false}` | 问答接口（`debug=true` 附带两路打分明细） |
| `POST` | `/api/eval/run` | — | 运行全量金标评测并返回四项指标与明细 |
| `POST` | `/api/eval/mark` | `{"run_id": 1, "usable": true}` | 人工标注非拒答回答的可用性 |

常用校验命令：

```bash
# 代码静态检查与离线单元测试（无需数据库与 API 额度）
go vet ./...
go test ./...

# 问答与拒答冒烟
curl http://localhost:8080/api/chunks
curl -X POST http://localhost:8080/api/ask -H "Content-Type: application/json" -d '{"question": "Gin 路由组怎么用？"}'
curl -X POST http://localhost:8080/api/ask -H "Content-Type: application/json" -d '{"question": "怎么做法式红酒炖牛肉？"}'

# 一键评测与人工标注
curl -X POST http://localhost:8080/api/eval/run
curl -X POST http://localhost:8080/api/eval/mark -H "Content-Type: application/json" -d '{"run_id": 1, "usable": true}'
```

如需按 3 分钟录屏节奏演示完整功能，可直接参考 [docs/demo-script.md](docs/demo-script.md)。
