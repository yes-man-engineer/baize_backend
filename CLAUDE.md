# 项目上下文

## 这是什么

**产品定义以用户的说法为准，这里只记他原话说过的，没说过的不要补。**

> 做一个平台，帮助任何行业的人创业，上至高精尖、尖端科技，下至贩夫走卒、
> 夜市摆摊。不仅仅是创业和副业，还包括教人们怎么找工作、怎么工作得更好，
> 解决全社会所有人的就业问题。

范围就这么宽，**不要往窄里收**。尤其不要拿小生意、地摊当默认场景去推算
条目数、方案大小、提问内容。

创业、找工作、改善现在的工作，这三件事**在产品里不分支**。底层是同一件事：
从现状走到一个可验证的下一步。是哪一种由模型从用户的话里自己判断，
代码和提示词都不要预设。

## 一条总原则：代码判结构，模型判语义

这是 2026-09 推倒重写的起因。上一版的做法是尽量用代码写规则：正则抠金额、
关键词判断某条信息是不是本地相关、按位置兜底凑够条数。每一处线上事故都出在
同一个地方，**代码在做语义判断**。

分工是：

- **代码只做结构性的事**：查表、判断字段空不空、集合里有没有、数一数几条、
  状态能不能这么转。这些判断对错是确定的。
- **语义交给模型**：这句话是什么意思、用户说的够不够、这条信息可不可靠。
  这些判断没有确定答案，用关键词去近似一定会错。

引申的两条：

- 给模型的输入和期望的输出要规范好，但规范完就信它，别在外面再补一层关键词纠偏。
- 让模型说话的调用和让模型吐结构的调用要分开。说话那条路上不加任何格式约束，
  加了模型就会分心去凑格式。`llm.Stream` 和 `llm.JSON` 这么分就是这个原因。

## 代码风格：裸函数 + 全局连接 + Decorate

Go 是面向过程的语言，这个仓里**默认写裸函数**，不要为了挂方法而造对象。
`XxxService` / `XxxRepo` / `XxxHandler` 这类只为当接收者而存在的结构体，
一个都不要有。

只有两种情况写方法：

1. **在实现接口。** `Facts.Value` / `Facts.Scan`（driver.Valuer、sql.Scanner）、
   `Project.TableName`（GORM）这些必须是方法，没得选。
2. **类型自己真的有状态**，而且只在包内部用。`llm` 里的 `client` 是这种，
   但它不导出，外面只看得见 `llm.Stream` / `llm.JSON`。

### 连接放包级全局，只开公开函数

数据库连接和模型客户端都是包级私有变量，`main` 里各 `Init` 一次，之后谁都不用
再传。**连接本身不外泄**，要用就调这个包的公开函数：

```go
repository.Init(dsn, dev)          // main 里一次
repository.GetProject(ctx, id)     // 别处只看得到这个，看不到 *gorm.DB

llm.Init(baseURL, key, model, temperature, timeout)
llm.Stream(ctx, msgs, onDelta)
```

好处是业务函数的签名里只剩业务参数，一眼能看出这次调用在干什么。

### 框架和业务在 Decorate 那一层断开

业务函数长这样，入参是业务请求，出参是业务响应，**看不到 `*gin.Context`**：

```go
func StartProject(ctx context.Context, req StartProjectReq) (*Detail, error)
func Reply(ctx context.Context, req ReplyReq, onDelta func(string)) (*ReplyResp, error)
```

`internal/handler` 只有 `decorate.go` 一个文件，绑参数、翻译错误、写响应、
铺 SSE 全在里面。路由就是一行接一行：

```go
api.POST("/projects", handler.Decorate(service.StartProject))
api.GET("/projects/:project_id", handler.Decorate(service.GetDetail))
api.POST("/projects/:project_id/messages", handler.DecorateStream(service.Reply))
```

