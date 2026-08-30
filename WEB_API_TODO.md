# Public Feed API V1 实施清单

本清单将 `docs/web_api.md` 拆成可独立 review、独立回退的阶段。每阶段完成后单独提交并运行其
指定门禁；不得跨阶段提前暴露半成品 endpoint。产品与安全语义以 spec 为准，清单只记录执行顺序。

## 0. Contract baseline

- [x] 将 `docs/web_api.md` 定稿并由 review 明确批准 V1 scope。
- [x] 新增 HTTP contract fixtures：success/error/feed/entry/list DTO 的 golden JSON。
- [x] 新增 route test，证明 `/api/v1/*` 尚未注册，避免无意复用现有 `/a/*`。
- [x] 记录现有 media limits、Entry mutation hooks、Feed manage 权限 helper 和 cursor 行为。

验收：

- golden fixture 不包含 protobuf-only 字段、rawBody、commands 或 credential；
- route baseline 和现有 Browser BFF tests 全绿；
- 仅测试/文档改动，不改变生产行为。

## 1. Feed API Key persistence

- [x] protobuf 新增 `FeedApiKeyRecord`；固定 `TableFeedApiKey = 123` 和 key/value codec。
- [x] 实现 token generate/parse、SHA-256 digest、constant-time compare。
- [x] 实现 Store/model 层 get/generate/rotate/revoke/authenticate primitive。
- [x] 同步 `model/types.go`、`docs/database_design.md`、根 `AGENTS.md`。

验收：

- token entropy、格式、错误长度/版本/字符、零 UUID 有 table tests；
- Pebble 中 grep/scan 不出现完整 token 或 raw secret；
- concurrent Generate 只有一个成功；Rotate commit 后旧 token 立即失败；Revoke 幂等；
- iterator 全关闭，写入单 batch，race 定向测试通过；
- `go test -race ./model/...` 与完整 Go 门禁通过。

## 2. ffdb key lifecycle and authentication RPC

- [x] additive 增加 status/generate/rotate/revoke/authenticate RPC 和 request/response。
- [x] 统一 Feed manage authorization：personal owner、Group admin、super。
- [x] 注册 server handlers；日志/interceptor 不输出 request/token。
- [x] 为 deleted actor/feed、非 admin、missing Feed、active Generate、revoked Rotate 固定 error code。

验收：

- personal 与 Group 权限矩阵 server tests 完整；
- token 只在 Generate/Rotate response 出现，status 永远不含 secret；
- Authenticate 只返回 canonical Feed UUID 和 non-secret key ID，所有失败使用统一错误码；
- 非授权调用不改变表；并发 role/key mutation 不产生越权窗口；
- protobuf compatibility tests、Go build/vet/test 通过。

## 3. Browser key management

- [x] 新增 `/feed/:id/api` authenticated React 页面与 Browser BFF actions。
- [x] 接入 Feed management nav，但普通 member 不显示且直接访问稳定 403。
- [x] Generate/Rotate/Revoke 使用统一 button/confirmation popover。
- [x] token 仅保存在 mutation 后当前 React state，不进入 bootstrap/URL/storage。

验收：

- handler tests 使用 OAuth ID 与 Profile slug 不相等的 fixture；
- component tests 覆盖一次显示、刷新消失、copy、rotate/revoke confirmation；
- browser history、HTML bootstrap、服务端日志 fixture 不含 token；
- 前端完整门禁和 key-management E2E 通过；
- 此阶段仍不注册 `/api/v1`。

## 4. Public API transport shell

- [x] 新建 `httpd/api` package，注册 `/api/v1` route group。
- [x] 实现 request ID、JSON/no-store/nosniff headers、error mapper。
- [x] 实现严格 Bearer parser、HTTP timeout/size boundary 和全局并发槽。
- [x] 通过 package 内测试 handler 验证 credential 传递，不暴露 debug route。

验收：

- 所有 error paths 返回 spec JSON，不出现 HTML/raw gRPC error；
- missing/malformed/unknown/revoked key 的对外形状一致；
- Authorization header 不出现在 access/application test logger；
- 并发槽满时 503、shutdown 和并发 race tests 通过；
- `/a/*` Browser BFF 行为零变化。

## 5. Read API

- [x] ffweb 每个 Public HTTP 请求调用 `AuthenticateFeedApiKey` 一次，不缓存结果。
- [x] 复用 `FetchFeed`、`FetchEntry`；以 ffweb 生成的可信 Feed identity metadata 表达 capability，
  不新增平行数据 RPC。
