# Home Timeline 容量治理 TODO

## 背景与固定决策

生产审计发现约 773 万条 Entry 对应 2,858 万条 TimelineIndex 和同量
TimelinePosition。当前问题不是 108/109 的编码，而是为全部历史 follower 永久物化完整
Home：派生表无生命周期，inactive 用户也持续接收 fanout。

本轮固定以下边界：

- 保留 TimelineIndex 108、TimelinePosition 109 及现有 key/value 编码；
- Home 仍按 activity 排序，Like/Comment bump 语义不变；
- TimelineIndex/Position 是可丢弃、可重建的派生缓存；
- 每个 viewer 最多保留 `10,000` 条；
- 保留独立的可调整时间窗口，当前设为 `MAX`（无限制），未来允许改为 90 天或其他窗口；
- 历史 Entry 可以进入 Home，不能因数据较老而被全部排除；
- 暂不新增全局 interaction activity index；有界候选外的长尾互动 bump 不保证在重建时恢复；
- OAuth 身份不等于活跃状态；
- 不改 Profile feed、Public 或 Search；
- 所有全表操作必须流式或固定 batch，禁止按全库行数分配 map/slice。

## 1. TimelineState 基础表

- [ ] 新增 `TableTimelineState = 110`，不改已有表号；
- [ ] 编码保持最小：

  ```text
  key   = table prefix(4) | viewer UUID(16)
  value = last_access UnixMillis(8), big-endian
  ```

- [ ] 增加内部方法：读取状态、touch、判断是否仍活跃、删除状态；
- [ ] 固定活跃期限常量，第一版建议 30 天，不引入配置系统；
- [ ] 定义 timeline 内容保留窗口；当前值为 MAX，语义是没有日期 cutoff，但重建与裁剪接口
  必须接收并执行该选项，未来可直接调整为 90 天等时长；
- [ ] 拒绝 nil、零 UUID、非法时间及错误长度，不 panic；
- [ ] 单测覆盖编码、过期边界、重复 touch 和损坏数据；
- [ ] 同步 `docs/database_design.md`、`docs/timeline.md` 与 AGENTS 的持久化契约。

本阶段只提供状态能力，不接 Home、fanout 或迁移。

## 2. 单用户有界重建

- [ ] 重构 `rebuildTimelineForProfile`，只生成时间窗口内、activity 排序最靠前的 10,000 个
  唯一 Entry；
- [ ] 当前时间窗口为 MAX，不产生日期 cutoff，允许历史关注内容进入结果；测试同时覆盖有限
  窗口（例如 90 天），确保选项不是只存在于签名中的死参数；
- [ ] 不能先把所有关注 feed 的完整历史装入 map 再截断；内存上限应与 10,000 条及固定 batch
  成正比，而不是与源 Entry 总数成正比；
- [ ] 逐个前向扫描各 direct EntryIndex 的最新端，用 10,000 条最小堆保留满足窗口的全局
  最新唯一 publish 候选；同一时间只打开一个 iterator，不使用 Pebble 反向迭代；
- [ ] 在候选集合内正确合并 publish、当前 Like、当前 Comment 的 activity，并保留现有
  bump/cooldown 规则；候选外历史 Entry 的近期互动允许不进入重建结果；
- [ ] 用固定 batch 原子替换该 viewer 的 TimelineIndex/Position；
- [ ] 旧行超过 10,000 条或落在所选时间窗口之外时，两张表成对删除；当前 MAX 窗口只触发
  条数裁剪；
- [ ] `-user`、`-max-limit`、`-dry-run` 继续可用于小范围验证；
- [ ] `-user` 成功后创建或刷新 TimelineState；默认模式只重建已有有效 State 的 viewer，
  不再用 OAuth 集合选择用户；
- [ ] 测试覆盖：不足 10,000、超过 10,000、全是历史数据、多 feed 去重、互动 bump 和幂等重建。

先完成可独立调用的单用户重建，不在请求 handler 中直接拼装迁移逻辑。

## 3. Home 按需初始化与刷新活跃状态

