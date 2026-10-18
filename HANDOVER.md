# HANDOVER

当前状态、下一步与待决策；工程规则见 AGENTS.md。

## 当前状态

- 已完成 Redis、账号/角色、皮肤/衣柜及经典 Yggdrasil 核心后端。Yggdrasil 原生注册在程序内，不依赖 PHP、插件、Passport 或 JWT。React 文件没有改动，仍为空白基底。
- Redis：go-redis，redis/rediss URL、ACL/TLS 配置、命名空间、连接池、明确超时；原子限流和 60 秒一次性进服凭据。已配置 Redis 纳入 readyz；运行时故障拒绝凭证操作，恢复后自动重连。原始客户端诊断由 main 禁用，仅保留外层脱敏日志。
- 账号：bcrypt 注册，注册事务创建初始角色；邮箱或角色名登录，Web Bearer 令牌；单会话退出、改密、改邮箱、软注销；角色增删改；普通/管理员/所有者权限；用户列表与封禁/解禁。HTTP 权限变化和 CLI 所有者授权均在事务内撤销目标全部令牌。所有者必须从部署终端显式授予。
- 令牌：随机 accessToken，只存 SHA-256；Web/游戏用途隔离；24 小时有效、游戏 7 天刷新窗口；原子旋转、并发仅一个刷新成功；每账号 20 个令牌，淘汰最旧。clientToken 保存哈希和 AES-GCM 加密副本，密钥由对应 accessToken 派生；用户已批准此协议兼容例外，登记在 AGENTS.md。
- 皮肤：公开库、个人上传、私有详情/原图、改名/模型/隐私、删除、衣柜与备注、角色皮肤/披风穿戴。PNG 字节/MIME/输入大小/宽高/比例/像素校验、重编码与 SHA-256 文件名；22×17 披风补边。共享文件发布和最后引用删除使用数据库会话锁；SQL 写入使用事务与外键。公开转私有会清除其他用户的衣柜和穿戴引用。
- 原生协议：authenticate/refresh/validate/invalidate/signout、join/hasJoined、profile、批量按名称查询、标准材质 PUT/DELETE 与 /minecraft/profile/skins 入口。默认 unsigned=true，指定 false 才签名；签名为 SHA1withRSA，根元数据发布 RSA 公钥；客户端令牌校验、IP 匹配、令牌撤销和一次性消费均已实现。
- 两库各有 15 个前向迁移。新增 tokens、用途字典、角色名大小写唯一索引、clientToken 加密列；015 撤销不具备加密副本的早期令牌。既有 001–010 未修改。README 包含配置、接口、权限和部署边界。
- 改动留在工作区，没有提交、推送或切换分支。

## 验证状态（2026-10-17）

- Go 1.27.2：go build ./...、go vet ./...、go test ./... 均通过；gofmt -l 干净，git diff --check 通过。新增测试覆盖令牌过期/用途/封禁、clientToken 加密与篡改拒绝、离线 UUID、PNG 格式和尺寸拒绝及披风补边。
- PostgreSQL 与 MySQL 均实际应用 15 个迁移。两库各完成 33 项核心、31 项管理、34 项材质和 14 项生命周期浏览器检查；未以 curl 替代。
- SQL 与文件核对：两库迁移无 dirty，令牌仅哈希与加密列、密码为 bcrypt；缺失材质文件和无引用文件均为 0。CLI 所有者授权前各有一个旧令牌，授权后均为 0；MySQL 重复授权成功。
- 访问日志检查：未发现凭证字段、请求体、原始 query 或原始客户端 IP；最终 Redis 故障测试 stderr 为空，启动失败仅一条脱敏 JSON 错误。
- React 未改动，本轮未重跑 npm 构建。没有声称验证新的 UI。

### 浏览器验收记录

