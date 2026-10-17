# 批准正文与原件读取的绑定

`VerifyApprovedOriginal` 将标准 FrozenContentReader、独立 OriginalReadControl 和 BlobStore 串联。先获取并验证当前批准正文的双摘要，再从冻结正文的 Blob 清单按平台 BlobID 选择完整 BlobRef；调用方不能另传摘要、大小或媒体类型覆盖批准记录。

OriginalReadControl 必须使用可信身份、文档归属和当前策略签发文件读取范围，不能将调用参数本身当作授权。核验器不自动改写 knowledge.search 权限：控制源返回独立读取请求后，核对主体、空间、知识库范围、请求身份、配置、期限和完整 BlobRef，再交给 BlobStore 执行实际读取。最后一次批准正文读取同时受最新原件读取授权期限限制，授权在核验结束前过期会失败。

实际原件长度、SHA-256和流终态核验完成后，再次解析当前原件读取权限，并重新获取批准正文验证。后续观察检查到撤权或批准失效时拒绝返回成功。该序列提供多个当前观察点，不是原件、元数据库和网络输出之间的原子屏障。

文件大小按 AKOS-CJSON 的精确数值解释，例如 `16e0` 是合法整数。只接受0至64 MiB的精确整数，并比较规范字节往返结果，拒绝被浮点转换舍入成整数的小数、缺失/null和超限值。

当前验证使用真实本地文件与测试批准正文/权限提供者，覆盖未知Blob、原件元数据/主体/空间/KB/期限替换、复用搜索范围、读取后撤权/批准失效以及精确数值边界。

这项绑定只证明原件来自批准正文的Blob清单且实际字节匹配，不证明它是某条Evidence所指的块、摘录确实来自该原件，或页码/表格/OCR位置正确。生产控制源、具体锚点/摘录核验、PostgreSQL联调及最终CurrentBackend/产品输出接入仍待完成。

本地证据：[绑定专项记录](evidence/approved-original-local-review.json)与[470项输入的统一本地回归](evidence/unified-local-checks.json)。未将测试控制源等同真实IAM，也未新增PG或最终输出运行证据。

2026-10-16后续状态：已由[PG当前证据后端](POSTGRES_EVIDENCE_BACKEND.md)完成真实数据库/原件的组合验证；此前本地阶段的记录保留为历史范围。最新475项输入、三版PG各183项及独立证据见[矩阵](POSTGRES_VERSION_MATRIX.md)。实际身份/控制为fixture，精确定位、其他格式和最终产品输出仍待验。
