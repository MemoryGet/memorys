# 上传并入队解析运行时

`api.UploadParseHandler` 是生产 HTTP 组合根可注入的上传边界。它先调用宿主提供的可信准备器，准备器负责认证、授权以及文档、草稿、候选和 Job 身份；multipart 表单只允许携带一个文件，不能提交这些绑定字段。

Handler 校验请求大小、截止时间和文件长度后调用 `ingestion.UploadParseService.UploadAndEnqueue`。该服务先写入 Blob，再把 BlobRef 放入 `DraftParseCommand` 并创建 queued parse task；任务创建失败会按原 Scope 补偿删除 Blob。响应返回 queued Job ID、文档/草稿 ID 和 Blob 摘要，解析由后台 Worker 完成，不让 HTTP 请求假装完成解析。

Bootstrap 通过 `Config.UploadParse` 显式挂载该能力；未装配时 `/api/documents/upload` 仍返回 503。这样主程序不会伪造身份、BlobStore、任务仓储或解析器，真实宿主可以选择基本原件上传或上传并入队解析模式。

PostgreSQL 本地运行时还提供 `localruntime.Search.NewParseWorker`：它把固定空间/知识库分区的 `BoundParseTaskQueue`、候选事务仓储、当前策略复核和宿主 `contracts.Parser` 组装成 `ingestion.ParseWorkerRuntime`。`memorys-server` 在显式设置 `MEMORYS_PARSE_ENABLED=1`、分区及 Worker 身份后，会使用 `parser.DispatchParser` 装配 Markdown、Office、PDF、OCR 和音频解析器，并先执行各解析器健康探测，再随服务器生命周期启动/停止。未设置这些变量时不会伪造后台 Worker。

需要跨分区消费时可额外设置 `MEMORYS_PARSE_ALL=1`，服务器使用全局有界队列；每条任务仍以自身保存的 space/KB Scope 做授权和候选提交，不能借 Worker 身份扩大权限。

发布准备任务也有独立的 `publicationassembly` 装配层。设置 `MEMORYS_PUBLICATION_ENABLED=1`、`MEMORYS_PUBLICATION_SPACE_ID`、`MEMORYS_PUBLICATION_KB_ID`、`MEMORYS_PUBLICATION_WORKER_ID` 和 Worker Bearer Token 后，服务器会把 PostgreSQL publication queue、租约任务、chunk splitter、实际索引准备和 completion 回执接入后台 `preparation.Service`；发布者原始 Principal 会在每次任务读取和完成前重新授权。
