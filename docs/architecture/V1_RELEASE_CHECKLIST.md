# AI Knowledge OS V1 发布验收清单

本清单按证据强度区分实现完成与生产完成。任何局部探针、fixture 身份或历史快照都不能替代生产门禁。

| 领域 | 当前状态 | 证据或缺口 |
| --- | --- | --- |
| 契约与模块边界 | 已通过 | `go test ./...`、`go vet ./...`、依赖边界测试 |
| HTTP/OpenAPI 组合根 | 已通过本地门禁 | `pkg/bootstrap`、三份 OpenAPI 检查 |
| 当前/历史读取与授权 | 局部实库通过 | PG16 探针；生产 IAM、Grant 和发送屏障仍缺 |
| 删除、清理、取消/显式重试控制 | 局部实库通过 | PG16 探针；生产 worker 身份、人工对账、HTTP重试入口、Purge 仍缺 |
| 取消审计与 outbox | 契约和 SQL 已实现 | 控制审计、identity-backed event_seq outbox、EventBus、Runner、CursorStore；生产消费者部署仍缺；旧非序列游标升级需清空并重放 |
| PG16 SQL/仓储 | 当前稳定窗口通过，条件生产验收 | 当前 WSL 16.15 SQL/Go 探针和资源清理通过；fixture 身份/部署、生产 IAM 与长期稳定性仍缺 |
| 生产身份与部署 | 装配边界/JWKS加载已通过本地测试，未生产验收 | 需要真实 issuer、令牌登记与撤销、密钥轮换、IAM Grant、模块注册、部署快照和宿主 assembler |
| 检索效果与性能 | Harness边界已通过本地测试，正式结果未验收 | 需要固定中文评测集、Recall@10、真实延迟/成本和并发矩阵 |

## 当前本地门禁

`python docs/architecture/check_architecture.py --report docs/architecture/evidence/current-local-checks.json`必须报告 Go 全量测试、Go vet、Python 架构测试和 OpenAPI 全部通过，并且源码清单在检查期间不变。

## PG 实库解释

`python docs/architecture/check_postgres_wsl.py --major 16 --port 55432`通过只证明指定 WSL PostgreSQL 版本和 fixture 身份下的 SQL/仓储行为。若墙钟健康检查捕获宿主跳变，当前轮必须标为未通过；不得用此前稳定窗口替代当前证据。

## 发布前不可省略

生产凭据和密钥轮换、真实部署/模块快照、持久 outbox 消费与告警恢复、跨消费者并发/重启、显式重试/人工对账、Purge、迁移回退以及检索质量/性能结果全部具备独立证据后，才可把 V1 状态从条件通过改为生产通过。
