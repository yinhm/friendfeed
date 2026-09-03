# Data maintenance

本文件集中记录运行时数据的检查、修复和安全删除入口。命令中的 `FFDB_ADDRESS` 由操作者按部署环境设置，且必须保持在 ffdb 的 loopback 信任边界内。

## OAuth identity

OAuth identity 以不可变的 `provider:user_id` 为 key；多个 identity 可以指向同一个 Profile UUID。
检查或解除绑定不会修改 Profile、Entry、FeedService 或其他 OAuth 行，也不会输出 token。

```bash
./cli --address "$FFDB_ADDRESS" oauth inspect twitter example-user-id
./cli --address "$FFDB_ADDRESS" oauth unlink twitter example-user-id
./cli --address "$FFDB_ADDRESS" oauth unlink twitter example-user-id --apply
```

`unlink` 默认只做 dry-run。Apply 要求输入 `UNLINK provider:user_id`；服务端会重新检查 identity，
并拒绝删除 Profile 的最后一个 OAuth 登录身份。

## Group administrators

通过现有 Group 领域 RPC 提升或降级管理员，不直接写 GroupAdmin 表。`--actor` 必须是该
Group 的现有 admin 或 super；目标用户必须已经是 Group member。命令接受 Feed ID 或 UUID，
默认只解析并显示三方身份，追加 `--apply` 才会修改角色：

```bash
./cli --address "$FFDB_ADDRESS" group admin promote \
  --actor operator --group example-group --user alice

./cli --address "$FFDB_ADDRESS" group admin promote \
  --actor operator --group example-group --user alice --apply

./cli --address "$FFDB_ADDRESS" group admin demote \
  --actor operator --group example-group --user alice --apply
```

服务端仍会执行权限、membership 和 last-admin 检查，并按既有 Group 行为发送角色变更通知。
