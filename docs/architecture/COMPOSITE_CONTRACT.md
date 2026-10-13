# 复合检索引擎标准契约 V1

本契约补齐验收问题D-01。固定选择是：**复合引擎提供单一composite_ingest_search端口，不归一化成可独立替换的Embedder、IndexWriter和Retriever。** Go接口见 [CompositeEngine](../../pkg/contracts/ports.go)，请求见 [CompositePrepareRequest](../../pkg/contracts/retrieval.go)。

## 1. 职责与数据边界

复合引擎负责已批准内容的检索投影写入、内部嵌入/索引、准备验证和查询。平台独立保存原件、标准Block、审批、发布指针和引用；复合引擎不能调用发布命令，也不能将自己的publish状态作为批准事实。

四项操作为PrepareProjection、Verify、Delete和Search。PrepareProjection不接收外部预计算Vectors，避免声明后端未暴露的向量写入能力。准备和查询仍使用平台Generation/ReadinessProof/Candidate及作用域语义。

厂商确实提供独立能力时，可以另行声明并通过独立公共契约认证；不能仅因复合对象有几个方法，就将内部模块计为已实现细粒度替换。

## 2. 准备请求与证明

| 字段 | 权威来源及规则 |
|---|---|
| Request | 平台请求ID、deadline、固定Profile摘要；取消沿context传播 |
| Scope | 平台已验证的构建权限；客户端不能自填可信范围 |
| GenerationSetID、Generations | 当前KB/Revision/BackendEpoch下的候选通道目标；不能混用修订、epoch或规格 |
| OperationID、AttemptID、FencingToken | 平台发布操作与权威任务租约；每次外部尝试隔离，不能覆盖活动产物 |
| Chunks | 来自已批准标准内容的投影及Block范围；保持Revision、来源映射和内容校验 |
| ProjectionFormatVersion | 平台投影序列化版本，与IndexProfile一致；不是厂商内部格式号 |

投影变化但不改变权威内容时创建新代际。改变人工确认内容时必须创建新Revision并审核。引擎二次分块时保存映射；无法确定来源的片段不能形成正式Evidence。跨Block结果拆为可单独引用的证据。

向量通道必须声明并验证真实EmbeddingSpec（模型修订、维度、距离、归一化与预处理）。内部query_text嵌入路径可以省略Query.Vector，但不能省略对目标规格真实性的核验。规格未知的后端只用于探索，不通过正式向量通道认证。

PrepareProjection返回按目标通道记录的ReadinessProof；Verify验证实际可搜索性、内容数量/映射、规格及前置过滤。既有ValidateReadyGenerationSet只验证字段关系，不能替代Verify、审批、权限、fencing和发布CAS。

## 3. 查询与删除

Search使用标准RetrieveRequest。Query.Text是基础输入；只有已验证的query.vector特性才允许传入预计算向量，且规格须与目标代际一致。未声明该特性时拒绝向量输入，不静默丢弃或自动换模型。

space/KB/Revision/Generation过滤在候选选择前生效；空合法发布集合直接返回空结果，不调用后端的空过滤路径。返回仍需应用层复核。一次混合召回返回一份排名列表，每条Candidate保留所有来源GenerationIDs，不能拆成两份相同排名增加融合权重。

Delete按IndexDeleteRequest指定的逻辑代际或Chunk集合处理，不扩大到整个物理collection。若多个逻辑通道共用一个对象，适配器协调引用/保留后删除；重复删除幂等，失败保留清理墓碑。撤回立即停止可见性，不等待后端删除完成。

## 4. 配置到内核的映射

| 层次 | 唯一映射 |
|---|---|
| 模块清单 | capability.name=composite_ingest_search；version/features按能力声明，整体替换限制记录在composite_groups |
| 内核Factory | Services[contracts.CompositeRetrieval]提供一个满足CompositeEngine全部方法的实例；没有自动独立能力别名 |
| IndexProfile | mode=composite_projection，处理bindings仅包含该能力，至少一条index_route绑定同一个复合实例 |
| QueryProfile | 复合引擎拥有的通道只能由同一实例召回；每个复合实例在该QueryProfile中最多一条召回路线 |
| 应用调用 | 对该路线Resolve[CompositeEngine]，准备调用PrepareProjection，查询调用Search；独立路线继续使用IndexWriter/Retriever |

整组替换时同时修改处理、复合写入和复合召回绑定，创建新BackendEpoch并重建，校验后切换与重启。不能仅替换其中一个绑定。

灵活扩展保留：同一IndexProfile可增加与复合通道不重叠的独立写入路线，例如图谱；QueryProfile可以增加相应独立召回路线。通道覆盖、代际/布局、作用域和全局预算仍需验证，扩展不得借用复合通道的产物。拆分原复合引擎负责的通道属于替换方案迁移，不能作为一次绑定更新处理。

正式部署归一化器仍待实现。它必须保留经过验证的Profile实例引用，不能把这些映射降为全局默认binding；本契约不代表归一化或真实WeKnora已经集成。

## 5. 当前验证证据

- Go内核两个测试覆盖两假设提供方整体替换、强类型解析、不产生独立索引别名、资源回收及部分能力拒绝。
- 离线5项复合试验覆盖完整配置通过、拆开写入/召回拒绝、相同结果重复融合拒绝、独立图谱通道扩展通过。
- 现有8项文件级检查和原装配场景继续通过。

运行命令：

```powershell
& '.\.tools\go\bin\go.exe' test -cover ./...
& '.\.tools\go\bin\go.exe' vet ./...
python -m unittest discover -s docs/architecture -p 'test_*.py'
python docs/architecture/check_assembly_semantics.py
```

这证明契约映射和假设装配规则，不证明真实投影保真、过滤规模、后端规格、准备结果或迁移回退已通过。真实门槛仍执行 [WeKnora试验](WEKNORA_ADAPTER.md)。
