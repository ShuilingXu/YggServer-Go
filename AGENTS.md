# AGENTS.md

本仓库所有 AI Agent（以及按本文件工作的人）必须遵守的工程约定。
覆盖：编程、功能复用、日志、数据库、测试、设计、验证、Git 提交、仓库卫生。

**优先级**：用户本轮明确指令 > 本文件 > Agent 的默认习惯 / 训练偏好。
**冲突时**：停下来指出冲突并询问，禁止自行取舍。
**开工前**：必读 §0 与 §12（已知的坑）；其余章节按任务相关性查阅。

项目：**YggServer-Go** —— 用 Go 重建 Minecraft Yggdrasil 登录 API，并内置皮肤站（SkinServer）。
技术栈：Go + React；PostgreSQL 为主，兼容 MySQL。当前状态、环境与下一步见 [HANDOVER.md](HANDOVER.md)。

---

## 0. 硬规则（不可违反，违反即视为未完成）

1. **不伪造验证**。没真正编译、没真正用浏览器看到，就写"未验证 + 原因"，禁止用 curl 结果或"应该没问题"充当证据。
2. **日志无鉴权信息**。token、密码、密钥、会话、验证码、Cookie 一律不得进入日志（§3）。
3. **type 用整数**。数据库禁止用字符串/ENUM 存类型语义字段（§4）。
4. **不重复造功能**。新代码先找既有实现，能复用就复用或改它，而不是并列再写一份（§1、§2）。
5. **不在仓库留垃圾**。临时脚本、缓存、日志、数据库文件、截图一律不入库（§8）。

---

## 1. 编程行为

### 1.1 简洁优先
- 一个概念只实现一次。同一逻辑不允许出现两份，也不允许"新函数调用旧函数但把旧逻辑抄一遍"。
- 能用一个函数解决就不拆成三个；能用一个结构体字段解决就不加一个表。
- 不写"以后可能要用"的抽象：接口、泛型、工厂、事件总线，无实际第二实现就是负担。
- 没有第二个使用方之前，不做提前解耦。三个相似实现出现后才考虑提取。
- 禁止把简单逻辑包装成多层调用链（handler → xService → yManager → zHelper 只为转一次参数）。

### 1.2 功能集成，不是按需求堆函数
- 新需求先判断能否并入既有入口：加参数、加分支、加枚举值，而不是新增一条平行链路。
- 典型正例：令牌的生成、校验、过期判断只在 `token` 包一处实现，`authenticate` / `refresh` / `validate` 全部复用它。
- 典型反例：先写 `AuthenticateUser`，下个需求又写 `AuthenticateUserV2`，再下个写 `LoginUser`。
- `V2`、`New/NewXxx`、`Compat` 之类的平行实现，除非协议强制，否则不写。

### 1.3 先复用后新增（改动前必做）
新增任何函数/类型前，用搜索确认没有既有实现。搜索目标：
- 路由注册：`HandleFunc` / `mux.Handle` / `r.GET` / `r.POST`
- 同类业务：按领域词搜（`token`、`profile`、`skin`、`session`、`authenticate`）
- 数据访问：搜表名与 SQL 片段，确认没有第二个地方在查同一张表
- 常量与错误：搜枚举值、"already exists" 之类的错误文本

结果处理：
- 已有可复用 → 直接复用；需要小改 → 改那一处。
- 已有但不合适 → 先说明为什么不合适，再决定是重构它还是新增；不允许默默复制一份。

### 1.4 注释
- 只在**必要且影响理解**的地方写简体中文注释：不直观的约束、协议怪癖、为什么这样做（而不是做了什么）。
- 不写装饰性注释：不写文件名横幅、不写 `// 定义变量`、不写与代码同义的废话。
- 导出的函数/类型默认不写注释（除非行为不显然）。需要 doc 注释时用中文写清前提与副作用。
- 不留被注释掉的代码、不留空的 `// TODO`；确需记录的问题写进 §12 或交给用户。

### 1.5 错误处理
- 库代码返回 `error`，不 `panic`、不 `log.Fatal`。
- 统一用 `fmt.Errorf("...: %w", err)` 包装，错误文本用小写、结尾不加句号。
- 只在最外层（handler / main）把 error 转成响应或日志；内层不重复打印。
- 对外的错误信息不泄漏内部细节（SQL、文件路径、栈）。
- 业务错误用哨兵错误或类型化错误 + `errors.Is/As` 判断，不靠字符串比较错误文本。

