# 后续高价值工作

本清单只记录已经确认值得推进的产品与运维改进。按顺序执行；每项独立提交、独立验收，
不得借机调整既有数据库、RPC 或持久化契约。

## 1. Service 来源失败通知（已完成：`44c003f`）

闭合 Service → Task → Notification → UI 链路，让绑定者及时知道长期失效的外部来源。

- 增加 `FEED_SERVICE_FAILED` notification：个人 Feed 通知本人，Group 通知当前 admins。
- 只在来源进入长期 `dead`（或明确需要人工处理的终态）时产生，不把短暂 degraded 当作通知。
- 同一故障周期只通知一次；Task retry、进程重启和多个 FeedService binding 不得制造重复通知。
- 通知不保存 URL query、响应正文或凭据，只包含安全摘要并链接到对应 Import Services 页面。
- 手动 Refresh 成功继续按现有规则恢复 active；不要求删除历史失败通知。
- 覆盖个人 Feed、Group 多 admin、幂等重试、恢复后再次进入新故障周期等测试。

验收：来源长期失效时相关用户各收到一条可导航通知，短暂故障和重复任务不产生通知风暴。

## 2. Public Group 发现页（已完成）

完整实现契约与验收矩阵见 [`docs/group_discovery.md`](docs/group_discovery.md)。

- 新增公开 `/groups`，现有用户 Group 列表迁到 `/feed/:id/groups`。
- 使用 `TableGroupIndex = 119` 按最近创建、发帖和互动活动排序，避免每次扫描 Profile
  筛选 Group。
- 列表复用现有用户 Group 页面样式，只展示 metadata；关系状态与操作统一进入 Group Feed 处理。
- 分页和 rebuild 必须流式有界；请求路径不扫描 Profile、Entry、Follow 或 Group 关系。

验收：新用户可以发现并加入活跃 Public Group，Private Group 不泄露内容。

## 3. RSS/Atom 导入体验（暂缓）

当前导入能力满足实际需求，本轮不优化交互，V1 明确不支持 OPML。未来重新启动本项时，
仍须复用现有 Service 身份、SSRF 防护、响应大小和超时边界，不得另建旁路抓取链路。

## 4. Notification 实时 badge（已完成）

- 复用现有 Realtime SSE transport，只发送“notification summary 已变化”的 hint。
- Feed React 复用 Home 已有 SSE；收到通知 hint 后只标记 badge，不发起额外 summary 请求。
- SSE 不携带通知正文或未读计数；精确计数继续由普通 SSR 页面初始化。
- 隐藏页关闭连接，恢复可见时重连；慢连接和断线不影响领域 mutation。

验收：在线 Feed 页面无需整页刷新即可看到新通知标记；下次普通页面加载恢复精确计数。

## 5. 可见性与权限收口

- 将 Feed、Entry permalink、Search、Home 和 Interaction feed 的 viewer-aware 判断收敛到
  ffdb 的单一权限语义，避免各路径独立猜测 private 规则。
- 用权限矩阵测试覆盖 owner、member/follower、无关系用户、匿名用户和已删除对象。
- 修正 `docs/feed.md` 等与当前 permalink/private 实现不符的描述。
- 完成本项前不开放其他用户的 `/feed/:name/likes`、`comments`。

验收：同一 Entry 经所有入口得到一致的允许/拒绝结果，私有内容无旁路。

## 6. 后台系统状态摘要

提供 loopback/CLI 可读取的有界诊断摘要，不引入外部监控框架：

- Task ready/inflight/dead 数及最老 ready age；
- Service active/degraded/dead 数；
- Realtime subscriber/drop 数；
- Timeline maintenance 失败、耗时和 backlog；
- Notification trim backlog。

不得输出 task payload、URL query、token、正文或其他敏感数据，不得用全表加载换取统计。

## 暂不推进

- Pebble/store 重写或持久化 key 调整；
- WebSocket 替换 SSE；
- 通用消息中间件、多进程 worker 或 celebrity fanout；
- mention、comment thread subscriber、email/Web Push；
- 纯 CSR 化和主题系统扩张。
