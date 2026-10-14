# 记忆与图谱：来源、删除及替换契约

本规范细化[详细架构](DESIGN.md)的可选扩展。V1可以关闭记忆/图谱，但开放后必须遵守同一当前权限、发布、引用及删除规则。当前[Memory/Graph端口](../../pkg/contracts/intelligence.go)仅为草案，尚无实际后端及生命周期验收。本规范不宣称已接入外部记忆服务或图数据库。

## 1. 权威与衍生边界

| 数据 | 权威归属 | 替换时处理 |
|---|---|---|
| 用户明确保存的偏好、任务事实、记忆版本 | 平台记忆记录；有来源、账户/空间归属和保留规则 | 标准导出与导入，保留ID、版本、有效期和墓碑 |
| 从企业知识生成的记忆摘要 | 平台保存摘要版本及完整来源依赖；不能成为新发布知识 | 对来源重新核验，必要时形成新候选记忆版本 |
| 记忆向量、召回索引、供应商内部状态 | 衍生投影；供应商ID放BackendBinding | 从标准记忆记录重建，供应商状态不能成为唯一事实 |
| 图谱实体、关系及属性证据投影 | 从已审核已发布内容生成的衍生数据 | 从冻结Revision/Block重建，不能靠图数据库备份证明权威知识完整 |
| 行业术语/人工确认的本体规则 | 平台版本化配置或审核后的知识记录 | 独立导出，不与抽取模型的临时输出混为一体 |

记忆不得把模型推断当成用户确认偏好；模型经验须标记建议/任务轨迹来源，不能反向授予新工具权限。已有Save只保存应用核验后的记录，不是允许Agent任意自报Owner或来源的接口。

## 2. 作用域与当前读取

OwnerID、SpaceID、可选KB/Task归属由应用依据已认证主体和当前授权决定。请求里的OwnerID只是选择条件，不是身份声明；机器服务要读取用户记忆须有明确委托。跨空间复制必须作为新的授权写入，不能修改旧条目的空间或所有者绕过原规则。

知识来源型记忆及图谱查询同时使用AccessScope和当前发布投影。只有通过当前主体、空间、KB、有效Revision、撤回/删除、来源位置及TTL检查的候选才允许进入回答。无来源的个人偏好不能作为企业事实证据；任务记忆还须验证当前任务参与权限，不能仅用不可猜测TaskID证明可读。

不同类型需要独立来源规则：用户确认的偏好关联确认事件；任务经验关联受控任务/反馈记录；知识摘要关联平台Citation。MemoryProvenance现已补ConfirmationID及三类来源关系校验；确认事件真实性、完整依赖绑定及正式协议映射仍待实现，不得用自由文本Kind或空Citations掩盖来源缺失。

后端必须支持必要的前置作用域过滤，输出后再次进行当前核验。缓存命中也核验；可选模块不可用可以降级为基础检索，但身份或当前状态核验失败必须终止相关输出。查询语言/脚本不是伙伴接口，不向Agent暴露无限制Cypher、SQL或供应商管理调用。

## 3. 复合来源与属性泄漏

同一条摘要或关系依赖多份资料时，首版读取规则是所有依赖都有效且可读才返回整个条目。不能删掉不可读Citation后继续返回由该来源推导的正文、实体名、属性、置信度或统计计数。若需要保留可读部分，必须从允许来源重新生成新版本，并记录新的完整依赖；旧版本仍不可见。

图谱按事实/属性分别保留证据与来源归属，而不是只在节点上放一个全局KB标签。跨KB合并实体不允许将受限来源的属性并入公共节点返回。实体匹配与别名归一化规则有版本；供应商节点ID不作为外部稳定引用。

GraphResult的当前Entity/Relation类型没有细粒度属性来源和投影代际字段。正式协议需新增事实/属性证据绑定，或限制首版只返回整体可核验的实体/关系。该选择是显式能力限制，不得宣称草案已支持任意跨KB知识网络。

## 4. 撤回、撤权与物理删除

权威状态变更先立即通过读取闸门隐藏相关内容，随后异步清理记忆/图谱/缓存投影。事件至少一次，消费者按来源版本、投影代际和处理阶段幂等，旧事件不能删除或覆盖新代际。异步清理完成之前也不能继续读取旧内容。

