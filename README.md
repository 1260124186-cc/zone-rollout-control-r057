# Zone Rollout Control

Zone Rollout Control 是仅依赖 Go 标准库的本机 HTTP 控制面，用于协调区域版本上线。它管理滚动计划、不可变区域版本、解析节点租约、探针结果和一致性判定。

本仓库是健康项目初始化基线。当前不包含单元测试、夹具或浏览器测试；测试工作有意延后到后续测试阶段。现在通过真实 HTTP 路由的生产探针验证构建和四条核心工作流。

## 环境要求

- Go 1.22 或更高版本。
- 本机可用 TCP 端口。
- 无需外部数据库、远程 DNS 服务或环境变量。

## 构建

```bash
go build ./...
```

## 启动服务

```bash
go run ./cmd/zrc \
  --addr 127.0.0.1:8090 \
  --state data/zrc-state.json
```

参数：

- `--addr`：HTTP 监听地址，默认 `127.0.0.1:8090`。
- `--state`：JSON 状态快照路径，默认 `data/zrc-state.json`。
- `--shutdown-timeout`：中断后的关闭窗口，默认 `10s`。

服务会锁定 `<state>.lock`，避免两个进程同时修改同一份状态。每次成功事务先复制状态，再通过同目录临时文件和原子替换写入快照。

## 目录结构

```text
cmd/zrc                     服务进程入口
cmd/zrcverify               离线快照完整性检查入口
cmd/workflowprobe           工作流探针入口
internal/domain             聚合、DNS 值校验、状态迁移和判定计算
internal/store              锁内副本事务和原子 JSON 持久化
internal/platform           时钟、标识、游标和文件锁
internal/plan               计划及区域版本服务
internal/fleet              解析节点档案服务
internal/allocation         节点选择和租约生命周期
internal/probe              探针登记、幂等重放和租约完成
internal/assessment         一致性聚合和最新结果查询
internal/httpapi            HTTP 路由、中间件和响应映射
internal/verify             快照引用、状态、请求索引与摘要的只读校验
internal/workflowprobe      真实 HTTP 路径验证
```

## 快速使用

以下示例假设服务监听 `127.0.0.1:8090`。

登记解析节点：

```bash
curl -sS -X PUT http://127.0.0.1:8090/v1/nodes \
  -H 'Content-Type: application/json' \
  -d '{
    "node_name": "resolver-east-01",
    "region": "east"
  }'
```

创建滚动计划：

```bash
curl -sS -X PUT http://127.0.0.1:8090/v1/plans \
  -H 'Content-Type: application/json' \
  -d '{
    "zone": "edge.example",
    "minimum_success_ratio": 1,
    "maximum_latency_ms": 80,
    "expected_nodes": 2
  }'
```

添加区域版本：

```bash
curl -sS -X PATCH http://127.0.0.1:8090/v1/plans/<plan_id>/revisions \
  -H 'Content-Type: application/json' \
  -d '{
    "serial": 2026092501,
    "authority": "ns1.edge.example",
    "entries": [
      {"owner": "edge.example", "type": "A", "value": "192.0.2.10"},
      {"owner": "www.edge.example", "type": "CNAME", "value": "edge.example"}
    ]
  }'
```

激活计划：

```bash
curl -sS -X PATCH http://127.0.0.1:8090/v1/plans/<plan_id>/activate \
  -H 'Content-Type: application/json' \
  -d '{"expected_version": 2}'
```

分配节点租约：

```bash
curl -sS -X PATCH http://127.0.0.1:8090/v1/plans/<plan_id>/leases \
  -H 'Content-Type: application/json' \
  -d '{
    "request_key": "lease-request-0001",
    "region": "east",
    "lease_seconds": 600,
    "max_nodes": 2
  }'
```

提交探针：

```bash
curl -sS -X PATCH http://127.0.0.1:8090/v1/leases/<lease_id>/probes \
  -H 'Content-Type: application/json' \
  -d '{
    "request_key": "probe-request-0001",
    "success": true,
    "observed_serial": 2026092501,
    "latency_ms": 12,
    "observed_at": "2026-09-25T12:00:00Z"
  }'
```

请求一致性判定：

```bash
curl -sS -X PATCH http://127.0.0.1:8090/v1/plans/<plan_id>/assessments \
  -H 'Content-Type: application/json' \
  -d '{"request_key": "assess-request-0001"}'
```

## 离线完整性检查

`zrcverify` 在变更前对快照做全量只读校验，发现启动和业务读取只能 lazy 发现的断链与不闭合：

```bash
go run ./cmd/zrcverify --state data/zrc-state.json
```

行为约定：

