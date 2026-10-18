# 删除清理任务：固定目标消费与实际运行状态

后续增量：[清理模块保留绑定与可信装配](CLEANUP_REGISTRY_RUNTIME.md)已接入当前封存目录和kernel命名模块，当前585项/326检查及本页回归通过。生产部署控制、制品/数据集证明、远程Grant和完整V1仍开放；本轮间歇失败记录保留。

后续增量：[真实service身份及独立清理/索引权限](WORKER_AUTHORITY_RUNTIME.md)已在PG16通过当前身份/ACL、目标准入与实际效果验证；当前579项/317检查及本页既有回归通过。原阶段fixture授权记录保留，不能把其全部视为新身份链路验收。生产worker装配、模块注册及远程Grant仍开放。

后续增量：[删除队列发现与有界轮询](DELETION_QUEUE_RUNTIME.md)已在独立KB实库通过自动发现、续权分页、三任务完成及取消；下文568项/302检查为本阶段历史快照，当前574项/309检查证据见该文档。生产持久调度、worker IAM及完整V1仍未验收。

更新：2026-10-18。当前实现是按任务ID调用、每次至多处理一个目标的有界消费者；不是持续队列调度器，也不执行原件Purge。

## 固定范围与标准端口

软删除事务新增document_delete_job_targets，将文档该删除epoch之前所有已登记CleanupPlan的目标共同冻结。范围包括历史换版、撤回及本次删除的当前源，不只消费删除回执的最后一份计划。数据库提交约束检查完整覆盖，成员绑定同一空间/KB/文档及删除epoch；命令提交后不允许补写或改删成员。未登记派生产物的发现和物理Purge不在此任务范围内。

contracts.DeletionCleanupStore提供NextDeletionCleanup/ReconcileDeletionCleanup；CleanupTargetRunner复用现有RunOne目标执行端口。cleanup.DeletionWorker复制请求及作用域，一次只运行一个固定目标，再协调任务状态。不同Job Store或目标运行器可通过同一调用端口替换，本轮没有证明第二套协调存储或其他引擎已通过替换验收。

每个目标继续使用CleanupTasks的真实租约、attempt、fencing token、独立回执核验和Lookup优先语义。聚合任务没有虚构额外的任务级租约。并发消费者的目标争用仍由逐项Claim/CAS阻止，新的聚合并发/故障矩阵尚未验收。

## 运行状态与完成门禁

不可变Job初始定义仍为queued；单独的document_delete_job_progress记录state、attempt、row_version、total_targets、completed_targets及安全错误追踪。首次领取协调任务时转running，aggregate attempt变为1；目标进度变化时行版本递增。GET任务来源将不可变定义与实际进度组合，返回真实running/completed/failed和进度，不再永远投影初始queued。

进度由固定成员关联的真实cleanup_progress汇总。所有目标成功还不够：默认PostgreSQL协调器在完成前独立检查每个实际索引的删除fence和零残留投影行，SQL更新约束重复执行同一门禁。伪造成功任务记录或直接更新聚合completed均不能绕过实际效果；缺失或不支持的后端事实不会被当作成功。

当前完整运行验证覆盖PG原生关键词索引。默认PG完成门禁不接受未核实的graph/memory/cache或其他引擎效果，须接入相应独立事实核验和协调存储实现。每个目标的模块解析、任务执行和index.delete授权在原生探针中仍为明确的fixture，不代表生产worker身份、Grant或模块部署绑定已完成。

没有目标的任务需经真实协调调用才能完成，进度为1。已完成任务重复调用不再领取目标或修改版本。模块不可用导致的unknown结果进入failed，保留目标事实并返回“Cleanup requires reconciliation”的安全状态；失败任务重复调用不盲目执行。控制端口现在区分取消与显式重试：重试要求调用者携带当前Job版本，任务为failed、没有有效目标租约、且所有未成功目标都是已知failed；unknown、pending或running目标一律拒绝。重试审计和outbox先于`failed -> running`状态切换提交，目标回到pending但保留既有attempt/fencing单调性。它不把未知外部效果当作可重试，也不提供隐式自动重试。

## 权限与内容保留

协调存储要求独立作用域校验和实际Job/Resource准入回调，核对任务归属、KB与冻结配置摘要。目标执行器另行获得index.delete授权，不能把cleanup作用域简单改名作为权限证明。查询状态继续使用真实认证及独立resource.view_job，运行状态不是正文读取授权。

原件和Revision保持保留。默认索引删除沿用实际owner、删除fence和迟到写入拒绝；其他文档的代际不能随本Job清除。完整生产IAM、读/写/发送屏障、审计及持续调度仍待实现。

## 当前证据

568项输入在PG16通过302项原生检查，Go全量测试/go vet、228项Python与三份OpenAPI通过。独立清理、任务查询、删除及历史/当前读取复核同步通过，报告均对应同一源码快照，临时实例目录与端口已独立确认清理。本轮只重验PG16，未借用旧PG15/18快照。

真实八个epoch发布/撤回链路及epoch 9软删除产生7个退役来源目标。实际消费者使用每项4秒租约和既有PG索引适配器完成全部目标，GET逐项显示running及最终completed；独立Python重算每份清理操作摘要/结果引用，核对attempt/fence/版本、实际代际归属、删除fence、零残留记录及所有退役历史计划覆盖。完成任务重放不产生新尝试，零目标任务实际完成；原件SHA-256及字节保持不变，另一文档的实际代际保留。

故障试验先伪造成功任务元数据，协调器及SQL完成门禁均拒绝，然后只恢复该私有fixture的注入数据再执行实际清理。独立的负向源元数据和治理授权fixture产生另一实际删除任务；模块解析失败后，任务failed、目标unknown、索引记录仍在，重复调用没有盲目重试。负向源创建不是实际发布流程验收，正向七目标试验才使用完整发布链路。

新增故障分支曾以sql.ErrNoRows中止；报告保留在deletion-cleanup-fault-fixture-failed.json。改用独立故障fixture后通过，不据此关闭原有租约间歇失败或声称生产稳定性达标。新增删除清理验证使用独立20秒fixture阶段，任务租约和业务请求预算未增加，不能当作性能验收。

见[实库证据](evidence/deletion-cleanup-postgres16-review.json)、[实际清理独立复核](evidence/deletion-cleanup-runtime-review.json)、[本地检查](evidence/deletion-cleanup-local-checks.json)、[任务查询回归](evidence/deletion-cleanup-job-status-review.json)、[删除回归](evidence/deletion-cleanup-deletion-review.json)、[历史回归](evidence/deletion-cleanup-history-read-review.json)、[当前读取回归](evidence/deletion-cleanup-current-read-review.json)及[资源清理](evidence/deletion-cleanup-cleanup-review.json)。

生产worker身份及模块装配、其他目标/引擎的核验、人工对账、执行审计、完整TaskRepository、Purge、并发与恢复矩阵、迁移/回退、效果/性能及完整V1仍未验收；取消/显式重试控制、事务审计/outbox、EventBus和Runner已有独立增量，但生产凭据和宿主消费装配仍未验收。显式重试现已接入管理HTTP路由和既有OpenAPI命令契约，实际生产身份、授权和端到端重试证据仍未验收。
