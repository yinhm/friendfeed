# tools/audit

Group 领域的数据库完整性审计工具（docs/group.md 实施顺序第 8 步）。

检查项：

1. **admin 非成员**：GroupAdmin 行对应的 Follow(user -> group) 成员边是否存在。
2. **无 admin Group**：Type=group 且未删除的 profile 是否至少有一行 GroupAdmin。
3. **孤儿 membership**：Follow 边指向不存在或已软删除的 group。
4. **单边 membership**：指向存活 group 的 Follow(user -> group) 与 Follower(group -> user) 是否成对存在。
5. **deleted Group 残留**：GroupAdmin 行指向不存在或已软删除的 group。

## 用法

```
go run ./tools/audit -data /srv/ffdb/data
```

全部通过退出码为 0；发现任何 issue 退出码为 1。

## 已知噪音

stock/系统 feed 也是 Type=group 且刻意没有成员/admin（见 docs/group.md
差距清单），检查 2「无 admin Group」会报告它们。这是有意保留的现状，
属可接受噪音，工具不加豁免逻辑。
