# 删除清理队列发现与有界轮询

后续增量：[清理模块保留绑定与可信装配](CLEANUP_REGISTRY_RUNTIME.md)已接入当前封存目录和kernel命名模块，当前585项/326检查及本页回归通过。生产部署控制、制品/数据集证明、远程Grant和完整V1仍开放；本轮间歇失败记录保留。

后续增量：[真实service身份及独立清理/索引权限](WORKER_AUTHORITY_RUNTIME.md)已在PG16通过当前身份/ACL、目标准入与实际效果验证；当前579项/317检查及本页既有回归通过。原阶段fixture授权记录保留，不能把其全部视为新身份链路验收。生产worker装配、模块注册及远程Grant仍开放。

2026-10-18：在[逐目标删除消费者](DELETION_CLEANUP_RUNTIME.md)之上接入标准队列发现端口、PG实现以及可取消的轮询器。调用方不再需要预先提供删除任务ID。当前实现提供可嵌入宿主的运行循环；生产worker身份、正式服务装配、持久调度和显式对账仍未验收。

## 标准端口与范围

contracts.DeletionCleanupQueue.DiscoverDeletionCleanup返回实际Job绑定的JobID、ResourceID、空间、知识库和配置摘要；DeletionJobRunner复用每次至多执行一个目标的RunOne。发现器与执行器均可独立替换。本轮只证明PG发现实现与现有PG协调/目标消费者的组合，没有宣称第二种队列存储通过替换验收。

PG发现器要求当前publication.cleanup.execute作用域以及每个真实Job/Resource的独立准入回调。查询限定单空间、单KB，串行化事务开始及提交前再次校验作用域；读取任务时复核不可变定义、已提交删除命令、回执和已删除文档的关联。任务配置摘要来自真实定义，轮询器不使用调用者指定的配置代替实际任务配置。

可执行集合为queued，以及存在pending、到期租约、失败/未知目标需协调，或者所有目标均已结束需汇总的running任务。completed/failed不会重新发现；仅有有效running目标租约的任务暂时跳过。发现不会预先领取目标，竞争仍由既有逐目标Claim/CAS及fencing token解决。本轮尚无新增多消费者竞争、租约过期恢复或故障重启矩阵的完整实库验收。

## 分页与公平推进

内部keyset游标包含AfterJobID、UpperJobID和Binding。首次查询冻结当前可执行集合的最大ID，按PG C排序逐页扫描。每周期默认无隐含无限循环：显式PageSize限制每页1至100项，MaxPages限制每周期1至1000页，CycleBudget不超过30秒。每个发现项每次至多运行一个目标，后续周期保存位置，整轮结束才从头扫描。新任务若排序位于已扫位置之前或当前上界之后，会在后续轮次进入；其余新任务可能在本轮后续查询中进入，因此上界不等于完整集合快照。

Binding以规范JSON及akos.delete.queue-cursor/1域计算SHA-256，绑定主体、worker、动作、空间、KB、策略版本和页大小。ScopeID及续期时间不参与摘要，避免每次续期都回到首页；策略或其他稳定授权上下文变化则清空游标。该摘要是内部上下文绑定，既不是MAC签名，也不是公开令牌或权限证明，不能代替真实校验及资源准入。

轮询器在执行任何一项之前检查整页的排序、上界、归属、配置摘要和下一页游标；请求、作用域和页数据均复制，阻止提供方修改后续执行基准。已处理位置跨周期保留。已知Job返回failed及错误时记入统计并继续后续任务；无法取得可信Job结果的错误、发现错误或授权错误则停止。执行结果必须与发现项的Job、资源、空间和配置摘要一致。

## 生命周期与取消

NewDeletionPoller要求可信Scope提供方，每个周期重新取得并校验当前作用域；RunOnce同时受调用者截止、周期预算和作用域到期约束。单个轮询器串行保护内部游标，等待该保护期间也能响应取消；Run阻止同一实例同时启动两个持续循环。

Run属于调用者生命周期，不自行启动后台进程。每周期后通过可取消定时器等待PollInterval，空队列和已知任务失败也不会形成忙循环。发现或执行返回后重新检查取消，避免已取消的空查询被报告为成功。停止循环不代表取消已有删除Job，不改变任务失败状态，也不实现重试/对账。

Scope和各端口须遵守context；实现没有为不合作的提供方创建无法回收的后台调用。应用重启会丢失进程内游标并重新扫描，但已完成/失败任务仍由PG持久状态排除；跨进程公平性和恢复时限尚未验收。

## 运行证据与边界

574项源码输入在PG16通过309项原生检查。六组独立复核、Go全量/go vet、228项Python和三份OpenAPI全部通过，报告对应同一未变化源码清单；临时目录不存在且55432端口无监听，已另行核对。

PG16新增独立20秒fixture阶段，保持业务请求预算及逐目标4秒租约不变。独立KB的受控元数据/治理fixture通过真实软删除事务创建四个Job：两个无目标、一个可清理关键词索引、一个模块不可用的索引。负向任务先经真实租约和unknown路径进入failed。随后每周期一页一项，三个待处理任务自动完成；索引实际有删除fence且投影行归零，两个空任务实际协调完成。失败目标保持unknown、索引行仍在且无第二次尝试。

真实PG发现器拒绝旧策略游标、跨KB、过期及撤销作用域。轮询器更换策略后重新从首页发现，随后保持同策略游标继续；独立Python重新计算游标摘要并核对实际定义/配置、排序上界、周期统计、目标租约和物理效果。调用者取消终止Run，终态发现为空，其他KB已有任务运行记录完全一致。单元测试另外覆盖持续running任务的跨周期公平顺序、整页外来资源拒绝、提供方修改请求、已知/未知错误隔离、空轮询等待及取消后返回。

这些新增任务使用明确的元数据fixture，不代表重新验证了候选至发布链路；此前真实八epoch发布/撤回再软删除的七目标消费者和全部读取/删除回归在同一源码快照下重新通过。任务/index授权仍为独立fixture，查询状态原有真实认证/ACL链路保持回归。

首次实库尝试因新fixture的SQL参数域类型推断冲突（42P08）失败，保留[失败记录](evidence/deletion-queue-fixture-parameter-failed.json)；明确转为text后重跑通过，没有改变生产授权或放宽约束。此前其他租约间歇失败仍开放，不能据此宣称生产稳定性已达标。

见[实库证据](evidence/deletion-queue-postgres16-review.json)、[队列独立复核](evidence/deletion-queue-runtime-review.json)、[本地回归](evidence/deletion-queue-local-checks.json)、[既有消费者复核](evidence/deletion-queue-cleanup-runtime-review.json)、[任务状态](evidence/deletion-queue-job-status-review.json)、[删除](evidence/deletion-queue-deletion-review.json)、[历史读取](evidence/deletion-queue-history-read-review.json)、[当前读取](evidence/deletion-queue-current-read-review.json)及[资源清理](evidence/deletion-queue-cleanup-review.json)。

生产Bootstrap/worker IAM与模块装配、持久TaskQueue及完整TaskRepository、更多后端、[安全取消控制](DELETION_CONTROL_RUNTIME.md)的生产凭据/出站消费、显式重试/对账、执行审计、跨消费者并发/重启恢复、Purge、迁移与效果/性能及完整V1仍未验收。本轮仅PG16，旧PG15/18记录不作为当前证据。
