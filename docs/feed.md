# Feed 与用户互动 Timeline

本文定义用户 Feed 之外的 LikeTimeline 与 CommentTimeline。2.0 只允许登录用户查看自己
Like 过或 Comment 过的 Entry；同一 Entry 的 Comment 折叠到该用户最后一条，权威互动明细
不复制进 Timeline。

## 页面与语义

提供两个只读页面：

```text
GET /feed/:name/likes
GET /feed/:name/comments
```

外部 URL 与既有 `/feed/:name` 保持一致，`:name` 的值是可读 Profile ID；复数
`likes/comments` 表示资源集合。Gin 路由参数必须继续命名为 `:name`，同一层级不能另写
`:id`，否则会与既有通配路由冲突。Web handler 通过既有 `UserMap` 解析为稳定 Profile UUID，普通映射未命中时再走
`UserRenameMap`。旧 ID 必须保留页面后缀做 302 soft redirect：

```text
/feed/old-id/likes    -> /feed/new-id/likes
/feed/old-id/comments -> /feed/new-id/comments
```

`renamedFeedLocation` 显式接收受控 suffix（空、`likes`、`comments`），并继续保留原 query；
不得靠字符串拼接任意用户输入路径。

索引、RPC request 和 cursor 的身份边界始终使用解析后的 Profile UUID，rename 不重写互动索引。
不存在、已删除、非 user 的 Profile 返回 404。

- **LikeTimeline**：按 Like 创建时间倒序，每个 Like 一项。Like 被取消后该项消失；重新 Like
  产生新的时间位置。
- **CommentTimeline**：按用户在每个 Entry 下最后一条 Comment 的创建时间倒序，每个 Entry
  最多一项。多条 Comment 只用于折叠和确定 Entry 的排序位置；页面中的 Entry 卡片与其他
  timeline 一致，展示该 Entry 当前全部现存 Comment，不把列表裁成用户的最后一条。
- 每项携带目标 Entry 作为上下文；Comment 项额外返回该用户在此 Entry 下最后一条 Comment，
  供排序位置校验而不限制渲染内容；Like 项额外指出本条 Like。
- 页面只展示既有数据，不产生 timeline bump，也不提供批量删除等新 mutation。

只在本人 Home/Profile 导航中提供 “Likes” 与 “Comments” 链接。2.0 不提供其他用户入口、
跨用户聚合、搜索、计数排行或 RSS 输出。

## 互动页授权边界

互动页保持 owner-only，并复用 `docs/perm.md` 定义的统一 Entry 可见性判断：

1. 两个 Web route 必须经过登录中间件；
2. URL 解析出目标 Profile UUID 后，要求当前 session 的稳定 UUID 与目标 UUID 完全相同；
3. ffdb RPC 再执行同一条 `viewer_uuid == profile_uuid` 校验，不能只相信 httpd 隐藏链接；
4. 不相同返回 403；不存在、已删除或非 user Profile 返回 404；
5. 通过本人校验后，仍逐条检查用户当前能否读取 Entry target；退出 private Group 或失去
   Follow 边后立即隐藏对应 Entry。已删除或缺失 Entry 视为派生索引孤儿，读路径跳过并安排
   有界懒删，audit/rebuild 最终收敛。

本页不承诺其他用户可见；未来开放前必须单独设计授权矩阵，不得只放宽 httpd 路由。

## 权威数据与派生索引

现有表继续是唯一权威数据：

```text
Like     (106): entry UUID | actor UUID   -> pb.Like
Comment  (107): entry UUID | comment UUID -> pb.Comment
```

它们不能按 actor 前缀查询，因此使用两个 actor-first 排序索引，以及 CommentTimeline 的定位表；
三者都是可丢弃、可重建的派生数据，不得在请求时扫描 Like/Comment 全表。

| 表 | 固定表号 | key | value |
|---|---:|---|---|
| `TableLikeTimeline` | 115 | `prefix(4) \| actor_uuid(16) \| reverse_ms(8) \| entry_uuid(16)` | empty |
| `TableCommentTimeline` | 116 | `prefix(4) \| actor_uuid(16) \| reverse_ms(8) \| entry_uuid(16)` | latest comment UUID(16 raw bytes) |
| `TableCommentTimelinePosition` | 117 | `prefix(4) \| actor_uuid(16) \| entry_uuid(16)` | `reverse_ms(8) \| latest_comment_uuid(16)` |