### 1.6 依赖与格式
- 优先标准库。新增第三方依赖必须能一句话说明"不引入就要写多少行、有多少坑"。
- 不为一个小功能引入整套框架（如为一处缓存引入完整 DI 容器）。
- 提交前 `gofmt` 必须干净（Go 已装，见 §11；未跑就说明未跑）。

### 1.7 明确禁止
- 擅自改架构、批量重命名、顺手"优化"无关代码。
- 为通过测试而降低断言或删除断言。
- 在仓库根目录随手创建脚本、说明文档、示例数据。
- 用 `git` 清理别人未提交的改动（§9）。

---

## 2. 功能复用

### 2.1 单一数据源
同一事实只能有一处定义：常量、错误、响应结构、SQL、配置键、类型枚举各自唯一。

```go
// 反例：handler 里拼 SQL，和 store 里的查询重复
row := db.QueryRow("SELECT id FROM accounts WHERE email = ?", email)

// 正例：复用 store 的唯一实现
acc, err := s.accounts.ByEmail(ctx, email)
```

### 2.2 收敛扩展点
- 请求参数的公共校验（必填、长度、格式）在一处做，不散落在每个 handler。
- 响应体组装（Yggdrasil 的 profile / error 结构）在一处做，不在每个 handler 手写 map。
- 需要新行为时优先"给既有实现加枚举值/参数"，而不是"新增一个几乎相同的函数"。

### 2.3 复用不等于耦合
- 不为了复用让 A 功能直接调用 B 功能的 handler 层函数；复用只发生在 service / store 层。
- 被复用的函数如果开始长出大量 `if 调用方 == xxx` 分支，说明拆分错了，回头收敛。

---

## 3. 日志

### 3.1 红名单（绝对不能出现）
- 密码原文、密码哈希、盐
- `accessToken`、`clientToken`、`refreshToken`、会话 ID、签名私钥、HMAC 密钥
- `Authorization`、`Cookie`、`X-Auth-Token` 等头部原值
- 邮箱验证码、找回密码令牌、邀请码
- 完整请求体 / 完整 query string（可能夹带 ts、signature 等）

### 3.2 需要记录时怎么记
- 只记能定位问题的最小标识：`account_id`、`player_uuid`、`request_id`。
- 必须证明某个 token 值时：记**长度**或 sha256 前 8 位，形如 `token=sha256:ab12cd34`。
- IP 记 `ip_hash` 或网段，不记完整原值（除非排查必需且已获用户授权）。

```go
// 禁止
slog.Info("login", "password", req.Password, "accessToken", tok)
log.Printf("auth body: %s", string(body))

// 正确
slog.Info("login ok", "account_id", acc.ID, "request_id", rid)
slog.Warn("login failed", "reason", "bad_password", "request_id", rid)
```

### 3.3 级别与分工
- `Debug`：只有本地排查才开，默认不输出；不放在高频热路径上长期保留。
- `Info`：生命周期事件（启动、监听地址、迁移完成、账号创建成功）。
- `Warn`：可恢复异常（密码错误、token 过期、触发限流、非法参数）。
- `Error`：需要人介入（DB 不可用、内部错误、配置缺失）。
- 不用日志代替错误返回；不在循环里逐条打印同质日志。

### 3.4 请求日志
每个请求恰好一条访问日志，固定字段：`request_id`、`method`、`path`、`status`、`dur_ms`、`ip_hash`。
不记录请求体；query 参数不整体记录（如确需，先过 §3.1 过滤后只记白名单键）。

### 3.5 一次一记
一个错误只在能决定"怎么处理"的那一层记录一次；中间层只包装返回。禁止同一错误打印 3 行。

---

## 4. 数据库与类型

### 4.1 type 一律用整数
- 所有表示"类型 / 状态 / 枚举 / 来源 / 权限位"的字段必须是整型（PostgreSQL `SMALLINT`，MySQL `TINYINT` / `SMALLINT`，SQLite `INTEGER`）。
- 禁止 `VARCHAR` / `TEXT` / `ENUM` / `SET` / `CHAR` 存 `"active"`、`"banned"`、`"skin"` 这类值。
- 布尔语义同样用 0/1 整数，不用字符串 `"true"`。
- 类型字段禁止 NULL，必须有明确默认值。

