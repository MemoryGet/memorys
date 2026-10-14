# 元数据读写数据源绑定与替换验收

本规范衔接[查询权威服务](QUERY_AUTHORITY.md)、[构建锁](BUILD_LOCK.md)及[迁移日志](JOURNAL_PROTOCOL.md)。部署角色与特性相符只能证明装配声明，不证明只读适配器与写事务使用同一份权威数据。当前已有设计与Go身份/观测端口及局部关系检查；离线review Schema已补齐；真实探测和提交点验证尚未实现。

## 1. 必须区分的身份

| 身份 | 含义及变更规则 |
|---|---|
| dataset_id | 平台知识权威数据集身份；迁移数据库时保持，克隆成独立环境时重新分配 |
| metadata_store_epoch_id | 当前权威存储实例代际；恢复、克隆或切换底层库时重新激活，不是KB索引BackendEpoch |
| binding_version | 控制面批准的读写绑定版本，单调推进，不能仅恢复旧配置降低版本 |
| metadata_model_version / schema_digest | 平台权威数据模型版本及已验证结构摘要；不使用数据库产品版本代替 |
| journal_stream_id | 该数据集当前权威提交日志链的身份；相同cursor文本而不同stream不能互换 |
| activation_cursor | 本绑定批准切换时必须覆盖的封存提交点；它不是“永远最新数据”证明 |
| provider / instance / endpoint_identity | 实际模块与已认证数据端点身份；连接字符串、配置别名或IP相同不是充分证据 |

dataset与存储代际由平台控制面治理。数据端点探测结果须来自已认证适配器及真实元数据记录，不信任用户请求或供应商索引的自报值。数据库中复制的身份记录可以随备份一起变旧，因此还必须与备份之外的当前控制面期望核对。

## 2. 控制面期望与读写观测

拟定StoreBindingExpectation固定dataset、metadata store epoch、binding version、model/schema、journal stream、activation cursor及获准读写实例/端点映射。稳定的绑定引用由部署锁定；随维护激活变化的当前期望由独立控制面记录提供并验证来源/高水位，不能要求每条业务写入重新生成BuildLock。

写事务服务和QueryReadUnit在启动时分别产生StoreObservation，核对上述共同身份。元数据仓储、事务内任务/审计/outbox/journal的归属仍须是同一原子写包；只读连接池可以独立实现或部署，但必须读取同一获准数据集与存储代际。

所有观测与控制面期望一致，且所需提交点覆盖证明通过后，才发布应用就绪状态。未知、不可读、来源失配、模型缺失、切换未决或绑定版本回退均不接收知识请求。普通健康检查成功和同名Schema不能自动修复失配，也不能创建身份记录冒充既有数据源。

当前QueryReadIdentity仅包含StoreEpochID/ReadVersion/Watermark，无法完整表达本规范。后续须定义版本化身份扩展及验证期望；现有Go页面/Guard校验只能核对给定标签，不能据此宣称数据源绑定通过。

## 3. 水位与一致性边界

activation cursor按[日志协议](JOURNAL_PROTOCOL.md)核验相同stream、前驱链和封存提交边界，不比较哈希/字符串大小来判断新旧。读端点必须覆盖整个批准提交点，包括版本、撤回、权限、后端映射和审计关系；只统计行数或向量数量不够。

首版正式查询优先要求权威读快照；若使用异步副本，必须由具体适配器提供可验证的重放覆盖及当前权限/发布检查能力。启动时覆盖activation cursor不证明此后所有新撤回都已到达副本，不能把一次探测当作永久允许。无法证明当前状态时失败关闭，不能返回旧数据作为可选降级。

运行中的ReadIdentity绑定当时metadata store epoch和binding version；维护切换使旧观察点失效。应用按既有预算重新准备或报快照过时，不改写旧ID/摘要。当前检查另取新观察点，并核验仍来自获准活跃存储。

本规范的外部activation checkpoint只证明规定恢复目标，不保证所有后续已确认写入零丢失。若产品要求RPO=0，必须另验证持续日志持久化/确认与恢复协议；不能只凭独立控制面标签声称识别了任意历史备份或所有丢失提交。

## 4. 替换与回退

| 变更 | 必须完成的处理 |
|---|---|
| 仅替换只读适配器/连接池 | 指向同一活跃数据集、代际、模型与日志；验证观察点/游标/权限、取消和性能，再装配重启 |
| 切换元数据库 | 暂停写入或执行已验收的在线迁移协议；封存提交点，迁移全量权威库存并核验，读写共同激活新存储代际 |
| 恢复备份 | 独立控制面确认当前期望与恢复目标；重放至规定提交点，核验墓碑/权限/映射，再激活新的store epoch |
| 克隆测试环境 | 独立dataset、凭据、端点与控制面绑定；不能沿用生产当前代际伪装成只读副本 |
| 回退 | 对旧存储补齐回退窗口日志与当前状态，作为新的绑定版本/存储激活处理；不简单降低版本或恢复旧锁 |

切换先准备候选，再验证并进行受控激活；失败时旧读写绑定继续有效。提交结果未知时先核对持久激活事实，不能两边同时自行宣布权威。激活及失效通知必须包含版本和代际，缓存通知不是唯一来源。

原件存储、索引BackendEpoch和元数据store epoch分别治理，但迁移验收要核对它们的引用关系。元数据库切换不能顺带将供应商索引ID当作平台Document/Revision ID，历史引用仍依当前授权/撤回语义读取。

