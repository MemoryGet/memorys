# 管理 DELETE：认证、权限、真实任务与 202

更新：2026-10-18。已接入受控本地装配与真实PostgreSQL；默认服务仍不自动配置身份、权限或数据库。

## 契约入口

DELETE /api/v1/knowledge-os/documents/{id}接受唯一Idempotency-Key头和唯一expected_document_version查询参数。后者以十进制uint64解析，必须大于0且小于最大uint64，避免浮点舍入及行版本溢出。请求正文必须为空，未知或重复查询参数、重复幂等头、无效版本均拒绝；客户端不能传入Principal、Scope、空间、KB或动作。

返回契约DeleteAccepted的202响应仅含document_id、job_id、publication_epoch。job_id引用同一删除事务中实际持久化的document.delete.cleanup任务，不复用操作ID或虚构任务。响应及安全错误均带no-store和X-Request-ID。

默认newMux注册该路径但返回503/capability_missing。可信装配可单独提供DocumentDeletionApplication与ProfileIdentitySource，不依赖启用搜索服务。后续已接入[删除任务初始状态查询](JOB_STATUS_RUNTIME.md)；其余管理CRUD、完整任务管理与UI仍未完成。

## 认证与资源授权

AuthenticatedDeletion先认证访问令牌，再通过DeletionAuthority从主库查找文档的实际空间/KB。PostgreSQL适配器以同一主体、客户端和文档资源申请封存ACL中的kb.delete权限，固定映射为内部document.delete作用域。此映射是平台代码，不接受客户端动作；knowledge.read和publication.withdraw不能隐含删除权限。解析允许已删除文档，用于当前有权主体恢复已提交回执。

每次请求创建独立DeletionControl及标准存储实例。控制绑定初始主体/发行方/客户端/委托、实际文档、空间/KB、策略版本、请求版本与幂等键。Domain Verify、SQL作用域入口、资源核验、提交前及应用返回前均使用控制重新认证并重新解析权限；新的ScopeID不改写稳定意图，但策略或主体变化会拒绝当前执行。

凭据独立复制，每次Authenticator获得另一份可清除副本，不把Bearer凭据放入删除命令或存储数据。控制结束后关闭并清除副本，保留控制引用也不能继续使用；String/GoString脱敏。提供方改变输入切片不能改写调用者凭据或绑定请求。

无效/撤销令牌返回401；缺失或不可访问资源统一404。行版本或同键意图冲突、已删除文档的新键命令返回409；同键重放恢复原202及原任务。依赖故障使用安全503，期限超时使用504。完整错误分类与故障矩阵仍未完成。

## 实际任务与事务

DeleteDocumentResult及DocumentDeletionCommand新增JobID，服务生成任务ID并在原子事务内登记标准Job初始定义。表document_delete_jobs绑定操作、历史、文档、空间/KB及配置摘要；Job阶段为document.delete.cleanup，state=queued、RowVersion=1、Attempt/FencingToken/Progress为0，无租约及错误。回执的JobID必须与该记录一致，缺少任务不能提交。任务定义不可变，后续已接入[独立运行进度与有界消费](DELETION_CLEANUP_RUNTIME.md)，执行仍使用逐目标真实租约及回执，不虚构聚合租约。

202响应只代表派生产物清理编排登记，不意味着任务已消费或原件已物理删除；实际进度由GET查询。当前源固定CleanupPlan、历史、审计、事件、tombstone与任务共同提交，保留既有[软删除事务](DOCUMENT_DELETION.md)的回滚及幂等语义。清理成员还冻结该文档已有历史计划目标，原件Purge仍是独立保留策略后的流程。

ProfileIdentitySource提供可信配置摘要，原生探针使用主库当前目录解码验证后的快照。接口不接受客户端覆盖摘要；本轮没有证明配置切换或权限撤销与SQL提交之间的严格写入屏障。生产Grant/发送屏障及完整UoW仍开放，逐次复核不能替代它们。

## 当前证据

553项输入在PG16通过283项原生检查，全量Go测试/go vet、228项Python和三份OpenAPI通过；管理删除/软删除、历史及当前读取独立复核同步通过，所有报告对应同一源码快照，临时目录与端口已独立确认清理。

实际RS256第二阅读者在策略4仅获得一条kb.delete授权，没有任何read授权，仍可经真实HTTP删除。原读取主体不能删除，缺失目标与无权目标的安全错误一致；同键重放返回原任务，版本变化及新键重复删除返回409，令牌撤销返回401，策略5撤销权限后旧回执恢复返回404。独立Python按管理OpenAPI检查响应，关联真实Job、回执、文档CAS及唯一授权行，并确认重放没有重复任务/回执。

单元与入口测试覆盖认证副本隔离、错误资源/动作、最终令牌撤销和策略变化、控制退出后拒绝、严格查询/空正文/重复幂等头、超过2^53的准确版本及默认未装配拒绝。

见[实库证据](evidence/management-delete-postgres16-review.json)、[独立复核](evidence/management-delete-runtime-review.json)、[本地检查](evidence/management-delete-local-checks.json)、[历史回归](evidence/management-delete-history-read-review.json)、[当前读取回归](evidence/management-delete-current-read-review.json)及[资源清理](evidence/management-delete-cleanup-review.json)。本轮仅PG16，旧三版本证据不能替代此快照。

生产issuer/密钥和管理授权配置、正式Bootstrap/迁移、完整任务查询/管理与消费、Purge、并发删除/发布及完整故障矩阵、发送屏障、其他管理接口/UI和完整V1均未验收。实际令牌与ACL使用临时测试登记，不代表生产IAM落地；原件授权与前期治理写入fixture的范围保持原有说明。既有任务租约间歇失败继续开放。