删除进度要区分逻辑不可见、活动投影已清理、供应商副本已确认清理、备份按保留规则到期四种状态。不得以消息投递成功宣称物理删除完成。保留锁/审计等例外遵循[保留契约](RETENTION_CONTRACT.md)，不能使保留内容恢复业务可见性。

TTL到期后的新读取拒绝，即使清理任务尚未执行。恢复备份或导入旧记录时核对当前墓碑、过期状态、来源撤回及策略版本；不得从旧向量或供应商快照复活已删除记忆。供应商不能提供精确清理语义时，明确限制能力或停止使用该投影，不能仅靠本地Delete返回成功关闭物理清理验收。

## 5. 生命周期端口缺口

| 已有草案 | 需要补充的标准语义 |
|---|---|
| Memory Save/Update | 当前授权、owner/space不可变、来源类型、依赖集摘要、CAS、内容版本、写入确认与审计 |
| Memory Search | 当前状态过滤、任务权限、TTL、完整依赖核验；稳定引用回到平台记录版本 |
| Memory Delete | 幂等墓碑、逻辑不可见时间、供应商清理作业/结果，不以单个error证明删除传播 |
| Graph Extract | 只产生候选投影，不改文档/发布；绑定抽取Profile及来源Revision与位置 |
| Graph Query | 必要前置过滤、逐事实证据、当前发布/作用域校验及代际绑定 |
| 尚无图谱管理端口 | Prepare/Verify/Activate/InvalidateSource/Export或重建/清理结果；这些属于受控管理接口，不能开放给普通Agent |

正式补充需要版本化DTO、机器Schema、Go/wire对照和公共契约测试。新增能力在模块清单声明；旧版本不支持过滤、来源或删除功能时拒绝相关流水线装配。可选默认关闭不免除启用时的验收。

## 6. 配置、替换与回退

记忆存储、嵌入检索、图谱数据库和实体抽取策略分别绑定实现；本体、抽取/记忆策略和嵌入规格分别版本化。替换数据库不同时隐式改变本体或来源规则。模型学习出的隐式内部记忆如果无法标准导出，只能标记为复合能力，不能声称可无损细粒度替换。

记忆替换暂停写入、导出权威记录及未决清理、校验新存储、重建索引，再切换配置并重启；核对TTL/墓碑/CAS及未决任务后恢复。图谱替换构建新代际，从相同冻结内容和配置重建，通过过滤/来源/删除测试后切换。回退同样应用当前墓碑/撤权及新增写入，不恢复旧访问范围。

## 7. 必须预留的公共验收

| 门槛 | 所需证据 |
|---|---|
| EXT-01 主体隔离 | 同名用户/任务/KB、伪造OwnerID及缺委托访问全部拒绝 |
| EXT-02 当前来源 | 来源撤回/换版/撤权后立即不可见，缓存和图扩展也不能绕过 |
| EXT-03 复合来源 | 多来源条目含任一不可读来源时整体隐藏；实体属性/计数不泄漏 |
| EXT-04 删除与恢复 | TTL、墓碑、重复/乱序清理、活动副本及旧备份恢复不复活内容 |
| EXT-05 权威边界 | 抽取/记忆建议不修改发布知识，不授予权限或触发未授权业务工具 |
| EXT-06 替换对照 | 同一契约替换后ID/引用/来源/删除兼容，保留未决任务并验证回退 |
| EXT-07 效果与成本 | 固定问题集比较启用/关闭后的质量、延迟、费用，不只统计增加的候选数 |

这些门槛尚未运行。本次交付为来源/删除/替换的设计与接口缺口清单，阶段G维持待实施。


## 8. 来源类型的Go局部校验

[ValidateMemoryProvenance](../../pkg/contracts/memory_provenance.go)定义user_confirmed、task_experience、knowledge_summary三类。确认来源须有平台ConfirmationID；任务经验须有TaskID或FeedbackID；知识摘要须有Citation。非对应身份字段混入拒绝。确认/任务条目若附知识Citation，仍须按全部知识依赖执行当前读取闸门，不能因来源分类而忽略引用。

校验器限制256条引用，检查Doc/Revision/Block标识及同Revision不能声明多个文档；不证明确认事件存在、用户授权、任务访问、来源位置或引用当前有效。它尚未自动挂接Save/Search，不可声称服务已执行这些规则。来源类别/新字段属于尚未冻结的V1草案演进，旧无来源记录不能默认为user_confirmed，应隔离并人工确认或补来源后迁移。