## 5. 准入与验证安排

| 门槛 | 所需运行证据 |
|---|---|
| STORE-01 身份来源 | 控制面独立期望与真实读写端点一致；同别名/旧备份/复制身份记录不能冒充当前绑定 |
| STORE-02 模型与库存 | 错模型、漏墓碑/授权/版本/映射被拒绝；模型摘要与迁移记录可追溯 |
| STORE-03 提交点覆盖 | 错stream、缺前驱、部分事务、字符串伪排序及副本滞后被拒绝；不把activation覆盖等同实时状态 |
| STORE-04 共同激活 | 读写切换、提交未知、并发请求及旧观察点失效符合规则；不存在两份同时活跃权威 |
| STORE-05 恢复回退 | 全量/增量/墓碑验证，独立恢复目标、新绑定版本及旧实例拒绝，明确实际RPO |
| STORE-06 替换对照 | 两种真实读仓储或数据库实现使用同一权限/版本/来源/故障/迁移测试，上层API/MCP兼容 |

接下来先定义StoreBindingExpectation/Observation的类型和机器契约，再实现受控端点探测及控制面激活，最后通过真实数据库试验。以上门槛均未执行；现有部署Schema、内核与只读包装测试不能关闭STORE、AUTHORITY-06或总体架构验收。

## 6. Go身份与观测契约

[store_binding.go](../../pkg/contracts/store_binding.go)新增MetadataStoreIdentity、StoreBindingExpectation、端点映射、StoreObservation以及StoreIdentityReader/StoreCoverageVerifier端口。Probe/Coverage请求使用部署Scope，知识库Scope不能作为控制面探测授权；真正部署授权与服务身份仍须实现。

ValidateStoreObservations只核对给定期望与观测的身份及端点库存，至少同时含metadata_uow/query_read_unit角色，允许读写不同实例。缺失/额外/重复角色、复用ObservationID或dataset/store epoch/binding/model/schema/stream/端点错配均拒绝。版本为正uint64，模型摘要规范，游标仅作有界非空UTF-8标识；不对字符串排序、不确认真实覆盖或当前权威来源。

StoreCoverageVerifier必须检查真实封存提交、同stream前驱关系及覆盖；其receipt只是声明的数据模型，暂无实现或可信来源。不能把ValidateStoreObservations成功当作ready，不能把独立期望从被检查数据库复制出来，也不能将任意verification_reference当作有效证明。

4项Go检查覆盖分离读写实例/顺序无关、八类身份与端点错配、缺/多/复用观测及明确不验证游标覆盖。全Go测试/vet通过，见[证据](evidence/store-binding-ports-review.json)。当前仅关系检查和端口声明，STORE-01至06与整体保持开放。

## 7. 离线review机器契约

[store-binding-review.v1.schema.json](schemas/store-binding-review.v1.schema.json)定义schema_version、expectation与observations闭集包络，嵌套身份/端点也采用闭集字段。它是操作者提供的离线核对输入，不是已签名控制面记录、部署配置或HTTP协议；Go字段已有下述显式离线映射；不能直接序列化Go struct冒充它，也不能将此包络作为未经验证的HTTP协议。

build_store_binding.py生成Schema，store_binding_validation.py检查精确整数、给定期望与观测身份、角色/端点库存及Receipt唯一性。文本沿Go检查上限限制为4096 UTF-8字节，raw载荷≤4MiB，版本为正uint64；拒绝重复JSON键、浮点/布尔版本、非有限值、未知字段和非法Unicode。游标仅有界检查，不排序或认证。

报告provenance_verified、coverage_verified、ready_for_service固定false；把期望和观测放入一个包络不会让期望独立可信。生产控制面来源、探测服务身份及日志链验证必须另完成。5项新检查覆盖精确大整数/明确未就绪、身份错配、库存及复用、闭集/类型和raw JSON/Unicode字节预算；全部182项Python回归通过，见[证据](evidence/store-binding-schema-review.json)。STORE门槛仍开放。

## 8. 显式Go映射与实际往返

`pkg/modulewire.Encode/DecodeStoreBindingReview`登记完整字段映射，将平台Identity/Expectation/Endpoint/Observation转换为上述闭集Schema；不会采用Go默认字段名。编码前和解码后分别执行身份库存关系检查，原始版本数字只接受整数token，1.0/1e0不能绕过精确类型要求。重复键、非法Unicode、多根JSON、缺字段/未知字段和uint64越界由严格codec拒绝。

离线cmd/architecture-store-binding仅从stdin读取≤4MiB记录并输出往返JSON，不发网络请求、不导入数据、不修改控制面。解码结果是独立新值，但仍是给定观测与期望，不证明它们真实；正式启动必须继续执行来源/服务身份/覆盖和当前状态门槛。

3项Python检查编译并调用实际Go工具：闭集记录/中文和最大uint64往返与独立Schema相同；未知/大小写字段、浮点版本、身份错配、复用观测、缺库存拒绝；raw重复键、指数整数、越界、非法Unicode及尾随JSON拒绝。全部185项Python回归、全Go测试及vet通过，见[证据](evidence/store-binding-wire-review.json)。STORE门槛仍开放。

真实探测的新鲜性缺口、覆盖回执和联合就绪顺序见[数据源启动门](STORE_STARTUP_GATE.md)。当前ObservationID不绑定本次调用，不能排除历史响应重放。
