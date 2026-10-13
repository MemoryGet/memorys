# 标准契约 V1

本文定义平台语义。OpenAPI/JSON Schema及首批Go端口类型已形成草案；当前不是完整已实现SDK。内部领域端口和外部协议共享DTO语义，认证信息、事务句柄和字节流通过各自传输适配，不直接序列化Go对象。

进程内接口草案与模型见 [基础端口](../../pkg/contracts/ports.go)、[核心](../../pkg/contracts/core.go)、[内容](../../pkg/contracts/content.go)、[检索](../../pkg/contracts/retrieval.go)、[配置与代际](../../pkg/contracts/profiles.go)、[生命周期](../../pkg/contracts/lifecycle.go)、[生成/反思/记忆/图谱](../../pkg/contracts/intelligence.go)、[任务事件](../../pkg/contracts/async.go)、[事务仓储/评测观测](../../pkg/contracts/persistence.go)。共30个端口与辅助仓储接口，不等同30个业务模块或30个已实现能力。

Go字段与OpenAPI按语义映射，Content/AnswerEvent联合类型不是JSON格式，不能直接JSON编码当作HTTP/MCP响应。当前为接口草案，状态机、租约栅栏、权限证明、union有效性、输入限制、序列化和版本映射还需实现并测试。

## 1. 公共类型与规则

ID 使用平台生成的不可推测字符串；时间为 UTC RFC3339；内容校验使用 SHA-256。空集合表示无结果或无权限，绝不表示无限制。分页使用不透明游标，并固定排序与查询快照。写命令带 expected_version 和 Idempotency-Key，重复键且不同请求体返回 conflict。

| 类型 | 必需字段与约束 |
|---|---|
| Principal | subject_id、subject_type(user/service)、身份发行方、经验证的委托上下文；不得信任用户自行填写的角色 |
| AccessScope | scope_id、subject_id、action、space_id、kb_ids、policy_version、expires_at；仅授权模块生成 |
| Document | id、space_id、kb_id、title、created_by、active_revision_id、active_generation_set_id、publication_epoch、deleted_at、row_version |
| Draft | id、document_id、base_revision_id、editable_blocks、blob_refs、row_version；提交后冻结对应 Revision |
| DraftBlock | id、draft_id、kind、text/结构化载荷、source_anchor、order、row_version；解析和编辑阶段使用，不虚构 revision_id |
| Revision | id、document_id、parent_revision_id、blob_refs、hash_format、content_hash、blocks_hash、created_by、created_at；创建后不可变；精确编码见[冻结规范](FROZEN_CONTENT.md) |
| Block | id、revision_id、kind(text/table/image/audio)、text/结构化载荷、source_anchor、order；文本与表格格式采用平台 schema |
| SourceAnchor | kind(original/revision)、原件逻辑 ID 或修订 Block ID、页/工作表/幻灯片/段落定位、可选字符范围；字符范围采用 Unicode 码点半开区间；人工改写用 revision，原件仅作 derived_from |
| Chunk | id、revision_id、block_spans、text、chunk_profile_digest、content_hash；来源范围不得超出 Block |
| EmbeddingSpec | provider_model、model_revision、dimension、distance、normalization、preprocess_version；规范化 JSON 的摘要标识规格 |
| IndexGeneration | id、kb_id、revision_id、channel、profile_digest、embedding_spec（向量通道）、backend_epoch_id、backend_binding、state、readiness_proof；单修订逻辑产物，与物理 collection 分离 |
| GenerationSet | id、revision_id、backend_epoch_id、每个必需检索通道对应代际、准备状态；文档发布一次切换整个集合 |
| BackendEpoch | id、kb_id、backend_profile_digest、state、revision→generation_set 映射、cutover_watermark；知识库迁移切换单位 |
| QuerySnapshot | scope_version、kb_epoch_map、publication_filter_digest、generation_sets、query_profile_digest；请求内固定、输出前复核 |
| MigrationJournal | sequence、operation_id、resource/version、变更快照或不可变载荷引用、checksum、墓碑；与业务事务提交，回退窗口内保留 |
| Publication | operation_id、document_id、revision_id、generation_set_id、approval_id、expected_epoch、state、actor、时间 |
| Candidate | citation、chunk_id、route_id、retriever_id、KB、rank、score、score_kind、generation_ids、evidence_text；代际/后端诊断不进入外部Evidence |
| Evidence | citation、source_excerpt、relevance、来源锚点、已校验版本；可选图谱路径也须引用证据 |
| PipelineProfile | id、version、模块绑定、策略参数、预算、embedding_spec、digest；创建后不可变 |
| IndexRoute | route_id、channels、writer binding；每通道的权威索引写入路线，产物绑定IndexGeneration |
| RetrievalRoute | route_id、channels、retriever binding、candidate_limit、required；返回候选标注route_id |
| BackendBinding | platform_resource_type/id、module_instance_id、backend_id、generation_id；只在服务端使用 |
| BlobRef | id、sha256、size、media_type；物理路径由文件适配器管理 |
| Job | id、operation_id、stage、revision_id、profile_digest、state、attempt、lease/fencing_token、progress、error |
| MemoryRecord | id、owner/scope、content、provenance、created_at、expires_at、version；用户删除必须传播到索引 |

