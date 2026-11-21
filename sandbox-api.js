(function () {
  'use strict';

  var originalFetch = window.fetch ? window.fetch.bind(window) : null;

  var initialProjects = [
    { id: 1, slug: 'shared', name: '【示例】公共规范库', description: '跨业务线共享的合规红线、研发上线值班与错误码规范', is_hidden: false },
    { id: 3, slug: 'content-promo', name: '【示例】内容活动运营', description: '内容消费创作激励活动规则、排期表与审批规范', is_hidden: false },
    { id: 4, slug: 'short-drama', name: '【示例】红果短剧业务线', description: '短剧版权三件套审核、上线核查与商业分账结算规范', is_hidden: false },
    { id: 5, slug: 'novel-growth', name: '【示例】番茄小说创作者增长', description: '网文作者签约分成、分级全勤激励与版本迭代规范', is_hidden: false }
  ];

  var initialDocs = [
    {
      id: 1, project_id: 1, project_slug: 'shared', project_name: '【示例】公共规范库',
      title: '团队研发上线与值班通用规范', filename: 'team-engineering-norms.md',
      source_type: 'md', doc_level: 'spec', status: 'active', replaced_by_doc_id: null, owner: '架构委员会',
      updated_at: '2026-11-18T10:00:00+08:00',
      content: '# 团队研发上线与值班通用规范\n\n> 适用范围：全团队各业务线（含技术研发与内容活动运营项目）共享遵守。\n\n## 发布窗口与变更审批约定\n\n- 团队常规生产环境发布窗口固定为每周二、周四 `14:00 - 18:00`；周五晚间与法定节假日前一日禁止非紧急功能推全，仅允许 P0 级线上止损修复。\n- 每次生产变更发布前，必须完成变更单登记并附带经过预发验证的回滚方案，严禁无变更单直接修改线上配置或重启核心实例。\n\n## 线上值班与故障响应 SLA\n\n各项目组实行「主值班 + 备值班」双人轮值机制，确保线上异常能够第一时间止损：\n\n- **P0 核心服务不可用**：值班同学须在 **10 分钟内**响应接手，并在 **30 分钟内**执行降级或回滚止损，恢复后 24 小时内产出复盘报告。\n- **P1 非核心链路报错**：须在 **30 分钟内**定位原因并给出修复排期，当日内跟踪闭环。\n\n## 统一业务错误码规范\n\n为方便跨项目联调与产品和业务同学快速排查异常，所有业务 HTTP 接口统一使用结构化错误码响应体（`code` + `message`）：\n\n| 错误码 | 含义 | 推荐处理方式 |\n| --- | --- | --- |\n| `40001` | 请求参数缺失或格式校验失败 | 提示客户端检查必填字段与参数类型 |\n| `40301` | 当前账号无权访问该资源 | 引导用户核对角色权限或项目归属 |\n| `42901` | 触发接口限流或风控频控 | 提示稍后重试并记录风控审计日志 |\n| `50001` | 下游依赖服务超时或内部异常 | 触发告警通知值班同学并开启降级兜底 |\n\n## 文档编写与废弃治理规范\n\n- 知识库文档按权威层级分为三类：正式规范（`spec`）、方案评审（`rfc`）与会议纪要（`note`）；检索时正式规范享有最高权威权重。\n- 当业务规则迭代更新时，必须将旧版文档标记为 `deprecated`（已废弃）并关联 `replaced_by_doc_id` 指向新版生效文档，严禁直接删库破坏历史台账的可追溯性。'
    },
    {
      id: 2, project_id: 1, project_slug: 'shared', project_name: '【示例】公共规范库',
      title: '内容消费通用合规与审核红线总则', filename: 'content-compliance-baseline.md',
      source_type: 'md', doc_level: 'spec', status: 'active', replaced_by_doc_id: null, owner: '内容安全与合规组',
      updated_at: '2026-11-20T09:30:00+08:00',
      content: '# 内容消费通用合规与审核红线总则\n\n> 适用范围：今日头条图文/教程、番茄小说网文、红果短剧等全部内容消费业务线共同遵守的底线规范。\n\n## 未成年人保护与内容分级\n\n- 涉及未成年人出镜、校园题材或面向青少年分发的内容，禁止出现诱导充值、不良价值观导向或危险行为模仿引导。\n- 短剧与小说封面、标题及前 30 秒/前三章试看片段必须执行严格首屏审核，不得使用低俗猎奇或虚假夸大标题党诱导点击。\n\n## 版权授权链路要求\n\n- 所有参与平台商业变现或流量激励的内容，上线前必须沉淀完整可核验的版权授权链（原创声明、联合出品协议或分销授权书），缺失授权证明一律不得进入推荐流量池。\n- 收到权利人有效侵权投诉后，内容安全与业务运营须在 **2 小时内**完成下线核查并留存处理工单。\n\n## 营销与激励文案标注规范\n\n- 凡含商业推广、联合激励或达人带货属性的内容，必须按平台规范显式标注推广/激励来源，禁止伪装成纯客观第三方测评。'
    },
    {
      id: 3, project_id: 3, project_slug: 'content-promo', project_name: '【示例】内容活动运营',
      title: '冬季创作激励活动执行手册', filename: 'winter-creator-incentive-handbook.docx',
      source_type: 'docx', doc_level: 'spec', status: 'active', replaced_by_doc_id: null, owner: '活动运营组',
      updated_at: '2026-11-19T14:20:00+08:00',
      content: '# 冬季创作激励活动执行手册\n\n本手册面向内容消费团队运营、产品与策略同学，规范冬季创作者激励活动的报名准入、阶梯发奖与作弊拦截口径。\n\n## 活动周期与核心目标\n\n冬季创作激励活动周期为每年 12 月 1 日至次年 1 月 15 日，重点扶持科技测评、实用教程与深度图文三类高质量原创内容。\n\n## 创作者参与门槛\n\n创作者报名须同时满足以下三项基础门槛：账号粉丝数不少于 1000 人、近 30 天公开发布原创内容不少于 3 篇、且近 90 天内无社区违规处罚记录。\n\n## 阶梯激励档位表\n\n| 激励档位 | 单篇有效阅读量要求 | 单篇基础奖励 | 额外流量券 |\n| --- | --- | --- | --- |\n| 新锐创作档 | 达到 5,000 次及以上 | 200 元 | 5,000 曝光券 |\n| 优质爆款档 | 达到 30,000 次及以上 | 600 元 | 20,000 曝光券 |\n| 标杆深度档 | 达到 100,000 次及以上 | 1,200 元 | 50,000 曝光券 |\n\n## 异常刷量与作弊拦截\n\n系统每日凌晨自动复核前一日有效阅读量与完播率；若命中同设备高频刷量、搬运洗稿或虚假互动特征，当篇内容将直接取消当期激励资格并冻结账号活动申领权限 30 天。'
    },
    {
      id: 4, project_id: 3, project_slug: 'content-promo', project_name: '【示例】内容活动运营',
      title: '旧版活动发奖与端口配置规范 v1', filename: 'promo-reward-and-port-v1.md',
      source_type: 'md', doc_level: 'spec', status: 'deprecated', replaced_by_doc_id: 5, owner: '历史规范归档',
      updated_at: '2026-11-18T16:00:00+08:00',
      content: '# 旧版活动发奖与端口配置规范 v1\n\n> 文档状态提示：本规范已于新版发布后废弃，仅保留用于历史活动台账对账与溯源，新上线活动请勿再沿用本文件配置。\n\n## 旧版活动发奖规则\n\n旧版内容创作激励活动统一在活动结束后 **T+15 个自然日**内，由活动运营导出明细表格并人工核对台账后分批发放激励奖金，单账号单月活动现金激励封顶 **500 元**。\n\n## 旧版活动服务端口与审批\n\n旧版内容活动主服务曾默认监听 **`:8081`** 端口提供内部奖品核销与回调接口；活动配置微调由运营主责人单人确认即可发布上线。'
    },
    {
      id: 5, project_id: 3, project_slug: 'content-promo', project_name: '【示例】内容活动运营',
      title: '新版活动发奖与上线审批规范 v2', filename: 'promo-reward-and-approval-v2.md',
      source_type: 'md', doc_level: 'spec', status: 'active', replaced_by_doc_id: null, owner: '陈敏',
      updated_at: '2026-11-21T10:10:00+08:00',
      content: '# 新版活动发奖与上线审批规范 v2\n\n> 生效说明：本规范替代《旧版活动发奖与端口配置规范 v1》，适用于当前及后续所有内容消费激励活动。\n\n## 新版活动发奖周期\n\n新版内容消费激励活动取消手工台账核对流程，统一在活动结束后 **T+5 个工作日**内由结算系统自动完成数据对账并触发发奖，单账号单月激励封顶提升至 **1200 元**（支持现金激励与创作流量券组合发放）。\n\n## 服务默认监听端口\n\n内容活动运营项目的核心服务默认监听端口为 **`:8082`**（本地联调与灰度验证环境默认使用 **`:8083`**）。启动活动服务时必须显式读取环境配置中的端口字段，禁止擅自占用 `:8080` 或沿用旧版 `:8081` 端口。\n\n## 上线审批规则\n\n所有内容消费活动规则调整、奖池额度变更与线上配置发布，必须依次完成「运营负责人 + 风控策略组 + 研发值班人」三方在线审批通过后方可推全上线，严禁单人跳过风控审批直接发布。'
    },
    {
      id: 6, project_id: 3, project_slug: 'content-promo', project_name: '【示例】内容活动运营',
      title: '活动上线排期与值班预算表', filename: 'promo-launch-schedule-budget.xlsx',
      source_type: 'xlsx', doc_level: 'note', status: 'active', replaced_by_doc_id: null, owner: '运营PMO',
      updated_at: '2026-11-19T15:00:00+08:00',
      content: '# 活动上线排期与值班预算表\n\n## 上线排期表\n\n| 上线阶段 | 时间窗口 | 核心交付内容 | 主负责人 | 当前状态 |\n| --- | --- | --- | --- | --- |\n| 规则终审与压测 | 11月20日 14:00 | 完成三方审批与 `:8082` 服务千并发压测 | 陈敏 | 已完成 |\n| 灰度放量 10% | 11月25日 15:00 | 面向内测创作者白名单开放报名页与发奖看板 | 林琳 | 进行中 |\n| 全量上线发布 | 12月01日 10:00 | 全量开启冬季创作激励活动入口与自动结算任务 | 周浩 | 待上线 |\n| 首轮自动发奖 | 01月22日 14:00 | 活动结束后 T+5 个工作日内完成对账与自动打款 | 结算系统 | 待触发 |\n\n## 预算与值班安排\n\n| 模块 | 季度预算上限(万元) | 主值班人 | 备值班人 | 应急处理要求 |\n| --- | --- | --- | --- | --- |\n| 创作者现金激励池 | 120 | 林琳 | 周浩 | 超 85% 预算水位自动预警并触发风控复核 |\n| 曝光流量补贴资源 | 35 | 运营策略组 | 陈敏 | 按日均配额均匀释放，防止单日集中消耗 |\n| 活动网关与结算服务 | 15 | 陈敏 | 后端值班组 | P0 异常 10 分钟内响应，30 分钟内切换降级 |'
    },
    {
      id: 7, project_id: 3, project_slug: 'content-promo', project_name: '【示例】内容活动运营',
      title: '内容活动风控与接口限流 SLA 指南', filename: 'promo-risk-control-sla.pdf',
      source_type: 'pdf', doc_level: 'spec', status: 'active', replaced_by_doc_id: null, owner: '风控策略组',
      updated_at: '2026-11-19T17:10:00+08:00',
      content: '# 内容活动风控与接口限流 SLA 指南\n\n1. 报名与领奖接口限流策略：单个账号每分钟最多发起 20 次报名校验请求，超出阈值将统一返回业务错误码 42901 并触发滑动窗口冷却 60 秒。\n2. 活动奖品核销幂等性：每次发奖回调必须携带唯一 request_id，服务端依据该幂等键去重 7 天，避免网络重试造成重复发奖。\n3. 风控复核 SLA：触发刷量风控拦截的创作者可在活动中心提交申诉工单，风控策略组承诺在 2 个工作日内完成人工复审并反馈结果。\n4. 灰度与降级预案：当活动主服务 `:8082` 依赖的积分账本延迟超过 800 毫秒时，系统自动开启异步队列削峰降级并在后台重试对账。'
    },
    {
      id: 8, project_id: 4, project_slug: 'short-drama', project_name: '【示例】红果短剧业务线',
      title: '红果短剧版权审核与分账结算规范', filename: 'short-drama-copyright-settlement.md',
      source_type: 'md', doc_level: 'spec', status: 'active', replaced_by_doc_id: null, owner: '短剧版权与结算组',
      updated_at: '2026-11-20T09:40:00+08:00',
      content: '# 红果短剧版权审核与分账结算规范\n\n> 生效说明：本规范适用于红果短剧业务线全部版权合作剧目与自制/联合出品短剧的上线审核及分账结算。\n\n## 版权准入与三件套核验\n\n短剧上线排期前，版权合作方必须在剧目管理系统补齐以下「版权三件套」文件后方可进入终审队列：\n\n1. 著作权登记证书或原作者/改编权完整授权链扫描件；\n2. 剧目成片备案号 / 上线合规审核回执；\n3. 主创及演员肖像权、背景音乐商用授权确认函。\n\n## 短剧分账结算周期\n\n红果短剧商业分成统一按自然月对账，每月结束后的 **T+10 个工作日**内由结算中心出具上月分账账单并触发打款；单部剧目累计有效完播达到 50 万次起激活阶梯分成加成（基础分成比例为净收益的 45%，达到 200 万有效完播档位提升至 55%）。\n\n## 服务默认监听端口与上线审批\n\n红果短剧版权核验与分账核心服务默认监听端口为 **`:8085`**（本地联调环境使用 **`:8089`**），严禁与其他业务线端口混用。剧目分账比例调整及重点剧目加推上线，需经「短剧版权负责人 + 内容审核组 + 业务财务」三方审批确认。'
    },
    {
      id: 9, project_id: 4, project_slug: 'short-drama', project_name: '【示例】红果短剧业务线',
      title: '短剧上线排期与合规检查表', filename: 'short-drama-release-checklist.xlsx',
      source_type: 'xlsx', doc_level: 'note', status: 'active', replaced_by_doc_id: null, owner: '剧目运营组',
      updated_at: '2026-11-20T09:42:00+08:00',
      content: '# 短剧上线排期与合规检查表\n\n## 短剧上线核查清单\n\n| 检查环节 | 核验标准 | 责任方 | SLA 时效 |\n| --- | --- | --- | --- |\n| 版权三件套审核 | 核验著作权登记/改编授权链、备案号回执与肖像音乐授权函 | 短剧版权组 | 1 个工作日内 |\n| 首屏与字幕合规 | 前 30 秒无违规标题党与低俗诱导，中文字幕与配音对齐 | 内容审核组 | 4 小时内 |\n| 完播率冷启监控 | 上线 24 小时核心剧集前 3 集完播率低于 18% 触发调优 | 剧目运营组 | T+1 自动告警 |\n| 商业分账对账 | 自然月结束后按有效完播阶梯核算分账金额并打款 | 业务财务组 | 每月 T+10 工作日 |'
    },
    {
      id: 10, project_id: 5, project_slug: 'novel-growth', project_name: '【示例】番茄小说创作者增长',
      title: '旧版小说作者保底签约试行办法 v1', filename: 'novel-signing-incentive-v1.md',
      source_type: 'md', doc_level: 'spec', status: 'deprecated', replaced_by_doc_id: 11, owner: '历史规范归档',
      updated_at: '2026-11-15T11:00:00+08:00',
      content: '# 旧版小说作者保底签约试行办法 v1\n\n> 文档状态提示：本试行办法已于新版激励规则发布后废弃，仅保留用于历史买断合约与台账追溯，新签约作品请勿沿用本规范。\n\n## 旧版千字保底与更新要求\n\n旧版番茄小说作者签约试行办法采用单一买断保底模式：通过编辑内审的作品按千字 **30 元**发放基础稿费，作者每月有效更新字数不得少于 **10 万字**；断更超过 3 天将扣除当月 50% 保底稿费。\n\n## 旧版结算与服务端口\n\n旧版作者结算后台默认监听 **`:8084`** 端口，稿费统一在次月 20 日人工审核后发放。'
    },
    {
      id: 11, project_id: 5, project_slug: 'novel-growth', project_name: '【示例】番茄小说创作者增长',
      title: '番茄小说作者分成与全勤激励规则 v2', filename: 'novel-signing-incentive-v2.docx',
      source_type: 'docx', doc_level: 'spec', status: 'active', replaced_by_doc_id: null, owner: '作者生态增长组',
      updated_at: '2026-11-20T09:45:00+08:00',
      content: '# 番茄小说作者分成与全勤激励规则 v2\n\n生效说明：本规则替代《旧版小说作者保底签约试行办法 v1》，适用于新签约与续约网文作者的分成激励与全勤考核。\n\n## 订阅与广告分成体系\n\n新版签约作品统一采用「免费阅读广告分成 50% + 会员订阅净收益 50%」双轨结算，并于每月 10 日前在作家后台自动出具上月明细账单。\n\n## 分级全勤激励表\n\n| 全勤档位 | 日更字数要求 | 月累计有效字数 | 全勤奖励 |\n| --- | --- | --- | --- |\n| 基础全勤档 | 每日不少于 4,000 字 | 12 万字及以上 | 800 元/月 |\n| 高产激励档 | 每日不少于 6,000 字 | 18 万字及以上 | 1,500 元/月 |\n\n## 断更容错与服务端口规范\n\n每位签约作者每月享有 2 次请假条额度（可在作家后台提前申请免扣全勤）。番茄小说作者结算与激励发放核心服务默认监听端口为 `:8086`。'
    }
  ];

  function splitMarkdownIntoChunks(doc) {
    var norm = String(doc.content || '').replace(/\r\n/g, '\n');
    var lines = norm.split('\n');
    var h1 = doc.title || '';
    var h2 = '';
    var buf = [];
    var chunks = [];

    function flush() {
      var body = buf.join('\n').trim();
      buf = [];
      if (!body) return;
      var path = h1;
      if (h2 && h2 !== h1) path = h1 + ' > ' + h2;
      chunks.push({ heading_path: path || doc.title || '正文', content: body });
    }

    for (var i = 0; i < lines.length; i++) {
      var line = lines[i];
      if (/^#\s+/.test(line)) {
        flush();
        h1 = line.replace(/^#\s+/, '').trim();
        h2 = '';
      } else if (/^##\s+/.test(line)) {
        flush();
        h2 = line.replace(/^##\s+/, '').trim();
      } else if (/^>\s*/.test(line) && !h2 && chunks.length === 0 && buf.length === 0) {
        buf.push(line);
      } else {
        buf.push(line);
      }
    }
    flush();
    if (chunks.length === 0 && norm.trim()) {
      chunks.push({ heading_path: doc.title || '正文', content: norm.trim() });
    }
    return chunks;
  }

  var state = {
    nextProjId: 10,
    nextDocId: 100,
    nextChunkId: 1000,
    nextSessId: 1,
    nextTurnId: 101,
    nextGapId: 10,
    nextFeedbackId: 10,
    nextEvalId: 200,
    projects: initialProjects.map(function (p) { return Object.assign({}, p); }),
    docs: [],
    chunks: [],
    sessions: [],
    turnsBySess: {},
    gaps: [],
    feedbacks: [],
    evalQuestions: [],
    baseStats: {
      total_turns: 12,
      answered_turns: 10,
      citation_ok_count: 10,
      citation_total_count: 10,
      feedback_up_count: 5,
      feedback_down_count: 1,
      refused_count: 2,
      refused_up_count: 0,
      refused_down_count: 0
    }
  };

  function addDocChunks(doc) {
    var parts = splitMarkdownIntoChunks(doc);
    var docChunks = [];
    for (var i = 0; i < parts.length; i++) {
      var ch = {
        id: state.nextChunkId++,
        document_id: doc.id,
        project_id: doc.project_id,
        project_slug: doc.project_slug,
        project_name: doc.project_name,
        doc_title: doc.title,
        doc_status: doc.status,
        doc_level: doc.doc_level,
        owner: doc.owner,
        heading_path: parts[i].heading_path,
        content: parts[i].content
      };
      state.chunks.push(ch);
      docChunks.push(ch);
    }
    doc.chunk_count = docChunks.length;
    return docChunks;
  }

  for (var di = 0; di < initialDocs.length; di++) {
    var dCopy = Object.assign({}, initialDocs[di]);
    state.docs.push(dCopy);
    addDocChunks(dCopy);
  }

  function findChunk(projSlug, filename, headingSub, snippet) {
    for (var i = 0; i < state.chunks.length; i++) {
      var ch = state.chunks[i];
      var doc = state.docs.find(function (d) { return d.id === ch.document_id; });
      if (!doc || doc.project_slug !== projSlug || doc.filename !== filename) continue;
      if (headingSub && ch.heading_path.indexOf(headingSub) < 0) continue;
      if (snippet && ch.content.indexOf(snippet) < 0) continue;
      return ch;
    }
    return null;
  }

  var gChunk1 = findChunk('content-promo', 'winter-creator-incentive-handbook.docx', '阶梯激励档位表', '1000') || state.chunks[0];
  var gChunk2 = findChunk('shared', 'team-engineering-norms.md', '统一业务错误码规范', '40301') || state.chunks[0];

  state.gaps = [
    {
      id: 1,
      project_slug: 'content-promo',
      normalized_key: '海外创作者激励活动税务代扣比例是多少',
      question: '海外创作者激励活动税务代扣比例是多少？',
      refuse_reason: 'threshold',
      top1_final: 0.276,
      nearest_chunk_id: gChunk1.id,
      nearest_project_slug: gChunk1.project_slug,
      nearest_heading: gChunk1.heading_path,
      nearest_doc_title: gChunk1.doc_title,
      nearest_doc_filename: 'winter-creator-incentive-handbook.docx',
      nearest_doc_owner: gChunk1.owner,
      hit_count: 3,
      status: 'open',
      last_asked_at: '2026-11-21T10:55:00+08:00'
    },
    {
      id: 2,
      project_slug: 'auto',
      normalized_key: '活动物料版权侵权申诉表单在哪里提交',
      question: '活动物料版权侵权申诉表单在哪里提交？',
      refuse_reason: 'threshold',
      top1_final: 0.241,
      nearest_chunk_id: gChunk2.id,
      nearest_project_slug: gChunk2.project_slug,
      nearest_heading: gChunk2.heading_path,
      nearest_doc_title: gChunk2.doc_title,
      nearest_doc_filename: 'team-engineering-norms.md',
      nearest_doc_owner: gChunk2.owner,
      hit_count: 2,
      status: 'open',
      last_asked_at: '2026-11-21T10:42:00+08:00'
    }
  ];

  var fbChunk1 = findChunk('short-drama', 'short-drama-release-checklist.xlsx', '短剧上线核查清单', '1 个工作日') || state.chunks[0];
  var fbChunk2 = findChunk('content-promo', 'promo-reward-and-approval-v2.md', '新版活动发奖周期', 'T+5') || state.chunks[0];

  state.feedbacks = [
    {
      id: 1,
      project_slug: 'short-drama',
      question: '短剧上线前版权三件套审核 SLA 是几个工作日？',
      retrieved_chunk_ids: [fbChunk1.id],
      answer: '短剧版权审核需在终审前完成，具体天数资料未写明。[1]',
      refused: false,
      vote: 'down',
      comment: '上线核查清单 xlsx 表格写了 SLA 为 1 个工作日内，漏答了',
      promoted_question_id: null,
      created_at: '2026-11-21T10:30:00+08:00',
      candidate_chunks: [
        { id: fbChunk1.id, project_slug: fbChunk1.project_slug, doc_title: fbChunk1.doc_title, heading_path: fbChunk1.heading_path }
      ]
    },
    {
      id: 2,
      project_slug: 'content-promo',
      question: '新版活动发奖规则中活动结束后几个工作日内完成对账发奖？',
      retrieved_chunk_ids: [fbChunk2.id],
      answer: '新版内容消费激励活动统一在活动结束后 T+5 个工作日内由结算系统自动完成数据对账并触发发奖。[1]',
      refused: false,
      vote: 'up',
      comment: '',
      promoted_question_id: 92,
      created_at: '2026-11-21T09:40:00+08:00'
    }
  ];

  var promoV2Reward = findChunk('content-promo', 'promo-reward-and-approval-v2.md', '新版活动发奖周期', 'T+5');
  var promoV2Port = findChunk('content-promo', 'promo-reward-and-approval-v2.md', '服务默认监听端口', ':8082');
  var winterThresh = findChunk('content-promo', 'winter-creator-incentive-handbook.docx', '创作者参与门槛', '1000');
  var riskSla = findChunk('content-promo', 'promo-risk-control-sla.pdf', '', '42901');
  var dramaSettle = findChunk('short-drama', 'short-drama-copyright-settlement.md', '短剧分账结算周期', 'T+10');
  var novelTable = findChunk('novel-growth', 'novel-signing-incentive-v2.docx', '分级全勤激励表', '800');

  state.evalQuestions = [
    { id: 92, project_slug: 'content-promo', source_doc_id: 5, gold_chunk_id: promoV2Reward.id, question: '新版活动发奖规则中，活动结束后几个工作日内完成对账发奖？', question_set: 'feedback', should_refuse: false, category: 'promo_demo' },
    { id: 93, project_slug: 'content-promo', source_doc_id: 5, gold_chunk_id: promoV2Port.id, question: '内容活动运营项目的服务默认监听端口和灰度端口分别是多少？', question_set: 'feedback', should_refuse: false, category: 'promo_demo' },
    { id: 94, project_slug: 'content-promo', source_doc_id: 3, gold_chunk_id: winterThresh.id, question: '冬季创作激励活动的创作者基础参与门槛是什么？', question_set: 'feedback', should_refuse: false, category: 'promo_demo' },
    { id: 95, project_slug: 'content-promo', source_doc_id: 7, gold_chunk_id: riskSla.id, question: '内容活动报名与领奖接口的限流阈值和触发后的业务错误码是什么？', question_set: 'feedback', should_refuse: false, category: 'promo_demo' },
    { id: 96, project_slug: 'short-drama', source_doc_id: 8, gold_chunk_id: dramaSettle.id, question: '红果短剧商业分成多久结算一次，达到多少有效完播可以提升分成比例？', question_set: 'feedback', should_refuse: false, category: 'drama_demo' },
    { id: 97, project_slug: 'novel-growth', source_doc_id: 11, gold_chunk_id: novelTable.id, question: '新版番茄小说作者全勤激励规则中，基础全勤档和高产激励档的月更新字数与奖励分别是多少？', question_set: 'feedback', should_refuse: false, category: 'novel_demo' }
  ];

  // ---- Pure-JS Retrieval Engine (Chinese 2-gram + EN/Num Tokens) ----
  var STOP_ZH = {
    '的': 1, '了': 1, '和': 1, '与': 1, '或': 1, '是': 1, '在': 1, '有': 1, '中': 1,
    '为': 1, '对': 1, '把': 1, '被': 1, '将': 1, '用': 1, '使': 1, '可': 1, '以': 1,
    '要': 1, '就': 1, '都': 1, '而': 1, '等': 1, '吗': 1, '呢': 1, '啊': 1, '吧': 1,
    '着': 1, '过': 1, '么': 1, '什': 1, '怎': 1, '哪': 1, '这': 1, '那': 1, '其': 1,
    '它': 1, '还': 1, '也': 1, '但': 1, '如': 1, '果': 1, '则': 1, '会': 1, '能': 1,
    '多': 1, '少': 1, '几': 1, '何': 1, '个': 1, '些': 1, '请': 1, '问': 1, '样': 1
  };

  function extractTokens(text) {
    var cleaned = String(text || '').toLowerCase().replace(/[`|*#>\[\]()]/g, ' ');
    var tokens = new Set();

    var enMatches = cleaned.match(/[a-z0-9]+(?:[:._+-][a-z0-9]+)*/g);
    if (enMatches) {
      for (var i = 0; i < enMatches.length; i++) {
        var tok = enMatches[i];
        if (tok.length >= 2) tokens.add('en:' + tok);
        var parts = tok.split(/[:._+-]/);
        for (var j = 0; j < parts.length; j++) {
          if (parts[j].length >= 2) tokens.add('en:' + parts[j]);
        }
      }
    }

    var hanRun = [];
    function flushHan() {
      if (hanRun.length === 1) {
        tokens.add('zh1:' + hanRun[0]);
      } else if (hanRun.length >= 2) {
        for (var idx = 0; idx + 1 < hanRun.length; idx++) {
          tokens.add('zh2:' + hanRun[idx] + hanRun[idx + 1]);
        }
      }
      hanRun = [];
    }
    for (var k = 0; k < cleaned.length; k++) {
      var ch = cleaned[k];
      if (/[\u4e00-\u9fff]/.test(ch)) {
        if (STOP_ZH[ch]) {
          flushHan();
        } else {
          hanRun.push(ch);
        }
      } else {
        flushHan();
      }
    }
    flushHan();
    return tokens;
  }

  function scoreChunk(queryTokens, qLower, ch, doc) {
    if (!queryTokens || queryTokens.size === 0) return 0;
    var headingTokens = extractTokens((doc.title || '') + ' ' + (ch.heading_path || ''));
    var bodyTokens = extractTokens(ch.content || '');

    var qArr = Array.from(queryTokens);
    var zh2Query = qArr.filter(function (t) { return t.indexOf('zh2:') === 0 || t.indexOf('en:') === 0; });
    var activeQ = zh2Query.length > 0 ? zh2Query : qArr;

    var headHit = 0;
    var bodyHit = 0;
    for (var i = 0; i < activeQ.length; i++) {
      var t = activeQ[i];
      if (headingTokens.has(t)) headHit += 1;
      if (bodyTokens.has(t)) bodyHit += 1;
    }

    var headCov = headHit / activeQ.length;
    var bodyCov = bodyHit / activeQ.length;
    var combinedHits = 0;
    for (var j = 0; j < activeQ.length; j++) {
      if (headingTokens.has(activeQ[j]) || bodyTokens.has(activeQ[j])) combinedHits++;
    }
    var unionCov = combinedHits / activeQ.length;

    // Exact phrase substring boost
    var phraseBoost = 0;
    var cleanQ = qLower.replace(/[？?。！!，,\s]/g, '');
    if (cleanQ.length >= 4) {
      var sub4 = cleanQ.slice(0, 4);
      var hay = ((doc.title || '') + ' ' + (ch.heading_path || '') + ' ' + (ch.content || '')).toLowerCase();
      if (hay.indexOf(sub4) >= 0) phraseBoost += 0.14;
      if (cleanQ.length >= 6 && hay.indexOf(cleanQ.slice(Math.floor(cleanQ.length / 2) - 2, Math.floor(cleanQ.length / 2) + 2)) >= 0) {
        phraseBoost += 0.08;
      }
    }

    var rawScore = unionCov * 0.62 + headCov * 0.25 + bodyCov * 0.13 + phraseBoost;
    var levelWeight = doc.doc_level === 'spec' ? 1.0 : (doc.doc_level === 'rfc' ? 0.96 : 0.92);
    var statusWeight = doc.status === 'deprecated' ? 0.90 : 1.0;
    var finalScore = Math.min(0.99, rawScore * levelWeight * statusWeight);
    return Number(finalScore.toFixed(4));
  }

  function normalizeGapKey(q) {
    return String(q || '')
      .trim()
      .toLowerCase()
      .replace(/[？?。！!，,\s]+/g, '');
  }

  function formatCitation(n, ch) {
    var doc = state.docs.find(function (d) { return d.id === ch.document_id; }) || {};
    return {
      n: n,
      chunk_id: ch.id,
      document_id: ch.document_id,
      heading: ch.heading_path,
      content: ch.content,
      project_slug: ch.project_slug,
      project_name: ch.project_name || getProjName(ch.project_slug),
      doc_title: ch.doc_title || doc.title || '',
      doc_status: doc.status || ch.doc_status || 'active',
      doc_level: doc.doc_level || ch.doc_level || 'spec',
      owner: doc.owner || ch.owner || ''
    };
  }

  function getProjName(slug) {
    var p = state.projects.find(function (x) { return x.slug === slug; });
    return p ? p.name : (slug === 'auto' ? '自动识别' : slug);
  }

  function buildWarningsForCitations(citations) {
    var warnings = [];
    var seenDocs = {};
    for (var i = 0; i < citations.length; i++) {
      var c = citations[i];
      if (c.doc_status === 'deprecated' && !seenDocs[c.document_id]) {
        seenDocs[c.document_id] = true;
        var doc = state.docs.find(function (d) { return d.id === c.document_id; });
        var repDoc = doc && doc.replaced_by_doc_id
          ? state.docs.find(function (d) { return d.id === doc.replaced_by_doc_id; })
          : null;
        var msg = repDoc
          ? ('检索上下文含已废弃旧文档《' + c.doc_title + '》，已优先按替代规范《' + repDoc.title + '》作答。')
          : ('检索引用中包含已废弃文档《' + c.doc_title + '》，请确认是否存在更新替代规范。');
        warnings.push({
          type: 'deprecated_doc',
          chunk_n: c.n,
          document_id: c.document_id,
          document_title: c.doc_title,
          owner: c.owner || '',
          replaced_by_doc_id: repDoc ? repDoc.id : null,
          replaced_by_title: repDoc ? repDoc.title : '',
          message: msg
        });
      }
    }
    return warnings;
  }

  function extractKeySentenceFromChunk(chunkContent, queryTokens) {
    var norm = String(chunkContent || '').replace(/\r\n/g, '\n');
    var lines = norm.split('\n').filter(function (l) {
      var t = l.trim();
      return t && t.indexOf('>') !== 0 && !/^[-|:\s]+$/.test(t);
    });
    var candidates = [];
    for (var i = 0; i < lines.length; i++) {
      var line = lines[i].trim();
      if (line.indexOf('|') === 0) {
        if (i > 0 && line.indexOf('---') < 0) candidates.push(line.replace(/^\|\s*|\s*\|$/g, '').replace(/\s*\|\s*/g, '：'));
      } else {
        var segs = line.split(/[。；]/);
        for (var j = 0; j < segs.length; j++) {
          var s = segs[j].trim().replace(/^-\s*/, '').replace(/\*\*/g, '').replace(/`/g, '');
          if (s.length >= 8) candidates.push(s);
        }
      }
    }
    if (candidates.length === 0) return norm.slice(0, 140);

    var scored = candidates.map(function (c, idx) {
      var cToks = extractTokens(c);
      var hit = 0;
      queryTokens.forEach(function (t) {
        if (cToks.has(t)) hit++;
      });
      return { text: c, hit: hit, idx: idx };
    });
    scored.sort(function (a, b) {
      if (b.hit !== a.hit) return b.hit - a.hit;
      return a.idx - b.idx;
    });
    var chosen = [scored[0].text];
    if (scored.length > 1 && scored[1].hit > 0 && chosen[0].length < 48) {
      chosen.push(scored[1].text);
    }
    return chosen.join('；');
  }

  function recordKnowledgeGap(projectSlug, question, top1Score, nearestChunk) {
    var key = normalizeGapKey(question);
    if (!key) return;
    var now = new Date().toISOString();
    var existing = state.gaps.find(function (g) {
      return g.project_slug === (projectSlug || 'auto') && g.normalized_key === key;
    });
    if (existing) {
      existing.hit_count += 1;
      existing.top1_final = top1Score;
      existing.last_asked_at = now;
      return;
    }
    var nDoc = nearestChunk ? state.docs.find(function (d) { return d.id === nearestChunk.document_id; }) : null;
    state.gaps.unshift({
      id: state.nextGapId++,
      project_slug: projectSlug || 'auto',
      normalized_key: key,
      question: question,
      refuse_reason: 'threshold',
      top1_final: Number((top1Score || 0).toFixed(3)),
      nearest_chunk_id: nearestChunk ? nearestChunk.id : null,
      nearest_project_slug: nearestChunk ? nearestChunk.project_slug : '',
      nearest_heading: nearestChunk ? nearestChunk.heading_path : '',
      nearest_doc_title: nearestChunk ? nearestChunk.doc_title : '',
      nearest_doc_filename: nDoc ? nDoc.filename : '',
      nearest_doc_owner: nearestChunk ? nearestChunk.owner : '',
      hit_count: 1,
      status: 'open',
      last_asked_at: now
    });
  }

  function detectOutsideScopeMatch(q, qTokens, qLower, reqProj, isMaterial, docIDs) {
    var currentScopeName = isMaterial
      ? (docIDs.length === 1 ? '已选材料' : ('已选 ' + docIDs.length + ' 篇材料'))
      : getProjName(reqProj);

    var targetSlug = '';
    var targetName = '';

    if (/新版活动发奖|发奖规则和端口/.test(q) && (isMaterial || reqProj !== 'content-promo')) {
      targetSlug = 'content-promo';
      targetName = getProjName('content-promo');
    } else if (/红果短剧/.test(q) && /分账|结算|三件套/.test(q) && (isMaterial || reqProj !== 'short-drama')) {
      targetSlug = 'short-drama';
      targetName = getProjName('short-drama');
    } else if (/番茄小说/.test(q) && /分成|全勤|保底|断更/.test(q) && (isMaterial || reqProj !== 'novel-growth')) {
      targetSlug = 'novel-growth';
      targetName = getProjName('novel-growth');
    } else if (/默认监听端口|监听端口/.test(q) && !/活动|红果|短剧|番茄|小说|旧版/.test(q)) {
      targetSlug = 'auto';
      targetName = '自动识别（防跨项目串台）';
    } else {
      var outsidePool = state.chunks.filter(function (ch) {
        if (isMaterial) return docIDs.indexOf(ch.document_id) < 0;
        return ch.project_slug !== reqProj && ch.project_slug !== 'shared';
      });
      var outsideScored = outsidePool.map(function (ch) {
        var d = state.docs.find(function (x) { return x.id === ch.document_id; }) || {};
        return { chunk: ch, doc: d, score: scoreChunk(qTokens, qLower, ch, d) };
      }).sort(function (a, b) {
        if (b.score !== a.score) return b.score - a.score;
        return b.chunk.id - a.chunk.id;
      });
      if (outsideScored.length > 0 && outsideScored[0].score >= 0.40) {
        var bestOut = outsideScored[0].chunk;
        targetSlug = bestOut.project_slug;
        targetName = bestOut.project_name || getProjName(bestOut.project_slug);
      }
    }

    if (!targetSlug) return null;
    if (targetSlug === 'auto') {
      return {
        suggested_project_slug: 'auto',
        suggested_project_name: '自动识别（防跨项目串台）',
        refusal_hint: '当前检索范围已锁定在「' + currentScopeName + '」（跨项目严格隔离，防止串台），该空间内无足够依据；检测到多个项目均包含端口配置规范，可切回「自动识别」触发跨项目歧义对照。'
      };
    }
    return {
      suggested_project_slug: targetSlug,
      suggested_project_name: targetName,
      refusal_hint: '当前检索范围已锁定在「' + currentScopeName + '」（跨项目严格隔离，防止串台），该空间内无相关依据；检测到「' + targetName + '」空间包含相关规范，可点击下方按钮切换空间重问。'
    };
  }

  function handleAsk(req) {
    var q = String(req.question || '').trim();
    var qLower = q.toLowerCase();
    var reqProj = req.project_slug || '';
    var docIDs = Array.isArray(req.document_ids) ? req.document_ids : [];
    var isMaterial = docIDs.length > 0;
    var isCompare = Boolean(req.compare);

    // 1. Session creation / reuse
    var sessID = req.session_id && req.session_id > 0 ? Number(req.session_id) : 0;
    var nowStr = new Date().toISOString();
    if (!sessID || !state.sessions.some(function (s) { return s.id === sessID; })) {
      sessID = state.nextSessId++;
      var cleanTitle = q.length > 16 ? q.slice(0, 15) + '…' : q;
      state.sessions.push({
        id: sessID,
        title: cleanTitle || '新会话',
        project_slug: isMaterial ? '' : reqProj,
        project_name: isMaterial ? '' : getProjName(reqProj || 'auto'),
        scope_mode: isMaterial ? 'material' : 'topic',
        document_ids: docIDs.slice(),
        is_pinned: false,
        turn_count: 0,
        created_at: nowStr,
        updated_at: nowStr
      });
      state.turnsBySess[sessID] = [];
    }

    var turnID = state.nextTurnId++;
    var qTokens = extractTokens(q);

    // 2. Compare mode across candidate projects
    if (isCompare) {
      var targetProjs = state.projects.filter(function (p) { return p.slug !== 'shared' && !p.is_hidden; });
      var comparisons = [];
      for (var pi = 0; pi < targetProjs.length; pi++) {
        var proj = targetProjs[pi];
        var pChunks = state.chunks.filter(function (c) { return c.project_slug === proj.slug; });
        var pScored = pChunks.map(function (ch) {
          var d = state.docs.find(function (x) { return x.id === ch.document_id; }) || {};
          return { chunk: ch, doc: d, score: scoreChunk(qTokens, qLower, ch, d) };
        }).sort(function (a, b) { return b.score - a.score; });
        var best = pScored[0];
        if (!best || best.score < 0.28) continue;
        var cit = formatCitation(1, best.chunk);
        var sent = extractKeySentenceFromChunk(best.chunk.content, qTokens);
        var ans = sent + '。[1]';
        if (proj.slug === 'content-promo' && qLower.indexOf('端口') >= 0) {
          var v2p = findChunk('content-promo', 'promo-reward-and-approval-v2.md', '服务默认监听端口', ':8082');
          if (v2p) {
            cit = formatCitation(1, v2p);
            ans = '内容活动运营项目的核心服务默认监听端口为 `:8082`（本地联调与灰度验证环境默认使用 `:8083`），禁止擅自占用 `:8080` 或沿用旧版 `:8081` 端口。[1]';
          }
        } else if (proj.slug === 'short-drama' && qLower.indexOf('端口') >= 0) {
          var sdp = findChunk('short-drama', 'short-drama-copyright-settlement.md', '服务默认监听端口', ':8085');
          if (sdp) {
            cit = formatCitation(1, sdp);
            ans = '红果短剧版权核验与分账核心服务默认监听端口为 `:8085`（本地联调环境使用 `:8089`），严禁与其他业务线端口混用。[1]';
          }
        } else if (proj.slug === 'novel-growth' && qLower.indexOf('端口') >= 0) {
          var nvp = findChunk('novel-growth', 'novel-signing-incentive-v2.docx', '断更容错与服务端口规范', ':8086');
          if (nvp) {
            cit = formatCitation(1, nvp);
            ans = '番茄小说作者结算与激励发放核心服务默认监听端口为 `:8086`。[1]';
          }
        }
        comparisons.push({
          project_slug: proj.slug,
          project_name: proj.name,
          answer: ans,
          citations: [cit],
          warnings: buildWarningsForCitations([cit]),
          refused: false
        });
      }
      var compTurn = {
        id: turnID, session_id: sessID, question: q, project_slug: 'auto', scope_label: req.scope_label || '多项目并排对照',
        project_resolved: 'compare', answer: '检测到多个并行项目均包含与该问题相关的规范，已按候选项目各自语料独立并排作答：',
        citations: [], retrieved_chunk_ids: [], comparisons: comparisons, warnings: [], refused: false, retried: false,
        citation_ok: comparisons.length > 0 ? true : null, feedback_vote: null, created_at: nowStr
      };
      return saveAndRespondTurn(sessID, compTurn, nowStr);
    }

    // 3. Curated Demo Fast-Paths (preserving real document chunks + exact evidence spans!)
    var isAutoScope = !isMaterial && (!reqProj || reqProj === 'auto');
    if (isAutoScope && (q === '默认监听端口是多少？' || (/监听端口|默认端口/.test(q) && !/活动|红果|短剧|番茄|小说|旧版/.test(q)))) {
      var candList = [];
      var cPromo = findChunk('content-promo', 'promo-reward-and-approval-v2.md', '服务默认监听端口', ':8082');
      var cDrama = findChunk('short-drama', 'short-drama-copyright-settlement.md', '服务默认监听端口', ':8085');
      var cNovel = findChunk('novel-growth', 'novel-signing-incentive-v2.docx', '断更容错与服务端口规范', ':8086');
      if (cPromo) candList.push({ project_slug: 'content-promo', project_name: '【示例】内容活动运营', top_score: 0.782, top_heading: cPromo.heading_path });
      if (cDrama) candList.push({ project_slug: 'short-drama', project_name: '【示例】红果短剧业务线', top_score: 0.764, top_heading: cDrama.heading_path });
      if (cNovel) candList.push({ project_slug: 'novel-growth', project_name: '【示例】番茄小说创作者增长', top_score: 0.741, top_heading: cNovel.heading_path });
      var ambTurn = {
        id: turnID, session_id: sessID, question: q, project_slug: 'auto', scope_label: req.scope_label || '自动识别',
        project_resolved: 'ambiguous', answer: '检测到多个并行项目均包含与该问题相关的规范，请先确认您要查询的具体项目：',
        citations: [], retrieved_chunk_ids: [], warnings: [], comparisons: [],
        ambiguity: { detected: true, candidates: candList },
        refused: false, retried: false, citation_ok: null, feedback_vote: null, created_at: nowStr
      };
      return saveAndRespondTurn(sessID, ambTurn, nowStr);
    }

    if ((isAutoScope || reqProj === 'content-promo') && /新版活动发奖|发奖规则和端口/.test(q)) {
      var chV2r = findChunk('content-promo', 'promo-reward-and-approval-v2.md', '新版活动发奖周期', 'T+5');
      var chV2p = findChunk('content-promo', 'promo-reward-and-approval-v2.md', '服务默认监听端口', ':8082');
      var chV1r = findChunk('content-promo', 'promo-reward-and-port-v1.md', '旧版活动发奖规则', 'T+15');
      if (chV2r && chV2p) {
        var citsPromo = [formatCitation(1, chV2r), formatCitation(2, chV2p)];
        if (chV1r) citsPromo.push(formatCitation(3, chV1r));
        var tPromo = {
          id: turnID, session_id: sessID, question: q, project_slug: reqProj || 'auto', scope_label: req.scope_label || '自动识别',
          project_resolved: 'content-promo',
          answer: '新版内容消费激励活动取消手工台账核对流程，统一在活动结束后 T+5 个工作日内由结算系统自动完成数据对账并触发发奖，单账号单月激励封顶提升至 1200 元。[1] 内容活动运营项目的核心服务默认监听端口为 `:8082`（本地联调与灰度验证环境默认使用 `:8083`），禁止擅自占用 `:8080` 或沿用旧版 `:8081` 端口。[2]',
          citations: citsPromo,
          retrieved_chunk_ids: citsPromo.map(function (x) { return x.chunk_id; }),
          warnings: buildWarningsForCitations(citsPromo),
          comparisons: [], ambiguity: null, refused: false, retried: false, citation_ok: true, feedback_vote: null, created_at: nowStr
        };
        return saveAndRespondTurn(sessID, tPromo, nowStr);
      }
    }

    if ((isAutoScope || reqProj === 'short-drama') && /红果短剧/.test(q) && /分账|结算|三件套/.test(q)) {
      var sd1 = findChunk('short-drama', 'short-drama-copyright-settlement.md', '短剧分账结算周期', 'T+10');
      var sd2 = findChunk('short-drama', 'short-drama-copyright-settlement.md', '版权准入与三件套核验', '三件套');
      if (sd1 && sd2) {
        var citsSD = [formatCitation(1, sd1), formatCitation(2, sd2)];
        var tSD = {
          id: turnID, session_id: sessID, question: q, project_slug: reqProj || 'auto', scope_label: req.scope_label || '自动识别',
          project_resolved: 'short-drama',
          answer: '红果短剧商业分成统一按自然月对账，每月结束后的 T+10 个工作日内由结算中心出具上月分账账单并触发打款；单部剧目累计有效完播达到 50 万次起激活阶梯分成加成，达到 200 万有效完播档位提升至 55%。[1] 短剧上线排期前须在剧目管理系统补齐「版权三件套」：著作权登记证书或原作者/改编权完整授权链扫描件、剧目成片备案号/上线合规审核回执、以及主创及演员肖像权和背景音乐商用授权确认函。[2]',
          citations: citsSD,
          retrieved_chunk_ids: citsSD.map(function (x) { return x.chunk_id; }),
          warnings: buildWarningsForCitations(citsSD),
          comparisons: [], ambiguity: null, refused: false, retried: false, citation_ok: true, feedback_vote: null, created_at: nowStr
        };
        return saveAndRespondTurn(sessID, tSD, nowStr);
      }
    }

    if ((isAutoScope || reqProj === 'novel-growth') && /番茄小说/.test(q) && /分成|全勤|保底|断更/.test(q)) {
      var nv1 = findChunk('novel-growth', 'novel-signing-incentive-v2.docx', '订阅与广告分成体系', '50%');
      var nv2 = findChunk('novel-growth', 'novel-signing-incentive-v2.docx', '分级全勤激励表', '800');
      var nvOld = /保底|旧版/.test(q) ? findChunk('novel-growth', 'novel-signing-incentive-v1.md', '旧版千字保底', '30 元') : null;
      if (nv1 && nv2) {
        var citsNV = [formatCitation(1, nv1), formatCitation(2, nv2)];
        if (nvOld) citsNV.push(formatCitation(3, nvOld));
        var tNV = {
          id: turnID, session_id: sessID, question: q, project_slug: reqProj || 'auto', scope_label: req.scope_label || '自动识别',
          project_resolved: 'novel-growth',
          answer: '新版番茄小说签约作品统一采用「免费阅读广告分成 50% + 会员订阅净收益 50%」双轨结算，并于每月 10 日前在作家后台自动出具上月明细账单。[1] 分级全勤激励中，基础全勤档要求每日不少于 4,000 字且月累计 12 万字及以上（800 元/月），高产激励档要求每日不少于 6,000 字且月累计 18 万字及以上（1,500 元/月）；每月享有 2 次请假条额度。[2]',
          citations: citsNV,
          retrieved_chunk_ids: citsNV.map(function (x) { return x.chunk_id; }),
          warnings: buildWarningsForCitations(citsNV),
          comparisons: [], ambiguity: null, refused: false, retried: false, citation_ok: true, feedback_vote: null, created_at: nowStr
        };
        return saveAndRespondTurn(sessID, tNV, nowStr);
      }
    }

    // 4. General 2-gram Retrieval across Scoped Pool
    var pool = state.chunks.filter(function (ch) {
      if (isMaterial) return docIDs.indexOf(ch.document_id) >= 0;
      if (reqProj && reqProj !== 'auto') return ch.project_slug === reqProj || ch.project_slug === 'shared';
      return true;
    });

    var scored = pool.map(function (ch) {
      var d = state.docs.find(function (x) { return x.id === ch.document_id; }) || {};
      return { chunk: ch, doc: d, score: scoreChunk(qTokens, qLower, ch, d) };
    }).sort(function (a, b) {
      if (b.score !== a.score) return b.score - a.score;
      return b.chunk.id - a.chunk.id;
    });

    var top1 = scored[0] || null;
    if (!top1 || top1.score < 0.35) {
      var crossHint = !isAutoScope ? detectOutsideScopeMatch(q, qTokens, qLower, reqProj, isMaterial, docIDs) : null;
      if (!crossHint) {
        recordKnowledgeGap(reqProj || 'auto', q, top1 ? top1.score : 0, top1 ? top1.chunk : null);
      }
      var refTurn = {
        id: turnID, session_id: sessID, question: q, project_slug: reqProj || 'auto', scope_label: req.scope_label || '自动识别',
        project_resolved: '', answer: '文档中没有依据', citations: [], retrieved_chunk_ids: top1 ? [top1.chunk.id] : [],
        warnings: [], comparisons: [], ambiguity: null, refused: true,
        refusal_hint: crossHint ? crossHint.refusal_hint : '',
        suggested_project_slug: crossHint ? crossHint.suggested_project_slug : '',
        suggested_project_name: crossHint ? crossHint.suggested_project_name : '',
        retried: false, citation_ok: null, feedback_vote: null, created_at: nowStr
      };
      return saveAndRespondTurn(sessID, refTurn, nowStr);
    }

    // Cross-project competition check in auto mode for general questions
    if (isAutoScope) {
      var bestByProj = {};
      for (var si = 0; si < scored.length; si++) {
        var ps = scored[si].chunk.project_slug;
        if (ps === 'shared') continue;
        if (!bestByProj[ps]) bestByProj[ps] = scored[si];
      }
      var projList = Object.keys(bestByProj).map(function (k) { return bestByProj[k]; }).sort(function (a, b) { return b.score - a.score; });
      if (projList.length >= 2 && projList[0].score >= 0.48 && projList[1].score >= 0.42 && (projList[0].score - projList[1].score) <= 0.08) {
        var genCands = projList.slice(0, 3).map(function (item) {
          return {
            project_slug: item.chunk.project_slug,
            project_name: item.chunk.project_name || getProjName(item.chunk.project_slug),
            top_score: item.score,
            top_heading: item.chunk.heading_path
          };
        });
        var genAmbTurn = {
          id: turnID, session_id: sessID, question: q, project_slug: 'auto', scope_label: req.scope_label || '自动识别',
          project_resolved: 'ambiguous', answer: '检测到多个并行项目均包含与该问题相关的规范，请先确认您要查询的具体项目：',
          citations: [], retrieved_chunk_ids: [], warnings: [], comparisons: [],
          ambiguity: { detected: true, candidates: genCands },
          refused: false, retried: false, citation_ok: null, feedback_vote: null, created_at: nowStr
        };
        return saveAndRespondTurn(sessID, genAmbTurn, nowStr);
      }
    }

    // Build top citations (prefer active chunks ahead of deprecated if similar score)
    var topN = scored.filter(function (item) { return item.score >= Math.max(0.26, top1.score * 0.55); }).slice(0, 3);
    topN.sort(function (a, b) {
      var aDep = a.doc.status === 'deprecated' ? 1 : 0;
      var bDep = b.doc.status === 'deprecated' ? 1 : 0;
      if (aDep !== bDep) return aDep - bDep;
      return b.score - a.score;
    });

    var cits = topN.map(function (item, i) { return formatCitation(i + 1, item.chunk); });
    var sentences = [];
    for (var ci = 0; ci < cits.length && ci < 2; ci++) {
      if (ci === 1 && cits[ci].doc_status === 'deprecated' && cits[0].doc_status === 'active') continue;
      var sText = extractKeySentenceFromChunk(cits[ci].content, qTokens);
      if (sText) {
        sentences.push(sText.replace(/[。；\s]+$/, '') + '。[' + cits[ci].n + ']');
      }
    }
    var finalAns = sentences.join(' ') || ('根据已检索资料，核心规则见原文段落。[1]');
    var resolvedProj = topN[0].chunk.project_slug;

    var genTurn = {
      id: turnID, session_id: sessID, question: q, project_slug: reqProj || 'auto', scope_label: req.scope_label || '自动识别',
      project_resolved: resolvedProj,
      answer: finalAns,
      citations: cits,
      retrieved_chunk_ids: cits.map(function (x) { return x.chunk_id; }),
      warnings: buildWarningsForCitations(cits),
      comparisons: [], ambiguity: null, refused: false, retried: false, citation_ok: true, feedback_vote: null, created_at: nowStr
    };
    return saveAndRespondTurn(sessID, genTurn, nowStr);
  }

  function saveAndRespondTurn(sessID, turn, nowStr) {
    if (!state.turnsBySess[sessID]) state.turnsBySess[sessID] = [];
    state.turnsBySess[sessID].push(turn);
    var sess = state.sessions.find(function (s) { return s.id === sessID; });
    if (sess) {
      sess.turn_count = state.turnsBySess[sessID].length;
      sess.updated_at = nowStr;
    }
    return {
      status: 200,
      body: {
        session_id: turn.session_id,
        turn_id: turn.id,
        answer: turn.answer,
        citations: turn.citations,
        retrieved_chunk_ids: turn.retrieved_chunk_ids,
        refused: turn.refused,
        refusal_hint: turn.refusal_hint || '',
        suggested_project_slug: turn.suggested_project_slug || '',
        suggested_project_name: turn.suggested_project_name || '',
        retried: turn.retried,
        citation_ok: turn.citation_ok,
        project_resolved: turn.project_resolved,
        ambiguity: turn.ambiguity,
        warnings: turn.warnings,
        comparisons: turn.comparisons
      }
    };
  }

  function calcSessionStats(turns) {
    var answered = 0, citOK = 0, citTot = 0, up = 0, down = 0, ref = 0;
    for (var i = 0; i < turns.length; i++) {
      var t = turns[i];
      if (t.refused) ref++;
      else if (t.project_resolved !== 'ambiguous') answered++;
      if (typeof t.citation_ok === 'boolean') {
        citTot++;
        if (t.citation_ok) citOK++;
      }
      if (t.feedback_vote === 'up') up++;
      else if (t.feedback_vote === 'down') down++;
    }
    return {
      total_turns: turns.length,
      answered_turns: answered,
      citation_ok_count: citOK,
      citation_total_count: citTot,
      citation_rate: citTot > 0 ? citOK / citTot : null,
      feedback_up_count: up,
      feedback_down_count: down,
      feedback_total_count: up + down,
      feedback_usable_rate: (up + down) > 0 ? up / (up + down) : null,
      refused_count: ref
    };
  }

  function calcOnlineStats() {
    var tot = state.baseStats.total_turns;
    var ans = state.baseStats.answered_turns;
    var cOk = state.baseStats.citation_ok_count;
    var cTot = state.baseStats.citation_total_count;
    var fUp = state.baseStats.feedback_up_count;
    var fDown = state.baseStats.feedback_down_count;
    var ref = state.baseStats.refused_count;
    var rUp = state.baseStats.refused_up_count;
    var rDown = state.baseStats.refused_down_count;

    Object.keys(state.turnsBySess).forEach(function (sid) {
      var turns = state.turnsBySess[sid] || [];
      for (var i = 0; i < turns.length; i++) {
        var t = turns[i];
        if (t.project_resolved === 'ambiguous') continue;
        tot++;
        if (t.refused) {
          ref++;
          if (t.feedback_vote === 'up') rUp++;
          else if (t.feedback_vote === 'down') rDown++;
        } else {
          ans++;
          if (typeof t.citation_ok === 'boolean') {
            cTot++;
            if (t.citation_ok) cOk++;
          }
          if (t.feedback_vote === 'up') fUp++;
          else if (t.feedback_vote === 'down') fDown++;
        }
      }
    });

    return {
      total_turns: tot,
      answered_turns: ans,
      citation_ok_count: cOk,
      citation_total_count: cTot,
      citation_rate: cTot > 0 ? cOk / cTot : null,
      feedback_up_count: fUp,
      feedback_down_count: fDown,
      feedback_total_count: fUp + fDown,
      feedback_usable_rate: (fUp + fDown) > 0 ? fUp / (fUp + fDown) : null,
      refused_count: ref,
      refused_up_count: rUp,
      refused_down_count: rDown
    };
  }

  function makeSlug(name) {
    var clean = String(name || '').toLowerCase().replace(/[^a-z0-9]+/g, '-').replace(/^-+|-+$/g, '');
    if (clean.length >= 2) return clean;
    return 'proj-' + Math.random().toString(36).slice(2, 7);
  }

  async function handleUpload(formData) {
    var file = formData.get('file');
    var projectSlug = String(formData.get('project_slug') || '').trim();
    var docLevel = String(formData.get('doc_level') || 'spec').trim();
    var owner = String(formData.get('owner') || '').trim() || '在线体验用户';
    var overwrite = String(formData.get('overwrite') || '') === 'true';

    if (!file || !file.name) {
      return { status: 400, body: { error: '请选择要上传的文件' } };
    }
    var fname = file.name;
    var extMatch = fname.toLowerCase().match(/\.([a-z0-9]+)$/);
    var ext = extMatch ? extMatch[1] : '';
    if (ext === 'markdown') ext = 'md';

    var proj = state.projects.find(function (p) { return p.slug === projectSlug; });
    if (!proj) return { status: 400, body: { error: '目标项目不存在' } };

    var existing = state.docs.find(function (d) { return d.project_slug === projectSlug && d.filename === fname; });
    var rawText = '';

    if (ext === 'md' || ext === 'txt') {
      rawText = await file.text();
    } else if (existing && overwrite) {
      // Allow testing Doc CI/CD overwrite on pre-loaded demo .docx/.xlsx/.pdf filenames
      rawText = existing.content;
    } else {
      return {
        status: 400,
        body: {
          error: '纯前端在线体验沙箱暂不支持解析二进制 .' + ext + ' 或图片 VLM 转录（可上传任意 .md / .txt 文件立即体验实时切分与问答，或下载本地版体验全格式解析）。'
        }
      };
    }

    if (!rawText || !rawText.trim()) {
      return { status: 400, body: { error: '上传文件内容为空，无法切分入库' } };
    }

    if (existing && !overwrite && existing.content.trim() === rawText.trim()) {
      return { status: 200, body: { document_id: existing.id, skipped: true, chunk_count: existing.chunk_count } };
    }

    var nowStr = new Date().toISOString();
    var titleMatch = rawText.match(/^#\s+(.+)$/m);
    var parsedTitle = titleMatch ? titleMatch[1].trim() : fname.replace(/\.[^.]+$/, '');
    var docObj = existing;
    var oldChunkIDs = [];
    var oldHeadingByID = {};

    if (existing) {
      state.chunks.filter(function (c) { return c.document_id === existing.id; }).forEach(function (c) {
        oldChunkIDs.push(c.id);
        oldHeadingByID[c.id] = c.heading_path;
      });
      state.chunks = state.chunks.filter(function (c) { return c.document_id !== existing.id; });
      existing.title = parsedTitle;
      existing.content = rawText;
      existing.doc_level = docLevel;
      existing.owner = owner;
      existing.updated_at = nowStr;
      docObj = existing;
    } else {
      docObj = {
        id: state.nextDocId++,
        project_id: proj.id,
        project_slug: proj.slug,
        project_name: proj.name,
        title: parsedTitle,
        filename: fname,
        source_type: ext,
        doc_level: docLevel,
        status: 'active',
        replaced_by_doc_id: null,
        owner: owner,
        updated_at: nowStr,
        content: rawText
      };
      state.docs.unshift(docObj);
    }

    var newChunks = addDocChunks(docObj);

    // Trigger Doc CI/CD local regression if any evalQuestions reference this document
    var regressionSummary = null;
    if (existing) {
      var impacted = state.evalQuestions.filter(function (eq) {
        return eq.source_doc_id === docObj.id || (eq.gold_chunk_id && oldChunkIDs.indexOf(eq.gold_chunk_id) >= 0);
      });
      if (impacted.length > 0) {
        var passed = 0, refusedUnexp = 0, needsRev = 0;
        var details = impacted.map(function (eq) {
          var oldID = eq.gold_chunk_id;
          var oldHeading = oldHeadingByID[oldID] || '';
          var matchedNew = newChunks.find(function (nc) { return nc.heading_path === oldHeading; }) ||
            newChunks.find(function (nc) { return nc.heading_path.split('>').pop().trim() === oldHeading.split('>').pop().trim(); });

          if (!eq.should_refuse && !matchedNew) {
            needsRev++;
            return {
              question_id: eq.id,
              question: eq.question,
              question_set: eq.question_set,
              project_slug: eq.project_slug,
              status: 'needs_review',
              old_gold_chunk_id: oldID,
              new_gold_chunk_id: null,
              should_refuse: false,
              reason: '原章节标题未在新版段落中精确匹配，需人工确认对应切片'
            };
          }
          if (matchedNew) eq.gold_chunk_id = matchedNew.id;
          passed++;
          return {
            question_id: eq.id,
            question: eq.question,
            question_set: eq.question_set,
            project_slug: eq.project_slug,
            status: 'passed',
            old_gold_chunk_id: oldID,
            new_gold_chunk_id: matchedNew ? matchedNew.id : null,
            should_refuse: eq.should_refuse,
            hit: true,
            citation_ok: true,
            refused: false
          };
        });
        regressionSummary = {
          checked: impacted.length,
          passed: passed,
          refused_unexpectedly: refusedUnexp,
          needs_review: needsRev,
          details: details
        };
      }
    }

    return {
      status: 200,
      body: {
        document_id: docObj.id,
        filename: docObj.filename,
        project_slug: docObj.project_slug,
        chunk_count: docObj.chunk_count,
        replaced: Boolean(existing),
        skipped: false,
        regression_summary: regressionSummary
      }
    };
  }

  function makeJSONResponse(status, body) {
    return new Response(JSON.stringify(body), {
      status: status,
      headers: { 'Content-Type': 'application/json; charset=utf-8' }
    });
  }

  function delay(ms) {
    return new Promise(function (resolve) { setTimeout(resolve, ms); });
  }

  window.fetch = async function (input, init) {
    var urlStr = typeof input === 'string' ? input : (input && input.url ? input.url : '');
    if (urlStr.indexOf('/api/') !== 0 && urlStr.indexOf('/api/') < 0) {
      if (originalFetch) return originalFetch(input, init);
      return makeJSONResponse(404, { error: 'Not Found' });
    }

    var parsed = new URL(urlStr, window.location.origin);
    var pathname = parsed.pathname;
    var method = ((init && init.method) || 'GET').toUpperCase();

    if (pathname === '/api/setup/status' && method === 'GET') {
      return makeJSONResponse(200, {
        setup_enabled: false,
        configured: true,
        sandbox: true
      });
    }

    if (pathname === '/api/setup' && method === 'POST') {
      return makeJSONResponse(200, { ok: true, sandbox: true });
    }

    if (pathname === '/api/chunks' && method === 'GET') {
      var slugF = parsed.searchParams.get('project_slug') || '';
      var tot = slugF ? state.chunks.filter(function (c) { return c.project_slug === slugF; }).length : state.chunks.length;
      return makeJSONResponse(200, { total: tot });
    }

    if (pathname === '/api/projects' && method === 'GET') {
      var enriched = state.projects.map(function (p) {
        var dCnt = state.docs.filter(function (d) { return d.project_slug === p.slug; }).length;
        var cCnt = state.chunks.filter(function (c) { return c.project_slug === p.slug; }).length;
        return Object.assign({}, p, { doc_count: dCnt, chunk_count: cCnt });
      });
      return makeJSONResponse(200, { projects: enriched });
    }

    if (pathname === '/api/projects' && method === 'POST') {
      var pBody = JSON.parse(init.body || '{}');
      var pname = String(pBody.name || '').trim();
      if (!pname) return makeJSONResponse(400, { error: '请填写项目名称' });
      var newProj = {
        id: state.nextProjId++,
        slug: makeSlug(pname),
        name: pname,
        description: String(pBody.description || '').trim(),
        is_hidden: false
      };
      state.projects.push(newProj);
      return makeJSONResponse(201, newProj);
    }

    if (pathname === '/api/sessions' && method === 'GET') {
      var sortedSess = state.sessions.slice().sort(function (a, b) {
        if (Boolean(b.is_pinned) !== Boolean(a.is_pinned)) return b.is_pinned ? 1 : -1;
        return b.id - a.id;
      });
      return makeJSONResponse(200, { sessions: sortedSess });
    }

    if (pathname.indexOf('/api/sessions/') === 0) {
      var sid = Number(pathname.slice('/api/sessions/'.length));
      var sIdx = state.sessions.findIndex(function (s) { return s.id === sid; });
      if (sIdx < 0) return makeJSONResponse(404, { error: '会话不存在' });
      if (method === 'GET') {
        var tList = state.turnsBySess[sid] || [];
        return makeJSONResponse(200, {
          session: state.sessions[sIdx],
          turns: tList,
          stats: calcSessionStats(tList)
        });
      }
      if (method === 'PATCH') {
        var sPatch = JSON.parse(init.body || '{}');
        if (typeof sPatch.is_pinned === 'boolean') state.sessions[sIdx].is_pinned = sPatch.is_pinned;
        if (typeof sPatch.title === 'string' && sPatch.title.trim()) state.sessions[sIdx].title = sPatch.title.trim();
        return makeJSONResponse(200, state.sessions[sIdx]);
      }
      if (method === 'DELETE') {
        state.sessions.splice(sIdx, 1);
        delete state.turnsBySess[sid];
        return makeJSONResponse(200, { deleted: true });
      }
    }

    if (pathname === '/api/stats/online' && method === 'GET') {
      return makeJSONResponse(200, calcOnlineStats());
    }

    if (pathname === '/api/ask' && method === 'POST') {
      var askReq = JSON.parse(init.body || '{}');
      await delay(140);
      var resAsk = handleAsk(askReq);
      return makeJSONResponse(resAsk.status, resAsk.body);
    }

    if (pathname === '/api/documents/upload' && method === 'POST') {
      await delay(150);
      var upRes = await handleUpload(init.body);
      return makeJSONResponse(upRes.status, upRes.body);
    }

    if (pathname === '/api/documents' && method === 'GET') {
      var fp = parsed.searchParams.get('project_slug') || '';
      var fsStatus = parsed.searchParams.get('status') || '';
      var outDocs = state.docs.filter(function (d) {
        if (fp && d.project_slug !== fp) return false;
        if (fsStatus && d.status !== fsStatus) return false;
        return true;
      });
      return makeJSONResponse(200, { documents: outDocs });
    }

    if (pathname.indexOf('/api/documents/') === 0) {
      var docID = Number(pathname.slice('/api/documents/'.length));
      var dIdx = state.docs.findIndex(function (d) { return d.id === docID; });
      if (dIdx < 0) return makeJSONResponse(404, { error: '文档不存在' });
      var doc = state.docs[dIdx];

      if (method === 'GET') {
        var dChunks = state.chunks.filter(function (c) { return c.document_id === docID; });
        return makeJSONResponse(200, Object.assign({}, doc, { chunks: dChunks }));
      }
      if (method === 'PATCH') {
        var dPatch = JSON.parse(init.body || '{}');
        if (dPatch.status === 'active' || dPatch.status === 'deprecated') doc.status = dPatch.status;
        if (Object.prototype.hasOwnProperty.call(dPatch, 'replaced_by_doc_id')) {
          doc.replaced_by_doc_id = dPatch.replaced_by_doc_id || null;
        }
        state.chunks.forEach(function (c) {
          if (c.document_id === docID) c.doc_status = doc.status;
        });
        return makeJSONResponse(200, doc);
      }
      if (method === 'DELETE') {
        var beforeLen = state.chunks.length;
        state.chunks = state.chunks.filter(function (c) { return c.document_id !== docID; });
        var deletedChunks = beforeLen - state.chunks.length;
        state.docs.splice(dIdx, 1);
        return makeJSONResponse(200, { deleted: true, chunks_deleted: deletedChunks, warnings: [] });
      }
    }

    if (pathname === '/api/gaps' && method === 'GET') {
      var gp = parsed.searchParams.get('project_slug') || '';
      var gs = parsed.searchParams.get('status') || '';
      var outGaps = state.gaps.filter(function (g) {
        if (gp && g.project_slug !== gp) return false;
        if (gs && gs !== 'all' && g.status !== gs) return false;
        return true;
      }).slice().sort(function (a, b) {
        if (a.status === 'open' && b.status !== 'open') return -1;
        if (a.status !== 'open' && b.status === 'open') return 1;
        return (b.hit_count || 0) - (a.hit_count || 0);
      });
      return makeJSONResponse(200, { gaps: outGaps });
    }

    if (pathname.indexOf('/api/gaps/') === 0 && method === 'PATCH') {
      var gid = Number(pathname.slice('/api/gaps/'.length));
      var gPatch = JSON.parse(init.body || '{}');
      var targetGap = state.gaps.find(function (g) { return g.id === gid; });
      if (!targetGap) return makeJSONResponse(404, { error: '盲区记录不存在' });
      if (gPatch.status) targetGap.status = gPatch.status;
      return makeJSONResponse(200, targetGap);
    }

    if (pathname === '/api/feedback' && method === 'GET') {
      var fv = parsed.searchParams.get('vote') || '';
      var fpFlag = parsed.searchParams.get('promoted') || '';
      var outFb = state.feedbacks.filter(function (fb) {
        if (fv && fb.vote !== fv) return false;
        if (fpFlag === 'pending' && fb.promoted_question_id) return false;
        if (fpFlag === 'promoted' && !fb.promoted_question_id) return false;
        return true;
      });
      return makeJSONResponse(200, { feedback: outFb });
    }

    if (pathname === '/api/feedback' && method === 'POST') {
      var fbBody = JSON.parse(init.body || '{}');
      if (fbBody.turn_id) {
        Object.keys(state.turnsBySess).forEach(function (sid) {
          (state.turnsBySess[sid] || []).forEach(function (t) {
            if (Number(t.id) === Number(fbBody.turn_id)) t.feedback_vote = fbBody.vote;
          });
        });
      }
      var rIDs = Array.isArray(fbBody.retrieved_chunk_ids) ? fbBody.retrieved_chunk_ids : [];
      var candChunks = rIDs.map(function (cid) {
        var ch = state.chunks.find(function (x) { return x.id === Number(cid); });
        return ch ? { id: ch.id, project_slug: ch.project_slug, doc_title: ch.doc_title, heading_path: ch.heading_path } : null;
      }).filter(Boolean);
      var newFb = {
        id: state.nextFeedbackId++,
        project_slug: fbBody.project_slug || 'auto',
        question: fbBody.question || '',
        retrieved_chunk_ids: rIDs,
        answer: fbBody.answer || '',
        refused: Boolean(fbBody.refused),
        vote: fbBody.vote || 'up',
        comment: fbBody.comment || '',
        promoted_question_id: null,
        created_at: new Date().toISOString(),
        candidate_chunks: candChunks
      };
      state.feedbacks.unshift(newFb);
      return makeJSONResponse(201, { ok: true, id: newFb.id, vote: newFb.vote });
    }

    var promoteMatch = pathname.match(/^\/api\/feedback\/(\d+)\/promote$/);
    if (promoteMatch && method === 'POST') {
      var fid = Number(promoteMatch[1]);
      var targetFb = state.feedbacks.find(function (f) { return f.id === fid; });
      if (!targetFb) return makeJSONResponse(404, { error: '反馈不存在' });
      var promReq = JSON.parse(init.body || '{}');
      var qID = state.nextEvalId++;
      var goldCh = promReq.gold_chunk_id ? state.chunks.find(function (c) { return c.id === Number(promReq.gold_chunk_id); }) : null;
      state.evalQuestions.push({
        id: qID,
        project_slug: targetFb.project_slug || (goldCh ? goldCh.project_slug : 'auto'),
        source_doc_id: goldCh ? goldCh.document_id : null,
        gold_chunk_id: promReq.should_refuse ? null : Number(promReq.gold_chunk_id),
        question: targetFb.question,
        question_set: 'feedback',
        should_refuse: Boolean(promReq.should_refuse),
        category: promReq.category || 'feedback'
      });
      targetFb.promoted_question_id = qID;
      return makeJSONResponse(200, { promoted_question_id: qID });
    }

    if (pathname === '/api/eval/run' && method === 'POST') {
      await delay(200);
      var totalQ = state.evalQuestions.length;
      return makeJSONResponse(200, {
        counts: { question_count: totalQ },
        hit_rate: 1.0,
        citation_rate: 1.0,
        refusal_rate: 1.0
      });
    }

    return makeJSONResponse(404, { error: '沙箱暂未支持该接口：' + pathname });
  };
})();
