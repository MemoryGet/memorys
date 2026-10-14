# 撤回存储约束原型与真实 PostgreSQL 验证

本交付验证[撤回事务设计](WITHDRAW_TRANSACTION.md)的部分数据库关系。SQL位于[约束原型](sql/withdraw_contract_v1.sql)和[数据库试验](sql/withdraw_contract_test.sql)，使用独立akos_withdraw_v1 schema和合成数据，不是生产迁移。

## 1. 数据库责任

| 数据 | 约束 |
|---|---|
| Document/Revision/GenerationSet/IndexGeneration | 空间/KB/文档/版本复合外键、渠道唯一、当前版本与代际成对 |
| command_operations | 空间/主体语义/动作/幂等键唯一；命令摘要、actor和动作绑定历史结果 |
| publication_history | epoch递增及uint64边界、动作关系、同文档epoch唯一、操作/结果双向关联 |
| cleanup_plans/targets | 固定撤回前版本/epoch、空间/历史关系、固定实例/配置/目标定义、代际唯一 |
| withdraw_outbox | 同计划/历史/操作及新聚合版本，每计划一个撤回事件 |

提交时约束触发器检查渠道覆盖、计划时间和withdraw必有plan/outbox。普通UPDATE/DELETE不得修改不可变记录。binding definition只检查JSON对象，具体Schema/Go读取、生产角色/RLS、审计/迁移日志、租约/fencing和数据集身份仍未实现。