三个装饰器，按业务函数的形状挑：`Decorate` 收一进一出的，`DecorateNoResp`
收只返回 error 的，`DecorateStream` 收边算边吐的。

**路由里的占位符必须和请求结构体的 json 标签同名**，比如 `json:"project_id"`
对应 `/projects/:project_id`。`bind` 是自己按 json 标签找路径参数的，没用 gin 的
`ShouldBindUri`（那个只认 `uri` 标签，会在业务结构体上留框架痕迹）。

路径参数在请求体之后绑，所以请求体里塞一个同名字段盖不掉路径里的值。顺序反了
就是个越权读取的洞。

### 不上 IDL，理由记在这

2026-09-24 评估过 protobuf 和 thrift，**决定不用**，等真出现微服务之间的 RPC
再说。以后想重提这件事，先看完下面这三条实测结果：

- **枚举会变成数字。** gin 用 `encoding/json`，proto 生成的枚举是 int32，
  序列化出来是 `"status":1` 而不是 `"status":"chatting"`。要字符串就得换
  `protojson`，而它默认又把字段名改成 `projectId`，得开 `UseProtoNames`。
- **`omitempty` 去不掉。** 生成的 tag 一律带，空字符串和空 map 会从响应里消失。
  我们的 `content` 和 `facts` 允许为空，前端就得处理 `undefined`。
  救回来要开 `EmitUnpopulated`，又是一个必须换渲染器的理由。
- **proto 结构体兼不了 GORM 模型。** 它带 `state` / `unknownFields` / `sizeCache`
  三个内部字段，没有 gorm tag，`Facts` 也不是我们那个带 Valuer/Scanner 的类型。
  结果是一份结构体变三份：proto 生成的、GORM 模型、中间的转换代码。

我们是纯 JSON over HTTP，proto 的跨语言、二进制传输、RPC 契约三样收益一样都
用不上。付四笔钱只买到「有个 IDL 文件」。

前后端类型漂移是真问题（前端 `ProjectStatus` 曾经少了 `ready` 很久没人发现），
但那个用 Go 结构体当来源、生成前端 TS 就能解决，不需要 IDL。现在是**手工保持
一致**，改 `model` 或 service 的请求响应结构体，记得同步改前端 `src/api/types.ts`。

## 技术栈与结构

Go 1.22 / Gin / GORM / MySQL / zap，大模型走 **OpenAI 兼容接口**
（换 `LLM_BASE_URL` + `LLM_MODEL` 即可切换，线上是 Kimi `kimi-k2.6`）。

```
cmd/api/main.go     入口 + 优雅关闭
internal/config     .env 配置
internal/model      Project / Message
internal/repository GORM + AutoMigrate
internal/llm        client.go（Stream / JSON 两个方法）+ prompt.go
internal/service    project（建项目、取详情）/ chat（对话、抽事实）
internal/handler    HTTP，chat 走 SSE
internal/router     路由
pkg/logger          zap
pkg/response        统一响应
```

`migrations/init.sql` 是上一版留下的，表结构实际由 AutoMigrate 维护，
里面的 DDL 已经不作数。

**没有账号体系**，project id 就是访问凭证，前端放在链接里。
不要另造一个 token 概念，token 这个词在这个项目里只指模型的输入输出计量。

## 几条已经踩实的技术约束

- **temperature 对 Kimi k2/k3 只能是 1**。配置项 `LLM_TEMPERATURE` 为空时
  整个字段从请求里省略，不要写死一个值。
- **异步任务必须用 `context.Background()` 另起超时**，不能挂在请求的 ctx 上。
  请求一返回 ctx 就取消了，后台抽取会被一起掐掉。
- **SSE 要发 `X-Accel-Buffering: no`**，否则 nginx 会把流缓冲成一整坨。
- **流中途出错只能当事件发出去**，那时响应头已经 flush，改不了状态码了。

## 写 prompt 的一条教训

**不要在 prompt 里写带引号的完整例句。** 模型会把例句当成该输出的答案直接吐
出来，绕过 JSON 包装，解析时连花括号都找不到。

