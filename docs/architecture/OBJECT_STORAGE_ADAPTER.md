# SDK 无关对象存储适配器

`pkg/storage.ObjectBlobStore` 为对象存储提供最小的 `PutObject`、`GetObject`、`DeleteObject` 端口。S3、MinIO、Azure Blob 或其他厂商 SDK 只需在宿主层实现 `storage.ObjectClient`，`contracts.BlobStore` 和领域层不依赖厂商类型。

适配器沿用本地 BlobStore 的边界：请求必须有截止时间、当前 Scope、单知识库、有效媒体类型、大小上限和预期 SHA-256；每次读写删除都重新调用 `Authorize`。对象键由空间/知识库命名空间摘要和受限十六进制 Blob ID 组成，Prefix 拒绝绝对路径和路径穿越片段，不接受调用方提供的原始对象键。

写入使用请求中的幂等键和内容元数据派生稳定 Blob ID，客户端重试会指向同一个对象。上传完成后校验实际读取字节数和 SHA-256；长度或摘要不一致时尝试删除刚写入的对象并返回安全错误。读取流在 EOF 时再次校验长度和摘要，删除沿用相同授权和截止时间。

当前已覆盖内存假客户端的 Put/Open/Delete、重复写入、摘要不匹配、Prefix 穿越和授权拒绝。生产验收仍需由宿主注入真实 S3/MinIO/Azure 实现，验证条件请求、网络重试、超时、对象版本/删除语义、跨区域故障恢复和凭据轮换；SDK 适配器本身不在核心包中锁定。
