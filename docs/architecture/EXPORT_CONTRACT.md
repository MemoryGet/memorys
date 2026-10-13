# 元数据导出清单与导入边界 V1

本规范细化[数据模型](DATA_MODEL.md)及[一致性协议](CONSISTENCY.md)的全量元数据库迁移，补充D-04。[ExportManifest Schema](schemas/export-manifest.v1.schema.json)已提供，当前校验覆盖清单、段摘要和记录包络；各类记录payload的机器Schema、真实快照、事务水位与导入实现仍未完成。

## 1. 导出范围

首个清单类型固定为`metadata_full_snapshot`：暂停全部元数据写入，包括任务领取/完成、授权和紧急撤回；维护期间不可声称仍能完成这些写操作。所有后台写入者到达明确屏障后取得同一快照。KB索引迁移仍按原一致性协议允许授权/紧急撤回并进入日志，不能复用本清单类型假装它们已经暂停。

必须显式列出所有权威记录族，即使某类记录为0也提供空段。清单检查不能由“全部表已导出”一句自述代替；各仓储实现须声明其逻辑记录族，并与平台注册表对照。新增权威记录族需先升级清单契约，不能将它藏进未知扩展而让旧导入器忽略。

| 记录族 | 保留目的 |
|---|---|
| space / knowledge_base / principal / identity_binding | 平台ID、目录与企业身份关联 |
| membership / kb_grant / authorization_policy | 当前权限、策略版本及撤销事实 |
| document / draft / draft_block / draft_blob | 发布指针、草稿编辑内容及未提交附件关联 |
| revision / block / revision_blob | 冻结版本、标准内容、来源定位与附件清单 |
| blob / storage_binding | 稳定BlobRef及版本化物理映射；不包括原件字节本身 |
| approval_request / approval / publication_operation / publication_history | 审批请求和决定、内容哈希、并发发布与撤回历史 |
| backend_epoch / generation_set / index_generation / backend_binding / profile_snapshot | 活动后端代际、配置与引用映射；指针记录不证明外部产物仍可用 |
| job / job_attempt / outbox / consumer_receipt / idempotency_record | 任务所有权、结果和消息重复交付去重 |
| audit_record / migration_journal / migration_operation / tombstone | 审计、事务变更、迁移进度及删除/撤回墓碑 |
| feedback / draft_proposal / request_record | 反馈、建议及请求归属/引用追踪，支持恢复后检验反馈代办权限 |
| memory_record | 用户/Agent权威记忆事实及来源；记忆向量不作为权威数据 |
| retention_hold / purge_job / retention_policy_snapshot | 回收保护、删除未知结果及不可缩短的保留承诺 |
| invocation_record / immutable_payload / credential_reference | 远程调用结果未知记录、不可变任务/日志载荷、凭据引用及版本 |

图投影、向量、全文与缓存排除，须重建或独立验证。标准Block不能以可重建为由排除。原件、附件和不可变外置载荷字节须在另行冻结的文件复制包中清点校验；没有这些字节不能切换。凭据正文不进入普通包，恢复环境从受控凭据服务解析固定引用并验证身份；本地密码/认证凭据的安全迁移协议仍待定义，不能因有credential_reference就判定认证数据迁移完成。

## 2. 清单、分段与记录包络

清单记录source部署ID、已核对BuildLock摘要、元数据契约版本、snapshot ID、journal stream ID和提交水位。水位是源仓储在完整事务之后给出的不透明游标，不能按文本排序、转成分配序号或推断没有遗漏。迁移日志机器契约还需冻结游标比较和提交屏障证明。

每段记录`entry_id / resource_type / record_contract / encoding / record_count / byte_length / sha256`。entry_id是包内逻辑标识，不是文件路径或URL；运输层使用显式目录映射，导入器不能据此自行下载或解压任意路径。一个记录族允许多段，但entry_id全包唯一，`resource_type + record_id`跨段唯一。摘要覆盖完整原始字节，计数、类型和字节数必须吻合。

`jsonl-envelope/1`是UTF-8、每行一个JSON对象、LF终止，无空行或原始CR；字段恰好是`record_type`、`record_id`、`payload`。payload必须为对象，其完整字段、不变量与版本兼容应由对应`akos.metadata.<resource_type>/1`记录契约验证。**这些记录契约标识目前是设计分配，尚未全量实现payload Schema，不能据包络检查通过冻结全部数据导出格式。**重复JSON键、无效Unicode、非有限数值、错误类型或重复主键拒绝。类型化实现还须用精确整数/十进制解码，不允许以浮点读写改变fencing或版本号。

平台身份/权限、审批/版本、租约、幂等和迁移日志须来自同一一致快照，不能按表逐次读取不同时间的状态。每段payload应包括完整逻辑记录，不仅是某次通知事件或供应商内部ID。

## 3. 导入与切换闸门

1. 在隔离命名空间验证包来源信任、清单结构、字节摘要、完整库存及类型化记录。仅检查摘要只能证明字节一致，不能证明导出没有遗漏。
2. 校验唯一约束、外键、空间归属、内容冻结哈希、当前授权策略、审批与发布指针、代际集合、附件字节、任务结果/幂等关联和墓碑完整性。
3. 所有在途租约必须失效，保留递增fencing的权威历史，不能重置成0让旧Worker重新有效。已确认终态不得重复执行；远程结果未知先按调用账本恢复和探查，不能盲目重放。
4. 新外部物理位置/供应商ID由目标适配器产生新绑定，旧绑定保留为迁移历史，不能直接当成目标环境可访问位置。已有公开citation依旧按平台版本/位置解析。
5. 验证一致性屏障与实际提交水位、固定配置/epoch、原件及索引准备，完成并发/事务验收后原子切换；导入任一失败保持旧部署或维护状态。
6. 从较早备份恢复时补回后续撤回/删除/权限和新增写入；不能立即把旧授权状态对外服务。回退窗口继续持有原件和日志保留锁。

任何步骤失败不得将`ready_for_import`或服务就绪设为true。这里的闸门是尚待实现的生产约束。

## 4. 当前离线验证

执行：

```powershell
python docs/architecture/build_export_manifest.py
python -m unittest discover -s docs/architecture -p test_export_manifest.py
```

`export_validation.validate_package`接受显式entry-id到bytes映射，不读取路径、不下载、不导入。离线参考实现最多1000段，单段4MiB、总计128MiB，JSON嵌套64层；这些是设计检查工具限制，生产流式导出不能据此限制企业数据量。成功固定`ready_for_import=false`并列明未验证项。

测试覆盖空族明确列出、缺族/重复条目、缺失/额外字节、摘要/数量篡改、多段重复记录、契约/类型失配、JSON重复键/行格式、精确计数和恢复规则。空库存只是假设设计样本；它不证明某个运行部署真的没有数据。[日志水位](JOURNAL_PROTOCOL.md)及[原件复制清单](BLOB_COPY.md)已另提供局部机器契约；真实导出/导入、各类payload Schema、实际字节复制与提交水位验证仍待完成，D-04保持开放。
