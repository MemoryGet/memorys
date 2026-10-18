# 文档软删除：事务、回执与保留边界

更新：2026-10-18。这是内部治理服务与PostgreSQL适配器的实现，不是完整V1、生产迁移或管理DELETE接口验收。

## 标准调用与权限

contracts.DeleteDocumentRequest使用CommandContext、DocumentID和ExpectedDocumentVersion，符合管理设计的expected_document_version语义。document.DeletionService依赖标准DocumentDeletionStore，要求独立Verify回调验证已认证主体、委托及实际文档授权；仅传入Principal/Scope不构成权限证明。内部动作document.delete不能由publication.withdraw或knowledge.read推导。

管理契约DELETE /documents/{id}标注kb.delete权限，2026-10-18已增加[管理DELETE入口、认证及权限映射](MANAGEMENT_DELETE_RUNTIME.md)，以可信平台代码将实际kb.delete资源授权映射为内部document.delete。DeleteAccepted的202投影引用真实持久化Job；默认服务器注册路径但未装配依赖时仍返回503。

服务对请求和提供方校验输入做独立复制；删除意图绑定文档行版本、空间/KB、文档、配置摘要和稳定主体/委托语义。命令及主体摘要分别使用akos.delete.command/1、akos.delete.actor/1域分隔。请求ID、ScopeID、期限、策略版本和临时委托证明不进入稳定摘要，重试仍必须执行当前权限核验。配置摘要或行版本变化不能借同一幂等键改写原意图。

## 实际事务

PostgreSQL DocumentDeletions在Serializable会话内执行：

1. 验证作用域和实际资源权限，按稳定主体、动作和幂等键保留命令，冲突拒绝。
2. 锁定未删除文档，核对实际行版本；读取当前publication epoch及可空的revision/set。
3. 写入action=delete的不可变历史，epoch递增。存在当前发布源时，以实际代际及已登记依赖冻结清理计划；从未发布或已撤回时不伪造版本、代际或清理计划。
4. CAS设置deleted=true并清空有效版本/代际，行版本递增；回执、初始document.delete.cleanup任务、document.delete审计、document.deleted事件及命令完成在同一事务提交。
5. 提交前再次核验权限。应用返回前也复核当前权限；拒绝时不泄露操作结果，已提交结果仍需通过同键且重新授权的恢复调用读取。

数据库延迟约束要求删除历史与实际tombstone、回执、清理、审计和事件一致。审计主体及时间必须与历史相符，回执版本/epoch/结果必须匹配真实文档。缺少审计或直接设置删除标志不能提交；原记录不可变。已删除标志不能直接撤销，也不能重新挂载发布头。此实现没有定义恢复已删除文档的业务操作。

## 重试与访问失效

首次提交后丢失响应时，新适配器和新的请求/ScopeID可在重新授权后恢复原回执，既不重新删除也不复活来源。相同键更换文档版本或其他稳定意图会冲突。当前及历史读取、文档定位和发布服务继续使用未删除文档前置条件；已有历史及有效签名游标不能绕过删除状态。

原三状态治理试验的授权仍是明确的fixture回调；新增管理入口试验已使用真实RS256/主库状态和封存kb.delete授权，不代表生产IAM已完成。读取拒绝使用真实RS256/主库状态和封存read/history ACL，并经实际REST/MCP入口验证。原件保留验证使用独立原件fixture授权；它不是已删除文档的普通访问授权。

## 原件与清理

软删除保留全部Revision、冻结正文、历史和审计，并保留实际原件字节。当前源的清理目标固定为旧revision/set/epoch和真实代际；旧换版/撤回产生的历史清理计划不被改写。后续已接入[固定成员和有界清理消费者](DELETION_CLEANUP_RUNTIME.md)，覆盖全部已登记退役计划；持续调度、全模块清理及原件PurgeJob仍未完成。

物理删除仍须按DATA_MODEL中的保留、迁移、恢复引用及任务规则独立设计，不能将软删除等同于原件擦除。生产原件Grant及发送撤权屏障仍开放，不能仅由本轮元数据拒绝证明所有外部访问路径已撤销。

## 当前证据

最新管理入口增量为553项输入、PG16共283项检查，真实删除专用身份及Job/202关系、重放/冲突/令牌与权限撤销均通过独立复核；全量本地检查和历史/当前读取回归通过。见[管理入口证据](evidence/management-delete-runtime-review.json)。以下547项记录保留为当时的内部服务快照，不能替代当前源码证明。

547项源码输入在PG16通过275项原生检查；全量Go测试/go vet、228项Python及三份OpenAPI通过。新服务测试覆盖稳定意图、文档CAS绑定、主体发行方绑定、回调副本隔离和最终权限拒绝。实库覆盖三种文档状态、错误行版本、事件失败回滚、提交前撤权、漏审计的提交拒绝、裸tombstone提交拒绝、冷连接响应恢复、幂等冲突、当前/历史及REST/MCP拒绝、再次发布/撤销删除标志拒绝、真实原件和三个Revision保留。

独立Python校验实际head/history/receipt/audit/outbox及清理代际归属、实际原件SHA-256/长度/删除前后字节一致，以及新请求和旧游标的安全错误字段。历史和当前读取回归同步通过，所有报告匹配同一源码快照，临时目录和端口已独立确认清理。本轮仅PG16，没有借用旧快照声称PG15/18同步通过。

见[实库证据](evidence/document-deletion-postgres16-review.json)、[删除独立复核](evidence/document-deletion-runtime-review.json)、[历史读取回归](evidence/document-deletion-history-read-review.json)、[当前读取回归](evidence/document-deletion-current-read-review.json)、[本地检查](evidence/document-deletion-local-checks.json)及[资源清理](evidence/document-deletion-cleanup-review.json)。

生产身份/管理授权、完整管理接口与UI、正式迁移与跨版本运行、任务查询/消费及完整Task/UoW、持续清理与Purge、发送屏障、并发删除/发布和完整故障矩阵、第二仓储替换/恢复及完整V1继续保持未验收。既有任务租约间歇失败不因本轮通过而关闭。
