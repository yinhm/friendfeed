# Service 聚合实施清单

完成后删除本文；长期约束写入 `docs/service_aggregation.md`、数据库文档和 AGENTS。

## 1. 模型与命名

- [x] `pb.Service` 表示全局来源；原绑定消息改为 `pb.FeedService`，字段号保持既有数据可读。
- [x] 表变量改为 FeedService(101)、Service(111)、ServiceState(112)、ServiceFeedIndex(113)。
- [x] 删除未发布的 Subscription protobuf、RPC、model API 和 synthetic Profile/Follow 逻辑。
- [x] 增加四表 codec、非法 UUID/key、既有 Twitter FeedService 解码测试。

## 2. FeedService 写路径与授权

- [x] Add/Remove/List FeedService RPC 使用 actor UUID 与 target Feed UUID。
- [x] user 仅本人管理；group 仅 admin 管理；普通 follower 不获得权限。
- [x] Service 去重创建、FeedService 与 ServiceFeedIndex 同批写入/删除。
- [x] OAuth 只保存在 FeedService；Service 严禁凭据。
- [x] 添加 Web Feed 时原子入队 `feed_service.seed`。

## 3. 抓取与 Task

- [x] `service.fetch` 读取 ServiceState 并投递全部有效 FeedService。
- [x] `feed_service.seed` 无条件抓取近期 item，只投递新增绑定。
- [x] 支持 RSS/Atom/JSON Feed，统一稳定 item identity 和 Entry UUID。
- [x] 使用统一 UA、SSRF/redirect/大小/超时限制、条件 GET 和长期退避。
- [x] 部分投递失败可重试；删除后的 Service/FeedService task 是 no-op。

## 4. Web UI

- [x] 账户 Import 页管理当前用户 FeedService。
- [x] Group 管理页为 admin 提供同一组件；非 admin 不展示也不能调用。
- [x] 展示 pending/最近成功/安全错误状态；支持添加、停用、删除、刷新。
- [x] 前端不抓远程 Feed，不接触 OAuth token。

## 5. 运维与收尾

- [x] audit Service↔State、FeedService↔ServiceFeedIndex、孤儿和 dormant Service。
- [x] inspect/refetch/disable 工具均有界，不绕过服务端授权写普通配置。
- [x] 更新生成代码、数据库设计、部署说明和 API 测试。
- [ ] Go、前端、e2e 门禁全绿后删除本文并提交收尾。
