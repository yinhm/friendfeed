# Pebble v2 迁移评估与 TODO

> 评估日期：2026-07-22。所有关键结论均已实测验证（探针模块编译 `store` 包 + v1/v2 双版本 FMV 实验），非凭文档推断。

## 结论

可行，代码工作量小，但有一个不可跳过的数据格式（Format Major Version, FMV）前置步骤。建议拆成两个独立发布。

## 背景

- 当前依赖 `github.com/cockroachdb/pebble v1.1.5`，已是 v1 线最终版；v2 线为独立 module path `github.com/cockroachdb/pebble/v2`（评估时最新 v2.1.6，CockroachDB 25.3 生产使用）。
- v2 最低只支持 FMV 13（FormatFlushableIngest）；本项目 `NewStoreOptions`/`NewMetaStoreOptions` 未设置 `FormatMajorVersion`，**线上所有 DB 均为 FMV 1**（v1 默认 FormatMostCompatible）。
- 实测：v2 打开 v1 默认创建的 DB 直接拒绝：`database was written in format major version 1, which is no longer supported`。
- 实测验证的升级路径：先用 v1.1.5 ratchet 到 FMV 13，v2 即可正常打开并读写（实验数据完整读回）。
- 参考：[pebble README 格式版本表](https://github.com/cockroachdb/pebble#format-major-versions)。

## 代码改动面（实测编译 + 测试通过）

把 `store/` 包源码复制到探针模块、仅替换 import 为 `pebble/v2` 后，编译错误仅 4 处，全部集中在 `store/store.go` 的 Options 构造：

| v1 | v2 | 位置 |
|---|---|---|
| `Options.MaxConcurrentCompactions func() int` | `Options.CompactionConcurrencyRange func() (lower, upper int)`（语义从"上限"变为"区间"） | `store/store.go:80` |
| `Levels` 为切片 `[]LevelOptions` | 固定数组 `[7]LevelOptions` | `store/store.go:105` |
| `LevelOptions.TargetFileSize` | 移到 `Options.TargetFileSizes [7]int64` | `store/store.go:113` |
| `LevelOptions.EnsureDefaults()` | 拆分为 `EnsureL0Defaults()` / `EnsureL1PlusDefaults(prev)` | `store/store.go:115` |

其余全部 API 零改动兼容（已实测）：`NewIter` 仍返回 `(*Iterator, error)`、`Get/Set/Delete/NewBatch/DeleteRange/Flush/Metrics/Close`、`WriteOptions/Sync/NoSync`、`NewCache`、`IterOptions` 边界、`bloom.FilterPolicy`、`FilterType`（v2 中已标记 legacy 但保留）。

修完上述 4 处后，**store 包全部测试在 v2 下原样通过**。受影响文件仅 `store/store.go`、`store/store_test.go`；`store/iterator.go`、`server/command.go` 无需改动。

## TODO（两步走，不能合并）

### 第一步：FMV ratchet（v1 时代，独立发布）

- [ ] `NewStoreOptions`/`NewMetaStoreOptions` 增加 `FormatMajorVersion: pebble.FormatFlushableIngest`（一行改动）。
- [ ] 升级前备份数据目录（FMV 升级**不可逆**，回滚只能靠备份）。
- [ ] 安排维护窗口：ratchet 会经过 FMV 7（blocking 迁移），大库在 Open 期间阻塞做标记 compaction。
- [ ] 测试/dev 环境先跑，确认 ratchet 完成后再上生产。

可选加速项：仓库未使用自定义 Comparer/Merger（已 grep 确认），可用官方工具离线升级，跳过在线 ratchet：

```
go run github.com/cockroachdb/pebble/cmd/pebble@v1.1.5 db upgrade <db-dir>
```

### 第二步：切换到 pebble/v2

- [ ] `go get github.com/cockroachdb/pebble/v2`，替换 `store/` 内 import path。
- [ ] 按上表修改 `store/store.go` 4 处 Options 代码及 `store/store_test.go` 对应断言。
- [ ] 跑全量门禁：`go build ./... && go vet ./... && go test ./...`。
- [ ] 用真实数据目录做一次冷启动演练（含 `server`、`model` 测试）。

## 风险与注意事项

- **不可逆**：FMV 单行道，第一步发布即锁死回退旧二进制的可能。
- **保守运行可行**：v2 打开 FMV 13 的库后默认停留在 FMV 13，不会自动启用 columnar blocks / value separation 等新格式；不设置 `FormatMajorVersion` 即可稳住，收益主要是 bugfix 和性能改进。
- **compaction 并发语义变化**：当前 `MaxConcurrentCompactions: 3` 是硬上限；v2 的 `CompactionConcurrencyRange` 返回区间，且在高读放大/compaction debt 时会自动调高。直接映射 `return 3, 3` 可保留现状，但建议借机确认预期行为。
- 按 AGENTS.md 契约：`Storage`、key 编码、迭代顺序、同步写入开关不受影响（store 测试全过即为证据）。

## 工作量评估

代码侧约半天；真正成本在 FMV ratchet 的运维编排（备份、维护窗口、不可回退）。