两项Go测试覆盖允许类别、缺来源/混合类别拒绝、空标识、矛盾Revision归属和数量限制。统一Go测试/vet、199项Python与三份OpenAPI结构校验通过，见[当前本地检查报告](evidence/unified-local-checks.json)。没有实际记忆提供方、权限/TTL/删除传播试验，EXT门槛仍未通过。


## 9. 记忆记录机器草案

[Schema](schemas/memory-record.v1.schema.json)及[生成器](build_memory_record.py)定义schema_version=1.0的活动记录快照：平台ID/Owner/Space、可选KB/Task、内容、来源、创建/到期时间及正uint64版本。引用和来源位置复用伙伴协议闭合类型，生成后为自包含$defs，无供应商字段。可选字段省略而非null；ID遵循伙伴wire的200码点上限，内容1–16000码点，引用最多256条。

[离线校验](memory_record_validation.py)补充精确整数版本、Unicode、expires_at严格晚于created_at、任务来源与记录Task归属相同、Revision/Document声明一致。UTC时间使用现有规范格式，先验证真实日历，再用整数秒及9位分数比较，避免微秒解析丢掉纳秒差异。

该格式是未来导出/读取DTO草案，不是MemoryRecord的Go JSON直序列化；schema_version由wire包络提供。当前Go校验只覆盖来源类别/标识关系，不包括全部记录规则，正式映射仍需逐字段实现和跨语言往返测试。JSON解码时重复键、原始大小上限及输入认证尚未由本校验器保证，不能据此开放远程写入。

删除墓碑、供应商清理结果、来源依赖完整证明和真实确认事件仍是独立权威记录/接口缺口；活动记录Schema不能覆盖迁移的全部数据。旧未知来源记录隔离处理，不能用默认确认ID填充通过检查。

四项Python测试覆盖Schema生成一致性、超过double精确范围的版本、缺失/混合/未知来源、非法Unicode及浮点/布尔版本、纳秒到期/非法日历、任务范围与矛盾引用。统一检查共203项Python及Go/vet/三份OpenAPI通过；报告保持source_authenticated=false和currently_authorized=false。EXT及实际替换验收仍开放。


## 10. Go/wire离线映射与跨语言样本

[modulewire/memory.go](../../pkg/modulewire/memory.go)提供EncodeMemoryRecord/DecodeMemoryRecord，以显式DTO附schema_version，复用平台Citation/SourceAnchor/Locator映射。检查来源类型、Task关联、200码点ID、16000码点内容、正整数版本及精确到期；原始输入上限4MiB，拒绝重复JSON键、未知字段、显式null、浮点/指数版本和非法Unicode。原件/修订定位类型互斥；字符区间及归一化框验证顺序，但不验证实际内容/页面范围。

Go四项测试覆盖uint64最大值、纳秒差、闭合字段、重复键、版本格式、Task/到期/内容限制和带小数bbox引用往返。[离线工具](../../cmd/architecture-memory-record/main.go)读取记录后Decode/Encode，不提供HTTP/MCP、授权或写库。两项Python试验实际编译该工具，用确认/任务记录与PythonSchema对照最大版本、纳秒到期和非法输入；不能从这些样本推导所有边缘字段完全等价，引用多形态及完整负例对照仍需补齐。

统一205项Python及Go/vet/三份OpenAPI通过；源码库存已绑定映射与试验。持久Save/Search/Delete尚未接入；确认/任务真实性、当前知识来源、TTL读取和供应商删除传播仍未证明。旧缺来源记录仍须隔离，正式导出含墓碑/审计/清理进度的完整契约尚未完成。


## 11. 边缘定位与时间跨语言回归

扩展实际Go工具对照发现五处差异并修复：Python此前允许Go零值创建时间、倒序字符区间/框；Go此前将wire显式空worksheet/cell_range转为本地空值并放行。现在Python补非零创建及区间/框顺序，Go在解码返回前检查可选文本的存在与非空，不能把非法字段省略后声称有效。

