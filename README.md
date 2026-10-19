# YggServer-Go

Go + React 的 Minecraft Yggdrasil 登录服务与皮肤站，PostgreSQL 为主，兼容 MySQL。Redis 用于原子限流和短期进服凭据；账号与令牌的撤销状态保存在 SQL。Yggdrasil 是程序内置能力，不需要插件。正式 React 前端位于同级 YggWeb-React，按 Blessing Skin 默认布局重写；本仓库 web/ 保留早期空白基底。

## 环境与启动

需要 Go 1.27+；数据库使用 PostgreSQL 17+ 或 MySQL 8.4+；Redis 7+。仅开发前端时需要 Node.js 24 与 npm。HTTP 使用标准库 net/http。

运行依赖为 pgx、go-sql-driver/mysql、go-redis 和 x/crypto/bcrypt：数据库与 Redis 客户端复用连接池、认证、TLS 和协议编码，避免自行维护数千行线协议；bcrypt 复用经过维护的密码哈希实现，避免自行实现高风险密码算法。

```powershell
go run ./cmd/server -generate-key "$env:TEMP/ygg-signing.pem"
$env:YGG_SIGNING_KEY_FILE = "$env:TEMP/ygg-signing.pem"
$env:YGG_DATABASE_DRIVER = 'postgres'
$env:YGG_DATABASE_DSN = 'postgres://USER:PASSWORD@127.0.0.1:5432/ygg?sslmode=disable'
$env:YGG_REDIS_URL = 'redis://127.0.0.1:6379/0'
$env:YGG_TEXTURES_DIR = 'D:/YggServer/textures'
$env:YGG_PUBLIC_URL = 'https://skin.example.com'
go run ./cmd/server -migrate
go run ./cmd/server
```

数据库须提前创建。连接串中的凭证是占位符，生产部署应配置数据库 TLS、Redis ACL/TLS 和 HTTPS。配置仅从环境变量读取，不加载 `.env`。Redis 凭证和连接错误不会原样记录到日志。材质目录由部署方指定，启动时创建；多个应用进程须共用同一数据库、Redis 命名空间及材质目录。

MySQL 使用 `YGG_DATABASE_DRIVER=mysql` 与 `YGG_DATABASE_DSN=USER:PASSWORD@tcp(127.0.0.1:3306)/ygg`。两库统一 UTC；MySQL 强制 parseTime 并禁用多语句执行。迁移命令不依赖 Redis 或签名私钥，HTTP 启动不会自动执行迁移。

私钥使用 RSA 4096；生成命令拒绝覆盖。启动只加载已有 PKCS#1/PKCS#8 PEM，不自动换钥。生产应设置私钥目录 ACL 并备份。

未配置数据库时仍可运行元数据、存活检查和前端基底，业务接口返回 503。注册、登录、刷新和改密等凭证操作需要 Redis，Redis 故障时拒绝继续处理；进服验证也需要 Redis。配置了数据库或 Redis 但无法连接时，启动失败。

## 配置

| 环境变量 | 默认值 / 用途 |
| --- | --- |
| `YGG_LISTEN_ADDR` | `127.0.0.1:8080`；设为 `127.0.0.1:0` 时实际端口见启动日志 |
| `YGG_PUBLIC_URL` | 未设置时使用实际监听地址；部署时设为不含路径、query 或凭证的 HTTP(S) origin |
| `YGG_SERVER_NAME` | `YggServer-Go` |
| `YGG_SKIN_DOMAINS` | 额外材质域名，逗号分隔；自动加入公网 URL 的 hostname |
| `YGG_SIGNING_KEY_FILE` | HTTP 启动必填，持久私钥路径 |
| `YGG_DATABASE_DRIVER` | `postgres` 或 `mysql` |
| `YGG_DATABASE_DSN` | SQL 连接串；迁移与业务功能必填 |
| `YGG_REDIS_URL` | `redis://` 或 `rediss://` 连接 URL，支持 ACL、密码和数据库编号 |
| `YGG_REDIS_PREFIX` | `ygg:`；共享 Redis 时按站点隔离，最多 64 字节且不含空白 |
| `YGG_TEXTURES_DIR` | 材质持久目录；上传、删除和原图分发需要配置 |
| `YGG_WEB_DIR` | 可选，包含 index.html 的 React 构建目录 |
| `YGG_UUID_STRATEGY` | `v3`（离线 UUID，改名变化）或 `v4`（随机 UUID，改名保留）；仅影响新角色 |
| `YGG_SMTP_ADDR` | 可选 SMTP `host:port`；启用邮件时必须配置 PUBLIC_URL 与 MAIL_FROM |
| `YGG_SMTP_MODE` | `starttls`；另支持 `tls`（隐式 TLS）、`plain`（仅 loopback 测试） |
| `YGG_SMTP_USER` / `YGG_SMTP_PASSWORD` | 可选 SMTP 认证，必须成对配置；生产认证使用 TLS |
| `YGG_MAIL_FROM` | 发件人邮箱，支持合法显示名 |
| `YGG_VERIFICATION_TTL` / `YGG_RESET_TTL` | `24h` / `1h`；Go duration，范围 1m–168h |
| `YGG_UPLOAD_MAX_BYTES` | `1048576`；输入 PNG 上限，最多 16777216 字节 |
| `YGG_UPLOADS_PER_MINUTE` | `0` 不额外限流；启用时每账号每分钟最多指定次数，失败尝试也计入 |
| `YGG_STORAGE_QUOTA_BYTES` | `0` 不限额；按账号所有材质条目重编码后的字节合计，包括私有/下架条目，同 hash 每条分别计量 |
| `YGG_BACKEND_URL` | Vite 开发代理目标，默认 `http://127.0.0.1:8080` |