- [ ] Home 请求读取 TimelineState；
- [ ] 状态不存在或过期时，先执行单用户有界重建，再写入新的 TimelineState；
- [ ] 状态有效时 touch `last_access`，直接读取现有 Home；
- [ ] 初始化失败不得留下“已活跃但缓存不完整”的状态；
- [ ] 并发首次访问同一 viewer 必须串行或合并，不能并发全量重建；
- [ ] 保持 cursor、旧 `Start/PageSize` 链接和未登录页面行为；
- [ ] 明确首次访问可能较慢，测试锁定失败重试和并发行为。

如果同步重建实测超过 HTTP 超时，再单独设计 job；第一版不预先引入异步状态机。

## 4. Fanout 仅维护活跃 viewer

- [ ] `FanoutTimelineActivity` 只更新 TimelineState 有效的作者和 followers；
- [ ] 不得用 TimelinePosition 是否存在判断活跃，否则新 Entry 无法首次进入 Home；
- [ ] 源 Entry/Like/Comment 继续先提交，timeline fanout 仍在源 batch 外；
- [ ] inactive viewer 不创建 TimelineIndex/Position；其下次访问由按需重建补齐；
- [ ] 第一版不在每次 fanout 后扫描 10,001 行；在重建与 Home 首页访问时收敛到 10,000 条，
  fanout 期间允许短暂超出；
- [ ] 测试覆盖 active/inactive follower、新 Entry 首次插入、Like/Comment bump 和 fanout 失败。

活跃状态本身有过期期限，Home 首页和维护命令负责收敛，不能重新形成永久无限增长。

## 5. 现有数据清理工具

- [ ] 新增独立命令 `compact_timelines`，默认 dry-run；
- [ ] compact 只裁剪现有 108/109 行，不读取 Follow/Entry/Like/Comment、不重算 activity，
  不承担修复缺失内容的职责；
- [ ] 支持 `-user` 和小规模限制，先在 dev/单用户验证；
- [ ] 有效 TimelineState：裁剪到时间窗口内最新 10,000 条；当前 MAX 窗口不按日期删除；
- [ ] 无状态或已过期 viewer：删除该 viewer 的全部 TimelineIndex/Position；
- [ ] 整个 viewer 的清理优先使用两个 prefix `DeleteRange`，不要逐行保存待删除 key；
- [ ] dry-run 输出 viewer、Index/Position 现有量和预计删除量，但保持内存有界；
- [ ] apply 使用固定 batch，失败可安全重跑；
- [ ] `audit_store` 增加 TimelineState 与 10,000 条上限检查，但不得恢复全量内存集合或海量
  双向随机查验。

## 6. 部署与验收

- [ ] 先部署 TimelineState、按需重建和受限 fanout，不立即删除现有 timeline；
- [ ] 观察真实 Home 访问，让活跃用户建立 TimelineState；
- [ ] 对已知用户执行单用户 dry-run/apply，核对历史内容、排序、互动 bump 和分页；
- [ ] 运行 `compact_timelines` dry-run，确认 inactive/active 数量及预计回收规模；
- [ ] apply 清理后运行 `audit_store`；
- [ ] 记录清理前后 108/109 行数、数据库大小、Home 首次访问耗时和稳定访问耗时；
- [ ] 更新迁移手册，明确新二进制、初始化、观察期、compact、audit 的执行顺序；
- [ ] 写明回滚路径：旧二进制可恢复全 follower fanout，但被跳过的派生行不会自动出现，需对
  目标用户重建；
- [ ] 全部稳定后删除本 TODO，将最终契约沉淀到正式文档。

## 暂不采用

- 当前默认启用 90 天等有限日期窗口：历史数据占主体，会让低活跃用户 Home 为空；时间窗口
  能力仍保留，待实际数据与产品需要明确后调整；
- 完全读时合并所有关注 feed：互动排序和 cursor 更复杂，读取成本随关注数增长；
- 只删除 TimelinePosition：会使 bump 从点查退化为扫描；
- 只压缩 TimelineIndex key：不能改变派生行无限增长；
- 用 OAuth 行或现有 TimelinePosition 推断活跃用户；
- 为第一版引入复杂异步 job、版本状态机或可配置策略。
