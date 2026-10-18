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

角色 UUID 使用离线 v3 算法并落库；改名时重新计算 UUID，因此离线身份会随名称变化。角色名在两库均不区分大小写地唯一。注销账号保留邮箱与账号 UUID，不能通过再次注册恢复；所有者不允许从 Web 自行注销或被管理员降权/封禁。

首次所有者需先注册，再由拥有部署终端权限的人显式执行：

```powershell
go run ./cmd/server -promote-owner 'owner@example.com'
```

管理员只能管理普通账号；所有者可授予/撤销管理员和封禁用户，不能通过这些接口修改其他所有者。权限或封禁变化立即撤销目标账号的全部令牌。

## 材质与衣柜

| 方法 | 路径 | 请求 / 行为 |
| --- | --- | --- |
| GET | `/api/textures?offset=0` | 公开皮肤库，每页 50 |
| GET | `/api/textures/mine?offset=0` | 我的上传，含私有条目 |
| POST | `/api/textures` | multipart：name、type（0 steve / 1 alex / 2 cape）、public（0/1）、file |
| GET | `/api/textures/{uuid}` | 公开详情；私有条目须携带上传者的 Web 凭证 |
| GET | `/api/textures/{uuid}/file` | Web 鉴权原图读取，允许上传者读取未穿戴私有材质 |
| PUT | `/api/textures/{uuid}` | `{name,type,public}`；仅上传者，类型变更重新校验尺寸 |
| DELETE | `/api/textures/{uuid}` | 仅上传者；清理角色和衣柜引用，最后引用删除后移除文件 |
| GET | `/api/closet?offset=0` | 衣柜及自定义 itemName |
| PUT / DELETE | `/api/closet/{uuid}` | `{name}` 收藏或改备注 / 移除；uuid 是材质 UUID |
| GET | `/textures/{hash}` | image/png，ETag；仅分发公开或已穿戴且所属账号正常的材质 |

文件必须声明 image/png，实际字节也必须是 PNG，输入最多 1 MiB。解码前检查宽、高、比例与 1600 万像素上限，宽高最多 8192；steve 支持 64×32 与 64×64 的整数倍，alex 仅正方形，cape 支持 64×32 的整数倍，以及经透明补边的 22×17 整数倍。服务端重编码后计算 SHA-256，文件名由 hash 决定，不使用客户端路径。

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

两库各有 15 个前向迁移，包括 accounts、textures、players、closet、tokens；整数枚举字典在 AGENTS.md。迁移使用校验和、会话锁和 dirty 标记，拒绝篡改、遗漏或失败历史。014 增加 clientToken 加密列，015 撤销无法恢复 clientToken 的早期令牌。已执行 SQL 不可修改，后续变更追加迁移。

```powershell
go build ./...
go vet ./...
go test ./...
gofmt -l internal cmd migrations
```

测试只使用标准库 testing，不连接外部网络或生产数据库。浏览器与双数据库集成验收记录见 HANDOVER.md。真实 Minecraft/authlib-injector 客户端联调未覆盖。

React 基底仍通过 `cd web; npm ci; npm run dev` 开发，`npm run build` 生成可由 YGG_WEB_DIR 托管的产物。API 根目录不作为 UI 页面。没有增加 BS 的 PHP 前端兼容层、积分、邮件发送、密码找回、OAuth/OIDC 或旧库迁移。