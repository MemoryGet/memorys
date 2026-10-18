# 上传读超时与取消的错误分类修复

后续诊断：[治理时钟前置条件](GOVERNANCE_CLOCK_DIAGNOSTICS.md)已实际捕获4至5秒墙钟步变；当前590项本地检查通过，但本轮完整PG因环境时钟异常失败，不代表此页业务守卫应放宽，也不以旧通过快照代替当前验收。审批/续租稳定性与完整V1保持开放。

2026-10-18：修复ReceiveHTTPBlobUpload/ReceiveBlobMultipart在实际读超时先于应用context定时回调时，将deadline误报为context.Canceled的一类竞态。接收与暂存契约见[BLOB_MULTIPART_TRANSPORT](BLOB_MULTIPART_TRANSPORT.md)。本轮没有增加上传期限、放宽文件验证或改变BlobRef提交边界。

## 复现及根因

此前全量回归中的静默上传测试曾返回context canceled。单独重复原场景10次没有复现，不能据此关闭问题。本轮构造固定触发顺序的真实localhost HTTP/1.1测试：保留服务端socket读取deadline，固定观察“读取超时先于应用deadline回调”的顺序；客户端发送部分chunked multipart后保持连接，不发送下一块或终止块。

修复前连续3次失败，均记录到实际Body.Read的i/o timeout、HTTP请求父context已取消、有效deadline已到且暂存Abort次数为1。它与“没有打断读取”或“暂存未清理”不同，是已经有界终止后的错误分类丢失。测试中的deadline context只用于固定这一观察顺序，不是新的生产context实现。

按可能性依次核查：读取返回后的context错误覆盖底层超时；另一个读取期限提前触发；清理阶段改写错误。实际读错误及期限记录排除了提前期限，清理逻辑只会在失败上追加安全ErrCleanup，未将错误改为Canceled。当前Go的net/http在底层Read出错后会取消请求context；它可能早于应用deadline回调到达子context。copyUpload原先读后先返回ctx.Err()，从而丢失真正的网络超时原因。

见[修复前三次失败](evidence/upload-deadline-before-fix.json)。记录包含当时输入摘要及实际错误，不以随后通过的运行覆盖该证据。

## 最终行为

新增内部uploadReadFailure统一处理读取失败：只有实际错误链符合net.Error.Timeout且对应有效deadline已到，才优先返回context.DeadlineExceeded；其他情况沿用既有context或安全协议错误分类。观察到取消时deadline仍有效，则保持context.Canceled，不因为用于打断读取而设置的socket deadline也返回timeout就改报deadline。

这项判断应用于metadata部分、file部分头部、文件复制、额外部分及HTTP尾部读取。它不返回底层网络地址或原始I/O错误详情，不改变长度/摘要/字节预算、权限回调、Body所有权、关闭与Abort流程，也没有新增生产goroutine或延长截止时间。写入及清理错误的原有分类未被统一改为deadline。

| 实际观察 | 返回行为 |
|---|---|
| 网络读取超时且有效deadline已到，即使父context先显示Canceled | DeadlineExceeded |
| 主动取消，读取失败时有效deadline仍未到 | Canceled |
| deadline读取失败且Abort报错 | DeadlineExceeded及安全ErrCleanup，空receipt |
| metadata尚未交给暂存时读取超时 | DeadlineExceeded，空receipt，不虚构Abort |
| 文件或multipart结束后的HTTP尾部读取超时 | DeadlineExceeded，清理已交付暂存，空receipt |

取消信号发出时间并不在标准Context中形成可查询时间戳；本实现以读取错误和观察时的有效deadline分类，没有声称为所有并发取消/超时建立全局先后顺序。

## 回归与证据

真实HTTP六个场景各10次，共60/60通过：原始客户端不主动断开的上传、固定socket先触发的次序、期限内提前取消、deadline叠加Abort失败、metadata读取及epilogue读取。回归核对预期错误与相反错误不同时成立、清理计数、安全错误无SECRET fixture文本，以及整个ValidatedBlobUpload均为空。

新增metadata负向测试不打开生产暂存，测试自身提前创建的临时文件须由测试负责关闭；扩展时发现Windows拒绝删除仍打开的fixture文件，已补上测试端句柄清理。之前遗留的三个空文件已按实际路径逐个核验并非递归删除。自动审批随后拒绝删除剩余临时目录结构，仅返回“blocked by policy”，所以这些目录结构仍保留，见[清理记录](evidence/upload-deadline-fixture-cleanup.json)。本记录没有宣称所有Windows临时目录都已移除。

当前586项输入下，Go全量测试/go vet、228项Python和三份OpenAPI通过；PG16保持326项检查及八组模块装配、worker身份、队列、清理、任务、删除、历史/当前读取独立复核通过，所有当前报告对应同一未变化源码清单。PG临时实例目录和55432端口另外确认已清理。本轮未重验PG15/18。

见[60次运行及输入摘要](evidence/upload-deadline-runtime-review.json)、[本地全量回归](evidence/upload-deadline-local-checks.json)、[PG16](evidence/upload-deadline-postgres16-review.json)、[模块装配](evidence/upload-deadline-registry-review.json)、[worker身份](evidence/upload-deadline-worker-review.json)、[队列](evidence/upload-deadline-queue-review.json)、[清理](evidence/upload-deadline-cleanup-runtime-review.json)、[任务](evidence/upload-deadline-job-status-review.json)、[删除](evidence/upload-deadline-deletion-review.json)、[历史](evidence/upload-deadline-history-read-review.json)、[当前读取](evidence/upload-deadline-current-read-review.json)及[PG资源检查](evidence/upload-deadline-pg-cleanup-review.json)。

复用核验脚本check_upload_deadline_runtime.py检查保存的修复前失败，然后实际运行当前六项HTTP测试各10次，记录每项Go JSON事件、结果及源码清单。以后其他模块的正常变化仅记录为相对复现快照的差异，不强迫整个代码库停留在旧快照；运行期间源码变化仍明确失败。

## 防复发与尚未完成项

读取失败不应在保留具体I/O原因之前被一个并发context状态覆盖；接收器现在在读取边界集中分类。固定触发顺序的网络回归补上了依赖偶然定时器竞赛的测试盲区，不依赖“多跑几次刚好通过”判断修复。

该结论限于已复现的HTTP/1.1读超时分类，不是所有上传稳定性、HTTP/2、TLS、生产Blob服务、真实Grant、吞吐或持久提交验收。审批与发布任务续租的间歇失败继续保留并待定位，不能因本轮通过而关闭。生产worker/服务装配、制品/数据集证明、远程发送/写入屏障、任务对账/重试/Purge、效果/性能与完整V1仍未验收。
