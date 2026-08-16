# 用户互动 Timeline 实施清单

设计基准：`docs/feed.md`。每项完成后运行相关定向测试并独立提交。

## 1. 持久化与 RPC 契约

- [ ] 固定 115/116/117 表号并同步 database_design.md、AGENTS.md。
- [ ] 实现 LikeTimeline、CommentTimeline、CommentTimelinePosition raw key 编解码及测试。
- [ ] 增量新增 InteractionKind、request/item/response 和 FetchInteractionFeed RPC。

## 2. 原子互动写路径

- [ ] Like/Unlike 同 batch 维护 LikeTimeline。
- [ ] Comment create/edit/delete 同 batch 维护折叠 timeline 与 position。
- [ ] 删除最新评论流式回退到次新；保持 entryLifecycleMu.RLock 生命周期互斥。
- [ ] 覆盖幂等、编辑不移动、删除回退和 batch 失败回滚测试。

## 3. 查询与分页

- [ ] owner-only 双重 UUID 授权，非本人 403。
- [ ] 实现 Base58 cursor、只 next、扫描预算和 actor 前缀重建。
- [ ] 返回 Entry + Like 或 latest Comment；同 Entry Comment 不重复。
- [ ] 缺失 Entry/互动/position 安全跳过并有界懒删。

## 4. Web 页面

- [ ] 注册 `/feed/:name/likes`、`/feed/:name/comments` 登录路由。
- [ ] 扩展 rename redirect，保留受控 suffix 与 query。
- [ ] 复用 Entry 展示并明确目标 Like/latest Comment。
- [ ] 只在本人导航显示 Likes/Comments，覆盖 handler/前端测试。

## 5. 运维

- [ ] 新增 `rebuild_interaction_timelines`，支持 `-user`、dry-run、流式固定工作集。
- [ ] audit_store 检查权威行、timeline、position、Entry 的双向一致性。
- [ ] 记录迁移顺序、统计字段和单用户验证命令。

## 6. 收尾

- [ ] 前端完整门禁。
- [ ] Go build/vet/test 全量门禁。
- [ ] 删除本 TODO，确保工作区干净。