新增两项跨语言测试。拒绝矩阵八组包含上述五类及空paragraph_path、无page的bbox、负page；合法样本包含完整修订定位、Unicode工作表、单元格、段落路径、字符范围、derived_from原件框、可选KB/Task和9999年纳秒到期。实际GoDecode/Encode输出再次用PythonSchema校验并精确对照输入。

本次先获得五类失败回归，再修复通过；统一207项Python及Go/vet/三份OpenAPI通过。证据见[边缘回归](evidence/memory-wire-edge-review.json)与[统一本地报告](evidence/unified-local-checks.json)。这是已覆盖矩阵的语义一致性，不是穷尽所有JSON/定位组合；实际内容边界、来源真实性和业务层授权仍未验证。


## 12. 最低声明能力与装配拒绝

[extension-features.v1.json](../../pkg/kernel/extension-features.v1.json)是受控V1草案政策：Go以embed编译入内核，Python离线Manifest校验读取同一文件。不是部署者可以删除的required_features配置。所有声明memory/graph的模块必须满足最低项，不仅检查已绑定模块；用户省略要求不能减弱最低规则。没有声明这两项的基础模块不受该可选政策影响。

| 能力 | 最低声明 |
|---|---|
| memory | prefilter.space、prefilter.owner、prefilter.task、prefilter.kb、prefilter.ttl、evidence.provenance、delete.idempotent、migration.standard_records |
| graph | prefilter.space、prefilter.kb、prefilter.revision、prefilter.generation、evidence.provenance、delete.source_invalidation、migration.rebuild |

standard_records表示实现支持平台标准记忆记录的恢复/迁移路径，不表示已导出全部墓碑或已证明无损迁移；恢复协议仍须单独验收。source_invalidation表示需要明确的来源清理能力，其管理端口仍是缺口。过滤声明不授予授权，也不能替代当前发布/权限闸门。

Go在Manifest/绑定/依赖预检检查最低清单，缺失时不创建实现。三项Go测试逐一删去15项最低声明，检查拒绝发生在Create之前；两种假实现按同一端口装配；返回的政策副本不能修改内部规则。两项Python测试检查相同清单的15种缺失与基础解析Manifest不受影响。统一209项Python及Go/vet/三份OpenAPI通过。

这些证据只证明拒绝缺失声明。模块虚假声称支持的情况，必须由真实过滤、来源、TTL/删除、迁移和公共契约试验发现；健康检查及接口类型匹配也不能证明声明真实。正式部署装配、产品启用配置和真实扩展能力验收仍未完成。


## 13. 独立图谱投影管理端口

[GraphProjectionManager](../../pkg/contracts/graph_projection.go)补充准备、验证、InvalidateSource、LoadCleanup和DeleteGeneration五项标准操作，对应graph_projection_manager能力。它与Graph抽取/查询端口独立：抽取策略、查询引擎及投影管理可分别实现，复合适配器也可同时声明。当前为Go端口草案与内核类型注册，没有状态验证器、机器Schema、远程映射或真实图数据库。

Target绑定平台Space/KB、BackendEpoch、Generation，以及索引/抽取/本体/源快照摘要。每个Target只属于一个KB/代际，跨KB实体融合由应用处理，不能用供应商全局节点ID代替平台引用。Prepare绑定Command、独立Attempt、冻结Revision及Evidence输入；平台验证这些证据的完整来源和调用权限，不接受模型自报内容作为权威材料。

准备可以读取当前发布内容，或应用已审核冻结、明确获准准备的候选版本；候选产物始终隔离。Proof的prepared/verified只是声明和验证结果，输入摘要、来源/过滤和内容完整性还须核验。管理端口不提供Activate：有效发布/代际指针通过平台元数据事务CAS切换，后端不得自行让候选成为已发布知识。失败保留原有效代际。

InvalidateSource绑定目标、文档/版本及PublicationEpoch，只清理由该来源产生的事实/属性；同实体的独立有效来源不能被无条件删掉。应用先通过当前读取闸门停止输出，再异步清理。旧任务只能处理精确绑定的代际/来源，不能扩大成全库删除或覆盖新版本。清理Receipt区分queued/running/completed/failed/unknown；只有核验供应商清理记录后才标completed，未决/未知操作保持可读的产品状态。

