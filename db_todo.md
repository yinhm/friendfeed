# Store / 数据库改进计划

## 结论与边界

当前的 Pebble 单机 KV 架构与项目规模匹配，继续保留：

- 单个 Pebble 数据库及 4 字节大端表前缀；
- protobuf value 和 UUID 实体身份；
- index value 只保存 Entry key；
- Entry 与 author/feed 直接索引原子提交；
- 无上限 timeline fanout 不进入同一个大 batch；
- mdb/rdb 继续作为同一 Store 的职责别名；
- public FeedIndex、timeline 和 Bleve 明确视为可重建派生数据。

本计划用于重整 store，而不是在旧抽象旁继续堆兼容层。已明确允许直接修改当前
store API 及其仓库内调用方；若旧 API 的语义不合理，应替换或删除，不以“兼容新增”
作为默认方案。该授权不自动扩大到 protobuf、Storage、graph 或持久化 key/schema；
持久化格式变化仍须作为独立迁移实施。

## 数据分层

### 源数据

- Profile、OAuth identity；
- Entry；
- Follow 关系；
- UserMap、UserRenameMap；
- 当前仍内嵌在 Entry 中的 Comment/Like。

### 必须原子维护的直接索引

- author entry index；
- target feed entry index；
- Follow/Follower 双向边。

### 可重建派生数据

- follower timeline；
- public FeedIndex；
- Bleve search index。

## 阶段 A：修正 store 基础语义（零 schema 风险）

按顺序逐项实施，每项独立提交。

- [x] 用 characterization test 验证同一 feed、同一秒、不同 Entry UUID 会发生 EntryIndex 覆盖。
- [x] `Store.ForwardScan` 在循环结束后返回 `iter.Error()`。
- [x] `Table.Keys/Iter/IterValue/Find` 在循环结束后返回 iterator 错误。
- [x] `Store.Get` 以明确 `ErrNotFound` 区分缺失、合法空 value 和读取失败；仓库内调用方已迁移。
- [x] 存在性查询改为 `Exists(key) (bool, error)`，底层读取错误不再伪装成“不存在”。
- [x] iterator 创建直接返回 error，移除库内 panic；全部调用方已迁移。
- [x] `Flush` 直接返回底层错误并调整调用方。
- [x] 扫描实现均关闭 iterator 并检查最终错误；未为形式复用引入新抽象。
- [x] `GraphFollow` 使用一个 `ApplyBatch` 同时更新 Follow/Follower。
- [x] `DeleteEntry` 原子删除 Entry、author direct index、target feed direct index；timeline 清理保持独立。
- [x] 新增内部 `putEntryRecord`，只更新 Entry value。
- [x] Like、Unlike、Comment、DeleteComment 改用 `putEntryRecord`。
- [x] `ApiServer` 以简单串行锁覆盖 Entry 写入、互动、编辑和删除，避免 RMW 丢更新。

## 阶段 B：数据审计

- [x] `audit_store` 对比源 Entry、direct EntryIndex 和 timeline index。
- [x] `audit_store` 统计同 feed 同秒 Entry 数及潜在覆盖量。
- [x] `audit_store` 审计 Follow/Follower 是否完全对称。
- [x] `audit_store` 统计 orphan index 和缺失 timeline index。
- [x] 记录发帖 fanout 数量与耗时，不记录正文或敏感信息。
- [x] 复现测试确认同秒覆盖，已获授权进入 EntryIndex 修正。

## 阶段 A 未决项

以下事项方向可讨论，但没有测量或完整契约前不直接实施：

- [x] `Get` 采用明确的 `ErrNotFound`；不增加第三个布尔返回值。
- [ ] 扫描 callback 是否改为借用 iterator buffer。这样可消除当前每条 key/value 的两次复制，但传入切片只能在 callback 返回前有效，属于明确的 API 契约变化。先 benchmark 当前 feed、迁移和 rebuild 路径；确认分配有实际占比后再决定。若采用，直接重定 `ForwardScan` 契约，不同时维护 safe/unsafe 两套公共 API。
- [ ] feed 的“索引扫描 + N 次 Entry Get”是否是主要瓶颈。先分别测量冷/热 block cache 下的 iterator、Get、protobuf unmarshal、profile hydration 和格式化耗时；没有数据前不把 Entry 本体或摘要冗余进 fanout index。
- [ ] 是否配置 `Comparer.Split` 并使用 `SeekPrefixGE`/prefix bloom。当前 keyspace 含多种表布局，Split 必须覆盖所有格式；旧 SSTable filter 也需要重写/全量 compaction 验证。它属于数据库格式升级，不是可顺手开启的 iterator 优化。

## 阶段 C：EntryIndex 修正（需审计证据和单独授权）

Flake ID 是现有的分布式、k-ordered 身份与排序设计，即使当前只部署单节点，也不因
“目前用不到”而替换。EntryIndex 继续使用 `UUIDFlakeKey` 和完整 128-bit flake 布局，
并继续通过反转时间使 newest → oldest 按 key 升序排列，查询使用
`First/SeekGE + Next` 前向迭代。

当前待验证的问题不是 flake 设计本身，而是 `TimeTravelReverseId` 每次新建 generator、
将时间截断到秒，并让索引去重依赖 worker/sequence 后缀。先用测试和数据审计确认是否
确有覆盖；确认后只修正反转 flake 的生成与去重逻辑，不自作主张换成
`timestamp + entry UUID` 等另一套 key。具体编码在证据齐备后另行设计。

