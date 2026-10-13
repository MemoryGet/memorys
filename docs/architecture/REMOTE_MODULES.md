# 远程模块HTTP契约草案 V1

机器契约见 [modules.v1 OpenAPI](openapi/modules.v1.json)，生成器见 [build_remote_contract.py](build_remote_contract.py)。首轮包含17项操作，当前是协议设计和离线样例，没有服务处理器、签名验证器、网络适配器或真实模块联调。

## 1. 接入边界

应用服务调用进程内端口；HTTP客户端适配器负责标准DTO转换。远程模块是受控能力服务，不能作为公开伙伴API，不能接收用户或Agent提供的任意模块地址、Scope或供应商凭据。gRPC由适配器映射至同一业务语义，不跨网络传递SQL事务或进程内句柄。

URL固定为`/modules/v1/<capability>/<operation>`，服务地址由部署允许清单选择。该版本与业务contract_version=1.0分别校验。HTTP语义版本兼容不替代后端索引布局、模型规格和持久化epoch检查。

## 2. 请求包络与授权

每个JSON请求包含context与input，禁止未知字段。context包含request_id、invocation_id、module_instance_id、contract_version、deadline、profile_digest和scope。request_id追踪业务请求，invocation_id标识一次远程调用，operation_id/attempt_id标识发布/任务尝试；三者不可混用。

时间采用UTC的RFC3339Nano规范表示：大写T/Z、带秒、最多9位小数且无冗余尾零，不支持偏移时区或闰秒。必须验证实际日历值，不能只信任Schema的format注解或截断更高精度时间。deadline不得晚于scope.expires_at；运行时还需检查当前时间、时钟偏差和剩余总预算。

整数必须按目标端口的有界整数无损映射，不能经double中转；bbox使用float64最短JSON数值的可往返表示，无法保留的额外精度拒绝，业务高精度数值使用number_text。机器注解不替代这些语义检查。

Vector明确使用平台float32派生dtype，按最近值/偶数舍入转换，最短表示编码；拒绝溢出及非零下溢为零。不能把此向量转换规则用于权威精确值。Generation的state/backend_binding/readiness本次补齐，以保持Go字段对照；它们始终需要真实后端与平台状态复核，不作为自证权限。

服务身份由Authorization Bearer验证，部署同时限制内部网络/HTTPS与服务访问权限。`X-AKOS-Access-Grant`是额外的受验证访问能力，不等同服务身份；两者均验证成功才能调用能力。服务凭据与原用户Token不互相替代，不将原用户Token转发给厂商。

GrantVerifier的可信结果必须绑定：发行方/目标模块与服务调用方、method/path、invocation_id、规范请求摘要、scope摘要、profile_digest及有效期。凭据/签名校验使用受控成熟实现，不能自行写算法，不能从普通JSON字段构造“已验证”结果。具体token格式、密钥轮换和验证器接入还需在生产接入阶段锁定。

普通scope DTO不是授权证明。模块校验Grant与context/input一致，并通过受控访问闸门检查当前授权/发布版本。签名或TTL本身不能满足立即撤销；只能离线验签的服务不能获此语义认证。远程处理已收到的在途内容无法追回，新的读取/查询/输出遵循既定线性化边界；应用返回给用户前仍复核。

请求摘要使用AKOS-CJSON/1，不受JSON字段顺序或数值等价表示影响。multipart只规范化metadata，其expected_sha256和size绑定文件；接收方按实际流验证字节。超过当前参考编码器的载荷需经验证的流式摘要实现，不能截断或切换成不一致的序列化方式。

## 3. 操作与进程内端口映射

