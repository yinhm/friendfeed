# OAuth 身份标识

## 结论

OAuth 身份必须由“提供商 + 提供商分配的稳定主体 ID”确定：

```text
twitter:<Twitter numeric user ID>
google:<Google subject/account ID>
```

不得使用用户名、显示名、邮箱、access token 或 refresh token 作为身份主键。这些字段都可能变化。

## 当前持久化规则

OAuth 记录使用：

```go
oauthUserIDFrom(provider, userId)
```

内部 profile UUID 使用：

```go
UniqueKeyFrom(provider, userId)
```

因此，新用户的 OAuth key 和 profile UUID 都由同一组 `(provider, userId)` 确定；不同 provider 下相同的 `userId` 不会冲突。已经从 FriendFeed 迁移并绑定 UUID 的用户必须继续保留原 UUID，不能重新生成。

当前 `KeyFromString`/`UniqueKeyFrom` 会把整个组合字符串转成小写。Twitter 和当前 Google ID 都是数字，因此没有实际影响。OpenID Connect 的 `sub` 原则上区分大小写；未来增加可能返回非数字 subject 的 provider 时，只应规范化 provider，不能改变 subject，并且必须先设计兼容的版本化 key，不能直接改变现有持久化算法。

## Twitter/X

goth 的 Twitter provider 映射为：

```text
goth.User.UserID   = id_str
goth.User.NickName = screen_name
goth.User.Name     = display name
```

本项目回调再映射为：

```text
OAuthUser.UserId   = goth.User.UserID
OAuthUser.Name     = goth.User.NickName
OAuthUser.NickName = goth.User.Name
```

各字段用途：

| 字段 | 含义 | 用作唯一身份 |
|---|---|---|
| `OAuthUser.UserId` | Twitter 数字用户 ID | 是 |
| `OAuthUser.Name` | username / screen name | 否；可修改 |
| `OAuthUser.NickName` | 显示名 | 否；可修改 |
| email | 邮箱 | 否 |

X 官方将 `id` 定义为用户的唯一标识，并明确说明 `username` 虽然唯一但可以修改。因此，身份查找必须始终使用 `(twitter, UserId)`；username 变化只刷新 profile/service 属性，不能生成新 OAuth 身份或新 UUID。

Twitter profile URL 应使用 username：

```go
"https://twitter.com/" + authinfo.Name
```

不能使用可能包含空格的显示名 `authinfo.NickName`。

参考：

- [X API User 数据字典](https://docs.x.com/x-api/fundamentals/data-dictionary)
- [X API 按 User ID 查询](https://docs.x.com/x-api/users/get-user-by-id)

## Google

Google 的规范身份标识是 OpenID Connect `sub`：

- 在 Google Accounts 范围内唯一；
- 不会随邮箱变化；
- 永不复用；
- 区分大小写。

Google 明确禁止使用 email 作为用户记录的主键：一个账户可能在不同时期拥有不同邮箱，email 也不保证唯一。

当前 goth Google provider 请求旧版 OAuth2 UserInfo，并把返回的 obfuscated account `id` 写入 `goth.User.UserID`。本项目随后将它写入 `OAuthUser.UserId`。当前实际返回的数字 Google Account ID（例如 `114770841089446623145`）就是应持久化的稳定身份，而不是 Gmail 地址。

若未来升级 goth、改用现代 OIDC UserInfo 或自行验证 ID Token，应读取 `sub`，并在切换前确认它与已有 goth `UserID` 一致，避免为现有 Google 用户创建第二套身份。

参考：

- [Google OpenID Connect Reference](https://developers.google.com/identity/openid-connect/reference)
- [OpenID Connect Core 1.0](https://openid.net/specs/openid-connect-core-1_0.html)

## 修改约束

首次登录会创建 profile：初始 Profile ID 是系统生成的 `ff-` 随机 slug（见 `docs/profile_rename.md`），provider 显示名只作为 `Name`。`PutOAuth` 通过 gRPC response header `x-profile-newly-created`（常量定义在 `pb/helper.go`）标记本次新建的 profile——Profile 是持久化类型，不承载 transient RPC 状态；web 层据此把首次登录引导到 profile 页选择 ID。soft-deleted 账号在任何凭据或 FeedService 写入之前就被拒绝登录。Twitter 等 provider 的 FeedService 写入安排在 profile 创建之前：service 写入失败时不会留下半创建的账号，下次登录仍是首次并保留 onboarding；service 以确定性 profile UUID 为 key，会被之后创建的 profile 自然接管而不是成为孤儿。

以下行为属于持久化身份契约，修改前必须提供迁移方案和回归测试：

- OAuth key 的 provider/user ID 拼接方式；
- `UniqueKeyFrom(provider, userId)` 生成 profile UUID 的方式；
- goth `UserID` 到 `OAuthUser.UserId` 的映射；
- 已有 OAuth 身份与迁移 profile UUID 的绑定关系；
- provider 名称的规范化方式。

回归测试至少应覆盖：

- 同一 provider、同一 User ID 重登时 UUID 不变且 token 刷新；
- 新 profile 的初始 ID 是 `ff-` 随机 slug，满足 `ValidateProfileId` 且不占用已有 `UserMap` 映射；
- username、显示名或 email 改变时仍命中原身份；
- 不同 provider 下相同 User ID 不互相覆盖；
- 传入 UUID 与已有绑定冲突时拒绝写入；
- Twitter profile URL 使用最新 username，而身份 key 仍使用数字 User ID；
- Google email 改变时仍使用相同 subject/account ID。
