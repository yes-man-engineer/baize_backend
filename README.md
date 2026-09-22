# baize_backend

把一个模糊的「我想做点什么」变成今晚就能开始验证的起步方案。

核心机制是**置信分层**：方案里每一条都标明是确定的（green）、AI 猜的（yellow）、
还是只有用户自己知道的（red）。黄红两色本身就是任务板，用户去现场核实、回填真实数据，
对应条目变绿，绿色占比上升就是进度。

## 技术栈

Go 1.22 / Gin / GORM / MySQL / zap，大模型走 OpenAI 兼容接口。

## 两个入口，一条主干

```
入口 A  已有想法 ─────────────────────────┐
                                         │
入口 B  不知道做什么                        │
   └→ 盘点手上现成有什么                    │
      └→ 给 3 条能启动的路径 → 选一条 ──────┤
                                         ↓
                             带假设的方案（🟢🟡🔴）
                                         ↓
                        核实 → 回填 → 变绿 → 绿色占比上升
                                         ↓
                                 用户点「项目结束」
```

入口 B 是前置选路器，选完立刻按主干规则继续提问，所以业务逻辑只有一条主干。
`status` 依次是 `scouting → choosing → interviewing → planned → ended`，
入口 A 直接从 `interviewing` 起步。

## 目录

```
cmd/api             入口
internal/config     配置
internal/model      Project / Message / PathOption / PlanItem
internal/repository 数据访问
internal/llm        模型客户端 + 四套提示词（主干提问、盘点提问、候选路径、生成方案）
internal/service    提问编排、选路、方案生成与回填
internal/handler    HTTP
internal/router     路由
pkg/logger          日志
pkg/response        统一响应
migrations          建库 DDL（表结构由 AutoMigrate 维护）
```

## 跑起来

```bash
cp .env.example .env    # 填 DB 和 LLM_API_KEY
mysql -uroot -p < migrations/init.sql
go mod tidy
go run ./cmd/api
```

## 接口

| 方法 | 路径 | 说明 |
| --- | --- | --- |
| POST | `/api/projects` | 开新项目。`idea` 为空即入口 B。返回 token 和第一个问题 |
| POST | `/api/projects/:token/answers` | 提交回答，返回下一个问题和 `next_action` |
| POST | `/api/projects/:token/paths` | 入口 B：盘点完生成 3 条候选路径 |
| POST | `/api/projects/:token/paths/:id/select` | 选一条，立刻合流回主干提问 |
| POST | `/api/projects/:token/plan` | 生成方案。点「够了，先给我方案」也调这个 |
| GET | `/api/projects/:token` | 一次拿全：方案文档、任务板、候选路径、对话、进度 |
| POST | `/api/projects/:token/items/:id/verify` | 回填真实数据，该条变绿 |
| POST | `/api/projects/:token/end` | 用户主动结束项目 |

`next_action` 告诉前端下一步调哪个接口：`ask` 继续答题 / `paths` 去选路 / `plan` 去出方案。
刷新页面时 `GET /api/projects/:token` 也会返回它。

token 就是访问凭证，没有账号体系，前端把它存在链接里。

## 写死在代码里的产品规则

1. **第一个问题永远是亏损上限**（`service.FirstQuestion`），两个入口都一样。
   这个数字决定整份方案和每条候选路径的大小，不交给模型决定。
2. **候选路径固定 3 条**，且必须分别对应最稳 / 最快回钱 / 天花板最高。
   给 1 条等于替用户做决定，给 10 条等于没给。
3. **方案可以是「先别做」**。`verdict=stop` 时不给创业方案，直接劝退。
4. **不装懂**：凡是涉及具体街道的人流、价格、竞争，一律不许标 green。
5. **「不知道」不追问**，直接转成方案里的待验证项。

规则都在 `internal/llm/prompt.go` 和 `internal/service` 里。
