# 远程操作动作与受控子能力 V1

本规范细化[Grant绑定](ACCESS_GRANT.md)。动作表是必要约束，不赋予主体新权限。Authorizer、可信任务/流水线和Grant签发器共同建立子调用，用户不能自填Scope选择动作。

## 1. 完整操作策略

| 操作 | 允许Scope动作 | 必须另核验的事实 |
|---|---|---|
| parser/parse | document.parse | 任务原件、草稿版本、attempt |
| blob_store/put | blob.put | 上传操作、字节、保留锁；确认资格另查 |
| blob_store/open | blob.open | Blob归属与当前访问，或限定原件的解析子能力 |
| blob_store/delete | blob.delete | 引用、保留锁、墓碑及清理库存 |
| embedder/embed | knowledge.search / knowledge.ask / index.prepare | 同一查询或已审核索引任务、输入来源与模型 |
| retriever/search | knowledge.search / knowledge.ask | 有效发布集合、代际及主体范围 |
| reranker/rerank | knowledge.search / knowledge.ask | 同一查询的合法候选，不扩大来源 |
| generator/generate | knowledge.ask | 已核验证据、模型、预算及问答意图 |
| index_writer/prepare | index.prepare | 审批内容、operation/attempt/fencing与代际 |
| index_writer/verify | index.verify | 同一候选库存及准备证明 |
| index_writer/delete | index.delete | 明确代际/Chunk库存、保留闸门与旧写入隔离 |
| composite_ingest_search/prepare_projection | index.prepare | 同独立准备，另核对复合能力组 |
| composite_ingest_search/verify | index.verify | 复合各必需通道与来源/过滤 |
| composite_ingest_search/delete | index.delete | 同一复合库存和回收闸门 |
| composite_ingest_search/search | knowledge.search / knowledge.ask | 发布范围与来源、融合不重复放大 |
| control/status | invocation.status | 目标调用所有者/委托及资源当前权限 |
| control/cancel | invocation.cancel | 目标控制资格，不能回滚已提交事实 |

动作名是内部策略词汇，index.verify等不直接赋予用户新角色。发布者经已授权命令创建任务，Worker不能以自身服务身份取代原主体当前资格。scope.action正确也不能代替审批、内容、资源和任务核验。

## 2. 原业务意图与子调用

用户授权具体业务意图，例如获准KB内问答。应用在固定PipelineProfile中派生嵌入、召回、重排和生成Grant，每份限定实际input、目标模块、模型/配置和期限。Agent拥有knowledge.ask也不能直接向模型接口发送另一批数据、换供应商或延长预算。

子权限取原主体、客户端/服务能力、业务资源和配置的交集。解析只读子能力指向所需原件，不从document.parse推导读取全部Blob；删除只有通过回收守卫才签发。动作不按字符串前缀继承，不设置通配符回退。

签发器从权威请求/任务事实取得父意图，不接受模块自称合法任务。当前wire没有完整父证明，运行服务须通过持久调用/任务仓储关联；仅传operation_id无法自证。控制接口还需核验目标调用，越权隐藏为not_found。

## 3. 机器表与局部证据

[策略表](examples/remote-operation-policy.v1.json)由remote_operation_policy.py生成，与OpenAPI的17个operationId精确对照。固定POST/path、动作及read/job/pipeline/control分类；分类不是授权结果。

check_operation仅检查操作、路由、动作及非空无重复的空间/KB范围。空KB不能表示无限权限；控制目标的实际范围应从账本取得。真实验签、当前主体、资源归属、审批、发布、租约和清理均未执行，报告固定authorized=false。

17操作Grant金标已按此表修正scope.action，纠正共享字段样例的占位动作。原字段样例只证明DTO；新金标也不证明调用者权限。摘要随动作变化，旧报告为历史证据，最新见[执行证据](evidence/operation-policy-review.json)。

4项专项检查覆盖完整库存、读动作不能删/verify/cancel、查询嵌入不扩大生成权限及未知路由/空范围拒绝。真实委托、权威父任务、资源提取、撤权和流式输出仍待验，D-03开放。
