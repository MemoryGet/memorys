# 清理 worker 的真实身份与独立授权

后续增量：[清理模块保留绑定与可信装配](CLEANUP_REGISTRY_RUNTIME.md)已接入当前封存目录和kernel命名模块，当前585项/326检查及本页回归通过。生产部署控制、制品/数据集证明、远程Grant和完整V1仍开放；本轮间歇失败记录保留。

2026-10-18：在[删除队列](DELETION_QUEUE_RUNTIME.md)之上新增cleanup.WorkerAuthority，使用标准Authenticator/Authorizer接入实际RS256 service身份、PG当前身份状态和封存ACL。删除任务、逐目标任务及索引后端的校验回调均可接入同一控制器，无须在回调中编造权限。生产身份发放、worker启动装配、模块注册及远程Grant仍未完成。

## 身份与控制器生命周期

NewWorkerAuthority要求明确的worker、空间、单KB、access_token、认证/授权提供方及独立目标准入函数。构造时认证并固定service主体、issuer、client及无委托关系；user主体、主体不符、缺少提供方（包括typed nil）均拒绝。后续每次决策重新认证且要求完整Principal与初始值一致，不能在持有控制器期间更换客户端或身份。

控制器复制凭据，认证调用获得临时副本并在返回后清除。Close关闭控制器、清除其持有的凭据和活动作用域；不会修改调用者仍拥有的凭据。String/GoString只返回redacted标记。调用者须自行保管和清除原凭据，本轮未实现凭据获取/自动轮换或生产密钥管理。

每个Scope调用重新取得publication.cleanup.execute决策，作为新周期唯一活动执行作用域，并撤销之前的本地索引请求。VerifyScope只接受控制器实际发出的当前作用域；不仅比对字段，还重新认证、调用当前Authorizer并核对策略版本和到期。策略变化时旧作用域拒绝，新周期可重新取得新版本；同周期不接受把旧ScopeID或Action改写后冒充权限。

实例供一个顺序消费者使用，内部最多保存一个周期和一个索引请求。同步保护用于防止凭据/作用域在关闭或更新期间被不一致地读取，不代表支持多个并行目标共享该控制器。提供方必须遵守context，控制器不为不合作的调用创建后台执行。

## 任务与索引分别决策

AuthorizeJob核对真实任务的空间、资源及配置摘要，再以ResourceIDs申请当前publication.cleanup.execute决策。PG Authorizer的权限粒度仍为KB动作，ResourceIDs额外检查文档确实属于已授权空间/KB；本轮没有新增逐文档ACL语义。

IndexRequest先重算固定CleanupOperation并核对空间/KB/类型，调用独立目标准入，再重新认证并分别申请执行资源和index.delete资源授权。两个动作必须对应同一当前策略版本，不能通过修改执行作用域的Action取得索引权限。索引作用域由真实Authorizer新发出，作用域与请求截止不得超过当前调用截止；目标ProfileDigest及GenerationID来自固定计划。

索引后端再次VerifyScope时，还会重新执行该索引操作的独立准入，并核对仍是同一周期/同一索引请求，然后重新认证与核权。这样，仅保存之前签发的本地索引作用域不能绕过后来失效的目标或权限。索引scope属于进程内受控回调上下文，不是可传往远程模块的签名Grant，也没有替代远程签名/账本/受众及重放检查。

## PG 当前目标准入

CleanupTasks.VerifyDeletionOperation从权威CleanupPlan读取固定目标，重新构造完整操作并逐字段比对；检查成员属于真实删除Job、任务运行中、删除命令已提交、文档已删除，目标当前running且租约仍有效、worker等于执行主体。事务开始和提交前均调用实际作用域校验。

Operation保持稳定效果身份，不携带每次租约的fence/attempt。精确租约token/version/attempt核对仍由Worker的VerifyLease在执行前后承担，不能用VerifyDeletionOperation替代；该准入也没有证明外部模块部署身份、稳定幂等实现或实际后端效果。默认组合继续使用已有PG索引适配器及独立效果回读。

## 真实运行与验收边界

579项输入在PG16通过317项原生检查，七组独立身份/队列/清理/任务/删除/历史/当前复核、Go全量测试/go vet、228项Python及三份OpenAPI通过。各报告对应同一未变化源码清单，临时目录和端口已独立确认清理；本轮仅PG16。

新增独立20秒fixture阶段，未增加业务预算或逐目标4秒租约。探针通过SDK生成/验签RS256，独立worker issuer/key/service/client/token实际登记PG；查询当前token、client、key及封存ACL。该阶段记录223次成功认证、220次实际授权决策，仅用于证明反复核查，不是性能指标或固定调用次数契约。

ACL版本9只有publication.cleanup.execute。真实消费者领取目标后，索引查询因缺少独立index.delete授权拒绝，目标进入unknown、Job failed，实际索引记录保留且没有删除fence。现有消费者把此类无法继续核验的结果隔离为需要对账，不会因为后来补权而盲目重试该任务。

版本10补齐两个动作，旧版本作用域拒绝。逐项撤销token、client和key后队列发现均拒绝且不返回条目。发现期间在Job授权后激活版本11（只保留index.delete），最终作用域复核阻止整个页输出。版本12恢复双授权后，轮询器重新取得真实作用域并自动完成另一个任务；索引实际fence存在、投影行归零，完成目标租约结束。终态不再发现，失败任务重放不增加尝试。目标结束后的操作准入、跨KB及控制器Close后调用均拒绝。

独立Python核对实际身份登记、key/token生存期、封存策略与每份授权决策的主体/动作/资源/版本，核对真实Job/成员/目标/租约与实际索引效果。单元测试还覆盖不能改名作用域、资源拒绝、客户端变化、准入失效、缺失提供方、凭据拷贝/清除及诊断脱敏。

新增两份源数据仍使用明确的元数据和治理授权fixture，通过真实软删除事务创建Job；它们不是候选至发布流程的新生产验收。此前实际发布/撤回/删除七目标链路和全部既有回归同步通过，但其旧fixture授权分支仍保留为独立测试，不因此全部升级为真实worker授权证明。

见[实库证据](evidence/worker-authority-postgres16-review.json)、[身份与效果独立复核](evidence/worker-authority-runtime-review.json)、[本地检查](evidence/worker-authority-local-checks.json)、[队列](evidence/worker-authority-queue-review.json)、[消费者](evidence/worker-authority-cleanup-runtime-review.json)、[任务](evidence/worker-authority-job-status-review.json)、[删除](evidence/worker-authority-deletion-review.json)、[历史](evidence/worker-authority-history-read-review.json)、[当前读取](evidence/worker-authority-current-read-review.json)及[资源清理](evidence/worker-authority-cleanup-review.json)。

生产Bootstrap及身份/ACL发放与审计、可信模块注册和配置装配、远程Invocation Grant/发送与写入屏障、持久调度、显式对账/重试/取消、其他后端、并发/恢复/续租、Purge、完整IAM/TaskRepository、迁移/回退及效果/性能和完整V1仍未验收。已有租约间歇失败记录没有因此关闭。
