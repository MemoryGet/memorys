# 同数据索引交接与回退试验

代码：[标准快照](../../pkg/contracts/index_snapshot.go)、[校验与规范化](../../pkg/indexsnapshot/snapshot.go)、[内存恢复](../../pkg/index/snapshot.go)、[PG恢复](../../pkg/postgres/index_snapshot.go)。范围为现有keyword投影数据与删除状态，不是完整索引引擎/元数据迁移。

## 标准数据及校验

IndexProjection替代原Record结构并保留类型别名。IndexSnapshot包含format、space/KB、代际Owner/删除状态/记录数、完整Chunk/Citation投影、分块删除标记和Checksum。Owner绑定Document/Revision/BackendEpoch；不能只搬运搜索文本而丢掉引用或删除标记。

局部格式akos.index.snapshot/1。按GenerationID及GenerationID+ChunkID排序，空记录/分块标记数组规范为[]；Checksum字段清空后，以AKOS-CJSON/1编码并计算SHA-256(format+LF+bytes)。格式/范围、重复代际/记录/标记、Owner关系、删除状态、记录数和引用关系不符拒绝。上限1000代际、10000记录/分块标记、4MiB编码；规范为进程内草案，完整跨语言/远程格式冻结仍待完成。

校验值证明内容一致，不证明来源或批准状态；Published只是原投影标志。导出/导入必须独立授权index.export/index.import，原文件、审核、当前权限、Profile/EmbeddingSpec及发布状态仍由平台权威数据掌握。

## 原子恢复与目标条件

MemoryIndex先构建暂存实例，验证并恢复Owner/记录/代际与分块删除标记，在当前授权/期限复核后一次替换空目标；不覆盖已有Owner。导出在固定读锁下取快照并克隆，返回前在锁外复核当前权限。

PG要求平台Generation目录已在目标范围预先登记，逐一锁定并核对Owner与快照完全一致；所选代际必须无记录/删除标记。全部投影和删除标记在同一个Serializable事务写入，失败回滚。分块标记同样推进父代际projection_epoch并拒绝迟到写入，不能恢复后只保留整代际标记而丢掉局部分块删除。

目标空间/KB、Generation目录和真实授权不是由快照自动创建；生产切换还需隐藏目标命名空间、源写入暂停/水位追平、验证、配置重启与回退窗口。当前尚无该部署级迁移协调器。

## 本轮实际试验

同一套中文完整投影包含3个代际：整个旧代际删除、正常存活代际，以及删除一个分块但保留另一个分块。试验路径：MemoryIndex导出 → PG导入/导出 → 新MemoryIndex导入/导出，三个快照Checksum完全一致；两端均拒绝旧代际/已删除分块重写；恢复后的内存存活记录可继续写入，PG存活内容在导出校验中保持完整。

PG注入删除标记恢复失败（父版本溢出）发生在投影INSERT之后，确认没有投影残留，然后恢复条件再完整导入。另验证损坏Checksum拒绝及已有内容目标不被覆盖。权限、平台Generation登记与投影为fixture，未证明真实身份或完整发布流程。

尚需标准源文件/元数据/审核/任务/配置的全量迁移、真实权限、外部成熟引擎、向量规格/模型数据、跨语言金标、数据库重启/断电、写入追平和实际部署切换/回退。不能把本试验扩大为整个Knowledge OS模块替换验收；WTX-06及总体生产验收保持开放。


本轮证据：15.17/18.0 SQL+Go同数据往返及真实恢复故障回滚均通过，测试容器清理；全量Go测试/vet、223项Python和三份OpenAPI检查通过，当前输入哈希与归档一致。全量平台迁移及生产恢复仍未验收。


已补[配置装配试验](INDEX_MODULE_ASSEMBLY.md)，同一快照通过内存/PG受管端口导出一致；外部部署Schema及生产Bootstrap仍待完成。
