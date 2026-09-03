[简体中文](README.zh-CN.md) | [English](README.md)

# ffdb

这是我为一小部分仍然喜欢 FriendFeed 的用户编写和维护的社区项目。

FriendFeed 已经关闭多年，但它把 Feed、订阅、Like、Comment、Group 和外部内容聚合放在同一条
信息流里的方式，至今仍然简洁而独特。ffdb 的目标不是复制一个停留在 2009 年的网页，也不是
建立通用社交平台；它是在保留 FriendFeed 核心交互模型的前提下，为一个小型、可信社区提供
可以长期运行和维护的现代实现。

本仓库不是 Meta/Facebook 的官方项目，也不与原 FriendFeed 服务存在关联。

## 2.0 状态

当前 `master` 已进入 **2.0 发布冻结阶段**。2.0 以当前 new DB/Pebble v2 数据模型为唯一运行
基线，不兼容 old DB、Pebble v1，也不支持用 2.0 写过的数据库降级运行。仍持有旧数据库的部署
必须先使用 `v1.0.0` 工具完成迁移，具体步骤见 [数据库迁移手册](docs/db_migration.md)。

2.0 的重点不是增加更多实验功能，而是收口已经投入实际使用的能力：稳定身份、Group、活动排序
Home、独立互动数据、RSS/Atom Service、持久化 Task、Notification、统一权限和可恢复的派生索引。

## 与原 FriendFeed 的功能对照

| 能力 | ffdb 2.0 | 与原 FriendFeed 的差异 |
| --- | --- | --- |
| 用户 Feed、关注与取消关注 | 完整 | 保留核心模型；新关注只向 Home 增量补入最近最多 100 条内容 |
| Home timeline | 完整 | Like/Comment 可有限 bump；使用有界热/冷缓存和 cursor，不追求历史页面快照 |
| Public feed | 完整 | 独立、可重建的活动 timeline；private 内容在读写两侧排除 |
| 发帖、富文本、链接、图片、YouTube | 完整 | 使用 React 19、Plate 53 和严格 URL/HTML 消毒；不恢复所有历史 embed provider |
| Like 与 Comment | 完整 | 权威数据独立存储；提供本人 `/likes`、`/comments` 历史页 |
| Group | 核心功能完整 | 支持创建、加入、退出、admin、成员管理、private 申请、投稿、Service 和发现页；暂不做主动邀请 |
| Private Feed/Group | 完整 | metadata 公开以便发现和申请，内容统一按 owner/member/follower/super 授权 |
| Profile rename | 完整 | 旧 ID 使用 soft redirect；一次 rename 记录可由管理员回收 |
| 外部 Service 聚合 | 部分完整 | 支持公开 RSS、Atom、JSON Feed；没有恢复原站 Flickr、Delicious 等完整 provider 生态 |
| Twitter/X | 登录兼容，抓取受限 | 保留旧 OAuth/归档兼容路径；X API 变化后不承诺持续同步新内容 |
| Notification | 核心功能完整 | 支持关注申请、互动、Group 角色/成员和 Service 失效通知；不做邮件、Web Push 或聚合通知 |
| 实时更新 | 有意简化 | SSE 只发送 dirty hint；Home 显示“有新动态”后读取权威第一页，不推送完整 Entry |
| Search | 完整 | 本地 Bleve 索引，返回前执行与 Feed/permalink 相同的可见性检查 |
| Public Feed API | 独立设计的 V1 | per-Feed Bearer key 支持 Feed metadata、cursor Entry 读取和 multipart 发布；不兼容原 FriendFeed API 协议 |
| 多机/大规模部署 | 非目标 | 面向单机、小社区；Pebble、ffdb、ffweb 和 nginx 构成主要运行栈 |

## 当前功能

- Google/Twitter OAuth 登录，使用 provider 的稳定 subject ID 绑定本地身份；首次登录引导用户
  选择可读 Profile ID。
- 用户 Feed、Home、Public、Search、tag、permalink，以及 cursor 分页。
- 发帖、编辑、删除、Like、Comment、富文本与历史 `rawBody` 兼容渲染。
- Public/Private Group、成员和 admin 管理、关注申请、发现页和个人 Group 活跃度导航。
- 用户及 Group 的 RSS/Atom/JSON Feed 导入；条件请求、SSRF 防护、来源迁移与失败生命周期。
- 持久化 Task Queue，包含 lease、epoch fencing、重试、dead history、audit 与运维工具。
- 持久化站内通知和 SSE dirty hint。
- 版本化 Public Feed API：per-Feed key、private/Group Feed 隔离、cursor 读取和经验证的媒体上传。
- 本地或 R2 媒体存储、历史媒体 URL 迁移和 Twitter 图片抢救工具。
- Pebble 在线一致性备份、数据库 audit、索引/timeline rebuild 和有界迁移工具。
- systemd + journald + nginx/Fabric 3 的单机部署方式。

