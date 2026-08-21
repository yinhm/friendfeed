# Follow Request（private feed/group 统一审批流）执行计划

分支：`feat/follow-request`。每步一个小提交，全部完成后合回 master。

设计共识（docs/group.md + 合并讨论）：

- Follow 边是唯一关系事实：订阅 user feed 与加入 Group 是同一条边。
- 对 private target（user feed 或 group）建立 Follow 边需要批准；唯一类型分支是批准人：user feed → owner 本人，group → 任一 admin。
- 请求表只是工作流数据，不冒充关系；approve 后关系以 Follow 边生效。
- reject/撤回即删除请求，允许重新申请；邀请制不做（第二阶段）。
- 顺带修正两个不对称：private user feed 读取闭环（行为变更=修复 private 语义）；批准后统一入队 home.rebuild。公开 follow 行为不变。

## Phase 1 — model 层

- [ ] 1.1 `TableFollowRequest = 118` + table 变量 + key helper；登记 `model/types.go`
- [ ] 1.2 状态机 stage 函数（全部幂等、流式）：
  - `StageRequestFollow`：target 有效且 private、requester 有效 user、未已 follow
  - `StageCancelFollowRequest` / `StageRejectFollowRequest`：删请求
  - `StageApproveFollowRequest`：同 batch 删请求 + 写双边（group 复用 activity 调整）；已 follow 时幂等
  - `IsFollowRequestPending`、`ListFollowRequests(target, limit, cursor)`
- [ ] 1.3 泛化 membership 读取 helper（private user feed 可见性用）
- [ ] 1.4 `StageExitAllGroups` / 账号注销临界区清理 requester 请求；删除 Group/Profile 清理 target 请求
- [ ] 1.5 model 单测：状态机全路径、幂等、approve 后 Follow 边生效、清理路径

## Phase 2 — server / RPC 层

- [ ] 2.1 proto：`RequestFollow`/`CancelFollowRequest`/`ApproveFollowRequest`/`RejectFollowRequest`/`ListFollowRequests`；`FollowResponse.requested`；`GroupView.has_pending_request`；`make` 重新生成
- [ ] 2.2 RPC 实现 + 授权（owner/admin/super）；GraphFollow 对 private target 路由 RequestFollow
- [ ] 2.3 读取闭环泛化：`enforcePrivateGroupRead` → feed 级 `enforcePrivateFeedRead`；Entry 级检查、Home stale 重校验、Search 过滤同步泛化
- [ ] 2.4 `CreateGroup(private=true)` 放开（`StageCreateGroup` 移除拒绝）
- [ ] 2.5 approve 路径统一入队 home.rebuild（两类 target）
- [ ] 2.6 server 测试：非授权 approve 被拒、申请人不能自批、private user feed 读取闭环

## Phase 3 — httpd 层

- [ ] 3.1 Follow 按钮三态：Follow / Requested（置灰）/ Following
- [ ] 3.2 group members 页 Pending 区块 + Approve/Reject（复用 member-action 表单模式）
- [ ] 3.3 `/account/requests` SSR 页：本人 private feed 的待批列表
- [ ] 3.4 group create 表单放开 private checkbox，更新提示文案
- [ ] 3.5 httpd 测试

## Phase 4 — 收尾

- [ ] 4.1 `audit_store`：requester/target 已删除的孤儿请求
- [ ] 4.2 文档：`docs/group.md` 差距清单、`docs/database_design.md` 表 118、`AGENTS.md` 表号登记
- [ ] 4.3 E2E：user private（设 private → B 申请 → A 批准 → B 可读；拒绝一条）、group private 同路径
- [ ] 4.4 全量验证：`go build/vet/test ./...` + `pnpm lint/typecheck/test/build` + `pnpm run test:e2e`
- [ ] 4.5 删除本文件，合并分支
