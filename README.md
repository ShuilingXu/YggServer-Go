# YggServer-Go

Go + React 的 Minecraft Yggdrasil 登录服务与皮肤站，PostgreSQL 为主，兼容 MySQL。Redis 用于原子限流和短期进服凭据；账号与令牌的撤销状态保存在 SQL。Yggdrasil 是程序内置能力，不需要插件。React 保留原有空白基底，本轮没有页面或视觉设计。

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

React 基底仍通过 `cd web; npm ci; npm run dev` 开发，`npm run build` 生成可由 YGG_WEB_DIR 托管的产物。API 根目录不作为 UI 页面。没有增加 BS 的 PHP 插件/前端兼容层、积分、OAuth/OIDC 或旧库迁移。邮箱恢复仅提供必要功能表单，React 页面仍未设计。
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
