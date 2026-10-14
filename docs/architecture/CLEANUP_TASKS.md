# 清理任务持久化与领取边界

实现：[标准清理任务端口](../../pkg/contracts/cleanup_tasks.go)、[PostgreSQL适配](../../pkg/postgres/cleanup_tasks.go)。这是清理专用 Claim/Finish 切片，不是完整 TaskRepository、TaskQueue 或后台消费者。

## 数据与事务

每个不可变 CleanupTarget 插入时，同事务创建 cleanup_progress。目标/计划写入失败时进度记录也回滚；进度只记录运行事实，不修改来源或固定清理定义。主键为 PlanID+TargetID，外键绑定实际目标。

状态为 pending、running、succeeded、failed、unknown。Attempt、FencingToken与RowVersion使用uint64无损存储，递增前检查溢出。只有已提交撤回计划可领取，租约包含固定旧Revision/GenerationSet/PublicationEpoch、目标与绑定引用；不会用当前发布头推算清理范围。

## 领取与完成

Claim要求publication.cleanup.execute业务Scope，并将WorkerID绑定到经验证的SubjectID。数据库原子更新pending或过期running任务；正常活动租约不能接管。每次领取递增attempt/fence/version，租期为整毫秒1..30000ms，并限制在Scope到期前。数据库服务器时钟决定到期，租约时间精度为微秒；机器时钟偏差监测仍需生产部署验证。

Finish重新验证当前Scope，核对空间/KB、worker、fence、行版本、租约到期时间和值；过期或被接管的旧令牌拒绝。终态记录保持worker与token，精确相同结果可在新连接中幂等重放；不同回执不能覆盖终态，成功任务不能再次领取。failed/unknown不会自动领取，需另外的恢复/对账协议。

Claim提交异常不返回可执行租约；未知提交结果必须查询/对账。Finish当前信任内部消费者提供的Outcome/ResultReference，本身不证明外部删除成功，不能把该端口暴露成允许Agent任意报告成功的工具。

## 外部效果的边界

FencingToken目前只阻止旧消费者写回本数据库进度，不代表外部检索/图谱/记忆引擎已经支持fencing。过期running的Attempt>1可能已有未知外部效果，新消费者必须先查询原固定操作结果，再按适配器已验证的幂等协议执行或转为unknown；不能把重新领取等同于允许盲目重做。

外部幂等操作标识必须由固定PlanID+TargetID及来源/配置派生，不能随Attempt、WorkerID或新Scope改变。消费者还需重新加载固定定义、核对模块实例与配置代际，验证真实回执。当前外部执行器、回执核验、unknown对账、续租/取消/显式重试、outbox投递和消费者循环仍未实现。

## 已有验证与替换验收

实库探针覆盖活动领取排他、模拟到期后令牌递增、旧消费者完成拒绝、适配器/连接重建后的完成重放、终态回执覆盖拒绝、unknown不重做，以及两个并发事务仅一个领取成功。模拟到期由隔离fixture调整数据库时间字段，不是业务API；没有证明进程强杀、数据库重启或真实外部效果恢复。

换用其他任务实现时，需共同验证固定目标、当前授权、领取排他、递增令牌、结果幂等、未知结果处理、导出迁移/回退与生产故障恢复。当前没有第二种任务存储或迁移证据，不能注册完整任务/队列能力，也不关闭WTX-05/06。


本轮实际证据：15.17/18.0 SQL+Go均通过，含双消费者并发领取；测试容器清理。全量Go测试/vet、223项Python和三份OpenAPI检查通过，当前SQL/源文件哈希与归档一致。生产、外部副作用、崩溃及迁移验收保持未通过。


已补[单项消费者编排](CLEANUP_WORKER.md)及PostgreSQL当前租约核验；外部执行器、准入和效果核验仍为fixture，持续循环/outbox/真实后端及恢复验收保持开放。