PipelineProfile 中索引构建与查询编排分别使用 IndexProfile/QueryProfile 子版本。索引构建规格以 KB 的 BackendEpoch 为准，查询不能用当前模型默认配置覆盖已有代际。跨 KB 的规格分组和迁移时序见 [一致性协议](CONSISTENCY.md)。

Block 的 text 用于统一检索；结构化表格必须保留单元格、合并关系和表头，图片保存 BlobRef 与可选描述。厂商扩展进入有命名空间的 extensions 字段，上层核心流程不能依赖未纳入契约的扩展。

草稿解析、冻结哈希、记录约束与事务边界见 [逻辑数据模型](DATA_MODEL.md)。DocumentBlocks 是解析结果 DTO，冻结之后才成为带 revision_id 的权威 Block。

## 2. 模块端口

所有调用携带 RequestContext（request_id、deadline、profile_digest、取消信号）；需要内容访问的端口另带已验证作用域。仓储也检查资源空间，不能仅凭 ID 读取任意租户数据。

| 端口 | 输入 → 输出 | 必须保证 |
|---|---|---|
| Authenticator.authenticate | Credential → Principal | 过期、撤销、错误发行方拒绝；凭据不回显 |
| Authorizer.authorize | Principal、Action、RequestedResources → AccessScope | 默认拒绝、请求范围只缩小、明确策略版本 |
| Ingestor.ingest | SourceDescriptor、KB、IdempotencyKey → Document/Draft、Job | 大文件流式接入、类型/大小校验、禁止重复创建 |
| BlobStore.put/open/delete | 字节流/BlobRef → BlobRef/字节流/结果 | 原子完成、校验和、幂等删除、避免路径逃逸；逻辑引用稳定 |
| Parser.parse | BlobRef、格式、选项 → DocumentBlocks、诊断 | 声明支持格式；无法解析明确错误；来源定位可验证 |
| Splitter.split | Blocks、ChunkPolicy → Chunks | 来源可追溯、确定性或显式记录随机种子 |
| Embedder.embed | Texts、EmbeddingSpec → Vectors | 数量匹配、维度一致、有限数值、规格准确；不静默换模型 |
| IndexWriter.prepare/delete/verify | Generation、Chunks/Vectors → ReadinessProof | 幂等、隔离代际、删除可验证、确认后才可切换 |
| CompositeEngine.prepareProjection/verify/delete/search | 已批准投影/通道目标或标准RetrieveRequest → ReadinessProof/Candidates | 单一整组端口；不冒充独立向量写入；规范见[复合契约](COMPOSITE_CONTRACT.md) |
| Retriever.search | Query、AccessScope、PublicationFilter、Generation、TopK → Candidates | 必需过滤在候选选择前生效；分数类型与排序声明明确 |
| Fuser.fuse | 多路 Candidates、策略 → Candidates | 默认 RRF，按 citation/chunk 去重；保持来源 |
| Reranker.rerank | Query、Candidates、Limit → RankedEvidence | 只排序/筛选输入证据，不能添加未知来源 |
| Generator.generate | Question、Evidence、Budget → AnswerEvents | 支持取消；引用只能指向提供的证据；供应商原始流不外泄 |
| PublicationService.submit/approve/publish/withdraw | Actor、Revision、ExpectedVersion → Operation | 强制状态机、审计、CAS；外部审批回调须验证身份并去重 |
| Reflection.assess/refine/propose | Query、Evidence、Answer、Budget → Assessment/QueryPlan/DraftProposal | 不直接写发布知识；不扩大授权范围；遵守总预算 |
| Memory.save/search/update/delete | Scope、MemoryRecord/Query → Records | 作用域过滤、TTL、版本冲突、删除传播 |
| Graph.extract/query | Evidence/Query、Scope → Entities/Relations/Evidence | 每个事实有来源；查询结果不能携带未授权节点/边 |
| MetadataRepository + UnitOfWork | 查询/变更/导出包 → DTO/提交结果 | CAS、唯一性、原子提交业务记录+审计+outbox；实现不得泄漏 SQL 类型 |
| FeedbackService.submit | 经过授权的目标、意见、幂等键 → FeedbackResult | citation/request_id恰好一个；不直接审核发布 |
| TaskRepository.claim/renew/complete | JobRef、Lease/FencingToken → Job/结果 | 元数据事务中的权威租约；安全重领、条件提交、幂等 |
| TaskQueue.enqueue/receive/ack/retry | JobRef、DeliveryReceipt → Receipt | 至少一次通知；重复交付；投递回执不是任务租约 |
| EventBus.publish/consume | EventEnvelope → Receipt | 至少一次，消费者去重，不承诺全局顺序 |
| Evaluator.evaluate | Dataset、RunProfile → Report | 保存完整输入版本、指标定义、失败项和可复现配置 |
| Observer.record | Metric/Trace/AuditRef → Result | 普通指标失败不改变业务结果；必需审计失败阻止相关写事务 |