`TableGroupAdmin = 114` 已由 `group.md` 使用，互动表不得占用。表号与编码已经同时登记在
`model/types.go`、`docs/database_design.md` 和根 `AGENTS.md`。

`reverse_ms = ^uint64(created_at.UnixMilli())`，使 Pebble 前向扫描直接得到最新互动。运行时
以 RFC3339Nano 保存截断到毫秒的服务端时间，使权威 Date 能精确重建排序 key；历史秒精度
Date 继续兼容。同一 actor、Entry 若恰在同一毫秒产生多条 Comment，以 Comment UUID 字典序
较大者作为折叠后的确定性最新项，运行时、rebuild 与 audit 使用同一规则。Entry UUID
和 Comment UUID 都使用 raw 16 bytes，禁止写 UUID/hex 字符串。cursor 只编码表内位置后缀，
服务端按当前 actor UUID 重建完整 seek key，不信任 cursor 携带身份。

Like 的 `(actor, entry)` 在权威表天然唯一；timeline key 使用 Like 的服务端创建时间。
CommentTimeline 是按 `(actor, entry)` 折叠的最新位置，value 指向权威 Comment 的 raw UUID；
读路径由 key 中的 Entry UUID 和 value 中的 Comment UUID 直接读取 Comment，不复制正文。
CommentTimelinePosition 只用于 O(1) 找到需要删除或替换的旧排序 key，不是第二份内容真相。

## 写入一致性

新增索引后，互动权威行和对应 timeline 行必须同生共死：

- 新 Like：`Like + LikeTimeline` 在同一 Pebble batch 提交；已有 Like 的幂等命中不新增索引；
- Unlike：按权威 Like 的 Date 算出确定性 timeline key，在同一 batch 删除两行；
- 新 Comment：点查 `(actor, entry)` position，删除旧 timeline key，再把新 `Comment +
  CommentTimeline + CommentTimelinePosition` 在同一 batch 提交；
- 编辑 Comment：只更新 Comment value，不移动 timeline；
- 删除非最新 Comment：只删除权威 Comment；排序位置不变；
- 删除最新 Comment：删除权威 Comment、timeline 和 position；同时流式扫描该 Entry 的
  Comment 前缀，排除正在删除的 Comment，只保留该 actor 最新的一个候选。若存在候选，在同一
  batch 写回 timeline 与 position；不存在则保持两张派生表无记录。

任何 timeline fanout 均在上述原子提交之后，保持既有 activity timeline 错误边界。索引写失败
不得留下“权威互动已成功但用户互动页永久缺行”的中间状态。

新增 Comment 不扫描历史。只有删除当前最新 Comment 才扫描该 Entry 的 Comment 前缀，扫描中
只保留一个最佳候选，内存恒定，不收集 Comment slice/map。所有互动写入都必须通过同一
`ApplyBatch` 串行边界，使读取旧位置、删除旧索引和写入新位置之间没有并发窗口。server 层
仍必须在既有 `entryLifecycleMu.RLock` 内完成整个互动 mutation；`ApplyBatch` 只解决互动之间
的串行化，不能替代该锁与 `PostEntry/DeleteEntry` 的互斥契约。

历史 Like/Comment 可能缺少有效 `From.Uuid` 或 Date：运行时新写入必须具备两者；迁移时无法
可靠解析 actor 的记录只报告并跳过，禁止按可回收的 `From.Id` 认领。缺 Date 的记录同样报告，
不伪造排序时间。

## API 与分页

不要给既有 `FetchFeed` 增加含糊 mode。新增兼容 RPC：

```proto
enum InteractionKind {
  INTERACTION_KIND_UNSPECIFIED = 0;
  INTERACTION_KIND_LIKE = 1;
  INTERACTION_KIND_COMMENT = 2;
}

message InteractionFeedRequest {
  string profile_uuid = 1;
  InteractionKind kind = 2;
  string cursor = 3;
  int32 page_size = 4;
  string viewer_uuid = 5; // loopback 过渡字段，沿用当前可信边界
}

message InteractionItem {
  Entry entry = 1;
  Like like = 2;
  Comment latest_comment = 3;
}

message InteractionFeedResponse {
  Profile profile = 1;
  repeated InteractionItem items = 2;
  string next_cursor = 3;
}

rpc FetchInteractionFeed(InteractionFeedRequest)
    returns (InteractionFeedResponse);
```

