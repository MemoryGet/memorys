# 有界multipart上传与隔离暂存 V1

后续增量：[上传读超时与取消分类](UPLOAD_DEADLINE_RUNTIME.md)已有修复前3次确定性复现及修复后60次真实HTTP回归；当前586项输入/PG16 326检查和本页既有回归通过。它只关闭已复现的读错误分类原因，审批/续租和其他稳定性与完整V1保持开放；临时目录清理限制见该文档。

本规范补充[文件远程映射](BLOB_WIRE_MAPPING.md)。`pkg/moduletransport/blob_multipart.go`提供公共接收辅助器，返回已验证的上传receipt；不生成BlobRef，也不提交元数据或发布知识版本。

## 1. 接收与暂存契约

`ReceiveBlobMultipart`依次读取metadata JSON和file二进制部分。调用者提供有限deadline、metadata/file/完整请求字节上限、清理超时，以及当前访问检查与暂存工厂。metadata经modulewire解码，拒绝身份、期限和声明长度不合法的请求；通过首次访问回调后才打开暂存。

文件按不超过64KiB的块写入隔离暂存，同时核对长度和SHA-256。文件名不参与路径选择。只接受metadata在前、file在后的两部分；拒绝额外部分、错误媒体类型、传输编码、截断、超限和摘要不符。使用标准库NextRawPart，避免quoted-printable透明解码。完整请求预算覆盖multipart包络及尾部；MIME头还受Go标准库内部限制，并未另设可配置的单头预算。

读取至正常EOF后再次调用访问回调，关闭暂存成功且deadline仍有效才返回receipt。回调收到metadata副本；结果和暂存参数从保留的原字节重新解码，避免回调修改共享切片后改变后续输入。访问回调目前是接口，真实服务必须执行身份、Grant、当前权限及发布范围检查。

`UploadStaging.Close`只能刷新并关闭隔离资源，不能激活文件绑定。`Abort`必须幂等并遵守context。失败时关闭暂存，再用独立有限清理context执行Abort；清理失败返回安全ErrCleanup，receipt为空，调用方须记录孤立资源并安排恢复。工厂若返回错误，应自行清理其尚未交付的部分资源。

## 2. 服务器读取与取消

实际net/http服务使用`ReceiveHTTPBlobUpload`，通过ResponseController安装请求读取deadline；不支持该能力的ResponseWriter明确拒绝，包装器须支持Unwrap。调用context或metadata期限结束时先把读取deadline设为当前时间，再关闭请求Body。仅关闭服务器Body无法保证打断阻塞读取。

一般接收器适用于可中断的读取源；有限context不会强制终止任意自定义Read。暂存Write/Close/Abort及访问回调必须支持取消或有界返回。已过期的连接读取deadline不能靠事后清除恢复。接收辅助器拥有并关闭Body；不等同后台业务停止确认。

## 3. 验证证据与后续门槛

新增7项Go测试覆盖5MiB和空文件、64KiB写块、不可信文件名、错误顺序/编码/预算、长度/哈希/多余部分拒绝、前后模拟访问拒绝、Close/Abort失败安全返回，以及真实localhost HTTP成功、客户端中断和客户端不主动断开时的服务器超时。失败暂存删除以真实临时文件验证；失败时无成功receipt。

见[执行证据](evidence/blob-multipart-review.json)。该证据不证明生产认证、真实授权/Grant、持久调用账本、存储适配器、原子BlobRef确认、孤立资源恢复、知识发布、TLS或吞吐表现。D-03仍开放；后续应将验证receipt接入事务提交与恢复流程，再验证权限变化和提交失败的完整服务行为。
