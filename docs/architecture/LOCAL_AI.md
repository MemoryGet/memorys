# 本地多格式、语义检索与 DeepSeek 问答

本轮扩展已有 `memorys-local`。数据库继续使用 PostgreSQL 18.6；本机 Ollama 提供嵌入，
DeepSeek 提供问答。命令行扩展不新增数据库或公开 HTTP 接口；另有 [本地网页入口](LOCAL_WEB.md)。首次使用先完成
[本地初始化](LOCAL_CLI.md)，在仓库根目录运行以下命令。

## 启用

```powershell
# 本机需要正在运行的 Ollama，以及固定版本的文档转换环境。
ollama pull embeddinggemma
& .local/markitdown-env/Scripts/python.exe tools/parsers/markitdown_worker.py --health
& .local/bin/memorys-local.exe enable-ai
& .local/bin/memorys-local.exe doctor
```

默认嵌入模型 `embeddinggemma:latest`；启用时记录实际模型摘要和向量维度，以后每次使用都检查摘要。
也可以在首次启用前设置 `MEMORYS_EMBEDDING_MODEL`（例如 `bge-m3:latest`）。已经启用的安装拒绝
直接替换模型；恢复原模型或单独规划模型迁移。Ollama 地址 `MEMORYS_OLLAMA_URL` 默认
`http://127.0.0.1:11434`，只接受本机 IPv4 回环 HTTP 地址。

转换环境使用 MarkItDown 0.1.3。新机器须安装 `docs/architecture/markitdown-requirements.lock.txt`
指定的依赖；支持 `MEMORYS_PARSER_PYTHON`、`MEMORYS_PARSER_WORKER` 覆盖解析进程路径。
默认路径相对启动工作目录（仓库根目录）；状态和原件路径相对 `--state` 目录。
迁移解析环境时重新创建虚拟环境，不直接复制虚拟环境中的绝对路径。

`enable-ai` 是显式权限升级：封存 ACL 版本 2，增加所有者的解析、原件读写和问答权限。
旧 ACL 和原生关键词 Profile 保留，worker 仍然只有索引准备权限。
数据库提交而本地文件未更新的中断可用相同命令恢复；未知政策、撤权和配置冲突会拒绝。
启用后须重启原来运行的 `serve`，使它加载原件核验能力。

## 文件导入与发布

```powershell
$draft = & .local/bin/memorys-local.exe import 'E:\资料\手册.docx' | ConvertFrom-Json
$draft | Format-List
# 核对 preview 和 content_hash 后明确批准：
& .local/bin/memorys-local.exe publish $draft.draft_id --expect-hash $draft.content_hash
& .local/bin/memorys-local.exe serve
```

增加普通文本 PDF、DOCX、XLSX、PPTX，原件最多 8 MiB，转换正文最多 256 KiB。
扫描 PDF/OCR、加密文档、宏执行、旧版 DOC/XLS/PPT、图片理解和精确页码/单元格引用不在本轮范围。
Office 转成文本后保留实际原件及 `original/coarse` 引用；不改写成 revision 来跳过原件检查。
TXT/Markdown 继续按正文、`revision` 来源发布。转换器禁止联网并过滤继承的模型密钥、数据库密码等环境变量。

相同原件、安装和固定转换版本复用文档；原件改变创建新文档。导入只有草稿，必须通过摘要绑定的
`publish` 才能检索。发布前独立重新核对原件摘要和转换正文；查询时重新核对已批准原件、字节和摘录。

## 语义检索与问答

```powershell
# 无须 serve；直接读取当前获权且已发布的原生投影，再在本机排序。
& .local/bin/memorys-local.exe semantic-search '遇到数据库故障应该怎样恢复？'

# 密钥只从当前进程环境读取，不写到 runtime 或仓库。
$chatKey = Read-Host 'DeepSeek API key' -AsSecureString
$env:DEEPSEEK_API_KEY = [System.Net.NetworkCredential]::new('', $chatKey).Password
& .local/bin/memorys-local.exe ask '遇到数据库故障应该怎样恢复？'
Remove-Item Env:DEEPSEEK_API_KEY
```

默认聊天模型 `deepseek-flash`，`MEMORYS_CHAT_MODEL` 也接受 `deepseek-v4-pro`。
用户选择的模式会把问题及当前获权的文档片段发送到 DeepSeek 官方接口；本地原件、私钥和数据库凭据
不会作为模型输入。最多发送 4 个片段、总计 24 KiB，最多生成 1024 token、单次请求不超过 30 秒。
不执行工具、不自动重试、不输出未复核的流式内容。

`semantic-search` 为本地 `semantic_scan`：最多扫描 512 片、512 KiB 已发布正文；使用真实本地向量、
余弦排序，受保护的 `vectors.json` 缓存可重建。它保留原生关键词发布代际，另有答案配置摘要，
不声明已经发布向量索引。超过界限会拒绝，不静默漏掉语料；当前适合小型个人知识库。
模型上下文不足时拒绝，不静默截断。没有加入 pgvector 或额外向量数据库。

问答在嵌入前、生成前后重新认证并核对当前权限、有效批准、发布索引和原件。
`knowledge.answer` 和 `original.read` 各自独立授权。生成中撤权、原件损坏、来源变化会阻止输出。
模型只能引用传入的来源，未知引用、未标注引用和超预算响应拒绝。引用检查保证来源绑定，
不等同于证明模型每句话都正确。无候选或余弦分数低于 0.25 时返回证据不足；该阈值未经质量标定。

## 备份与验证

备份仍是 PostgreSQL 加完整 `.local/runtime/`；该目录现在还包含 `blobs/` 原件、嵌入摘要及
可重建的向量缓存。遗漏原件会导致相应检索失败。数据库恢复到隔离空库后复制匹配目录并运行
`init`、`doctor`，按原摘要恢复模型，重新配置解析器及当前进程 DeepSeek 密钥。
`doctor` 只读报告解析器、本地模型摘要和密钥是否配置，不调用远程生成、不打印密钥。

```powershell
python docs/architecture/check_local_cli_windows.py --ai --pg-bin 'D:\Program Files\PostgreSQL\18\bin' --report .local/architecture-check/local-ai-postgres18-windows.json
python docs/architecture/check_architecture.py
```

`--ai` 仅在拥有的临时集群使用合成文档进行真实转换、发布、HTTP 引用、本地嵌入和 DeepSeek 调用；
另用受控提供者验证嵌入/生成期间撤权、伪造引用、独立原件权限及升级中断恢复。
恢复测试包括数据库、原件、状态目录和向量缓存迁移。报告不含正文、凭据或私钥。
这些属于本地功能验证，`production_acceptance=false`；中文复杂版式、规模/延迟、Recall@10、
模型事实准确率需要后续独立验收；网页已另见 [本地网页验收](LOCAL_WEB.md)。

协议参考：[Ollama embed](https://docs.ollama.com/api/embed)、
[EmbeddingGemma](https://ollama.com/library/embeddinggemma)、
[DeepSeek Chat Completions](https://api-docs.deepseek.com/api/create-chat-completion/)。