`GET /` 发布元数据、公钥、域名白名单与 `feature.non_email_login=true`；`GET /healthz` 检查进程；`GET /readyz` 检查完整迁移历史、dirty 状态和已配置的 Redis。故障返回 503。所有响应带 Request ID 和 ALI 头。访问日志只记路由模板、状态、耗时与 IP 的 HMAC，不记录请求体、query、凭证或 Cookie。

## 账号与角色

注册使用规范化的小写邮箱、8–72 字节密码和 3–16 位 ASCII 字母/数字/下划线角色名。账号与初始角色在同一事务创建，注册不自动登录，不自动授予管理员权限。密码使用 bcrypt；账号状态和权限每次鉴权都从 SQL 检查。

Web 接口使用 `Authorization: Bearer ACCESS_TOKEN`，不使用 Cookie，因此当前不依赖浏览器 CSRF 会话。Web 和游戏令牌通过整数用途字段隔离。Web 令牌有效期 24 小时；游戏令牌有效期 24 小时，可在 7 天内刷新。刷新原子旋转令牌；每账号最多 20 个令牌，颁发时淘汰最旧令牌。凭证入口共享每 IP 每分钟 20 次上限，账号密码校验另有每账号每分钟 20 次上限；超限返回 429 和 Retry-After。当前按直连 RemoteAddr 限流和匹配进服 IP，不信任转发头；部署在反向代理后时该地址会是代理地址，不能直接将 hasJoined 的可选 ip 参数用于真实客户端 IP 匹配。

accessToken 使用 crypto/rand 生成，只保存 SHA-256 哈希；clientToken 保存校验哈希及 AES-GCM 加密副本，密钥由对应 accessToken 派生，以支持 refresh 省略 clientToken 时回显原值。此例外已获用户批准并登记在 AGENTS.md。

| 方法 | 路径 | 请求 / 行为 |
| --- | --- | --- |
| POST | `/api/auth/register` | `{email,password,playerName}`；201，冲突 409 |
| POST | `/api/auth/login` | `{username,password}`；邮箱或角色名，返回 accessToken |
| POST | `/api/auth/forgot-password` | `{email}`；合法请求统一 202，不回显凭据；未配置邮件返回 503 |
| POST | `/api/auth/reset-password` | `{token,newPassword}`；一次性消费并撤销全部 Web/游戏令牌；204，无效/过期 403 |
| POST | `/api/auth/verify-email` | `{token}`；一次性确认当前邮箱；204，无效/过期 403 |
| POST | `/api/account/verification` | Web 鉴权，向当前邮箱发送验证邮件；202 |
| GET | `/account-recovery` | 邮件链接的最小功能表单；fragment 承载凭据，加载后立即从地址栏移除 |
| POST | `/api/auth/logout` | 撤销当前 Web 令牌；204 |
| GET | `/api/account` | 当前账号，不含密码哈希 |
| PUT | `/api/account/password` | `{password,newPassword}`；撤销全部令牌 |
| PUT | `/api/account/email` | `{password,email}`；重置邮箱验证状态并撤销全部令牌 |
| DELETE | `/api/account` | `{password}`；软注销、删除角色和衣柜、材质转为私有、撤销全部令牌 |
| GET | `/api/admin/users?offset=0` | 管理员分页，每页 50 |
| PUT | `/api/admin/users/{uuid}` | `{state,permission}`；state 为 0/1，permission 为 0/1 |
| GET / POST | `/api/players` | 列出角色 / `{name}` 创建角色，每账号最多 20 个 |
| PUT / DELETE | `/api/players/{uuid}` | `{name}` 改名 / 删除；撤销绑定该角色的令牌 |
| PUT | `/api/players/{uuid}/textures` | `{skin?:textureUUID,cape?:textureUUID}`；空字符串清空，省略则保留 |

角色 UUID 默认使用离线 v3 算法；`YGG_UUID_STRATEGY=v4` 可选择随机 UUID。每个角色持久保存整数策略，v3 改名重新计算 UUID，v4 改名保持 UUID；已有角色迁移后登记为 v3，修改配置只影响新角色，不重算旧身份。v4 不代表 Mojang 正版身份。角色名在两库均不区分大小写地唯一。注销账号保留邮箱与账号 UUID，不能通过再次注册恢复；所有者不允许从 Web 自行注销或被管理员降权/封禁。

首次所有者需先注册，再由拥有部署终端权限的人显式执行：

