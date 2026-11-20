# 企业级可溯源知识问答与文档治理工作台（Knowledge QA Harness）

> 用纯 Go 零重型编排框架打造的**可控、可溯源、可量化回归**的知识库问答与文档治理系统。支持 `.md / .txt / .docx / .xlsx / .pdf / 图片(VLM)` 多格式入库、项目隔离与废弃版本提示、句级证据荧光溯源、一键复制飞书可信引用、`--setup-ui` 免落盘网页引导配置、MySQL 多轮会话任务持久化、文档变更自动局部回归（Doc CI/CD）以及「盲区雷达 + 用户反馈 + 金标评测集」全链路闭环。

- **3 分钟全链路演示脚本**：见 [docs/v2_demo_script.md](docs/v2_demo_script.md)
- **Windows 免环境一键安装包**：见 GitHub Releases 中的 `knowledge-qa-release.zip`（内含预编译 `.exe` 与双击启动脚本，无需安装 Go）

---

## 面向 AI 产品 / 产运：1 页纸核心设计思考

> 为什么不用现成 RAG 编排框架套壳，而要手写一套 **Agent Harness**？因为团队真正不敢用 AI 问答，根因从来不是“模型不够聪明”，而是**边界不可控、指标无真值、文档会过期、改完不敢上线**。

1. **用确定性代码约束替代“祈祷式 Prompt”**：Retrieval `< 0.35` 直接零 LLM 拒答；生成采用 **G1 Quote-First JSON 契约**（先逐字摘录原句再作答），并经过 `ValidateCitations` 句级硬校验，失败仅重试 1 次、仍不过兜底拒答。
2. **用对抗盲测与“敢回滚”防止刷指标**：保留 `base52`（52 题）基线同时独立构建 39 题 `holdout` 盲测集，专门设置 13 题复用真实 API 名的**高分陷阱拒答题（`near_miss_refusal`）**守住 `100%` 拒答底线；发现 `gte-rerank-v2` 虽能拉高单题却使 `base52` 语义改写题退化 `-2.5%` 后，严格按红线默认关闭。
3. **坚持“口径 B”反虚假繁荣**：线上用户提问没有标准 `gold_chunk_id`，系统**绝不伪造线上 100% 命中率**；「当前会话 / 全部提问」仅统计客观可测的句级引用校验率、用户主动 👍/👎 反馈率与真实拒答次数，离线回归与线上盲区库物理隔离。
4. **把问答延伸到团队协作流**：回答卡片支持**「📋 复制可信引用」**（自动把 `[n]` 还原为可读文档名、章节与废弃警示，方便直接粘贴进飞书群/周报）；文档覆盖更新自动触发 **Doc CI/CD 局部回归**，防止新改一篇规范悄悄冲垮旧问答。

---

## 一、核心痛点与工程解法

| 企业知识问答常见痛点 | 本系统（Harness）的工程化解法 |
|---|---|
| **1. 多格式文档导入难，切块破坏结构** | 纯 Go 解析 `.md / .txt / .docx / .xlsx / .pdf`（图片或扫描件按需走 VLM 转录并严格拒绝静默乱码），保留标题层级、`table` 行与围栏代码块完整性。 |
| **2. 大模型“一本正经胡说八道”、不敢采信** | 向量 `0.75` + BM25 `0.25`（`KeywordNormFloor=45.0`）融合检索，**Top1 融合分 `< 0.35` 直接拒答且零 LLM 调用**；生成阶段强制句级 `[n]` 引用与纠偏重试，前端一键定位原文并对支持句做 `<mark>` 荧光高亮。 |
| **3. 多项目同名规范“张冠李戴”、误用过期文档** | 项目空间独立隔离 + `shared` 公共规范自动合并；`auto` 模式下检测跨项目候选竞争差 `<= 0.08` 立即拦截提示并支持并排独立对照；命中 `deprecated` 文档主动弹出黄色替代版本警示。 |
| **4. 改 Prompt / 更新文档怕悄悄退化** | 自带 **52 题基准集（`base52`）+ 39 题对抗盲测集（`holdout`，含 13 题近似拒答）+ 反馈活评测集（`feedback`）**；支持按文件 `overwrite=true` 热更新时自动触发关联评测题的局部回归体检（Doc CI/CD），不污染线上盲区库。 |