DeleteGeneration需要平台验证当前RetentionPermitID，确认不再被有效查询/回退窗口/保留锁保护。该ID是引用而非授权证明。Command幂等、任务栅栏、未知提交后的持久读回、审计/outbox和清理记录由应用/仓储协议保证；网络操作不与元数据假装共享本地事务。

两项内核Go测试：两种假管理实现能装配相同标准端口；只实现Graph的假实现不能冒充管理端口，拒绝后资源关闭。统一209项Python及Go/vet/三份OpenAPI通过。仅类型装配，不证明管理行为；部署启用Graph时要求管理端口的联结、最低管理特征、DTO/Grant映射及EXT-02/04/06真实试验仍未实现。


## 14. 查询与管理的必需装配联结

Go内核对声明graph但未同时声明graph_projection_manager的模块，补入不可省略的projection_manager依赖槽。可用ModuleConfig.dependency_bindings指定管理实例；缺少显式槽绑定时使用部署解析计划的全局管理能力绑定。预检验证目标能力、契约及依赖拓扑，任何实现Create前完成；管理模块先于查询创建，并可通过Lookup取得标准GraphProjectionManager端口。

同一复合模块同时声明并提供查询/管理两项时，不注入自身依赖，避免虚假环；实例服务仍分别做类型检查。分离实现必须使用管理能力，不能把普通Graph/Parser当管理端口。Python部署离线检查采用相同必需槽语义，外部部署需显式dependency_bindings；将该槽声明optional=true也不能省略绑定。外部到内部的正式归一化仍待接入。

新增三项Go试验覆盖缺少管理在创建前拒绝、实例级管理绑定/Lookup和复合无自身环；新增三项Python试验覆盖创建顺序、缺失/错误/可选绕过拒绝及复合装配。既有两种查询假实现试验已补管理模块。统一212项Python及Go/vet/三份OpenAPI通过。

联结证明能力/类型/创建顺序，不证明确实共享正确KB、BackendEpoch或Generation。每个业务Target的来源、后端映射与清理授权需应用验证，正式启动还须可信构建锁、健康及持久状态核对。不能从两个假端口健康就推导真实图谱清理/迁移通过。


## 15. 图谱管理结果的局部关系校验

[graph_projection_validation.go](../../pkg/contracts/graph_projection_validation.go)检查Target四项平台标识和四项策略/源摘要。ValidateGraphProjectionProof使用应用给出的目标、Attempt、输入摘要及prepared/verified期望状态逐项匹配；不得把另一空间/KB/后端代际/配置/源快照的证明套用于当前操作。实体/关系数量可为0，但数量本身不证明真实内容或正确过滤。

ValidateGraphCleanupReceipt匹配目标及OperationID，限定queued/running/completed/failed/unknown。completed须有非零CompletedAt和受控证据记录引用；其他状态不得携带完成时间。unknown不是完成或释放许可，不能据此删除保留记录、释放回退保护或宣称物理删除。

四项Go测试覆盖缺失绑定、十二类目标/Attempt/摘要/证明状态漂移、清理完成缺证据/时间或错操作/代际、未决状态伪造完成。统一212项Python及Go/vet/三份OpenAPI通过。这些函数尚未自动挂接管理服务返回路径；不会核验外部证据真实性、当前权限、保留许可、实际源内容或清理效果，不能关闭EXT-02/04/06。


## 16. 清理回执的操作类型与冻结请求绑定

修复第15节只绑定Target/OperationID的缺口。GraphCleanupExpectation及Receipt增加OperationKind和RequestDigest；类型仅允许source_invalidation/generation_deletion。InvalidateSource/DeleteGeneration请求与LoadCleanup同样携带该绑定。ValidateGraphCleanupReceipt改为使用应用独立取得的Expectation，逐项匹配目标、操作号、类型及摘要，不接受供应商回执自建期望。这是尚未冻结V1草案的接口演进，旧无绑定回执不得默认为任一类型。

摘要应由平台冻结语义DTO生成，按AKOS-CJSON/1及分别隔离的domain tag计算：akos.graph-cleanup/source-invalidation/1加NUL，或akos.graph-cleanup/generation-deletion/1加NUL。DTO需包括契约版本、操作类型/号、经过验证的主体/空间、完整Target，以及来源操作的Doc/Revision/PublicationEpoch，或整代操作的RetentionPermitID。所有整数精确编码，供应商内部ID不入业务DTO。

