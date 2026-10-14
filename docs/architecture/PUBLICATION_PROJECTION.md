# 发布投影身份与分库过滤契约

本规范细化[分库请求](PARTITION_REQUEST.md)的PART-03。发布投影由自有应用和权威仓储生成，检索后端只消费投影，不决定哪些知识已发布。当前是V1草案设计；闭集Schema和离线关系校验已实现；Python/Go摘要金标已有局部证据；真实快照服务和运行验证尚未完成。

## 1. 三种身份分别承担什么职责

| 身份 | 内容与用途 |
|---|---|
| 父QuerySnapshot | 固定授权/发布观察点、KB epoch映射、GenerationSets及QueryProfile，供本轮规划与跨KB追踪 |
| 父发布摘要 | 现有QuerySnapshot.PublicationFilterDigest，标识父发布事实；不是子请求过滤集合的摘要 |
| 子投影摘要 | 对本规范完整投影记录的规范摘要，标识一个路线、一个KB/epoch和选定发布对象集合；填入子PublicationFilter.SnapshotDigest |

父摘要与子摘要不能混用。子范围变小仍需生成自己的投影身份；不是把旧PublicationFilter裁剪后继续使用父摘要。父摘要来自权威服务，子摘要内容相符也不证明父服务真实、主体获准或记录仍有效。

现有字段名SnapshotDigest保留，明确子RetrieveRequest中的该字段表示发布投影摘要。本规则仅适用于未冻结V1分库路径；历史单库请求若仍把父摘要放入该字段，必须在接入验证时显式转换/升级，不能假定语义已经兼容。后端不需要读取父发布摘要，平台追踪持有两者关系。

## 2. 子投影逻辑记录

发布快照服务产生不可变PublicationProjectionV1，包含以下闭集字段；字段已由[草案Schema](schemas/publication-projection.v1.schema.json)定义，仍未冻结或用于生产接入。

| 字段 | 约束 |
|---|---|
| schema_version | 固定整数1 |
| projection_id | 平台分配稳定唯一ID，按准备尝试产生；不使用供应商ID |
| request_id / invocation_id | 关联本次父请求和唯一子调用 |
| parent_publication_digest | 权威父QuerySnapshot.PublicationFilterDigest |
| query_profile_digest / pipeline_digest | 分别来自已核验QueryProfile和Pipeline，不能互相替代 |
| parent_scope_id / child_scope_id / policy_version | 由授权服务提供的父子关系及子策略版本 |
| space_id / knowledge_base_id / backend_epoch_id | 唯一目标，必须在父快照及子授权中有效 |
| route_id / module_instance_id | 冻结路线和真实装配实例，绑定通道/布局兼容性核验结果 |
| channels | 去重、规范排序的路线通道，不能为空 |
| entries | 目标有效发布对象的完整关系记录，不能为空 |
| expires_at_unix_ns | 精确整数时间，不晚于父请求deadline和父子Scope有效期 |

每个entry包含document_id、revision_id、publication_version、generation_set_id、generation_id、channel、index_profile_digest。版本来自权威Publication记录，generation_set/代际来自当前目标epoch的有效映射。版本为正整数；所有ID非空，摘要为规范SHA-256十六进制。每个generation_id唯一，不能被重复计入多个版本/通道。

entries规范排序键为(document_id, revision_id, channel, generation_id)，ID比较采用UTF-8字节序；channels使用固定顺序keyword、vector、graph。拒绝重复和未知通道，而不是静默覆盖。每个声明通道至少有一个选定entry；每个entry通道均在channels中。同一Document最多一个当前发布Revision，不能同时纳入旧版与新版。

projection_id、Invocation及有效期纳入摘要，因此相同内容的新尝试会有新身份。该身份用于追踪和绑定，不作为内容去重键；缓存内容需另外使用正文/版本/模型等键，并再次执行权限与发布检查。投影不含私钥、正文或供应商内部ID。

## 3. 从记录到RetrieveRequest的等式

Publication.SpaceID等于space_id；KnowledgeBaseIDs恰为[knowledge_base_id]。RevisionIDs是entries.revision_id的去重稳定列表；GenerationIDs是entries.generation_id的稳定列表。Generations与entry逐项一一对应，KB/epoch/Revision/Channel/IndexProfileDigest分别相符，且平台已校验Readiness及后端映射。不能只验证两个列表分别非空。

