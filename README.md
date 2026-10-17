# YggServer-Go

Go + React 的 Minecraft Yggdrasil 登录服务与皮肤站。数据库以 PostgreSQL 为主，兼容 MySQL。

当前仅为可运行基底：配置、HTTP、元数据、公钥加载、健康检查、安全访问日志、双数据库迁移及 React 工程。React 挂载空根节点，没有 UI；登录、会话、皮肤上传和角色/衣柜操作尚未实现，相关业务路径返回 404。

## 环境与启动

需要 Go 1.27+、Node.js 24 和 npm；数据库建议 PostgreSQL 17+ 或 MySQL 8.4+。后端使用标准库 `net/http`，没有额外路由框架。

Go 的第三方运行依赖仅为 pgx 和 go-sql-driver/mysql 两个数据库驱动及其传递依赖；不引入驱动就需要自行实现数千行数据库线协议、认证、编码和连接管理，并承担兼容性与安全风险。

在仓库根目录运行（PowerShell；若 `go` 不在 PATH，使用 AGENTS.md 中的绝对路径）：

```powershell
go run ./cmd/server -generate-key "$env:TEMP/ygg-signing.pem"
$env:YGG_SIGNING_KEY_FILE = "$env:TEMP/ygg-signing.pem"
$env:YGG_LISTEN_ADDR = '127.0.0.1:8080'
go run ./cmd/server
```

私钥为 RSA 4096，生成命令拒绝覆盖已有文件。正式部署时将密钥保存在受限目录并做好备份；POSIX 使用 0600，Windows 还需由部署方设置目录 ACL。启动只加载现有密钥，不自动生成或更换。支持 PKCS#1 / PKCS#8 PEM；对外仅输出公钥。

未配置数据库时，元数据、前端和存活检查仍可运行，`/readyz` 返回 503。配置了连接串但无法连接时，启动直接失败。使用 `127.0.0.1:0` 可分配空闲端口，实际地址见启动日志。

## 数据库

先在数据库服务器创建空数据库，再设置环境变量。应用不会创建数据库，也不会在 HTTP 启动时执行迁移。

PostgreSQL：

```powershell
$env:YGG_DATABASE_DRIVER = 'postgres'
$env:YGG_DATABASE_DSN = 'postgres://USER:PASSWORD@127.0.0.1:5432/ygg?sslmode=disable'
go run ./cmd/server -migrate
go run ./cmd/server
```

MySQL：

```powershell
$env:YGG_DATABASE_DRIVER = 'mysql'
$env:YGG_DATABASE_DSN = 'USER:PASSWORD@tcp(127.0.0.1:3306)/ygg'
go run ./cmd/server -migrate
go run ./cmd/server
```

连接串中的 USER/PASSWORD 是占位符。生产连接请启用数据库 TLS。配置仅从进程环境读取，不自动加载 `.env`，不要把真实连接串写进仓库。Go 驱动统一使用 UTC；MySQL 强制 `parseTime`、禁用多语句执行。PostgreSQL 使用 `TIMESTAMPTZ(3)`，MySQL 使用 `DATETIME(3)`。

`migrations/postgres` 与 `migrations/mysql` 保持同样的迁移编号和业务语义，分别使用对应数据库的 SQL。现有模型包括 accounts、textures、players、closet；枚举及布尔字段均为 SMALLINT，字典见 AGENTS.md §4.2。各表有自增 BIGINT 主键，公开实体使用独立 UUID 唯一列；材质 hash 只建普通索引，允许同一文件对应多个皮肤条目。没有积分、冗余计数或额外 UUID 映射表。

删除材质时清空角色引用、级联衣柜；删除账号时若仍有上传材质，外键阻止物理删除。账号 `state=2` 表示注销，后续业务必须过滤状态，不能把软删除当作物理删除。角色材质类型、归属及私有访问控制将由后续 service 校验，本轮没有开放写接口。

每个编号 SQL 文件只含一条 DDL，避免手写通用 SQL 分割器。显式迁移命令建立历史表，取得数据库会话锁，记录 SHA-256 校验和与 dirty 状态后执行；重复运行不会重复建表。已执行的 SQL 不可修改，新增变更必须追加文件。SQL 保持 LF 换行，`.gitattributes` 固定换行以保证跨系统校验和一致。

