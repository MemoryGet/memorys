# 当前新增模块质量修复记录

日期：2026-10-14。范围为未提交的memorys-server、api、storage、parser、document、ingestion、chunk、index和retrieval；未部署服务。此次修复不代表完整知识业务闭环、Recall@10或生产认证验收通过。

## 问题与修复证据

| 审查发现 | 修复 | 回归证据 |
|---|---|---|
| 检索忽略权限/发布过滤 | MemoryIndex在选候选前检查Space/KB/Revision/Generation/BackendEpoch及投影发布标志；拒绝空授权/空发布过滤；MemoryRetriever完整转交请求 | TestMemoryIndexFiltersBeforeLimitAndPreservesOwnedCitation、TestMemoryIndexRejectsUnfilteredEmptyExpiredAndCancelledRequests |
| Markdown把格式名当正文 | 通过标准BlobStore.Open读取真实文件，检查实际字节数、SHA-256、UTF-8及支持格式；原样保留空白、换行和来源字符范围 | TestRealFileParseDraftAndDeterministicUnicodeChunks |
| 无界上传/读入内存 | 请求体最多65MiB（含multipart开销），原件最多64MiB；存储使用临时文件流式写入、摘要和长度校验；失败移除临时文件 | TestUploadRequiresAuthorizationAndRejectsOversizeBeforeStorage、TestStoreRejectsPartialOversizeChecksumAndCorruption |
| 引用DocumentID/来源缺失 | 投影记录必须含完整平台文档/版本/块及来源；读取返回独立引用副本；API只输出公共字段并保留source_anchor | 索引引用测试、TestSearchRequiresPreparationAndCurrentOutputVerification |
| 自定义BlobWriter阻断替换 | UploadService和MarkdownParser使用contracts.BlobStore；LocalBlobStore实现Put/Open/Delete并有编译期接口检查 | 本地存储往返、真实文件处理链路测试 |
| 分块ID跨版本重复 | ID包含Revision、Block、字符范围、Policy与Profile；按Unicode码点分块且保留范围，拒绝无效策略/非法编码并限制数量和体量 | 真实文件分块测试检查确定性、不同版本不同ID及范围一致 |
| 新增包未登记、全量测试失败 | 明确依赖允许清单；业务处理纯包禁止数据库/HTTP/供应商SDK，文件访问封装在storage | TestProductionDependencyBoundaries |
| 可变索引及引用共用对象 | mutex保护投影写入和检索；入库与返回时复制引用；相同代际/块键禁止更换所属文档/版本 | 索引副本测试、TestMemoryIndexConcurrentProjectionChangesAndSearch |

9项新增Go测试覆盖上述范围；并发测试不是竞态检测器或性能压力测试。统一回归结果及当前源码SHA以[evidence/unified-local-checks.json](evidence/unified-local-checks.json)为准。

## 数据与认证边界

- 文件ID为随机平台标识，SHA-256仅表示内容，不能再从内容hash虚构DocumentID。UploadService只保存原件；文档/草稿创建需要权威元数据事务。
- 解析流水线返回DraftBlock候选，不再自行生成“已冻结Revision”。提交/审核时由既有治理契约产生不可变版本及双摘要，仓储负责行版本。
- LocalBlobStore需要注入可信Authorize，按空间/单KB隔离物理目录；Open核对持久化BlobRef并在完整读取EOF时核对实际长度/摘要。读取前/写入完成时检查预算/范围期限，不能把传入Scope视作认证证明。
- API需要可信应用服务的Prepare；搜索还要求Verify在输出前做当前权限、发布、批准及来源复核。测试使用替身，尚无真实控制源和QueryRead提供者。真实认证不得从客户端参数拼装。
- Bootstrap仅监听127.0.0.1并设请求时间限制；健康端点可用，未配置授权/治理服务的业务端点返回503，未开放可直接写入或检索的匿名业务入口。
- 当前/api示意接口尚未实现正式伙伴OpenAPI映射和MCP。内存索引是关键词子串实现，不能据此声明混合检索质量、Recall@10≥90%或生产性能。

