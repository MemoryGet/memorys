# 本地索引实际清理适配器

实现：[cleanup_memory.go](../../pkg/adapters/cleanup_memory.go)、[inspect.go](../../pkg/index/inspect.go)。这是一种使用现有MemoryIndex代码的实际执行实现，身份/权限签发仍需可信来源；不代表成熟外部向量库或生产持久化索引已经接入。

## 接口与授权

MemoryCleanup实现标准CleanupExecutor，匹配固定ModuleInstanceID/ProfileDigest且仅支持keyword索引目标。接到Operation后重新计算固定效果摘要，拒绝目标/摘要不符。Authorize必须核验固定来源并独立签发index.delete请求，不能把任务Scope的publication.cleanup.execute动作直接改名。

原生MemoryIndex.Delete和InspectGeneration均要求当前AuthorizeDelete来源、有限context预算、未过期Scope和实际生成代际所属空间/KB。适配器还核对Document/Revision/BackendEpoch，不能把清理某个固定代际扩大成整个文档或当前索引。Authorize接收目标指针的独立拷贝，不能通过修改GenerationID改写编排输入。

## 实际效果核验

InspectGeneration读取原生索引的Owner、代际删除标记及剩余记录数。Lookup只对已知且所属一致的代际返回状态；未知代际拒绝，不把无法证明所属当成成功。Deleted且记录数为0才返回succeeded，否则已知未删除返回absent。

Execute调用现有Delete，并重新Lookup。VerifyReceipt再读取实际Owner/删除标记/记录数并比较回执；相同digest和成功字符串不能代替真实删除。成功结果引用由固定OperationID派生，重复执行仍删除同一代际。不同模块/配置、错误授权动作、分块级删除请求或缺少后端授权均拒绝。

## 已验证与尚未验证

2项适配器Go测试覆盖实际删除/重放、删除前伪造成功拒绝、新代际保持完整、旧代际迟到Upsert拒绝，以及错误动作/实例/缺少后端权限拒绝。

PG原生链路探针使用真实CleanupTasks、VerifyLease、Worker、MemoryCleanup、MemoryIndex.Delete与独立InspectGeneration，最终检查数据库任务成功、旧代际空且有删除标记、新代际记录保留、旧记录不能重写。准备的投影数据及权限/配置准入来源仍为fixture，新代际数据不证明完整审核/重新发布流程已实现。

Owner/删除标记仍只在进程内，重启会丢失；已有Task状态持久化不能替代索引侧的重启防复活。当前模块Profile由受控适配器配置声明，生产装配还要证明实际后端配置/绑定一致。原生Inspect权限沿用内部index.delete授权，不作为普通检索用户接口。

后续需持久化索引代际/fence、真实身份及发布者权限、受控注册工厂、配置/依赖检查、外部检索适配器、第二实现替换、导出迁移与崩溃恢复。当前仍不注册完整IndexWriter/生产cleanup能力，WTX-05/06及生产验收保持开放。


本轮证据：15.17/18.0 SQL+Go真实任务/本地索引链路均通过，测试容器已清理；全量Go测试/vet、223项Python及三份OpenAPI检查通过，当前源文件/SQL哈希与归档一致。身份与新投影数据为fixture，生产与迁移验收未完成。


已抽取[双实现清理后端接口](INDEX_CLEANUP_BACKENDS.md)，通用IndexCleanup适配器可注入MemoryIndex或PG清理原型；完整检索和迁移验收未完成。
