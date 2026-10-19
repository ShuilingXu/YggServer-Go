# Go 插件开发入口（API 1）

插件是随服务编译的可信 Go 代码，开发包为 `github.com/ShuilingXu/YggServer-Go/plugin`。不使用系统 `.so`，可在当前 Windows、Linux、macOS 构建方式中复用。它不兼容 BS 的 PHP 插件，不提供代码沙箱；安装插件意味着部署方已经审核其源码。核心鉴权、站点限制、数据库约束和协议签名仍由服务执行，插件不能通过 Hook 返回成功绕过这些规则。

## 配置与注册

`YGG_PLUGINS_FILE` 指向仓库外的 UTF-8 JSON 文件，最大 64 KiB；不配置时全部关闭。每个键是已编译的插件 ID，`enabled` 只接受整数 0/1。修改后重启服务。未知 ID、未知配置字段、非法选项、重复注册、未知 Hook 或不匹配的 API 版本均拒绝启动；错误不回显配置内容。

```json
{
  "reserved-names": {"enabled": 1, "options": {"names": ["Admin", "Owner", "System"]}},
  "activity": {"enabled": 1, "options": {}}
}
```

两个内置实现位于 `plugins/builtin.go`，可直接作为可运行开发范例：

- `reserved-names`：完整角色名、不区分大小写精确匹配，最多 256 个，复用核心角色名校验。覆盖密码注册、OIDC 自动注册、新增角色和改名；不追溯修改旧角色、不限制旧角色登录和删除。
- `activity`：提交后以原子计数统计所有事件。所有者查询 `/api/plugins/activity/counts`；数据仅属于当前进程，重启清零，不能作为财务流水或集群总数。

自定义插件定义一个 `Definition`，在 `cmd/server/main.go` 中调用现有 `httpserver.New` 时作为末尾参数传入；多个定义按参数顺序传入，内置定义排在前面。开发包不依赖任何 internal 类型，插件可以放在独立 Go 模块再由服务导入编译。

```go
func Definition() plugin.Definition {
    return plugin.Definition{
        ID: "my-policy", Version: "1.0.0", APIVersion: plugin.APIVersion,
        Description: "检查角色名策略",
        Actions: []plugin.Action{plugin.AccountCreate, plugin.PlayerSave},
        Build: func(raw json.RawMessage) (plugin.Instance, error) {
            var options struct { Blocked string `json:"blocked"` }
            if err := plugin.DecodeOptions(raw, &options); err != nil {
                return plugin.Instance{}, err
            }
            return plugin.Instance{Hooks: plugin.Hooks{
                Before: func(ctx context.Context, event plugin.Event) error {
                    if strings.EqualFold(event.Name, options.Blocked) {
                        return plugin.ErrDenied
                    }
                    return nil
                },
            }}, nil
        },
    }
}

// 在现有装配调用末尾追加，不另起 HTTP 服务：
// httpserver.New(c, publicKey, key, db, cache, logger, mypolicy.Definition())
```

示例需要标准库 `context`、`encoding/json`、`strings` 与开发包 `plugin`。部署文件随后可配置 `"my-policy":{"enabled":1,"options":{"blocked":"Reserved"}}`。`Build` 在启动时同步调用一次，包括已关闭插件，以便尽早发现无效配置。它应仅构造本地不可变配置或并发安全的内存状态，不在此启动 goroutine、打开长期资源或执行网络请求；当前 API 没有后台任务及资源关闭生命周期。

## Hook 契约