```sql
-- 正例
CREATE TABLE accounts (
  id            BIGINT UNSIGNED NOT NULL AUTO_INCREMENT PRIMARY KEY,
  email         VARCHAR(255)    NOT NULL,
  password_hash VARBINARY(255)  NOT NULL,
  state         TINYINT         NOT NULL DEFAULT 0, -- 0=正常 1=封禁 2=注销
  created_at    DATETIME(3)     NOT NULL,
  UNIQUE KEY uk_accounts_email (email)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
```

```go
// 枚举只在一个文件里定义，值显式写出，声明顺序不影响存储
type AccountState int8

const (
	StateActive  AccountState = 0
	StateBanned  AccountState = 1
	StateDeleted AccountState = 2
)
```

### 4.2 枚举纪律
- 新增取值**只追加、不改号、不复用旧号**；已上线的取值永不重排。
- 取值到含义的映射与枚举同文件维护，禁止散落在 handler 里做 `switch` 翻译。
- 新增任何 type 字段必须登记到下表（本表是仓库的类型字典，缺失即视为不合规）。

| 表.字段 | 含义 | 取值 |
| --- | --- | --- |
| `accounts.state` | 账号状态；后续业务查询须过滤封禁/注销 | `0` 正常 / `1` 封禁 / `2` 注销 |
| `accounts.permission` | 账号权限 | `0` 普通 / `1` 管理员 / `2` 站点所有者 |
| `accounts.verified` | 邮箱验证 | `0` 未验证 / `1` 已验证 |
| `textures.type` | 材质种类 | `0` steve（default） / `1` alex（slim） / `2` cape |
| `textures.is_public` | 是否进入公开皮肤库；原图鉴权由后续业务定义 | `0` 私有 / `1` 公开 |
| `schema_migrations.dirty` | 迁移失败保护 | `0` 已完成 / `1` 执行中或失败待修复 |

### 4.3 表设计
- 主键用自增 `BIGINT`；对外暴露用 UUID 字符串列（单列唯一索引）。
- 时间统一 UTC 存储，字段名 `*_at`，类型带毫秒精度。
- 唯一性与外键优先交给数据库约束，不在应用层"先查再插"。
- 不存可派生数据（如冗余计数），除非有明确性能理由并在注释里写明。
- 表名、列名小写蛇形；索引名 `idx_*`、唯一索引 `uk_*`、外键 `fk_*`。
- 软删除用 `state` / `deleted_at` 表示，并说明查询是否需要过滤。

### 4.4 迁移
- 表结构变更写成独立 SQL 迁移文件（`migrations/` 下按序编号），禁止运行时隐式建表。
- 迁移只前进，不写回滚脚本（需要回滚时另写一份新迁移）。
- 迁移文件一旦提交不再修改；改结构就加新文件。

---

## 5. 测试

### 5.1 只写"会影响使用"的测试
判断标准：**这个断言失败，是否意味着用户用不了这个功能？** 否则不写。

写：
- 登录 / 刷新 / 校验令牌的通过与拒绝（含过期、错误密码、错误 serverId）
- 鉴权中间件拒绝未授权请求
- Yggdrasil 响应的关键结构与状态码（缺少字段会导致客户端起不来的那种）
- 皮肤上传的格式 / 尺寸 / 权限校验
- 类型字段写入读出后语义不变（Int 存的枚举取回来还是同一个值）

不写：
- getter / setter、纯转发 handler、时间格式化、JSON 字段拼装细节
- 覆盖率驱动的补齐测试、针对私有函数细节的测试
- 需要引入 mock 框架才能测的分支（说明设计有问题，先改设计）

### 5.2 形态
- 标准库 `testing` + `net/http/httptest`；默认不引第三方断言库。
- 表驱动，一个用例只断言关键行为，失败信息用 `t.Fatalf` 写清期望与实际。
- 测试不得依赖网络、不得 `sleep`、不得写仓库内文件、不得连生产库。
- 可重复运行：用内存 SQLite 或每次重建的临时库，跑完清理。
- 不写 `t.Skip` 掩盖失败；确实无法测就删掉该测试并说明。

### 5.3 运行
```powershell
go build ./...
go vet ./...
go test ./...          # 只测一个包：go test ./internal/auth
```
上述命令在 Go 装好后可执行（见 §11）；**未跑就写"未跑"**。

---

## 6. 设计行为

### 6.1 分层
- `cmd/<binary>`：装配与启动，不写业务逻辑。
- `internal/<domain>`：handler / service / store 三层，依赖单向向内（handler → service → store）。
- 跨领域共享的只有：类型模型、错误定义、日志入口、配置。不允许 `auth` 直接调 `skin` 的 store。
- 业务逻辑不进 handler，SQL 不进 service。

