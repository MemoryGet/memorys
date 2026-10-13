# 嵌入、检索和重排的进程内/远程映射

本交付继续推进D-03。实现见 [embedding](../../pkg/modulewire/embedding.go)、[retrieval](../../pkg/modulewire/retrieval.go)、[reranking](../../pkg/modulewire/reranking.go)；全部是DTO编解码/关系检查，不调用真实模型或检索后端。

## 1. 端口和字段对照

| 端口 | Go→wire映射 | 校验与边界 |
|---|---|---|
| EmbedRequest | RequestContext→context；Texts/Spec→input.texts/spec | Go原端口没有Scope字段，传输适配器须显式注入已验证Scope，不从texts推导身份 |
| EmbedResult | Vectors及完整EmbeddingSpec→result.vectors/spec | 数量对应文本，维度对应Spec，模型修订/距离/归一化/预处理完全匹配 |
| RetrieveRequest | Query/Publication/Generations/BackendEpochID/RouteID/Channels/Limit→相应input字段；Scope→context.scope | 保存完整发布过滤、目标通道和代际，不默默使用模块默认模型或默认KB |
| IndexGeneration | 所有10字段映射，包括state、backend_binding、readiness | 初稿缺少后3字段，本次补入内部机器契约；它们不是可信授权或真实准备证明 |
| Candidate | Citation、Chunk/Route/Retriever/KB、rank/score/score_kind、generation_ids、text | 全部来源代际保留；结果属于本次Scope/发布范围/目标代际；未知来源拒绝 |
| RerankRequest | RequestContext→context；Query/Candidates/Limit→input | 同样需显式注入Scope；仅消费已经复核的候选 |
| RerankResult | Evidence.Citation/SourceExcerpt/Rank→result.evidence | 来源必须存在于输入候选；片段只能来自该候选文本，不允许新增事实或引用 |

BackendBinding的所有字段与ReadinessProof/GenerationReadiness完整映射。供应商标识仅出现在受控内部协议，不能进入伙伴Evidence/MCP；接收方不得把backend_id解释为任意可访问URL，也不能按用户提供的module_instance_id动态选服务地址。

状态、准备证明与Scope只是传输数据。必须结合可信Grant、当前权限/发布状态、固定模块地址、目标后端布局及真实Verify复核；不能因JSON包含state=ready就允许发布或读取。

## 2. 数值与过滤规则

Vector对应平台float32。有限、范围内JSON数值按IEEE-754最近值/偶数舍入转换为float32，编码使用该dtype的最短JSON表示。拒绝溢出、NaN/Infinity和非零数值下溢为零。这是已声明的派生向量dtype转换，不用于角色版本、ID或权威精确数字。模型/向量库替换仍须声明一致的规格与处理策略。

float64分数使用可往返的最短数值表示；大整数使用精确十进制处理。与解析映射相同，禁止未知字段、非法UTF-8、错误联合内容或响应关联标识。

检索请求的space必须等于Scope，KB为允许KB子集；Revision/Generation过滤非空，目标Generation属于声明的KB/Revision/epoch/通道。所需通道必须有对应目标，代际ID不能重复；向量维度和Spec一致。空有效版本/作用域由应用直接返回空结果，不发送无过滤RPC。

候选数量不超过Limit；KB、版本和每个来源generation_id都在本次允许目标内，且彼此归属一致。此检查只能发现字段越界，不证明ID的实际数据库归属、当前授权、前置过滤执行或片段与原文一致。应用返回前的证据复核仍必需。

重排返回不超过Limit与输入数量。Citation须匹配输入候选，source_excerpt是该候选文本的片段；这不替代来源范围和真实Block对照。候选和Evidence不能作为自动发布事实。

## 3. 执行证据

Go测试覆盖完整嵌入/检索/重排往返，含float32、Unicode、完整规格、私有绑定/准备证明，以及错模型、错维度/数量、空或扩大的过滤、错epoch、重复代际、越界来源和新增重排文字的拒绝。

[check_retrieval_wire_mapping.py](check_retrieval_wire_mapping.py)分别构造三个请求/响应，先经独立Python Schema验证，再调用实际Go离线工具转为平台contracts类型并重新编码，最后复核Schema与规范字节。含超过2^53的权限版本、来源锚点、Binding与ReadinessProof；另拒绝不在过滤集合中的来源代际。

```powershell
python docs/architecture/build_remote_contract.py --check
python docs/architecture/check_retrieval_wire_mapping.py
& '.\.tools\go\bin\go.exe' test -cover ./...
& '.\.tools\go\bin\go.exe' vet ./...
```

离线工具沿用architecture-parser-wire名称，新增operation选择embedder_embed/retriever_search/reranker_rerank；没有服务器或网络执行。当前编解码仍有4 MiB参考限额，不替代生产大载荷协议。

## 4. D-03剩余门槛

索引prepare/verify/delete、复合prepare、文件二进制、生成SSE、控制状态/取消及安全错误的完整Go映射仍待补齐。服务认证/Grant、调用账本、当前状态闸门和真正HTTP行为未实现。真实嵌入质量、检索过滤、Recall与后端替换均未验证，D-03及整体运行验收保持未通过。