```powershell
go run ./cmd/server -promote-owner 'owner@example.com'
```

管理员只能管理普通账号；所有者可授予/撤销管理员和封禁用户，不能通过这些接口修改其他所有者。权限或封禁变化立即撤销目标账号的全部令牌。

## 材质与衣柜

| 方法 | 路径 | 请求 / 行为 |
| --- | --- | --- |
| GET | `/api/textures?offset=0` | 公开皮肤库，默认每页 50；检索/分页参数见下文 |
| GET | `/api/textures/mine?offset=0` | 我的上传，含私有条目 |
| POST | `/api/textures` | multipart：name、type（0 steve / 1 alex / 2 cape）、public（0/1）、file |
| GET | `/api/textures/{uuid}` | 公开详情；私有条目须携带上传者的 Web 凭证 |
| GET | `/api/textures/{uuid}/file` | Web 鉴权原图读取，允许上传者读取未穿戴私有材质 |
| PUT | `/api/textures/{uuid}` | `{name,type,public}`；仅上传者，类型变更重新校验尺寸 |
| DELETE | `/api/textures/{uuid}` | 仅上传者；清理角色和衣柜引用，最后引用删除后移除文件 |
| GET | `/api/closet?offset=0` | 衣柜及自定义 itemName |
| PUT / DELETE | `/api/closet/{uuid}` | `{name}` 收藏或改备注 / 移除；uuid 是材质 UUID |
| GET | `/api/textures/{uuid}/avatar` | 2D PNG 头像，叠加帽子层；披风返回 400 |
| GET | `/api/textures/{uuid}/preview` | PNG 平面正面预览，支持 steve/alex/旧版皮肤及披风 |
| POST | `/api/textures/{uuid}/reports` | Web 鉴权，`{reason}`；201，重复待处理举报 409 |
| GET | `/api/admin/reports?offset=0` | 管理员举报列表，每页 50 |
| PUT | `/api/admin/reports/{uuid}` | `{resolution:1,state:1,note}` 处理并下架；`{resolution:2,note}` 驳回；204 |
| PUT | `/api/admin/textures/{uuid}/state` | `{state:0或1}` 上架/下架；204 |
| GET | `/api/admin/audit?offset=0` | 管理员审计列表，每页 50，字段见下文 |
| GET | `/textures/{hash}` | image/png，ETag；仅分发公开或已穿戴且所属账号正常的材质 |

文件必须声明 image/png，实际字节也必须是 PNG，输入默认最多 1 MiB，可通过 YGG_UPLOAD_MAX_BYTES 调整。解码前检查宽、高、比例与 1600 万像素上限，宽高最多 8192；steve 支持 64×32 与 64×64 的整数倍，alex 仅正方形，cape 支持 64×32 的整数倍，以及经透明补边的 22×17 整数倍。服务端重编码后计算 SHA-256，文件名由 hash 决定，不使用客户端路径。

上传在事务中写入材质与衣柜，可同时穿戴；同 hash 文件通过数据库会话锁串行发布和删除，防止多个进程误删共享文件。文件先原子发布，失败后尝试清理未引用文件；数据库不可用或进程崩溃的极端窗口仍可能留下不可达文件，需要后续离线核对，不能直接按文件名批量删除。

只有自己的材质或衣柜内的公开材质可以穿戴，skin/cape 分类不可混用。公开转私有时清除其他用户衣柜与穿戴引用。已穿戴的私有材质为了游戏客户端读取，会以内容 hash URL 分发；“私有”控制条目与未穿戴原图的访问，不代表已发布到游戏的材质可以保密。原图每次访问均重新检查数据库，使用 max-age=0, must-revalidate，避免隐私变更被一年公共缓存绕过。

## 原生 Yggdrasil