### 6.2 API 契约
Yggdrasil 客户端兼容优先：路径、字段名、状态码、错误码按协议来，禁止自创字段名或"更好懂"的结构。
首批目标端点（实现前对照协议文档核对字段，不凭记忆改）：

| 方法 | 路径 | 用途 |
| --- | --- | --- |
| POST | `/authserver/authenticate` | 账号密码登录，返回令牌与角色 |
| POST | `/authserver/refresh` | 刷新令牌 |
| POST | `/authserver/validate` | 校验令牌（204 / 403） |
| POST | `/authserver/invalidate` | 作废令牌（204） |
| POST | `/authserver/signout` | 注销账号密码 |
| POST | `/sessionserver/session/minecraft/join` | 客户端进服上报 |
| GET | `/sessionserver/session/minecraft/hasJoined` | 服务端验证加入 |
| GET | `/sessionserver/session/minecraft/profile/{uuid}` | 查询角色与皮肤属性 |
| GET | `/` | authlib-injector 元数据（serverName / skinDomains / 公钥） |
| POST | `/minecraft/profile/skins` | 上传皮肤 |

- 皮肤文件自身路径可自定义，但必须与 profile 属性里返回的 URL 一致。
- 统一错误响应：`{"error": "...", "errorMessage": "..."}`；成功但无内容用 204。
- 新增非标准端点必须在 `/` 元数据或本文件里声明，不让客户端猜。
- 基底端点：`GET /healthz` 存活检查；`GET /readyz` 检查数据库与迁移（未配置、未迁移或异常均 503）；可选 `GET /web/` 托管 React 构建产物，本轮为空白基底。
- 破坏性变更（字段改名、语义变化）必须先问用户。

### 6.3 安全底线
- 密码用 bcrypt / argon2 存哈希，永不回显、永不打日志。
- 令牌用 `crypto/rand` 生成，落库只存哈希，比较用常量时间比较。
- 所有 SQL 用参数化查询，禁止字符串拼接。
- 上传文件校验 MIME 与实际字节、限制尺寸，落盘文件名由服务端生成。
- 不在仓库提交任何密钥；配置从环境变量或未跟踪的本地文件读取。

### 6.4 配置与时间
- 配置集中在一个 `Config` 结构，启动时一次性校验（缺关键项直接失败并给出明确信息）。
- 端口、数据库 DSN、外链域名、密钥全部可配置，不写死在代码里。
- 时间统一 UTC；JSON 里带时区或明确毫秒；令牌过期用秒或明确单位，不混用。
- 时间依赖通过参数注入（便于测试），不把 `time.Now()` 埋在业务深处。

### 6.5 并发与事务
- 共享状态（缓存、限流、在线会话）必须说明并发安全方式，默认 `sync.RWMutex` 或 `sync.Map`，不裸 map。
- 多表写入放在一个事务里；事务里不做网络调用。
- 不引入无退出机制的后台 goroutine；启动的东西必须能优雅关闭。

---

## 7. 验证：浏览器是唯一验收口

**硬规则：新功能必须用浏览器实际访问过，curl 通过不算成功。**

做法：
1. 后台启动服务，记下实际端口（不要假设 8080 可用）。
2. 用浏览器打开对应 URL。有页面的走页面；纯 API 端点也要在浏览器直接打开，确认状态码、JSON 渲染正常、无浏览器报错。
3. 打开 DevTools：Network 看请求/响应与状态码，Console 确认无报错。
4. 走完整用户路径，而不是只点一个端点（如：注册 → 登录 → 带令牌访问 → 刷新令牌）。
5. 需要真实客户端才能覆盖的部分（Minecraft 启动器），明确写"未覆盖"，不得假装验证过。

交付时必须给出这样一段记录（缺这段视为未验证）：
```
- 功能：登录
- 访问：http://127.0.0.1:PORT/...（浏览器）
- 操作：错误密码 1 次、正确密码 1 次
- 看到：错误密码返回 403 且页面提示"密码错误"；正确密码跳转并显示角色名
- 未覆盖：真实客户端联调（环境无客户端）
```

禁止：
- 用 curl / `Invoke-WebRequest` 的输出替代浏览器验证（本机 `curl.exe` 一定存在，很容易被顺手用来糊弄）。
- 只验证"服务起来了"就宣布功能完成。
- 无法验证时编造截图或描述。**没验证就明说没验证，并给出下一步怎么验证。**