PostgreSQL的CHECK得到NULL可能视为满足；有条件必需项用明确非空条件，跨行/表用外键或触发器，不能用查询别的表的CHECK。[官方约束说明](https://www.postgresql.org/docs/18/ddl-constraints.html)

uint64用未限制scale的numeric域并检查非负/整数/最大值，避免numeric(20,0)先舍入小数。最大值文本试验证明无精度损失。[官方数值类型说明](https://www.postgresql.org/docs/18/datatype-numeric.html)

时间以固定九位小数UTC文本保留纳秒，另校验格式和可解析性。Go SQL适配必须统一转换；当前Go、SQL私有编码及外部RFC3339映射尚需完整测试。

## 2. 实际运行证据

PostgreSQL 15.17兼容性试验已执行，见[证据](evidence/postgres-withdraw-15-review.json)：uint64最大值及非法数值、outbox前故障完整回滚、正常撤回提交、过期epoch、历史/计划/定义不可变、空渠道/跨空间/重复目标拒绝、时间纳秒保留，以及缺少outbox/目标的提交时拒绝并回滚。

跨空间负向试验起初先触发重复代际约束，不能证明目标外键生效；换用不冲突的cache目标后重新执行通过。不能只凭报错判定期望约束通过。

这是SQL合成工作流，没有Go PostgreSQL UoW、真实授权或API闭环，未覆盖全部审计/日志/租约故障点。WTX-01至WTX-06保持开放。18版本须单独记录，不能由15.17结果替代。

## 3. 可复现与隔离

[运行器](check_postgres_withdraw.py)要求已有本地Docker引擎/镜像，不自动拉取：

```powershell
python docs/architecture/check_postgres_withdraw.py --image postgres:15-alpine
python docs/architecture/check_postgres_withdraw.py --image postgres:18.0-alpine
```

运行器核对本地socket，创建唯一标签、无网络/宿主端口的临时容器，数据目录临时内存挂载；记录镜像ID、服务器版本、SQL SHA与输出。psql开启ON_ERROR_STOP，完成标记/SHA不变均必需，失败不记PASS。最终只清理自身标签匹配的容器及匿名卷，不清理其他容器、镜像或用户数据。

普通统一回归纳入.sql输入SHA，但不自动启动Docker或声称执行过SQL。数据库验证由独立运行器证明；镜像ID也不代表供应链签名或生产部署。

## 4. 后续实施

补租户/KB/身份、当前授权、审计/日志、目标定义、RLS和迁移模型，再实现Go仓储load/append与Document CAS并执行全部WTX试验。fixture_withdraw只是合成试验，不是业务服务，其固定actor/文档值不得用于生产。

Go历史时间检查已同步限定年份1..9999，拒绝0000和五位年份，避免与RFC3339/数据库编码范围产生差异。


运行器已增加失败清理：docker run创建成功但启动失败时仍按预分配名称定位并核对所有权标签，避免只依赖成功返回的容器ID而泄漏资源。3项Python替身测试覆盖远程socket拒绝、启动失败清理、错误标签拒绝；15.17真实路径再次执行通过并清理容器。18.0-alpine拉取会话94179仍运行，未因等待超时重复启动。


## 5. PostgreSQL 18与Go仓储适配片段

18.0-alpine下载会话94179已实际结束成功，不再等待或重启。当前15.17和18.0均通过SQL与Go探针，分别见[15 Go证据](evidence/postgres-go-repositories-15-review.json)和[18 Go证据](evidence/postgres-go-repositories-18-review.json)。pgx固定v5.11.0，版本与[官方发布](https://github.com/jackc/pgx/releases/tag/v5.11.0)核对；默认代理失败后仅对命令临时使用备用代理，未改全局Go设置。

[pkg/postgres](../../pkg/postgres/session.go)通过database/sql适配History/Plan的load/append，数字用文本/uint64转换、时间保留纳秒，Actor独立私有JSON映射。驱动只由[离线探针](../../cmd/architecture-pg-repositories/main.go)注册，不进入contracts。WithinPublication仅提供这两个仓储片段，明确不是完整UnitOfWork，不注册metadata_uow能力。

事务Scope冻结且限定单KB，要求有限父预算和可信VerifyScope；每次仓储操作/提交前重新验证，参数Scope不能重绑或扩大期限；内部锁串行操作，callback返回后关闭句柄并等待进行中的操作。事务失败回滚，提交异常返回需要对账状态，不能假定未提交。取消要求驱动/提供者合作；验证源仍是测试替身，不提供生产权限/RLS证明。

真实Go探针通过：历史/清理计划读取、uint64/纳秒/Actor映射、跨Scope拒绝、已退出事务句柄拒绝、实际History INSERT后callback故障回滚、授权拒绝零业务callback。另有3项Go单元测试覆盖作用域/期限/关闭、verifier panic释放锁和委托映射。

```powershell
python docs/architecture/check_postgres_withdraw.py --image postgres:15-alpine --go-repositories
python docs/architecture/check_postgres_withdraw.py --image postgres:18.0-alpine --go-repositories
```

运行器交叉编译Linux探针，在自身无网络/无宿主端口容器内运行，保存探针SHA及结果并清理自身容器/临时二进制。元数据、幂等、审计、outbox/迁移日志仓储、完整UoW、生产Auth、全部WTX故障/并发/迁移测试及服务bootstrap仍未实现，不能把本片段拼成已通过的撤回业务服务。


## 6. 验证后取消与提交前权限变化

仓储进入SQL前，VerifyScope返回nil也必须重新检查操作及父事务context，避免验证期间取消被忽略。新增Go单元测试分别取消操作/父context，确认仓储拒绝。

真实Go探针新增commit_scope_revocation_rollback：History INSERT实际成功，最终VerifyScope拒绝时WithinPublication返回错误，数据库中对应History不存在。15.17/18.0均再次执行通过，验证容器已清理。权限变化由测试verifier注入，不是实际IAM/RLS；不提供跨服务即时撤销线性化。


## 7. 清理计划与多目标写入回滚

Go探针新增[plan_probe](../../cmd/architecture-pg-repositories/plan_probe.go)：在隔离fixture内用真实History.Append/Plan.Append写入历史、计划头和index/cache两个目标，并在同事务Load读取；callback主动失败后检查三类记录均不存在。另将第二个目标改为未知BindingReference，实际外键失败后确认计划头/第一个目标也不残留。

15.17/18.0两版均通过plan_targets_append_roundtrip与partial_target_failure_rollback并清理容器。种子命令/文档/定义由合成SQL初始化，不是权威业务状态转换或真实授权。此测试证明当前仓储片段的写入/回滚路径，未完成元数据CAS、审计/outbox/日志或全部WTX门槛。


## 8. 发布头 CAS 与 outbox 原子提交

新增 [WithinWithdrawal](../../pkg/postgres/withdrawal.go)，在同一 Serializable 事务内复用 History/Plan 仓储，并以预期行版本、旧 publication epoch 和有效 revision/generation 对执行 CAS。成功后写 publication.withdrawn outbox 并完成对应 command_operation；任一步失败都禁止提交，包括 callback 忽略 Complete 错误。失败事务不再接受后续仓储调用。

真实 Go 探针覆盖成功提交、全部写入后 callback 故障、旧行版本冲突、CAS 后 outbox 约束错误、第二清理目标外键错误。验证发布头、历史、计划、目标、事件与命令状态共同提交或回滚。未返回缺少字段的伪 Document；接口保持为私有适配片段。

仍未完成完整 UnitOfWork、权威命令预留/重放、审计、真实身份权限、清理消费者及生产迁移。fixture 的 pending command 由测试 SQL 预置；本结果不代表完整撤回业务服务通过 WTX 验收。


本轮 PostgreSQL 15.17/18.0 真实 SQL+Go 探针均通过，临时容器已清理；全量 Go 测试、vet、222项 Python 与三份 OpenAPI 结构检查通过。最新证据已更新到 evidence 目录，生产验收标记保持 false。


## 9. 命令预留与历史重放

[commands.go](../../pkg/postgres/commands.go) 为 WithinWithdrawal 提供 Reserve：以 space、主体/委托语义摘要、withdraw 动作及幂等键作为唯一范围；预留、历史、发布头和事件共用一个事务。新记录返回 Created，callback 失败时预留也回滚。

同键既有记录核对 KB、Document、actor 和命令摘要。不同内容返回 ErrCommandConflict；pending 返回 ErrCommandPending，不接管、不重做。committed 从原 PublicationHistory 返回 OperationID/HistoryID/PublicationEpoch，重新执行当前 Scope 验证，不采用当前文档头推算结果。SQL 序列化冲突和提交未知结果仍需应用层对账，未实现自动重试。

实库探针在正常撤回后用合成 SQL 将文档头推进，再确认重试仍返回原 epoch=2，且历史、计划、事件不重复；同时验证变更摘要冲突和 pending 拒绝。该推进只是验证重放来源，不证明重新发布业务已实现。

ActorSemanticDigest/CommandDigest 必须来自可信应用服务；规范化编码、域隔离金标、委托身份来源以及真实授权仍未冻结。该私有适配接口不能作为未经验证的客户端输入 API，也未注册为完整 IdempotencyRepository 或 UnitOfWork。


Complete 还要求该 OperationID 在当前事务中由 Reserve 新建，并核对 Document/CommandDigest；既有 pending 或重放结果不能绕过预留直接推进发布头。


命令预留/重放本轮证据：PostgreSQL 15.17/18.0 SQL+Go 均通过，测试容器清理；全量 Go 测试/vet、222项 Python、三份 OpenAPI 检查通过，当前输入哈希与归档报告一致。总体及 WTX 门槛仍未通过。


## 10. 撤回审计与原子性

SQL 原型增加 withdraw_audit，绑定 space/KB/Document、History/Operation、可信 RequestID、动作、Actor（含委托）和原历史发生时间。同历史/操作唯一，数据库校验主体与完整 Actor JSON、时间均与不可变撤回历史一致，并禁止应用 UPDATE/DELETE。历史的延迟约束要求审计存在，不能跳过 Go 方法而提交无审计的撤回。

WithinWithdrawal.Complete 改为接收私有 WithdrawalCompletion（PlanID、预期行版本、EventID、AuditID、RequestID），在 CAS 和 outbox 后插入审计，再完成命令记录。审计内容由已加载历史生成，未接受客户端填写的 Actor 或时间。审计插入失败会使事务不可提交；callback 忽略错误也不能保留部分撤回。

SQL 探针覆盖漏写审计、主体不匹配及修改/删除拒绝。Go 实库探针在 CAS/outbox 后通过非法审计 ID 触发真实约束错误，检查文档头、历史、清理计划/目标、命令预留、事件和审计全回滚；成功及幂等重放均只产生一份审计。

这是撤回专用审计适配片段，不代表完整 AuditRepository、生产审计查询/保留规则、数据库角色/RLS或防管理员篡改能力已实现。RequestID/Principal 必须由可信应用层提供；当前探针仍使用测试身份。迁移日志、完整 UoW、真实授权、应用服务与清理消费者仍待完成，WTX 总体验收不关闭。


本轮审计证据：PostgreSQL 15.17/18.0 SQL+Go 均通过并清理自身测试容器；全量 Go 测试/vet、222项 Python 与三份 OpenAPI 检查通过，当前 SQL/源文件哈希与归档一致。事务替换边界已补到 DEPENDENCY_BOUNDARIES 第4节。总体仍有条件通过。


## 11. 标准事务端口与撤回应用编排

[withdrawal_transaction.go](../../pkg/contracts/withdrawal_transaction.go) 将命令预留、发布头/清理目标快照及完成操作定义为平台进程内契约。PostgreSQL 旧导出类型改为别名，WithdrawalAdapter 实现 WithdrawalUnit；供应商数据库/驱动类型未进入 contracts。[service.go](../../pkg/withdrawal/service.go) 仅依赖 contracts，禁止数据库/HTTP/进程依赖，并使用标准 WithdrawRequest/WithdrawResult 编排撤回。

流程为：冻结请求与预算 → 可信 Prepare → 事务 Reserve → 原结果重放，或 LoadCurrent → 比对 ExpectedEpoch → 校验并写历史/计划 → Complete → 提交后返回结果。事务失败/提交未知不返回构造中的成功结果。Prepare 接收独立拷贝，不能修改编排持有的 Scope 或委托；3项单元测试覆盖准备拒绝、当前epoch不符、提交失败及拷贝隔离。

LoadCurrent 在事务内按 space/KB/Document 锁定未删除的当前发布头，读取有效 revision/set/epoch/行版本；索引定义必须对应每个实际代际，缺少或重复匹配拒绝。非索引目标只来自 publication_cleanup_dependencies 登记的同文档、版本、代际及旧epoch依赖，不扫描某个模块的全部资源。快照为窄投影，没有编造完整 Document 的标题/创建者。SQL 原型新增 deleted 标记供读取/CAS拒绝已删除文档；删除业务本身尚未实现。

实库探针现在通过 Service + WithdrawalAdapter 完成正常撤回及历史重放；准备器和身份仍为 fixture。首次实库执行发现目标 UNION 查询排序列重名，已明确 binding_reference 别名并复验。

此流程仅实现 Withdraw 切片，不假装实现尚未建设的 Submit/Approve/Publish，不注册完整 PublicationService 或 metadata_uow。Prepare 的真实身份/委托验证、当前文档权限、命令规范化与域隔离金标、ID分配，以及迁移日志、来源登记写入协议/迟到任务fencing、全局清理覆盖与消费者仍待实现。契约为进程内草案，远程/MCP及迁移协议还未冻结，WTX门槛保持开放。


本轮应用切片证据：PostgreSQL 15.17/18.0 SQL+Go 实际应用提交及历史重放均通过；自身容器清理。全量Go测试/vet（含3项新服务单元测试）、222项Python及三份OpenAPI检查通过，当前源文件/SQL哈希与归档一致。


## 12. 实际命令摘要准备器

已实现[命令语义与准备器](WITHDRAW_COMMAND_SEMANTICS.md)，PG应用探针不再使用固定b/c摘要，而是运行Preparer的真实规范化/哈希代码；ID/时间及权限来源仍为fixture。Go/Python金标和临时字段/身份变化、拒绝/取消与ID生成测试通过。生产身份/文档授权、格式导出迁移及完整WTX仍待验。


本轮准备器证据：15.17/18.0 SQL+Go 均通过，实际准备代码参与应用提交/重放；自身容器已清理。全量Go测试/vet、223项Python与三份OpenAPI检查通过，361项当前源文件哈希与归档一致。生产验收保持未通过。


## 13. 来源登记与计划封存约束

SQL 原型补来源登记触发器：锁定同 space/KB/Document 的当前头，只接受未删除且 active revision/set/publication epoch 完全匹配的来源。撤回后旧epoch登记拒绝；已登记记录禁止修改/删除。Go fixture 已调整为先准备有效发布头，再登记来源。

计划提交时除索引数量检查，还双向核对该旧发布epoch的 graph/memory/cache 登记：漏掉已登记目标或添加未登记目标都拒绝提交。索引的不可变定义中 generation_id 必须与实际目标代际相同。清理目标插入会锁定所属操作记录，只允许 pending 操作；committed 计划禁止追加目标，配合已有更新/删除禁令封存定义。

SQL 测试增加遗漏依赖、额外依赖、错误索引定义、提交后追加目标、撤回后登记及登记修改拒绝。原 duplicate 目标测试改为检查封存拒绝；唯一索引约束仍存在。首次普通Go连接暴露触发器未限定schema的ROWTYPE类型，已限定为akos_withdraw_v1.documents后复验。

这些约束覆盖单库权威记录边界。实际图谱/记忆/缓存生产者仍需先登记来源并遵循任务fencing/当前授权协议；外部写入、消费者并发、崩溃恢复、角色/RLS与迁移尚未验，不能宣称已解决所有迟到任务或关闭WTX-05/06。


本轮来源/封存约束证据：PostgreSQL15.17/18.0 SQL+Go均通过并清理测试容器，全量Go测试/vet、223项Python和三份OpenAPI检查通过，当前源文件/SQL哈希与归档一致。历史事实恢复不能直接沿用当前来源登记路径：受控导入/暂存验证协议尚未实现，不能通过关闭触发器宣称迁移已验收。


## 14. 持久化清理任务领取

[清理任务设计/实现](CLEANUP_TASKS.md)：目标同事务初始化进度，数据库租约、attempt/fence/version与终态幂等写回；真实外部执行、回执核验、投递、unknown恢复和迁移尚待完成。
