# 2.0 发布说明与检查清单

## 发布判断

2.0 可以发布。当前功能已经形成稳定闭环，剩余事项主要是明确延期的产品/架构选择，不影响
单机小社区的核心使用。发布前冻结 schema、protobuf 和用户可见功能，只接受文档、测试、数据
迁移与明确的 release blocker 修复。

## 与 1.0 的边界

- 2.0 只支持 Pebble v2 和 new DB；不读取 old DB、Pebble v1，也不支持降级写入。
- `v1.0.0` 是旧数据库迁移工具基线。旧部署必须先用该 tag 完成 old DB → new DB，再按
  [db_migration.md](db_migration.md) 执行当前迁移、rebuild 和 audit。
- 2.0 将 Profile/OAuth、Entry、Like/Comment、Follow/Follower 视为主要权威数据；timeline、
  interaction timeline、GroupIndex、NotificationInbox 和 Bleve 等派生结构可以重建。
- 2.0 保留 legacy Twitter FeedJob、ArchiveFeed/ForceArchiveFeed 和受保护 API；发布不等于
  未经退役方案删除兼容入口。

## 2.0 完成范围

- Pebble v2 单库数据模型、raw UUID Entry/EntryIndex 和有界流式迁移；
- 活动排序 Home/Public timeline、热/冷缓存、cursor 与重建/audit；
- 独立 Like/Comment 权威表及用户互动 timeline；
- Profile rename、OAuth 稳定身份与首次登录引导；
- Group 创建、成员/admin、private request、投稿审核、发现和导航；
- RSS/Atom/JSON Feed Service、来源生命周期和 Task Queue；
- Notification 与 realtime dirty hint；
- Feed、permalink、Home、Public、Search、Interaction 和 mutation 的统一可见性；
- R2/本地媒体、Bleve search、健康检查、备份与 systemd/journald 运维。

## 已知但不阻塞发布

以下事项不是 2.0 未完成实现，决策入口统一在 [open_decisions.md](open_decisions.md)：

- legacy Twitter 抓取模型与旧 Job API 的退役；
- `deploy_client` 的去留；
- SSR/CSR 最终边界、后台状态摘要、prefix bloom 和 durable fanout outbox；
- Python 工具的完整传递依赖锁定。

产品层明确不在 2.0 承诺：原 FriendFeed 公共 API 兼容、全部历史 Service provider、OPML、
多机部署、邮件/Web Push、实时正文推送和主动 Group 邀请。

## 发布前检查

1. 工作区干净，所有发布提交已进入 `master`；
2. 前端依赖 frozen install，通过 lint、typecheck、unit、build 和必要的 E2E；
3. Go build、vet、全量 test 通过；
4. 在 production 一致性副本执行 `audit_store`，保存输出；
5. 按迁移手册确认必需的一次性迁移、interaction/group/timeline/search rebuild 已完成；
6. 验证 ffdb 只监听 loopback，ffweb/nginx、SSE、OAuth callback、R2 和 journald 正常；
7. 创建并转移一份可恢复的 Pebble snapshot；
8. 记录当前 commit，创建 annotated `v2.0.0` tag，并发布对应二进制/部署说明；
9. 发布后执行登录、Home、发帖、Like/Comment、private request、Group、RSS、Notification、
   Search 和重启恢复 smoke test。

## 回滚

应用二进制可以回滚，但数据库不能假设可降级。任何涉及 schema/格式迁移的生产发布，回滚单位
必须是“旧二进制 + 发布前完整数据库备份”；不得让旧程序直接打开已经由 2.0 写入的数据库。
