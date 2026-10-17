# 当前发布版本的冻结正文读取器

状态：默认实现及当前范围的实库验证已通过。实现 `contracts.FrozenContentReader`，不改变现有正文校验器的标准接口。

`postgres.PublishedFrozenReader` 依赖 `QueryReads`，仅返回当前授权范围内、仍获批准且有效发布的指定 Document/Revision。底层仓储或授权提供者失败、请求无期限、版本不匹配、草稿、撤回或批准失效时，返回错误且不返回部分正文。

同一只读快照内，读取当前发布和审核事实，然后加载类型化 Revision 与保存的 `akos.freeze/1` JSON。重新构建标准冻结内容，逐字节比较保存正文，并核对 ContentHash、BlocksHash 及 HashFormat。原发布准备任务的内容读取也复用同一个内部核验器，执行租约和发布状态准入仍由各自调用路径独立控制。

正文取回后另开当前观察点，比对发布、批准、策略及存储身份。返回的字节独立复制，调用方修改不会改变数据库或其他读取结果。单份标准正文最多4 MiB，类型化 Revision最多8 MiB，并受请求、Scope和父Context的共同时间预算限制。

该模块核实数据库当前权威正文与批准摘要的一致性。它不检查原件或媒体实际字节、解析语义正确性、具体Evidence位置、索引可用性，也不构成最终模型/API输出许可。当前实库权限提供者仍是fixture；真实身份、CurrentControl/CurrentBackend与产品输出接入保持待验。

当前原生解析候选的引用是 original/coarse，而既有 `VerifyTextEvidence` 只接受 revision 文本定位。必须补原件/来源核验或经验证的引用映射，不能直接改写来源类型来跳过校验。

## 验证证据

[独立运行复核](evidence/published-frozen-runtime-review.json)核对三版PG共12份实际正文的Schema、canonical字节、双摘要及当前发布归属；原生实库覆盖返回值隔离、跨空间/跨Revision/草稿拒绝、迟到权限失效、无期限/取消及撤回/撤销批准后拒绝。

同一479项输入与同一Go探针在PG15.19/16.15/18.6各通过184项累计检查；Go全量测试/vet、227项Python、三份OpenAPI与既有冻结/审批/发布/检索独立复核均通过。测试实例、私有目录及端口清理已另行核对。该证据不关闭完整CHECK、原件真实性或产品输出验收。

2026-10-16后续接入：[EvidenceBackend](POSTGRES_EVIDENCE_BACKEND.md)已在实库串联索引、冻结正文及原件引用复核，原文中的“当前后端待实现”为该阶段历史边界。生产CurrentControl/IAM、最终模型/API输出及完整运行验收仍待完成。