Request.RequestID、ProfileDigest与投影对应；modulewire.InvocationID/ModuleInstanceID与投影对应；Scope必须由授权服务提供并与child_scope_id、主体、空间、策略及唯一目标KB相符。Request deadline不得晚于投影有效期；投影有效期同时不晚于原父预算。即便应用后来选择更短deadline，也须重新计算最终请求绑定Grant。

本记录没有查询文本/向量和Limit；它定义发布集合及目标关系。这些字段由冻结Call/模型准备提供，并由完整request_digest绑定。不能因投影摘要不含Limit就允许签发后修改Limit。不同IndexProfileDigest的代际可以按真实关系存在于同KB发布库存，但是否可在同路线使用必须经过布局与EmbeddingSpec核验，不仅比较摘要名字。

## 4. 摘要与生成顺序

本V1草案固定域为`akos.publication-projection/1`，摘要输入是完整记录经既有AKOS-CJSON/1规范化后的bytes，前缀为域UTF-8加单个NUL。摘要输出字段不包含在记录输入中。精确时间/版本保留整数，不经过浮点；进入摘要前完成列表规范排序与关系验证。

顺序为：分配Invocation → 子授权确认 → 发布事实读取/一致性验证 → 建立投影记录 → 生成子摘要与PublicationFilter → 冻结完整RetrieveRequest → 签发Grant。不能先签发Grant再填投影摘要。生成期间授权/发布状态变化时撤销该准备结果，重新取得当前事实；不能自行修改不可变记录。

