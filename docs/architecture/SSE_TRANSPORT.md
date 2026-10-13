# 有界SSE接收与本地HTTP验证 V1

本规范将[生成事件映射](GENERATION_WIRE_MAPPING.md)推进到实际响应Body读取，实现在`pkg/moduletransport/answer_stream.go`。这是可替换HTTP适配器的公共接收辅助器，不是完整客户端/服务器、认证器、权限服务或模型实现。

## 1. 标准基础和平台约束

依据[WHATWG事件流解析规范](https://html.spec.whatwg.org/multipage/server-sent-events.html#parsing-an-event-stream)，处理UTF-8、首个BOM、LF/CRLF/CR、冒号注释、逐字段解析和多行data；空行结束事件，未结束的末尾data不派发。字段名区分大小写。该标准基础不等于启用浏览器EventSource的连接恢复行为。

AI Knowledge OS采用有限调用的明确规则：必须有200及text/event-stream、唯一request/invocation回显，event名必须与data.type一致。缺省message类型不符合五类事件契约。非法UTF-8直接拒绝；未完成业务帧使整个读取失败，而非把残片当作一个补充事件。

id/retry/未知SSE字段按解析规则忽略，不触发重连、Last-Event-ID恢复或更换调用。meta在前、引用/增量在中、唯一done/error在后；只有终态且响应Body正常EOF才完成接收。收到done后继续读取至有限响应结束，以拒绝多余事件；服务器必须结束这次响应体，不依赖TCP连接关闭。

done/error先保留在接收器内，直到正常EOF且顺序验证通过，再重新执行访问闸门并输出终态。这样后续截断、网络错误或矛盾事件不会让sink先收到成功done再得知失败；增量正文仍属于已说明的在途边界。

这些是平台V1语义，比通用长连接事件消费更严格。出现文本后不自动重试生成；新问题或结果未知恢复遵循原调用账本规则。

## 2. 明确资源预算

| 预算 | 计量与拒绝规则 |
|---|---|
| MaxLineBytes | 单行UTF-8字节，不含终止符；长注释/未知字段也受限 |
| MaxFrameBytes | 各行原始UTF-8字节＋每行一个规范LF，包括注释和忽略字段；CRLF不算两次 |
| MaxWireBytes | 真实响应字节，含BOM/注释/实际换行；最多读取预算加一个溢出哨兵 |
| Answer.MaxEvents / MaxTextBytes | 复用AnswerSequence整流事件计数与UTF-8答案上限，防止空增量无限放大 |
| context deadline | 强制有限deadline，实际调用必须取客户/服务端/模块/Scope及剩余预算的最小值 |

单帧最多4MiB以匹配当前data编解码器。预算由可信部署/能力协商后传入，不由远程回复或材料指令扩大。字节上限不代表实际token计量；模型token和费用仍须独立适配器实现。

## 3. 访问闸门、sink与关闭

接收函数强制非空当前访问闸门，在每一业务事件输出前调用，随后重新检查取消再交给sink。闸门应从应用闭包核对原证据集合、主体、当前授权/发布版本，而非只检查当前text事件中是否带引用。拒绝后不再输出该批次；先前已传出的字节遵循原在途边界，无法追回。

Body由接收函数接管，任何成功/错误/取消路径都关闭，使用一次关闭守卫协调deadline回调与返回清理。context取消关闭Body，使标准net/http中的阻塞读取退出；sink返回错误也关闭Body，不在原流拼接另一份答案。传输/闸门/sink错误使用安全类别，error业务终态返回已投影的PublicError，不当成成功。

任意自定义ReadCloser的Close未必能中断Read，任意同步闸门或sink也无法被此函数强行抢占。生产实现须采用可取消网络Body及有界、能响应context的回调；不能因有deadline字段就承诺任意第三方实现都可强制停止。

该函数不发起网络请求、不读取服务凭据、不验Grant、不决定模块地址、不确认后台任务停止，也不使某个知识版本发布。真正HTTP客户端须先校验受控地址/制品/服务身份及输入Grant，真实当前访问服务仍待接入。

## 4. 已执行证据

```powershell
& '.tools/go/bin/go.exe' test -count=1 -v ./pkg/moduletransport
```

八项测试覆盖换行/BOM/注释/多行JSON、截断与事件名错配、终态后事件、非法UTF-8、单行/帧/整流预算、回显/编码/状态、必需deadline/闸门、安全错误终态，以及合法done后迟到的网络错误/终态前访问拒绝。闸门/sink采用测试回调，不是生产授权证明。

其中三项包含真实localhost net/http测试：分块/Flush边界、第二批次模拟撤权拒绝、阻塞读取取消、sink失败关闭服务器请求、静默服务器deadline退出。它们证明接收端和本地网络取消行为，不证明控制账本、模型后台停止、TLS/服务身份/Grant或第三方云网络。

测试报告见[执行证据](evidence/sse-transport-review.json)。生产HTTP/multipart/Blob调用、当前权限、账本与真实模型/组件、token/性能评测仍未完成，D-03及完整架构验收继续开放。
