# Outbox 事件消费运行时

`pkg/events`提供与具体消息系统无关的有界消费协调器。它通过现有`contracts.EventBus`读取一页事件，先校验事件身份和版本，再调用幂等处理器；处理器成功后才写入`ConsumerReceiptRepository`。任何处理或回执失败都会保留原游标，下一轮会安全重放未确认事件。

取消和显式重试清理使用版本化`EventEnvelope`（`document.delete.cleanup.cancelled`或`document.delete.cleanup.retry_requested`），PG outbox负责事务内落盘，消费者负责异步投递。重复事件由消费者回执去重，不能把回执当作任务租约或业务状态写屏障。

`events.Runner`在此消费者之上提供可停止的周期调度、每轮截止时间和可替换的`CursorStore`。只有游标保存成功后才推进本地位置；宿主Bootstrap负责选择持久化实现、失败告警和具体事件处理器装配，也负责数据库或传输客户端生命周期。

游标持久化失败与总线/处理器失败使用同一`OnError`告警边界；本地游标保持在未提交位置，下一轮继续重放。持久化重试等待受Runner上下文约束，停止不会被整段退避时间阻塞。告警钩子仍通过panic隔离，不得改变提交或重放语义。

PG适配器`postgres.EventCursorStore`将游标存入独立的`event_consumer_cursors`表，缺失游标从空位置开始，更新采用按consumer唯一键的单调upsert；旧实例试图覆盖较新游标时返回错误，不参与任务租约、fence或业务状态转换。

`postgres.DeletionControlEventBus`把删除控制outbox按PostgreSQL identity-backed `event_seq` keyset分页映射为`EventEnvelope`，不再按随机请求ID排序；游标编码为固定20位十进制序列，只在Runner保存成功后推进。这样并发提交的后写事件不会因为ID字典序变化而被跳过；升级时发现旧的非序列游标应清空并从最小序列重放。它只实现消费，禁止通过EventBus伪造发布或修改任务状态。

Runner的只读`Snapshot`提供当前游标、启动/停止状态和最近投递错误，供Bootstrap健康检查或告警采集；快照不会成为业务完成或租约有效性的依据。

`events.RunnerGroup`按稳定consumer名称管理多个Runner：确定性启动、启动失败回滚、逆序停止、全组Ready和只读快照。它允许删除、发布等outbox消费者共享进程生命周期，但每个Runner仍保留自己的游标、回执和幂等边界。
