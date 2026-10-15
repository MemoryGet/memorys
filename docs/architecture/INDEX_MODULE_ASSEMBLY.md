# 索引窄能力配置装配

实现：[index_factory.go](../../pkg/adapters/index_factory.go)。新增仅用于进程内解析后计划的index_cleanup/index_snapshot端口，分别校验IndexCleanupBackend/IndexSnapshotStore。没有将部分实现注册为完整IndexWriter或metadata_uow；远程模块与外部部署Schema尚未加入这两项能力。

IndexStoreFactory由可信宿主提供已配置后端、快照端口及Health。构造器负责数据库、真实权限和凭据，Settings仅接受{}，不允许通过设置提供密码或绕过绑定。模块实例是受控注册，不加载动态插件。当前Provider示例reference.index.memory/reference.index.postgres在同一注册清单中，由Config.Provider选择。

声明generation.delete/inspect、delete.fence和snapshot.export/import/delete_fences、target.empty。持久实现额外声明fence.durable；内存实现不能满足此要求。声明是启动兼容性检查，实际能力仍需共同测试与验证，不能仅因Host传入durable=true就证明后端可靠。

启动拒绝typed-nil端口、缺少Health、非空/额外Settings、版本或所需特性不兼容。Health由宿主提供，PG探针仅验证连接；生产还必须验证完整Schema、实际授权/配置及迁移状态。当前不是已验收的生产Bootstrap/BuildLock归一化。

关闭只使受管门面失效，不关闭宿主共享DB/后端。宿主必须先停止投递并排空进行中的调用；没有实现运行中热替换或任意插件强制中断。重建运行时产生新门面，旧引用不能继续使用。

实库配置试验通过同一组GenerationIDs/完整快照，分别构建内存Provider运行时和PG Provider运行时；相同业务端口ExportIndex返回同一Checksum，Provider变化绑定不同ConfigVersion。关闭后旧引用拒绝，宿主PG连接和原后端仍可用。2项Go测试覆盖持久特性不满足、关闭引用、typed-nil及设置拒绝。

尚未完成外部部署配置加载、真实依赖/权限装配、完整检索器、UI/伙伴调用切换、持续写入排空/追平、自动迁移协调、数据库重启及生产回退窗口。这个结果证明窄端口通过配置选择两种实现，不代表整个Knowledge OS已完成模块替换验收。


本轮证据：15.17/18.0配置装配实库试验通过，测试容器清理；全量Go测试/vet、223项Python及三份OpenAPI检查通过，当前输入哈希与归档一致。生产验收标记保持false。