## 架构概览

```text
browser
   │ HTTP / SSE
nginx
   │
ffweb (Gin + templates + React assets)
   │ loopback gRPC
ffdb
   ├── Pebble v2          canonical data + derived indexes
   ├── Bleve              rebuildable search index
   ├── Task workers       Service/timeline/notification maintenance
   └── local media / R2
```

ffdb 的 gRPC 只允许监听 loopback。该边界是当前请求中携带 actor/viewer UUID 的安全前提，不能
直接把端口暴露到公网。项目以单机可靠性为目标，不在 2.0 中引入 Redis、Kafka 或分布式一致性层。

## 开发环境

需要：Go 1.26（以 `go.mod` 为准）、Node.js 24（精确版本见 `.nvmrc`）、Corepack 与仓库
锁定的 pnpm。`uv` 只用于 Fabric 部署和 Python 工具。

先构建前端，再构建 Go。生产 Go 二进制会嵌入 `httpd/static/` 和 templates：

```bash
cd httpd/app
corepack enable pnpm
pnpm install --frozen-lockfile
pnpm run build

cd ../..
go build -o ffdb .
go build -o httpd/httpd ./httpd
```

准备配置：

```bash
cp conf/example.config.json conf/config.json
```

至少确认 `address` 是 loopback 地址（如 `127.0.0.1:3000`），`db_path`/`media_path` 可由运行
用户读写，ffweb 的 `-rpc` 与 `address` 一致，OAuth callback 与实际 HTTPS 域名一致。R2 未配置
时只使用本地媒体存储。

```bash
./ffdb -c conf/config.json -d
./httpd/httpd -c conf/config.json -rpc 127.0.0.1:3000 -p 8080 -s '<cookie-secret>' -d
```

前端开发可在 `httpd/app` 执行 `pnpm dev`。完整说明见
[前端 README](httpd/app/README.md)。

## 验证

```bash
cd httpd/app
pnpm lint
pnpm run typecheck
CI=true pnpm test
pnpm run build

cd ../..
go build ./...
go vet ./...
go test ./...
```

影响 Feed/编辑器完整交互时再运行 `pnpm run test:e2e`。

## 部署

部署脚本使用 Fabric 3：

```bash
uv venv .venv
uv pip install -r requirements.txt
uv run --no-project fab --list
uv run --no-project fab production bootstrap
uv run --no-project fab production deploy_env
uv run --no-project fab production deploy_config
uv run --no-project fab production deploy_nginx
uv run --no-project fab production deploy_db
uv run --no-project fab production deploy_web
```

生产服务由 systemd 管理，日志只写 stdout/stderr：

```bash
systemctl status ffdb.service ffweb.service
journalctl -f -u ffdb.service -u ffweb.service
```

nginx、TLS、SSE buffering、媒体站点和 systemd 模板位于 `conf/`。仓库部署配置包含实际环境
假设，使用前必须按自己的主机、域名、用户和目录审核，不能把 secret 提交进 Git。

## 备份与恢复

ffdb 可以在服务运行时创建 Pebble point-in-time snapshot：

```bash
cli run --t BackupDB
```

运行中 ffdb 的内存组成和后台状态可通过 loopback CLI 检查；使用方法和安全边界见
[运行时诊断](docs/runtime_diagnostics.md)。

备份会原子发布到 `/tmp/backup-YYYYMMDD-HHMMSS`。`/tmp` 会被系统清理，必须立即复制到其他
磁盘或主机。恢复时停止 ffdb，保留原数据库目录，将完整备份放到配置的 `db_path` 后再启动。
详细的 schema、迁移和 audit 操作见 [数据库迁移手册](docs/db_migration.md)。

## 文档

- [数据库设计](docs/database_design.md) / [迁移与运维](docs/db_migration.md)
- [Home 与 Public timeline](docs/timeline.md) / [Feed 与互动历史](docs/feed.md)
- [Group](docs/group.md) / [Group 发现](docs/group_discovery.md) / [Group 导航](docs/group_navigation.md)
- [Service 聚合](docs/service_aggregation.md) / [Task Queue](docs/task_queue.md)
- [Notification](docs/notifications.md) / [Realtime SSE](docs/realtime_sse.md)
- [权限](docs/perm.md) / [OAuth 身份](docs/oauth_identity.md) / [Profile rename](docs/profile_rename.md)
- [主题与前端样式](docs/theme.md) / [健康检查](docs/healthcheck.md)
- [Public Feed API V1](docs/web_api.md) / [运维手册](docs/web_api_operations.md)
- [2.0 发布说明](docs/release_2.0.md) / [未决架构事项](docs/open_decisions.md)

设计文档描述的是当前持久化和行为契约，而不是功能愿望清单。真正尚未决定的工作只记录在
`docs/open_decisions.md`。
