# HANDOVER

只记录当前状态、下一步与待决策；工程规则见 AGENTS.md。

## 当前状态

- 已按用户更正采用 **Go + React**，PostgreSQL 为主并兼容 MySQL，不使用 Rust。本轮仅完成基底，没有 UI 或业务流程。
- Go：标准库 HTTP；集中配置校验；RSA 4096 私钥显式生成/加载与公钥发布；元数据 `/`；`/healthz`；检查迁移历史的 `/readyz`；JSON 错误；请求 ID、路由模板、IP HMAC 脱敏日志；优雅关闭。
- 数据库：pgx + go-sql-driver/mysql，连接池、超时、UTC；accounts/textures/players/closet 的两套 SQL 迁移；唯一约束、外键和整数枚举；显式 `-migrate`、会话锁、校验和与 dirty 失败保护。没有运行时隐式迁移，没有登录/令牌/会话写入。
- React：React 19.3.1 + Vite 8.3.4 + TypeScript 7.0.2；锁文件、空根节点、存活检查调用、开发代理、`/web/` 生产构建托管。没有样式、页面、路由或 UI 组件库。
- 工程：`.gitignore`、LF `.gitattributes`、Go 模块与 npm 锁文件、CI 配置、README 启动与配置说明已齐备。没有空的 auth/session/skin 包。
- 工作区尚未提交或推送，未新建/切换分支。AGENTS.md、HANDOVER.md、blessing-skin-report.md 在本任务开始前就是未跟踪文件，未清理这些用户文件。

## 验证状态

- Go 1.27.2：`gofmt -l` 干净，`go build ./...`、`go vet ./...`、`go test ./...` 均通过。
- 前端：`npm ci` 干净安装与 `npm run build` 均通过，TypeScript 检查通过。Windows 原生依赖文件锁问题已通过停止本任务的 Vite 进程解决。
- TEMP 临时实例：PostgreSQL 18.4、MySQL 8.4.11，均实际执行 10 个迁移、重复执行及并发执行；整数枚举回读、唯一约束、CHECK、外键拒绝均通过；测试数据在事务内回滚。
- 两库的 dirty 标记、篡改校验和均令迁移命令拒绝执行；恢复正确历史后可重新运行。HTTP 启动不自动修复历史。
- 实际 Ctrl+C 后看到 `server stopped` 日志；这是优雅关闭路径的验证，SIGTERM 未另测。

### 浏览器验收记录（2026-10-17）

- 功能：元数据、数据库就绪、React 基底与开发代理。
- 访问：PostgreSQL 服务 `http://127.0.0.1:58463/`、`/readyz`、`/web/`；MySQL 服务 `http://127.0.0.1:58462/`、`/readyz`；Vite `http://127.0.0.1:54615/web/`（均为实际浏览器访问）。
- 操作：两库正常迁移后访问；设置 dirty 后再次访问并运行迁移；恢复历史后访问；测试错误方法、未知路径和 Vite `/healthz` 代理。另访问未配置数据库的 `http://127.0.0.1:62128/readyz`。
- 看到：元数据含站点名、域名白名单、PEM 公钥与 ALI 头；正常就绪 200、dirty/未配置数据库 503、错误方法 405、未知路径 404；React 生产和开发入口均为空白根节点，存活检查成功，Console 无错误。
- 状态码证据：TEMP 中的浏览器诊断页 `http://127.0.0.1:51635/` 通过实际浏览器请求读取 Response.status、响应头及 JSON；不是 curl 验收。Console 通过浏览器日志接口检查。内置浏览器没有开放独立 DevTools Network 面板，未声称操作过该面板。
- 未覆盖：真实 Minecraft/authlib-injector 客户端、完整登录和上传流程（本轮未实现）；GitHub CI 远程执行（未推送）；SIGTERM 的单独进程信号测试。验收进程已停止，记录端口不再提供服务。TEMP 目录清理被自动审批拒绝，详见下方待清理项。

## 环境

- 当前执行 Shell 为 PowerShell 7.6.5，`rg` 已可用，旧环境记录不能代替自检。
- Go 1.27.2 使用 `%LOCALAPPDATA%/Programs/Go/bin/go.exe`；Git 2.53.0 使用 GitHub Desktop app-3.6.7 自带程序。
- Node 24.21.0 / npm 11.19.0；未更改用户 npm registry 或 Go 代理设置。
- 数据库仅为 TEMP 内的独立临时实例，没有安装系统服务或改动系统数据库。

## 下一步

1. 按 README 使用本地持久私钥与自建数据库启动；运行显式迁移，再启动 HTTP 服务。
2. 完成账号/角色的 service/store、密码哈希、UUID 及随机令牌哈希存储，再接 authenticate/refresh/validate/invalidate/signout。
3. 随后实现 join/hasJoined、profile SHA1withRSA 属性签名和材质上传/分发，复用现有配置、日志、错误响应和迁移入口。
4. UI 在后续任务中实现；当前不需要添加页面或更换前端栈。

## 待业务决策

- 公网域名、部署端口、生产 TLS 与数据库凭证由部署环境提供，基底均已参数化。
- 后续业务仍需确认 UUID v3/v4 策略、是否多角色、是否迁移 BlessingSkin 旧库，以及私有材质的访问策略。不得直接复制调研中的 client_token 明文落库、字符串枚举或会话表设计。

## 待清理项

本轮验收进程已停止。用户已明确授权永久删除本轮生成的临时目录，但获授权后的删除命令仍被执行策略拒绝（仅返回 blocked by policy，无详细原因）。目录为 C:/Users/shuil/AppData/Local/Temp/yggserver-scaffold-qa，包含测试数据库、测试密钥、下载包和一次性脚本；这些文件均在仓库外，未提交或复制到仓库。不再要求用户重复授权；需要由用户手动删除该目录，或等待执行策略解除后再清理。
