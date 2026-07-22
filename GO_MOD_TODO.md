# Go 依赖升级计划

更新基线：2026-07-22。`go.mod` 声明 Go 1.25，当前工具链为 Go 1.26.4。

## 执行原则

- 只升级明确列出的直接依赖，不使用无边界的 `go get -u`。
- 一个风险域一个提交；OAuth、配置、搜索索引和分词器不得混在一起升级。
- 不因升级修改公开 API、存储、Graph 或外部服务契约。
- 传递依赖只允许随目标模块解析或 `go mod tidy` 必要调整。
- 每个检查点运行 `go build ./... && go vet ./... && go test ./...`。

## 升级检查点

- [x] 建立基线：当前完整 Go 门禁通过。
- [x] 补丁升级：`github.com/dghubble/oauth1` 0.7.0 → 0.7.3，`github.com/sirupsen/logrus` 1.9.3 → 1.9.4。
- [x] Twitter 客户端：`github.com/dghubble/go-twitter` 2021-06-09 → 2022-11-04；确认客户端仍使用调用方注入的 OAuth HTTP client，项目使用的 timeline API 契约未变。
- [x] OAuth provider：`github.com/markbates/goth` 1.67.1 → 1.82.0；锁定 Google/Twitter callback 的 provider 解析，保持 `provider:user-id` 身份键入口不变。
- [x] 配置：`github.com/spf13/viper` 1.8.1 → 1.21.0；锁定显式 `config.json` 路径、JSON 值和环境变量读取语义。
- [ ] 搜索：`github.com/blevesearch/bleve/v2` 2.4.0 → 2.6.0；让 `bleve_index_api` 随主模块解析，验证既有磁盘索引可打开以及 rebuild 流程。
- [ ] 分词：`github.com/go-ego/gse` 0.80.2 → 1.0.2；比较中英文切词和搜索结果，不仅检查编译。
- [ ] 实验库：`golang.org/x/exp` 2024-08-23 → 2026-07-18；先盘点使用 API，再独立升级。
- [ ] Go 版本：单独评估将 `go` 指令从 1.25 提升到 1.26；不能夹带在依赖升级中。

## 当前无需升级

2026-07-22 查询没有发现直接依赖的新版本：Pebble 1.1.5、Gin 1.12.0、Cobra 1.10.2、Testify 1.11.1、Bluemonday 1.0.27、OAuth2 0.36.0、gRPC 1.82.1、Protobuf 1.36.11，以及当前 `x/net`、`x/sys`。

## 未主动升级

`go list -m -u all` 还会列出大量传递依赖候选。它们不构成独立升级任务；只有直接上游升级需要时才更新，避免把行为变化和兼容风险混入无关检查点。