| Action 常量 / 标识 | Before 时点 | After 时点与主要字段 |
| --- | --- | --- |
| `AccountCreate` / `account.create` | 公共注册事务内，账号写入前；也覆盖 OIDC 新账号 | 完整账号/角色/积分及外部绑定事务提交后；AccountID、AccountUUID、Name、At。初始角色不另发 PlayerSave |
| `SessionIssue` / `session.issue` | 账号锁内，状态及密码快照校验后、旧令牌撤销和新令牌写入前 | 会话事务提交后；AccountID、AccountUUID、PlayerUUID、TokenKind、At。覆盖 Web/游戏密码登录、OIDC 登录、游戏刷新 |
| `PlayerSave` / `player.save` | 账号锁内，新增或改名事务写入前 | 事务提交后；AccountID、AccountUUID、Name、PlayerUUID、At。新角色 Before 的 UUID 为空，改名 Before 是旧 UUID；After 为最终 UUID |
| `PlayerDelete` / `player.delete` | 账号锁内，删除事务写入前 | 事务提交后；AccountID、AccountUUID、PlayerUUID、At，Name 为空 |
| `TextureUpload` / `texture.upload` | PNG 真实解码、清洗和重编码后，文件发布前；Web 与游戏共用 | 元数据/积分/可选穿戴事务提交后；AccountID、TextureUUID、Name、TextureKind、Public、SizeBytes、可选 PlayerUUID、At |
| `CheckIn` / `account.check_in` | 账号锁内，站点开启和签到冷却校验后，流水写入前 | 签到流水提交后；AccountID、AccountUUID、At |

所有事件为值副本。时间为 UTC，类型值沿用 AGENTS.md 的整数字典。结构未承诺的字段为零值；插件不能取得数据库事务、密码、令牌、邮箱、Cookie、原始 HTTP 请求或图片字节。回调 context 保留取消/截止时间，但过滤应用上下文值，仅保留服务生成的 Request ID；`Actor` 也只包含最小账号标识。

回调按定义注册顺序同步执行，不并行调度、不持有注册表全局锁。Before 第一次返回 `ErrDenied` 即停止，原事务回滚，原生 API 返回 403 `PluginDenied`（Yggdrasil 登录继续保持原协议错误形态）。未知错误或 panic 转成 `ErrFailed`，返回 503；不回显插件错误/栈。

After 只在提交成功后调用；插件之间独立，失败或 panic 只记一次脱敏错误并继续下一个，不撤销业务、不自动重试。请求取消后仍尝试通知，共用一秒的协作式截止时间；回调必须检查 context，Go 无法强制终止不合作的同步代码。它不是持久消息队列，崩溃可能丢通知，不提供投递恰好一次保证。需要可靠副作用时，应另行设计事务内 outbox，不把网络发送放到现有 Hook。

Before 必须快速、无外部副作用，不访问网络或重入业务接口，尤其不能在账号锁内再次取账号锁。After 同样不可重入业务链；材质上传的文件锁此时可能尚未释放。插件回调可能被多个 HTTP 请求并发调用：配置只读，计数使用 atomic，其他可变状态自行同步；禁止无退出机制的后台任务。内置插件没有新增依赖或后台任务。

## 开发查询入口

`Instance.Endpoints` 可声明单段路径（如 `/counts`）及 `func(context.Context, plugin.Actor) (any,error)`。服务注册为 `/api/plugins/{ID}{Path}`，统一使用现有 Web 鉴权及站点所有者权限，只接受 GET/HEAD，返回 200 JSON；其他方法 405，未知路径/关闭插件 404，匿名或普通账号 403。插件不能覆盖核心路径或自行声明更弱鉴权。接口必须只读，返回值必须可被 JSON 编码，不得含凭据。错误或 panic 返回统一 503。

`GET /api/admin/plugins` 返回 ID、版本、API 版本、开启状态、说明、Hook 与查询路径，不包含部署文件路径和 options。正式前端站点配置页展示此清单，并能在相同鉴权下读取声明的查询结果。没有可执行脚本注入、动态菜单、任意写接口、数据库迁移或热加载入口；需要这些能力时应明确扩展契约，而不是让插件绕过核心服务。

插件装配记 INFO；规则拒绝记 WARN；未知失败、panic 和通知失败记 ERROR。只记录固定 plugin_id、Hook、结果和 Request ID，不输出回调错误原文、配置或业务载荷。HTTP 访问继续沿用每请求恰好一条脱敏日志。