### 7.1 测试通过 ≠ 功能可用
- 单元测试通过不能作为功能可用的证据，功能可用只认 §7 的浏览器结果。
- 测试失败必须修代码或修测试，禁止为了让流程变绿而放宽断言。
- 出现"测试通过但浏览器是坏的"，必须把原因追加到 §12。

---

## 8. 仓库卫生

禁止入库：
- 临时脚本（`*.ps1` / `*.sh` / `*.py` 排查脚本）、一次性 SQL、示例数据
- 缓存与产物：`bin/`、`*.exe`、`coverage.out`、`*.test`、`node_modules/`
- 本地数据：`*.db`、`*.sqlite*`、`*.log`、`.env`、截图、抓包文件
- 本次任务的进度记录、"改动说明"文档（唯一例外：`HANDOVER.md`，见 §13）

临时文件一律放 `$env:TEMP`，任务结束删除。截图证据放 TEMP 或用户指定位置，不进仓库。

`.gitignore` 至少覆盖：`bin/`、`*.exe`、`*.db`、`*.sqlite*`、`*.log`、`.env`、`coverage.out`、`*.test`、`.idea/`、`*.tmp`。

结束任何任务前执行：
```powershell
& "$env:LOCALAPPDATA\GitHubDesktop\app-3.6.7\resources\app\git\cmd\git.exe" status --short
```
（git 不在 PATH，路径规则见 §11；输出中只允许出现本次意图内的文件。）

---

## 9. Git 提交

- **默认不提交**。只有用户明确要求、或"一个完整大功能（端到端可用且经 §7 验收）已完成"时才提交。
- **禁止细碎原子提交**：不允许把一个功能拆成 `feat: 加字段`、`feat: 加校验`、`feat: 接路由` 多条提交。一个功能一次提交完成。
- **小修小补、bugfix、重构由人工提交**：Agent 只留工作区改动 + 报告，不自行 `commit`。
- 禁止 `commit --amend`、`rebase`、`reset --hard`、强制推送、改写历史、清理他人未提交改动。
- 不擅自新建 / 删除 / 切换分支，不擅自 push。
- 提交信息格式：
  ```
  <type>(<scope>): 中文一句话说明

  为什么改、影响哪些行为、如何验证（浏览器 URL + 观察到什么）
  ```
  `type` 取 `feat` / `fix` / `refactor` / `chore` / `docs`。
- 提交前确认：构建与测试跑过（或注明未跑）、无 secrets、`git status` 只剩意图内文件。

---

## 10. 交付说明格式

每次任务结束按此结构回答，缺项视为不合格：

```
- 改了什么：文件 + 行为变化（一句话一个）
- 怎么验证的：浏览器 URL / 操作 / 看到的结果；哪些没验证、为什么
- 复用了什么：沿用了哪些既有实现，新增了哪些并说明为何不能复用
- 遗留与风险：需要用户决策或下一轮处理的事项
```

---

## 11. 本机环境事实

校准时间：2026-10-17。**依赖这些事实前先重新校准**，路径会随软件升级变化。

| 项 | 事实 |
| --- | --- |
| 系统 / Shell | Windows；本会话实际执行 Shell 为 **PowerShell 7.6.5**，已重新校准；文档命令仍使用兼容 5.1 的语法 |
| Go | **已安装 1.27.2**：`C:\Users\shuil\AppData\Local\Programs\Go\bin\go.exe`（含 `gofmt`），已写入用户 PATH。当前 DSH 进程要重启后才可见，见 §12 |
| Go 模块代理 | 已用 `go env -w` 写入 `GOPROXY=https://goproxy.cn,direct`、`GOSUMDB=sum.golang.google.cn`（存于 `%APPDATA%\go\env`），**立即生效**，不依赖进程环境 |
| 网络 | `go.dev`、`proxy.golang.org`、`dl.google.com` 直连被阻断；可用 `golang.google.cn`、`goproxy.cn`。npm 官方源可达但慢（§12） |
| git | **不在 PATH**。GitHub Desktop 自带：`C:\Users\shuil\AppData\Local\GitHubDesktop\app-3.6.7\resources\app\git\cmd\git.exe`（2.53.0） |
| GitHub Desktop 版本目录 | 升级后 `app-x.y.z` 会变，用 `Get-ChildItem "$env:LOCALAPPDATA\GitHubDesktop" -Directory` 取最新 |
| Node / npm | v24.21.0 / 11.19.0，已在 PATH；React 19.3.1 / Vite 8.3.4 / TypeScript 7.0.2，安装与构建实测通过 |
| 数据库客户端 | `mysql` / `sqlite3` / `psql` 均不在 PATH；数据库类型与服务端可用性待确认 |
| 其他 | Python/JDK/curl 见原环境记录；本会话已实测 `rg` 可用 |
| 仓库 | `origin` = https://github.com/ShuilingXu/YggServer-Go.git，分支 `main`，历史仅 `Initial commit` |

