# Feed 管理 CLI

本文定义 ffdb loopback 管理边界上的 Feed 诊断与可见性维护命令。它们面向操作者，不是浏览器功能，也不对公网暴露。

## 状态边界

当前不存在独立的“停用 Feed”状态：

- `Profile.Deleted` 是软删除终态，不是可逆停用开关；
- `Profile.Private` 只控制可见性，Feed 仍可登录、发帖、导入内容；
- `TimelineState` 的 active/inactive 只描述 Home 缓存冷热；
- `FeedService.Enabled` 只控制一个导入绑定。

本版只提供 user Feed 的 public/private 切换。Group privacy 在创建时固定，`special` 与 deleted Profile 也不能通过本命令修改。若未来需要可逆运营停用，应单独定义 `suspended` 的登录、读取、写入、Service 和 Feed API 语义，不能复用 `Deleted`。

## Inspect

```bash
./cli --address 127.0.0.1:8901 feed inspect yinhm
./cli --address 127.0.0.1:8901 feed inspect <uuid> --entries 50
./cli --address 127.0.0.1:8901 feed inspect yinhm --json
```

`InspectFeed` 可以读取 soft-deleted Profile 的管理摘要，但不会返回 Entry 正文、`RawBody`、OAuth 凭据、Cookie/session 或 Feed API secret。输出包含：

- Profile UUID/ID/type/private/deleted/super；
- UserMap 回指一致性；
- direct EntryIndex、Following、Follower、pending request 与 FeedService 数量；
- Group 的 admin/member 数量；
- TimelineState、Feed archive snapshot/dirty marker；
- Feed API key 是否存在及是否 active；
- 默认最近 20 条、最多 100 条 Entry 的 UUID/作者/目标/时间摘要；
- 孤儿索引或损坏派生状态的有界 warning。

Entry 总数等计数需要扫描对应 Feed 前缀，因此该命令是显式运维诊断，不应被监控系统高频轮询。

旧 `cli debug --u` 是兼容入口，本版不删除或改变其语义；新运维流程使用 `feed inspect`。

## Privacy

单 Feed 预览：

```bash
./cli --address 127.0.0.1:8901 feed privacy private --feed yinhm
./cli --address 127.0.0.1:8901 feed privacy public --feed yinhm
```

批量预览：

```bash
./cli --address 127.0.0.1:8901 feed privacy private \
  --feed alice \
  --feed bob

./cli --address 127.0.0.1:8901 feed privacy private --file feeds.txt
```

列表文件每行一个 Feed ID 或 UUID，允许空行和以 `#` 开头的注释。`--feed` 可重复，并可与 `--file` 合并；重复标识会在客户端和服务端解析后去重。每次 RPC 最多处理 100 个 Feed。

默认永远是 dry-run。实际写入必须显式追加 `--apply`，并输入预览给出的确认词，例如：

```text
Type "PRIVATE 3" to apply:
```

服务端在 apply 时重新解析并验证全部 Feed，不依赖先前预览结果。整个请求持有 Profile 状态更新锁，并在一个 Pebble batch 中提交；不存在、deleted、Group 或 special Feed 会令整批失败，不会部分写入。

## Privacy 状态转换

Public → Private：

- 保留现有 Follow/Follower 边，现有 follower 继续可读；
- 清理理论上不应存在的旧 pending request，避免早期 private 周期的申请重新生效；
- 新关注者改走 FollowRequest 审批；
- 不重建 Home、public timeline 或 search；旧派生行由现有 request-scoped visibility resolver 过滤；
- FeedService 继续导入，Feed API key 继续只授权自己的 canonical Feed；
- Feed archive 不失效，因为 direct Entry 集合没有变化。

Private → Public：

- 保留现有 Follow/Follower 边；
- 在同一 batch 删除该 Feed 尚未处理的 FollowRequest；
- 不重建 timeline、search 或 archive。

## RPC 契约

```protobuf
rpc InspectFeed(InspectFeedRequest) returns (InspectFeedResponse);
rpc UpdateFeedState(UpdateFeedStateRequest) returns (UpdateFeedStateResponse);
```

`UpdateFeedState` 使用 `FeedStatePatch`，当前只有 optional `private`。未来若正式定义 `suspended`，可以 additive 地扩展 patch，而不把 RPC 变成包含删除、改名和重建的通用命令。两个 RPC 仅通过 ffdb loopback gRPC 暴露；不得转发为公开 HTTP API。
