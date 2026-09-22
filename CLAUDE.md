# 项目上下文

## 这是什么

一个帮普通人把模糊的「我想做点什么」变成**今晚就能开始验证的起步方案**的网站后端。

用户不是迷茫于方向，而是迷茫于**从意图到第一个动作之间那段真空**。真实场景里
用户第二句话就会说"我想去夜市卖烧烤"，卡住的是不知道第一步干什么。

## 核心机制：置信分层（这是整个产品的地基）

AI 给出的方案，每一条都必须标明置信度：

- **green 确定** — 不依赖具体地点的通用事实。办证材料、设备成本、毛利怎么算、
  一个人一天的产能上限。这部分给足、给具体。
- **yellow 我猜的** — 和本地强相关。给一个**具体的、带数字的假设**让用户去反驳，
  同时给出 `verify_action`。
- **red 只有用户知道** — 完全无从得知的。不给假设，只给 `verify_action`。
  摊位费、城管几点来、这条街允不允许摆。

**为什么必须这样**：互联网上不存在"某市某区某条街"这种颗粒度的数据。AI 一旦把猜的
东西标成确定，本地用户一眼就能看穿，看穿一次信任归零，再也不来。把不确定性摊开
反而建立信任——"这东西不装懂"。

**为什么带假设比空清单好**：让人开放式观察，他不知道看什么。给他一个具体的错误结论
（"我估计烤脑花能卖 8 块"），他站在摊前第一反应就是"放屁，老王卖 6 块"。
人反驳一个具体的东西，比从空白开始容易十倍。

黄红两色本身就是任务板，不用另开一张清单。

## 五条写死在代码里的产品规则（改之前先问）

1. **第一个问题永远是亏损上限** — `service.FirstQuestion`，两个入口都一样。
   这个数字决定整份方案和每条候选路径的大小，不交给模型决定。
2. **候选路径固定 3 条**，分别对应 `steady` 最稳 / `fast_cash` 最快回钱 /
   `high_ceiling` 天花板最高。给 1 条等于替用户做决定，给 10 条等于没给。
   模型给了不认识的 angle 时，代码按位置兜底，保证 3 条始终拉得开。
3. **方案可以是「先别做」** — `verdict=stop` 时不给创业方案，直接劝退。
   能说"不要做"是这个产品和满大街创业内容唯一的分界线。
4. **涉及具体街道的人流、价格、竞争，一律不许标 green**。
5. **「不知道」不追问** — 这是高频回答（用户本来就不了解他要做的事，这正是他来的原因）。
   不换个说法再问一遍，直接转成方案里的待验证项。提问阶段的每处空白自动变成一项作业。

## 提问什么时候停

不设数量上限。三个出口任一满足即停：用户点「够了，先给我方案」按钮 /
用户在对话里表达够了（模型识别）/ 再问也不会改变方案里任何一条。

由此推出可落地的规则：**每个问题必须挂钩方案里的一个具体条目**，问不出条目的不问。

## 两个入口，一条主干

```
入口 A  已有想法 ──────────────────────────┐
                                          │
入口 B  不知道做什么                         │
   └→ scouting 盘点手上现成有什么            │
      └→ choosing 给 3 条路径 → 选一条 ─────┤
                                          ↓
                            interviewing 主干提问
                                          ↓
                         planned 带假设的方案（🟢🟡🔴）
                                          ↓
                      核实 → 回填 → 变绿 → 绿色占比上升
                                          ↓
                                  ended 用户主动结束
```

选路后**立刻合流**：`PathService.Select` 写完选择就切到 `interviewing` 并当场
调主干提问 prompt 再问一轮。业务逻辑只有一条主干，不是两条并行的线。

入口 B 盘点时问的是具体事实，不问技能不问兴趣：上一份工作一天干什么、
每周多少时间、在什么地方、**别人常找你帮什么忙**、**你家现成有什么**。
最后两条最关键——对普通人来说创业起点不是"我会什么"，是"我手上现成有什么"。

## 技术栈与结构

Go 1.22 / Gin / GORM / MySQL / zap，大模型走 **OpenAI 兼容接口**
（DeepSeek、Kimi、通义都能用，换 `LLM_BASE_URL` + `LLM_MODEL`）。