重新定位 git：
```powershell
$dir = Get-ChildItem "$env:LOCALAPPDATA\GitHubDesktop" -Directory |
  Where-Object Name -match '^app-\d' | Sort-Object Name -Descending | Select-Object -First 1
& "$($dir.FullName)\resources\app\git\cmd\git.exe" --version
```

---

## 12. 已知的坑（持续追加）

规则：谁踩到谁追加，一行一条，带日期、现象、应对。已失效的条目标注"（已失效）"，不删除。

| 日期 | 坑 | 现象 | 应对 |
| --- | --- | --- | --- |
| 2026-10-17 | `git` 不在 PATH | 直接执行 `git` 报 CommandNotFound | 用 §11 的 GitHub Desktop 自带 `git.exe`；不要因此去装/改环境 |
| 2026-10-17 | Go 已装但 `go` 仍找不到 | 装完 `go` 后本会话 `Get-Command go` 仍为空 | DSH 进程环境在启动时冻结：**重启 DSH / 新开终端**后 PATH 才生效。重启前用 `& "$env:LOCALAPPDATA\Programs\Go\bin\go.exe"`；不要再写"未安装 Go" |
| 2026-10-17 | 生成的 `.go` 文件是 CRLF | `gofmt -l` 报该文件；`gofmt -w` 把整文件改成 LF，git diff 变整文件重写 | 写完就跑 `gofmt -w`；生成源码用 `[IO.File]::WriteAllText($p,$s)` 并保持 LF |
| 2026-10-17 | npm 官方源很慢 | `npm install` 装 27 个包耗 6 分钟，容易超时 | 必要时 `npm config set registry https://registry.npmmirror.com`，改完告知用户（可随时改回官方源） |
| 2026-10-17 | `go.dev` / `proxy.golang.org` 直连被阻断 | curl 返回 000，`go get` 失败 | 用 `golang.google.cn` 下载、`goproxy.cn` 拉模块（已写入 `go env`，不要改回默认） |
| 2026-10-17 | 无 `rg`（已失效） | 搜索命令不可用 | 用 `Select-String` 或 Agent 自身的搜索工具 |
| 2026-10-17 | `curl.exe` 一定存在 | 容易顺手拿 curl 结果当验收 | 验收只认浏览器（§7），curl 仅用于排查 |
| 2026-10-17 | 本会话环境与旧记录不同 | 当前 Shell 为 PowerShell 7.6.5，`rg` 已可用（旧“无 rg”记录已失效） | 以当前 `Get-Command` 和 `$PSVersionTable` 重新校准为准 |
| 2026-10-17 | PowerShell 原生命令参数不能写 `--datadir=(Join-Path ...)` | MySQL 收到空 datadir 并尝试使用程序目录，初始化拒绝执行 | 先计算路径，再传入完整的 `"--datadir=$myData"` 参数 |
| 2026-10-17 | Windows 上 Vite 会占用原生依赖文件 | 开发服务运行期间执行 `npm ci`，删除 rolldown 的 `.node` 文件报 EPERM | 先停止本任务启动的 Vite，再运行 `npm ci`；不要改权限或清理其他进程 |
|  |  |  |  |

---

## 13. 本文件的维护

- 本文件是活文档：约定变化改对应章节，踩坑追加到 §12，不要只在末尾堆内容。
- 修改时保持"可执行"：每条规则要么能一眼判断是否违反，要么给出具体命令 / 模板。
- 不写进度日志、不写"本次改动说明"；这些属于交付说明（§10），不进仓库。
- `HANDOVER.md` 是唯一允许长期留在仓库的状态文档：只写"当前状态 + 下一步 + 待决策"，按状态覆盖更新，不追加任务流水，也不重复本文件的规则。
- 大改本文件前先问用户（属于流程变更，不是普通代码改动）。
