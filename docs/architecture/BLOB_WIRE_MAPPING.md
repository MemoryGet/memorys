# Blob远程字段与二进制边界 V1

本规范补充[远程模块协议](REMOTE_MODULES.md)的三项Blob操作。Go映射位于`pkg/modulewire/blob.go`，二进制检查位于`blob_stream.go`。它们是DTO和完整性检查，尚无HTTP适配器、multipart处理器、当前授权/Grant或真实存储调用。

## 1. 字段与响应关联

| 操作 | Go端口字段 | Wire映射与约束 |
|---|---|---|
| put | BlobWriteRequest.Request / Scope | context，沿用精确UTC时间、主体/作用域和配置摘要；实际Grant单独验证 |
| put | ExpectedSHA256 / Size / MediaType / IdempotencyKey | multipart metadata的input.expected_sha256 / size / media_type / idempotency_key；Size为非负int64，不经double中转 |
| put | Put返回BlobRef | JSON result.id / sha256 / size / media_type；回显request/invocation ID匹配，哈希/大小/类型必须等于独立请求期望 |
| open / delete | BlobReadRequest.Request / Scope / Blob | context及input.blob，保持全部平台标识与摘要；未知字段拒绝 |
| open | io.ReadCloser | application/octet-stream；X-AKOS响应头回显IDs，正文是原始字节 |
| delete | error / nil | result.completed=true只代表本次删除RPC完成；false或关联ID不一致拒绝，不证明治理已允许物理删除 |

put字节放在独立file部分，不编码JSON数组、Base64或下载URL。metadata摘要绑定预期sha256/size，实际流须重新计算。服务用应用分配或受信模块分配的逻辑Blob ID；不能把对象名/URL当Blob ID返回。客户端和治理核对版本/空间/原件归属，不能由DTO转换器代替。

open/delete使用同一数据载荷，但Grant必须绑定不同method/path、动作、规范请求摘要与对象；不能用一次读Grant调用删除。调用幂等、结果未知、访问撤销和保留锁继续使用远程协议及[回收契约](RETENTION_CONTRACT.md)。

## 2. 二进制校验与取消

`ValidateBlobResponseHeaders`检查大小写无关的唯一request/invocation回显、octet-stream类型和可选Content-Length。重复回显、不同ID、错误长度或JSON类型拒绝；缺Content-Length允许块传输，但不省略正文校验。HTTP分帧、TLS、重定向与连接安全仍由真实传输适配器实施。

`ValidateBlobStream`只消费并检查输入流，64KiB缓冲、调用方总字节上限、声明长度加一个溢出哨兵，并要求实际EOF、长度及SHA-256一致。恰好收到声明字节但后续错误、额外字节、提前EOF、同长度篡改或持续无进展读取均失败；不能仅因读取长度已够就报告成功。读取的原始供应商错误不回显。

该函数在读前/读后检查context取消，**不关闭输入源，也不能中断任意阻塞Read**。实际HTTP适配器必须设置有界超时、让取消关闭响应Body并确认读取退出；上传服务也须中断请求体、隔离未完成staging。不能把局部context测试当成端到端取消通过。

校验函数不把字节交付业务或写存储。真正put先写隔离staging，校验完成后再原子确认Blob；任何失败不得发布残片。open若直接流给调用方，已传出的字节无法追回，终态必须等待完整性验证；消费者不能把中间字节当完整读取成功。应用对用户/Agent的每批次输出仍执行当前权限/撤回检查。

删除失败/超时记录结果未知及墓碑，通过调用账本探查和幂等重试恢复；不恢复公开访问，也不清除仍受迁移保护的关联。当前Ack编解码不会执行这些行为。

## 3. 验证证据与剩余门槛

```powershell
python docs/architecture/check_blob_wire_mapping.py
& '.tools/go/bin/go.exe' test -count=1 ./pkg/modulewire
```

Python脚本将put/open/delete三组样本分别送入实际Go类型映射，并在前后独立检查OpenAPI Schema，规范字节对照保持所有字段；样本size=9007199254740993验证整数不会经double丢失。它是metadata样本，不代表传输了这么大的文件。另拒绝上传结果大小/调用ID错配及未知URL字段。

Go试验覆盖DTO往返、上传期望、删除Ack、普通EOF和数据+EOF、截断/溢出/哈希篡改、底层错误/无进展、空文件、预算、取消前/读取中取消、回显/类型/长度与大小写拆分重复头。未执行multipart、HTTP/SSE传输、Grant/当前权限、幂等账本、阻塞读取取消、实际Put/Open/Delete或UI/MCP读取。

D-03的文件数据映射取得局部证据，生成流、控制和安全错误映射以及正式认证/网络协议验收仍未闭合，不能冻结完整远程协议。
