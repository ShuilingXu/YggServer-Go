# Blessing Skin 6.x 调研报告

> 调研对象：Blessing Skin Server（PHP/Laravel 皮肤站）及其 Yggdrasil 插件  
> 调研时间：2026-10-17  
> 调研方式：GitHub 源码静态阅读 + authlib-injector 规范对照；**未运行 PHP、未部署、未做浏览器/Minecraft 客户端联调**  
> 用途：YggServer-Go 的皮肤站与 Yggdrasil API 设计参考

---

## 目录

1. [摘要](#1-摘要)
2. [调研对象与版本](#2-调研对象与版本)
3. [代码结构与运行方式](#3-代码结构与运行方式)
4. [数据模型](#4-数据模型)
5. [皮肤站本体接口](#5-皮肤站本体接口)
6. [材质上传、存储与渲染实现](#6-材质上传存储与渲染实现)
7. [Yggdrasil 插件实现](#7-yggdrasil-插件实现)
8. [账号、权限与安全](#8-账号权限与安全)
9. [对 YggServer-Go 的落地建议](#9-对-yggserver-go-的落地建议)
10. [验证边界与未覆盖项](#10-验证边界与未覆盖项)
11. [附录](#11-附录)

---

## 1. 摘要

### 1.1 最重要的结论

1. **Blessing Skin 本体不是 Yggdrasil 服务端**。它只是一个皮肤站（上传、管理、展示皮肤/披风、角色绑定、衣柜、皮肤库）。Yggdrasil 由插件提供：
   - BS 3/4/5：旧插件 [`yggdrasil-api`](https://github.com/bs-community/yggdrasil-api)，最后 v4.1.4，2020 年归档；
   - BS 6：新插件 [`yggdrasil-connect`](https://github.com/bs-community/blessing-skin-plugins/tree/master/plugins/yggdrasil-connect)，v6.2.0，2025 年维护，同时实现经典 AuthServer 和 Yggdrasil Connect/OIDC。
2. **BS 6 + yggdrasil-connect 是最值得参考的组合**。旧插件只能作为历史行为参考，不应作为 BS 6 的实现依据。
3. **Yggdrasil 协议层的关键事实**：
   - profile 的 `unsigned` 参数缺省为 `true`，即默认不返回签名；只有 `unsigned=false` 才返回 `signature`。
   - 属性签名算法是 **SHA1withRSA**，不是 SHA256。PHP `openssl_sign()` 的默认算法就是 SHA1，所以插件代码中没有显式传算法。
   - 材质 URL 的文件名就是材质 hash；响应必须是 `Content-Type: image/png`，hash 建议使用 SHA-256。
   - UUID 默认使用离线模式 v3：`md5("OfflinePlayer:" + name)`，再设置 version/variant 位。
4. **皮肤站本体的 HTTP 接口是“两套风格”**：
   - 写操作统一返回 `{code, message, data?}`；
   - 读操作直接返回模型/分页器，没有 `code/message`；
   - 业务错误通常仍是 HTTP 200，校验错误 422，权限错误 403。
5. **Blessing Skin 的数据库设计不能照抄**：
   - `users.email`、`players.name`、`textures.hash`、`options.option_name` 都没有唯一索引；
   - `user_closet` 没有主键、没有外键；
   - 全库几乎无外键；
   - `textures.type` 是字符串 `steve/alex/cape`；
   - 上传流程没有事务。
6. **对 Go 项目的建议**：
   - 协议、字段、状态码、材质校验、UUID 算法、签名算法可以按规范复刻；
   - 数据库约束、类型枚举、令牌存储、事务、缓存要按 YggServer-Go 的 AGENTS.md 重做；
   - 不要复刻 Laravel Cache 存令牌、Passport JWT、PHP 插件系统、Blade/Twig。

### 1.2 一句话版本

> Blessing Skin 本体是“皮肤站 + 角色/衣柜 + 材质管线”，Yggdrasil 是外挂插件；Go 重写应把 Yggdrasil 协议作为硬契约，把 BS 的皮肤站实现当作业务参考，而不是架构模板。

---

## 2. 调研对象与版本

| 对象 | 地址 | 版本/状态 | 说明 |
|---|---|---|---|
| Blessing Skin Server | [bs-community/blessing-skin-server](https://github.com/bs-community/blessing-skin-server) | `dev` 分支；`config/app.php` 仍写 `6.0.2` | 2022 年发布 6.0.2，但 dev 持续更新到 2025 年（如 2025 年新增 `max_texture_width` 迁移） |
| 旧 Yggdrasil 插件 | [bs-community/yggdrasil-api](https://github.com/bs-community/yggdrasil-api) | v4.1.4，已归档 | 只声明支持 BS 5；使用 Laravel Cache 存 token |
| 新 Yggdrasil 插件 | [yggdrasil-connect](https://github.com/bs-community/blessing-skin-plugins/tree/master/plugins/yggdrasil-connect) | v6.2.0 | 要求 `blessing-skin-server: ^6`、`php: ^8.1`；使用 Passport PAT/JWT |
| Janus | [bs-community/janus](https://github.com/bs-community/janus) | 2025 | 独立 NestJS OIDC 服务，与 BS 共用 MySQL；配合 yggdrasil-connect 实现 Yggdrasil Connect |
| 协议规范 | [authlib-injector Yggdrasil 服务端技术规范](https://yushijinhun.github.io/authlib-injector/zh/Yggdrasil-%E6%9C%8D%E5%8A%A1%E7%AB%AF%E6%8A%80%E6%9C%AF%E8%A7%84%E8%8C%83.html) | — | 本文协议部分的基准 |

### 2.1 技术栈

| 项 | 事实 |
|---|---|
| 语言/框架 | PHP `^8.1` + Laravel 10 |
| Web 认证 | Session + CSRF（`web` 中间件组） |
| OAuth2 | Laravel Passport 11（`/oauth/*`、`auth:oauth`） |
| 前端 | Twig/Blade 页面 + React/TypeScript 组件（webpack） |
| 数据库 | MySQL/MariaDB、SQLite、PostgreSQL（安装向导三选一） |
| 材质渲染 | `blessing/texture-renderer`，纯 PHP + GD 软渲染 |
| 图片处理 | Intervention Image + Imagick（上传清洗） |
| 许可证 | MIT |

核心依赖见 [composer.json](https://github.com/bs-community/blessing-skin-server/blob/dev/composer.json)。

---

## 3. 代码结构与运行方式

### 3.1 目录职责

| 路径 | 作用 |
|---|---|
| `routes/web.php` | 页面 + AJAX 写接口，走 `web` 中间件（session、CSRF） |
| `routes/static.php` | 材质、头像、预览、CustomSkinAPI，**不加载 session** |
| `routes/api.php` | OAuth2 资源 API，`auth:oauth` + scope，前缀 `/api`，`throttle:60,1` |
| `app/Http/Controllers/SkinlibController.php` | 上传、皮肤库列表、详情、改名、改类型、隐私、删除 |
| `app/Http/Controllers/TextureController.php` | 原图、预览、头像、CustomSkinAPI JSON |
| `app/Http/Controllers/PlayerController.php` | 角色增删改、设置/清空材质 |
| `app/Http/Controllers/ClosetController.php` | 衣柜（收藏、改名、移除） |
| `app/Models/Texture.php` / `Player.php` / `User.php` | 核心模型；`model` 是访问器，不落库 |
| `app/Services/Option.php` | options 表 KV 读取，字符串布尔转换 |
| `app/Services/PluginManager.php` / `Hook.php` | PHP 插件系统 |
| `database/migrations/` | 25 个迁移，最终 schema 以迁移链为准 |
| `config/options.php` | 49 个默认配置项，全部字符串 |
| `config/filesystems.php` | `textures` disk；默认 `storage/textures` |
| `resources/assets/src/views/` | React 页面：皮肤库、上传、角色、衣柜、后台 |
| `plugins/yggdrasil-connect/` | Yggdrasil 经典 AuthServer + OIDC + 材质上传 |

### 3.2 请求分层

```text
Web 页面/AJAX:
routes/web.php
  → web 中间件（EncryptCookies / StartSession / VerifyCsrfToken / ...）
  → Controller
  → Model
  → DB / Storage

静态材质:
routes/static.php
  → 无 session
  → TextureController
  → Storage::disk('textures')

OAuth2 API:
routes/api.php
  → api + throttle:60,1
  → auth:oauth
  → scope
  → Controller

Yggdrasil:
插件 Hook::addRoute
  → /api/yggdrasil/*
  → CheckContentType / CheckLoginCredentials / Throttle / CheckAccessToken
  → 插件 Controller
```

### 3.3 两套前端渲染

- 页面主体是 Twig（`resources/views/*.twig`），不是 Blade；
- 交互部分用 React 挂载到固定 DOM：
  - `#file-input`：上传页；
  - `#previewer`：3D 预览；
  - `#side`：皮肤详情侧栏；
  - `#players-list`：角色管理；
  - `#closet-list`：衣柜。
- 页面通过 `window.blessing` 注入 `base_url`、CSRF token、`extra` 数据。

> Go 重写如果自带前端，不需要复刻 Twig/React；如果要复用 BS 的前端 bundle，则必须复刻路由、DOM id、`blessing.extra` 字段和 `{code,message,data}` 响应。

---

## 4. 数据模型

### 4.1 ER 关系

```text
users(uid) 1──N players(pid)              players.uid
users(uid) 1──N textures(tid)             textures.uploader
users(uid) N──M textures(tid)             user_closet(user_uid, texture_tid, item_name)
players(pid) N──1 textures(tid)           players.tid_skin  → skin
players(pid) N──1 textures(tid)           players.tid_cape  → cape
users(uid) 1──1 textures(tid)             users.avatar = tid（0=无，逻辑引用）
reports(id) N──1 textures / users         tid / uploader / reporter
uuid(插件)                                 pid / name / uuid
```

### 4.2 核心表

#### users

| 列 | 类型（MySQL 推断） | 说明 |
|---|---|---|
| `uid` | INT UNSIGNED AI PK | 自增主键 |
| `email` | VARCHAR(100) | **无唯一索引** |
| `nickname` | VARCHAR(50) | 默认空串 |
| `locale` | VARCHAR(255) NULL | 用户语言 |
| `score` | INT | **无默认值**，注册必须显式写入 |
| `avatar` | INT | 默认 0；逻辑引用 textures.tid |
| `password` | VARCHAR(255) | 哈希后的密码 |
| `ip` | VARCHAR(45) | IPv6 足够 |
| `is_dark_mode` | TINYINT(1) | 默认 0 |
| `permission` | INT | `-1` 封禁 / `0` 普通 / `1` 管理员 / `2` 超管 |
| `last_sign_at` | DATETIME | 签到冷却 |
| `register_at` | DATETIME | 注册时间 |
| `verified` | TINYINT(1) | 默认 0 |
| `verification_token` | VARCHAR(255) | 默认空串；**代码从未使用** |
| `remember_token` | VARCHAR(100) NULL | Laravel remember |

没有 `created_at/updated_at`；`User::$timestamps = false`。

#### players

| 列 | 类型 | 说明 |
|---|---|---|
| `pid` | INT UNSIGNED AI PK | 角色 ID |
| `uid` | INT | 所属用户 |
| `name` | VARCHAR(50) | 角色名；**无唯一索引** |
| `tid_skin` | INT | 皮肤 tid；迁移默认 `-1`，代码一律写 `0` |
| `tid_cape` | INT | 披风 tid，默认 0 |
| `last_modified` | DATETIME | 被当作 `UPDATED_AT` |

#### textures

| 列 | 类型 | 说明 |
|---|---|---|
| `tid` | INT UNSIGNED AI PK | 材质 ID |
| `name` | VARCHAR(50) | 显示名，不是文件名 |
| `type` | VARCHAR(10) | `steve` / `alex` / `cape` |
| `hash` | VARCHAR(64) | `sha256(重编码后的 PNG)`；**无唯一索引** |
| `size` | INT | 单位 KB，`ceil(bytes/1024)` |
| `uploader` | INT | 上传者 uid |
| `public` | TINYINT | 0/1；无默认值 |
| `upload_at` | DATETIME | `CREATED_AT` |
| `likes` | INT UNSIGNED | 默认 0；上传时初始化为 1 |

`model` 不是列，是访问器：

```php
public function getModelAttribute()
{
    return $this->type === 'alex' ? 'slim' : 'default';
}
```

#### user_closet

| 列 | 类型 | 说明 |
|---|---|---|
| `user_uid` | INT | 用户 |
| `texture_tid` | INT | 材质 |
| `item_name` | TEXT NULL | 用户给这个材质起的备注名，与 `textures.name` 独立 |

**无主键、无索引、无外键**，允许重复行；应用层用 `where('tid',$tid)->count()>0` 挡重复。

#### reports

| 列 | 类型 | 说明 |
|---|---|---|
| `id` | INT UNSIGNED AI PK | 举报 ID |
| `tid` | INT | 被举报材质 |
| `uploader` | INT | 冗余存储的上传者 |
| `reporter` | INT | 举报人 |
| `reason` | LONGTEXT | 理由 |
| `status` | INT | 0 PENDING / 1 RESOLVED / 2 REJECTED |
| `report_at` | DATETIME | 举报时间 |

#### options / scopes / notifications

- `options(id, option_name VARCHAR(50), option_value LONGTEXT)`：所有值都是字符串；`option_name` 无唯一索引，靠 `updateOrInsert`。
- `scopes(id, name VARCHAR(255) UNIQUE, description VARCHAR(255))`：OAuth scope 字典。
- `notifications`：Laravel 标准通知表；`notifiable_type` 存 PHP 类名。

#### uuid（Yggdrasil 插件）

旧插件：

```php
$table->increments('id');
$table->string('name');
$table->string('uuid', 255);
```

无唯一索引、无索引。

新插件：

```text
id, pid, name, uuid
pid 外键 → players.pid，ON DELETE CASCADE
pid/name/uuid 唯一约束
```

### 4.3 数据库层面的负面清单

| 问题 | 事实 | Go 建议 |
|---|---|---|
| email 不唯一 | 只有应用层 `unique:users` | `UNIQUE KEY uk_users_email` |
| 角色名不唯一 | 只有应用层 `unique:players` | `UNIQUE KEY uk_players_name` |
| 材质 hash 不唯一 | 同一 hash 可有多行（不同上传者/名字），文件靠引用计数 | 保留多行语义；hash 建普通索引；或拆 `texture_files(hash PK)` |
| 衣柜无主键 | 并发可插重复 | `PRIMARY KEY(user_uid, texture_tid)` |
| 全库几乎无外键 | 删用户只删 players，textures/closet/tokens 残留 | 明确 `ON DELETE CASCADE/SET NULL`，事务内做级联 |
| 类型字段是字符串 | `textures.type`、`notifications.notifiable_type` | 内部 `TINYINT`，只在 JSON 边界映射字符串 |
| 权限与封禁混一列 | `permission = -1/0/1/2` | 拆 `state` + `permission` |
| options 无类型 | `'true'/'1024'` 全存文本 | 配置结构体/分列，不把布尔数字塞字符串 |
| 上传无事务 | 先写 DB，再写文件，再扣分，再入衣柜 | 一个事务 + 临时文件 + rename |

### 4.4 与 AGENTS.md “type 用整数”的冲突

Blessing Skin 的 `textures.type` 是字符串，且前端、皮肤库过滤、`model` 访问器都依赖它：

```php
return $this->type === 'alex' ? 'slim' : 'default';
```

Go 侧建议：

```go
type TextureType int8

const (
    TextureSteve TextureType = 0 // 旧 "steve"，model=default
    TextureAlex  TextureType = 1 // 旧 "alex"，model=slim
    TextureCape  TextureType = 2 // 旧 "cape"，model=default
)

func (t TextureType) Model() string {
    if t == TextureAlex {
        return "slim"
    }
    return "default"
}
```

DB 存 `TINYINT`；HTTP JSON 如果兼容 BS 前端，再序列化成 `"steve"|"alex"|"cape"`。这是“存储用整数、协议用字符串”，需要明确决策。

---

## 5. 皮肤站本体接口

### 5.1 统一响应

`app/helpers.php` 的 `json()`：

```php
json($message)                 => {"code":1,"message":...}
json($message, $code)          => {"code":$code,"message":...}
json($message, $code, $data)   => {"code":$code,"message":...,"data":{...}}
```

| code | 含义 | 出现场景 |
|---|---|---|
| `0` | 成功 | 所有写操作 |
| `1` | 通用失败 | 业务失败；422 也会被前端映射成 1 |
| `2` | 重复 / 特殊 | 上传 hash 重复、隐私切换冲突；签到被拒绝、邮件失败也用 2 |
| `7` | 积分不足 | 添加角色 |
| `-1` | 被封禁 | `RejectBannedUser` 中间件 |

HTTP 语义：

- 业务失败：通常 **HTTP 200**，body 里 `code != 0`；
- 校验失败：**422** `{message, errors:{field:[msg]}}`；
- 权限/封禁：**403**；
- 成功无内容：**204**。

> Go 侧如果自带前端，建议直接用正确的 HTTP 状态码 + 统一 error body；如果要复用 BS 前端，才需要复刻 200+code 这套。

### 5.2 认证/用户路由

| 方法 | 路径 | 字段/说明 |
|---|---|---|
| POST | `/auth/login` | `identification`（邮箱或角色名）、`password`、`captcha`、`keep` |
| POST | `/auth/register` | `email`、`password`、`captcha`、`player_name` 或 `nickname` |
| POST | `/auth/forgot` | `email`、`captcha`；1 小时签名链接 |
| POST | `/auth/reset/{uid}` | 签名 URL + 新密码 |
| POST | `/auth/logout` | 清 session |
| GET | `/auth/captcha` | JPEG 验证码，`Cache-Control: no-store` |
| POST | `/user/profile` | `action=nickname/password/email/delete` |
| POST | `/user/profile/avatar` | `tid`，0=清空，cape 拒绝 |
| POST | `/user/sign` | 签到积分 |
| POST | `/user/email-verification` | 发送验证邮件，60 秒节流 |
| PUT | `/user/dark-mode` | 204 |

### 5.3 角色/衣柜路由

| 方法 | 路径 | 字段/说明 |
|---|---|---|
| GET | `/user/player/list` | 裸 JSON 数组 |
| POST | `/user/player` | `{name}`；扣 `score_per_player`；不足返回 code 7 |
| PUT | `/user/player/{pid}/name` | 改名 |
| DELETE | `/user/player/{pid}` | 删除，可退分 |
| PUT | `/user/player/{pid}/textures` | `{skin?:tid, cape?:tid}`；tid 必须在自己的衣柜 |
| DELETE | `/user/player/{pid}/textures?skin=true&cape=true` | 清空；query 而非 body |
| GET | `/user/closet/list` | `category=skin/cape`、`q`、`page`、`perPage` |
| GET | `/user/closet/ids` | 返回 tid 数组 |
| POST | `/user/closet` | `{tid,name}`；`item_name` 为用户自定义备注 |
| PUT | `/user/closet/{tid}` | 改备注名 |
| DELETE | `/user/closet/{tid}` | 移除并 `likes--` |

### 5.4 材质路由

| 方法 | 路径 | 说明 |
|---|---|---|
| POST | `/texture` | multipart：`name`、`type`、`file`、`public` |
| GET | `/texture/{tid}` 或 `/skinlib/info/{tid}` | 完整 Texture JSON |
| PUT | `/texture/{tid}/name` | `{name}` |
| PUT | `/texture/{tid}/type` | `{type}` |
| PUT | `/texture/{tid}/privacy` | 无 body，翻转 public |
| DELETE | `/texture/{tid}` | 删除；文件按 hash 引用计数 |
| GET | `/skinlib/list` | `filter/keyword/uploader/sort/page`，每页 20 |
| GET | `/skinlib/show/{tid}` | 页面 |
| POST | `/skinlib/report` | `{tid,reason}` |

### 5.5 静态/图片路由

| 方法 | 路径 | 说明 |
|---|---|---|
| GET | `/{player}.json`、`/csl/{player}.json` | CustomSkinAPI R1，被封禁 403 |
| GET | `/textures/{hash}`、`/csl/textures/{hash}` | 原图，`image/png`，ETag + 1 年 max-age |
| GET | `/raw/{tid}` | 下载；受 `allow_downloading_texture` 控制 |
| GET | `/preview/{tid}`、`/preview/hash/{hash}` | 皮肤/披风预览，默认 WebP，`?png` 强制 PNG |
| GET | `/avatar/{tid}`、`/avatar/user/{uid}`、`/avatar/player/{name}`、`/avatar/hash/{hash}` | 2D/3D 头像，`size`、`3d`、`png` |

### 5.6 上传接口契约

`POST /texture`，`multipart/form-data`：

| 字段 | 规则 |
|---|---|
| `name` | required；可选 `texture_name_regexp` |
| `type` | `steve` / `alex` / `cape` |
| `file` | required；`mimes:png`；最大 `max_upload_file_size` KB（默认 1024） |
| `public` | required；`boolean`；前端传 `'1'`/`'0'` |

服务端额外校验：

| 校验 | 规则 |
|---|---|
| 宽度 | `width <= max_texture_width`（默认 8192） |
| 整除 | `width % 64 == 0 && height % 32 == 0` |
| 比例 | `steve`：1:1 或 2:1；`alex`：**只能 1:1**；`cape`：**只能 2:1** |
| 重编码 | 一律 PNG，去元数据，压缩 |
| hash | `sha256(重编码后的 PNG 字节)` |
| size | `ceil(重编码字节 / 1024)` KB |

成功：

```json
{"code":0,"message":"...","data":{"tid":123}}
```

重复（同 hash 且已公开，或自己上传过）：

```json
{"code":2,"message":"...","data":{"tid":123}}
```

前端 `Upload.tsx`：

- 选择文件后，若当前不是 cape，用客户端 `isAlex()` 自动判断 steve/alex；
- `public` 用 `'1'`/`'0'`；
- code 0 跳详情；code 2 弹“在皮肤库中查看”并跳 `data.tid`。

### 5.7 角色/衣柜的关键语义

- `players.name` 全局唯一（应用层校验），默认规则 `^[A-Za-z0-9_]+$`，长度 3–16。
- 添加角色扣 `score_per_player`（默认 100），不足返回 **code 7**；删除时按 `return_score` 退分。
- 设置角色材质要求该 tid 在**当前用户衣柜**里；值为 0 表示不改，不是清空。
- 清空材质用 query `?skin=true&cape=true`。
- 衣柜的 `item_name` 与 `textures.name` 独立：前者是“我给这个皮肤起的备注”，后者是皮肤本身的名称。
- 上传成功会**自动把材质加入上传者衣柜**，`item_name = name`，`likes = 1`。

---

## 6. 材质上传、存储与渲染实现

### 6.1 上传完整时序

`SkinlibController::handleUpload()`：

1. 文件有效性检查（`isValid()` 失败只记日志，不阻断）。
2. Laravel 校验 `name/file/type/public`。
3. 插件过滤点：`uploaded_texture_file`、`uploaded_texture_name`、`can_upload_texture`。
4. `getimagesize()` 取宽高；宽度上限 8192。
5. 宽必须 64 的倍数，高必须 32 的倍数。
6. 比例规则（见上）。
7. `Image::make()` → Imagick：

   ```php
   $imagick->setOption('png:compression-filter', '0');
   $imagick->setOption('png:compression-level', '9');
   $imagick->setOption('png:compression-strategy', '0');
   $imagick->setOption('png:exclude-chunk', 'all');
   $imagick->stripImage();
   $sanitized = $image->encode('png')->getEncoded();
   ```

8. `hash('sha256', $image->encoded)`，即重编码后的字节。
9. 查重：同 hash 且 `(public=1 OR uploader=me)` → code 2 + 已有 tid。
10. 计算费用：

    ```php
    $fileSize = ceil(strlen($sanitized) / 1024);
    $cost = $fileSize * ($isPublic ? score_per_storage : private_score_per_storage)
          + score_per_closet_item
          - score_award_per_texture;
    ```

11. **无事务**地：
    - 先 `Texture::save()`；
    - 再 `Storage::disk('textures')->put($hash, $sanitized)`（若不存在）；
    - 扣分；
    - `closet()->attach($tid, ['item_name' => $name])`。
12. 派发 `texture.uploaded`。

### 6.2 存储布局

| 项 | 事实 |
|---|---|
| disk | `textures` |
| root | `TEXTURES_DIR`，默认 `storage/textures` |
| 文件名 | 就是 hash，64 位十六进制，无扩展名 |
| 目录 | 完全扁平，不分片 |
| 内容 | 重编码后的 PNG，不是原始上传字节 |
| 去重 | 同 hash 只存一份文件；DB 可以有多行 |
| 删除 | 正常删除时，若该 hash 只剩一行才删文件；举报审核删除直接删文件（会误删共享文件） |

### 6.3 服务与缓存

`TextureController`：

```php
return response($disk->get($hash))
    ->withHeaders([
        'Content-Type' => 'image/png',
        'Content-Length' => $disk->size($hash),
    ])
    ->setLastModified(...);
```

- `/textures/{hash}` 不受 `allow_downloading_texture` 保护；`/raw/{tid}` 才受保护。
- 路由中间件设置 `Cache-Control: etag, public, max-age=31536000`。
- `ETag` 由 Laravel 对 body 做 md5；`Last-Modified` 用文件 mtime。
- `/avatar/user/{uid}` 这种内容会变的 URL 也带 1 年 max-age，是原项目的一个缓存问题。

### 6.4 预览/头像

| 场景 | 调用 |
|---|---|
| 皮肤预览 | `renderSkin($file, 12, $type === 'alex')`；`?height` 对皮肤无效 |
| 披风预览 | `renderCape($file, $height)`，默认 200 |
| 2D 头像 | `render2dAvatar($file, 25)`，再 `resize($size,$size)`，默认 100 |
| 3D 头像 | `render3dAvatar($file, 25)`，`?3d` 触发 |

- 输出默认 WebP；`?png` 强制 PNG；服务端不支持 WebP 时自动 PNG。
- 头像只允许 `steve`/`alex`，cape 返回 422。
- 无材质或文件缺失时使用 `resources/misc/textures/avatar2d.png` / `avatar3d.png` 兜底。
- 缓存 key：
  - `preview-t{tid}-{format}`：**不含 height**，不同尺寸会互相污染；
  - `avatar-{2d|3d}-t{tid}-s{size}-{format}`。
- TTL：`enable_preview_cache` / `enable_avatar_cache` 开启时 1 年，否则 **1 分钟**；这两个 option 默认不在 `config/options.php` 里，所以全新安装默认是 1 分钟。

### 6.5 CustomSkinAPI

`Player::toJson()`：

```json
{
  "username": "PlayerName",
  "skins": {"default": "<hash>"},
  "cape": "<hash>"
}
```

`model` 为 `default` 或 `slim`；没有皮肤时值为 `null`。`TextureController@json` 会检查用户是否被封禁，403。

> 我核对过 Laravel 10 的 `JsonResponse::setData()`：它优先判断 `Jsonable`，而 Eloquent Model 实现了 `Jsonable`，所以 `response()->json($player)` 确实会调用 `Player::toJson()`，不是返回模型属性数组。

### 6.6 现有实现的坑

| 坑 | 影响 | Go 建议 |
|---|---|---|
| 上传无事务 | 文件/DB/积分/衣柜可能半成功 | 一个事务 + 临时文件 rename |
| 只限宽不限高 | 解压炸弹面 | 同时限制 height 和总像素 |
| `type` 可随意改 | 可把 cape 改成 skin，绕过尺寸校验 | 改类型时重新校验尺寸/比例 |
| 设置角色材质不校验类型 | 可把 cape 设成 skin | `tid_skin` 必须是 steve/alex |
| 私有材质按 hash 公开可取 | hash 泄露即可读 | 保持不可猜 hash；如需更强隐私，加签名 URL |
| 预览缓存 key 不含 height | 尺寸污染 | key 加 height |
| 举报删除不查引用计数 | 误删共享文件 | 统一引用计数 |
| 上传即入衣柜、likes=1 | 删除/隐私逻辑复杂 | 明确产品语义后重做 |

---

## 7. Yggdrasil 插件实现

### 7.1 用哪个插件

| | 旧 `yggdrasil-api` | 新 `yggdrasil-connect` |
|---|---|---|
| 支持 BS | 3/4/5 | **6.x** |
| 状态 | v4.1.4，2020 归档 | v6.2.0，2025 维护 |
| 令牌 | 随机 UUID，存 Laravel Cache | Passport Personal Access Token（JWT），存 DB |
| clientToken | 校验 | **不校验/不保存** |
| UUID 表 | `id/name/uuid`，无索引 | `id/pid/name/uuid`，唯一约束 + 外键 |
| join 记录 | profile，永久 | `{profile, ip}`，120 秒 |
| hasJoined | 不校验 IP，检查后删除 | 校验 IP，**不删除** |
| 材质上传 API | 无 | 有 |
| OIDC/Yggdrasil Connect | 无 | 有（配合 Janus） |

**结论**：BS 6 参考新插件；旧插件只在需要理解历史行为时看。

### 7.2 元数据 `GET /api/yggdrasil`

```json
{
  "meta": {
    "serverName": "Example Skin",
    "implementationName": "Yggdrasil Connect for Blessing Skin by LittleSkin",
    "implementationVersion": "6.2.0",
    "links": {
      "homepage": "https://skin.example.com/",
      "register": "https://skin.example.com/auth/register"
    },
    "feature.non_email_login": true
  },
  "skinDomains": ["skin.example.com"],
  "signaturePublickey": "-----BEGIN PUBLIC KEY-----\n...\n-----END PUBLIC KEY-----\n"
}
```

要点：

- `signaturePublickey` 是 **PEM 公钥**，来自 `ygg_private_key`，要求 **RSA >= 4096**。
- `skinDomains` = `site_url` 的 host + 当前请求 host + `ygg_skin_domain` 逗号列表。
- 如果材质走 CDN，必须把 CDN 域名加进 `skinDomains`，否则客户端会报 `Textures payload has been tampered with (non-whitelisted domain)`。
- `feature.non_email_login=true` 表示允许角色名登录。
- 可选 `feature.openid_configuration_url` 指向 Janus。
- ALI 响应头：`X-Authlib-Injector-API-Location: https://skin.example.com/api/yggdrasil`。

### 7.3 端点总表

以下路径都相对 `/api/yggdrasil`：

| 方法 | 路径 | 请求 | 成功 | 主要错误 |
|---|---|---|---|---|
| GET/ANY | `/` | — | 200 metadata | 400/403 RSA 配置错误 |
| POST | `/authserver/authenticate` | `username,password,clientToken?,requestUser?,agent?` | 200 token | 400 参数；403 密码/封禁/未验证/无角色；403 限流 |
| POST | `/authserver/refresh` | `accessToken,clientToken?,selectedProfile?,requestUser?` | 200 新 token | 400 参数/未绑定/角色不匹配；403 token 无效 |
| POST | `/authserver/validate` | `accessToken,clientToken?` | **204** | 403 token 无效 |
| POST | `/authserver/invalidate` | `accessToken,clientToken?` | **204（总是）** | 无 |
| POST | `/authserver/signout` | `username,password` | **204** | 失败也 204 |
| POST | `/sessionserver/session/minecraft/join` | `accessToken,selectedProfile,serverId` | **204** | 400 参数；403 token/角色/归属 |
| GET | `/sessionserver/session/minecraft/hasJoined` | `username,serverId,ip?` | 200 profile 或 **204** | 参数错也 204 |
| GET | `/sessionserver/session/minecraft/profile/{uuid}` | `unsigned?` | 200 profile 或 204 | 204 表示不存在 |
| POST | `/api/profiles/minecraft` | `["name1","name2"]` | 200 `[{id,name}]` | 403 超过数量上限 |
| POST | `/api/minecraft/profile/lookup/bulk/byname` 等 | 同上 | 200 | 兼容别名 |
| GET | `/api/users/profiles/minecraft/{name}` 等 | — | 200 `{id,name}` 或 204 | 兼容别名 |
| PUT | `/api/user/profile/{uuid}/{skin\|cape}` | multipart `model,file` + Bearer | **204** | 400 尺寸；401 Bearer；403 角色/权限；404 角色 |
| DELETE | `/api/user/profile/{uuid}/{skin\|cape}` | Bearer | **204** | 同上 |
| GET | `/minecraftservices/publickeys` | — | 200 公钥 | — |

错误 body 统一：

```json
{"error":"ForbiddenOperationException","errorMessage":"用户名或密码错误"}
```

状态码：

- `ForbiddenOperationException` → 403
- `IllegalArgumentException` → 400
- `NotFoundException` → 404
- `UnauthorizedException` → 401

### 7.4 authenticate

请求：

```json
{
  "username": "user@example.com 或 PlayerName",
  "password": "******",
  "clientToken": "optional",
  "requestUser": true,
  "agent": {"name": "Minecraft", "version": 1}
}
```

响应：

```json
{
  "accessToken": "eyJ...",
  "clientToken": "32hex-or-client-value",
  "availableProfiles": [
    {"id": "32hex", "name": "PlayerName"}
  ],
  "selectedProfile": {"id": "32hex", "name": "PlayerName"},
  "user": {"id": "32hex", "properties": []}
}
```

规则：

- `username` 是邮箱时按 email 查；否则按角色名查。
- 只有一个角色 → 自动绑定。
- 多个角色且用角色名登录 → 自动绑定该角色，`availableProfiles` 只返回它。
- 多个角色且用邮箱登录 → 不绑定，返回全部，等客户端 refresh 选角色。
- `clientToken` 不传则生成 32 位无横线 UUID。
- `requestUser=true` 时返回 `user.id`：新插件是 `UUIDv5(NAMESPACE_DNS, uid)`，旧插件是 `UUIDv5(NAMESPACE_DNS, email)`。
- 没有角色 → 403 `你没有可用的角色...`。

### 7.5 refresh / validate / invalidate / signout

**refresh**：

- 校验 accessToken 存在、可刷新。
- 请求带 `selectedProfile` 时：
  - 原 token 已绑定且与新角色不同 → 400；
  - 新角色不存在或不属于该用户 → 403；
  - 原 token 未绑定 → 绑定到新角色。
- 不带 `selectedProfile` 时沿用原绑定；原 token 未绑定则 400。
- 颁发新 accessToken，吊销旧 token。
- 新插件**忽略请求里的 clientToken**，返回自己生成的新 clientToken；这是对规范的偏离，Go 应保存并校验旧 clientToken。
- 响应 `availableProfiles` 只含新绑定角色（插件的有意行为，非规范要求）。

**validate**：

- 规范：token 有效 → 204，否则 403。
- 新插件用 `canJoinServer()`，要求 token 已绑定有效角色；未绑定 token 会 403。比规范严格。

**invalidate**：

- 无论 token 是否有效，HTTP 都 204；有效时吊销。

**signout**：

- 校验用户名/密码；成功吊销该用户所有 token；失败也 204（新插件行为）。

### 7.6 join / hasJoined

**join**：

```json
{
  "accessToken": "...",
  "selectedProfile": "32hex",
  "serverId": "..."
}
```

- token 必须有效且 `selectedProfile` 与 token 绑定角色一致。
- 角色必须存在且属于 token 的 owner。
- 记录 `{profile, ip}` 到 `yggdrasil-server-{serverId}`，TTL **120 秒**。
- 返回 204。

**hasJoined**：

```text
GET .../hasJoined?username=PlayerName&serverId=...&ip=...
```

- 参数不合法 → 204。
- 查缓存；IP 提供时必须与 join 时一致。
- `username` 必须等于角色当前名称。
- 返回**带签名**的 profile（`serialize(false)`）。
- 新插件**不删除**缓存，120 秒内可重复查询；旧插件会删除。

> Go 建议：`server_id` 作为主键，存 `player_id/ip/expires_at`；hasJoined 成功后立刻删除，严格一次性；TTL 30–60 秒。

### 7.7 profile 与签名

`GET /sessionserver/session/minecraft/profile/{uuid}?unsigned=false`

```json
{
  "id": "32hex",
  "name": "PlayerName",
  "properties": [
    {
      "name": "textures",
      "value": "base64...",
      "signature": "base64..."
    },
    {
      "name": "uploadableTextures",
      "value": "skin,cape"
    }
  ]
}
```

`value` 解码后：

```json
{
  "timestamp": 1690000000000,
  "profileId": "32hex",
  "profileName": "PlayerName",
  "isPublic": true,
  "signatureRequired": true,
  "textures": {
    "SKIN": {
      "url": "https://skin.example.com/textures/<sha256>",
      "metadata": {"model": "slim"}
    },
    "CAPE": {
      "url": "https://skin.example.com/textures/<sha256>"
    }
  }
}
```

要点：

- `unsigned` 缺省或 `true` → 不签名；只有 `unsigned=false` → 签名。
- `profileId`/`id` 都是**无横线** UUID。
- `timestamp` 是毫秒。
- `isPublic` 固定 true。
- `signatureRequired` 只在签名响应里出现。
- `SKIN.metadata.model` 仅 alex 有，值 `slim`；steve 不写。
- `uploadableTextures` 是 authlib-injector 扩展，新插件固定 `skin,cape`。
- 签名算法：**SHA1withRSA**；`base64` 标准编码。
- 签名对象是 `properties[].value` 字符串本身（base64 后的 JSON），不是解码后的 JSON。

Go 签名示例：

```go
h := sha1.Sum([]byte(value))
sig, err := rsa.SignPKCS1v15(rand.Reader, privKey, crypto.SHA1, h[:])
signature := base64.StdEncoding.EncodeToString(sig)
```

> 不要用 SHA256；authlib-injector 规范明确要求 SHA1withRSA。

### 7.8 UUID

两种算法：

**v3（离线兼容，默认）**：

```text
md5("OfflinePlayer:" + playerName)
b[6] = (b[6] & 0x0f) | 0x30
b[8] = (b[8] & 0x3f) | 0x80
hex(b)  // 32 位，无横线
```

Go：

```go
sum := md5.Sum([]byte("OfflinePlayer:" + name))
b := sum[:]
b[6] = (b[6] & 0x0f) | 0x30
b[8] = (b[8] & 0x3f) | 0x80
uuid := hex.EncodeToString(b)
```

**v4（随机）**：随机 16 字节，设置 version/variant 位，落库保存映射。

`uuid` 表建议：

```sql
CREATE TABLE uuid (
  pid  BIGINT UNSIGNED NOT NULL,
  name VARCHAR(50) NOT NULL,
  uuid CHAR(32) NOT NULL,
  PRIMARY KEY (pid),
  UNIQUE KEY uk_uuid_uuid (uuid),
  UNIQUE KEY uk_uuid_name (name),
  CONSTRAINT fk_uuid_player FOREIGN KEY (pid) REFERENCES players(pid) ON DELETE CASCADE
);
```

- v3 可以每次重算，但建议仍落库，便于改名/查询。
- v4 必须落库。
- 角色改名：v3 重算；v4 保留 UUID，更新 name；旧 token 应失效或暂时失效，让启动器刷新。

### 7.9 令牌模型

新插件：

- `AccessToken::create()` 用 Passport `createToken('Yggdrasil Connect', [PROFILE_SELECT, SERVER_JOIN])`。
- JWT 由 `storage/oauth-private.key` 签名，`storage/oauth-public.key` 验证。
- JWT 包含 `jti`、`aud`（Passport client id）、`sub`（uid）、`selectedProfile`、scope。
- DB `oauth_access_tokens` 存 token 记录；吊销即 `revoked=1`。
- 过期：`ygg_token_expire_1`（默认 259200 秒 = 3 天）；刷新窗口：`ygg_token_expire_2`（默认 604800 秒 = 7 天）。
- 角色改名后，通过缓存 `player-renamed-{uuid}` 与 token 创建时间比较，让旧 token 暂时失效。

旧插件：

- `accessToken` = 随机 UUID（无横线）。
- `serialize($token)` 存 Laravel Cache，key `TOKEN_{accessToken}`；用户维度 key `ID_{email}`；单用户单 token。
- 过期判断用 `createdAt + expire1/expire2`。

**Go 建议**：不要照抄 Laravel Cache 或 Passport。用不透明随机 token + DB：

```sql
CREATE TABLE tokens (
  id              BIGINT UNSIGNED AUTO_INCREMENT PRIMARY KEY,
  account_id      BIGINT UNSIGNED NOT NULL,
  access_hash     CHAR(64) NOT NULL,       -- sha256(accessToken)
  client_token    VARCHAR(64) NOT NULL,
  player_id       BIGINT UNSIGNED NULL,    -- 绑定的角色
  created_at      DATETIME(3) NOT NULL,
  expires_at      DATETIME(3) NOT NULL,    -- access 有效期
  refresh_until   DATETIME(3) NOT NULL,    -- 可刷新截止
  revoked_at      DATETIME(3) NULL,
  UNIQUE KEY uk_tokens_access_hash (access_hash),
  KEY idx_tokens_account (account_id)
);
```

- 只存 hash，不存明文；比较用常量时间。
- refresh 时旋转 token，旧 token 立即 revoke。
- validate 时检查 `expires_at`、`revoked_at`、clientToken、绑定角色；join 时再检查绑定。
- signout 吊销该账号所有 token。
- 角色改名时 revoke 绑定该角色的 token，或设置 `renamed_at` 让 token 暂时失效。

### 7.10 规范 vs 新插件的差异

| 项 | 规范 | 新插件 | Go 建议 |
|---|---|---|---|
| `clientToken` | refresh/validate 要校验 | 不保存、不校验 | 保存并校验；refresh 返回原值 |
| `availableProfiles` | 返回全部可用角色 | 单角色/角色名登录时只返回绑定的 | 按规范返回全部，或明确兼容模式 |
| `validate` | token 有效即可 204 | 还要求已绑定角色 | 按规范；绑定检查放 join |
| Content-Type 错误 | 通常 415 | 返回 **200** + error JSON | 用 415，或明确兼容 200 |
| join 记录 | accessToken + IP + serverId，TTL 短 | 只存 profile+IP，TTL 120s | 存 player/ip，TTL 30–60s |
| hasJoined 一次性 | 应消费记录 | 不删除，120s 可重复 | 成功后删除 |
| signout 失败 | 应报错 | 也 204 | 按规范返回 403，或明确兼容 |
| 令牌数量限制 | 建议限制 | 有 `ygg_tokens_limit` 选项但代码未强制 | 实现上限，淘汰最旧 |
| user.id | 任意 UUID | v5(uid) | 固定算法并文档化 |
| 错误文案 | 英文/可读 | 中文本地化 | 统一错误码 + 可读 message |

### 7.11 Janus / Yggdrasil Connect

- Janus 是独立的 NestJS OIDC 服务，和 BS 共用 MySQL；需要 `yggdrasil-connect` 插件。
- 它把 AuthServer 的“密码交给第三方应用”替换成 OAuth2/OIDC 授权码流程，适合现代启动器。
- 如果 YggServer-Go 只做经典 Yggdrasil + authlib-injector，**第一版不需要 Janus/OIDC**；先把经典 AuthServer 做对。

---

## 8. 账号、权限与安全

### 8.1 注册/登录

- 注册：`email` + `password`（8–32）+ 验证码 + 角色名（默认 3–16，官方规则 `^[A-Za-z0-9_]+$`）。
- 同一 IP 历史累计注册数 ≥ `regs_per_ip`（默认 3）就拒绝。
- 初始积分 1000；注册即创建角色，不扣分；注册后自动登录。
- 登录：`identification` 可以是邮箱或角色名；密码 6–32；同 IP 失败 >3 次后要求验证码。
- **被封禁用户仍能登录成功**，403 发生在后续 `authorize` 中间件；`/auth/logout` 例外放行。
- 未验证邮箱/未填邮箱由中间件拦截，不在登录处拦截。

### 8.2 邮箱验证/找回密码

- 验证链接是 Laravel 签名 URL，**永不过期**，可重复使用。
- 找回密码链接 1 小时有效，**可重复使用**，不是一次性 token。
- 邮件发送有 60 秒/180 秒节流。
- `verification_token` 字段存在但代码从未使用。

### 8.3 密码哈希

- 默认 BCRYPT；支持 ARGON2I、PHP_PASSWORD_HASH、MD5、SALTED2MD5、SHA256、SALTED2SHA256、SHA512、SALTED2SHA512。
- SALT 只对 SALTED2* 生效；bcrypt/argon2 忽略。
- **没有 rehash/升级逻辑**。
- Go 接入旧库注意：PHP bcrypt 产出 `$2y$`，Go `x/crypto/bcrypt` 只认 `$2a$/$2b$`；比较前需把 `$2y$` 归一化为 `$2a$`（算法一致，但建议写测试验证）。

### 8.4 权限与 OAuth

- `permission`：`-1` 封禁 / `0` 普通 / `1` 管理员 / `2` 超管。
- OAuth2 用 Passport；scope 包括 `User.Read`、`Player.Read/ReadWrite`、`Closet.Read/ReadWrite` 等。
- 有一个拼写 bug：注册的 scope 是 `Closet.ReadWrtie`，API 路由用的是 `Closet.ReadWrite`，导致该 scope 实际不可授予。
- `/api/*` 是 OAuth2 资源 API，与 Yggdrasil 无关；Go 第一版可不做。

### 8.5 安全发现（按严重度）

| 发现 | 说明 |
|---|---|
| 无 DB 唯一约束 | email/角色名/hash 并发可重复 |
| 验证/重置链接可重放 | 不是一次性 token |
| 改密/改邮箱/删号不吊销 OAuth token | Passport token 默认 1 年 |
| 删号不清理 textures/closet/tokens | 只删 players |
| 上传无事务 | 半成功状态 |
| 只限宽不限高 | 解压炸弹面 |
| 预览缓存 key 不含 height | 尺寸污染 |
| 私有材质按 hash 公开可取 | hash 泄露即可读 |
| 举报删除不查引用计数 | 误删共享文件 |
| 插件系统无沙箱 | PHP 插件拥有完整应用权限（Go 不要复刻） |

---

## 9. 对 YggServer-Go 的落地建议

### 9.1 必须保留的对外行为

1. **Yggdrasil 路径/字段/状态码**：`/authserver/*`、`/sessionserver/session/minecraft/*`、`/api/profiles/minecraft`、`/api/user/profile/{uuid}/{type}`。
2. **错误格式** `{error, errorMessage}` + 正确 HTTP 状态码；不要混用 BS 的 `{code,message}`。
3. **`unsigned` 默认 true**；`unsigned=false` 才签名。
4. **SHA1withRSA** 签名；RSA 4096；metadata 返回 PEM 公钥。
5. **UUID v3 离线算法**（默认）；v4 可选。
6. **材质 URL 文件名 = hash**；`Content-Type: image/png`；SHA-256。
7. **metadata**：`serverName`、`implementationName`、`implementationVersion`、`links`、`skinDomains`、`signaturePublickey`、`feature.non_email_login`。
8. **ALI 头** `X-Authlib-Injector-API-Location`。
9. **上传校验**：PNG、最大 KB、宽高倍数、比例、重编码、hash。
10. **CustomSkinAPI**（可选但成本低）：`GET /{player}.json`。

### 9.2 不要照抄的部分

- 字符串类型枚举、缺失唯一约束、无主键衣柜、无外键。
- Laravel Cache 当令牌库、Passport JWT、插件系统、Blade/Twig、options 字符串布尔。
- 上传无事务、举报删除不查引用计数、预览缓存 key 缺 height。
- 以 `permission=-1` 同时表达封禁和权限。

### 9.3 建议的 Go schema（摘要）

```sql
CREATE TABLE users (
  uid           BIGINT UNSIGNED AUTO_INCREMENT PRIMARY KEY,
  email         VARCHAR(255) NOT NULL,
  nickname      VARCHAR(50)  NOT NULL DEFAULT '',
  password_hash VARBINARY(255) NOT NULL,
  state         TINYINT NOT NULL DEFAULT 0,      -- 0正常 1封禁 2注销
  permission    TINYINT NOT NULL DEFAULT 0,      -- 0普通 1管理员 2超管
  score         INT NOT NULL DEFAULT 0,
  avatar_tid    BIGINT UNSIGNED NULL,
  verified      TINYINT NOT NULL DEFAULT 0,
  locale        VARCHAR(35) NULL,
  ip_hash       CHAR(64) NULL,                   -- 不存原始 IP
  last_sign_at  DATETIME(3) NOT NULL,
  created_at    DATETIME(3) NOT NULL,
  UNIQUE KEY uk_users_email (email)
);

CREATE TABLE players (
  pid        BIGINT UNSIGNED AUTO_INCREMENT PRIMARY KEY,
  uid        BIGINT UNSIGNED NOT NULL,
  name       VARCHAR(50) NOT NULL,
  tid_skin   BIGINT UNSIGNED NULL,
  tid_cape   BIGINT UNSIGNED NULL,
  updated_at DATETIME(3) NOT NULL,
  UNIQUE KEY uk_players_name (name),
  KEY idx_players_uid (uid),
  CONSTRAINT fk_players_user FOREIGN KEY (uid) REFERENCES users(uid) ON DELETE CASCADE
);

CREATE TABLE textures (
  tid         BIGINT UNSIGNED AUTO_INCREMENT PRIMARY KEY,
  name        VARCHAR(50) NOT NULL,
  type        TINYINT NOT NULL,                  -- 0 steve 1 alex 2 cape
  hash        CHAR(64) NOT NULL,
  size_kb     INT UNSIGNED NOT NULL,
  width       SMALLINT UNSIGNED NOT NULL,
  height      SMALLINT UNSIGNED NOT NULL,
  uploader    BIGINT UNSIGNED NOT NULL,
  is_public   TINYINT NOT NULL DEFAULT 0,
  likes       INT UNSIGNED NOT NULL DEFAULT 0,
  created_at  DATETIME(3) NOT NULL,
  KEY idx_textures_hash (hash),
  KEY idx_textures_uploader (uploader),
  KEY idx_textures_type_time (type, created_at)
);

CREATE TABLE user_closet (
  user_uid    BIGINT UNSIGNED NOT NULL,
  texture_tid BIGINT UNSIGNED NOT NULL,
  item_name   VARCHAR(50) NULL,
  PRIMARY KEY (user_uid, texture_tid),
  CONSTRAINT fk_closet_user FOREIGN KEY (user_uid) REFERENCES users(uid) ON DELETE CASCADE,
  CONSTRAINT fk_closet_texture FOREIGN KEY (texture_tid) REFERENCES textures(tid) ON DELETE CASCADE
);

CREATE TABLE uuid (
  pid  BIGINT UNSIGNED NOT NULL PRIMARY KEY,
  name VARCHAR(50) NOT NULL,
  uuid CHAR(32) NOT NULL,
  UNIQUE KEY uk_uuid_uuid (uuid),
  UNIQUE KEY uk_uuid_name (name),
  CONSTRAINT fk_uuid_player FOREIGN KEY (pid) REFERENCES players(pid) ON DELETE CASCADE
);

CREATE TABLE tokens (
  id            BIGINT UNSIGNED AUTO_INCREMENT PRIMARY KEY,
  account_id    BIGINT UNSIGNED NOT NULL,
  access_hash   CHAR(64) NOT NULL,
  client_token  VARCHAR(64) NOT NULL,
  player_id     BIGINT UNSIGNED NULL,
  created_at    DATETIME(3) NOT NULL,
  expires_at    DATETIME(3) NOT NULL,
  refresh_until DATETIME(3) NOT NULL,
  revoked_at    DATETIME(3) NULL,
  UNIQUE KEY uk_tokens_hash (access_hash),
  KEY idx_tokens_account (account_id)
);

CREATE TABLE minecraft_sessions (
  server_id  VARCHAR(64) PRIMARY KEY,
  player_id  BIGINT UNSIGNED NOT NULL,
  ip         VARCHAR(45) NULL,
  expires_at DATETIME(3) NOT NULL
);
```

说明：

- `textures.hash` 不做 UNIQUE，因为同一文件可能对应多个“皮肤条目”（不同上传者/名称）；建普通索引，文件按 hash 去重。
- 如果希望更严格，可拆 `texture_files(hash PK, size, width, height)`，`textures.file_hash` 外键；但会增加一层，按需。
- `players.tid_skin/tid_cape` 用 NULL 表示无，不用 0/-1 哨兵。
- `avatar_tid` 用 NULL；`ON DELETE SET NULL` 可选。
- 所有时间 UTC，`DATETIME(3)`。

### 9.4 令牌/会话实现顺序

```text
authenticate:
  校验账号密码/封禁/邮箱验证
  查角色列表
  决定 selectedProfile（1 个 / 角色名登录）
  生成 accessToken（32 字节随机，返回 base64url）
  生成/回显 clientToken
  DB 存 sha256(accessToken) + client_token + player_id + 过期
  返回 availableProfiles/selectedProfile/user

refresh:
  按 accessToken hash 查行
  校验 clientToken、未吊销、refresh_until
  校验 selectedProfile（未绑定才允许选择；已绑定必须一致）
  生成新 token，插入新行，旧行 revoke
  返回新 token + 原 clientToken

validate:
  校验 accessToken、clientToken、未吊销、未过期
  204 / 403

invalidate:
  能查到就 revoke；总是 204

signout:
  校验用户名密码；吊销该账号所有 token；204

join:
  校验 token、selectedProfile 绑定、角色归属
  写 minecraft_sessions(server_id, player_id, ip, expires_at=now+60s)
  204

hasJoined:
  查 session；校验 username/IP
  删除 session
  返回带 SHA1withRSA 签名的 profile
```

### 9.5 实施顺序

1. DB 迁移 + `uuid` v3/v4 + RSA 密钥加载。
2. `/authserver/authenticate` + token 表 + 错误格式。
3. `/authserver/refresh/validate/invalidate/signout`。
4. `/sessionserver/join/hasJoined` + 会话 TTL + IP。
5. `/sessionserver/profile/{uuid}` + profile property + SHA1withRSA 签名。
6. `/api/profiles/minecraft` + metadata + ALI。
7. 材质上传 + `/textures/{hash}` + 校验。
8. 角色/衣柜/皮肤库（可选）。
9. 管理后台/积分/邮件/OAuth（可延后）。

### 9.6 测试清单

- UUID v3 固定向量；v4 版本/变体位。
- SHA1withRSA 签名：用公钥验证；确认不是 SHA256。
- authenticate：单角色自动绑定；多角色邮箱登录不绑定；角色名登录绑定；封禁/未验证/无角色。
- refresh：clientToken 不匹配；未绑定选角色；已绑定换角色；过期窗口。
- validate/invalidate：204/403；invalidate 总是 204。
- join/hasJoined：绑定不一致、角色不属于用户、IP 不匹配、一次性消费。
- profile：`unsigned` 默认无签名；`unsigned=false` 有签名；204 不存在。
- 上传：非 PNG、超 KB、宽高非倍数、alex 2:1、cape 1:1、hash 去重、并发重复。
- 纹理服务：`Content-Type: image/png`、ETag、缓存头。

---

## 10. 验证边界与未覆盖项

### 10.1 本次实际做了什么

- 逐文件读取了 BS 核心的路由、控制器、模型、迁移、配置、前端上传/皮肤库/角色/衣柜代码。
- 逐文件读取了旧 `yggdrasil-api` 和新 `yggdrasil-connect` 的路由、认证、会话、Profile、Token、异常、UUID、中间件。
- 对照了 authlib-injector 的 Yggdrasil 服务端技术规范。
- **没有创建孙代理**；子代理只做各自调研，未再分派。

### 10.2 没有验证的

| 项 | 原因 |
|---|---|
| 实际运行 Blessing Skin | 本机没有 PHP/Composer/MySQL 环境，且外网受限 |
| 浏览器页面/真实 AJAX | 未部署 |
| Minecraft 客户端/authlib-injector 联调 | 环境无客户端 |
| 真实 `/{player}.json` 抓包 | 已从 Laravel 10 `JsonResponse` 源码确认走 `toJson()`，但未运行 |
| Passport 四张表逐列结构 | 来自 vendor 迁移，未逐列抓取 |
| 旧插件在 BS 6 上是否可运行 | 旧插件只声明支持 BS 5，未实测 |
| 官方 Docker 镜像上传材质 | Dockerfile 只装 gd+zip，但上传路径用 Imagick API，存在矛盾，未实测 |
| 前端 403 分支的 `toast.error(undefined)` | 静态推断，未浏览器复现 |

### 10.3 关键源码入口

- 核心路由：[routes/web.php](https://github.com/bs-community/blessing-skin-server/blob/dev/routes/web.php)、[routes/static.php](https://github.com/bs-community/blessing-skin-server/blob/dev/routes/static.php)
- 上传：[SkinlibController.php](https://github.com/bs-community/blessing-skin-server/blob/dev/app/Http/Controllers/SkinlibController.php)
- 材质服务：[TextureController.php](https://github.com/bs-community/blessing-skin-server/blob/dev/app/Http/Controllers/TextureController.php)
- 角色/衣柜：[PlayerController.php](https://github.com/bs-community/blessing-skin-server/blob/dev/app/Http/Controllers/PlayerController.php)、[ClosetController.php](https://github.com/bs-community/blessing-skin-server/blob/dev/app/Http/Controllers/ClosetController.php)
- 迁移：[database/migrations](https://github.com/bs-community/blessing-skin-server/tree/dev/database/migrations)
- 新 Yggdrasil 插件：[yggdrasil-connect](https://github.com/bs-community/blessing-skin-plugins/tree/master/plugins/yggdrasil-connect)
- 旧插件：[yggdrasil-api](https://github.com/bs-community/yggdrasil-api)
- 规范：[Yggdrasil 服务端技术规范](https://yushijinhun.github.io/authlib-injector/zh/Yggdrasil-%E6%9C%8D%E5%8A%A1%E7%AB%AF%E6%8A%80%E6%9C%AF%E8%A7%84%E8%8C%83.html)

---

## 11. 附录

### 11.1 端点速查

```text
# Yggdrasil（前缀 /api/yggdrasil）
GET    /
POST   /authserver/authenticate
POST   /authserver/refresh
POST   /authserver/validate
POST   /authserver/invalidate
POST   /authserver/signout
POST   /sessionserver/session/minecraft/join
GET    /sessionserver/session/minecraft/hasJoined
GET    /sessionserver/session/minecraft/profile/{uuid}
POST   /api/profiles/minecraft
PUT    /api/user/profile/{uuid}/{skin|cape}
DELETE /api/user/profile/{uuid}/{skin|cape}

# 皮肤站本体
POST   /texture
GET    /textures/{hash}
GET    /raw/{tid}
GET    /preview/{tid}
GET    /avatar/{tid}
GET    /{player}.json
GET    /skinlib/list
POST   /user/player
PUT    /user/player/{pid}/textures
POST   /user/closet
```

### 11.2 默认配置项（节选）

| 键 | 默认值 | 说明 |
|---|---|---|
| `site_url` | 空 | 站点绝对 URL，材质/签名 URL 的根 |
| `site_name` | `Blessing Skin` | 站点名 |
| `register_with_player_name` | `true` | 注册时创建角色 |
| `require_verification` | `false` | 是否要求邮箱验证 |
| `regs_per_ip` | `3` | 每 IP 历史注册上限 |
| `player_name_rule` | `official` | `official/cjk/utf8/custom` |
| `player_name_length_min/max` | `3/16` | 角色名长度 |
| `user_initial_score` | `1000` | 初始积分 |
| `score_per_storage` | `true` | 公开材质每 KB 积分（true=1） |
| `private_score_per_storage` | `10` | 私有材质每 KB 积分 |
| `score_per_player` | `100` | 添加角色积分 |
| `score_per_closet_item` | `0` | 收藏积分 |
| `score_award_per_texture` | `0` | 上传奖励 |
| `max_upload_file_size` | `1024` | KB |
| `max_texture_width` | `8192` | px（2025 迁移写入） |
| `cache_expire_time` | `31536000` | 秒 |
| `status_code_for_private` | `403` | 私有材质返回 403/404 |
| `allow_downloading_texture` | `true` | 是否允许 `/raw/{tid}` |
| `texture_name_regexp` | 空 | 材质名正则 |
| `sign_gap_time` | `24` | 签到冷却小时 |
| `sign_score` | `10,100` | 签到积分范围 |

### 11.3 需要你决策的 5 件事

1. **兼容目标**：是“只兼容 authlib-injector/Yggdrasil 协议”，还是也要兼容 Blessing Skin 的皮肤站前端/接口（`{code,message,data}`、字符串 type、Twig 页面）？
2. **UUID 算法**：默认 v3（兼容离线服，改名会变 UUID）还是 v4（随机，改名不变）？建议默认 v3，允许配置。
3. **令牌形态**：不透明随机 token（推荐，简单可吊销）还是 JWT（无状态，但需要 jti/吊销表）？
4. **功能范围**：第一版是否要角色多角色、衣柜、积分、签到、举报、邮件验证、OAuth2？这些会显著增加工作量。
5. **旧库迁移**：是否需要直接读 Blessing Skin 的 MySQL 库？如果需要，`textures.type` 字符串、`permission=-1`、无唯一约束这些都要做兼容映射。

### 11.4 建议的第一版范围

如果按“Yggdrasil 优先、皮肤站最小可用”的路线：

- 账号/角色；
- `authenticate/refresh/validate/invalidate/signout`；
- `join/hasJoined`；
- `profile`；
- `/textures/{hash}`；
- `POST /texture`；
- metadata。

其余（皮肤库 UI、衣柜、积分、OAuth2、Janus）延后。

---

*报告完。*
