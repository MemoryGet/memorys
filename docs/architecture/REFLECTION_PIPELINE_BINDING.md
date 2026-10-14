# 反思循环与知识检索应用的接入契约

本规范衔接[有限反思编排](REFLECTION_ORCHESTRATION.md)、[查询权威服务](QUERY_AUTHORITY.md)和[分库请求](PARTITION_REQUEST.md)。现有Loop、规划器、执行器和Fuser分别已有局部证据，但尚没有真实RoundSearch/EvidenceGate把它们串成应用闭环。本次是接入设计，真实适配器和端到端验证未完成。

## 1. 适配器归属与固定输入

RoundSearch和EvidenceGate由自有知识应用构造，不接受客户端提供的函数、权限结果或后端句柄。它们依赖已装配的QueryAuthority、Embedder/CompositeEngine、Retriever、Fuser和Reranker；编排框架不持有数据库/供应商的直接绕行通道。

一次请求固定Principal、原始问题、Pipeline/QueryProfile ID/版本/摘要、父RequestID及全请求预算。每轮可更新发布观察点，但不能静默替换策略、扩大KB范围或用新RequestID重置预算。配置变化需新请求或明确的受控重启，而非修改旧摘要。

首版若当前策略版本与父Scope不再一致，终止并要求重新准备请求；不在Gate返回nil后继续把旧Scope送给模型/远程模块。未来如果支持请求内Scope刷新，必须新增显式返回/绑定协议，保留原请求预算和追踪，不能由Reflector自行更新。

## 2. 初始轮和追加轮

| 阶段 | 初始轮 | 追加轮 |
|---|---|---|
| 权威准备 | 当前父授权与有效发布库存 | 同一主体/范围重新核验当前状态，构建本轮新快照 |
| 查询输入 | 原始问题 | Refine的已验证QueryPlan；未知/重复/缺必需路线拒绝 |
| 配额计划 | Build基础Plan | BuildRefinement，所有改写共享路线/KB配额 |
| 调用执行 | Execute基础工作池 | ExecuteRefinement共享工作池，记录query index |
| 路线结果 | 跨KB Merge | 每查询跨KB Merge，再MergeRefinement形成一份路线排名 |
| 融合 | 原Profile绑定Fuser | 同一Fuser；改写不能成为新加权路线 |
| 最终证据 | 已配置重排或合法直接投影 | 同一最终证据规则，不能返回每改写的独立top_k叠加 |

RoundSearch在任何强制阶段失败时返回错误，不用空证据伪装失败。可选检索路线按既有规则整路线降级，QueryAuthority/嵌入必需规格/身份/发布检查不能降级。轮次及具体降级原因写内部追踪，不能只靠Loop结果的insufficient猜测。

最终证据数量服从AnswerEvidenceLimit，评测以这些真正发送给用户/模型的片段计算Recall。追加轮获得旧证据不能重复计分；若业务需要跨轮保留证据，应定义版本化保留/重排策略，重新复核所有保留来源，不能把每轮列表直接拼接。

## 3. 新文本对应新嵌入

每个改写的文本必须按目标向量代际的完整EmbeddingSpec准备向量。缓存键至少包含精确查询文本、provider/model revision、维度、距离、归一化和preprocess版本；缓存归属与查询隐私另受租户/主体策略控制。不能复用原问题向量、只按维度命中缓存或令模型输出一串数字冒充嵌入。

目标代际不兼容时按完整规格分组，并在共享预算内调用；未知规格、模型不可用或维度/有限值不合法均失败。CompositeEngine可以在已验收内部嵌入路径消费文本，但同样必须接收目标前置过滤，不能为了改写放开knowledge_ids。

编排层只有标准Embedder及检索端口，不引用供应商模型类型。模型替换按新索引代际/规格迁移，不在追加轮期间把旧向量索引换成新模型。真实嵌入调用次数/usage和缓存命中需要计量，当前Loop尚没有该证据。

## 4. 每个子请求和返回结果

应用为每个query/route/KB调用分配独立Invocation，子Scope和发布投影对应新观察点；冻结RetrieveRequest后校验投影关系，再签发新Grant。旧问题、另一改写或另一KB的令牌不能复用。Scope/Publication/Query.Vector/Limit/deadline签发后不可再改。

候选必须逐项校验平台KB/版本/代际/Block范围和真实原文，拒绝未授权父块扩展与供应商内部ID引用。Context中的源文按数据处理，不能成为额外工具指令。模型Assess/Refine之前的Gate使用当前权威事实，不仅检查citation字符串非空。

重排返回的Evidence也要重新核验，尤其是重排器自行补上下文、改来源或省略必要片段的情况。Echo的SourceExcerpt与平台位置不符时强制失败；不得把“模型认为相关”当作来源合法。直接投影模式同样执行这些规则。

## 5. 全请求计量和输出边界

每轮执行器已有共享检索并发/字节预算，但全请求还须统一记录各轮检索、嵌入、Assess/Refine及最终生成调用。拟定请求预算包含剩余调用数、候选处理字节、模型输出tokens及费用上限；字段和usage契约尚未实现，不能宣称当前30秒时间限制同时保证费用。

每个阶段只得到父预算的剩余额度，失败/降级已经处理的成本不退还；内部框架重试也计入。跨请求/重启复用结果必须有已验收的持久预算/幂等协议，不能把内存Loop计数当作它。

Loop的sufficient只表示给定模型在合法证据上作出的判断，不证明答案正确；生成另走Generator和当前Gate。首版反思结束前不对外发送生成内容。EvidenceGate负责模型输入及最终输出前的当前核验，不能用仅启动时授权代替。

## 6. 端到端验收安排

| 门槛 | 所需证据 |
|---|---|
| REF-BIND-01 初始/追加贯通 | 真实权威库存、规划/调度、融合/重排和Loop连接，必需失败不伪装空结果 |
| REF-BIND-02 改写与嵌入 | 相同维度不同规格、文本变化和缓存隔离；新向量匹配真实代际，两个模型/引擎对照 |
| REF-BIND-03 授权与投影 | 每轮策略变化/撤回/切epoch，以及错子Scope/Grant/调用，均阻止旧证据泄漏 |
| REF-BIND-04 来源与重排 | 原件/Block跨度、父块扩展、错误excerpt、跨轮保留及重排改来源均核验 |
| REF-BIND-05 统一计量 | 全轮数/所有真实调用/重试/usage/费用可追踪，耗尽预算不扩额 |
| REF-BIND-06 效果替换 | 同一封存中文集最终Recall/引用准确性/延迟/费用对照，REST/MCP兼容 |

这些门槛尚未执行。下一步先建立类型化应用接入与计量契约，再完成真实权威/模型/检索适配器及端到端试验。局部Loop与ExecuteRefinement通过不关闭REF-BIND、REFLECTION或完整目标。

全请求usage、精确费率、预留/结算与未知状态细则见[统一计量](REQUEST_METERING.md)。时间上限不等同费用上限。
