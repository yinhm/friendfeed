# gRPC 健康检查

ffdb 的 gRPC server 注册了标准健康检查服务 `grpc.health.v1.Health`（`google.golang.org/grpc/health`，无新增依赖），不修改任何业务 protobuf。

## 报告的服务名

| service | 含义 |
| --- | --- |
| `ffdb.Storage` | `NewApiServer` 成功打开存储（Pebble） |
| `ffdb.Search` | `InitIndexService` 完成 |
| `ffdb.Api` | ApiServer 整体就绪（`Serve` 前最后置位） |
| `""`（空） | 整体 readiness，以上全部 SERVING 后才为 SERVING |

所有 service 启动时均为 `NOT_SERVING`，对应初始化步骤成功后才置为 `SERVING`；未报告就绪的组件保持 `NOT_SERVING`。收到关停信号后、
`GracefulStop` 排干之前，全部状态立即切回 `NOT_SERVING`，排干期间新探测即失败。

## 探测方式

假设服务监听 `127.0.0.1:8080`（以实际 `config.json` 的 `address` 为准）。

grpc_health_probe（退出码 0 表示 SERVING，适合脚本与监控系统）：

    grpc_health_probe -addr=127.0.0.1:8080
    grpc_health_probe -addr=127.0.0.1:8080 -service=ffdb.Storage

grpcurl（便于人工查看）：

    grpcurl -plaintext 127.0.0.1:8080 grpc.health.v1.Health/Check
    grpcurl -plaintext -d '{"service": "ffdb.Search"}' 127.0.0.1:8080 grpc.health.v1.Health/Check

## systemd 集成

`conf/ffdb.service` 保持 `Type=simple`：gRPC health 不是 `sd_notify`，不要把 unit 改成 `Type=notify`。可选集成方式：

- **外部监控**：由监控系统定期执行 `grpc_health_probe -addr=<addr>`，非零退出码即告警；这是最简单可靠的方式。
- **本机 watchdog timer**：新增 `ffdb-healthcheck.service`（`ExecStart=grpc_health_probe -addr=<addr>`，`OnFailure=` 告警）加同名 `.timer` 周期执行；不要把探测塞进 `ffdb.service` 的 `ExecStartPost`，避免单次探测时序耦合启动结果。
- **依赖健康状态的调用方**（如前置代理）应直接消费 `grpc.health.v1.Health/Check`，空 service 名代表整体 readiness。

排干语义：systemd 停止 ffdb（SIGTERM）后服务立刻报告 NOT_SERVING，但 `GracefulStop` 会继续处理在途请求直到 `TimeoutStopSec=30`，监控端看到 NOT_SERVING 不等于进程已退出。
