# 索引准备、复合投影与验证/删除的远程映射

这是D-03索引部分的字段与关系验证。实现见 [indexing](../../pkg/modulewire/indexing.go)、[index controls](../../pkg/modulewire/index_controls.go)。没有真实索引服务、远程调用、发布事务或后台Worker。

## 1. 端口映射

| 进程内对象/操作 | wire字段或结果 | 规则 |
|---|---|---|
| IndexPrepareRequest.Request/Scope | context | 业务请求与已验证作用域；普通DTO不自证授权 |
| GenerationSetID/Generations | generation_set_id/generations | 所有目标绑定同KB、Revision、BackendEpoch和索引Profile；每通道一个逻辑代际 |
| OperationID/AttemptID/FencingToken | operation_id/attempt_id/fencing_token | 三者完整保留，uint64不经float中转；外部产物以attempt隔离 |
| Chunks/BlockSpan | chunks/spans与char_start/char_end | 保留来源Block、码点半开范围、文本、分块Profile和内容摘要 |
| Vectors | vectors | 只用于独立IndexWriter；向量数量/维度匹配Chunks及确切目标Spec |
| CompositePrepareRequest | 同上，另含projection_format_version | 没有Vectors字段，外部向量与未知字段拒绝；后端内部嵌入仍须核验规格 |
| ReadinessProof及私有映射 | PrepareResult.proof/bindings | Proof完整逐通道；generation_id到opaque_backend_ref完整、一一覆盖目标 |
| IndexWriter/Composite.Verify | input.proof→Ack | 映射原Proof；Ack.completed=true表示此远程操作完成，不是平台发布完成 |
| IndexDeleteRequest | generation_id/chunk_ids/mode→Ack | 显式区分整个指定代际与指定分块，不能用空列表推断无边界删除 |

Verify原Go方法只接收Proof，传输层显式补RequestContext和Scope。提供给远程模块的所有状态/Scope/Proof字段都需结合可信身份、Grant及当前平台/后端状态检查；不能把它们当授权凭据。

## 2. 准备请求的不变量

编码与解码检查目标非空、ID/操作/尝试非空、fencing_token非零；候选State为building，禁止把活动ready产物作为原地准备目标。目标必须同KB/Revision/epoch/Profile，KB在Scope内，代际ID及通道不重复。

Chunks非空、ID唯一且同Revision；来源Span有Block ID，字符范围成对且0≤start≤end。完整来源归属、范围上限、原件/审批内容校验仍由应用负责。索引构建只消费已经批准的内容，不利用结构样例绕过审核。

独立向量通道需要外部Vectors，数量与Chunks一致、维度与目标Spec一致；没有向量通道时Vectors为空。复合通道只接收投影，版本严格声明。投影变更、重新解析和嵌入切换沿既定新代际/新Revision规则进行。

## 3. 准备响应与可信期望

`DecodePrepareResult`要求应用提供PrepareExpectation：目标集合、attempt、完整Generation元数据、批准内容摘要、逐目标预期计数和向量规格摘要。期望必须来自权威Revision/IndexProfile/构建清单，**不能从后端响应反推**。

校验包括：

- 集合、attempt、content_digest与索引Profile对应；请求Profile可能是组合流水线，不能误用它覆盖IndexProfile摘要。
- 期望目标唯一、同KB/Revision/epoch/Profile，数量映射完整且非负；摘要为规范SHA-256。
- Proof恰好覆盖各目标，通道/epoch/数量及向量Spec摘要匹配，拒绝缺失、重复、额外或过期目标。
- bindings与目标一一对应、引用非空；它们保存在适配映射仓储，不输出到伙伴citation。

计数的权威单位由构建清单确定；ChunkCount记录可检索的逻辑内容单位，不把厂商图顶点/物理对象数量偷换成内容覆盖率。该DTO校验不读取后端，不能证明内容真实可搜索、过滤有效、当前租约仍归该Worker或映射已持久成功。

适配器先验证/持久化私有映射，再把ReadinessProof交回应用；映射失败不得把发布操作推进为ready。随后仍须执行真实Verify、既有GenerationSet关系检查、当前审批/权限/fencing及发布CAS。旧attempt的成功响应不能覆盖已经选定的新产物。

## 4. 明确删除与Ack语义

wire DeleteInput的mode必需：generation表示删除generation_id指定的单个逻辑代际，chunk_ids必须为空；chunks表示只删该代际内列出的非空、唯一Chunk ID。Go既有IndexDeleteRequest中空ChunkIDs按前者映射，非空按后者映射，此规则在传输与原生适配器契约中保持一致。

空列表不表示删除整个collection、所有空间或所有代际。接收方查当前映射/保留与引用后执行，不能把backend_id作为任意资源地址。相同逻辑对象被多通道共用时仍遵守引用/保留规则。

Ack.completed=false、缺失、错误request_id/invocation_id均拒绝。Verify Ack只代表已完成验证调用，删除Ack只代表其定义的删除完成；两者都不自动改变发布指针。未知写入结果仍按调用账本/状态协调处理，不能用断连或缺少回执认定失败。

## 5. 证据与剩余门槛

Go测试覆盖索引/复合准备完整往返、uint64栅栏、来源范围、外部Vectors拒绝、过期attempt、可信期望/数量/Spec拒绝、验证/删除及Ack。另有错误Scope、活动目标、重复目标、不同Revision和向量形状等拒绝场景。

[check_index_wire_mapping.py](check_index_wire_mapping.py)对6项index/composite prepare/verify/delete操作运行独立Schema→实际Go类型往返→Schema与规范字节复核；另外拒绝旧attempt、错误私有映射和错误内容计数。样例把权威期望放在独立expectation数据中，不从response提取。

```powershell
python docs/architecture/check_index_wire_mapping.py
python docs/architecture/build_remote_contract.py --check
& '.\.tools\go\bin\go.exe' test -cover ./...
& '.\.tools\go\bin\go.exe' vet ./...
```

离线工具不会调用真实IndexWriter.Verify/Delete，也不提交映射或发布状态。真实后端准备/过滤、映射持久化、租约、授权、故障恢复和迁移回退均未通过；文件/生成/控制接口与生产安全/网络还需完成，D-03及整体目标保持未完成。