## 仍需完成的验收

1. 真实身份/权限源、当前元数据视图和批准规则挂接，替身不能关闭AUTHORITY/CHECK门槛。
2. 原件存储的操作账本、同键幂等、删除重复调用、崩溃恢复、孤儿清理、迁移和回退。当前本地数据文件与sidecar没有跨文件事务；异常中断可能留下孤儿，必须在生产准入前补齐恢复协议。
3. Parser/Splitter/Blob/索引实现对应的Manifest、Factory、依赖装配和双实现替换；直接使用接口只证明编译兼容。
4. 上传→权威草稿→编辑/提交/批准→隔离索引→发布/撤回的真实事务闭环；不允许将内存投影Published标志充当批准来源。
5. 正式REST/MCP DTO、查询准备和当前输出复核；真实服务并发权限变化、来源核验及发送边界测试。
6. 中文代表评测集、混合召回/重排、固定环境下Recall/延迟/成本与压力测试。

下一阶段沿这些缺口实现真实治理与装配，而不是继续绕过平台契约增加匿名API。当前可继续模块开发，完整运行验收保持未通过。


## 解析与分块的受控工厂接入

[adapters.MarkdownFactory/SplitterFactory](../../pkg/adapters/processing.go)提供内部kernel.Factory：解析器声明format.markdown/text.preserve/source.characters，通过blob_store依赖槽要求BlobStorage 1.0及stream.read；分块器声明strategy.characters/offset.unicode/id.deterministic。工厂不接受供应商对象穿过领域接口。

解析配置仅接受max_bytes整数，范围1..8MiB，拒绝缺失、重复/未知字段、浮点表示和超额；分块模块没有实例配置，严格接受空对象，实际策略绑定在SplitRequest/流水线Profile。校验在依赖资源创建前完成。关闭拒绝后续接口调用；正在执行的调用仍由host排空和原context取消，解析器不自行关闭共享文件存储。

两项Go集成测试通过标准接口实际上传/读取本地Markdown并分块；切换显式依赖到first/second实例时记录实际Open命中实例，确认没有走错全局绑定；关闭旧接口失败。负向测试覆盖6类配置及缺少stream.read声明，确认零资源创建。两实例均使用同一种本地存储实现，不等同两种独立引擎或存储的迁移/替换验收。

此接入只适用于内部resolved assembly plan，尚未接入正式部署Schema归一化、BuildLock/artifact验证或memorys-server业务bootstrap。正式Manifest、Profile覆盖及外部模块协议映射仍需同步；不得仅凭工厂Manifest结构声明生产接入完成。


## 本地上传幂等账本局部实现

[upload_journal.go](../../pkg/storage/upload_journal.go)按存储namespace（空间/KB）及主体/动作/Idempotency-Key定位操作记录。操作键摘要与请求摘要使用不同域；请求语义包含ExpectedSHA256、Size和MediaType，不含临时ScopeID/RequestID。语义输入拒绝非法UTF-8，防止JSON修复产生标识碰撞。重试重复的是同一内容承诺；重放返回原结果，不再次消费请求流或创建新文件。

O_EXCL创建持久化pending记录并Sync后开始流式写入，记录预分配随机BlobID；数据/sidecar完成并Sync后追加complete并Sync。完整相符的记录跨实例重放；同键不同承诺ErrConflict；未完成、截断或异常记录ErrPending，不另开重复操作。已知无文件效果的失败清理预留记录，已产生文件效果的失败保留pending供恢复。操作记录不保存密钥和正文。

三项Go测试覆盖跨实例重放（且不读取流）、当前ScopeID/RequestID变化不改变操作、不同主体隔离、同键变更冲突、pending记录拒绝、并发写入预留、删除后重放不复活内容。pending试验通过截断complete记录模拟，尚未进行进程强制退出、断电/目录持久化或网络文件系统试验。