```
cmd/api/main.go     入口 + 优雅关闭
internal/config     .env 配置
internal/model      Project / Message / PathOption / PlanItem
internal/repository GORM + AutoMigrate
internal/llm        client.go（含 JSON 容错提取）+ prompt.go（四套提示词）
internal/service    interview（提问编排）/ path（选路）/ plan（方案、回填、进度）
internal/handler    HTTP
internal/router     路由
pkg/logger          zap
pkg/response        统一响应
migrations/init.sql 建库 DDL（表结构实际由 AutoMigrate 维护）
```

`PlanItem` 是核心表：`confidence` + `assumption` + `verify_action` + `answer`。
文档视图渲染全部条目，任务板只取 yellow/red——**同一份数据的两个视图**。
回填 `answer` 就置 green 并打 `verified_at`，绿色占比由此算出。这个联动是
"进度可见"的唯一来源，不联动用户就感知不到自己在推进。

## 接口

| 方法 | 路径 | 说明 |
| --- | --- | --- |
| POST | `/api/projects` | 开新项目。`idea` 为空即入口 B |
| POST | `/api/projects/:token/answers` | 提交回答，返回下一问和 `next_action` |
| POST | `/api/projects/:token/paths` | 入口 B：生成 3 条候选路径 |
| POST | `/api/projects/:token/paths/:id/select` | 选一条，立刻合流回主干 |
| POST | `/api/projects/:token/plan` | 生成方案（点「够了」也走这里） |
| GET | `/api/projects/:token` | 一次拿全：文档、任务板、路径、对话、进度 |
| POST | `/api/projects/:token/items/:id/verify` | 回填，该条变绿 |
| POST | `/api/projects/:token/end` | 主动结束 |

`next_action`：`ask` 继续答题 / `paths` 去选路 / `plan` 去出方案。
GET 详情也返回它，前端刷新页面不用自己按 status 猜。

**没有账号体系**，`token` 就是访问凭证，前端存在链接里。

## 当前状态

- **编译通过**（Go 1.26 实测）、`go vet` 干净、`gofmt` 干净
- **单元测试覆盖三条最要命的规则**：亏损上限解析、置信度降级、3 条路径拉开
  （`internal/service/*_test.go`、`internal/middleware/cors_test.go`）
- **compose 全栈实测起得来**：MySQL healthy → AutoMigrate 建出 4 张表 →
  `/health` 200 → 开项目落库正确。走模型的接口还没验（缺 key）
- 代码**仍未提交**（`git status` 一堆 untracked）
- 远端 `main` 是重置后的空骨架单条提交

## 五条规则的代码落点

规则不能只写在 prompt 里——模型不守的时候得有代码兜住：

| 规则 | 代码位置 |
| --- | --- |
| 1 第一问是亏损上限 | `service.FirstQuestion`；`parseMoney` 只在第一轮采信裸数字 |
| 2 固定 3 条路径 | `PathService.Generate` 不足 3 条报错；`spreadAngles` 保证角度互不相同 |
| 3 可以「先别做」 | `stopSections` —— stop 时只落止损类条目，执行方案一条不落 |
| 4 本地信息不许 green | `localHints` + `normalizeItem`，命中就降 red（宁可误伤不许漏判） |
| 5 「不知道」不追问 | prompt 里约束；空白自动变成方案里的待验证项 |

另外 `normalizeItem` 还守着：yellow 必须带 assumption，带不出来降 red——
没有假设的黄色就是一张空清单，而"带假设比空清单好"是整个机制的立足点。

## 待办，按优先级

1. **接真实模型跑通一遍**：填 `.env` 的 `LLM_API_KEY`，
   `docker compose up -d`，走一遍入口 A 全流程（开项目 → 答几轮 → 出方案 → 回填变绿）。
   重点看 prompt 的实际表现：green/yellow/red 分得准不准、
   `verify_action` 是不是真的今晚两小时能做完。
2. **提交代码**。目前全是 untracked。
3. **前端**（`baize_frontend` 仓库）目前是 Vite+React19+TS 的空骨架，
   Tailwind / shadcn 都没装。需要：API 地址配置 `VITE_API_BASE`、
   对话页、方案文档+任务板双视图、绿色占比。

## 还没设计的（产品层，不是技术问题）

- **用户没做作业时产品说什么**？开了方案三天没勾任何一项，再打开看到的是什么？
  这个分支不设计，留存机制就是半截的，回填率指标也会被污染（分不清是不想做还是没机会）
- **green 部分要不要联网核实**？办证材料、设备价格有地域差异也会过期，标绿的门槛在哪
- 首版不做自动触达（没有 App 就没有 push，邮件国内没人看，短信要资质）。
  首批用户是身边人，当面追问替代。但正式版不解决，留存为零。
