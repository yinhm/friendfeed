# Database schema gate and migration retirement

目标：在 v2.2 提供最后一个可审计、可盖章的迁移窗口；只有 dev 与 production 都留下迁移完成证据后，v2.3 才允许强制 schema gate 并删除一次性迁移实现。

当前进度：

- [x] 1. 无行为 flags 进入弃用期。
- [x] 2. old-DB copy 命令开库前 fail-loud。
- [x] 3. application schema marker 基础能力。
- [x] 4. `inspect_schema` / `verify_schema`。
- [x] 5. `stamp_schema`。
- [x] 6. v2.2 server 启动策略。
- [x] 7. 文档与 v2.3 人工闸门。
- [ ] 8. 最终门禁与 review。

## 不变量

- Pebble `FormatMajorVersion` 与 ffdb application schema 是两套独立版本，不得混用。
- application schema marker 固定存于 `TableMeta | "db-schema/version"`，value 为 4-byte big-endian uint32；首版为 `1`。
- 所有全库验证必须流式且内存有界，不得收集全库 key/value。
- 非空、无 marker 的现有数据库不得自动盖章；损坏或未来版本 marker 必须 fail-loud。
- v2.2 不强制普通 server 拒绝未盖章的现有数据库，否则部署工具尚未运行时会形成启动死锁。
- `stamp_schema` 不提供 `-force`；只有同一次只读验证通过后才能写 marker。
- v2.3 的删除与强制打开是人工证据闸门后的独立工作，不得在本分支提前完成。

## 1. 无行为 flags 进入弃用期

实现：

- `cli --debug` 仅在用户显式传入时向 stderr 输出一次 warning；不新增日志行为。
- `mirror_twimg --no-wayback` 仅在用户显式传入时输出一次 warning；默认和实际行为不变。
- `--wayback` 与 `--no-wayback` 同时出现仍 fail-loud。
- warning 不得包含配置、URL、token 或数据库路径。

验收：

- 未传 flag 时 stderr 无弃用提示。
- 显式传入时恰好提示一次，命令仍执行原行为。
- `--wayback --no-wayback` 仍被拒绝。
- 文档明确两者计划在 v2.3 删除。

## 2. 退役 old-DB copy 命令

实现：

- `tools -c db` 与 `tools -c sync` 在校验 `-from/-to`、确认、建目录或打开 Pebble 前立即失败。
- 错误必须指向 tag `v1.0.0`，并要求只操作离线副本。
- v2.2 暂保留命令名和私有实现，保证错误明确；v2.3 再删除实现。
- `tools -c debug` 是有效诊断命令，不随 `cli --debug` 退役。

验收：

- 两个命令即使路径不存在也不会创建任何目录。
- 即使缺少 `-from/-to`，首先得到 retired 错误而非参数错误。
- 其他命令的开库模式不变。

## 3. application schema marker 基础能力

实现：

- 在 model 定义 marker key、当前版本、读取/写入和状态分类。
- 状态至少区分 missing、current、older、future、malformed。
- 写入使用 `ApplyBatch` 和同步写配置；禁止覆盖 future/malformed marker。
- 空库识别不得使用 Profile 数量猜测；用全 keyspace iterator 判断是否存在任何 record。

验收：

- 4-byte 编码、固定 key 和 version=1 有单测锁定。
- missing/older/current/future/malformed 全覆盖。
- iterator 全部关闭，错误向上传递。
- 非空无 marker 不会被任何普通 open 隐式写入。

## 4. `inspect_schema` 与 `verify_schema`

实现：

- `inspect_schema` 只读输出 application schema、Pebble FMV 和是否可盖章，不修改数据库。
- `verify_schema` 复用 `audit_store` 的流式扫描，并补充一次性迁移的 blocker 统计。
- 输出必须稳定、可保存、每项有名称与计数；任一 blocker 非零时退出失败。
- archive 中无法映射 actor UUID 的只读 Like/Comment 快照不是 blocker。
- legacy rawBody/HTML/blockquote、Feedinfo/UserMap、保留表号不是 blocker。

schema 1 blocker：