当前只落实幂等与未知状态阻止重复的局部机制；自动恢复、对账、孤儿清理、重复删除、原件关联元数据事务、跨文件原子性及迁移/回退仍未完成。complete重放可能返回已删除对象的历史操作结果，随后Open仍按当前权限/存在状态拒绝，不自动补写原件。真实授权源尚未接入，此记录不作为访问允许证明。


## 本地删除记录与可重试清理

[delete.go](../../pkg/storage/delete.go)要求当前授权及完整BlobRef匹配，先写入/Sync删除记录，再以独占硬链接安装到稳定.deleted路径，不替换已有标记；然后移除原件和sidecar，缺失文件视为已清理。并发安装遇到现有记录时重新验证权限，仅同一BlobRef继续。未支持硬链接的文件系统拒绝此实现，不退回非原子的覆盖写。

Open在读取元数据前拒绝有删除记录的对象；重复Delete跨实例继续清理，错误BlobRef冲突，授权被撤销仍拒绝。损坏的记录返回ErrPending并保持读取拒绝。上传complete结果重放不补写文件，数据-only恢复不能绕过已保留删除记录。Put/Open/Delete预算取父context、Request和Scope期限中的最早值；MediaType按MIME语法校验并限制长度。

两项Go测试覆盖跨实例重复删除、数据-only恢复仍不可读、清理重试、引用冲突、权限拒绝、8路并发删除及损坏记录拒绝。原测试发现Windows覆盖重命名产生并发错误，已用独占安装修复并通过；只验证当前本地文件系统，不证明全部部署文件系统或断电持久性。

该记录仅控制本地Blob读取/清理，不撤回Document发布、不删除检索/图谱/记忆索引，也不能追回已打开流返回的字节。业务删除仍须由权威治理服务组织撤回及传播。完整备份/迁移必须保留删除和幂等记录，当前尚无自动恢复/备份整体验证；生产文件系统准入与目录持久化要求保持开放。


## 内存索引删除与旧任务阻止

[MemoryIndex.Delete](../../pkg/index/delete.go)实现原生IndexDeleteRequest的删除语义：GenerationID必需；空ChunkIDs删除该指定代际，非空列表只删除其内指定分块。对应远程协议仍须使用明确mode，不允许省略目标的无边界删除。方法要求有限父预算、有效index.delete Scope和可信AuthorizeDelete；检查空间/KB归属后在锁内转变状态与清理。

Upsert首次记录代际所属Space/KB/Document/Revision/BackendEpoch，不允许之后把同一代际放到另一主体资源。Delete保留所属关系和代际/分块删除标记；重复授权删除继续成功，旧任务Upsert被拒绝。未知代际没有所属证明，不能默认当作已删除成功；当前实现仍需权威代际登记/持久化映射才能处理重启后对未知代际的恢复请求。

三项Go测试覆盖整代际删除重复执行/检索消失/旧任务拒绝、分块删除保留其余块/代际所属不变、缺失或拒绝授权/跨空间KB/未知目标/重复分块/错动作/取消且无数据变更。原过滤测试改为不同资源使用不同代际，符合代际唯一所属约束。

这里只补内存索引方法，不等于实现完整IndexWriter（Prepare/Verify尚无对应实现）或真实权限源。删除标记在重启后会丢失；权威withdraw事务、outbox与消费者幂等、持久化fencing、真实索引删除、图谱/记忆/缓存传播和重建时排除撤回内容仍待完成。真实业务需要先提交权威撤回，再根据事件做派生清理，不能用本地索引删除代替撤回。


后续权威撤回实施见[撤回事务与派生清理](WITHDRAW_TRANSACTION.md)：不能从当前Document重放历史结果，清理必须指向固定旧代际而非当前索引。现有历史仓储、清理计划及来源epoch缺口保持显式待办。
