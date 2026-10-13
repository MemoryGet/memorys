# 迁移日志、提交水位与原子重放 V1

本规范细化[一致性协议](CONSISTENCY.md)的JOURNAL-01/02和[导出契约](EXPORT_CONTRACT.md)，是D-04的部分交付。[完整事务日志包Schema](schemas/journal-bundle.v1.schema.json)与离线链校验已提供；[KB迁移状态机器契约](MIGRATION_STATE.md)也已提供局部规则。源数据库提交屏障、目标原子重放、数据库/文件迁移及完整记录payload仍待实现验证。

## 1. 提交顺序与快照屏障

V1内置元数据实现选用**每个部署一个权威JournalHead，写事务先锁头再写业务**。这是首版用于正确性验证的实现选择，非数据库端口必须使用的厂商类型；替代实现可以用经验证的提交日志协议提供等价语义。该串行写入点的吞吐和事务时长必须实测，超出规模时先设计新的分区/屏障协议，不静默用预分配序号替换。

事务在同一UnitOfWork内锁定JournalHead，读取前驱游标，校验领域/CAS/授权条件，写完整权威变更、幂等/审计/outbox及不可变载荷引用，追加一个完整JournalTransaction并更新Head。锁持续到提交或回滚；下一写事务取得锁时只能读到前一事务已提交的头或其回滚前状态。业务写入、日志与Head必须全部原子提交。不可在事务外先更新头，也不可在日志后遗漏审批、撤回或权限变更。

一个部署的所有权威写路径遵守同一锁顺序，不能有后台任务或“紧急路径”绕行。外部字节先写临时不可变载荷并验证持久成功，再在事务中绑定；失败留下孤立载荷，由保留锁/GC协议处理。网络IO不进入持锁事务，死锁/取消/数据库错误回滚全部内容。

全量数据库迁移暂停全部写入者，等待已持锁事务终态，再在一致快照中读取Head及所有权威记录。KB索引迁移只暂停对应内容/索引写入，但授权/撤回仍按完整事务记入同一全局流。KB重放读取连续完整全局事务范围：可不修改与KB无关的记录，但必须理解并核验整笔事务；有跨空间授权或主体变化时不得仅按kb_id粗略过滤。

不把数据库sequence、开始时间、transaction_id、墙钟或outbox消费位置当提交水位。源实现必须通过两事务按与ID分配相反顺序提交、回滚、超时与进程中断的实测，证明一致快照不会漏掉任何已提交变更。

## 2. 水位与日志包编码

`journal_stream_id`标识不可变源流；流重建、分叉恢复或从旧备份独立启动必须登记新流，不能沿用旧ID伪造连续历史。游标格式为`j1:<64位小写SHA-256>`，外部只比较相等和前驱关联，不排序。Genesis由流注册表在初始化事务中冻结，独立保存到迁移/快照记录，不能由导出包自报替代。

一笔事务包含transaction_id、journal_stream_id、previous_cursor、cursor、mutation_count和有序mutations。每个mutation含0起连续ordinal、operation_id、record_type、record_id、previous_version（新建为null）、new_version、effect与不可变payload引用。一个资源在一笔事务中只记录最终权威状态；涉及多个资源时全部列出，不能按分页切开事务。migration_journal自身不递归作为mutation再写一份。

变更效果为upsert或tombstone。权限撤销一般是grant/policy的新状态，撤回/删除必须保留带版本的墓碑与原因/归属/禁止访问事实，不能只给一个后端delete ID。旧版本必须与目标事实一致，新版本严格增加；同一资源被删除后重新创建不能清零版本或复用已回收ID。各资源的版本映射仍需在typed payload契约中确定，不能以供应商row ID代替。

```text
core = 事务对象移除 cursor 字段（其他字段全部保留）
cursor = "j1:" + SHA256(UTF8("akos.journal/1") + 0x00 + AKOS-CJSON/1(core))
```

规范字节沿用[冻结编码规范](FROZEN_CONTENT.md)，保留精确整数。payload引用摘要是原始字节摘要，包内逻辑entry_id不能用于自动下载或拼接文件路径。payload包络恰好包含record_type、record_id、effect和对象payload；完整权威字段、附件、空间归属、旧/新状态及content_hash仍须typed校验。哈希链证明相互关联，不证明内容正确或来源可信。

日志包标记from_cursor（不含该事务）和through_cursor（含该事务），并声明固定格式akos.journal/1。验证者从独立快照/迁移记录取得from和目标已提交Head，检查流、边界、每个前驱、整事务计数、ordinal、游标摘要、版本衔接和payload字节。空范围只有两边相同且没有多余载荷才有效。包自己将through_cursor改短不能通过独立边界检查。

## 3. 目标重放与切换

每次重放一个完整源事务：在目标UnitOfWork锁定该迁移的checkpoint，要求当前位置等于source previous_cursor；核验初始版本与类型化payload，更新全部目标权威事实，并写`migration:<stream_id> + source_cursor`消费receipt、完整事务摘要与新checkpoint，一起提交。receipt和checkpoint属于目标权威迁移事实，须纳入consumer_receipt/migration_operation导出库存。

同一source_cursor再次交付只有receipt摘要相同且已提交才是重复成功；摘要不同拒绝。不能对半笔事务或部分通道更新checkpoint。目标本地审计/日志使用其自身流，不冒充源提交头；源和目标不是双向复制，禁止把目标重放产生的本地日志重新混入这次源范围。

原件/索引准备在目标事务外使用独立attempt，权威checkpoint只有必需产物已验证且整个源事务可应用时才推进。外部写入成功而目标事务失败，保留候选、幂等重试；权限与撤回访问闸门继续使用当前权威状态，不因为索引残留恢复可见性。

切换前重新暂停相应业务写入，取得真实提交Head，重放至该Head并对照有效发布集合、当前权限、墓碑和附件。切换事务同时记录新epoch/配置绑定/指针及cutover水位。尚有在途提交、through范围未覆盖、目标字段不支持、来源失配或检查故障时阻止切换，保持旧实现或维护状态。

恢复写入后回退也先暂停，再取得新实现的Head，原子重放新增/更新/删除/撤回/权限等全部事实至旧实现，验证后切换。当前简化单头方案下journal_stream不随后端epoch替换而更改；元数据库替换则需明确定义流继续或桥接，新流不能直接拿旧游标比较。该桥接/迁移状态机器契约尚未冻结。

## 4. 离线证据边界

```powershell
python docs/architecture/build_journal_contract.py
python -m unittest discover -s docs/architecture -p test_journal_contract.py
```

参考工具限制1000事务、每事务10000项mutation、10000个payload、每个4MiB/总128MiB，沿用64层JSON限制；生产流式处理另设计容量。expected边界必须来自可信调用方独立记录，校验器无法凭一个dict证明其来源。成功固定`ready_for_replay=false`，列出真实提交顺序、typed载荷、初始版本、原子receipt、授权/外部产物/切换等未验证项。

专项试验是合成链：证明任意ID顺序不影响前驱校验，漏段/乱序/截断、跨流/重复事务、部分事务/ordinal、版本冲突、缺失/篡改/多余载荷及墓碑目标失配会被拒绝。它不模拟真实事务调度，不证明数据库JOURNAL-02已通过。D-04继续保持开放。
