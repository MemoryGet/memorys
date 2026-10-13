# 调用状态、取消与类型化结果契约 V1

本规范补充[远程模块协议](REMOTE_MODULES.md)的control/status和control/cancel，Go映射位于`control.go`及`control_results.go`。当前只有数据编解码，没有持久调用账本、当前权限/Grant验证或实际取消执行。

## 1. 控制请求和两类身份

status/cancel都使用独立受认证context及input.target_invocation_id。控制请求具有新的invocation_id，不得与目标ID相同；它可以使用新的request_id，但不能改变目标调用最初的请求摘要、deadline、作用域、模型或产物尝试。

调用适配器从受信原始账本取得ControlExpectation：目标invocation ID、原业务operation、原request ID，生成结果还含原配置版本/摘要。不能从返回的result自报这些期望，不能依据客户端提交的user_id、target_request_id或“成功证明”绕过当前访问检查。

外层request_id/invocation_id对应**这次控制请求**；failed状态中error.request_id与缓存生成AnswerResult.request_id对应**原业务请求**。控制请求自身失败使用RemoteError，内外request ID则都指控制请求。两种错误不可混用。

目标地址、模块/制品版本和账本命名空间由原调用记录与受控部署解析；实例迁移/重启不能让同名ID指向别的调用。未知、越权、过期或不可安全解析的目标统一隐藏，不凭空重新启动工作。

## 2. 六种状态及闭合结果联合类型

| state | 载荷 | 可得出的结论 |
|---|---|---|
| running | 无result/error | 观察到仍运行；不能据此重复启动 |
| uncertain | 无result/error | 外部效果或账本事实未确定；保持同一目标探查/隔离 |
| cancel_requested | 无result/error | 请求停止已登记；不证明已经停止或回滚 |
| cancelled | 无result/error | 账本确认停止；不是一条成功业务结果 |
| succeeded | 必须有result，不能有error | 原调用有类型化结果；仍需原业务校验和当前权限 |
| failed | 必须有error，不能有result | 原调用确认失败；安全文案按原request ID投影 |

InvocationResult为operation＋value的封闭联合类型，不是任意map或供应商SDK对象。它覆盖15项业务操作，控制操作自身不作为业务目标：

| operation | value / Go分支 |
|---|---|
| parser_parse | ParseResult / Parsed |
| blob_store_put | BlobRef / Blob |
| blob_store_open | BlobTransferSummary（blob）/ Blob |
| embedder_embed | EmbedResult / Embeddings |
| index_writer_prepare、composite_ingest_search_prepare_projection | PrepareResult（proof＋私有bindings）/ Prepared |
| retriever_search、composite_ingest_search_search | RetrieveResult（candidates）/ Candidates |
| reranker_rerank | RerankResult（evidence）/ Evidence |
| generator_generate | AnswerResult / Answer |
| blob_store_delete、index_writer_verify/delete、composite_ingest_search_verify/delete | completed=true的Ack / Completed |

Native只能设置一个分支，operation必须与独立原调用期望一致。result=null、未知字段、未知operation、类型替换、完成Ack为false或非法状态组合拒绝。生成Trace核对原request ID、配置版本和摘要。

这些检查不替代原操作校验：嵌入须核对文本数量/规格/维度，准备须核对attempt/fencing/各通道证明，检索/重排须核对原过滤目标与证据，生成须复核原合法来源/结果语义，Blob须核对原字节期望。status中的open摘要不能代替收到真实二进制EOF/哈希一致；需读取字节时重新执行受授权的open。

## 3. 取消、账本和当前访问闸门

cancel只返回cancel_requested或already_terminal，必须回显相同target。already_terminal不指定原终态为成功/失败/取消，继续查询同一目标。请求发出、客户端context结束、单次查询超时、服务失联都不是确认cancelled的依据。已提交写入和平台发布不会因取消自动回滚。

生产账本在写操作执行前持久记录调用者、原主体/委托、模块/配置、目标对象、规范摘要、attempt、幂等信息和状态；终态和结果在同一事务确认。状态观察与取消登记使用当前版本/租约CAS，已确认终态不能因迟到观察变回running。外部结果未知保持uncertain，用同一关联键/attempt恢复；无法验证停止的旧Worker只写隔离产物，平台fencing阻止提交。

控制读取必须重新授权原调用者和原资源/来源，并复核当前权限、发布/撤回事实。旧答案包含已失去访问权的证据时不能只删引用继续返回正文；应拒绝这次控制读取。拒绝使用外层RemoteError，不把原账本的succeeded改成failed。内部准备映射和供应商引用不得因此进入公开伙伴/MCP结果。

本次实现没有这些账本/访问/取消行为，不能据一个DTO标签恢复或终止任务，也不能用控制请求绕过审核和发布事务。

## 4. 验证与仍待完成的证据

```powershell
python docs/architecture/check_control_wire_mapping.py
& '.tools/go/bin/go.exe' test -count=1 ./pkg/modulewire
```

22项实际Go样本覆盖15个业务结果分支、六种状态、两种cancel响应；独立Schema在前后验证并做规范字节对照。7类非法目标、类型/状态替换、原/控制request ID混淆、原生成配置失配和伪确认取消被拒绝。Go规则测试还检查自控制、模糊Native联合类型、缺失/多余/null结果、安全文案及完成Ack。

这是数据对照，不证明任何后端终态真实、当前权限正确或取消已执行。全量字段/类型映射取得局部证据后，D-03仍须HTTP/multipart/SSE、Grant、当前访问闸门、持久账本、幂等/断网/停止确认及真实提供方公共契约试验。整体架构验收继续未通过。
