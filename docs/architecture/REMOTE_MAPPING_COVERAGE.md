# 远程17操作字段映射覆盖与未验门槛

复核：2026-10-13。依据为实际Go契约/转换、modules.v1 OpenAPI、独立Schema前后样本与本次重新运行输出。这里的“通过”仅指**对应数据映射样本**，不是模块能力、完整领域验证或真实协议认证。

| operationId | Go数据边界 | 对照脚本 | 数据样本 |
|---|---|---|---|
| parser_parse | ParseRequest / ParseResult | check_parser_wire_mapping.py | 通过 |
| blob_store_put | BlobWriteRequest metadata / BlobRef | check_blob_wire_mapping.py | 通过；无multipart |
| blob_store_open | BlobReadRequest；正文另作局部流检查 | check_blob_wire_mapping.py | 请求通过；无真实二进制传输 |
| blob_store_delete | BlobReadRequest / Ack | check_blob_wire_mapping.py | 通过；未执行删除 |
| embedder_embed | EmbedRequest / EmbedResult | check_retrieval_wire_mapping.py | 通过 |
| index_writer_prepare | IndexPrepareRequest / PrepareResult | check_index_wire_mapping.py | 通过 |
| index_writer_verify | ReadinessProof / Ack | check_index_wire_mapping.py | 通过；未执行Verify |
| index_writer_delete | IndexDeleteRequest / Ack | check_index_wire_mapping.py | 通过 |
| retriever_search | RetrieveRequest / Candidates | check_retrieval_wire_mapping.py | 通过 |
| composite_ingest_search_prepare_projection | CompositePrepareRequest / PrepareResult | check_index_wire_mapping.py | 通过 |
| composite_ingest_search_verify | ReadinessProof / Ack | check_index_wire_mapping.py | 通过；未执行Verify |
| composite_ingest_search_delete | IndexDeleteRequest / Ack | check_index_wire_mapping.py | 通过 |
| composite_ingest_search_search | 与CompositeEngine.Search相同的RetrieveRequest / Candidates | check_composite_search_wire.py | 通过；不代表引擎已拆成Retriever |
| reranker_rerank | RerankRequest / Evidence | check_retrieval_wire_mapping.py | 通过 |
| generator_generate | GenerateRequest / 五类AnswerEvent data | check_generation_wire_mapping.py | 通过；无SSE帧/模型 |
| control_status | 控制请求 / 六种状态与15类结果联合类型 | check_control_wire_mapping.py | 通过；无账本 |
| control_cancel | 控制请求 / cancel_requested或already_terminal | check_control_wire_mapping.py | 通过；未执行取消 |

通用[安全错误映射](ERROR_WIRE_MAPPING.md)另有15种code对照，不增加operation数量。[控制映射](CONTROL_WIRE_MAPPING.md)保存原/控制请求身份区别，复合search复用平台检索DTO，不改变[复合能力整组替换边界](COMPOSITE_CONTRACT.md)。二进制/SSE body不被伪装成JSON成功响应。

## 本次检查

Go全量包测试（关闭测试结果缓存）、go vet、87项Python契约回归以及8个实际Go映射脚本通过，见[执行证据](evidence/control-wire-review.json)。映射脚本拒绝部分非法身份/类型/范围/结果关系；未覆盖所有Schema值域/所有领域不变量，不把正例库存齐全当成完整契约测试完成。

## D-03仍需的运行证据

- 版本化HTTP客户端/服务端及真实multipart、二进制、SSE帧/EOF/终态行为，与进程内调用对照。
- 服务身份、受信Grant及规范请求摘要、当前权限/发布闸门、结果缓存重取与访问撤销。
- 原调用持久账本、幂等/结果未知、状态单调性、观察超时后的同目标探查、取消与确认停止。
- 请求/集合/字节/并发/总deadline、token计量、sink失败和传输关闭，不能由字段预算代替。
- 对原Request执行全量值域/来源/产物关系校验，真实解析、规格、过滤、Verify/删除与供应商适配。

其中对象正文授权和索引/数据迁移继续穿透真实后端。D-03目前从“字段映射缺失”推进到“逐操作局部数据证据齐全、真实协议未验”，**尚未关闭，也没有证明所有模块已可替换或Recall/响应速度达标。**