每项只能设置 `like` 或 `latest_comment` 之一，并且必须与请求 kind 相符。CommentTimeline
不是 Comment 历史明细 API；需要完整历史时仍从权威 Comment 表按 Entry 查询。服务端不接受
客户端提交 actor、索引时间或 Entry UUID 来构造结果。
2.0 RPC 仅在 `viewer_uuid` 和 `profile_uuid` 都是有效非零 UUID 且完全相等时返回数据；
缺失 viewer 不能降级为匿名访问。loopback 边界取消前应把 viewer 替换为可信 principal。

分页规则：

- 默认 30，最大 100；读取至多 `page_size + 1` 个可见项目判断是否还有下一页；正常满页时
  `next_cursor` 锚定最后一个已返回项目，预读的第 31 项不能被跳过；
- cursor 使用现有 URL-safe opaque 编码约定，只提供 next，不提供 prev；
- 遇到孤儿或损坏项时继续向后扫描，但单次请求设置明确扫描预算（建议
  `max(page_size*10, 300)`），防止异常数据导致无界读取。若尚未填满一页就耗尽预算，
  `next_cursor` 才锚定最后扫描的索引位置，使下一请求能越过过滤区；
- 翻页期间 Unlike/DeleteComment 会删除锚点，seek 使用严格大于 cursor 位置，行为与现有
  cursor Feed 一致，不要求快照隔离。

Web handler 只负责按既有 Feed 规则解析 ID/旧 ID redirect、解析 cursor、传递当前 viewer 和
渲染；授权、过滤及索引读取必须在 ffdb 完成，不能让 httpd 先取得私有数据再过滤。

## Rebuild、audit 与容量

新增维护命令：

```text
rebuild_interaction_timelines [-user <profile-uuid>] [-dry-run]
```

- 全量模式分别流式扫描 Like、Comment；重建 Comment 时使用固定上限（例如 500）的
  `(actor, entry) -> latest Comment` 工作 map，与已提交 position 比较后批量替换排序 key 和
  position；每批提交即清空，禁止按全表或完整 Entry 聚合；
- `-user` 仍需扫描权威表，但只为指定 actor 生成索引，适合上线前小范围验证；
- apply 使用小 batch 增量写入，重建前用对应派生表的 range delete 清理目标范围；
- dry-run 输出 likes/comments、indexed_likes/indexed_comments、unresolved_actor 和
  missing_date。孤儿 Entry、非法派生行和重复/不成对索引统一由 `audit_store` 的
  interaction_orphans/interaction_mismatches 报告，rebuild 不重复维护第二套诊断口径；
- 命令可重复执行，结果确定。

`audit_store` 增加双向检查：权威互动缺 timeline、timeline/position 不成对、二者位置或最新
Comment UUID 不一致、派生行缺权威互动、actor/date/key 不匹配、孤儿 Entry。检查必须多遍
流式、内存有界。三张新表可由 rebuild 修复，不能反向创造 Like/Comment。

容量为每个 `(actor, liked Entry)` 一行，加每个 `(actor, commented Entry)` 一组 timeline 与
position；多条 Comment 不会线性放大派生表。不做每 viewer fanout，也不设置 Home timeline
式 active/inactive 缓存。权威 Like/Comment 不因 Timeline 折叠或重建而改变。

## 已完成的实施与验收顺序

1. 固定 protobuf、表号和 key 编解码，测试 raw UUID、排序、cursor 不能跨 actor；
2. 将 Like/Unlike、Comment create/edit/delete 改为权威行与索引原子 batch，覆盖失败回滚；
3. 实现 model 查询、owner-only 双层授权、孤儿懒删和新增 RPC；
4. 实现两个 Web 页面、导航和目标互动高亮；
5. 实现 rebuild/audit，先针对单用户 dry-run 与 apply，再全量构建；
6. 部署顺序为停服升级二进制、全量 rebuild、audit 验收、恢复服务。新写路径与新页面必须同版
   上线，不能先开放读取半构建索引。

最低回归测试包括：rename 后按 UUID 仍可查询；Unlike 删除索引；Comment 编辑不移动；同一
Entry 多 Comment 只产生一个 timeline 项但卡片展示全部评论；删除最新 Comment 后正确回退到次新，删除最后一条后移除
索引；非本人请求稳定返回 403 且不泄露互动；删除 Entry 的孤儿不
返回；cursor 遇删除锚点和大量过滤项可继续；legacy 缺 UUID/Date 的 rebuild 安全跳过；全量
迁移内存有界。