| 远程操作 | 进程内端口/载荷 | 必需语义 |
|---|---|---|
| parser/parse | Parser.Parse；ParseInput→ParseResult | Draft/原件归属；返回ParsedBlock不虚构Revision；来源与提取媒体均可验证 |
| blob_store/put | BlobStore.Put；multipart metadata+file→BlobRef | 大文件流式、实际大小/哈希、幂等及逻辑Blob标识 |
| blob_store/open | BlobStore.Open；BlobReadInput→octet-stream | 当前授权、取消；收到EOF且字节哈希/长度匹配才算完整读取 |
| blob_store/delete | BlobStore.Delete；BlobReadInput→Ack | 指定逻辑对象的幂等删除，保留/关联规则由治理校验 |
| embedder/embed | Embedder.Embed；EmbedInput→EmbedResult | 数量、维度、有限数值与确切Spec一致；float32映射不能静默溢出 |
| index_writer/prepare | IndexWriter.Prepare；IndexPrepareInput→PrepareResult | attempt隔离、权威fencing、不可覆盖活动产物 |
| index_writer/verify、delete | IndexWriter.Verify/Delete | 真实过滤/可搜索性验证；逻辑代际删除与独立重试 |
| retriever/search | Retriever.Search；RetrieveInput→Candidates | 前置scope/发布/代际过滤；空允许集合不上送无过滤后端调用 |
| composite_ingest_search/prepare_projection | CompositeEngine.PrepareProjection | 不接受外部Vectors；复合通道整体绑定与规格验证 |
| composite_ingest_search/verify、delete、search | CompositeEngine对应方法 | 与独立接口相同安全语义，内部能力不冒充细粒度替换 |
| reranker/rerank | Reranker.Rerank；Candidates→Evidence | 只排序/筛选提供的合法候选，不新增来源 |
| generator/generate | Generator.Generate/AnswerSink；GenerateInput→SSE | 相同预算和证据白名单；取消传播，合法终态，不能自行扩权反思 |
| control/status、cancel | 远程调用账本/取消；不属于业务知识模块 | 查询本次调用结果或请求停止，不能批准、发布或回滚已提交业务 |

RequestContext/AccessScope转为context，解析选项、Chunk范围、Generation/Spec、Proof、Candidate及预算转为明确wire字段。Go联合内容/AnswerEvent不能直接默认JSON编码。其他端口首轮保留进程内实现，远程化时必须增加相应正式映射，不能套无类型万能payload。

PrepareResult额外返回generation_id与opaque_backend_ref的私有映射。客户端适配器核对它与目标/Proof的唯一性和一致性，在自己的映射仓储持久化，再返回Go端口的ReadinessProof。引用始终只用平台ID；后端引用不能进入伙伴Evidence或MCP结果。收到Proof不代表已发布，仍需后端Verify、关系校验与平台CAS。

## 4. 响应、二进制与SSE

Blob三操作的实际Go metadata映射、局部完整性检查与尚未实现的传输边界见[文件远程映射](BLOB_WIRE_MAPPING.md)。局部EOF/context/响应头试验不能证明真实HTTP取消或存储删除已通过。

JSON成功响应包含request_id、invocation_id和result；ID必须与请求一致。二进制/SSE响应以对应X-AKOS响应头回显ID，服务须发送，客户端须检查；OpenAPI头声明本身不能证明实际发送。错误使用RemoteError包络，内部Error.request_id同样匹配，不输出供应商原始cause或密钥。

实际Go RemoteError映射、安全模板与重试/结果未知边界见[错误映射](ERROR_WIRE_MAPPING.md)。已验证DTO安全投影，供应商分类、HTTP/调用账本和重试执行仍待实现。

blob_store/put的multipart metadata部分为JSON，file为二进制；open的成功体为application/octet-stream，不把字节塞进JSON数组或供应商URL。失败若尚未发送响应则返回安全JSON错误；已开始字节流后中断读取，由校验失败/取消显式传播，不能往二进制体追加错误JSON。

生成使用标准AnswerEvent载荷：meta在前，answer_delta/citation在中，done或error是唯一终态。event名与data.type一致，done只表示完整结果；流断开或超时不是成功。客户端AnswerSink错误使适配器取消远程调用。出现文本增量后不自动重试整次生成，以免拼接两份答案。远程引用和最终内容在应用输出前重新检查。

实际Go请求、五类event-data及顺序/白名单检查见[生成映射](GENERATION_WIRE_MAPPING.md)。目前未实现SSE帧、模型调用或sink/网络取消，不把事件数组校验当成真实流式联调。