索引写入与召回端口可由不同模块实现，但必须共享经过验证的索引格式或远程协议。一体化后端不暴露标准向量写入时，声明 composite_ingest_search；不能绑定到要求独立 Embedder + IndexWriter 的流水线。

远程模块端点按 `/modules/v1/<capability>/<operation>` 映射，认证使用服务身份，AccessScope 通过受签名保护的内部上下文或受认证的内网请求传递。文件传递使用受限时效的内部 Blob 读取能力，避免把任意 URL 下载能力暴露为 SSRF 入口。

具体包络、二进制、SSE、Proof私有映射、调用账本和取消规则已成文，见 [远程模块协议](REMOTE_MODULES.md) 与 [17操作机器契约](openapi/modules.v1.json)。普通scope DTO仍不是授权证明，必须同时验证服务身份与访问Grant/当前状态。机器契约不代表远程适配器或验签已实现。

UnitOfWork的TransactionRepositories只在callback内有效，包含metadata/tasks/audit/outbox/idempotency/journal/consumer receipts，必须一起提交或回滚；返回后不能使用逃逸句柄。存储实现负责检查句柄生命周期，接口声明本身不能防止逃逸。导出/导入流使用一致性快照和专门迁移上下文，禁止在普通短写事务中执行外部网络I/O。

RetrieveRequest的Generations由当前请求通道与GenerationSet选取，不能只传一个向量代际并忽略关键词代际。IndexPrepareRequest可包含同一Revision/BackendEpoch的多个通道目标，ReadinessProof逐个记录GenerationReadiness；所有必需目标准备完成才能切换。内部Candidate保留全部来源generation IDs，最终Evidence仅暴露平台citation和已校验片段。

Chunk可跨多个Block，但单条Candidate/Evidence的source_excerpt必须能定位到其单个citation范围。跨Block片段需由适配器拆成分别可引用的候选，不能将整块混合文字标为其中一个Block的原文；融合和TopK按实际证据单元计算，保留共同Chunk的内部关联。

Go中的ApprovalRequest是待审事实记录，ApprovalDecisionRequest是包含actor/expected version的命令。Document.CreatedBy属于内部权威元数据，不因API目录DTO省略而丢弃；传输层只输出OpenAPI允许的字段。Profile和QuerySnapshot的slice/map须在边界冻结或复制，Go结构声明本身不提供不可变性。

[ValidateReadyGenerationSet](../../pkg/contracts/generation_validation.go)已实现代际准备关系校验，支持复合证明和独立通道证明。其GenerationExpectation必须来自权威KB/Revision/BackendEpoch/Profile及待构建内容清单；不得从引擎响应或客户端请求反推期望值。该函数不访问后端，不做授权、审批、租约栅栏或发布CAS，也不证明self-reported readiness是真实的。

校验错误为安全PublicError，传输层还需绑定当前request_id并按错误协议输出；不能直接编码缺request_id的领域错误。正确字段关系只是IndexWriter.Verify和发布事务之间的一层检查，不替代任何既有发布门槛。