2026-09-23 踩过一次：为了焊死某个问法，规则里写了一句带引号的完整问句，
模型走到那一轮就原样复读这句纯文本。**这不是随机抖动**，同样的 prompt 每次都
稳定诱导同样的输出，所以原样重发的重试一次都救不回来。

改法是例句只给方向，不给可以整句抄走的句子；重试时追加一条说明上次错在哪的
纠正消息，而不是把原请求再发一遍。

## 部署

生产跑在一台云主机上：**systemd + nginx**，不走 docker compose
（compose 那套是给全新机器准备的，现有机器的 MySQL 是独立容器）。

流程固定是 commit → push → 服务器 pull → build → restart，不要把未提交的
文件 scp 上去。

后端：`git pull` → `go build -trimpath -ldflags="-s -w" -o baize ./cmd/api`
→ `systemctl restart baize`（服务名 `baize`，监听 8080）。
配置在同目录 `.env`，**不进 git**。

前端：`git pull` → `npm ci && npm run build`，产物 `dist/` 就是 nginx 的 root。

nginx 把 `/api/` 反代到 `127.0.0.1:8080`。前端 `VITE_API_BASE` 默认就是 `/api`
走同源，所以后端 `CORS_ORIGINS` 留空即可。

### 三个踩过的坑

**一、Go 拉不到依赖。** 国内云主机连不上 proxy.golang.org（i/o timeout），
必须先 `go env -w GOPROXY=https://goproxy.cn,direct`，否则 `go build`
卡死在下载依赖。

**二、nginx 默认 60 秒会掐断大模型请求。** `proxy_read_timeout` 不配就是
默认 60 秒，而一次生成动辄上百秒。表现是浏览器拿到 504，后端却还在跑，
日志里一切正常，只改后端的 `LLM_TIMEOUT_SECONDS` 永远查不出来，那个值
在外层被 nginx 切断之后根本不起作用。两个超时必须一起设，且 Go 侧要比
nginx 小，这样超时一定由内层主动抛出，日志里能看到是哪次调用超的。

**三、`index.html` 被缓存会让前端发版不生效。** nginx 不配 `Cache-Control`
就不发这个头，浏览器于是按启发式规则自己缓存。`index.html` 决定加载哪个
bundle，它一被缓存，后端发了新版用户也只会拿到旧 JS。症状极其难反推：
新后端配旧前端，界面会出现各种说不通的错乱，而服务器上查什么都是对的。

nginx 里这三段是配套的，缺一不可：

```nginx
location /api/ {
    proxy_read_timeout 1260s;   # 必须大于后端的 LLM_TIMEOUT_SECONDS
    proxy_send_timeout 1260s;
}
location = /index.html {
    add_header Cache-Control "no-cache";
}
location /assets/ {             # 文件名带内容 hash，长缓存是安全的
    add_header Cache-Control "public, max-age=31536000, immutable";
}
```

**查线上数据**用容器里的 `mysql` CLI 时记得带 `--default-character-set=utf8mb4`，
不带会看到乱码，那是 CLI 的问题不是存进去的数据有问题。

**安全底线**（这台机器 2026-07 因此被删过库，别再犯）：

1. MySQL 端口必须绑 `127.0.0.1:3306:3306`。写成 `"3306:3306"` 就是绑
   `0.0.0.0`，全网可扫。
2. 数据库密码用 `openssl rand -hex 16` 生成。**任何密码都不许出现在提交里**，
   git 一旦记录就要当永久泄露处理，force push 重置分支也删不掉旧 commit。
3. 服务器地址、凭证、事故细节记在本地 memory，不写进这个公开仓库。

## 还没定的

「方案」长什么样、怎么存、怎么让用户回来，都还没定。**不要从旧代码、
旧文档或者我之前的提议里推断答案**，这些都要和用户逐个聊清楚再写。