后续已提供[SSE接收辅助器](SSE_TRANSPORT.md)，有帧与真实本地HTTP读取/取消/关闭证据；终态等正常EOF后才输出。尚未接入真实权限/Grant、模型或调用账本，不把接收端停止当成后台任务已取消。

当前机器Schema限制各集合数量；消息字节、文件字节、并发和执行时间由部署/能力清单协商，客户端和服务端同时执行。业务30秒问答上限保持；解析/索引Worker使用独立有界任务预算，上传不等待远程解析完成。不能因同步HTTP接口而把长解析搬回用户上传请求。

## 5. 幂等、结果未知与取消

同一invocation_id的重试保留同一规范请求体/摘要，不能延长原deadline；相同ID不同载荷返回conflict。写调用执行前持久记录调用者、scope/对象、摘要、attempt与状态，终态和可复核结果需要持久保存。账本保留期与重试/回退窗口明确配置，过期后不承诺永久去重。

网络超时后不能断言写入失败。先用新的受认证控制请求查询原target_invocation_id；status可为running、uncertain、succeeded、failed、cancel_requested或cancelled。succeeded必须有类型化结果，failed必须有安全错误，其他状态不得夹带成功结果。status需要当前调用者/资源授权，未知或越权ID隐藏为not_found。

缓存查询结果允许在重启后过期，但重取前须重新授权；写账本缺失/不确定不能作为安全重发或新版本激活的证明。厂商创建结果不明时采用可核验关联键和隔离attempt协调，无法唯一识别的产物保持uncertain/孤立，不绑定正式发布。

cancel只返回cancel_requested或already_terminal，不能宣称请求发出即已停止。适配器取消context/厂商任务并核对终态；已经提交的远程写入或平台发布不会自动回滚。不能停止的工作只写旧attempt命名空间，平台fencing阻止迟到结果提交。失联、观察超时不等同终态；控制查询继续跟踪同一个调用。

控制请求/原请求关联、15类结果与六种状态的实际Go映射见[控制契约](CONTROL_WIRE_MAPPING.md)。[17操作覆盖表](REMOTE_MAPPING_COVERAGE.md)汇总当前局部数据证据；持久账本、当前授权及真实停止仍未实现验证。

只读失败可以在剩余预算和重新授权后有限重试；写入必须按去重与未知结果规则协调。不能为重试更换scope、模型修订或目标代际，也不能让底层SDK的默认无限重试突破应用预算。

## 6. 当前证据与未完成门槛

17项操作机器契约已生成，独立OpenAPI3.1.1规范校验通过。7项离线测试覆盖服务身份/Grant声明、解析样例及字段/日期拒绝、复合Vectors边界、候选代际、安全错误、调用状态与二进制/SSE声明；finite/日历检查不依赖可选format插件。

尚未完成：全部操作的进程内/远程字段对照与往返样例、GrantVerifier/当前访问闸门、请求/结果关联和语义验证、持久调用账本、真正的取消/超时/二进制/SSE行为，以及真实提供方公共契约试验。因此D-03已有机器契约与传输规则，尚不能标记完整验收关闭。

解析部分现已补 [Go字段映射与7组往返](PARSER_WIRE_MAPPING.md)，验证真实contracts类型及转换前后Schema/规范字节。其他操作及生产安全/网络行为仍待验证，不能用该子集证明全部远程端口兼容。

嵌入/检索/重排也补 [完整字段与实际Go往返](RETRIEVAL_WIRE_MAPPING.md)，包括完整代际控制字段与结果目标关系检查；尚未执行模型/后端或网络行为。

索引与复合prepare/verify/delete补 [完整字段及6操作往返](INDEX_WIRE_MAPPING.md)。删除加入显式mode，准备响应使用独立权威期望校验；真实Verify、删除、映射持久化和发布仍未执行。

```powershell
python docs/architecture/build_remote_contract.py --check
python -m unittest discover -s docs/architecture -p test_remote_contract.py
```

结构测试不能证明签名、授权或网络行为；对照和故障试验通过后再冻结对应远程端口。
