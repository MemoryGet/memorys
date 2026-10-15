# AI Knowledge OS 设计文档

查看 [总进度图](PROGRESS.md)：当前处于架构设计与契约基础验证，业务闭环和真实组件验收仍待实施。

当前阶段：契约基础与知识治理链路的局部实现，完整架构验收保持开放。默认采用 Go 模块化单体、Vue 管理界面、PostgreSQL 权威元数据、Redis 任务投递、本地原件存储和云模型；这些都是实现选择，不能进入产品契约。

| 阅读顺序 | 文档 | 回答的问题 |
|---|---|---|
| 1 | [总体方案](AI_Knowledge_OS_开发计划与模块化架构.md) | 产品目标与总体模块划分 |
| 2 | [详细架构](architecture/DESIGN.md) | 依赖方向、数据归属、发布一致性与部署 |
| 3 | [契约规范](architecture/CONTRACTS.md) | 标准对象、模块端口、注册配置、错误、事件、REST/MCP |
| 3.1 | [一致性与迁移协议](architecture/CONSISTENCY.md) | 并发发布、后端代际、授权时序、租约与迁移日志 |
| 3.2 | [模块装配协议](architecture/MODULE_PROTOCOL.md) | 能力清单 Schema、依赖绑定、生命周期与接入清单 |
| 3.2.1 | [装配内核与接入边界](architecture/KERNEL_RUNTIME.md) | 内部计划、工厂注入、超时回收与正式部署归一化门槛 |
| 3.2.1.1 | [受控构建锁](architecture/BUILD_LOCK.md) | 部署/清单/制品/保留策略摘要及持久化代际启动核对 |
| 3.2.2 | [复合引擎契约](architecture/COMPOSITE_CONTRACT.md) | 统一准备/检索端口、整体替换约束与独立通道扩展 |
| 3.2.3 | [远程模块协议](architecture/REMOTE_MODULES.md) | 17项HTTP操作、身份/Grant、二进制/流、结果未知与取消规则 |
| 3.2.4 | [解析远程映射](architecture/PARSER_WIRE_MAPPING.md) | 实际Go类型与wire字段、精度/联合内容边界及7组Schema往返 |
| 3.2.5 | [检索链路远程映射](architecture/RETRIEVAL_WIRE_MAPPING.md) | 嵌入/检索/重排的完整字段、向量dtype、过滤与候选来源约束 |
| 3.2.6 | [索引远程映射](architecture/INDEX_WIRE_MAPPING.md) | 准备/验证/删除、attempt/fencing、可信期望与私有映射 |
| 3.2.7 | [文件远程映射](architecture/BLOB_WIRE_MAPPING.md) | 上传metadata、读取/删除、关联头、EOF/长度/哈希与取消边界 |
| 3.2.8 | [远程错误映射](architecture/ERROR_WIRE_MAPPING.md) | 封闭错误码、安全模板、关联ID与未知结果/重试边界 |
| 3.2.9 | [生成与事件映射](architecture/GENERATION_WIRE_MAPPING.md) | 请求/预算、五类事件、证据白名单、完整正文与唯一终态 |
| 3.2.10 | [状态与取消映射](architecture/CONTROL_WIRE_MAPPING.md) | 控制/原请求关联、15类结果、六种状态与停止确认边界 |
| 3.2.11 | [远程映射覆盖表](architecture/REMOTE_MAPPING_COVERAGE.md) | 17操作逐项数据证据、二进制/SSE边界和D-03运行缺口 |
| 3.2.12 | [SSE接收与HTTP验证](architecture/SSE_TRANSPORT.md) | 帧边界/预算、逐批闸门、有界读取与取消关闭的本地网络证据 |
| 3.2.13 | [multipart上传与隔离暂存](architecture/BLOB_MULTIPART_TRANSPORT.md) | 流式大小/摘要核验、前后访问回调、失败清理与服务器读取超时 |
| 3.2.14 | [上传确认与恢复](architecture/BLOB_CONFIRMATION.md) | 接收/持久/确认三层边界、幂等身份、事务和崩溃恢复门槛 |
| 3.2.15 | [远程Grant绑定](architecture/ACCESS_GRANT.md) | 服务身份/令牌/实时权限三层边界与调用摘要 |
| 3.2.16 | [操作动作与子能力](architecture/REMOTE_OPERATION_POLICY.md) | 17操作策略、父业务意图与资源授权 |
| 3.2.17 | [Grant令牌与SDK](architecture/GRANT_TOKEN_PROFILE.md) | 专用JWT Profile、go-jose固定候选及密钥轮换 |
| 3.2.18 | [公钥信任激活](architecture/KEY_TRUST_ACTIVATION.md) | 来源信任、持久高水位、部署重启与防回滚恢复 |
| 3.2.19 | [排名融合与来源轨迹](architecture/FUSION_CONTRACT.md) | 首个RRF Fuser、路线去重与完整来源/代际 |
| 3.2.20 | [检索协调与跨库配额](architecture/RETRIEVAL_COORDINATOR.md) | 有界规划、合并策略、失败语义及运行门槛 |
| 3.2.21 | [分库请求与授权边界](architecture/PARTITION_REQUEST.md) | 子Scope、发布投影、代际隔离与新Grant |
| 3.2.22 | [发布投影身份](architecture/PUBLICATION_PROJECTION.md) | 父子快照、关系记录、过滤等式与摘要设计 |
| 3.2.23 | [查询权威服务](architecture/QUERY_AUTHORITY.md) | 可替换读快照、父子授权、当前复核与一致性边界 |
| 3.2.24 | [元数据读写绑定](architecture/METADATA_STORE_BINDING.md) | 数据集/存储代际、端点观测、共同激活与回退 |
| 3.2.25 | [数据源启动门](architecture/STORE_STARTUP_GATE.md) | 独立期望、本次探测绑定、真实覆盖与联合就绪 |
| 3.2.26 | [探测接收账本](architecture/STORE_PROBE_LEDGER.md) | 一次性CAS、重复/冲突、接管与提交未知恢复 |
| 3.2.27 | [有限反思与草稿编排](architecture/REFLECTION_ORCHESTRATION.md) | 多查询共享预算、输出边界、反馈采纳与替换验收 |
| 3.2.28 | [反思检索应用接入](architecture/REFLECTION_PIPELINE_BINDING.md) | 新授权/向量/投影、最终证据与全请求计量 |
| 3.2.30 | [依赖边界检查](architecture/DEPENDENCY_BOUNDARIES.md) | 精确import清单、领域/编排隔离与静态检查 |
| 3.2.29 | [全请求计量](architecture/REQUEST_METERING.md) | usage、精确费用、并发预留、未知与恢复 |
| 3.3 | [逻辑数据模型](architecture/DATA_MODEL.md) | 记录关系、唯一约束、事务矩阵、冻结与回收 |
| 3.3.1 | [保留与回收契约](architecture/RETENTION_CONTRACT.md) | 保留期限、回退保护、幂等、保留锁与物理删除闸门 |
| 3.3.2 | [元数据导出契约](architecture/EXPORT_CONTRACT.md) | 权威记录库存、清单/分段摘要、快照水位和隔离导入闸门 |
| 3.3.3 | [迁移日志与水位](architecture/JOURNAL_PROTOCOL.md) | 完整事务、提交屏障、前驱游标链和原子重放 |
| 3.3.4 | [KB迁移状态机](architecture/MIGRATION_STATE.md) | 切换/重启/恢复写入、取消与回退、租约和窗口守卫 |
| 3.3.5 | [原件复制与映射](architecture/BLOB_COPY.md) | 权威文件库存、流式字节核验、稳定BlobRef与物理映射切换 |
| 3.4 | [管理界面契约](architecture/MANAGEMENT_INTERFACE.md) | 管理页面、命令API、权限、异步与冲突体验 |
| 3.4.1 | [管理OpenAPI草案](architecture/openapi/management.v1.json) | 26项管理命令/查询、类型化DTO与乐观锁输入 |
| 3.5 | [伙伴OpenAPI草案](architecture/openapi/partner.v1.json) | 四项伙伴接口DTO、认证、引用、反馈与流事件 |
| 3.6 | [标准内容格式](architecture/CONTENT_FORMAT.md) | 表格/图片/音频载荷、检索投影和来源坐标 |
| 3.6.1 | [冻结编码与哈希](architecture/FROZEN_CONTENT.md) | 精确字节规则、冻结字段、两级哈希及跨语言金标 |
| 4 | [选型与决策](architecture/DECISIONS.md) | 默认方案、替代方案、WeKnora 边界及待验证事项 |
| 4.0.1 | [候选组件准入](architecture/COMPONENT_ADMISSION.md) | 固定许可/部署证据、制品与数据隔离、统一准入门槛 |
| 4.0.2 | [解析部署与替换](architecture/PARSER_DEPLOYMENT.md) | 引擎/字体/模型完整包、OCR能力、子进程预算与双实现门槛 |
| 4.1 | [WeKnora适配器](architecture/WEKNORA_ADAPTER.md) | 固定源码证据、文本投影、候选隔离和使用门槛 |
| 4.1.1 | [Qdrant独立向量适配器](architecture/QDRANT_ADAPTER.md) | 显式向量/过滤/代际、数据重建及复合路线对照 |
| 4.1.2 | [中文关键词适配器](architecture/KEYWORD_ADAPTER.md) | OpenSearch/分词候选、精确过滤与独立混合检索对照 |
| 4.2 | [MCP适配器](architecture/MCP_ADAPTER.md) | 协议/SDK基线、OAuth与服务身份、工具映射和互操作验收 |
| 5 | [验证与实施顺序](architecture/VALIDATION.md) | 如何证明可替换、权限安全、召回和性能达标 |
| 5.1 | [证据召回评测](architecture/RETRIEVAL_EVALUATION.md) | 前10窗口、标准来源区间、失败分母、权限负例与离线算术 |
| 6 | [实施状态](IMPLEMENTATION_STATUS.md) | 文档、代码和实测分别完成了什么 |
| 7 | [需求覆盖与证据审查](architecture/REQUIREMENTS_AUDIT.md) | 原始20个模块、五类成果和验收条件的覆盖与缺口 |
| 8 | [架构验收报告](architecture/ACCEPTANCE_REVIEW.md) | 2026-10-12逐项判定、13项检查证据、契约冻结缺口与关闭标准 |

