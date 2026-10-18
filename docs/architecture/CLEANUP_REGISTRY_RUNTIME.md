# 清理模块的保留绑定与可信装配

后续增量：[上传读超时与取消分类](UPLOAD_DEADLINE_RUNTIME.md)已有修复前3次确定性复现及修复后60次真实HTTP回归；当前586项输入/PG16 326检查和本页既有回归通过。它只关闭已复现的读错误分类原因，审批/续租和其他稳定性与完整V1保持开放；临时目录清理限制见该文档。

2026-10-18：新增标准CleanupModuleSource、PG封存部署目录及adapters.CleanupRegistry，将[真实worker身份](WORKER_AUTHORITY_RUNTIME.md)与现有kernel装配的命名索引端口组合。模块和连接须由宿主事先构造，任务不能提供工厂、连接地址或可执行代码。当前限PG关键词索引的进程内组合；生产部署控制面、制品验证及跨源写屏障仍未验收。

## 冻结来源与当前部署分别核对

CleanupModuleBinding记录空间、KB、BindingReference、ModuleInstanceID、目标ProfileDigest、GenerationID、BackendEpochID、Channel，以及当前部署版本、Provider、ImplementationVersion、ContractVersion和RuntimeConfigDigest。它与不可变CleanupPlan及binding_definitions分开保存：旧索引的来源不能改变，当前是否允许调用该旧模块则可以通过新的封存部署版本撤销。

PG增加cleanup_module_snapshots/bindings/heads。目录先插入、再封存、最后激活；绑定只可在未封存时追加，禁止更新或删除。已封存快照不可改，活动head只能前进且必须指向封存快照。空快照可撤销全部调用资格；它不会删除来源元数据或伪造任务已成功。目录条目可保留旧Profile，清理不要求旧目标变成当前查询/写入Profile。

CleanupModules读取前执行独立的当前租约/冻结目标准入，串行化读取当前封存目录，开始及提交前核验真实作用域。提交后另用主库查询当前head，阻止事务快照掩盖读取期间已发生的激活。返回值不是权限令牌，配置控制面仍须限制谁可以发布目录；当前SQL是验证原型，不是完整生产迁移、RBAC或审计实现。

## 命名模块及配置选择

宿主通过已有kernel.BuildBound预先构造模块、检查契约及特性，并显式提供RuntimeConfigDigest到Runtime的允许列表。解析器复制这份map，检查键与实际kernel ConfigVersion一致。目录只能从这些已装配运行时中选择，不能按操作字段创建新服务，也不会回退到当前默认索引。

解析器先重算CleanupOperation，复制传给提供方的参数，并核对当前执行作用域、目录全部目标/来源字段。随后核对实际kernel快照的模块实例、Provider和精确实现版本，按命名ModuleBinding调用GetBound；要求index_cleanup、匹配契约，以及generation.delete、generation.inspect、delete.fence、fence.durable。当前只接受keyword目标，其他渠道和后端须另行接入验证。

契约和特性声明本身不是制品身份或后端耐久性证明。kernel ConfigVersion绑定已解析宿主计划及Manifest，未绑定预构造DB对象的真实数据集身份、凭据或部署制品；这些仍须与现有StoreBinding/制品锁及生产Bootstrap贯通。目录也不是远程Invocation Grant。

## 返回引用的失效及回执

Resolve返回的执行器固定操作和当时的完整部署绑定。每次Lookup/Execute重新取得同周期作用域、读取当前部署、匹配完整绑定和运行时；版本变化、条目撤销、配置/实现不符、运行时缺失或关闭均拒绝。Source接收独立操作拷贝，不能改写后续核对基准。WorkerAuthority.CurrentScope负责重验现有周期，不会为了模块解析重建一个周期或无意撤销当前索引请求。

实际后端仍为kernel管理的IndexCleanupBackend和已有IndexCleanup适配器。VerifyReceipt重新取得当前绑定并独立读取实际owner/fence/行数，不接受单纯回显成功字段。索引动作继续单独获权，逐目标fence/version/attempt继续由原Worker.VerifyLease核对；注册解析器没有替代这些责任。

