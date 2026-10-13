# 生成请求、回答事件与顺序契约 V1

本规范补充[远程模块协议](REMOTE_MODULES.md)的generator/generate。Go映射为`generation.go`，顺序检查为`answer_sequence.go`；当前没有模型调用、HTTP/SSE帧解析器、当前权限闸门、token计量或传输取消实现。

## 1. 请求字段与预算

| Go字段 | Wire字段 | 约束 |
|---|---|---|
| GenerateRequest.Request / Scope | context | 与其他模块相同的请求/调用/模块身份、配置摘要、精确UTC时间和范围；DTO不是Grant |
| Question | input.question | 非空，保留原始字符与换行，不将材料内容变成系统权限 |
| Evidence | input.evidence | 1–1000项标准Evidence，保留引用、摘录与rank；调用前须由应用验证当前发布/权限/来源 |
| Budget.Deadline | input.budget.deadline | 不晚于context.deadline；context.deadline不晚于Scope到期 |
| MaxOutputTokens / MaxExtraRounds | budget.max_output_tokens / max_extra_rounds | 1–32768 / 0–2；不能新增总预算或允许模型自行调用检索/发布工具 |

证据不足时由应用有限反思或直接返回insufficient_evidence，不向远程生成端发送空证据伪装成已检索。协议字段上下界检查不证明模型实际遵守输出token或30秒总预算，生产适配器仍须计量与停止。

## 2. 五类事件与结果字段

Go AnswerEvent为进程内联合类型，wire为AnswerEvent oneOf，编码/解码都拒绝混合载荷及未知字段。

| Kind / type | Wire载荷 | Go映射 |
|---|---|---|
| meta | request_id | Native无单独meta ID字段；编码从受信请求上下文补入，解码核对后返回Kind=meta |
| answer_delta | text | Text；允许空增量但仍计入事件上限 |
| citation | citation | Citation指针；必须来自本请求的独立证据白名单 |
| done | result | AnswerResult指针，字段完整且Trace对应独立配置版本 |
| error | error | PublicError指针，使用[封闭安全错误投影](ERROR_WIRE_MAPPING.md)；不能当成成功结果 |

AnswerResult映射request_id/answer/citations/status/profile_version/profile_digest/timings/degraded。TimingsMS限定map[string]float64并无损JSON往返，拒绝负数和非有限值；退化reason只允许unavailable/deadline/policy_disabled。profile_version使用uint64，不能经double中转。实际耗时/退化来源必须是受控观测和模块事实，不把LLM自报指标当性能证据。

event-name与JSON data.type必须一致、HTTP响应回显本次request/invocation ID，这些由后续SSE适配器执行。当前函数只处理data JSON，不读取SSE帧、不解析注释/多行data/网络EOF；单帧参考编解码上限4MiB不是总流预算。

## 3. 流顺序、引用与唯一终态

`AnswerSequence`构造时从应用取得request ID、不可变Profile版本/摘要和合法citation列表，深复制来源锚点，避免调用方随后改指针扩大白名单。它串行消费一个流，不支持并发调用。

顺序固定：meta恰好一次且在前，之后answer_delta/citation，最后唯一done或error。缺meta、第二个meta、未知type、终态后的事件或断开而无终态均失败；失败后不能继续追加另一份回答补救。Finish只有done成功，error返回安全PublicError，不把失败终态当完成回答。

引用与白名单做全部平台ID和来源坐标的精确匹配。不能添加父块、扩大范围或用供应商ID构造引用；更窄引用若需支持，应由平台先验证并加入合法证据集合。发出的citation事件必须在done.citations中保留，最终引用也不能超出白名单。

有文本增量时，done.answer必须与增量拼接完全一致；仅在done返回正文也受相同字节上限。answered需要非空答案和至少一条合法引用；insufficient_evidence可以没有引用。该规则不证明正文陈述被证据支持或答案正确，仍须应用证据支持检查和封存集评测。

应用从部署策略提供MaxEvents与MaxTextBytes，显式控制整流事件和UTF-8文本字节。该上限不是token上限的替代计算，不能根据字符数伪称已经计量tokens。真实端到端循环还须使用同一个剩余deadline，在每批输出前复核当前权限/发布状态，处理sink错误并关闭/取消远程读取。出现增量后不自动重试整次生成。

模型不能直接审批/发布草稿，也不能从资料里的指令取得额外工具权限。当前port只有回答回调，没有发布接口；真实模型/Agent适配器仍须确认未偷偷启用扩权工具。

## 4. 当前对照与未验证项

```powershell
python docs/architecture/check_generation_wire_mapping.py
& '.tools/go/bin/go.exe' test -count=1 ./pkg/modulewire
```

实际Go对照覆盖请求、五类事件和answered/insufficient/error三种样本；独立Schema在前后核验，profile_version大于2^53仍无损。7类变异拒绝超出deadline、缺meta/终态、重复终态、引用越界、配置版本失配和正文矛盾。

Go规则测试另覆盖错误终态不成功、安全错误模板、非法联合载荷、meta关联、白名单深复制和输出字节上限。检查输入是事件data数组，不是真实SSE网络；检查命令的stream_outcome=error只代表有效失败流被识别，不代表模型调用成功。

模型执行、SSE帧/网络边界、授权/撤回、实时总预算、token计量、sink错误取消和调用账本仍待实现验证。control映射和完整远程协议验收仍未闭合，D-03保持开放。
