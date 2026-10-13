# WeKnora 复合适配器边界与验证路线 V1

## 1. 固定源码证据

本次将 v0.8.2 解析到 commit `3e8b0bfc80b845b2d4b2ed683994748741450a97`，保存了5份源码/API资料的SHA-256，见 [核验清单](evidence/weknora-v0.8.2-source-review.json)。以下是源码/API核验，未部署运行，不证明过滤、性能和迁移已通过。

| 已确认入口 | 证据 | 对平台的意义 |
|---|---|---|
| Markdown 手工知识写入 | [知识API](https://github.com/Tencent/WeKnora/blob/3e8b0bfc80b845b2d4b2ed683994748741450a97/website-docs/04-api/02-api-knowledge.md)、[Handler](https://github.com/Tencent/WeKnora/blob/3e8b0bfc80b845b2d4b2ed683994748741450a97/internal/handler/knowledge.go) | 存在已审核文本进入引擎的候选路径；不是任意标准Block/向量直接写入的证明 |
| 混合检索带 knowledge_ids、query_embedding 和 skip_context_enrichment | 同一知识API | 平台发布对象映射可转成过滤集合；实际前置过滤与规模必须测试 |
| 分块分页读取、带版本编辑、历史及回滚 | [分块API](https://github.com/Tencent/WeKnora/blob/3e8b0bfc80b845b2d4b2ed683994748741450a97/website-docs/04-api/02-api-chunks.md)、[Handler](https://github.com/Tencent/WeKnora/blob/3e8b0bfc80b845b2d4b2ed683994748741450a97/internal/handler/chunk.go) | 读取可用于对照；编辑会更新当前分块并重建，不能替代平台不可变发布协议 |
| Chunk 含字符位置、父块、图片和metadata字段；SourceContent/ContextHeader不直接JSON输出 | [Chunk类型](https://github.com/Tencent/WeKnora/blob/3e8b0bfc80b845b2d4b2ed683994748741450a97/internal/types/chunk.go) | 分块返回不能被当作完整原始解析导出；需要来源映射和完整性试验 |

此前“标准内容往返待验证”进一步拆为：已确认有文本写入入口；无损表格/图片/定位、独立分块写入、索引准备证明与精确过滤仍未证明。不能把这四项合并成已支持。

## 2. 接入路线：标准内容作为检索投影

平台先独立保存 Draft/Revision/Block/Blob。经审核的 Revision 转换成检索投影并写入私有 WeKnora 后端。投影不是权威内容，原始表格、图片、引用锚点与人工编辑历史仍以平台为准。

首轮对照两种投影粒度：

| 粒度 | 平台与引擎映射 | 优点 | 代价/门槛 |
|---|---|---|---|
| 单Revision一个手工知识对象 | Revision/attempt → knowledge_id；chunk → Block范围映射 | 对象少、保留文档上下文 | 自动分块/格式变换后的来源对齐必须准确；新增Block和人工表格可能难映射 |
| 单标准Chunk一个手工知识对象 | Chunk/attempt → knowledge_id，再读取其内部chunk | 来源映射更直接，适合不可变证据投影 | 对象和过滤集合膨胀、后端可能二次分块；需容量与完整性测试 |

文本先转换成规范化Markdown投影并保存 ProjectionManifest，记录projection_hash、平台Block范围、平台与投影偏移转换、序列化版本及原件附件映射。表格可用于检索的文本化投影不取代原始单元格数据。无法确定映射的后端结果拒绝形成正式引用，不能靠模型猜位置。

如果投影过程损失了要求保留的检索含义、来源映射不能可靠恢复或后端二次分块无法验证，就改用独立 IndexWriter/Retriever 路线。WeKnora仍可承担通过公共契约测试的其它能力。

## 3. 候选准备和发布

1. 每次处理尝试创建独立后端对象，记录在 attempt 的 BackendBinding；绝不调用已活动对象的编辑或重解析端点。
2. 用经批准的标准内容生成候选投影，等待处理/索引状态并读取分块做完整性对照。后端status=publish只表示其内部索引行为，不能作为平台发布批准。
3. 创建准备证明：分块内容/数量、平台映射、各必需召回通道可查询、精确过滤及后端规格匹配。仅有“解析完成”不足以准备成功。
4. 平台按既定CAS流程选定获胜attempt的GenerationSet。未选中的对象不在正式允许ID集合内，按窗口回收。
5. 后端部署仅允许平台服务访问，关闭对用户直接开放的原生UI、聊天、MCP和检索入口，避免绕过平台审核与权限。

平台写入幂等键不能假定是后端创建API原生支持的参数。网络超时后创建结果未知时保存 uncertain 状态，通过可验证关联键/对象清单协调；无法唯一识别时允许孤立重复候选但不得激活未知对象。不能凭标题相同就认为找到了同一个版本。

## 4. 正式查询映射

先授权并加载各KB有效Revision/GenerationSet，将绑定转换成非空knowledge_ids。没有有效对象时直接返回空结果，不能发送空列表并依赖后端解释。跨KB分别调用、再在平台融合；不能把一个KB的knowledge_id集合发给另一KB。

首版默认skip_context_enrichment=true，resource_urls使用handle语义，资源通过平台Blob/授权代理转换。开启后端上下文扩展需另行证明父块/图片/关联证据都位于同一批准版本和允许范围。所有返回内容仍校验平台发布状态；未映射的ID、变动内容哈希和未知来源拒绝返回。

只在后端模型规格一致且验证query.vector特性时发送预计算query_embedding。经验证的内部query_text嵌入可以归为复合能力，不宣称Embedder独立替换已实现。若内部模型规格无法核实，query_text路径仅用于探索，不通过正式向量通道认证；不能绕过GenerationSet的规格核验。完整端口见 [复合契约](COMPOSITE_CONTRACT.md)。

## 5. 试验门槛

| 试验 | 输入 | 通过要求 |
|---|---|---|
| W-01 标准文本写入 | 中文、首尾空白、换行、标题、人工改写 | 所有返回片段可映射平台Block；内容变化被识别，引用不冒充原件 |
| W-02 结构化投影 | 合并单元格、单位、图片及OCR说明 | 检索证据含义可恢复；平台读取保留原结构；不泄露图片直链 |
| W-03 候选隔离 | 旧有效、新候选、撤回和跨KB对象混合 | 允许ID过滤在候选选择前生效，无草稿/撤回/越权内容 |
| W-04 准备失败 | 索引失败、单路失败、创建超时、迟到Worker | 旧有效版不变；未知/孤立产物不能激活 |
| W-05 内容对齐 | 两种投影粒度、重分块、相同文本不同来源 | citation唯一归属正确；不靠文本相同猜所属版本 |
| W-06 规模限制 | 递增允许ID集合和文档数量 | 记录请求上限、超时、召回变化；超限明确失败或切换已验证分区方案 |
| W-07 迁移回退 | 标准内容重建到另一引擎 | 公开文档/版本/引用不变，含新写入和撤回的回退验证通过 |

这些试验在架构优先阶段先明确输入、结果与可否使用的门槛。真实部署与执行记录属于后续兼容验证，当前不能把来源核验当成试验通过。
