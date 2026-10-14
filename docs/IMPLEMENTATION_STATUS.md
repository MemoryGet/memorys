# 实施状态

更新：2026-10-14（北京时间）。总目标：开发 AI Knowledge OS 模块化可替换架构设计 V1。

## 当前阶段：先完成架构设计

2026-10-14补[同数据索引交接](architecture/INDEX_SNAPSHOT_TRANSFER.md)：标准完整投影/Owner/删除标记快照，Memory→PG→新Memory校验值一致，损坏/覆盖拒绝及实际SQL失败回滚。权威目录为fixture，全量平台迁移/切换和生产验收仍开放。

2026-10-14补[索引清理双实现](architecture/INDEX_CLEANUP_BACKENDS.md)：标准删除/读回端口，通用适配器注入内存或PG；PG删除状态跨连接保留、旧快照写回被拒绝。完整IndexWriter/检索、同数据迁移/回退及数据库重启验证仍待完成。

2026-10-14补[本地索引实际清理适配器](architecture/CLEANUP_NATIVE_INDEX.md)：标准执行器调用MemoryIndex.Delete，独立核验真实Owner/删除标记/记录数；PG任务链路加入实际删除与新代际隔离。权限仍fixture，重启持久化及成熟引擎/替换验收待完成。

2026-10-14补[清理单项消费者](architecture/CLEANUP_WORKER.md)：固定效果身份、Lookup优先、独立租约/回执核验、执行准入及未知结果处理。真实PG存储和编排有探针；外部执行/准入仍fixture，完整消费者和生产验收未完成。

2026-10-14补[清理任务持久化领取](architecture/CLEANUP_TASKS.md)：固定目标进度同事务创建、原子租约/fence与幂等终态、unknown拒绝自动重做；并发实库探针已加入。外部执行/对账、消费者及完整任务能力仍待验。

