# 可替换清理执行器与单项消费者

实现：[worker.go](../../pkg/cleanup/worker.go)、[标准执行端口](../../pkg/contracts/cleanup_execution.go)、[当前租约核验](../../pkg/postgres/cleanup_verify.go)。业务编排只依赖contracts/canonical，禁止SQL、HTTP和供应商SDK。当前实现RunOne，不是持续后台循环或完整事件投递系统。

## 固定操作身份

清理操作使用固定Plan/Target、space/KB/Document、旧Revision/GenerationSet/publication epoch及目标kind/实例/BindingReference/ProfileDigest、代际/渠道构造。旧epoch编码十进制字符串，AKOS-CJSON/1后计算SHA-256(format + LF + bytes)。局部格式为akos.cleanup.effect/1，OperationID为cleanup-加64位小写摘要。

WorkerID、Attempt、FencingToken、RowVersion、ExpiresAt不参与外部效果身份，重领不会变成另一条删除命令。输入按目标类型检查，index必须有代际/BackendEpoch/channel，graph/memory/cache不混入索引字段。该效果摘要还是进程内草案，尚无独立跨语言金标及远程/迁移协议冻结证据。

## 执行顺序与必须接入的端口

Claim提交 → 独立VerifyLease → 按固定模块/配置Resolve → Lookup原固定操作 → 独立核验回执。已有成功结果直接完成任务，不再Execute；pending/unknown或无法核验的结果进入unknown，不自动重做。

只有Lookup已核验为absent，且AuthorizeExecution确认当前权限与已验证的稳定幂等能力，才可Execute。执行前再次检查租约；完成后核验真实回执、重新检查租约，再Finish成功。各插件接收独立操作/租约拷贝，不能修改编排的固定来源。预算绑定当前租约，提供者必须遵守context取消。

| 端口 | 必须承担的责任 |
|---|---|
| Tasks | 已提交计划、持久租约、token/version及结果幂等 |
| VerifyLease | 当前任务所有权与固定来源/目标；不能只比较调用方回显 |
| Resolve | 可信注册中心内的固定模块实例、旧配置与BindingReference，不按客户端挑选执行器 |
| AuthorizeExecution | 当前执行权限、适配器真实幂等协议和配置准入；不能只相信能力声明 |
| Lookup / Execute | 查询及执行同一固定操作，稳定幂等处理外部未知结果 |
| VerifyReceipt | 独立读取/核验实际后端事实；digest匹配仅是请求关系检查 |

PostgreSQL VerifyLease通过权威计划核对文档/版本/旧epoch/完整Target，并查询当前worker/fence/attempt/version/lease_until/running状态。过期令牌和改写目标均拒绝；它没有验证外部模块身份或删除效果，这两项仍由独立端口承担。

## 失败与证据范围

收到伪造回执、Lookup失败、执行未知或pending时，尝试写unknown。若期限或权限失效，Finish可能失败，保留running供下一所有者查原操作结果；不能因写回失败假定外部效果未发生。数据仓储fence不能自动阻止不支持fencing的外部引擎调用，必须有经过验证的稳定幂等/来源隔离协议。

4项Go测试覆盖成功重放零重复Execute、相同digest但无实际效果的伪造成功拒绝、执行前撤销阻止调用、效果身份不受重领影响、模块修改拷贝不影响原来源、pending不重做及缺失核验端口拒绝。

PG探针使用真实任务存储、权威租约核验和RunOne编排；外部执行器/准入/实际效果核验仍是明确的fixture。新增过期/篡改目标核验拒绝及真实数据库完成结果检查。未证明真实索引/图谱/记忆删除，不能把这条探针当作成熟组件接入或生产消费者验收。

下一步仍需真实执行适配器和回执查询、配置/模块身份验证、外部幂等公共测试、outbox传递/队列Ack、续租、unknown恢复、持续循环与关闭、崩溃及导出迁移/回退。整体WTX和生产验收保持开放。


本轮实际证据：PostgreSQL15.17/18.0均通过真实存储/租约核验/单项消费者探针（执行器仍fixture），自身测试容器清理。全量Go测试/vet、223项Python及三份OpenAPI检查通过，当前源文件/SQL哈希与归档一致。生产外部效果及完整WTX仍未验收。


已增加[本地索引实际适配器](CLEANUP_NATIVE_INDEX.md)：调用现有Delete并独立读取Owner/删除标记/记录数，PG链路已包含实际内存索引。外部成熟引擎、持久化防复活和真实权限仍待完成。
