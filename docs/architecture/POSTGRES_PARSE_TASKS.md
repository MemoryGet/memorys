# PostgreSQL 解析任务状态

`docs/architecture/sql/parse_tasks_v1.sql` 为已有 `withdraw_contract_v1` 增加解析任务头、审计和 outbox。`pkg/postgres.ParseTaskRepository` 提供带 row version 的读取、租约领取、fenced 失败回写、fencing 完成、取消和重试原子操作；失败任务只能从 `failed/cancelled` 重试，活动任务只能取消，所有控制都写入审计和 outbox。

已应用过 v1 的数据库还需顺序应用 `docs/architecture/sql/parse_tasks_v2.sql`，为失败回写增加 `parse.task.failed` 审计和 outbox 事件；不修改已登记的 v1 checksum。

应用方式：

```powershell
go run ./cmd/memorys-migrate -file docs/architecture/sql/parse_tasks_v1.sql -version parse_tasks_v1
```

`pkg/ingestion.ParseWorker` 现在把任务队列、`CandidateService.Prepare/Commit` 和完成回写串成一次有界执行，并支持持续轮询；结构/准备失败在支持该端口的持久队列中写入安全 `failed` 状态，候选提交或完成回执未知时保留租约等待接管；`postgres.BoundParseTaskQueue` 会按 queued 状态发现任务、领取租约并完成或失败回写。当前本地工作台仍使用 `parse-tasks.json` 以保持个人模式；生产环境还需要把上传服务创建的 parser 命令 payload 写入任务表。

上传应用可以调用 `ParseTaskRepository.CreateFromCommand`，把带当前 profile、原件 BlobRef、草稿版本和授权范围的 `DraftParseCommand` 作为受控 payload 写入 queued task；不会把凭据或权限判断交给任务表，Worker 仍会在 Prepare/Commit 前重新执行授权。

`ingestion.UploadParseService` 将 Blob 上传和任务创建编排为一个补偿边界：任务创建失败时按原请求 scope 删除刚上传的 Blob，避免上传成功但没有解析任务的孤儿原件。

`postgres.RedisParseTaskSource` 与 `RedisBoundParseTaskQueue` 提供 SDK 无关的 Redis 触发入口。Redis 消息只允许有界 JSON `task_id`，不会携带任务 payload、权限或租约；领取、状态和 fencing 仍由 PostgreSQL 负责。Redis 没有消息时队列回退到 PostgreSQL 的 queued 扫描，避免通知丢失使任务永久不可见。