MySQL DDL 不能事务回滚，所以迁移失败或执行中断会保留 dirty=1，迁移命令与就绪检查都会拒绝继续。修复前先备份并核对对应 SQL、实际表结构与历史记录；完整执行过的 DDL 才能由运维清除对应 dirty 标记，未完整执行则先修复表结构。没有自动清 dirty、回滚或强制重放开关。

## React 开发与托管

```powershell
cd web
npm ci
$env:YGG_BACKEND_URL = 'http://127.0.0.1:8080'
npm run dev
```

访问 Vite 输出地址下的 `/web/`。当前页面为空白，React 只发起存活检查。开发代理将 `/healthz`、`/readyz` 和未来业务 API 转发给 Go，浏览器不需要跨域配置；`YGG_BACKEND_URL` 只供开发服务器使用，不进入前端包。不要用 `VITE_` 变量存密钥。

```powershell
npm run build
cd ..
$env:YGG_WEB_DIR = (Resolve-Path web/dist).Path
go run ./cmd/server
```

构建产物可由 Go 托管在 `/web/`；API 根路径 `/` 始终保留给 authlib-injector 元数据。生产部署由反向代理提供 HTTPS。`web/dist` 和 `node_modules` 已忽略，不提交构建产物。

## 配置与端点

| 环境变量 | 默认值 / 用途 |
| --- | --- |
| `YGG_LISTEN_ADDR` | `127.0.0.1:8080` |
| `YGG_PUBLIC_URL` | 未设置时使用实际监听地址；部署时设为公网 HTTP(S) origin，不含路径、query 或凭证 |
| `YGG_SERVER_NAME` | `YggServer-Go` |
| `YGG_SKIN_DOMAINS` | 额外材质域名，逗号分隔；自动加入公网 URL 的 hostname，不信任请求 Host |
| `YGG_SIGNING_KEY_FILE` | HTTP 启动必填，私钥路径 |
| `YGG_DATABASE_DRIVER` | `postgres`，可选 `mysql` |
| `YGG_DATABASE_DSN` | 迁移时必填；未设置则就绪检查返回 503 |
| `YGG_WEB_DIR` | 可选，指向包含 index.html 的前端构建目录 |
| `YGG_BACKEND_URL` | Vite 开发代理目标，默认 `http://127.0.0.1:8080` |

| 方法 | 路径 | 行为 |
| --- | --- | --- |
| GET / HEAD | `/` | authlib-injector 元数据和 PEM 公钥；未实现非邮箱登录，功能标记为 false |
| GET / HEAD | `/healthz` | HTTP 进程存活，200 |
| GET / HEAD | `/readyz` | 数据库可访问且迁移完整、校验和一致、无 dirty 时 200，否则 503 |
| GET / HEAD | `/web/` | 配置 `YGG_WEB_DIR` 时托管 React 产物 |

JSON 错误采用 `{error,errorMessage}`。所有请求带 `X-Request-ID` 和 `X-Authlib-Injector-API-Location`。访问日志只含 request_id、method、路由模板 path、status、dur_ms、ip_hash；未知路径被替换，IP 用进程随机密钥做 HMAC，不记录请求体、query、鉴权头或 Cookie。进程结束后不能跨重启关联 IP。Ctrl+C / SIGTERM 触发优雅关闭。

## 验证与目录

```powershell
go build ./...
go vet ./...
go test ./...
cd web
npm ci
npm run build
```

`cmd/server` 仅负责装配与生命周期，`internal/config` 读取配置，`internal/database` 管理两种数据库连接，`internal/httpserver` 提供基础 HTTP 入口，`internal/signing` 管理密钥，`internal/model` 定义存储枚举；`migrations` 包含 SQL 和显式迁移入口，`web` 包含 React/Vite 工程。没有预先铺设空的 auth/session/skin service/store。

CI 配置会运行格式检查、Go 构建/vet/测试、React 构建，并在 PostgreSQL 17 与 MySQL 8.4 上分别重复运行迁移。测试本身不依赖网络或生产数据库。浏览器验收及未覆盖范围见 HANDOVER.md。
