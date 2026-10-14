# 分库检索请求与授权边界

本规范衔接[检索协调器](RETRIEVAL_COORDINATOR.md)、[Grant绑定](ACCESS_GRANT.md)和[检索wire映射](RETRIEVAL_WIRE_MAPPING.md)。目标是将内部Call转换成标准RetrieveRequest，不把多KB的权限、发布对象或供应商ID混在同一个分库调用里。当前是设计交付，尚未实现真实授权/发布仓储及请求适配器。

## 1. 权威输入与处理顺序

```mermaid
flowchart LR
    A[当前主体与授权范围] --> B[有效发布与代际快照]
    B --> C[有界Plan / Call]
    C --> D[目标KB的子授权与发布投影]
    D --> E[规格 / 布局 / 路线核验]
    E --> F[冻结RetrieveRequest]
    F --> G[新Invocation与Grant]
    G --> H[已装配检索模块]
    H --> I[候选来源与当前状态复核]
```

Call只包含目标KB/epoch、路线/实例、配额及required，不能证明权限或发布。主体、授权结果与出版事实来自平台仓储，客户端不直接提交它们。应用根据Call核验QueryProfile路线、通道及真实模块能力，不凭字段名字猜测后端支持。

子请求准备失败时按授权/发布/配置失败终止，不归类为ErrUnavailable。传输异常的可选降级发生在正确准备的请求已经进入适配器之后。当前Execute的SearchFunc必须由这个受控应用层提供，而不是公开扩展入口。

## 2. 字段映射与收窄约束

| RetrieveRequest字段 | 来源与不变量 |
|---|---|
| Request | 保留父RequestID及已冻结Pipeline摘要；deadline取父剩余预算、检索阶段上限和Scope有效期的最小值 |
| Query | 平台查询文本或已按目标EmbeddingSpec生成的向量；禁止把一个规格的向量用于另一个代际 |
| Scope | 经授权服务确认的目标KB子范围，Subject/Space/Action与父授权相同；KnowledgeBaseIDs恰为目标KB |
| Publication | 目标KB的有效发布投影；Space一致，KB列表恰为目标KB；RevisionIDs与GenerationIDs非空且只来自该投影 |
| Generations | 每项KB和BackendEpochID与Call相同，RevisionID在发布投影中，ID在GenerationIDs中，Channel属于该路线 |
| BackendEpochID / RouteID / Limit | 来自冻结Call，不能由用户扩大或由后端改写 |
| Channels | 来自冻结QueryProfile路线；每个通道都有目标有效代际，缺通道不能作为成功空结果 |

Generations与Publication.GenerationIDs必须集合相等；Publication.RevisionIDs必须等于选定Generations所覆盖的版本集合，不能保留其他KB的对象。列表使用稳定顺序且去重，重复代际或同KB多个epoch混入同一请求时拒绝。空授权、空发布、空代际不发送给后端：授权为空是拒绝；经完整权威查询确认无可检索发布对象时，在规划阶段形成显式零目标路线。

Generation.State=ready和ReadinessProof只是必要结构条件，应用还须核验该代际属于当前有效BackendEpoch/GenerationSet及原批准版本。BackendBinding与供应商布局的对应由受控适配器确认；写入器与检索器实例ID可以不同，但必须有已验证的兼容映射，不能仅凭同名通道通用。

CompositeEngine文本查询路径仅在已验收的内部嵌入能力下使用；它仍接收完整前置过滤。向量路径要求Query.EmbeddingSpec与所有目标向量代际规格一致，且维度/有限值有效。不兼容的KB分组生成不同查询向量并分别调用，不能把维度相同当作模型相同。

## 3. 子Scope与快照身份

不能直接修改AccessScope.KnowledgeBaseIDs并将其宣称为新的授权结果。授权服务应根据父主体/策略和目标KB返回经确认的子Scope，记录父请求/父Scope关系；子ScopeID由该服务分配或采用已有获准的派生规则，不能由检索适配器随意生成。其有效期不得晚于父Scope，策略版本和当前主体必须核对。

Publication.SnapshotDigest由平台发布快照服务提供并定义其投影身份，调用方不得剪裁列表后沿用一个语义不明确的摘要，也不能让供应商计算该摘要。快照服务必须明确关联父QuerySnapshot、目标KB/epoch、选定Revision/Generation集合与投影版本；冻结后的请求可据此追踪。拟定关系记录和摘要语义见[发布投影身份](PUBLICATION_PROJECTION.md)；服务、Schema和运行记录尚未实现，不能声称快照真实性已完成。

Request.ProfileDigest仍是已冻结Pipeline身份，不用子过滤摘要替代。Scope/Publication投影、Request与Call关联全部进入平台内部追踪；公开引用继续仅使用平台Document/Revision/来源位置。请求准备之后发生撤回、权限变化或epoch切换，由后端接收门与返回前当前状态复核处理；旧快照不能覆盖当前拒绝。

## 4. 新Invocation与Grant

每个子调用分配独立InvocationID，ModuleInstanceID来自Call.RetrieverID的已装配绑定，实际调用服务身份必须与它对应。父RequestID可以共同追踪多个子调用，InvocationID不能跨目标/重试复用为同一个已接受的调用；重试和账本规则仍服从现有[Grant规范](ACCESS_GRANT.md)。

冻结最终RetrieveRequest后按正式DTO计算request_digest和完整scope_digest，再为目标动作/方法/路径、实例、Invocation、Profile、deadline及有效期签发新Grant。旧父请求或另一KB的令牌不得复用。裁剪Scope、Publication、Query.Vector、Limit或deadline都会改变绑定输入；令牌生成之后不得再变动这些值。

进程内调用不要求伪造HTTP令牌，但仍必须由同一受控应用完成授权/发布准备和返回前检查；不能因内部调用绕过业务边界。远程SDK验签成功也不代替服务身份、当前策略与发布检查。模块适配器不得在权限/契约失败时返回ErrUnavailable来触发可选降级。

## 5. 验收门槛

| 门槛 | 必须取得的证据 |
|---|---|
| PART-01 集合隔离 | 同时含两个KB、旧候选epoch和撤回版本的权威库存；各子请求只包含目标有效对象，错误集合预检拒绝 |
| PART-02 子授权来源 | 子Scope确由当前策略产生；父范围外KB、过期策略、人工篡改列表均无法得到可调用请求 |
| PART-03 发布投影身份 | 投影记录与父快照/目标/对象集合一致，正文/版本/代际可追溯；剪裁后冒用旧摘要拒绝 |
| PART-04 嵌入/后端兼容 | 相同维度不同模型、错误通道/布局/epoch均拒绝；两种真实后端使用同一契约 |
| PART-05 Grant重新绑定 | 实际签发/验签新子调用；复用父/其他KB令牌或签发后改Limit/过滤/deadline均拒绝 |
| PART-06 当前检查 | 准备与返回之间的权限撤销/知识撤回/epoch切换不能泄漏过时证据 |

以上为新设计门槛，尚未执行。现有Go配额/合并/工作池和modulewire字段检查不能证明权威事实、子授权、投影身份、签发账本或实时复核；COORD-01/05与整体架构验收保持开放。

投影与实际Go RetrieveRequest的局部关系检查见[映射验证](PUBLICATION_PROJECTION.md#10-实际retrieverequest关系检查)。它不替代上述权威输入、子授权与当前状态门槛。