- 功能：注册、Web/游戏登录退出、权限、改密、改邮箱、注销、角色生命周期、皮肤/衣柜、Yggdrasil 与 Redis 故障恢复。
- 访问：PostgreSQL 实际后端 http://127.0.0.1:50030/ 与 /readyz；核心和管理诊断页 http://127.0.0.1:58005/__qa、/__qa-admin；最终材质和生命周期页 http://127.0.0.1:51056/__qa-skin、/__qa-lifecycle。MySQL 实际后端 http://127.0.0.1:51052，诊断页 http://127.0.0.1:51065/__qa、/__qa-admin、/__qa-skin、/__qa-lifecycle。以上均为实际浏览器访问；诊断代理只在 TEMP，不是产品 UI。
- 操作：重复注册、错误/正确密码、管理员权限和封禁、改密/邮箱/注销；多角色选择与并发刷新；PNG 上传、私有读取、跨用户修改拒绝、衣柜备注、穿戴、隐私变化、共享文件删除、披风补边；浏览器使用公钥验签；进服 IP 不匹配、一次性消费、自然等待 60 秒过期；角色改名/删除和未绑定令牌拒绝进服；保持应用运行，单独停止和恢复 Redis。
- 看到：注册 201、重复 409、错误凭证/越权 403、原生上传无凭证 401、非法图片/模型 400；正常退出/穿戴/进服 204；刷新后旧令牌失效且并发仅一次成功；省略 clientToken 可刷新并回显原值；浏览器确认 RSA/SHA-1 签名有效；私有未穿戴原图匿名 404，上传者鉴权可读；共享文件保留至最后引用删除。Redis 停机时 readyz=503、healthz=200、登录=503；恢复后两种后端均无需重启应用即可 readyz=200，错误凭证登录回到 403。
- 浏览器日志接口未记录 Console 错误。状态码通过诊断页中的实际浏览器 Response.status 检查；内置浏览器未开放独立 DevTools Network 面板，未声称操作过该面板。
- 验收脚本曾因集中请求触发预期 429，改用独立限流窗口后通过，未调整限制或断言；重启顺序曾导致 Redis 未就绪、应用拒绝启动及代理 502，已先确认 Redis 就绪后重跑并通过故障恢复。相关环境坑已记录在 AGENTS.md。
- 未覆盖：真实 Minecraft/authlib-injector 启动器与服务端联调；生产 TLS/Redis ACL/TLS；GitHub 远程 CI。当前任务未重新验证 Ctrl+C/SIGTERM。
- 验收进程均已停止，上述端口已不再提供服务。保留的真实截图：C:/Users/shuil/AppData/Local/Temp/yggserver-features-skin-qa.png。

## 下一步与待决策

1. 在自建持久数据库、Redis 和受限私钥目录中配置部署，显式迁移并授予所有者，进行真实启动器与 Minecraft 服务端联调。
2. 部署前确定 TLS 与可信代理/真实客户端 IP 方案。当前只信任 RemoteAddr；经过反向代理时限流共享代理地址，hasJoined 的可选 ip 不能直接匹配真实客户端地址。
3. 当前角色使用离线 UUID v3，改名会改变 UUID 和离线世界身份；若要求改名保留身份，需要另行确定随机 UUID 策略。当前软注销保留账号邮箱和 UUID。
4. 私有控制条目与未穿戴原图；穿戴后须允许游戏按 hash 下载，不能当成强保密文件。进程崩溃或数据库不可用的极端文件/事务窗口可能留下不可达文件，后续离线核对须保留数据库引用保护。
5. 邮件验证/找回密码、旧 BS 数据迁移、积分、举报、头像/预览渲染和 OAuth/OIDC 尚未实现，不属于当前经典认证与皮肤管理核心；如需要，再单独确定范围。UI 设计按用户要求延后。

## TEMP 清理状态

- 当前任务的应用、代理、Redis、PostgreSQL 和 MySQL 临时进程均已停止。
- 自动审批两次拒绝删除 C:/Users/shuil/AppData/Local/Temp/yggserver-features-qa，包括改成已核实路径的字面量 Remove-Item 后仍拒绝；仅返回 blocked by policy，无详细原因。目录仍在 TEMP，含本轮下载、测试程序、日志和一次性诊断脚本，未进入仓库。需要手动删除，不再重复请求已具备的清理授权。
- 任务开始前已存在的 C:/Users/shuil/AppData/Local/Temp/yggserver-scaffold-qa 被用于临时双数据库与测试依赖，本轮未删除该原有目录；其中数据库已停止。原交接记录中该目录也属于待清理项。