关闭kernel后GetBound及管理端口拒绝新调用，包括之前保存的执行器。宿主仍须先排空在途工作再Close，本轮没有证明强制中断在途外部删除或自动回收不合作模块。新建相同宿主配置的运行时可重新接入当前目录，但不会使旧的已关闭引用重新有效。

逐次重查缩小失效窗口；部署head与索引删除的跨控制源原子提交屏障尚未实现，因此不能宣称“撤销与任何并发删除完全线性一致”。最终发送/写入屏障、外部稳定幂等协议及生产模块身份仍是独立验收项。

## 实际证据

585项输入在PG16通过326项原生检查，八组独立装配/身份/队列/清理/任务/删除/历史/当前复核通过，Go全量测试/go vet、228项Python和三份OpenAPI也通过；报告对应同一未变化源码清单。临时目录不存在且55432端口无监听，已独立核对。本轮仅PG16；旧PG15/18记录不充当当前证据。

新增独立20秒fixture阶段，保留逐目标4秒租约和原业务预算。宿主计划显式声明registry-positive-engine与registry-revoked-engine，默认index_cleanup故意指向后者。实际RS256 service身份和当前PG ACL验证保持接入，目录不是从入站操作推导的工厂配置。

真实删除任务领取负向目标后，版本1目录允许实际absent查询；激活版本2空目录后，之前保存的执行器不能Execute。版本3错误实现、版本4未装配配置、版本5错误旧Profile均拒绝解析。版本6恢复正确条目，目标及目录仍有效时关闭kernel，原执行器拒绝调用。该目标以unknown隔离为failed，实际索引仍保留一行、没有删除fence，未伪装清理完成。

重新构造相同宿主配置后，新注册解析器通过真实队列消费另一任务，按固定命名实例选择后端并完成实际PG删除：fence存在、投影行归零，目标succeeded及Job completed。目录封存后修改、head倒退及未封存版本7激活均被SQL约束拒绝。

独立Python重算受控宿主计划的kernel配置摘要和固定清理操作摘要，逐项核对实际Manifest、保留Profile、目录版本/封存状态、每次绑定读取、Job/目标/代际/租约及实际效果。Go回归将默认实例设为另一个后端，证明不能误用默认端口；还覆盖Source修改参数、错误Provider/契约/配置/实现、缺少durable特性及关闭后的旧引用。单元测试中的特性声明是受控fixture，不构成内存实现耐久性验收。

两份新来源仍为元数据/治理fixture，部署目录由测试操作员发布；它们通过真实软删除事务产生Job，但不代表新的生产上传至发布流程已验收。既有真实七目标发布/撤回/删除链及其身份/读取回归同时保留。

本轮重跑还遇到既有发布任务续租、审批及HTTP上传超时测试的间歇失败，分别保留[续租失败](evidence/cleanup-registry-renewal-fixture-failed.json)、[审批失败](evidence/cleanup-registry-review-fixture-failed.json)和[本地失败](evidence/cleanup-registry-local-failed.json)。续租和审批探针新增不含正文的错误/剩余租约/时间与字段一致性诊断，没有增加租约或放宽规则；上传超时测试单独10次通过，但没有据此认定根因已修复。当前一次完整通过不等于稳定性达标，这些问题仍开放。

见[实库证据](evidence/cleanup-registry-postgres16-review.json)、[装配独立复核](evidence/cleanup-registry-runtime-review.json)、[本地检查](evidence/cleanup-registry-local-checks.json)、[worker身份](evidence/cleanup-registry-worker-review.json)、[队列](evidence/cleanup-registry-queue-review.json)、[消费者](evidence/cleanup-registry-cleanup-runtime-review.json)、[任务](evidence/cleanup-registry-job-status-review.json)、[删除](evidence/cleanup-registry-deletion-review.json)、[历史](evidence/cleanup-registry-history-read-review.json)、[当前读取](evidence/cleanup-registry-current-read-review.json)及[资源清理](evidence/cleanup-registry-cleanup-review.json)。

生产worker/服务Bootstrap、部署目录写权限/审计/制品验证、真实数据集与Endpoint证明、远程Grant及发送/写入屏障、其他后端、对账/重试/取消、并发/崩溃恢复、Purge、完整TaskRepository、迁移/回退及效果/性能和完整V1仍未验收。