---

## 二、系统架构（V3）

```text
  多格式输入 (.md / .txt / .docx / .xlsx / .pdf / 图片)
        │
        ▼ ingest.Convert + SplitMarkdownV3 (标题路径 / 代码块与表格完整 / 内容哈希去重)
        ▼ ingest.Embedder (OpenAI 兼容 /vectors 接口)
  MySQL 8.0 (`knowledge_qa_v3`)
   ├── projects / documents (active/deprecated/replaced_by) / chunks
   ├── qa_sessions / qa_turns (多轮任务持久化 & 口径 B 真实指标统计)
   ├── knowledge_gaps / eval_feedback (线上未命中拒答 & 用户 👍/👎 闭环)
   └── eval_questions (`base52` / `holdout` / `feedback`) / eval_runs
        │
        ▼ POST /api/ask (或 /api/eval/run)
  [1] 双路召回与项目隔离: 0.75 × Cosine + 0.25 × BM25 (KeywordNormFloor=45.0)
        ├── auto 模式屏蔽测试夹具 (is_hidden=1)，跨项目Top1差 <= 0.08 触发歧义拦截/并排对照
        ├── 按材料模式锁定 document_ids 精确切片池
        ▼
  [2] 阈值硬拒答门: Top1 < 0.35 → 直接返回「文档中没有依据」（不调用 LLM，线上写入盲区雷达）
        ▼
  [3] 受约束生成与代码级引文守门: 强制句级 [n] 编号，违规触发 1 次纠偏重试，仍不过兜底拒答
        ▼
  单页三栏工作台 (static/index.html，无 CDN / 无 innerHTML，深浅双主题 + 移动端适配)
   ├── 「智能问答」：左侧任务列表（置顶 / 悬停详情 / ... 菜单重命名删除）+ 多轮对话 + 口径B「当前会话」统计 + 原文 `<mark>` 高亮 + 复制可信引用
   ├── 「资料库治理」：项目与格式总览、全文预览、废弃替代设置、覆盖重切与 Doc CI/CD 回归体检单
   └── 「盲区与反馈」：待补充盲区直达治理、用户反馈一键转正评测集、反馈活评测 + 右对齐「全部提问」线上真实统计
```

---

## 三、即插即用：快速体验与部署

> 严禁将任何真实 API Key 或密码写入仓库文件。默认推荐通过 `.env`（已被 `.gitignore` 忽略）或 `--setup-ui` 网页端输入（**仅写入进程内存，绝不落盘**）。

### 方式 A：Docker Compose 一键启动（含 MySQL 8 + 自动建表）

1. 复制环境模板：
   ```bash
   cp .env.example .env
   ```
   - 若你**已在 `.env` 填好** `LLM_BASE_URL / LLM_API_KEY / LLM_CHAT_MODEL / LLM_EMBED_MODEL`（若对话与向量分属不同平台，可追加 `EMBED_BASE_URL` / `EMBED_API_KEY`），容器首次启动会自动幂等导入 `corpus/` 与 `corpus_v2/` 多格式演示语料及 4 道示例回归题。
   - 若你**暂未填写模型 Key**，可直接保留 `.env` 中的占位空值：Compose 默认开启 `ALLOW_WEB_SETUP=true`，服务会先以引导配置态启动，随后直接在网页弹窗中填写模型参数即可运行。
2. 构建并启动服务：
   ```bash
   docker compose up --build
   ```
3. 浏览器打开 `http://localhost:8080`。

> **本地测试环境说明**：仓库中的 `Dockerfile`、`docker-compose.yml` 与初始化脚本 `migrations/docker-init/00_init_v3.sh`（自动将只读的 `001_init.sql` 重定向至 `knowledge_qa_v3` 并叠加 `002_v2_schema.sql`）已完整编写并与空库模拟流程对齐。由于当前开发机未安装 Docker CLI，Docker Compose 容器启停命令未在本地实机执行，空库建表与灌库脚本已通过本地 MySQL 8 建新库方式完整实测通过。

---

### 方式 B：本地源码快速启动（Go 1.26+ & MySQL 8.0）

