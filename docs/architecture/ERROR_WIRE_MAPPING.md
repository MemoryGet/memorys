# 远程错误映射与重试边界 V1

本规范补充[远程模块协议](REMOTE_MODULES.md)的RemoteError。Go实现为`pkg/modulewire/errors.go`，使用平台PublicError；没有供应商自动分类器、调用账本、HTTP处理器或重试执行器。

## 1. 封闭错误码与安全文案

编码/解码都检查外层request_id、invocation_id和内层error.request_id关联。只接受[伙伴Error Schema](openapi/partner.v1.json)已有15个错误码；未知码明确拒绝，不当成成功或自动扩展字段。PublicError与RemoteError均拒绝cause、stack、连接串、原始响应、供应商ID等未知字段。

输入SafeMessage即使字段名声明safe也不直接信任。平台投影丢弃它，按code选择固定安全文案：

| code | 默认安全文案 | 是否允许保留上游retryable=true |
|---|---|---|
| invalid_argument | Invalid request | 否 |
| validation_failed | Validation failed | 否 |
| unsupported_format | Unsupported format | 否 |
| unauthenticated | Authentication required | 否 |
| forbidden | Access denied | 否 |
| not_found | Resource unavailable | 否 |
| conflict | Request conflict | 是 |
| publication_changed | Publication changed | 是 |
| cursor_expired | Cursor expired | 是 |
| capability_missing | Capability unavailable | 否 |
| contract_incompatible | Contract incompatible | 否 |
| rate_limited | Rate limit exceeded | 是 |
| deadline_exceeded | Deadline exceeded | 是 |
| dependency_unavailable | Dependency unavailable | 是 |
| internal | Internal failure | 否 |

文案不包含资源名、路径、SQL、凭据或原始异常；本地化由平台按code从受控模板提供，不拼接供应商正文。原始诊断只进入单独授权、脱敏和保留受控的诊断通道，不能挂在PublicError上。request/invocation ID由平台可信追踪上下文提供，不把Token或供应商URL当作ID。

上游retryable=false不会被升级为true。投影不证明code分类正确、错误来源可信或访问检查通过。跨空间资源存在性仍需应用访问闸门使用一致的not_found策略；仅替换报错文案不能消除forbidden/not_found或状态码的行为侧信道。

## 2. 重试、未知写入与取消

retryable是建议，实际行动必须同时检查阶段、剩余预算、原调用摘要/deadline、当前权限及幂等/结果未知状态。conflict、publication_changed、cursor_expired通常需要重新读取权威事实并创建新的显式业务请求，不能原样无限重试旧命令。

网络超时、断开、反向代理错误或无合法关联包络时，写操作可能已经提交；不据503/504或本地异常断定未执行。调用适配器保留uncertain状态，使用受信status账本探查并按原幂等键恢复。收到合法RemoteError也不自动覆盖已经确认的终态/业务事实；调用者仍核对目标invocation和持久记录。

context取消由传输关闭和control取消状态处理，不因为错误码目录缺少cancelled而伪装成一次成功或发布回滚。deadline_exceeded表示预算到期，不能仅因用户取消就误分类。当前投影函数不接收原始error，也不自动映射context/供应商异常；适配器分类器尚待实现并逐类核验。

流已经开始时，二进制不得追加错误JSON；SSE使用合法error终态且仍受当前授权/撤回检查。终态丢失、未知结果、取消后已提交与正常失败分开处理，不能把一个SafeMessage作为事务证据。

## 3. 机器验证与剩余缺口

```powershell
python docs/architecture/check_error_wire_mapping.py
& '.tools/go/bin/go.exe' test -count=1 ./pkg/modulewire
```

Python从实际OpenAPI枚举取得15种code，送入实际Go编解码，在前后独立验证Schema与关联；确认原始SECRET文本消失，永久错误不保留重试建议，两个固定模板与4类code/cause/ID非法样本对照。Go测试覆盖全部15码、入站与出站不信任原文、未知字段/码和内外关联拒绝。

这是安全投影与数据契约证据，不是供应商分类、授权、幂等账本、HTTP状态/重试策略或网络故障试验。生成/控制映射及完整远程协议仍待补齐，D-03保持开放。
