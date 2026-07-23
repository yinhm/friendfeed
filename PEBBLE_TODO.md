# Pebble v2 升级 TODO

## 决策

本仓库仅供内部部署，采用一次性、向前迁移方案：

- `v1.0` 是旧数据库迁移工具的最终基线。
- 升级 Pebble 前，必须先用 `v1.0` 把所有 `old_db` 迁移为当前 `new_db`。
- Pebble v2 只需打开迁移完成的 `new_db`，不再支持直接读取 `old_db`。
- 升级后不支持降级到 Pebble v1，也不保证旧二进制能够打开升级后的数据库。
- 不为第三方调用、旧数据库或跨版本回滚保留兼容代码。

目标版本：

- 当前：`github.com/cockroachdb/pebble v1.1.5`
- 目标：`github.com/cockroachdb/pebble/v2 v2.1.6`

Pebble v2 最低支持 FMV 13。当前 Pebble v1.1.5 的官方工具会把数据库直接升级到该版本支持的最新格式（FMV 16）；Pebble v2 可以打开 FMV 16。本项目允许直接使用这一激进路径，不要求停留在 FMV 13。

## 升级顺序

顺序不可颠倒：

1. 停止写入。
2. 用仓库 `v1.0` 工具完成全部 `old_db -> new_db` 迁移。
3. 验证 `new_db` 的用户、feed、entry、OAuth、社交图和媒体数据。
4. 备份最终的 `new_db`。
5. 用 Pebble v1.1.5 官方工具升级所有实际使用的 Pebble 数据目录。
6. 切换代码和依赖到 Pebble v2。
7. 完成全量测试及真实数据副本冷启动。
8. 部署并恢复写入。

第 5 步完成后，不再运行依赖 Pebble v1 的迁移工具。以后若还发现未迁移的 `old_db`，应使用独立的 `v1.0` 环境先迁移，再把产物按当前版本要求重新升级。

## TODO

### 1. 完成旧数据库迁移

- [ ] 冻结并记录所有待迁移的 `old_db`。
- [ ] 使用 `v1.0` 迁移工具生成最终 `new_db`。
- [ ] 确认生产配置、systemd 服务和维护命令都只引用 `new_db`。
- [ ] 验证迁移结果，不再保留运行时读取 `old_db` 的要求。

### 2. 直接升级数据库格式

- [ ] 停止 `ffdb`、`ffweb` 以及所有可能写入数据库的维护任务。
- [ ] 备份最终 `new_db`；备份用于灾难恢复，不作为支持降级的承诺。
- [ ] 盘点 `new_db` 下所有实际 Pebble 数据目录，包括仍在使用的独立 `meta` 目录。
- [ ] 对每个目录执行：

```bash
go run github.com/cockroachdb/pebble/cmd/pebble@v1.1.5 db upgrade <db-dir>
```

- [ ] 等待工具成功退出，不得中断格式升级。
- [ ] 用 Pebble v1.1.5 做一次只读校验后，立即进入 v2 升级，不重新开放生产写入。

该命令会升级到 Pebble v1.1.5 的 `FormatNewest`（FMV 16），不是 FMV 13。这是本方案的预期行为。

### 3. 切换 Pebble v2

- [x] 将依赖切换为 `github.com/cockroachdb/pebble/v2 v2.1.6`。
- [x] 替换以下文件中的 Pebble import：
  - `store/store.go`
  - `store/iterator.go`
  - `store/store_test.go`
  - `server/command.go`
- [x] 更新 `go.mod` 和 `go.sum`。
- [x] 调整 `store/store.go` 的 v2 Options：
  - `MaxConcurrentCompactions` 改为 `CompactionConcurrencyRange`（取 `(1, 3)`，等价 v1 的动态扩缩 + 上限 3）。
  - `Levels` 改为固定长度数组。
  - `TargetFileSize` 改为 `Options.TargetFileSizes`。
  - `EnsureDefaults` 改为 `EnsureL0Defaults` / `EnsureL1PlusDefaults`。
- [x] 保持 `Storage` 接口、key 编码、迭代顺序和同步写入语义不变。
- [x] 删除仅为 Pebble v1 或 FMV 13 过渡而增加的临时代码（无此类代码，无需删除）。

### 4. 验证与部署

- [x] 执行完整 Go 门禁：

```bash
go build ./...
go vet ./...
go test ./...
```

- [ ] 使用生产数据副本启动实际 `ffdb`，验证读写、重启、备份和索引重建。
- [ ] 检查 feed、profile、OAuth metadata、社交图、timeline 和 PublicFeed。
- [ ] 记录升级耗时、额外磁盘占用和最终 FMV。
- [ ] 部署 Pebble v2 版本并恢复写入。
- [ ] 删除升级期间的临时副本；按既定保留策略保存升级前备份。

## 风险

- FMV 只能向前升级，不能降级。
- 本项目明确不支持升级后回退到 Pebble v1。
- 数据格式升级可能触发阻塞迁移和 compaction，必须停写并预留维护窗口及额外磁盘空间。
- `CompactionConcurrencyRange` 应固定为与现有上限等价的范围，避免升级时顺带改变资源使用策略。
- 唯一受支持的恢复方式是恢复完整备份并重新执行既定迁移流程，不允许混用新旧二进制和数据目录。

## 完成条件

以下条件全部满足后删除本文件：

- 所有生产数据均已由 `old_db` 迁移到 `new_db`。
- 所有实际数据库目录均已升级并由 Pebble v2 成功读写。
- 完整测试、真实数据副本演练和生产部署均通过。
- 运行环境中不再依赖 Pebble v1 或直接读取 `old_db`。