以下路由始终由 Go 程序注册，字段和状态码对照 [authlib-injector 规范](https://yushijinhun.github.io/authlib-injector/en/yggdrasil-server-technical-specification.html)，业务参考见 blessing-skin-report.md。

| 方法 | 路径 | 行为 |
| --- | --- | --- |
| POST | `/authserver/authenticate` | username、password、可选 clientToken、requestUser、agent；多角色邮箱登录可不绑定 |
| POST | `/authserver/refresh` | accessToken、可选 clientToken / selectedProfile / requestUser；原子撤销旧令牌 |
| POST | `/authserver/validate` | 有效 204，无效 403 |
| POST | `/authserver/invalidate` | 忽略 clientToken，撤销令牌；204 |
| POST | `/authserver/signout` | username/password；撤销账号全部令牌 |
| POST | `/sessionserver/session/minecraft/join` | accessToken、selectedProfile、serverId；Redis 保存 60 秒 |
| GET | `/sessionserver/session/minecraft/hasJoined` | username、serverId、可选 ip；成功消费凭据并返回签名角色，失败 204 |
| GET | `/sessionserver/session/minecraft/profile/{uuid}` | 默认 unsigned=true，unsigned=false 时 SHA1withRSA 签名；不存在 204 |
| POST | `/api/profiles/minecraft` | 角色名数组，最多 100 项；仅返回存在且账号正常的角色 |
| PUT / DELETE | `/api/user/profile/{uuid}/{skin\|cape}` | 游戏 Bearer 令牌；multipart 上传或清空，204 |
| POST | `/minecraft/profile/skins` | 当前绑定角色上传皮肤；model 或 variant 支持 slim / classic，204 |

没有材质上传凭证时返回 401；其他协议错误使用 error/errorMessage。join 同时检查账号、用途、有效期与绑定角色；hasJoined 检查当时令牌是否仍有效，并匹配角色名和可选 IP。短期凭据一次性消费，IP 不匹配不会消费正确凭据。profile 的 textures 值按协议使用 Base64，签名对象是 Base64 字符串本身；公钥由根元数据提供。

## 数据库与工程

两库各有 21 个前向迁移，包括 accounts、textures、players、closet、tokens、recovery_tokens、reports、audit_events；整数枚举字典在 AGENTS.md。迁移使用校验和、会话锁和 dirty 标记，拒绝篡改、遗漏或失败历史。014 增加 clientToken 加密列，015 撤销无法恢复 clientToken 的早期令牌；016–021 增加角色 UUID 策略、恢复凭据、下架状态、举报与审计。已执行 SQL 不可修改，后续变更追加迁移。

```powershell
go build ./...
go vet ./...
go test ./...
gofmt -l internal cmd migrations
```

测试只使用标准库 testing，不连接外部网络或生产数据库。浏览器与双数据库集成验收记录见 HANDOVER.md。真实 Minecraft/authlib-injector 客户端联调未覆盖。

正式界面在同级 `YggWeb-React` 开发，使用其 `npm run dev` 和 `npm run build`；将 `YGG_WEB_DIR` 指向该项目 `dist` 即可托管 `/web/`，前端采用 hash 路由。API 根目录仍发布元数据。没有增加 BS 的 PHP 插件/前端兼容层或旧库迁移；站点运营与 OAuth/OIDC 见下文；邮件最终消费继续使用 `/account-recovery` 功能页。
## 邮件与恢复凭据

启用 SMTP 后，Web 用户通过 `/api/account/verification` 请求验证邮件，未登录用户通过 `/api/auth/forgot-password` 请求重置。默认不强制未验证账号验证后才能登录。找回入口对不存在邮箱、已封禁账号、发送冷却及 SMTP 投递失败均保持 202；非法邮箱 400，邮件/Redis 未配置或不可用 503。投递失败在外层脱敏记录，不向浏览器返回 SMTP 原始文本。

凭据来自 crypto/rand，只存 SHA-256；绑定账号、发信时邮箱、整数用途和有效期。邮箱验证与重置不可混用，账号锁事务保证只能消费一次；改密、改邮箱、注销时使该账号恢复凭据失效。重置密码还撤销所有 Web/游戏令牌。发送按规范化邮箱摘要冷却 60 秒；发送与提交同时复用现有每来源地址每分钟 20 次凭据入口限制，Redis 故障时拒绝继续处理。SMTP 发送不在 SQL 事务中。

邮件链接为 `PUBLIC_URL/account-recovery#action=verify-email或reset-password&token=...`。fragment 不会成为 HTTP query；功能表单清除地址栏 fragment，使用 no-store、no-referrer 和限制脚本来源的 CSP，通过 POST 提交。邮件未配置时不会打印凭据或生成可绕过邮件的调试链接。生产 SMTP 投递、发信域名与 TLS 证书仍需部署方验证。

## 检索、配额与预览

公开库、个人上传和衣柜共用 `q`、`type=0|1|2`、`uploader=账号UUID`、`sort=newest|oldest|name`、`offset`、`limit=1..100`。`q` 不区分大小写，`%`、`_` 按字面搜索；衣柜搜索自己的 itemName，其他列表搜索材质名。排序以主键打破同名顺序。响应仍为数组，`X-Total-Count` 返回过滤后的可见条目总数，`X-Limit` 返回每页大小，存在下一页时提供 `Link: <...>; rel="next"`。总数和当前页为独立查询，写入并发时总数可能短暂变化。

额度检查与上传条目写入复用账号锁事务，防止并发超额；超额返回 413。上传频率启用后复用 Redis，超限返回 429 与 Retry-After，Redis 不可用返回 503。现有 PNG 字节、MIME、比例和总像素限制继续有效。删除下架条目仍可由上传者执行，用于释放配额；下架不会自动删除文件。

头像与预览接受 `size=16..512`，默认 128；头像为正方形，预览以 size 为输出高度，身体或披风按比例确定宽度。PNG 使用最近邻缩放；未实现 3D 和渲染缓存，响应为 private,no-store，避免尺寸缓存污染与隐私变更后的旧缓存。私有内容仅上传者 Web 凭证可查看，下架内容返回 404。按 UUID 的接口不会因已知 hash 绕过权限。

## 举报、下架与审计

举报理由必须非空且不超过 1000 字符；每账号每分钟最多 10 次举报，依赖 Redis。唯一约束保证每用户对每材质最多一个待处理举报；处理后可再次举报。report state 为 0 待处理 / 1 已处理 / 2 已驳回，已结束举报重复处理返回 409。处理并下架必须显式提交 resolution=1,state=1；驳回不改变材质状态。普通管理员只能处理普通用户上传的材质，站点所有者可处理任意上传者的材质。

材质 state 为 0 正常 / 1 下架，独立于 public/private；修改隐私不能重新上架。公开库、个人列表、衣柜、详情、头像/预览、角色穿戴、Yggdrasil profile 和 hash 分发均应用下架判断。既有穿戴引用保留，上架后恢复显示；下架期间 profile 不返回该条目。共享 hash 只要还有其他允许分发的条目就保留分发，不能保证撤回客户端已下载的图像。没有增加管理员直接物理删除文件接口。

审计仅记录成功提交的管理操作，与变更同事务写入；不提供修改/删除审计的 HTTP 接口。action 为 0 用户管理 / 1 终端所有者授权 / 2 材质上架下架 / 3 举报处理；result 为 0 已执行 / 1 举报已驳回。targetState、targetPermission 表示本次目标值，-1 表示该字段未改动或早期记录未知。记录包含 actor、target、requestId、createdAt；CLI actor 为空，requestId 为 cli。举报理由及处理说明只在举报记录中保存，不把完整请求或凭据写进审计。

## 材质一致性与清理

CLI 不依赖 Redis 或签名私钥，但需要已迁移的数据库及材质目录：

```powershell
go run ./cmd/server -check-textures
# 默认仅报告，孤立文件至少保留 24h；下面的执行模式需显式指定。
go run ./cmd/server -check-textures -orphan-min-age 48h -apply-prune
```

输出 JSON 的 referenced 是不同 hash 的数据库引用数；missing、corrupt、orphans、removed 分别列出缺失、内容损坏、超过保留时间的孤立文件及本次已删除文件。非 hash 文件、暂存文件、目录和符号链接记入 ignored，不删除。发现缺失/损坏或检查错误时命令非零退出；检查错误仅向外报告脱敏失败信息。

清理沿用上传/删除的数据库会话锁，删除前再次核对全部条目引用和文件身份；私有、下架及封禁账号的条目仍算引用。有引用、状态变化或检查失败则保留文件。目录检查是在线巡检快照，写入并发时结果可能变化；备份校验应暂停写入后执行。工具不会自动修复损坏文件或删除数据库条目。

## 备份与恢复操作

恢复集合必须同时包含数据库、材质目录、签名私钥和迁移版本；Redis 只存短期进服凭据及限流状态，不作为持久身份数据恢复。备份目的目录应事先配置仅运维可读的 ACL，凭据通过客户端密码提示或现有凭据文件提供，禁止把真实密码写进命令历史或报告。

1. 暂停所有应用写入进程（包含其他节点和维护命令），确认没有后台上传/删除；停止写入后保持停机直到数据库转储和文件复制完成。先执行 `-check-textures`，处理缺失或损坏；保留该检查结果和 `schema_migrations` 的 version/checksum/dirty 查询结果。
2. 使用与数据库版本匹配的官方客户端，按实际环境替换 HOST、PORT、BACKUP_USER、数据库名和绝对路径。以下两组选用一组，不执行对另一种数据库的命令：

```powershell
# PostgreSQL：-W 交互提示密码；pg_dump 使用文件参数，避免 Shell 改写二进制输出。
pg_dump -h HOST -p 5432 -U BACKUP_USER -W --format=custom --no-owner --file='D:/YggBackups/snapshot/database.dump' ygg
# MySQL：-p 交互提示密码；使用 result-file，避免 Windows PowerShell 重定向编码改变 SQL。
mysqldump -h HOST -P 3306 -u BACKUP_USER -p --single-transaction --routines --triggers --events --hex-blob --result-file='D:/YggBackups/snapshot/database.sql' ygg
```

3. 在同一停写窗口内复制材质和私钥，备份目录必须是新的目录，不能嵌套在材质目录中；复制后核对文件数量/内容清单，并保存当前程序版本和迁移记录。下面的目的目录须事先建好且不含旧快照：

```powershell
$backupRoot = 'D:/YggBackups/snapshot'
Copy-Item -LiteralPath $env:YGG_TEXTURES_DIR -Destination "$backupRoot/textures" -Recurse -ErrorAction Stop
Copy-Item -LiteralPath $env:YGG_SIGNING_KEY_FILE -Destination "$backupRoot/signing.pem" -ErrorAction Stop
Get-ChildItem -LiteralPath "$backupRoot/textures" -File |
  Get-FileHash -Algorithm SHA256 |
  Select-Object Hash,@{Name='File';Expression={Split-Path $_.Path -Leaf}} |
  Export-Csv -LiteralPath "$backupRoot/textures-manifest.csv" -NoTypeInformation -Encoding UTF8
```

4. 恢复到**新的隔离数据库和目录**，不要覆盖生产库。先用数据库管理员创建空库并配置访问权限，再使用下面对应的恢复命令；私钥恢复使用备份原件，不运行 generate-key 替换身份签名密钥。

```powershell
pg_restore -h RESTORE_HOST -p 5432 -U RESTORE_USER -W --no-owner --no-acl --dbname=ygg_restore 'D:/YggBackups/snapshot/database.dump'
# MySQL 使用客户端 source，避免 PowerShell 对 SQL 文件做文本管道转换。
mysql -h RESTORE_HOST -P 3306 -u RESTORE_USER -p --database=ygg_restore --execute='source D:/YggBackups/snapshot/database.sql'
```

5. 将配置指向恢复库、恢复材质目录及原私钥，先使用与快照匹配的程序检查迁移及 `-check-textures`。确认完整迁移、引用文件内容和原公钥后，再单独规划版本升级及切流。因数据库备份可能恢复旧的 Web/游戏会话与恢复凭据，正式恢复切流前应在恢复库内撤销 `tokens` 和 `recovery_tokens`，让用户重新登录；不要恢复 Redis 短期进服记录。

以上是操作说明，尚未执行数据库备份恢复演练，也未进行生产切流或部署联调。


## 站点运营、积分与授权

正式前端在同级 `YggWeb-React`。站点所有者从 `/web/#/admin/site` 管理站点信息和规则，普通管理员没有配置与 OAuth 应用管理权限。`GET /api/site` 提供前端公开规则；`PUT /api/admin/site` 提交完整配置对象，成功 204，非法配置 400，非所有者 403。配置持久化在 `site_settings`，不缓存；保存后用于后续请求，配置更新与审计同事务。名称同时更新根元数据及前端页头、标题；简介和公告以纯文本展示。

配置字段：`name/description/announcement`；`registration=0|1`；`emailMode=0|1|2`（不限制/白名单/黑名单）、`emailDomains`（空格分隔、大小写不敏感、精确匹配，不隐式包含子域名，注册与改邮箱共用）；`skinDomains`（额外材质域名，空格分隔，加入根元数据白名单，保留启动配置与公开地址的域名）；`maxPlayers=1..100`（1 即单角色，降低上限不删除已有角色）；`singleSession=0|1`（开启后下次登录清除同类旧会话，Web 与游戏各自独立）；`signEnabled=0|1`、`signHours=1..8760`、`signMin/signMax=0..1000000`；`initialPoints/playerCost/uploadCost/privateUploadCost/closetCost=0..1000000`，私有单价不得低于公开；`oauthEnabled=0|1`。公开 URL、SMTP、数据库与密钥继续从部署环境读取，不能在界面改写。

`GET /api/account/points` 返回 `{balance,nextSignAt,entries}`（最近 50 条；流水含 `id/delta/kind/createdAt`）。`POST /api/account/sign` 成功返回更新后的同结构，签到间隔按上次成功时间滚动计算，随机奖励包含上下界；账号行锁保证并发只能成功一次。冷却中 409 `AlreadySigned`，关闭时 403。余额从持久化流水求和，不另存余额。新增角色、上传、收藏、转私有的扣款与业务写入共用账号锁和事务；不足返回 409 `InsufficientPoints`，失败回滚。上传按服务器重编码后的字节数向上取整为 KiB；首次上传自带衣柜条目不另收收藏费；改收藏备注不重复扣费，移除后重新收藏会收费。公开转私有按当前配置补差价；转公开、删除与取消收藏不退款，避免反复操作套取奖励。首次注册的角色不收费。迁移 026 为既有账号一次性发放 1000 初始积分；后续新注册使用当前 `initialPoints`。默认新账号 1000，签到 10–100/24h，新增角色 100，上传与收藏费用默认 0。

### 本站 OAuth2 授权服务

`oauthEnabled` 默认关闭。所有者创建公共客户端：`POST /api/admin/oauth/clients`，JSON `{name,redirectUri}`，201 返回 `{id,name,redirectUri}`；GET 列出应用；DELETE `/api/admin/oauth/clients/{uuid}` 撤销应用及全部关联授权。回调地址精确匹配，仅 HTTPS 或 loopback HTTP，不支持 fragment、通配符与 URL 凭据。

应用打开 `/oauth/authorize?response_type=code&client_id=...&redirect_uri=...&scope=User.Read%20Player.Read&state=...&code_challenge=...&code_challenge_method=S256`，前端在 `/web/#/oauth/authorize` 展示名称、回调与权限，用户登录后显式同意或拒绝。`POST /api/oauth/authorize` 使用 Web Bearer，JSON 同名字段加 `approve`，返回 `{redirect}`；客户端必须验证原样回传的 `state`。拒绝回调 `error=access_denied`。授权码五分钟有效、只能兑换一次、每账号最多保留 50 条有效授权；新授权时清理该账号过期记录。

`POST /oauth/token` 使用 `application/x-www-form-urlencoded`，字段 `grant_type=authorization_code/code/client_id/redirect_uri/code_verifier`。仅接受 S256 PKCE；成功返回 `access_token/token_type=Bearer/expires_in=3600/scope`；错误授权码、过期、重放或 PKCE 错误返回 400 `invalid_grant`。没有密码模式、隐式模式、refresh token 或 OIDC ID Token；本站 OAuth 与第三方 OIDC 登录为两个独立方向。

`GET /oauth/userinfo` 要求 `User.Read`，仅返回 `{sub:账号UUID}`；`GET /oauth/players` 要求 `Player.Read`，返回该用户角色数组。只提供这两个只读 scope，OAuth 令牌不能调用 Web 管理或游戏接口。授权依附于发起时的 Web 会话：Web 退出、改密、改邮箱、封禁、单会话替换等导致父令牌删除时，通过外键同步撤销；访问时还校验父会话过期和账号状态。用户在账号页查看应用并通过 `DELETE /api/account/oauth/{clientUuid}` 单独撤销；此操作不影响本站登录。授权码和 access token 仅存摘要；scope 在数据库中使用 SMALLINT 权限位（1=User.Read、2=Player.Read、3=两者），API 仍使用标准空格分隔字符串。

### 第三方 OIDC 登录

站点所有者配置 `oidcEnabled=1`、`oidcIssuer`（与提供方 issuer 完全一致）、`oidcClientId`、`oidcName`。提供方回调必须登记为 `YGG_PUBLIC_URL/oidc/callback`；生产 issuer 与发现的端点使用 HTTPS，HTTP 仅允许 loopback 隔离测试。机密客户端设置 `YGG_OIDC_CLIENT_SECRET` 并重启；公共 PKCE 客户端可不设置，按提供方应用类型配置。密钥不存站点表、不回显到 API、不进入前端。开发调试请从 `YGG_PUBLIC_URL/web/` 发起，确保返回同一 origin 与同一标签页的 sessionStorage。

登录页通过 `POST /api/auth/oidc/start` 获得 `{url,state}` 后跳转提供方；账号页 `POST /api/account/oidc/start` 使用当前 Web Bearer 发起绑定。服务端 Redis 保存十分钟一次性流程（key 是 state 摘要），浏览器仅在 sessionStorage 保存本次 state，回调清除地址栏中的 code/state，再经 `POST /api/auth/oidc/complete` JSON `{state,code}` 换取本站 Web 令牌。校验 PKCE、nonce、issuer、audience、有效期、JWKS 签名与存在时的 at_hash；流程消费与重放保护使用 Redis GETDEL。提供方的 access/ID token 不持久化、不传给前端。使用 `go-oidc` 和 `oauth2` 处理协议细节，网络有明确超时且拒绝提供方 HTTP 重定向。

外部身份以 issuer + subject 唯一标识。首次登录仅用 `email_verified=true` 的合法邮箱创建新账号，遵守注册开关与邮箱域名规则，复用注册事务创建 `OIDC_` 加随机后缀的角色及初始积分；用户可在角色页改名。同邮箱旧账号不会自动合并，须先以原凭据登录并显式绑定。已绑定身份登录会检查账号状态。新账号的本站密码为不可知随机值；需要游戏密码登录或解绑时，请先通过邮件找回流程设置本站密码（需配置 SMTP）。账号页 GET `/api/account/oidc` 查看绑定；DELETE `/api/account/oidc/{uuid}` JSON `{password}` 要求正确本站密码，解绑与撤销全部会话同事务，并记管理审计。没有实现 GitHub 等非 OIDC 平台的专用 OAuth 登录适配。

审计动作追加 4=站点配置、5=OAuth 应用、6=外部绑定、7=外部解绑。访问日志沿用每请求一条、路由模板和 IP HMAC，4xx 记 WARN、5xx 记 ERROR，其余 INFO，底层不重复打印同一错误；业务日志只含安全标识、结果与 Request ID，不记录 code、state、nonce、PKCE verifier、外部令牌或 Client Secret。迁移仅追加 022–034，旧迁移保持不变。

### 站点运营验收（2026-10-18）

- 构建：`go build ./...`、`go vet ./...`、`go test ./...` 通过，`gofmt -l internal cmd` 无输出。React 的类型检查、测试和生产构建通过。OIDC 新增 go-oidc/oauth2 依赖以复用 JWT/JWKS、签名轮换与协议校验，避免自行实现数百行安全敏感逻辑；没有新增前端依赖。
- 功能：配置、积分、签到、限制、本站 OAuth。访问：实际浏览器 `http://127.0.0.1:55427/__qa`、`/__qa-extra`（PostgreSQL），`http://127.0.0.1:55428` 同路径（MySQL）。操作：保存配置与越权拒绝、并发签到、角色/上传/收藏/隐私消费与不足回滚、域名和注册限制、授权同意/拒绝、错误 PKCE、授权码重放/并发兑换、scope 隔离及撤销。看到：两库最终分别通过 40+22 项检查；并发签到仅一次成功，余额不足不留下业务记录，OAuth 令牌不能用作本站会话。
- 功能：第三方 OIDC。访问：浏览器 `http://127.0.0.1:55407/__qa-oidc`、`http://127.0.0.1:55408/__qa-oidc`，以及正式页面 `http://127.0.0.1:55405/web/#/login`。操作：本地 RSA/JWKS 提供方真实授权跳转、注册/登录/绑定/解绑，错误 nonce、audience、签名、过期、未验证邮箱及重放。看到：两库各通过 15 项拒绝路径与 16 项身份流程检查；正式页面回到用户中心，地址栏清除回调凭据，同邮箱不自动合并。
- 功能：正式 React 页面。访问：`http://127.0.0.1:55407/web/#/admin/site`、`http://127.0.0.1:55405/web/#/points`、账号页和授权页；最终新库冒烟为 `http://127.0.0.1:55415/web/#/register`。操作：保存站点名称/公告、签到、授权同意/拒绝、登录及查看账号。看到：配置立即更新，最终账号积分 1000→1083，签到按钮进入冷却；1280 宽度深浅主题、375×812 深色布局实际查看。移动端未完成触控专项检查。
- 两库最终隔离新库均有 34 个迁移、dirty=0，OAuth scope 为 SMALLINT，无负余额及异常凭据存储。最终两库日志各 81 条访问记录，无重复 Request ID 访问日志，未检出测试密码或凭据字段。浏览器实际访问未鉴权接口得到 403，对应一条 WARN；停止 Redis 后 readyz=503，对应一条 ERROR，随后已恢复 Redis 完成回归。没有迁移用户原有数据库。
- 页面 Console 检查未见未捕获异常。状态码来自实际浏览器 Response.status；内置浏览器没有独立 DevTools Network 面板，未声称操作该面板。未覆盖：Google/Microsoft 等真实平台、生产 TLS/Client Secret、外部 SMTP、真实 Minecraft 客户端。验收服务现已停止，上述地址仅记录当时结果。

## Go 插件机制

插件 API、Hook 时点、开发示例、注册方法与配置契约见 [plugin/README.md](plugin/README.md)。当前采用随程序编译的可信 Go 插件；`YGG_PLUGINS_FILE` 指向仓库外 JSON 配置，默认全部关闭，重启后生效。核心业务限制保持内置。站点配置页向所有者展示运行清单、Hook 与查询入口；不从网页加载可执行代码。

已接入账号创建、会话签发、角色新增/改名/删除、材质上传和签到的前置校验与提交后通知。内置 `reserved-names` 验证策略拒绝，`activity` 验证并发安全的提交后统计。插件只能拒绝操作，不能改写核心参数来绕过规则；回调不接收凭据、HTTP 请求或数据库句柄，应用 context 值也会过滤。Before 拒绝回滚事务，异常 fail closed；After 失败不改变已提交结果。当前 API 不提供后台任务/资源关闭生命周期、热加载、任意写接口或可靠消息投递。

### 插件验收（2026-10-18）

- Go 构建、vet、测试及 gofmt 检查通过。新增测试覆盖 Hook 拒绝/异常/panic、通知失败后继续、context 凭据隔离、关闭状态、非法注册/路由/选项、查询 JSON 编码失败、保留名以及 100 次并发通知计数。前端类型检查、四项测试、生产构建通过；没有新增依赖，既有延迟加载 3D 包约 517 KB 的提醒保留。
- 功能：插件策略、事务、查询权限。实际访问浏览器 `http://127.0.0.1:55607/__plugins`（PostgreSQL）和 `http://127.0.0.1:55608/__plugins`（MySQL）；两库新建隔离数据库，各实际应用 34 个迁移。操作：保留名注册/新增/改名、正常注册/登录/角色/签到/上传、匿名及普通用户访问、所有者读取统计和错误方法。看到：两库各 17+13 项通过；拒绝返回 403 `PluginDenied` 且不扣款、不占邮箱；失败操作不进入提交计数，所有者可查询，普通用户 403，写方法 405，未知路径 404。
- 功能：部署关闭与前端。实际访问 `http://127.0.0.1:55617/__disabled`，五项通过：空部署配置全部关闭、插件路由 404、旧账号仍可登录、原保留名可注册及登录。正式页面 `http://127.0.0.1:55605/web/#/admin/site` 实际读取插件清单和 `/counts`；角色页输入 Owner 显示中文插件拒绝提示且会话保留。最终查询编码调整后重新构建，在正式页面读取到合法 JSON，并确认进程重启后统计归零。
- 实际查看 1280×720、375×812 深浅主题；375 视口文档宽 360（含滚动条差异），无横向溢出，手机宽度读取按钮可操作。页面控制台检查无 error/warn；预期 403 为业务拒绝，不是未捕获异常。状态码来自真实浏览器 Response.status；没有操作独立 DevTools Network 面板。
- 日志抽查：两库在完整 30 项检查及当时 UI 操作后分别有 38/24 条访问记录，无重复访问 Request ID、无 5xx，各三次策略拒绝都有安全 Hook 与 Request ID；未检出测试密码、认证头、令牌或 options。测试代理、配置、脚本、数据库与截图均在 TEMP；截图 `C:/Users/shuil/AppData/Local/Temp/ygg-plugins-proof.png`。未覆盖第三方自定义插件、生产部署、进程崩溃投递、真实 Minecraft/OIDC 平台；没有迁移用户原有数据库。