TaskRepository管理权威租约、完成/失败结果以及取消/重试，TaskQueue只投递。EventBus消费去重记录通过ConsumerReceiptRepository与消费的业务结果在同一事务保存。Generate的AnswerSink错误和context取消需终止输出；Reflector只返回评估/查询计划/修改建议，不获得PublicationService权限。

## 3. 注册、配置及版本

Manifest 至少包含 module_id、implementation_version、contract_version、capabilities、config_schema、dependencies、health、state_model、migration_modes、composite_groups。每项capability包含name、version和features；能力名例如parser、retriever，版本例如1.0；特性例如prefilter.kb、prefilter.revision、cancellation、export.canonical。以正式清单Schema为准，不把版本拼入能力名。

依赖按能力和版本约束声明，由部署明确选择实例，注册中心不自行猜测供应商。检查拓扑无环，同一能力允许多个实例，通过绑定决定每个流水线使用哪个。依赖销毁按逆拓扑执行；创建失败要关闭本次已创建资源。模块内部自己的多个能力可以互相协作，对外仍需声明复合限制。

概念配置（不是正式部署格式；正式[部署配置Schema与装配规则](MODULE_PROTOCOL.md)要求显式绑定对象、固定实现版本及IndexProfile/QueryProfile引用）：

```json
{
  "schema_version": 1,
  "modules": [
    {"instance_id":"parser-a","provider":"docreader-http","contract":"1.0","settings":{"endpoint":"http://docreader:8080"},"credential_ref":"secret/docreader"},
    {"instance_id":"blob-a","provider":"local-blob","contract":"1.0","settings":{"root":"./data/blobs"}}
  ],
  "bindings": {"parser":"parser-a","blob_store":"blob-a"},
  "profiles": [{"id":"chinese-default","version":1,"bindings":{"parser":"parser-a"},"reflection":{"enabled":false,"max_extra_rounds":2,"total_budget_ms":30000}}]
}
```

该示例只展示两个能力，不能作为完整可启动部署。完整部署必需认证、授权、元数据事务、文件、审核、任务、索引写入、检索、分块、嵌入和观测；问答启用后还必需生成模型。复合检索可替代其覆盖的处理端口，但必须满足相同产品不变量。

启动顺序：严格 schema 校验 → 解析凭据引用 → 契约兼容 → 特性门槛 → 依赖拓扑 → 创建 → 健康检查 → 就绪。未知字段、无效语义版本、缺失必需能力、空作用域过滤都不能静默忽略。

契约主版本变化代表破坏性变更；次版本只能增加可选字段/能力。未知响应字段可忽略，但未知状态/必需能力必须显式失败。请求内的错误字段不应被默默接受。配置摘要不包含密钥正文，包含凭据引用及版本；轮换密钥后重启装配，历史记录只保留引用版本。

空间/KB 管理员只能选择平台允许的解析、模型、检索、重排 Profile，不能覆盖部署级认证、元数据、审核规则和审计实现。请求到达后固定 Profile，不接受客户端任意指定模块实例。

## 4. 错误和事件

统一错误结构：code、safe_message、retryable、request_id。V1机器契约不提供details字段，未知字段拒绝；后续扩展须先升级对应Schema/契约。内部cause进入受控诊断；不返回连接串、API Key、原始后端错误或其它空间资源存在性。远程边界按[错误映射](ERROR_WIRE_MAPPING.md)丢弃原始文案并使用受控模板，retryable不等于写操作可自动重放。

| code | REST 状态 | 调用语义 |
|---|---|---|
| invalid_argument / unsupported_format | 400 / 415 | 修正请求后重试 |
| validation_failed | 422 | 请求结构合法但不满足内容完整性或状态等领域条件 |
| unauthenticated | 401 | 重新认证 |
| forbidden / not_found | 403 / 404 | 已知动作无权用 403；资源越权按 404 隐藏存在性 |
| conflict / publication_changed | 409 | 获取当前版本后重新提交；不能无限自动重试 |
| capability_missing / contract_incompatible | 503（启动时不就绪） | 修正部署配置 |
| rate_limited | 429 | 返回 Retry-After |
| deadline_exceeded / dependency_unavailable | 504 / 503 | 按预算重试；查询不返回未检查证据 |
| internal | 500 | 仅公开 request_id 和安全信息 |

insufficient_evidence 是成功请求的业务结果状态，不自动当 500。部分降级在响应 degraded 字段列出；权限校验失败不能作为降级继续运行。

