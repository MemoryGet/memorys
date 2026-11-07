# 伙伴反馈持久化

伙伴反馈现在有独立的应用服务边界：先认证主体，再由 PostgreSQL 解析 citation 对应的文档、知识库、修订和块，随后使用当前 `knowledge.feedback` 授权写入反馈。知识库 ID 不从请求体接受，幂等键按主体和空间绑定；同一键同一语义请求返回原结果，语义摘要不一致则返回冲突。

`docs/architecture/sql/feedback_v1.sql` 是在已应用 `withdraw_contract_v1` 后执行的增量迁移，创建反馈、审计和 outbox 表。使用 `memorys-migrate -file docs/architecture/sql/feedback_v1.sql -version feedback_v1` 应用。`MEMORYS_FEEDBACK_ENABLED=1` 时服务器装配伙伴 REST/MCP 反馈服务；未设置时保持 capability missing。

管理反馈建议另需应用 `docs/architecture/sql/feedback_proposals_v1.sql`（可用 `memorys-migrate -with-feedback-proposals`）。`POST /api/v1/knowledge-os/feedback/{id}/proposals` 会按反馈引用复制冻结版本为新草稿，并在同一事务写入建议、草稿创建审计和 outbox；返回的 `draft_id` 必须重新提交审核后才能发布。

管理上传另需应用 `docs/architecture/sql/document_metadata_v1.sql`（可用 `memorys-migrate -with-document-metadata`），并在服务器设置 `MEMORYS_BLOB_ROOT`。上传接口会先存原件，再创建源草稿并排队解析任务；未配置 BlobStore 或迁移时保持 fail-closed。

知识库创建另需应用 `docs/architecture/sql/knowledge_base_management_v1.sql`（可用 `memorys-migrate -with-knowledge-base-management`）。创建操作要求当前主体在空间 ACL 中拥有 `revision.approve` 管理授权，随后复制封存 ACL 并初始化审核、发布后端和查询配置头；创建成功后仍需单独上传文档。

反馈提交只记录评价，不改变正文、审核、发布或检索结果。按请求 ID 的反馈目标解析仍需要查询账本，因此当前生产路径先支持 citation 目标；请求目标会安全拒绝。