- 只读：不锁定、不创建、不修改、不替换快照文件，服务运行期间也可执行。
- 按稳定顺序检查：结构形态、实体引用、状态约束、请求索引、计数与摘要。
- 输出机器可读 JSON 报告，包含 `counts` 和稳定排序的 `problems` 列表；重复运行同一文件结果与问题顺序完全一致。
- 退出码：`0` 表示一致，`1` 表示发现完整性问题，`2` 表示检查未完成（文件不可读、超出限制、解码失败或被取消）。
- 有界扫描：`--max-bytes` 限制可读取的快照大小，`--max-problems` 限制收集的问题数量（超出时报告 `truncated: true`），`--timeout` 限制整体耗时；`SIGINT`/`SIGTERM` 可随时取消，取消时仍输出部分报告。
- `--compact` 以单行 JSON 输出报告。

## API 概览

成功响应：

```json
{
  "data": {},
  "meta": {
    "request_id": "req_..."
  }
}
```

失败响应：

```json
{
  "error": {
    "code": "validation_failed",
    "message": "plan input is invalid",
    "details": {
      "zone": "contains unsupported characters"
    }
  }
}
```

| 方法 | 路径 | 用途 |
| --- | --- | --- |
| `GET` | `/healthz` | 进程健康检查 |
| `PUT` | `/v1/nodes` | 登记解析节点 |
| `GET` | `/v1/nodes` | 按状态和区域分页查询节点 |
| `PUT` | `/v1/plans` | 创建计划草稿 |
| `GET` | `/v1/plans` | 按状态分页查询计划 |
| `GET` | `/v1/plans/{id}` | 查询计划 |
| `PATCH` | `/v1/plans/{id}/revisions` | 添加区域版本 |
| `PATCH` | `/v1/plans/{id}/activate` | 激活计划 |
| `PATCH` | `/v1/plans/{id}/complete` | 完成计划 |
| `PATCH` | `/v1/plans/{id}/leases` | 给节点分配租约 |
| `PATCH` | `/v1/leases/{id}/release` | 提前释放租约 |
| `PATCH` | `/v1/leases/{id}/probes` | 登记探针并完成租约 |
| `PATCH` | `/v1/plans/{id}/assessments` | 创建一致性判定 |
| `GET` | `/v1/plans/{id}/assessments/latest` | 查询最近判定 |

## 状态及失败行为

- 计划按 `draft -> active -> completed` 单向迁移，激活和完成需要匹配版本。
- 区域版本只在计划草稿状态可追加，序列号严格递增且内容不可变。
- A、AAAA、CNAME 条目在写入前规范化并稳定排序，重复条目直接拒绝。
- 节点按 `ready -> leased -> ready` 变化，隔离节点不会被选中。
- 一个节点同一时刻最多有一个 `claimed` 租约。
- 相同请求键和相同内容可安全重放；同键异载荷返回 `idempotency_conflict`。
- 一致性判定综合节点覆盖率、序列号、成功比例和最大延迟，返回 `consistent` 或 `review_required`。
- 所有校验和冲突在快照替换前返回，不留下部分状态。

## 后续工程任务的行为边界

项目保留了以下可独立观察和验证的生产边界，适合后续工程任务围绕它们设计需求：

1. 计划草稿、区域版本、激活、完成和版本冲突。
2. DNS 名称规范化、IP 校验、条目去重和稳定排序。
3. 序列号递增与重复版本拒绝。
4. 节点隔离、区域筛选、稳定选择和唯一租约。
5. 租约期限、释放、过期清理和重复释放。
6. 租约请求键与请求指纹重放。
7. 探针请求键、载荷指纹、租约完成原子性和冲突检测。
8. 成功率、节点覆盖、序列号一致性和延迟上限计算。
9. 原子快照替换、文件锁、进程关闭和并发事务串行化。
10. 严格 JSON 解码、分页游标和领域错误到 HTTP 状态的映射。

这些是正常运行机制，不是预置缺陷，也不包含缺陷票据、参考答案或测试文件。

## 验证

测试阶段有意延后，因此当前不运行单元测试或浏览器测试。以下命令分别验证四条工作流；每条命令启动真实本机 HTTP 服务、使用独立临时状态、覆盖成功与失败路径，然后关闭服务：

```bash
go run ./cmd/workflowprobe plan-revision-lifecycle
go run ./cmd/workflowprobe node-lease-allocation
go run ./cmd/workflowprobe probe-ingestion
go run ./cmd/workflowprobe consistency-assessment
```

探针不访问外部网络，也不依赖已有状态文件。

以下命令对已有快照做离线完整性检查，报告引用、状态、索引与摘要问题：

```bash
go run ./cmd/zrcverify --state data/zrc-state.json
```