1. **创建独立数据库 `knowledge_qa_v3` 并导入结构**：
   - 创建空库后，导入 `migrations/001_init.sql`（如导入至 `knowledge_qa_v3`，跳过首两行 `CREATE DATABASE/USE knowledge_qa` 语句）并执行 `migrations/002_v2_schema.sql`。
2. **使用一键启动脚本**：
   ```bash
   cp .env.example .env
   # 编辑 .env 填入本地 MySQL 连接串 MYSQL_DSN（模型 Key 可选，缺失时自动进 --setup-ui）
   ```
   - **Linux / macOS**：
     ```bash
     chmod +x scripts/quickstart.sh
     ./scripts/quickstart.sh --seed        # 若已配模型 Key，幂等导入多格式演示沙箱并启动
     # 或仅需在网页填写模型参数：
     ./scripts/quickstart.sh --setup-ui
     ```
   - **Windows PowerShell**：
     ```powershell
     .\scripts\quickstart.ps1 -Seed
     # 或仅需在网页填写模型参数：
     .\scripts\quickstart.ps1 -SetupUI
     ```
3. **内置演示沙箱（`scripts/seed_demo_v2.go`）说明**：
   - 默认运行 `go run ./scripts/seed_demo_v2.go` 会采用双模式导入：若 `go-gin` 为空则直接复用 `corpus/` 下 39 篇 Markdown；若库中已有基线语料则完整保留不动，并幂等写入 `corpus_v2/` 多格式示例文档（`.md` / `.docx` / `.xlsx` / `.pdf` 与 `v1 deprecated -> v2 active` 版本链）、`feedback` 示例回归题与 2 条脱敏盲区示例。
   - 如需在空库中一并导入完整 52 + 39 题金标与盲测夹具以复现全部量化指标，可追加参数：
     ```bash
     go run ./scripts/seed_demo_v2.go -with-base-eval
     ```

---

## 四、量化评测结果与关键实验决策

### 1. 阶段演进指标总览

| 阶段 / 数据集 | 题量 | 检索命中率 | 引用准确率 | 答案可用率 | 拒答正确率 | 关键说明 |
|---|---|---|---|---|---|---|
| **V1 阶段 6（`base52`）** | 52 (40+12) | `100.0%` (40/40) | `100.0%` (38/38) | `100.0%` (38/38) | `100.0%` (12/12) | `KeywordNormFloor=45.0` + `0.75/0.25` 权重 + 句级引文校验；仅 2 题保守误拒（详情见 [docs/iterations.md](docs/iterations.md)） |
| **V2 阶段 11（`base52` G1）** | 52 (40+12) | `100.0%` (40/40) | `100.0%` (40/40) | `100.0%` (40/40) | `100.0%` (12/12) | 误拒降为 0，100% 全指标达标（详情见 [docs/v2_iterations.md](docs/v2_iterations.md)） |
| **V2 阶段 11（`holdout` G1）** | 39 (23+16) | `95.7%` (22/23) | `100.0%` (23/23) | `100.0%` (23/23) | `100.0%` (16/16) | 首次在对抗盲测中守住 `near_miss_refusal = 13/13 (100%)`（详情见 [docs/v2_iterations.md](docs/v2_iterations.md)） |
| **V3 阶段 13 实测（`base52`）** | 52 (40+12) | **`100.0%` (40/40)** | **`100.0%` (40/40)** | 线下人工核验 | **`100.0%` (12/12)** | 合入多项目隔离、隐藏夹具与会话持久化后零退化（误拒 `0`） |
| **V3 阶段 13 实测（`holdout`）** | 39 (23+16) | **`95.7%` (22/23)** | **`100.0%` (23/23)** | 线下人工核验 | **`100.0%` (16/16)** | **`near_miss_refusal = 13/13 (100.0%)`**，误拒 `0`，仅 `q#77` 跨段对比题未进 Top5 |
| **V3 阶段 13 实测（`feedback`）** | 4 (4+0) | **`100.0%` (4/4)** | **`100.0%` (4/4)** | — | — | 覆盖 `.md` / `.docx` / `.pdf` 演示语料，Doc CI/CD 热更新回归 `2/2` 通过 |