- noncanonical Entry key 或 EntryIndex 编码；Entry key/ID 不一致；direct index missing/orphan。
- Like/Comment 权威表或互动 timeline 的 orphan/mismatch/unpaired/duplicate。
- Group Entry author、Group admin、Group index 不满足当前不变量。
- TimelineIndex/Position 不成对、孤儿、重复或时间不一致。
- Notification、Task、Service 已登记结构的 audit 错误。
- 仍使用已知旧 media URL、旧 default picture。
- retired public cache Meta 行仍存在。

Twitter OAuth 的历史字段顺序无法仅凭数据可靠自证，不使用启发式 blocker；盖章前必须保存
既有迁移执行证据和抽查结果。

验收：

- 验证全程流式、有界；百万级表不能按记录数增长内存。
- canonical fixture 返回 ready；每类 blocker 至少有一个定向测试。
- malformed record fail-loud，不得被记作 ready。
- 输出不含 OAuth 内容、正文、token 或 payload。

## 5. `stamp_schema`

实现：

- `-dry-run` 执行完整 verify，仅报告将写入 version=1，不写盘。
- apply 在同一进程内重新执行完整 verify，通过后写 marker。
- 命令加入 destructive/explicit confirmation 边界；确认发生在可写开库之前。
- current marker 幂等成功；older marker 仅在 verify 通过后升级；future/malformed 拒绝。

验收：

- verify 失败时 marker 不存在或保持原值。
- dry-run 后关闭重开仍无 marker。
- apply 后关闭重开可读 version=1，证明不依赖 `Flush()` 作为 apply 开关。
- current 重复执行不产生无意义写；future/malformed 不被覆盖。

## 6. v2.2 Store/server 策略

实现：

- model 提供只读 schema inspection API，Store 只补充 Pebble FMV 查询；保持 `NewStore`/`NewStoreReadOnly` 导出签名和 v2.2 打开行为。
- 新建空数据库在第一次业务初始化前可写 current marker；现有非空无 marker 仅记录一次明确 warning，继续运行以允许迁移。
- server 启动遇到 future/malformed marker 必须拒绝；missing/older 在 v2.2 warning 后继续。
- CLI 迁移工具必须能打开 missing/older 数据库；不能被 server gate 误伤。

验收：

- 空库初始化、非空 missing、older、current、future、malformed 分别有启动测试。
- warning 不包含路径之外的数据库内容。
- future/malformed 不进入提供 RPC 的状态。
- migration CLI 仍可修复并 stamp missing/older 数据库。

## 7. 文档与 v2.3 人工闸门

实现：

- 更新 `docs/db_migration.md`：v2.2 的 audit → verify → stamp 顺序、停服要求、备份和回滚单位。
- 更新 `docs/open_decisions.md`：flags 已进入弃用期，v2.3 删除。
- 登记 schema marker 编码到 `docs/database_design.md` 和 `AGENTS.md`。
- 明确永久保留 audit/rebuild/compact/inspect/backup、Task/Service 运维及历史读取兼容。

只有同时满足以下证据，后续 v2.3 TODO 才能解锁：

- [ ] dev `audit_store` 与 `verify_schema` 输出已保存且通过。
- [ ] dev 已 `stamp_schema`，重开后 `inspect_schema` 为 current。
- [ ] production 一致性副本验证通过。
- [ ] production 停服备份后已盖章并成功重启。
- [ ] v2.2 至少运行一个发布周期，无 schema gate 回归。
- [ ] `v2.0.0`/`v2.2.x` tag 可构建完整迁移工具。

闸门满足后，v2.3 才执行：

- 强制非空数据库必须是 current marker；missing/older/future/malformed 全部拒绝。
- 删除 `cli --debug`、`mirror_twimg --no-wayback`。
- 删除 db/sync 和一次性 migrate/backfill/fix/purge 实现及其私有 helper/test/doc。
- 永久保留 protobuf 字段、表号/key 前缀和 legacy 读取兼容。

## 8. 最终门禁

- `git diff --check`
- `go build ./...`
- `go vet ./...`
- `go test ./...`
- 本阶段无前端改动；若实现期间触及前端，补跑完整 pnpm 门禁。
- 每个阶段单独 commit，提交必须可回退。
- 完成后保留 TODO 和未勾选的 v2.3 人工证据，不宣称 v2.3 已完成，等待 review。