2026-10-14补[来源登记/计划封存约束](architecture/POSTGRES_WITHDRAW_STORAGE.md#13-来源登记与计划封存约束)：登记必须匹配锁定的有效旧epoch；提交双向核对全部登记目标，封存后不允许追加。消费者、外部投影fencing、并发/恢复与完整WTX仍待验。

2026-10-14补[撤回命令语义与准备器](architecture/WITHDRAW_COMMAND_SEMANTICS.md)：规范化摘要/域隔离、Go/Python金标、可信Verify门槛、独立平台ID及取消检查；PG应用已使用实际摘要。真实身份/文档权限与格式迁移仍待验。

2026-10-14补[撤回应用编排与标准事务端口](architecture/POSTGRES_WITHDRAW_STORAGE.md#11-标准事务端口与撤回应用编排)：业务仅依赖contracts，真实SQL提供当前头/已登记目标，应用流程提交与历史重放已有探针。真实Prepare、完整PublicationService/UoW、登记fencing、迁移及生产准入仍待验。

2026-10-14补[撤回审计原子写入](architecture/POSTGRES_WITHDRAW_STORAGE.md#10-撤回审计与原子性)：审计绑定不可变历史、禁止应用修改/删除；缺失/不匹配拒绝，审计故障全事务回滚。生产审计接口、角色/RLS、迁移日志与完整业务服务仍待验。

2026-10-14补[命令预留/历史重放](architecture/POSTGRES_WITHDRAW_STORAGE.md#9-命令预留与历史重放)：预留与撤回同事务、已提交返回原历史、同键摘要冲突及pending拒绝重做。可信命令摘要编码、真实委托身份和完整应用服务仍待实现。

2026-10-14补[撤回发布头CAS/outbox事务片段](architecture/POSTGRES_WITHDRAW_STORAGE.md#8-发布头-cas-与-outbox-原子提交)：同事务更新发布头、历史/清理计划、事件及命令状态；错误被忽略仍拒绝提交。完整UoW、真实权限、审计与业务闭环待验。

2026-10-14补[清理计划多目标实际写入/回滚](architecture/POSTGRES_WITHDRAW_STORAGE.md#7-清理计划与多目标写入回滚)：History/Plan/两个目标真实插入读取、主动失败和第二目标外键错误全回滚；15.17/18.0均通过。仍使用合成种子，完整权威事务未实现。

2026-10-14补[SQL取消/提交前复核](architecture/POSTGRES_WITHDRAW_STORAGE.md#6-验证后取消与提交前权限变化)：验证返回成功后仍检查父/操作context，提交前撤销回滚实际History INSERT；新增1项Go测试，15.17/18.0真实探针再次通过。IAM/RLS及完整治理闭环未接入。

2026-10-14接入[Go History/Plan SQL片段](architecture/POSTGRES_WITHDRAW_STORAGE.md#5-postgresql-18与go仓储适配片段)：事务Scope/预算/关闭及真实读取、History写入回滚，15.17/18.0实际SQL+Go均通过，临时容器清理。18下载已完成；完整UoW/Auth/审计/outbox/业务bootstrap仍待实现。

2026-10-14补[数据库运行器失败清理](architecture/POSTGRES_WITHDRAW_STORAGE.md)：创建后启动失败时按自身名称/标签清理、错标签及远程socket拒绝；3项Python测试通过，15.17实际路径再次通过。18下载会话94179仍存活；未重复启动，18验收仍未完成。

2026-10-14新增[撤回SQL原型/真实数据库验证](architecture/POSTGRES_WITHDRAW_STORAGE.md)：实际PostgreSQL 15.17约束/回滚试验通过并清理临时容器。18.0标准镜像两次下载截断，同版Alpine下载仍在进行；Go UoW及完整WTX未完成。

2026-10-14补[发布历史/清理计划Go契约](architecture/WITHDRAW_TRANSACTION.md#7-go契约草案与关系检查)：load/append事务端口、前后epoch/固定目标/绑定引用及关系/覆盖检查；3项Go测试通过。SQL、冻结目标定义、导出迁移及业务准入仍待实现，WTX保持开放。

2026-10-14细化[权威撤回事务/派生清理](architecture/WITHDRAW_TRANSACTION.md)：核实缺少可读取历史仓储和固定清理目标，定义历史幂等、CAS/outbox、旧代际隔离和6项WTX门槛。仅实施规格；SQL事务/服务/消费者未实现。

2026-10-14补[内存索引删除/旧任务阻止](architecture/MVP_QUALITY_REPAIR.md#内存索引删除与旧任务阻止)：代际唯一所属、指定代际/分块删除、重复操作及反复写入拒绝；3项Go测试通过。持久化fencing、权威撤回/outbox、重启恢复和真实索引仍待实现。

2026-10-14补[本地可重试删除](architecture/MVP_QUALITY_REPAIR.md#本地删除记录与可重试清理)：独占安装删除记录、拒绝新读取、跨实例继续清理、损坏记录失败关闭及期限绑定；2项Go测试通过。平台撤回/索引传播、完整备份恢复及生产文件系统准入仍待验。

2026-10-14补[本地上传幂等账本](architecture/MVP_QUALITY_REPAIR.md#本地上传幂等账本局部实现)：主体/动作/键及请求摘要绑定、跨实例重放、同键冲突、并发预留与未知结果拒绝重建；3项Go测试通过。自动恢复/清理、断电/进程退出、元数据事务及生产授权仍待验。

2026-10-14接入[解析/分块受控工厂](architecture/MVP_QUALITY_REPAIR.md#解析与分块的受控工厂接入)：标准端口、显式Blob依赖、严格设置、关闭后拒绝旧引用；2项Go集成测试通过。两实例重绑定不等同异构引擎替换，正式部署/BuildLock/业务bootstrap仍待接入。

2026-10-14修复当前新增MVP模块[质量问题](architecture/MVP_QUALITY_REPAIR.md)：统一BlobStore、真实原件解析、受限上传、带引用的前置过滤、稳定分块ID及依赖边界；新增9项Go回归。业务API要求可信Prepare/Verify，默认未配置时503；真实授权、元数据治理及完整运行验收仍待完成。

2026-10-14完善[可替换证据核验模块设计](architecture/EVIDENCE_VERIFICATION_MODULES.md)：拆分内容/原件/索引/专用定位角色，固定路由及声明覆盖，明确音频引用字段缺口、版本迁移和6项EV运行门槛。仅设计，配置示例及专用端口未注册或实施。

2026-10-14补[正文证据位置/摘录核验](architecture/CURRENT_CHECK_FLOW.md#11-正文证据定位与摘录核验)：同次读取实际双摘要后核验revision text块，exact按Unicode码点半开区间、coarse按连续子串；3项Go测试通过。原件/表格/媒体定位明确未支持，真实后端/输出接入仍待验。

2026-10-14补[待用证据冻结绑定](architecture/CURRENT_CHECK_FLOW.md#10-待使用证据的冻结绑定)：Evidence绑定原发布Document/Revision，完整复制后传给后端，非法证据零提供者调用；2项Go测试通过。实际位置/摘录核验与输出调用尚未接入，CHECK-03/CHECK-06保持开放。

2026-10-14补[标准冻结内容读取/双摘要核验](architecture/CURRENT_CHECK_FLOW.md#9-标准冻结内容读取与双摘要核验)：平台ID绑定、实际标准JSON规范化BlocksHash/ContentHash比对、父预算与静态错误；3项Go测试通过。真实读取提供者、原件字节、索引Ready与具体Evidence定位仍待实现。

2026-10-14补[局部当前复核编排](architecture/CURRENT_CHECK_FLOW.md#8-局部编排实现与验收边界)：两次控制刷新、新读取、批准及后端核验按序执行，失败停止、原值隔离及父预算；4项Go模拟测试通过。真实端口、REST/MCP/模型输出接入与CHECK运行验收未完成。

2026-10-14补[刷新主体/范围比对](architecture/CURRENT_CHECK_FLOW.md#7-刷新身份与授权范围的局部比对)：绑定主体、发行方、客户及委托，范围只可缩小并覆盖来源，保留空证据检查和父预算；3项Go测试通过。真实认证/授权源与CHECK-01仍待运行验收。

2026-10-14补[当前批准投影与关系检查](architecture/CURRENT_CHECK_FLOW.md#6-当前批准投影与局部关系检查)：同批次Revision/Approval/Validity，独立审核规则版本，缺失或无效投影拒绝；2项Go测试及统一回归通过。真实权威仓储和CHECK运行验收仍待完成。

2026-10-14细化[当前复核应用编排](architecture/CURRENT_CHECK_FLOW.md)：身份/委托范围、当前批准/冻结关系、真实Ready/引用及最终版本/预算检查；明确现有投影缺口和6项CHECK运行门槛。仅设计，未宣称协调服务已接入。

2026-10-14补[当前元数据/准备事实比对](architecture/QUERY_AUTHORITY.md#14-当前元数据与准备事实的关系比对)：StoreEpoch/策略/发布/Revision/代际/配置关系，拒绝过时/不完整批次，允许无关行版本前进；3项Go测试及统一回归通过。仅元数据关系，实时批准/Ready/权限和真实接入未验。

2026-10-14实现[另开观察点的有界当前批量读取](architecture/QUERY_AUTHORITY.md#13-另开观察点的有界当前批量读取)：自动句柄关闭、ID/数量/编码字节预算、错误零结果；3项Go测试及统一回归通过。已核对PostgreSQL官方隔离语义，真实SQL/ACL/副本新鲜度与AUTHORITY仍未验。

2026-10-14修复[特性声明结构差异](architecture/KERNEL_RUNTIME.md#特性声明结构一致性修复2026-10-14)：Go拒绝空/非法/过长/重复特性和重复要求，保留合法新名称；2项Go/2项Python新增测试，统一219项Python及Go/vet/三份OpenAPI通过。仅结构/声明，实际模块行为仍待验。

2026-10-14修复[版本协商不一致](architecture/KERNEL_RUNTIME.md#版本协商一致性修复2026-10-14)：去掉生命周期1.0额外前缀限制，次版本比较避免宿主整数溢出；2项Go/2项Python新增测试，统一217项Python及Go/vet/三份OpenAPI通过。仅协商声明，真实替换和互操作仍待验。

2026-10-14恢复后归档[清理语义Schema/独立金标](architecture/MEMORY_GRAPH_LIFECYCLE.md#18-清理语义schema与独立跨语言金标2026-10-14归档)：两类金标、实际Go/Python摘要对照与非法输入拒绝；215项Python及Go/vet/三份OpenAPI报告的293项输入哈希当前一致。目标已恢复active；真实操作账本、授权与清理/迁移仍未验。

2026-10-13实现[图谱清理语义DTO/摘要](architecture/MEMORY_GRAPH_LIFECYCLE.md#17-清理语义dto与域隔离摘要实现)：OperationID明确携带，闭合互斥参数、AKOS-CJSON/1+域隔离SHA-256；3项新增Go测试及统一回归通过。缺Schema/跨语言金标、权威原操作/Grant与真实效果，EXT开放。

2026-10-13修复[图谱清理操作绑定缺口](architecture/MEMORY_GRAPH_LIFECYCLE.md#16-清理回执的操作类型与冻结请求绑定)：来源失效/整代删除区分并匹配冻结请求摘要，期望来源独立；新增Go测试、既有关系测试迁移及统一回归通过。摘要编码/持久账本/服务身份与物理效果仍待验。

2026-10-13补[图谱管理结果关系校验](architecture/MEMORY_GRAPH_LIFECYCLE.md#15-图谱管理结果的局部关系校验)：目标/Attempt/输入摘要/状态绑定，清理完成需时间/记录引用，unknown不冒充完成；4项新增Go测试及统一回归通过。未挂接真实管理调用，外部证据/权限/保留/清理效果待验。

2026-10-13补[图谱查询/管理必需装配联结](architecture/MEMORY_GRAPH_LIFECYCLE.md#14-查询与管理的必需装配联结)：分离管理实例/Lookup、复合无自身环，缺失/错误/可选绕过拒绝。3项Go及3项Python新增检查，统一212项Python及Go/vet/三份OpenAPI通过；同源后端/Target和正式归一化仍待验。

2026-10-13补[独立图谱投影管理端口](architecture/MEMORY_GRAPH_LIFECYCLE.md#13-独立图谱投影管理端口)：准备/验证、来源失效、清理状态和整代删除；内核区分查询与管理类型。2项新增Go装配/拒绝测试及统一209项Python、Go/vet/三份OpenAPI通过。仅声明/类型，状态、wire、正式绑定与真实管理行为待验。

2026-10-13补[可选模块最低能力预检](architecture/MEMORY_GRAPH_LIFECYCLE.md#12-最低声明能力与装配拒绝)：同一受控清单供Go内核/Python检查，15项缺失均拒绝；3项Go和2项Python新增检查，全209项Python及Go/vet/三份OpenAPI通过。仅声明门/假实现装配，真实行为与正式部署仍待验。

2026-10-13修复[记忆跨语言边缘差异](architecture/MEMORY_GRAPH_LIFECYCLE.md#11-边缘定位与时间跨语言回归)：五类先失败后修复（零时刻、倒序区间/框、显式空定位文本）；八组拒绝及完整定位往返通过。统一207项Python及Go/vet/三份OpenAPI通过，实际内容/来源/业务授权仍待验。

2026-10-13补[记忆离线Go/wire映射](architecture/MEMORY_GRAPH_LIFECYCLE.md#10-gowire离线映射与跨语言样本)：4项Go检查和2项实际Go/Python往返，整数/时间/来源及闭合字段验证；修复vet构造警告后统一205项Python及Go/vet/三份OpenAPI通过。样本级映射，完整边缘字段/实际服务和EXT仍待验。

2026-10-13补[可选记忆记录机器草案](architecture/MEMORY_GRAPH_LIFECYCLE.md#9-记忆记录机器草案)：自包含Schema复用引用类型、整数版本、任务归属与纳秒到期；新增4项Python测试，统一203项Python及Go/vet/三份OpenAPI通过。非正式Go/wire往返，无真实来源/权限证明，EXT开放。

2026-10-13补[记忆来源Go关系校验](architecture/MEMORY_GRAPH_LIFECYCLE.md#8-来源类型的go局部校验)：确认事件ID及三类来源，缺来源/混合字段/矛盾引用拒绝；2项新增Go测试及统一检查通过。未挂接实际Save/Search或验证确认真实性，EXT保持开放。

2026-10-13细化[记忆/图谱生命周期](architecture/MEMORY_GRAPH_LIFECYCLE.md)：权威与投影归属、任务/主体授权、复合来源整体可读、撤回/TTL/墓碑、删除传播及替换回退；标出当前端口缺口并新增7项EXT门槛。仅设计，无真实扩展运行证据，阶段G保持待实施。

2026-10-13补齐[统一检查输入库存](architecture/VALIDATION.md#统一本地验证入口windows)：依赖锁及所有examples/testdata字节纳入，新增/删除/变更可检出；新增2项测试，199项Python及Go/vet/三份OpenAPI通过。报告绑定267项输入，工具二进制及安装依赖内容未验证。

2026-10-13将远程模块17项操作纳入[统一外部OpenAPI检查](architecture/VALIDATION.md#7-openapi规范校验证据)：伙伴4/管理26/远程17均通过固定校验器，统一入口全Go/vet及197项Python通过。仅规范结构与本地契约，真实联调仍待验。

2026-10-13增加[统一本地验证入口](architecture/VALIDATION.md#统一本地验证入口windows)：Go测试/vet、197项Python检查及伙伴/管理OpenAPI结构校验通过，命令失败/超时/工具缺失不会跳过；报告绑定输入哈希并检查运行期变化。仅本地契约回归，正式运行验收仍待完成。

2026-10-13增加[静态依赖边界门](architecture/DEPENDENCY_BOUNDARIES.md)：87个pkg/cmd非测试Go文件检查通过，3项测试含9类违规拒绝；全Go/vet通过。阻止领域/编排耦合SDK、SQL/HTTP及具体实现；未覆盖未来internal/frontend和实际替换运行，整体仍开放。

2026-10-13修复[输入用量分桶关系](architecture/REQUEST_METERING.md#9-输入用量分桶关系复核)：拒绝uncached超额、缺total及三桶矛盾，采用减法防溢出；新增2项测试，全Go/vet通过。仅局部关系验证，真实计量门槛保持开放。

2026-10-13补齐[计量应用端口与事务边界](architecture/REQUEST_METERING.md#8-计量应用端口与持久事务边界)：Open/Reserve/Begin/Reconcile/Abort/Load声明、版本/幂等及未知回执链设计；全Go/vet通过。无持久账本、供应商对账或并发/恢复证明，METER保持开放。

2026-10-13补齐[Go usage/金额与精确算式](architecture/REQUEST_METERING.md#7-go金额usage类型与精确算式)：6项关系/大整数检查及全Go/vet通过，未知/估算不冒充完整实测、缓存不重复收费。无真实预留/结算/来源证据，METER开放。见[证据](architecture/evidence/metering-arithmetic-review.json)。

2026-10-13补齐[全请求usage/费用计量设计](architecture/REQUEST_METERING.md)：明确全调用关联、整数费率、预留/结算、未知占额、并发和恢复；新增6项METER门槛。仅设计，无真实计量器/费用账本/供应商证据，整体开放。

2026-10-13补齐[反思检索应用接入设计](architecture/REFLECTION_PIPELINE_BINDING.md)：区分初始/追加轮、策略变化终止、新文本嵌入/子投影Grant、来源与最终证据复核及全请求计量；新增6项REF-BIND门槛。仅设计，真实适配器/计量/端到端未验，整体开放。

2026-10-13补齐[请求内有限反思循环](architecture/REFLECTION_ORCHESTRATION.md#10-请求内有限反思循环)：9项Go检查覆盖轮数/统一截止、重复/无新证据、必需Gate、模型隔离与返回复核，全Go/vet通过。受控回调，无真实模型/授权/费用或持久预算证明，REFLECTION开放。见[证据](architecture/evidence/reflection-loop-review.json)。

2026-10-13补齐[整轮反思多查询调度](architecture/REFLECTION_ORCHESTRATION.md#9-整轮多查询调度)：基础与反思复用dispatch池/预算策略，6项新Go检查及全Go/vet通过。只受控SearchFunc一轮，真实Reflector/后端、跨轮/模型计量和授权未验，REFLECTION开放。见[证据](architecture/evidence/refinement-execution-review.json)。

2026-10-13补齐[同路线多改写合并](architecture/REFLECTION_ORCHESTRATION.md#8-同路线多改写合并)：6项Go检查覆盖追踪/稳定顺序、单路线RRF投票、字节预算及冲突证据拒绝，全Go/vet通过。真实执行/循环/计量、跨路线汇总与权限未验，REFLECTION开放。见[证据](architecture/evidence/refinement-merge-review.json)。

2026-10-13补齐[反思多查询预算规划](architecture/REFLECTION_ORCHESTRATION.md#7-多查询预算规划与配置)：MaxPlanQueries进入Go/部署Schema，4项Go及1项配置检查，全部194项Python/全Go/vet通过。仅候选规划，真实执行/合并/计量与权限/反馈未验，REFLECT-01开放。见[证据](architecture/evidence/refinement-plan-review.json)。

2026-10-13补齐[有限反思与反馈草稿编排](architecture/REFLECTION_ORCHESTRATION.md)：明确Assess/Refine/Propose边界、多查询共享预算、流式/缓冲自检及采纳CAS审核；新增六项REFLECTION门槛。只有设计，预算字段/跨查询合并/真实模型与事务未验，REFLECT-01开放。

2026-10-13补齐[Check库存集体接收候选](architecture/STORE_PROBE_LEDGER.md#8-check库存与集体接收候选)：固定成员/Probe/端点及全量候选，5项Go检查与全Go/vet通过。候选不执行CAS或消费账本，真实原子提交/并发仍未验，PROBE-LEDGER开放。见[证据](architecture/evidence/store-probe-check-review.json)。

2026-10-13补齐[Go探测账本状态/事务端口](architecture/STORE_PROBE_LEDGER.md#7-go账本状态与事务端口)：5项关系检查覆盖状态顺序、终态、摘要不变、栅栏/截止及版本溢出，全Go/vet通过。无真实CAS/授权/集体Check/持久恢复证据，PROBE-LEDGER开放。见[证据](architecture/evidence/store-probe-ledger-ports-review.json)。

2026-10-13补齐[探测一次性接收协议](architecture/STORE_PROBE_LEDGER.md)：定义唯一挑战、状态机、CAS/审计/outbox原子提交、重复与冲突、fencing接管和未知提交恢复；新增6项PROBE-LEDGER门槛。仅设计，持久记录/摘要/事务未实现，整体开放。

2026-10-13补齐[Bound探测Go时间映射](architecture/STORE_STARTUP_GATE.md#9-绑定探测go时间映射)：3项实际Go/Python检查及1项Go时间范围检查，全部193项Python/全Go/vet通过。离线now输入不是可信时钟，真实认证/账本/覆盖未验，START-STORE开放。见[证据](architecture/evidence/store-probe-wire-review.json)。

2026-10-13补齐[Bound探测离线Schema](architecture/STORE_STARTUP_GATE.md#8-绑定探测离线机器契约)：闭集包络与精确纳秒关系检查，5项新测试及全部190项Python回归通过。认证/覆盖/ready固定false；Go时间映射和真实账本/服务未验，START-STORE开放。见[证据](architecture/evidence/store-probe-schema-review.json)。

2026-10-13补齐[Bound探测Go契约](architecture/STORE_STARTUP_GATE.md#7-首批绑定探测go契约)：4项Go检查绑定检查/探测/调用、部署授权及精确时间窗口，全Go/vet通过。旧离线格式保留；真实性、期望摘要认证、一次性账本、Schema/映射与覆盖未验，START-STORE开放。见[证据](architecture/evidence/store-probe-binding-review.json)。

2026-10-13补齐[数据源启动核对设计](architecture/STORE_STARTUP_GATE.md)：发现Observation未绑定本次请求，明确探测/回执新鲜性扩展、独立期望、真实覆盖、最终绑定复核及联合就绪；新增6项START-STORE门槛。仅设计，现有端口须升级，整体开放。

2026-10-13补齐[绑定记录显式Go映射](architecture/METADATA_STORE_BINDING.md#8-显式go映射与实际往返)：3项实际Go/Python检查及全部185项Python/全Go/vet通过。离线往返不探测、不激活；真实独立来源/覆盖未验，STORE开放。见[证据](architecture/evidence/store-binding-wire-review.json)。

2026-10-13补齐[数据源绑定离线Schema](architecture/METADATA_STORE_BINDING.md#7-离线review机器契约)：5项新检查及全部182项Python回归通过。报告provenance/coverage/ready固定false；不是已签名期望或正式Go映射，真实控制面/探测/覆盖未验，STORE开放。见[证据](architecture/evidence/store-binding-schema-review.json)。

2026-10-13补齐[Go数据源身份/观测端口](architecture/METADATA_STORE_BINDING.md#6-go身份与观测契约)：4项Go关系检查及全Go/vet通过；读写可不同实例但身份/库存必须匹配，游标覆盖另设端口且未实现。真实控制面/探测/Schema/覆盖均未验，STORE开放。见[证据](architecture/evidence/store-binding-ports-review.json)。

2026-10-13补齐[元数据读写数据源绑定](architecture/METADATA_STORE_BINDING.md)：区分dataset/store epoch/索引epoch、独立控制面与端点观测，明确水位不能字符串比较、共同激活及RPO边界；新增STORE-01至06门槛，尚无机器契约或真实运行证据。整体开放。

2026-10-13补齐[只读部署角色](architecture/QUERY_AUTHORITY.md#12-部署角色与声明特性补齐)：Schema要求query_read_unit与两项特性，更新离线示例；3项新检查及全部177项Python回归通过，配置报告ready_for_service=false。真实factory、数据源锁/归一化仍未验，整体开放。见[证据](architecture/evidence/query-read-deployment-review.json)。

2026-10-13补齐[只读模块内核装配](architecture/QUERY_AUTHORITY.md#11-内核能力装配及元数据归属)：query_read_unit能力/类型检查，2项内核检查覆盖两个测试提供方、生命周期及特性/错误端口拒绝，全Go/vet通过。正式部署角色、数据源锁及真实库迁移未验，整体开放。见[证据](architecture/evidence/query-read-module-review.json)。

2026-10-13补齐[ReadUnit自动保护](architecture/QUERY_AUTHORITY.md#10-readunit自动保护与回调协议)：自动Guard/Close及同步恰一次回调，7项Go检查含到期竞态修复，全Go/vet通过。受控底层单元，无真实事务/工厂/授权一致性证据，AUTHORITY开放。见[证据](architecture/evidence/query-read-unit-review.json)。

2026-10-13补齐[只读句柄生命周期包装](architecture/QUERY_AUTHORITY.md#9-回调仓储生命周期包装)：4项Go检查覆盖关闭旧引用、取消/join、范围副本及有效期；全Go/vet通过。包装器须由真实WithinRead回调正确装配，驱动取消/事务释放未验，AUTHORITY-01/05开放。见[证据](architecture/evidence/query-read-guard-review.json)。

2026-10-13补齐[有界多页库存收集](architecture/QUERY_AUTHORITY.md#8-有界多页库存收集器)：5项Go检查验证固定观察点、跨页重复/游标循环、预算不截断和取消/读失败零部分输出；全Go/vet通过。测试仓储，不证明真实快照/cursor/库存完整性，AUTHORITY开放。见[证据](architecture/evidence/query-read-collection-review.json)。

2026-10-13补齐[Go只读快照仓储端口](architecture/QUERY_AUTHORITY.md#7-首批go读端口与页面关系检查)：观察点/发布页面/当前状态类型和回调接口，4项页面关系检查及全Go/vet通过。真实读事务、cursor绑定/句柄失效、来源与工厂未验，AUTHORITY及整体开放。见[证据](architecture/evidence/query-read-ports-review.json)。

2026-10-13补齐[查询权威服务](architecture/QUERY_AUTHORITY.md)：定义可替换只读快照仓储、Prepare/子授权/投影与CheckCurrent，明确外部策略跨系统一致性和流式输出线性化边界；新增6项AUTHORITY运行门槛。仅设计，真实读端口/仓储/服务未实现，整体开放。

2026-10-13补齐[实际检索请求关系校验](architecture/PUBLICATION_PROJECTION.md#10-实际retrieverequest关系检查)：投影摘要、Scope/过滤集合/代际、身份、预算与有效期核对；3项Go检查含真实modulewire往返及19种错配拒绝，全Go/vet通过。合成权威输入，真实授权/发布及接入应用未验，PART-01/03开放。见[证据](architecture/evidence/publication-projection-request-review.json)。

2026-10-13补齐[Go投影闭集加载](architecture/PUBLICATION_PROJECTION.md#9-go闭集加载与关系校验)：Load/Validate/ValidatedDigest及显式validate工具模式，3项新增实际Go/Python检查；全部174项Python/全Go/vet通过。authorized=false，真实快照和RetrieveRequest映射未完成，PART-03开放。见[证据](architecture/evidence/publication-projection-load-review.json)。

2026-10-13补齐[发布投影固定域摘要](architecture/PUBLICATION_PROJECTION.md#8-固定域摘要与独立go复核)：5组Python/实际Go金标、4项新增检查及全部171项Python/全Go/vet通过。Go工具schema_validated=false，尚无Go领域映射、权威快照与实时状态证据，PART-03开放。见[证据](architecture/evidence/publication-projection-digest-review.json)。

2026-10-13补齐[发布投影机器契约](architecture/PUBLICATION_PROJECTION.md#7-首个机器契约与离线检查)：闭集Schema及离线关系校验，6项新测试与全部167项Python回归通过。authorized/ready固定false；摘要、实际DTO过滤映射、权威仓储/当前状态未验，PART-03开放。见[证据](architecture/evidence/publication-projection-schema-review.json)。

2026-10-13补齐[发布投影身份](architecture/PUBLICATION_PROJECTION.md)：区分父发布摘要与子过滤投影摘要，定义闭集关系记录、请求集合等式、精确摘要顺序及一致性/失效边界。仅设计，Schema/金标/仓储及实时状态证据未验，PART-03和整体开放。

2026-10-13补齐[分库请求与授权边界](architecture/PARTITION_REQUEST.md)：明确目标集合、子Scope来源、发布投影身份、模型/布局核验、新Invocation与Grant重新签发，新增PART-01至06运行门槛。当前为设计交付，权威仓储/投影记录与调用适配器未实现，COORD-01/05及整体开放。

2026-10-13补齐[部署调用上限传递](architecture/RETRIEVAL_COORDINATOR.md#9-部署准入限制完整传递)：Plan保留MaxCalls，执行器预检部署上限与并发关系；3项新Go检查及全Go/vet通过。正式配置来源、真实后端与权限/发布仍未验，目标开放。见[证据](architecture/evidence/retrieval-call-cap-review.json)。

2026-10-13修正[执行器总字节预算与混合错误](architecture/RETRIEVAL_COORDINATOR.md#8-跨路线字节预算与混合错误修正)：3项新Go回归，跨路线累计16MiB且降级不退款，混合契约错误不得作为传输降级；两缺口已重现并修复，全Go测试/vet通过。序列化预算不等于峰值内存，真实运行验收仍开放。见[证据](architecture/evidence/retrieval-budget-review.json)。

2026-10-13新增[检索工作池与故障分类](architecture/RETRIEVAL_COORDINATOR.md#7-固定工作池与故障分类)：6项Go检查覆盖固定并发、取消/join、deadline、必需失败与可选整路线降级及非法Plan；全Go回归/vet通过。受控回调实验，不保证忽略context的适配器截止；真实后端、权限/发布与效果未验，COORD-02/04局部推进。见[执行证据](architecture/evidence/retrieval-executor-review.json)。

2026-10-13新增[跨KB交错合并](architecture/RETRIEVAL_COORDINATOR.md#6-合并实现与局部证据)：6项Go检查覆盖乱序稳定、单路线RRF票数、目标/配额/候选校验、取消与副本；全Go回归/vet通过。仅合成成功结果，尚无实际执行/故障降级、权限/发布/代际真实性或后端/Recall，COORD-03局部推进，整体验收开放。见[执行证据](architecture/evidence/retrieval-merge-review.json)。

2026-10-13新增[检索协调器设计与配额规划](architecture/RETRIEVAL_COORDINATOR.md)：按路线共享跨KB候选预算，目标不能被静默跳过，调用数准入、固定排名合并及必需失败/可选整路线降级规则成文；5项规划Go检查、全Go回归/vet通过。实际工作池、合并、授权/发布和真实后端未验，ROUTE-02与整体验收开放。见[执行证据](architecture/evidence/retrieval-plan-review.json)。

2026-10-13新增[RRF受控模块装配](architecture/FUSION_CONTRACT.md#7-受控模块装配与重启边界)：3项Go检查验证配置身份、注册端口、重建摘要、健康与停止/独立生命周期；全Go回归/vet通过。正式Profile真实性、多KB工厂、第二算法及真实后端/效果未验，整体验收开放。见[执行证据](architecture/evidence/fusion-module-review.json)。

2026-10-13新增[配置绑定ProfileRRF](architecture/FUSION_CONTRACT.md#6-pipelinequeryprofile绑定的融合实例)，捕获获准路线/实例/预算和Pipeline摘要，拒绝额外路线、后端身份字段错配及策略覆盖；4项新Go检查通过。真实性来源/工厂、必需失败与可选降级、多KB调度及实际后端/替换未验，ROUTE-02局部推进，整体验收开放。见[执行证据](architecture/evidence/fusion-profile-review.json)。

2026-10-13新增[首个RRF融合实现](architecture/FUSION_CONTRACT.md)，精确排名比较、同路线/候选去重及FusionOrigins完整来源；4项Go测试和2项独立Schema/Go映射检查通过。Candidate草案与远程Schema新增可选轨迹；获准路线、真实后端/权限、替换与Recall/性能未验，ROUTE-02及整体验收开放。见[执行证据](architecture/evidence/fusion-review.json)。

2026-10-13补齐[中文关键词独立适配](architecture/KEYWORD_ADAPTER.md)，固定核对OpenSearch 3.3.2许可/SmartCN构建与注册/search规格，明确与Qdrant/平台融合的分工、元组过滤、批量准备和6项运行门槛。当前官方latest元数据为3.9.0；3.3.2只作兼容基线，未部署/安装插件或验证召回/维护，D-05开放。

2026-10-13补齐[导出Identity→TrustHead Journal起点](architecture/JOURNAL_PROTOCOL.md)，绑定8项独立期望及清单原始摘要，从同一校验bytes提取typed库存并核对日志起始游标；4项新检查通过。控制面来源、真实写屏障/全库快照和数据库重放未验，D-04开放。见[执行证据](architecture/evidence/trust-export-baseline-review.json)。

2026-10-13补齐[TrustHead独立Journal起点](architecture/JOURNAL_PROTOCOL.md)，要求显式起始库存、游标关联、首条row/high_water与Bootstrap核对，不修改输入；4项新检查通过。真实来源/完整库存、ExportManifest/控制面锚点及数据库重放未验，D-04开放。见[执行证据](architecture/evidence/journal-trust-baseline-review.json)。

2026-10-13补齐[TrustHead类型化Journal](architecture/JOURNAL_PROTOCOL.md)，核对payload/声明版本、Bootstrap与链内高水位/issuer推进，拒绝清除Head的墓碑；4项新测试通过。首次源快照、可信锚点及数据库原子重放未验，ready_for_replay仍false，D-04开放。见[执行证据](architecture/evidence/journal-trust-head-review.json)。

2026-10-13新增[TrustHead类型化导出](architecture/EXPORT_CONTRACT.md)：实际Go字段映射/精确编解码、独立Schema及导出payload/record_id校验，3项Python测试含真实Go往返。其它记录族、Journal payload、真实来源/CAS/导入尚未验证，ready_for_import仍false，D-04开放。见[执行证据](architecture/evidence/trust-head-record-review.json)。

2026-10-13扩展[元数据1.1导出/Journal库存](architecture/EXPORT_CONTRACT.md)：新增上传操作及部署信任9族，空段仍需完整列出，旧1.0声明拒绝；4项增量测试通过。typed payload、真实源库存/导入/重放和高水位锚点仍待验，D-04开放。见[执行证据](architecture/evidence/export-trust-inventory-review.json)。

2026-10-13新增[部署信任仓储端口](architecture/KEY_TRUST_ACTIVATION.md#6-平台仓储与部署事务端口)，独立DeploymentScope/Authorizer和TrustRepository/UnitOfWork，共同提交Head/操作/退役/审计/outbox/journal；3项版本关系测试通过。真实CAS/事务、部署授权及导出库存扩展未实现，D-03/D-04开放。见[执行证据](architecture/evidence/trust-ports-review.json)。

2026-10-13补齐[公钥信任激活、防回滚与恢复](architecture/KEY_TRUST_ACTIVATION.md)，明确TrustHead/高水位、退役公钥指纹、事务提交及分发重启边界，历史备份恢复须独立控制面锚点；6项TRUST运行门槛待执行。本次为设计交付，仓储、机器记录、BuildLock扩展及协调器未实现，D-03/D-04开放。

2026-10-13补齐[公钥快照JSON加载与独立期望](architecture/GRANT_TOKEN_PROFILE.md#8-严格json加载与独立部署期望)，新增Schema、Go/Python版本大整数金标及LoadVerifier；Go3项/Python4项检查覆盖闭集字段、规范Base64、精确时间和期望错配。配置来源、高水位、防回滚/工厂分发未验，D-03/D-04开放。见[执行证据](architecture/evidence/key-snapshot-load-review.json)。

2026-10-13新增[不可变公钥Verifier快照](architecture/GRANT_TOKEN_PROFILE.md#7-不可变公钥快照与窗口)，复制公钥、记录版本摘要，验证快照/key签发与验证窗口；4项新增真实SDK测试覆盖重叠、截止、输入修改隔离和替换移除。没有工厂装配、可信分发/防回滚、刷新或当前授权，TOKEN-04局部推进、D-03开放。见[执行证据](architecture/evidence/grant-key-snapshot-review.json)。

2026-10-13接入[成熟Grant SDK适配器](architecture/GRANT_TOKEN_PROFILE.md#6-首个sdk适配器及局部运行证据)：go-jose/v4 v4.1.5与sum已锁定，4项Go真实签发/验签场景验证篡改、错绑定/header/key、算法混淆、精确时间和非法字段。成功只证明签名及请求绑定；Trust/Expected仍为合成输入，服务身份/当前授权、完整轮换与账本未验，D-03开放。见[执行证据](architecture/evidence/grant-sdk-runtime-review.json)。

2026-10-13补齐[Grant令牌及成熟SDK选型](architecture/GRANT_TOKEN_PROFILE.md)，固定go-jose/v4 v4.1.5源码/许可/发布证据，决定紧凑JWS、专用typ、Ed25519及受控公钥快照，规定精确时间和6项运行门槛。尚未安装SDK或验签，D-03/授权接入仍待验。

2026-10-13新增[17操作动作策略](architecture/REMOTE_OPERATION_POLICY.md)，金标scope.action按受控表修正；4项专项检查覆盖读写/控制分离及有界范围。当前主体、父任务/资源、验签和实时授权仍待验，authorized固定false，D-03开放。见[执行证据](architecture/evidence/operation-policy-review.json)。

2026-10-13扩展[全17操作Grant摘要对照](architecture/ACCESS_GRANT.md#6-全17操作局部摘要对照)：独立Schema及实际Go Decode入口检查每项请求/Scope金标，input变异改变摘要且解码拒绝。Go一项17子场景、Python两项检查；共享Scope动作仍为字段样例，实际operation权限映射/验签/服务身份和账本未验，D-03开放。见[执行证据](architecture/evidence/operation-grant-review.json)。

2026-10-13补齐[Grant摘要Go/Python金标](architecture/ACCESS_GRANT.md#5-gopython摘要金标)：6组规范字节/摘要一致，含实际解析请求和完整Scope，Go/Python各2项测试覆盖域/整数/Unicode与拒绝边界。只证明所选样例编码一致；17操作绑定、成熟验签、当前身份/权限及账本仍待验，D-03开放。见[执行证据](architecture/evidence/grant-digest-review.json)。

2026-10-13新增[远程Grant绑定契约](architecture/ACCESS_GRANT.md)、内部Bindings Schema及离线摘要/绑定检查，5项合成测试覆盖11项调用关系、精确时间和规范摘要。报告固定authorized=false；未选择token/成熟验签SDK，真实身份/权限、账本与跨语言摘要仍待验证，D-03开放。见[执行证据](architecture/evidence/access-grant-review.json)。

2026-10-13新增[封存投影包完整性](architecture/RETRIEVAL_EVALUATION.md#6-封存投影包完整性)，用平台规范编码分别绑定语料清单与问题/主体/快照/标注，读取真实本地UTF-8文件核对摘要、码点范围及库存；8项专项测试。正式原件/映射包、来源/人工标注、不可变资产及真实后端仍待验，阶段F未通过。见[执行证据](architecture/evidence/retrieval-seal-review.json)。

2026-10-13补齐[评测机器Schema与请求绑定](architecture/RETRIEVAL_EVALUATION.md#5-机器schema及请求绑定)，要求同题的问题文本/主体/权限和发布快照一致，闭集字段拒绝供应商载荷；新增5项检查及原7项评分检查通过，全量Python回归115项通过。真实封存来源、调用轨迹、输出映射和90%目标仍未验。见[执行证据](architecture/evidence/retrieval-schema-review.json)。

2026-10-13补齐[证据召回评测契约](architecture/RETRIEVAL_EVALUATION.md)和离线评分器：按版本/Block区间覆盖计算宏平均，错误/超时不删题，权限负例和泄露另计。7项合成测试通过；没有真实中文封存集或检索/质量/性能证据，报告固定正式验收未通过，阶段F待验。

2026-10-13新增[Qdrant独立向量适配设计](architecture/QDRANT_ADAPTER.md)，固定v1.16.2 commit、LICENSE及版本OpenAPI，明确vector端口、获准组合过滤、attempt隔离、标准内容重建和6项运行对照。源码/API入口不证明真实过滤或替换，关键词/嵌入/重排仍需配套，D-05开放。

2026-10-13新增[解析部署与替换边界](architecture/PARSER_DEPLOYMENT.md)，固定核对DocReader的22项直接声明、80项锁条目及系统工具链，明确OCR/VLM独立能力、字体/模型资产、子进程预算和6项双实现运行门槛。未安装/执行解析，完整许可、镜像身份和路由适配仍待验；D-03/D-05开放。

2026-10-13补齐[候选组件准入与部署边界](architecture/COMPONENT_ADMISSION.md)。WeKnora固定commit的LICENSE/Compose/go.mod/第三方声明及Dockerfile摘要已核验，明确ParaDB默认镜像、latest制品锁定和平台/引擎数据隔离。仅源码及官方发布元数据证据；完整依赖许可、制品摘要、部署和替换测试仍待完成，D-05开放。

2026-10-13补齐[上传状态转换规则](architecture/BLOB_CONFIRMATION.md#7-状态转换与fencing高水位)：保留历史fencing高水位，验证版本递增、候选/attempt固定、取消意图单调及未知结果恢复。新增9项转换测试；facts仍为合成输入，实际CAS、事实来源、存储和恢复待验，D-03/D-04保持开放。见[执行证据](architecture/evidence/upload-transition-review.json)。

2026-10-13新增[UploadOperation机器记录](architecture/BLOB_CONFIRMATION.md#6-机器记录与离线核对)与独立期望核对，7项合成检查覆盖八状态、精确整数、候选/结果错配、未知提交和封闭错误。Schema生成一致性及94项Python回归通过。报告固定不具备确认资格；真实状态转换/CAS、摘要重算、存储桥接、权限、事务及恢复仍待完成，D-03/D-04开放。见[执行证据](architecture/evidence/upload-operation-review.json)。

2026-10-13补充[上传确认与恢复协议](architecture/BLOB_CONFIRMATION.md)，明确接收、对象持久及元数据确认边界，UploadOperation身份/阶段、幂等摘要、提交未知探查、保留清理及9项双实现运行门槛。此次为架构文档交付，尚无确认协调器、持久仓储、暂存桥接或恢复运行证据，D-03/D-04继续开放。

2026-10-13新增[multipart上传与隔离暂存](architecture/BLOB_MULTIPART_TRANSPORT.md)：新增7项Go测试，覆盖5MiB流式写入、长度/摘要/结构/预算拒绝、前后模拟访问回调、失败清理及真实HTTP中断和服务器读取deadline。只返回验证receipt，不确认BlobRef；认证/Grant、存储提交、账本和恢复仍待实现，D-03保持开放。复核结果见[执行证据](architecture/evidence/blob-multipart-review.json)。

2026-10-13新增[有界SSE接收与本地HTTP验证](architecture/SSE_TRANSPORT.md)，接收真实响应Body，覆盖三类换行/BOM/多行data、回显/类型/预算、逐批闸门、终态延迟至正常EOF和取消关闭。8项Go场景含3项真实localhost HTTP测试（分块、模拟撤权、阻塞取消/sink关闭和静默peer超时）；Go全包测试/go vet与87项Python回归通过，见[执行证据](architecture/evidence/sse-transport-review.json)。访问闸门仍是测试回调，未接入真实授权/Grant、账本或模型；不能把网络读取结束当作后台任务停止。D-03继续开放。

2026-10-13目标已恢复为active，继续完成暂停前的控制接口草稿。D-03补齐[状态/取消映射](architecture/CONTROL_WIRE_MAPPING.md)：原/控制请求身份区分、六种状态、15类封闭结果及两种取消响应；22项实际Go样本和7类非法变异通过，复合search也完成共享检索DTO对照。Go全包测试/go vet、87项Python回归及全部8个映射脚本通过，见[执行证据](architecture/evidence/control-wire-review.json)和[17操作覆盖表](architecture/REMOTE_MAPPING_COVERAGE.md)。逐操作局部数据样本已齐，但HTTP/multipart/SSE、当前授权/Grant、账本、真实取消和操作级全量领域验证仍未通过，D-03继续开放。

D-03补齐[生成请求与回答事件映射](architecture/GENERATION_WIRE_MAPPING.md)：实际Go请求/预算、五类联合事件、Trace/timings及顺序检查；核对独立引用白名单/配置版本、完整增量正文与唯一终态，error不会成为成功回答。三种事件流样本、全部5类事件、7项非法变异和独立Schema对照通过；Go全包测试、受影响包新增测试、go vet与87项Python回归通过，见[执行证据](architecture/evidence/generation-wire-review.json)。仍没有模型、SSE帧/网络、当前授权、实时deadline/token计量或sink取消；control和完整远程协议验收未闭合。

D-03补齐[远程安全错误映射](architecture/ERROR_WIRE_MAPPING.md)：实际Go编码/解码检查内外请求关联，15种Schema错误码使用封闭安全模板，丢弃原始文案并限制重试建议。完整Code枚举的独立Schema前后检查、非法cause/code/ID拒绝、Go全包测试/go vet及87项Python回归通过，见[执行证据](architecture/evidence/error-wire-review.json)。供应商分类、调用账本、HTTP/重试策略、生成/控制及真实协议验证仍未完成。统一契约已纠正V1不存在details字段的旧描述。

D-03补齐[Blob远程映射](architecture/BLOB_WIRE_MAPPING.md)：三项put/open/delete的实际Go metadata编解码、关联ID及上传期望检查，另有EOF/长度/SHA-256/字节预算/context边界和响应头检查。独立Schema前后往返与3项非法样本拒绝通过，Go全包测试/go vet及87项Python回归通过，见[执行证据](architecture/evidence/blob-wire-review.json)。没有multipart/HTTP适配器、阻塞读取取消、Grant/当前授权或实际存储调用；生成/控制/安全错误映射及真实协议验收仍待完成，D-03未关闭。

D-04新增[原件复制与映射契约](architecture/BLOB_COPY.md)、Blob复制清单Schema及只读流式文件校验。11项专项测试使用真实临时文件，覆盖8MiB读取、篡改/截断、身份/space/版本及独立库存；87项Python回归和复制/KB状态/导出Schema一致性通过，见[执行证据](architecture/evidence/blob-copy-review.json)。未执行实际复制或存储适配器调用，不证明目标绑定不可变、原子映射、重启或回退；文件/数据库状态和typed记录契约仍待补齐。

D-04补齐[KB后端迁移状态机](architecture/MIGRATION_STATE.md)和记录Schema，区分切换、重启核对、恢复写入、取消/回退及保留窗口。13项专项测试、76项Python回归和迁移/日志/导出Schema生成一致性通过，见[执行证据](architecture/evidence/migration-state-review.json)。输入facts仍是离线合成观察值；真实CAS/租约接管/授权/切换、数据库与文件迁移状态、typed载荷及流桥接尚未完成，D-04保持开放。

D-04补齐[迁移日志与水位协议](architecture/JOURNAL_PROTOCOL.md)、完整事务日志包Schema和离线前驱链校验。明确V1单JournalHead提交屏障、独立快照/Head范围核对、完整事务payload与原子重放receipt；11项专项测试、63项Python回归和两份Schema生成一致性检查通过，见[执行证据](architecture/evidence/journal-contract-review.json)。证据只覆盖合成链，不证明真实提交调度、可信Head、目标数据库重放或切换；typed记录payload、迁移状态机器契约及正式运行验收仍未完成。

D-04新增[元数据导出契约](architecture/EXPORT_CONTRACT.md)、ExportManifest生成Schema与离线清单/记录包络校验，明确权威记录族、完整事务快照、租约失效和隔离导入闸门。9项专项测试、52项Python回归、生成一致性及部署结构检查通过，见[执行证据](architecture/evidence/export-manifest-review.json)。各类payload Schema、原件复制包、认证凭据安全迁移、真实提交水位/导入事务及正式部署核对仍待完成，D-04保持开放。

D-04进一步补齐[受控构建锁](architecture/BUILD_LOCK.md)、锁Schema和本地离线检查命令，关联部署/清单/制品/保留策略摘要，拒绝覆盖不全、版本失配和检查期间文件变化。10项专项测试及43项Python回归通过，见[执行证据](architecture/evidence/build-lock-review.json)。正式运行制品身份、来源信任、持久化epoch核对、导出包及迁移水位仍未完成，D-04未关闭。

D-04保留与回收契约已补齐策略Schema和离线期限关系校验：回退/重放/恢复窗口、幂等结果、孤立产物保护和64位溢出共7项测试通过，全量Python契约回归33项通过。权威保留锁、生产GC、BuildLock、ExportManifest、迁移水位机器契约和正式部署对照仍待实现/验证，D-04尚未关闭。详见[保留与回收契约](architecture/RETENTION_CONTRACT.md)及[复核证据](architecture/evidence/retention-contract-review.json)。

按用户最新要求，当前完善架构文档及架构契约验证，业务服务尚未启动。文档入口见 [设计索引](README.md)。设计完成、内核实现和生产验证分别记录。

2026-10-12已执行 [架构设计验收](architecture/ACCEPTANCE_REVIEW.md)：总体架构评审有条件通过，13项现有检查全部通过；复合能力映射、冻结哈希精确编码、远程模块机器契约尚未闭合，V1契约冻结与原方案完整运行验收未通过。报告列出逐项证据和关闭标准。

同日复核已补齐复合端口、内核能力映射及整组配置规则：两个Go内核试验与5项Python复合场景通过；允许独立扩展通道。D-01设计缺口已收敛，D-02/D-03与正式部署/真实适配验收仍待完成。

后续复核补齐D-02精确冻结编码、Schema、两级哈希及HashFormat。7项内容金标由Go/Python交叉核对，6项Python测试验证差异/非法输入；管理Revision响应同步格式、块哈希与附件清单。实际提交映射/事务、大载荷流式处理和D-03远程契约仍待完成，不能把哈希辅助包视为完整知识治理。

D-03新增17项远程模块OpenAPI与传输规则，7项结构/日期/有限数值测试和规范检查通过。完整字段映射/往返样例、服务认证/Grant/当前授权验证、调用账本和实际取消/网络联调仍待完成，D-03尚未关闭。

解析端口进一步补齐实际Go/wire编解码，7组金标通过独立Schema及规范字节往返；请求/响应关联、完整内容字段、大整数/纳秒时间和非法输入有证据。其他端口映射、完整Schema/领域验证、真实解析及授权/网络仍未完成，D-03未关闭。

嵌入/检索/重排新增实际Go/wire映射与三个独立Schema前后对照；Generation补齐状态/绑定/证明，结果校验规格、数量、维度及本次目标来源。索引/文件/生成/控制映射、认证/Grant与网络验收仍待完成，不能把字段过滤检查当成真实后端过滤证明。

索引/复合prepare/verify/delete六项操作新增Go/wire对照与独立Schema往返，准备证明按独立权威期望核验；删除使用显式mode。映射/租约/真实Verify/发布与删除业务未执行；文件/生成/控制和安全/网络门槛仍待完成，D-03未关闭。

### 架构设计交付

- [x] 模块边界、分层依赖、数据所有权和部署设计。
- [x] 标准对象、端口语义、错误、事件和配置兼容规则。
- [x] 发布 CAS、索引代际、撤回、幂等和任务恢复设计。
- [x] REST/MCP 四项伙伴能力与身份传递设计。
- [x] 架构决策、候选组件矩阵和待验证能力清单。
- [x] 替换、迁移回退、故障场景与召回/性能验收方案。
- [x] 一致性审查：文档/知识库代际分离、权威租约、尝试级索引隔离、迁移日志、撤销时序和修订引用。
- [x] 模块清单/部署配置/索引与查询策略 JSON Schema、装配语义、生命周期与结构验证样例；运行注册中心与完整语义校验仍待实现。
- [x] 逻辑数据模型、草稿冻结、记录约束、事务矩阵、删除回收和仓储替换门槛。
- [x] 管理界面/命令契约与原始20模块、五类前期成果、最终验收条件的证据审查。
- [x] WeKnora固定commit/API核验：文本写入与分块接口，检索投影适配方案及试验门槛；未部署运行。
- [x] 伙伴及管理OpenAPI草案、表格/图片/音频类型化载荷与DTO/流事件样例；语义校验和正式协议联调仍待完成。
- [x] 管理API的26项操作、审核队列、冻结版本读取及契约生成/结构样例；端点实现和管理界面仍待完成。
- [x] 伙伴/管理OpenAPI3.1.1规范校验、必需字段拒绝对照、文件哈希报告与校验环境版本记录；不代表运行端点已通过。
- [x] MCP协议/Go SDK候选固定源码核验、传输认证、四工具映射与互操作方案；未集成SDK或运行联调。
- [x] 从伙伴OpenAPI生成四项MCP自包含工具契约与过期检查；运行服务的schema/isError/身份行为仍待实现验证。
- [x] 显式多路索引/召回配置、通道覆盖、代际兼容和全局候选预算设计；路线语义校验与运行替换仍待实现。
- [x] 假设模块的离线装配语义样例（能力、版本、依赖、通道与预算）；真实注册中心和后端能力验证仍待实现。
- [x] 复合组跨Pipeline绑定的完整性检查与拆分拒绝样例；后端内部耦合和实际整组迁移仍待验证。
- [x] 首批10项Go标准端口与核心/内容/检索模型，独立编译与go vet通过，仅依赖context/io/time；尚无能力实现。
- [x] 主要能力及辅助仓储Go草案共29个接口，补齐接入/审核/生成/反思/记忆/图谱/任务/事务/反馈/评测观测；单包编译与静态检查通过，缺行为实现和传输映射。
- [x] Go配置/代际/查询快照、审批请求记录与命令分离、多通道准备证明及回答追踪字段对照；不变量与映射仍待行为测试。
- [x] 代际准备关系校验器及复合/独立通道证明测试，拒绝16类不一致目标；真实后端Verify、权限、审批、栅栏与发布CAS仍待实现。
- [x] 文件级离线配置检查入口：读取正式部署格式及候选清单，输出文件哈希、依赖顺序与未验证门槛；8项输入/Schema/引用/信息隔离测试通过。正式部署归一化和运行就绪仍待实现。

以上表示设计已成文，不表示真实适配器已验证。WeKnora 标准内容往返、有效版本过滤规模、中文全文引擎与模型选择仍需试验收敛。

### 实现与验证待办

- [x] 架构方案归档到 docs。
- [ ] 标准数据模型/主要端口草案已成文；完成全量字段、状态机、边界验证及传输映射的契约验收。
- [ ] 模块清单、注册中心、依赖和契约版本检查。
- [ ] 配置加载、不可变配置版本、启动健康检查和资源回收。
- [ ] 两种解析器与两种文件存储实现的公共契约测试。
- [x] 可运行的离线配置与清单检查命令；生产启动检查命令仍待实现。
- [ ] 将架构契约生成 Go 类型、JSON Schema、OpenAPI，并运行适配器验证方案。
- [ ] 自动化测试、静态检查和持续集成。

## 后续阶段：保持完整目标

- [ ] 空间/用户/知识库权限及凭据管理。
- [ ] 不可变知识版本、人工审核、原子发布与撤回。
- [ ] 多格式解析生产适配器及云端嵌入/重排/生成模型。
- [ ] PostgreSQL/全文/向量索引、Redis 任务与持久化事件。
- [ ] API 与 MCP 共用知识应用服务及伙伴联调。
- [ ] Vue 知识管理界面。
- [ ] 检索反思、反馈草稿及降级处理。
- [ ] 模块迁移、索引代际、数据导出导入与回退。
- [ ] 长期记忆与图谱的可替换实现。
- [ ] 独立中文评测集与 Recall@10 ≥90% 验收。
- [ ] 固定负载下的响应性能、部署与恢复验收。

以上未完成项不会通过测试替身、示例数据或接口声明替代。当前没有真实行业召回率或生产负载测试结果。

内部模块装配内核已实现并通过包测试：受控工厂目录、强类型端口、能力/版本/特性检查、依赖槽与拓扑、启动健康检查、失败回收和只读快照。配置读取拒绝未知字段、重复JSON键、非对象根、尾随数据及大小/嵌套超限。细节和生产接入边界见 [内核设计](architecture/KERNEL_RUNTIME.md)。上方涉及“真实注册中心待实现”的条目指完整正式部署接入及真实适配器验收，不能与内部内核混同。

2026-10-12执行完整 `go test -cover ./...` 和 `go vet ./...` 通过；contracts语句覆盖率92.6%，kernel为89.6%。这些测试覆盖现有关系校验器和假设模块装配，不证明生产解析、权限、发布、检索、迁移或召回目标通过。正式部署Schema到内部计划的归一化、制品锁、流水线绑定与应用入口仍待实现。