### 2. 关键工程取舍（可解释的设计边界）

1. **为什么保留 `gte-rerank-v2` 为可选开关且默认关闭？**  
   阶段 11 对照实验（见 [docs/v2_iterations.md](docs/v2_iterations.md)）表明，当前双路融合（`0.75` 向量 + `0.25` BM25，`KeywordNormFloor=45.0`）已在 `base52` 达到 40/40 且在 `holdout` 守住 `near_miss_refusal` 13/13；引入 Cross-Encoder 重排后虽然召回过 `holdout` 单题，但在无原词语义改写题（`base52` `q47`）上出现 `-2.5%` 语义负迁移。遵循控制变量与零退化红线，默认保持纯双路融合，重排能力通过 `RERANK_BASE_URL` 按需启用。
2. **为什么默认启动校验与 `--setup-ui` 严格分层？**  
   生产命令行启动（`config.Load()`）严格校验全部 5 个环境变量，防止带病启动；仅当显式传入 `--setup-ui` 或设置 `ALLOW_WEB_SETUP=true` 时允许缺失模型配置启动进入网页引导态（仍强制 `MYSQL_DSN` 就绪），且网页提交的配置**只写进程内存**并立刻重建运行时客户端，绝不向本地文件或代码仓写入凭据。
3. **为什么线上统计严格采用「口径 B」？**  
   真实线上用户提问没有预先标注的标准切片（`gold_chunk_id`）和 `should_refuse` 真值，系统**绝不伪造“线上 100% 命中/拒答正确率”**：
   - 中栏头部**「当前会话」**仅展示客观可测的引用准确率（`citation_ok`）、用户主动点选 👍/👎 的反馈可用率（无评价时显示 `--`）与真实触发拒答次数；
   - 「盲区与反馈」中间栏底部右对齐的**「全部提问」**直接从 MySQL `qa_turns` 聚合全量真实线上轮次，与离线回归 `eval_runs` 及「运行 feedback 活评测」严格隔离。

---

## 五、常用接口一览

| 接口 | 说明 |
|---|---|
| `GET /` | 嵌入式单页工作台（零外部 CDN，禁用 `innerHTML`） |
| `GET /api/setup/status` / `POST /api/setup` | 内存态模型配置状态查询（脱敏无密钥）与应用 |
| `POST /api/ask` | 在线问答（支持 `project_slug` / `document_ids` / `compare` / `session_id`） |
| `GET/PATCH/DELETE /api/sessions(/:id)` | 持久化任务列表、详情恢复、置顶、重命名与级联删除 |
| `GET /api/stats/online` | 口径 B 全量真实线上提问统计聚合 |
| `GET/POST /api/projects` | 可见项目查询（默认隐藏 `is_hidden=1` 评测夹具）与项目空间创建 |
| `GET/POST/PATCH/DELETE /api/documents*` | 多格式上传入库、单篇全文预览、`overwrite=true` 覆盖触发 Doc CI/CD、状态与替代版本治理 |
| `GET/PATCH /api/gaps(/:id)` | 知识盲区雷达筛选、一键直达补录与状态流转 |
| `GET/POST /api/feedback` / `POST /api/feedback/:id/promote` | 用户 👍/👎 反馈提交与审阅转正至 `feedback` 评测集 |
| `POST /api/eval/run?set=base52\|holdout\|feedback` / `POST /api/eval/mark` | 金标评测批跑与人工 `usable` 核验落库 |

---

## 六、后续演进规划（Roadmap）

1. **可解释的多轮指代消解**：基于 `qa_turns` 历史轮次做轻量检索词补全，并在界面透明展示改写后的检索词，保持单步可验证与单轮评测基线零干扰。
2. **主动跨文档冲突巡检**：在「资料库治理」中针对高相似度 `active` 切片对自动比对数值、端口、时间与审批口径冲突，从被动拦截升级为主动排雷。
3. **飞书机器人 & MCP / CLI Skill 接入**：复用现有 `/api/ask` 与 `/api/gaps` 契约，支持飞书群 `@知识库助手` 卡片交互及 IDE 端 MCP 调用，进一步缩短日常协作反馈闭环。