不得在没有迁移工具和验证步骤时就地改变 `TableEntryIndex = 5`。若最终需要改变
持久化格式，应优先设计一次性离线升级，而不是长期双写两套索引；升级后不支持旧程序
读取或降级。

迁移步骤：

- [x] 最小修正保留完整反转 flake，在 key 末尾追加 canonical Entry key 作为唯一性后缀。
- [x] 选择原表离线迁移：`migrate_entry_index` 将旧 key 原子转换为新 key，不维护双轨。
- [ ] 从 Entry 源数据重建 author/feed direct index。
- [ ] 重建 timeline index。
- [ ] dry-run 对比每个 feed 的数量、顺序、首尾和重复项。
- [ ] 先针对指定用户和小 feed 验证 cursor 分页。
- [ ] 全量重建并切换读写路径。
- [ ] 完成备份、恢复和重启验证。
- [ ] 切换并验证完成后清理旧 index 或临时表，不保留永久兼容路径；升级后不支持降级。

## 阶段 D：Comment/Like 独立表

这是计划内的目标架构，不再以互动并发量或 Entry 体积作为启动前提。阶段 A 的
`putEntryRecord` 和串行锁只是迁移前的正确性修复，不能作为最终方案。

目标结构：

```text
TableLike    | entry UUID | actor UUID   -> Like
TableComment | entry UUID | comment UUID -> Comment
```

Like 以 `(entry UUID, actor UUID)` 天然幂等；Comment 以稳定 comment UUID 定位。
互动写入不再重写 Entry 本体、direct index、timeline 或 search 文档。Entry 读取时按前缀
加载互动数据，并保持当前 API 所需的稳定展示顺序。

实施步骤：

- [ ] 分配新的表前缀并固定 key/value 编码；不复用现有表号。
- [ ] 明确 Comment 的稳定排序键及同时间值的 tie-break，不能依赖 Pebble 偶然遍历顺序。
- [ ] 将 Like/Unlike、Comment/Edit/DeleteComment 改为独立表的点写或小 batch，并保留现有 UUID 授权语义。
- [ ] 修改 Entry 读取与 feed hydration，在返回 protobuf 前聚合 Like/Comment；避免聚合过程再次触发逐 actor 的无界 N+1 查询。
- [ ] 确认搜索索引只消费 Entry 正文和必要摘要，互动 mutation 不再重建搜索文档。
- [ ] 在 `migrate_db` 增加 dry-run 和 apply 迁移：从 Entry 内嵌数据写入新表，检测重复、非法 UUID、缺失 actor 和 comment UUID 冲突。
- [ ] 针对指定 Entry/用户小规模迁移并校验数量、顺序、权限、重复 like 和 rename 后身份。
- [ ] 全量迁移后重新打开数据库验证；升级采用一次性切换，不长期双写新旧存储。
- [ ] protobuf 中既有 Like/Comment 字段属于外部契约，不在本阶段删除；运行时是否继续填充由 RPC 兼容需求决定。
- [ ] 独立表稳定后移除 Entry mutation 串行锁中仅为互动 RMW 设置的部分，保留 Entry 编辑/删除真正需要的并发保护。

## 阶段 E：有实际需求后再决定

### Durable fanout outbox

当前继续同步 fanout，并保留 audit/rebuild。只有发生以下情况才设计持久化 outbox：

- follower fanout 经常超时；
- 部分 fanout 失败实际发生；
- follower 数量明显影响发帖延迟；
- 需要 celebrity feed 的读写混合 fanout。

不得为此强行复用 FeedJob 或先建立通用 job 框架。

## 代码抽象原则

值得在相关修改中逐步形成：

- 精确领域 key codec，如 `profileKey`、`followKey`、`entryIndexPrefix`；未进入 schema 迁移时输出必须与现有字节完全一致；
- 精确 mutation 函数，如 `putEntryRecord`、graph 双边更新、Entry direct delete；
- EntryIndex 确认需要迁移时再决定是否引入独立 index 类型。

当前不做：

- 通用 `Repository[T]`；
- 通用 Table codec 配置系统；
- 通用 `PutPair/DeletePair`；
- 泛型 `CollectProto[T]`；
- 为形式统一重写两套 key API；
- 仅因零引用删除仓库边界之外的 API；本轮可删除已确认无外部契约且设计不再需要的 store API，但须在对应提交说明替代语义；
- 无性能证据的统一 cache；
- 单独搬移 TimeTravel 函数等纯整理任务；
- 更换 Pebble 或引入外部数据库服务。

## 验收原则

- 每项先锁定现有行为或缺陷，再修改生产代码；
- 所有 iterator 必须关闭并检查最终错误；
- 小规模、有界 mutation 才进入同一个 Pebble batch；
- 无上限 fanout 不得扩大同步 batch；
- schema 迁移必须支持 dry-run、指定用户、小规模验证、备份和恢复；
- 本计划已获授权修改 store API；每项直接迁移仓库内调用方并删除被替代入口，避免双轨兼容层。
- model、protobuf、Storage、graph 及持久化 key/schema 仍按原契约保护；若要改变，须在对应迁移项再次明确范围。