EventEnvelope = event_id、type、schema_version、aggregate_id、aggregate_version、operation_id、space_id、occurred_at、profile_digest、payload。事件不携带原件全文与密钥。核心事件：revision.submitted、revision.approved、index.prepared、publication.committed、publication.withdrawn、feedback.created、job.failed。outbox 与业务事务一起写入；消费者保存 event_id 去重，并按 aggregate_version 拒绝过期覆盖或拉取最新状态。

## 5. REST 与 MCP 伙伴契约

REST 前缀 `/api/v1/knowledge-os`。MCP 使用官方 SDK 实现协议协商和 Streamable HTTP，首轮验证基线为2025-11-25、Go SDK候选v1.8.0；具体映射与认证见 [MCP适配器设计](MCP_ADAPTER.md)。本设计不自造 MCP 协议。HTTP 服务身份或委托身份由传输层认证，工具参数不能包含可冒充主体的 user_id。

四项伙伴接口的机器契约草案见 [OpenAPI](openapi/partner.v1.json)，使用[OpenAPI 3.1.1](https://spec.openapis.org/oas/v3.1.1.html)。正式路由尚未实现，管理API不包含在此草案中。反馈请求显式带space_id，以确定幂等和归属范围。

| 能力 | REST | MCP 工具 | 必需输入 | 返回 |
|---|---|---|---|---|
| 检索 | POST /search | knowledge_search | query、space_id；可选 kb_ids、top_k（1–100，默认10） | evidence[]、request_id、profile_version、timings、degraded |
| 读取 | POST /read | knowledge_read | document_id；可选 revision_id、block_ids、cursor | 标准内容块、citations、next_cursor；历史版本额外鉴权 |
| 问答 | POST /ask | knowledge_ask | question、space_id；可选 kb_ids、reflection、stream | answer、citations、status、request_id、timings、degraded |
| 反馈 | POST /feedback | knowledge_feedback | target citation/request_id、kind、comment、idempotency_key | feedback_id、state、draft_id（若已生成） |

输入范围与主体权限求交；无任何可读 KB 返回权限错误，合法范围无匹配返回空证据。搜索响应不包含后端标识、内部策略内容或其它空间计数。MCP 参数和 REST DTO 使用同源 schema，契约测试检查同一身份和输入产生同一业务结果。

REST 问答流使用 SSE 事件 meta、answer_delta、citation、done、error；done 表示终态并含 status。流中错误终止输出。MCP 首版返回相同最终结果对象，进度消息不是文本增量协议承诺。非流式和流式使用同一应用执行过程；反馈不能直接 approve/publish。

SSE每帧event名称与JSON data.type一致；done.result保存最终回答对象，error.error保存安全错误。deadline_exceeded以HTTP错误（尚未发送响应时）或流error终止，不能输出伪成功done。客户端以终态帧判断完成，断连不代表问答成功。非流式回答业务状态固定为answered/insufficient_evidence。

OpenAPI的字符长度限制按Unicode码点计算，传输另设字节大小上限；全空白问题由应用语义校验拒绝。SourceAnchor的字符终点范围、citation与Block归属和结构化载荷约束需进一步语义校验。表格/图片/音频类型化schema已定义，详见 [标准内容格式](CONTENT_FORMAT.md)；不得返回厂商原始对象。

管理 API 单独提供上传、任务查询、草稿编辑、提交、审批、发布、撤回、空间/KB 管理；这些操作不加入伙伴四工具的默认权限。原件上传支持 multipart/流式接口，大小、格式、OCR 页数和并发限制由部署配置给出。

具体管理资源、命令和页面行为见 [管理界面契约](MANAGEMENT_INTERFACE.md)。它与伙伴接口共享领域对象、权限和错误语义。

## 6. 迁移数据包

导出包含 schema_version、snapshot_id、watermark、文档/版本/Block、原件清单与哈希、审批/发布/撤回历史、授权、配置快照、反馈/记忆、后端映射和每文件校验。凭据正文单独由部署的密钥系统迁移。衍生索引可选导出，默认由权威内容重建。

导入先进入临时命名空间，校验引用完整性、跨空间归属、唯一键、哈希和发布状态，再切换。所有删除/撤回墓碑和权限变更必须包含在迁移水位内，否则旧内容可能复活。备份恢复必须跑与迁移相同的权限和引用验证。