详细规范对总体方案中的简写作进一步约束；实现发现冲突时应更新决策和契约版本，不能让适配器自行改变产品语义。

设计原则：**固定业务语义，替换技术实现；平台持有权威数据，外部组件提供能力。**“每个模块可替换”不意味着任意组件都能直接互换。替代实现必须通过对应契约与能力门槛，有状态模块还必须通过迁移和回退验证。

已有 GitHub 调研见 [组件调研](../AI_Knowledge_OS_GitHub选型调研_2026-10-11.md)。调研结论与实际适配验证分开记录，尚未实测的能力不能写作生产承诺。

统一本地检查入口见[验证方案](architecture/VALIDATION.md#统一本地验证入口windows)和[执行脚本](architecture/check_architecture.py)。仅本地设计/契约回归，不代表运行验收。

可选扩展设计见[记忆/图谱生命周期](architecture/MEMORY_GRAPH_LIFECYCLE.md)：权威归属、复合来源、撤回/删除、接口缺口与替换试验。

检索返回前的必需权威检查顺序见[当前复核应用编排](architecture/CURRENT_CHECK_FLOW.md)，含身份绑定、当前批准、真实Ready、最终版本检查及发送边界。

[可替换证据核验模块](architecture/EVIDENCE_VERIFICATION_MODULES.md)：内容/原件/索引/多格式定位的分工、路由与替换验收。

[新增模块质量修复记录](architecture/MVP_QUALITY_REPAIR.md)：检索过滤、真实文件解析、上传限制、引用、存储契约、分块ID与后续运行门槛。

[权威撤回事务与派生清理](architecture/WITHDRAW_TRANSACTION.md)：历史幂等结果、固定清理目标、事务/outbox边界及6项WTX验收。

[撤回存储约束与 PostgreSQL 验证](architecture/POSTGRES_WITHDRAW_STORAGE.md)：真实15.17证据、隔离原型和独立运行器。


[撤回命令语义与准备器](architecture/WITHDRAW_COMMAND_SEMANTICS.md)：规范化摘要、跨语言金标及当前权限验证边界。


[清理任务持久化与领取边界](architecture/CLEANUP_TASKS.md)：任务租约、旧消费者写回拒绝和外部效果恢复的验收范围。


[可替换清理执行器与单项消费者](architecture/CLEANUP_WORKER.md)：固定效果身份、独立回执核验及后端接入边界。


[本地索引实际清理适配器](architecture/CLEANUP_NATIVE_INDEX.md)：实际删除、独立效果核验和重启持久化的剩余边界。


[索引清理双实现与持久化边界](architecture/INDEX_CLEANUP_BACKENDS.md)：统一清理接口、PG删除标记及替换验收范围。


[同数据索引交接与回退试验](architecture/INDEX_SNAPSHOT_TRANSFER.md)：公共快照、双实现往返校验和迁移范围。


[索引窄能力配置装配](architecture/INDEX_MODULE_ASSEMBLY.md)：受控Provider选择及配置切换的当前证据。


[MarkItDown成熟组件接入试验](architecture/MARKITDOWN_ADAPTER.md)：四类文件真实转换及标准候选映射的验证范围。


[多格式解析器受控装配](architecture/MARKITDOWN_ASSEMBLY.md)：版本健康、文件依赖和能力不足拒绝的当前实现。

[PDF双解析器替换](architecture/PARSER_REPLACEMENT.md)：同一标准调用入口选择MarkItDown或独立pypdf后端，验证正文、来源及关闭语义。

[解析候选与草稿合并](architecture/PARSED_DRAFT_CANDIDATE.md)：保护人工编辑和已提交草稿，保留迟到解析候选；明确待实现的持久事务边界。

[候选持久化事务](architecture/CANDIDATE_PERSISTENCE.md)：同步候选、草稿CAS、回执、审计与outbox的隔离PostgreSQL实现及运行验证边界。

[候选应用服务](architecture/CANDIDATE_APPLICATION.md)：解析前后授权、配置绑定、供应商隔离和不重新解析的提交重试。

[候选读取与提交核对](architecture/CANDIDATE_READING.md)：独立读取授权、原主体摘要核验和未知结果恢复边界。

[独立WSL PostgreSQL验证](architecture/WSL_POSTGRES_VALIDATION.md)：PG15/16/18隔离实例运行、源码/制品证据及私有实例清理。

[文件到候选数据库串联](architecture/CANDIDATE_E2E_VALIDATION.md)：实际文件解析、应用服务、PG提交、冷连接重试及迟到结果保留。

[PostgreSQL多版本验收矩阵](architecture/POSTGRES_VERSION_MATRIX.md)：同一代码/二进制在15.19、16.15、18.6上执行当前SQL与150项事务/应用及查询读取检查。

[原生草稿冻结映射](architecture/NATIVE_FROZEN_MAPPING.md)：四类内容的规范载荷、双摘要与独立金标验证；权威提交事务待实施。

[草稿提交审核事务](architecture/SUBMISSION_TRANSACTION.md)：冻结Revision、待审核请求、草稿CAS、幂等回执及审计/outbox的原子提交与故障验证。

[审批决定事务](architecture/REVIEW_TRANSACTION.md)：批准/拒绝/撤销、当前决定指针、幂等历史重放及管理Schema对照。

[有效批准与发布准备登记](architecture/PUBLICATION_START.md)：按当前规则核验批准，绑定后端代际并原子保存准备操作、初始任务与outbox；最终发布待实施。

[发布任务租约与内容读取](architecture/PUBLICATION_TASKS.md)：稳定命令回执、续租与接管隔离、原发布者再授权及冻结内容核对。

[实际索引准备与独立读回](architecture/INDEX_PREPARATION.md)：模块化 Worker、文本关键词默认后端、持久 attempt 产物及实际投影验证。

[准备任务完成与未知结果恢复](architecture/PREPARATION_COMPLETION.md)：重新核验真实索引后原子提交completed、产物引用、回执、审计和事件，支持租约到期后的历史对账。

[原子最终发布与替换](architecture/FINAL_PUBLICATION.md)：当前批准/配置/实际索引复核、有效头CAS、历史/审计/事件和旧源清理原子提交；支持正常撤回与历史重放，正式有效检索待接入。

[PostgreSQL查询权威读层](architecture/POSTGRES_QUERY_READS.md)：标准QueryReadUnit的真实同快照分页、当前批准/代际及撤回/轮换验证；正式Retriever和最终输出授权仍待接入。