临时ScopeID、传输Invocation/重试次数和请求deadline不改变原操作语义摘要；恢复/重试仍重新核验当前权限和保留许可，不能以历史摘要获得新授权。来源/代际/许可变更是不同语义，应新建或显式冲突处理，不能沿用旧命令事实。摘要、OperationID唯一性、幂等结果与审计必须在共同权威记录保存，Load使用该记录的绑定查回执，不从最新默认参数重构旧请求。

当前只实现绑定字段与关系校验，语义DTO机器格式、domain hash计算、持久来源、Grant/服务身份及真实清理核验尚未实现。正确摘要字符串仍不证明供应商真的处理了正确来源或完整删除了数据。

新增Go测试拒绝跨类型、不同/缺失摘要、非法期望，并确认两种合法操作声明。此前四项关系测试已迁移为绑定期望。统一212项Python及Go/vet/三份OpenAPI通过；EXT与实际管理验收仍开放。


## 17. 清理语义DTO与域隔离摘要实现

[graph_cleanup_digest.go](../../pkg/modulewire/graph_cleanup_digest.go)实现闭合编码/解码及GraphCleanupRequestDigest。GraphCleanupSemantic是平台领域对象，wire包络为schema_version=1.0和operation；operation包含operation_id、subject_id、kind、target，以及来源操作的document_id/revision_id/publication_epoch，或整代操作的retention_permit_id。两类参数严格互斥，源PublicationEpoch正uint64。Target按四标识/四摘要显式映射，ID遵循200码点wire上限。

使用第16节分别隔离的含NUL域前缀，再对AKOS-CJSON/1字节作SHA-256，输出小写hex。原始JSON重复键、额外字段、浮点/指数整数字面量、非法Unicode及不兼容版本被拒绝。临时Scope/Invocation/deadline不在该语义模型中，不能把它们混入原操作摘要；实际恢复仍须从平台持久原操作取得同一语义，并重新授权。

请求现在明确携带平台OperationID，避免假设临时RequestID就是长期操作号。编码不从Principal/Scope自动认定主体真实性；应用必须解析经过认证的主体/委托，并核对Target、来源/许可和所有语义参数后生成摘要。不能让供应商自行生成期望并宣布匹配。

三项Go测试覆盖最大uint64往返、七类语义改变影响摘要、来源/整代参数互斥及许可变化、原始JSON重复/额外字段与浮点/版本拒绝。统一212项Python及Go/vet/三份OpenAPI通过。机器Schema及独立跨语言Hash金标已在第18节补齐；持久操作账本、Grant/真实管理返回接入及物理清理核验仍未实现。当前Hash不证明实际权限或效果。


## 18. 清理语义Schema与独立跨语言金标（2026-10-14归档）

[graph-cleanup-semantic Schema](schemas/graph-cleanup-semantic.v1.schema.json)闭合定义包络、平台标识、八项Target绑定及互斥操作参数。[Python参考](graph_cleanup_semantic.py)独立计算规范编码与域隔离摘要，生成[两类操作金标](examples/graph-cleanup-golden.v1.json)，包含Unicode主体和uint64最大PublicationEpoch。

[实际Go工具](../../cmd/architecture-graph-cleanup-digest/main.go)解码语义后计算摘要；[三项Python试验](test_graph_cleanup_semantic.py)实际编译并调用该工具，核对两类金标、字段顺序/空白变化不影响摘要，以及浮点、重复键、混合参数和不兼容版本在两端均拒绝。金标来自Python参考而非Go输出反填；这是合成语义一致性证据，不是授权证明。

对照过程中补齐Go原始字段存在性检查：来源操作不得包含retention_permit_id，即使值为空；整代删除不得包含document_id/revision_id/publication_epoch，即使空或0。不能将错误参数转成本地零值并省略后视为合法。尚无运行服务处理器或真实操作账本；临时Scope与传输身份仍须独立核验。

归档[统一本地报告](evidence/unified-local-checks.json)：215项Python、全Go测试/vet和三份OpenAPI结构检查通过，293项输入SHA-256经当前重新核对一致。未改动输入，不重复运行相同检查。业务授权、保留许可、持久原操作、供应商证据、真实清理/迁移和召回评测仍未通过；EXT与总体验收保持开放。
