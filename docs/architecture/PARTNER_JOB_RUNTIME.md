# 伙伴任务状态与控制

`memorys-server` 在启用 PostgreSQL 搜索时现在装配删除清理任务的状态、取消和重试应用服务。每次调用重新认证 Bearer 凭据、重新读取任务、绑定空间/知识库/文档和 profile digest，并要求客户端携带当前任务 `row_version`；底层 `DeletionCleanupJobs` 继续负责活动租约、未知效果、fencing 和审计/outbox 原子写入。

当前同时覆盖 PostgreSQL 解析任务的状态、取消和重试：解析任务读取器先从受控 `DraftParseCommand` 解析文档/知识库绑定，再重新申请 `document.create`；控制操作按 row version 调用 parse task repository，解析重试回到 `queued`。新增发布准备任务状态读取 `/api/v1/knowledge-os/publication-jobs/{id}`、安全取消 `/api/v1/knowledge-os/publication-jobs/{id}/cancel` 和新意图重试 `/api/v1/knowledge-os/publication-jobs/{id}/retry`。重试从旧失败任务的 publication intent 读取 revision/approval/epoch，重新申请 `publication.start` 并调用 `PublicationStarts`，返回新的 Job ID；旧任务保持 failed。发布取消使用 publication task 的 fencing、superseded receipt、audit/outbox 原子写入。发布准备 Worker 仍有独立队列/Runner 生命周期边界，真实多进程联合验收仍开放。

三类 Job 现在还提供 `/events` SSE 路由。每条事件是当前授权后的完整快照，`Last-Event-ID` 只负责续号，不充当权限或数据库游标；重新认证失败、任务读取失败或连接取消时流关闭。
