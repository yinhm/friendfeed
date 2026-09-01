# Public Feed API V1 运维手册

本文只描述已经实现的 `/api/v1` 上线、凭据处置和故障边界。HTTP contract、数据语义和媒体
验证规则分别以 `docs/web_api.md`、`docs/media_upload.md` 为准。

## 部署边界

- 公网只暴露 nginx/ffweb；ffdb gRPC 必须继续只监听 loopback。
- 先部署 ffdb，再部署 ffweb，最后部署 nginx。旧 ffweb 不会调用新增 RPC；反向顺序会让新
  ffweb 在旧 ffdb 上认证失败。
- nginx 的 `/api/v1/` 使用与上传功能相同的 `client_max_body_size`，read/send timeout 为 35 秒；
  ffweb 自身在 30 秒终止请求。systemd 的 ffweb stop 仍先取消在途 API context，再在既有
  `TimeoutStopSec` 内完成退出。
- nginx access log 可以记录 method/path/status/latency，但配置和日志格式不得加入
  `$http_authorization`、Cookie、multipart body 或 filename。应用日志同样不得 dump request。
- V1 不返回 CORS header，浏览器跨 origin integration 不属于当前支持面。

上线前运行 `nginx -t`，并确认生成的站点配置包含独立 `/api/v1/` location。启用 route 后先用
测试 Feed key 演练四个 endpoint，再向真实 integration 发放 key。

## Key 生命周期与泄漏响应

Feed owner、Group admin 或 super 在 `/feed/:id/api` 管理 key。完整 token 只在 Generate/Rotate
成功后显示一次；数据库只保存 key ID 与 secret SHA-256。

- **计划轮换**：Rotate，立即把新 token 写入调用方 secret store，再验证一次 GET。旧 token 在
  Rotate commit 后立即失效。
- **停止 integration**：Revoke。操作幂等；需要恢复时 Generate 新 key，旧 token 不会复活。
- **疑似泄漏**：先 Revoke（或必须维持服务时立即 Rotate），再检查 nginx/journald、shell history、
  CI artifact、browser storage 与 heap profile。不要把真实 token 贴进工单、聊天或命令示例。
- 管理日志只允许 actor UUID、Feed UUID、非敏感 key ID、action、result 和时间，不含 token。

`audit_store` 检查 Feed API key 的 key/value 编码、Feed ownership、key ID/digest 长度和 active/revoked
状态。`inspect-system` 只输出 active/revoked 聚合数量，不输出 Feed 或 key ID 列表；发现损坏
record 时 fail loud，由 `audit_store` 定位。

## curl 验收

把 token 放入临时环境变量；以下命令不得使用 shell tracing，结束后执行 `unset FF_API_TOKEN`：

```bash
export FF_API_TOKEN='ffk1_...'

curl --fail-with-body -H "Authorization: Bearer $FF_API_TOKEN" \
  https://friendfeed.example/api/v1/feed

curl --fail-with-body -H "Authorization: Bearer $FF_API_TOKEN" \
  'https://friendfeed.example/api/v1/feed/entries?limit=20'

curl --fail-with-body -H "Authorization: Bearer $FF_API_TOKEN" \
  https://friendfeed.example/api/v1/feed/entries/ENTRY_UUID

curl --fail-with-body -H "Authorization: Bearer $FF_API_TOKEN" \
  -F 'title=API smoke test' \
  --form-string 'body_html=<p>Created by the release smoke test.</p>' \
  -F 'file=@./fixture.png' \
  https://friendfeed.example/api/v1/feed/entries
```

GET 返回 `{"data": ...}`；list 另含 `pagination.next_cursor`；POST 成功为 201。错误统一为
`{"error":{"code","message","request_id"}}`，不得含 gRPC error、token 或服务端路径。
POST 非幂等：网络超时后先查询 Entry list，不要盲目重试。

历史外部内容使用独立 import contract。不要手写或记录真实 token；示例 metadata：

```json
{"source":{"kind":"twitter","account_id":"12345","item_id":"1295071681511407617","url":"https://x.com/example/status/1295071681511407617"},"published_at":"2020-08-17T12:34:56Z","title":"","body_html":"Archived post"}
```

```bash
curl --fail-with-body -H "Authorization: Bearer $FF_API_TOKEN" \
  --form-string "metadata=$(cat ./metadata.json)" \
  -F 'file=@./archive-media.jpg' \
  https://friendfeed.example/api/v1/feed/imports
```

首次创建返回 201/`created=true`，相同 identity 重放返回 200/`created=false`；409 表示来源 identity
与现存 Entry 冲突。archive import 不进入 Home/Public/realtime。批量归档应使用独立
`twitter-import` connector，而不是 shell 循环；完整边界见 `docs/external_import.md`。

## 回滚与媒体孤儿

回滚 ffweb/nginx 会暂停 Public API，但不会删除 `TableFeedApiKey` 行。回滚 ffdb 前必须确认目标
版本能够打开当前 application schema；不能以二进制回滚绕过 schema marker。恢复时仍按
ffdb → ffweb → nginx 顺序。

Public API 会在 `PostEntry` 前完成媒体验证与 canonical promote。若 promote 成功而领域 mutation
随后失败，可能留下没有 Entry 引用的 canonical object。它不获得额外执行权限，主动内容仍强制
下载；当前版本不自动删除这类对象。操作者应保留失败 request ID 和时间窗口，使用未来的
引用 audit/GC 处理，不得按猜测路径直接删除共享的 content-addressed object。

## Release gate

1. 在一致性测试环境生成 personal 和 Group key，分别执行 GET/list/single/POST。
2. Rotate 后确认旧 token 为同形状 401、新 token 可用；Revoke 后确认新 token也为 401。
3. 用另一个 Feed 的 Entry UUID 验证稳定 404；验证 invalid cursor、413、415 和并发槽满 503。
4. 执行前端完整门禁、Go build/vet/test、Public API/upload E2E 和 shutdown 测试。
5. 对 Git diff、HTTP/HTML response、browser storage、测试日志/journald fixture 与 heap sample 做
   credential 搜索；只允许专门测试 fixture 中出现伪 token。
6. `audit_store` 无 key blocker、`inspect-system` 计数符合预期且输出不含 identifier 后才启用公网 route。

V1 release note：新增 per-Feed API key 管理、三个 read endpoint、一个 multipart create endpoint、
Group machine-author 语义、复用的安全媒体 pipeline，以及相应 audit/inspect/runbook。它不是原
FriendFeed API 的兼容实现，也不承诺 idempotency、CORS、remote URL mirror 或应用层限流。
