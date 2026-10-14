# 模块装配与适配器协议 V1

## 1. 规范与验证边界

[模块清单 Schema](schemas/module-manifest.v1.schema.json) 和 [示例清单](examples/parser-manifest.json) 是架构交付物，用于约束后续注册中心设计。示例没有对应的生产解析器，不证明任何开源组件具备声明能力。

结构规范采用 [JSON Schema Draft 2020-12](https://json-schema.org/draft/2020-12/json-schema-validation)。结构校验可以检查必需字段、类型和取值，跨实例引用、版本协商和依赖环由装配语义检查完成；健康与效果由真实运行测试证明。三层验证不可相互替代。

模块能力标识规范化为 name + version，例如 `{"name":"parser","version":"1.0"}`。此前文档中的 parser.v1 是能力简写，不作为另一种机器编码。Manifest.contract_version 表示模块生命周期协议版本；capabilities[].version 表示各业务端口契约版本，两者分别协商。

实现版本首版固定为三段稳定版数字，不接受 latest、版本范围、预发布标签。开发阶段可以使用 0.x.y，并单独在构建锁文件保存源码 commit 和制品摘要；版本字符串不替代制品校验。厂商引擎版本是适配器的后端配置锁定项，不等于适配器自身版本。

## 2. 复合能力和状态归属

features 按能力声明，例如 retriever 的 prefilter.kb 不能被当作 graph 的过滤能力。清单只声明提供者的能力；平台 Profile 另外声明必需能力。只有提供能力包含全部必需项才可绑定，不能由适配器在运行时忽略不支持的参数。

composite_groups 声明必须整体替换的能力集合。集合成员必须存在于 capabilities，且同一个能力不能属于两个组。一个复合能力本身可以用单成员组表达，例如 composite_ingest_search。内部是否同时做嵌入和检索不重要，只有经端口测试证实的独立能力才能分别声明。

state_model 是整个模块的最高状态级别：任一能力持有权威数据就声明 authoritative；只持有可重建索引则 derived；完全无持久状态为 stateless。模块清单可拆成多个实现单元以支持更细的迁移，但不能用 stateless 隐藏供应商内部存储。

无状态模块不声明迁移方式；衍生数据至少支持 rebuild/export_import/copy_verify 之一；权威数据必须支持 export_import 或 copy_verify，不能只声明 rebuild。是否真的可以从平台权威数据重建，需通过独立恢复试验验证。

## 3. 装配配置的语义规则

BuildLock机器契约及部署/制品摘要核对见[受控构建锁](BUILD_LOCK.md)。当前仅有离线核对，正式归一化、运行制品及持久化epoch检查仍待实现。

现有配置示例是局部示例，不是完整部署。正式配置由以下对象组成：

完整结构见 [部署配置Schema](schemas/deployment-config.v1.schema.json)。bindings使用显式实例、能力、契约与特性要求；旧契约文档中的字符串绑定仅作概念示意，不作为正式可装配配置。

| 对象 | 字段 | 规则 |
|---|---|---|
| ModuleInstance | instance_id、provider、implementation_version、contract、settings、credential_ref、dependency_bindings | instance_id 全局唯一；provider/version 必须在受控制品锁文件中；settings 按模块 config_schema 校验 |
| DependencyBinding | dependency_slot → instance_id + capability | slot 必须出现在 Manifest.dependencies；绑定实例提供相同主版本且不低于 minimum_minor 的能力 |
| DeploymentBinding | role → instance_id + capability | 认证、授权、事务、审计、任务等由部署控制，空间无权覆盖 |
| IndexProfile | id、version、处理绑定、index_routes、策略 | KB 迁移生成 BackendEpoch；不存在配置修改即改写旧代际的路径 |
| QueryProfile | id、version、retrieval_routes、融合/重排/生成绑定、预算 | 只允许平台白名单选择；每路需与对应索引端兼容 |
| PipelineProfile | id、version、index_profile_ref、query_profile_ref | 引用已冻结子版本，任务和请求固定绑定 |
| BuildLock | provider、implementation_version、artifact_digest、源码版本、契约支持范围 | 受部署管理；不能由请求指定下载任意插件 |

缺省与空值分开：缺省可选依赖表示不用该能力；显式绑定但不可用默认启动失败，只有 Profile 明确允许相应降级时才能忽略。空 kb_ids 在业务请求中表示空集合，省略 kb_ids 才表示在该主体允许范围内搜索；客户端和传输适配器不得混淆。

语义检查顺序：

1. 清单/schema 自身合法；模块实例 ID、能力 name、依赖 slot 唯一。
2. 制品锁定、生命周期协议和业务端口主版本兼容，次版本满足要求。
3. settings 仅包含 config_schema 允许字段；密钥字段只能使用凭据引用，敏感值不进入普通配置。
4. 所有绑定存在且能力/特性匹配；索引写入器与检索器支持同一后端协议/数据布局。
5. 复合组在同一 Profile 内不得跨实例拆开；权威事务包必须整体绑定。
6. 依赖图无自环和循环；必需端口集合齐全；可选能力退化路径有定义。
7. 按拓扑顺序创建并检查健康，完成后才发布不可变 RuntimeSnapshot。

JSON Schema 的 uniqueItems 检查整个条目，不保证对象某一字段唯一；因此同名能力带不同 features 的重复项必须由步骤1拒绝。清单内 config_schema 本身也要再次作为 Schema 校验，不能只判断它是一个 object。

部署Schema已约束外壳和IndexProfile/QueryProfile.policy，策略定义位于同一文件的indexPolicy/queryPolicy。settings仍按每个模块的config_schema检查，拒绝未知字段。独立索引模式必须具备parser/splitter/embedder及各index_routes的index_writer；复合投影模式必须具备composite_ingest_search及已验证的标准内容映射。查询每路使用retriever或复合检索能力；启用问答/反思时对应能力必需。所有这些组合条件由语义校验器实施，不能仅凭Schema通过就接收业务请求。

平台保留的部署role必须绑定相同业务capability（metadata_uow、task_repository等角色映射在契约注册表固定）。授权、审批、审计、事务等role不能出现在Profile的bindings内；Profile白名单由部署管理。pipeline对子配置的id/version引用必须存在且不可变。任何IndexProfile变更都必须显式创建新版本并执行BackendEpoch流程，不将新配置直接套在旧索引上。

模块配置 Schema 及其引用必须随受控制品打包。运行装配不从任意网络地址加载 `$ref`；解析引用限制在制品内的 Schema 注册表。通用清单 Schema 只验证 config_schema 是对象，制品审核与配置验证阶段另检查其完整性和引用边界。

## 4. 生命周期和故障

生命周期：registered → validated → creating → ready → draining → closed。创建失败可以从 creating 进入 failed 并执行资源回收。运行期健康异常单独记录 healthy/degraded/unavailable，不任意重建整个依赖图。

Factory.Create 成功返回服务集合与实例句柄；失败时工厂负责释放尚未返回的局部资源。注册中心负责释放已成功返回的实例，严格逆依赖顺序。Close 必须幂等、有限时，前一个 Close 失败仍继续关闭其它实例，并聚合安全错误。

只有装配层能创建实例。依赖通过受限 Lookup 注入，只能获取声明并绑定的 dependency_slot；不向模块暴露任意全局服务定位器。启动期间不允许运行用户请求；RuntimeSnapshot 发布之后绑定只读。配置切换重新启动新进程，不在旧实例上逐字段修改。

健康检查分开报告“协议可用”“服务连通”“后端版本匹配”“关键过滤/事务能力验证状态”。一次 ping 成功不证明数据可见性或迁移完成；部署必须同时检查持久化 BackendEpoch 和配置摘要。

## 5. 扩展一个新模块的交付清单

1. 实现平台端口，提供清单、严格配置 Schema、健康检查和有界 Close。
2. 维护平台 ID 与后端 ID 的映射，转换来源、错误、分数和取消语义。
3. 声明支持/不支持的格式、过滤、流式输出及迁移方式，提供公共契约测试结果。
4. 有状态实现提供导出/重建、校验、切换、回退和故障恢复试验记录。
5. 提交依赖/许可证/后端版本锁定信息，使用受控构建加入注册中心。
6. 用同一客户端、同一数据和问题集完成替换试验；通过之后加入平台可选列表。

新增模块不应修改应用服务的供应商分支。若新增能力要求改变业务输入输出，应先升级标准端口并保留旧版本兼容，再实现适配器。

## 6. 当前验证证据

执行 `python docs/architecture/check_manifest_design.py`，需本机 Python 和 jsonschema 包。结果：清单 Schema 合法、2 个设计样例通过、12 个错误清单被拒绝，解析器 settings 的边界与未知字段检查通过。

错误样例覆盖未知字段、未锁定版本、空能力、缺失健康信息、有状态无迁移、权威数据只声明重建、无状态声明迁移、无效嵌套 Schema、同名能力、同名依赖槽、复合组引用缺失能力和负超时。

该脚本是文档规范校验器，未验证完整部署依赖图、所有复合组组合、外部引用加载策略、运行健康、迁移行为或检索质量。后续注册中心必须按本文完整规则实现，不得把这些样例通过当成模块已获生产认证。

部署结构校验执行 `python docs/architecture/check_deployment_design.py`：部署Schema合法、1个结构样例通过、23个错误配置被拒绝。样例的provider是未实现的结构占位，并非可启动环境。拒绝样例覆盖缺失授权、Profile中非法部署role、未知字段、未锁定版本、密钥字段、反思预算上限、未锁定子配置、未类型化依赖、策略限制、空路线和无效路线预算等。模块图、路线/策略交叉字段、能力/后端兼容性与持久化epoch一致性仍必须由后续运行装配验证。

## 7. 流水线策略语义

IndexPolicy冻结分块参数、EmbeddingSpec、必需索引通道和投影格式版本。QueryPolicy冻结召回候选规模、回答证据规模、RRF参数、重排与生成开关、阶段超时和可选图谱/记忆。默认必需通道为keyword+vector；具体Profile可以声明其他经验证的通道组合，正式质量仍用同一评测门槛验收。

下列条件无法仅凭结构Schema保证，装配和运行必须进一步验证：

- overlap_codepoints < max_codepoints；parent_child的parent_max_codepoints ≥ max_codepoints。分块参数不会强行截断表格含义，例外块通过明确策略处理并记录超限。
- answer_evidence_limit ≤ candidate_limit。用户top_k超过Profile的候选预算时明确拒绝，不无提示缩减。融合前各路候选预算、重排输入和最终证据规模单独观测。
- EmbeddingSpec对应确切的可用模型修订和后端布局，不接受latest或会被供应商静默替换的别名而无版本验证机制。复合引擎也必须取得可信规格，不能以配置中的自报值证明匹配。
- required_channels由IndexWriter和Retriever实际支持，readiness覆盖全部必需通道。QueryProfile不能把必需graph通道同时列为允许缺失的optional_capabilities；memory不能成为权威知识的必需替代来源。
- rerank/generation/反思开关启用时必须绑定相应端口；关闭生成的Profile可用于search/read，但ask明确返回capability_missing。图谱和记忆降级不得影响授权/发布/来源检查。
- 阶段超时是单次调用上限，不是新增总预算。实际deadline为客户端、服务器总预算和剩余时间的最小值；每轮反思消耗同一个剩余预算。不同阶段的最大值不能简单相加当作允许30秒之外继续执行的理由。
- V1融合策略预设为RRF，不比较原始供应商分数。增加新算法先登记版本化策略Schema和公共行为测试；不允许适配器偷偷忽略已声明的融合策略。

策略没有disable_acl、include_drafts、auto_publish或密钥字段。核心不变量不属于可配置的查询策略。模型调用凭据通过模块credential_ref注入，策略只能选择平台批准的绑定。

## 8. 多路索引与召回的装配

index_routes逐路声明route_id、channels和writer绑定；同一复合写入器可以同时提供keyword/vector产物。独立实现可将vector指向向量库、keyword指向全文引擎。单个Profile内每个通道只有一个权威写入路线；合并通道必须属于同一复合能力，不计作已独立替换。

retrieval_routes逐路声明route_id、channels、retriever绑定、candidate_limit和required。查询服务并行调用满足同一QuerySnapshot的路线，然后统一融合；同一路线在一个查询轮次仅向融合贡献一份全局排名列表；跨KB/代际分区先按受控策略合并，不能增加路线票数。复合混合召回返回一份排名列表，不能把它复制成两份假装关键词/向量两路从而增加融合权重。

索引绑定和召回绑定均有明确实例及契约，不能按渠道名称猜测连接。绑定实例可以不同，但必须由已验证的后端协议/布局兼容声明匹配GenerationSet；把一个Qdrant写入代际直接交给无映射的另一向量库检索器要拒绝装配。切换每条有状态索引路线仍走BackendEpoch迁移，纯查询排序实现替换走QueryProfile新版本。

语义检查补充：

- route_id在各自列表内唯一；index_routes覆盖全部required_channels且没有重叠通道。写入器不支持的通道不能通过配置声明出来。
- 每条召回路线的channels均存在于当前IndexProfile/GenerationSet；候选记录保留route_id、KB、代际、排名和来源。
- 必需召回通道有至少一条required=true的有效路线覆盖。可选路线失败必须显式记录，不能让失败的必需路线被可选降级规则掩盖。
- 每轮各路candidate_limit总和不得超过QueryPolicy.candidate_limit；跨KB扇出也纳入全局候选与并发预算。应用先计划预算和embedding规格分组，不能每个KB各拿完整预算无限放大调用量。
- fuser绑定是必需的，单路时可执行恒等排序；排序稳定的平局键为平台citation范围与route_id，不采用后端随机顺序。
- 可选图谱/记忆的来源仍经过发布/权限检查。后端query_text/查询向量路径都不允许由路线配置扩大AccessScope。

本次更新的是尚未投产的V1设计草案；旧的单一retriever绑定只作为早期概念示意。正式冻结后，路线Schema的破坏性变更必须升级版本，不能要求运行中的客户静默接受新必需字段。

## 9. 离线装配语义样例

Go内部装配内核现已实现，范围与生产接入门槛见 [内核设计](KERNEL_RUNTIME.md)。正式部署Schema到内部计划的归一化尚未实现；本节离线样例覆盖的流水线语义不能视为已接入运行内核。

执行 `python docs/architecture/check_assembly_semantics.py`：三个假设装配示例通过，依赖顺序正确；13个非法装配、一个依赖环和一个复合组拆分被拒绝。检查覆盖实例/配置版本唯一性、已声明能力、端口与生命周期主版本/最低次版本、特性门槛、严格settings、依赖槽与拓扑、启用能力、通道覆盖、复合组完整性和部分预算关系。

示例清单仅是假设声明，不代表reference-provider已实现。校验器不创建对象、不访问数据库/模型、也不证明自报过滤特性真实。尚未覆盖制品锁验证、真实后端布局、KB扇出和运行权限/迁移；复合组检查只覆盖按能力声明的完整性，不证明后端内部真的可独立替换。

复合组在每个PipelineProfile的部署绑定、IndexProfile处理/写入绑定和QueryProfile召回/处理绑定合并后校验。使用某实例组内任一能力，组内其它能力也必须有该实例绑定。并列使用其它完整后端可以保留，但仍需逐路线的代际/布局匹配；不能把相同capability名字视为相同索引协议。试验包含保持同一引擎完整绑定通过，以及仅把retriever换到独立实例、保留旧index_writer时明确拒绝。

首版事务包中metadata_uow/task_repository/audit使用同一实例的权威事务能力，outbox写入属于metadata_uow。event_bus和task_queue是投递适配器，可独立替换；不能为了原子提交要求把Redis/Kafka投递放进数据库事务。更细仓储拆分以后需要显式共同UnitOfWork协议并单独证明原子性。

## 10. 文件级离线配置检查入口

复合投影的Go端口与配置映射已补齐，见 [复合契约](COMPOSITE_CONTRACT.md)。除第9节场景外，`test_composite_design.py`另验证5项完整/拆分/重复融合/独立扩展场景。正式部署归一化与真实提供方仍待实现。

[check_config.py](check_config.py) 接受指定部署文件和一个或多个模块清单，复用第9节的语义检查。输入按UTF-8读取，每文件最多4 MiB、64层嵌套，拒绝重复JSON键、非对象根、非有限数值和尾随内容。错误不输出settings正文。

可直接检查仓库内的假设示例：

```powershell
python docs/architecture/check_config.py --config docs/architecture/examples/design-deployment.json --manifest docs/architecture/examples/design-provider-manifest.json
python -m unittest discover -s docs/architecture -p test_check_config.py
```

两个示例明确使用unimplemented-reference，不能用于启动产品。检查实际候选时替换文件路径，重复`--manifest`提供其他固定版本的模块清单；同一provider/release不能重复。命令成功返回0，失败返回1；报告列出原始文件SHA-256、依赖创建顺序、`status=offline_validated`和`ready_for_service=false`。

当前离线Schema子集只接受Draft2020-12和以`#/`开头的本地JSON Pointer引用，检查所有引用目标（包括未执行分支）。同时限制`$ref`和`$dynamicRef`，禁止`$id`改变资源解析基址。跨资源Schema注册表、锚点及受控制品Schema打包留给正式部署接入实现；不要把这个保守子集静默用于拒绝已承诺支持的生产适配器。

8项文件级测试覆盖有效报告、重复字段与错误信息隔离、非法Schema、动态外部引用、大小/嵌套/数字限制、本地引用、未使用的坏引用和settings错误。原有假设装配试验仍通过。

报告中的unverified列出制品锁、真实工厂/健康、凭据、后端布局、持久化代际、KB扇出、授权、迁移和检索效果。这份报告不能用于开启就绪状态；正式部署归一化器还必须完成这些门槛。

跨KB准入、配额、分区排名合并与失败处理细则见[检索协调器](RETRIEVAL_COORDINATOR.md)。当前纯规划器不代表实际并发/调用链已完成。

部署的必需只读角色query_read_unit及两项特性门槛见[读能力部署规范](QUERY_AUTHORITY.md#12-部署角色与声明特性补齐)。它是未冻结V1草案增量，旧配置必须显式升级；自报特性不代替真实快照/生命周期验收。


2026-10-14核对并修复Go内核与离线检查的[版本协商一致性](KERNEL_RUNTIME.md#版本协商一致性修复2026-10-14)：同主版本较新次版本不被额外1.0前缀限制拒绝，整数比较不依赖宿主位宽。仅声明协商，真实协议行为仍待验。


特性名称的格式、长度和唯一性现由Go内部注册与离线Schema共同约束，见[结构一致性修复](KERNEL_RUNTIME.md#特性声明结构一致性修复2026-10-14)。合法新特性保留扩展性，声明不能替代公共行为测试。


## 多格式证据核验装配提案

[证据核验模块设计](EVIDENCE_VERIFICATION_MODULES.md)提出独立内容/原件/索引角色及按Block.kind/Source.kind/Locator族的固定路由。其特性名称与配置示例尚未成为现有Schema/内核校验规则，需Go端口、工厂检查、配置归一化和远程映射同步完成后才可声明装配可替换。未覆盖正式发布定位、重复路由或必需模块缺失必须拒绝；真实替换验收仍待执行。
