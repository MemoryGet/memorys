# 索引清理双实现与持久化边界

标准端口：[index_cleanup.go](../../pkg/contracts/index_cleanup.go)。IndexCleanupBackend提供Delete和InspectGeneration，返回统一IndexDeletionState；原index.GenerationState为类型别名。通用适配器改为IndexCleanup，MemoryCleanup保留兼容别名，业务Worker不识别具体后端。

## 两种实现

| 实现 | 当前效果与限制 |
|---|---|
| MemoryIndex | 原生内存记录/Owner/删除标记，进程内幂等及晚到写入拒绝；重启标记丢失 |
| postgres.IndexCleanup | SQL原型中的投影记录/持久删除标记，原子删除与独立状态读回；没有完整检索/IndexWriter和标准数据迁移 |

两个实现通过同一适配器的Index接口注入，固定操作、授权、回执及调用方式保持一致。共享能力范围仅是代际级清理/读回；不能据此声称完整检索引擎、所有UI或伙伴调用已通过替换验收。

## PostgreSQL写入与删除隔离

index_projection_rows保存测试投影JSON对象，外键绑定权威IndexGeneration；不是已经冻结的完整Chunk/Citation导出协议。index_deletion_fences记录已删除代际并禁止应用修改/删除。

Delete验证当前index.delete Scope、空间/KB及实际Generation归属；同一个Serializable事务插入删除标记、删除该代际全部投影。未知代际或分块级请求拒绝。InspectGeneration读取实际Owner、删除标记和记录数，不以搜索没有命中推测删除成功。

投影insert/update先锁定Generation并检查删除标记。首次标记插入还推进父Generation的projection_epoch，使旧Repeatable Read/Serializable快照的迟到写入发生序列化冲突，而不是等待锁后继续用旧快照绕过标记。删除与状态读回持有独立请求/Scope拷贝。

## 实际证据范围

PG探针通过通用适配器实际删除、独立核验，重建SQL连接/后端对象后读取相同成功回执；另检查新代际不受影响、普通迟到写入拒绝，以及删除前建立的Repeatable Read快照不能重新插入旧代际（SQLSTATE 40001）。投影与权限仍为fixture，测试容器无宿主端口、临时数据会清理。

现有原生MemoryIndex测试继续通过相同接口。两种实现尚未使用同一完整评测集进行导出、迁移、回退与效果对照；PG探针是清理适配器试验，不是完整PG检索器、生产消费者或数据库重启/断电恢复试验。

后续必须实现：标准投影写入/读取、真实权限/角色/RLS、完整配置与注册工厂、删除标记和配置代际的导出迁移/回退、数据库重启/故障恢复、外部成熟引擎及真实召回/性能评测。当前不能注册完整IndexWriter或metadata_uow，也不关闭WTX-05/06。


本轮证据：15.17/18.0 SQL+Go清理后端、重建连接读回及旧快照拒绝均通过；自身测试容器清理。全量Go测试/vet、223项Python与三份OpenAPI检查通过，当前源文件/SQL哈希与归档一致。完整检索、迁移及生产恢复未验收。


已补[同数据索引快照交接](INDEX_SNAPSHOT_TRANSFER.md)：内存→PG→新内存校验值一致，恢复代际/分块删除状态及实际失败回滚；全量平台迁移与部署切换仍未验收。