摘要域及独立金标已在下节补齐，并登记于[规范编码目录](FROZEN_CONTENT.md#6-新增投影摘要域v1草案)；实际仓储、请求映射与远程接入仍未完成，不能只凭摘要函数允许请求。

## 5. 一致性、存储与失效

父QuerySnapshot与投影来源必须属于同一平台一致性观察点，采用已有[一致性协议](CONSISTENCY.md)的版本/CAS核对；不能跨多个独立读随意拼接Publication与epoch。准备阶段不得依赖供应商返回的版本号作为权威事实。

投影是请求期不可变快照，可由追踪仓储按安全保留期限记录，不是新的知识发布记录。日志仅记录受控身份/摘要，记录正文属于平台权限范围。知识迁移的权威库存不因本设计自动新增记录族；若未来需要持久重放投影，须另定义导出/账本/保留契约后接入，不在当前导出清单中伪造已支持能力。

记录过期、新授权策略、撤回或epoch切换时，接收门与返回前检查决定是否继续。摘要没有自动撤销能力，也不是可长期复用的权限凭据。历史引用读取重新检查当前状态，不因曾有合法投影便永久允许。

## 6. 验证安排

先补闭集Schema和标准摘要金标，再验证记录与实际Go RetrieveRequest之间的字段/集合关系；最后通过真实仓储并发试验验证当前状态。至少覆盖：两个KB相同供应商ID、同文档旧新版本、同代际跨版本冒用、遗漏通道、重复代际、父子摘要替换、Invocation/实例/Scope错配、精确大整数、准备期间撤回与epoch切换。

PART-03只有在记录来源、一致性、精确摘要、请求映射与当前状态运行证据全部通过时关闭。当前完成了设计说明，没有上述运行证据；不可据此关闭PART-03、COORD-01/05或整体架构验收。

## 7. 首个机器契约与离线检查

`build_publication_projection.py`生成闭集Schema，限制最多1000项entry、3通道、ID长度200字符、uint64版本及正int64纳秒时间；记录raw载荷最多4MiB。超限拒绝，不静默裁剪，正式应用还须定义有效库存超过上限时的明确准入/分页策略，不能用此上限降低可检索资料范围后宣称完整召回。

`publication_projection.py`验证字段类型、UTF-8、稳定顺序、通道覆盖、代际唯一、Document当前Revision/Publication版本/GenerationSet一致、Revision与Document归属、GenerationSet与IndexProfile一致，以及每Document每通道唯一代际。原始JSON拒绝重复键、浮点数字、非有限常量和非法Unicode。报告authorized与ready_for_retrieval固定false；不验证摘要或当前时间/外部事实，也不产生可调用请求。

6项新检查覆盖闭集字段/严格整数、大整数精度、重复代际/旧版本/发布版本、排序与遗漏通道、集合归属/规格摘要以及raw JSON边界；全部167项Python契约回归通过。见[证据](evidence/publication-projection-schema-review.json)。请求过滤集合与实际Go DTO的对应尚未验证，不能把该检查当作PART-01/03运行通过；摘要、真实权威服务及当前状态门槛保持开放。

## 8. 固定域摘要与独立Go复核

`projection_digest(raw)`先使用上述PythonSchema/关系/raw校验，再计算固定域摘要；仍不证明真实来源。`pkg/publicationprojection.DigestUnchecked`复用Go canonical.JSON和标准SHA-256，固定同一域，只验证JSON对象/规范编码，故名为Unchecked。离线cmd/architecture-projection-digest输出schema_validated=false、authorized=false；例如空对象可被计算摘要但不成为合法投影。它尚不能直接接入生产准备流程。

[5组金标](examples/publication-projection-golden.v1.json)覆盖中文/emoji、超double整数、键序/空白、Invocation/有效期/父摘要变更。4项新Python检查包含实际编译Go工具与逐组摘要对照，结构错误不得取得Python已验证摘要，Go拒绝重复键/非法Unicode/多根JSON/数组根。全部171项Python回归、全Go测试与vet通过，见[证据](evidence/publication-projection-digest-review.json)。

金标样本为合成发布记录；没有真实权威事实、实际Go RetrieveRequest映射、父Scope/快照关系或实时撤回证据。摘要主版本和Schema仍属未冻结V1草案；PART-03与整体保持开放。

## 9. Go闭集加载与关系校验

`pkg/publicationprojection.Record/Entry`是平台投影DTO，采用独立闭集字段；不引用供应商类型。Load先经规范JSON检查拒绝重复键/非法Unicode/超预算，再检查根及entry的精确字段名/必需字段，拒绝encoding/json默认大小写别名，最后加载精确uint64/int64并执行Validate关系规则。返回的记录来自本次独立解码，不与原输入bytes共享可写数组。

ValidatedDigest先Load再计算固定域摘要，失败返回零记录/空摘要及静态ErrInvalid；不检查真实父Snapshot、当前时间/授权/发布，也不校验实际RetrieveRequest。记录加载成功只意味着给定JSON结构与本地关系成立。直接调用Validate接受Go整数值，但不能作为原始JSON闭集检查的替代。

离线工具增加显式`--validate`模式，成功输出schema_validated=true、authorized=false。默认DigestUnchecked模式继续明确schema_validated=false，不能被应用误作已核验发布投影。生产准备只可在权威事实与实际请求关系补齐后使用ValidatedDigest结果。

新增3项Python检查调用实际Go工具：5组金标经两种校验摘要一致；字段大小写/未知字段、浮点/布尔版本、排序/通道遗漏、旧版本/重复代际/GenerationSet规格关系冲突均双侧拒绝；uint64/int64越界、null、非法Unicode和零版本拒绝。全部174项Python回归、全Go测试和vet通过，见[证据](evidence/publication-projection-load-review.json)。没有实际快照服务或请求映射，PART-03与整体保持开放。

## 10. 实际RetrieveRequest关系检查

`ValidateRequest(rawProjection, CallIdentity, RetrieveRequest)`先严格加载并计算投影摘要，再比对实际RequestID/Pipeline、路线/epoch、Invocation/模块、计划Limit、子Scope ID/策略/空间/唯一KB，以及请求deadline与Scope有效期边界。CallIdentity来自受控计划，不代表已认证服务身份。

Publication的RevisionIDs和GenerationIDs必须恰为投影的去重规范排序集合，Scope及Publication只允许目标KB；Channels必须与投影相同。Generations与entries一一对应、不得重复，KB/epoch/Revision/Channel/IndexProfile相符，State=ready且Readiness.GenerationSetID相符。不静默裁剪或修复错误请求。父发布摘要不能冒充子SnapshotDigest。

本检查不验证当前时间、主体真实授权、父子Scope来源、父QuerySnapshot、完整ReadinessProof、EmbeddingSpec与后端布局或正文真实性；Query/供应商绑定仍须标准DTO和相应领域检查。投影包含Document/Publication版本，但RetrieveRequest无这些字段，必须从真实仓储核验其归属，不能仅凭当前字段相等证明已发布。

3项Go检查包括真实modulewire Encode/Decode往返后关系继续成立；10种过滤/代际错配和9种身份/计划预算/有效期错配均拒绝。全Go测试与vet通过，见[证据](evidence/publication-projection-request-review.json)。样本为合成记录，接入真实应用、子授权、当前撤回检查及新Grant签发仍待完成；PART-01/03与整体不因此关闭。