- [x] 实现 Feed/Entry/Public pagination DTO mapper 和 golden contract tests。
- [x] 注册四个 endpoint 中的三个 GET endpoint。
- [x] 复用 direct Feed cursor，限制 1–100，拒绝 Start/PageSize。

验收：

- personal、Group、private Feed key 均只能读自身；单条 Entry 按 effective
  `FeedUuid`（历史空值回退 `ProfileUuid`）判定，Feed A key 不能按 UUID 读 B Entry；
- cursor 多页无重复/死循环，deleted anchor 与 invalid cursor 有测试；
- Entry DTO 无 rawBody/commands/likes/comments/internal fields；
- protobuf 新增测试字段不会自动进入 golden JSON；
- HTTP handler、RPC、Go 完整门禁通过。

## 6. Shared media pipeline

- [x] 从 browser upload handler 抽取 verified upload/promote helper，不改现有 endpoint contract。
- [x] helper 接受内存字节与明确 thumbnail limit，不接受 client storage path。
- [x] 保持图片、文件、active-content download、staging cleanup、R2 gate 语义。

验收：

- browser upload 既有单测和 E2E 完全不变；
- helper tests 覆盖 MIME spoof、zip/container、超限、promote failure、canonical reuse；
- HTML/SVG 仍不能在主站或 media origin inline 执行；
- 不产生第二套 allowlist、thumbnail 或 SSRF 逻辑；
- 前端、Go 和 upload E2E 门禁通过。

## 7. Write domain mutation

- [x] 复用既有 `PostEntry`，只在可信 Feed identity metadata 存在时进入 machine-author 分支；
  metadata 缺失时维持现有用户语义与 Group 拒绝规则。
- [x] API 分支由 ffdb 无条件生成新 Entry UUID 并锁死 create-only；不得按 request Entry ID
  查询或进入编辑路径。
- [x] 复用正常 Entry lifecycle hooks；明确 Group machine-authored 分支。
- [x] 明确 POST 非幂等；忽略且不储存 Idempotency-Key，不新增幂等表或 replay 状态。

验收：

- 每个成功 RPC 只创建一条 Entry；重复请求允许得到两条不同 Entry，contract tests 锁定该语义；
- 提交已有 Entry ID、伪造 Entry ID 或与现存 Entry 内容相同都不能覆盖旧 Entry；
- personal/Group machine author、空 To、服务端 Via、direct index、timeline/public/realtime/search/archive dirty 正确；
- 历史 `FeedUuid=empty, From=Group, Via!=empty` 只读兼容；`From=user` 的遗留 Group 行不被
  误判为 machine author；
- 普通 Group 投稿作者语义回归测试保持真实 user；
- 无可信 metadata 的 `PostEntry` 不发生能力扩张；race 与 Go 全量门禁通过。

## 8. POST multipart endpoint

- [ ] 注册 `POST /api/v1/feed/entries`，严格解析白名单字段。
- [ ] 使用共享 media helper，构造 canonical RPC request 和 Public Entry DTO response。
- [ ] 固定 201 create、413/415 等状态。
- [ ] 所有失败路径清理 staging；canonical orphan 边界写入运维文档。

验收：

- text-only、image、document attachment 发布 E2E；
- client identity/date/Via/storage path 注入被忽略或 400 拒绝；
- upload/promote 失败不创建 Entry；
- 文档和示例明确网络超时后的盲目重试可能重复发布；
- request body/filename/token 不进入日志；
- API E2E、upload E2E、完整门禁通过。

## 9. Audit, operations, and release gate

- [ ] `audit_store` 增加 key record 编码、Feed ownership 和 digest 长度检查。
- [ ] runtime/system inspect 只输出 active/revoked 数量，不输出 key ID 列表或 secret。
- [ ] nginx/systemd/deploy 文档增加 `/api/v1` 请求体、timeout 和 no-log credential 注意事项。
- [ ] 增加 operator runbook：rotate/revoke、泄漏响应、rollback、orphan media。
- [ ] 更新 README feature matrix 与 release notes；删除本 TODO 前把未完成事项移入 open decisions。

最终验收：

- dev 创建/rotate/revoke personal 与 Group key 的完整演练；
- 四个 endpoint 的 curl examples 与 golden contract 一致；
- 认证、跨 Feed、multipart、并发上限、shutdown E2E 全绿；
- frontend lint/typecheck/test/build/E2E 与 Go build/vet/test 全绿；
- credential 泄漏审计覆盖 Git diff、journald fixture、HTTP response、HTML、browser storage、heap sample；
- 部署顺序和 rollback 在一致性副本演练后才允许 production 启用 route。
