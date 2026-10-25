# 本地网页工作台

`memorys-local web` 将已有本地命令行能力接入浏览器：文档列表、文件导入、全文预览、
摘要绑定的明确发布、关键词/语义检索、带引用的 DeepSeek 问答和只读诊断。
界面使用当前数据库状态，已移除 dashboard 原先的示例文档、任务、健康分数和模拟答案。

## 启动

先完成 [本地初始化](LOCAL_CLI.md)；PDF/Office、语义检索和问答按 [AI 配置](LOCAL_AI.md) 启用。
在仓库根目录构建一次网页和程序：

```powershell
npm --prefix dashboard ci
npm --prefix dashboard run build
& .tools/go/bin/go.exe build -o .local/bin/memorys-local.exe ./cmd/memorys-local

# 数据库连接、PGPASSWORD/pgpass、DeepSeek 密钥仍从当前进程环境取得。
$env:MEMORYS_DATABASE_URL = 'postgres://postgres@127.0.0.1:5432/memorys?sslmode=disable'
& .local/bin/memorys-local.exe web
```

打开 `http://127.0.0.1:8080/local/`。如果同一端口已有 `serve`，先在它的终端按 Ctrl+C，
或给 `web` 指定 `--address 127.0.0.1:8082`，并打开对应端口。
停止当前终端即可停止网页服务。网页运行时只需 Go 可执行文件和构建好的静态资源，
不需要再开 Vite 或 Node 服务。

本机已经构建、初始化后，可从任意目录运行启动脚本：

```powershell
& E:\ai\memorys\tools\start-local-web.ps1
# 端口被占用时：加 -Address 127.0.0.1:8082
# 需要临时输入模型密钥时：加 -AskDeepSeekKey
```

脚本自动定位仓库、状态和网页资源，优先复用进程凭据或 pgpass，否则交互式输入数据库密码。
密码和临时模型密钥只用于当前进程，停止后恢复原环境，不写文件。`-CheckOnly` 仅检查本地启动文件，
不连接数据库/模型、不索取密码，也不证明服务已启动。脚本在前台运行，按 Ctrl+C 停止。

可指定 `--state <状态目录>`、`--ui-dir <dashboard/dist目录>`。移动后这两个目录均可重新定位；
默认解析器路径仍相对仓库根目录，搬迁配置方法见 AI 说明。网页构建目录不包含本地私钥或数据库凭据。

## 操作

1. 在“我的知识库”导入文件。每次一个，支持 TXT、Markdown、PDF、DOCX、XLSX、PPTX。
2. 预览完整解析正文，展开文档标识和 SHA-256 摘要。草稿尚未公开检索。
3. 勾选“我已核对全文，批准发布这份内容”，再点击“批准并发布”。服务端重新核对摘要，
   沿用提交、个人审批、索引准备和发布流程；重复上传已发布文件复用原文档。
4. 在“检索与问答”选择关键词、语义或问答。答案引用可跳转到对应来源，展开核对文档、版本和内容块。
5. “运行状态”读取数据库、身份、权限、解析器、本地模型和密钥是否配置；不会自动跑验收套件或调用远程模型。

文件大小、扫描件/OCR、正文级引用、语义扫描规模和模型预算沿用 AI 版本边界。
问答页面明确提示会将问题及当前获权片段发给 DeepSeek；界面不接收或保存 API 密钥。
文档内容始终作为纯文本显示，不执行其中的 HTML、脚本或 Markdown 链接。
列表仅显示前 100 份文档并提示截断，其他已发布文档仍参与受限语料范围内的真实检索。

## 本机访问边界

`web` 是由个人所有者显式启动的本机控制入口，会加载受保护私钥以在每次请求内签发短期身份，
仍向真实数据库重新认证和授权。原 `serve` 继续只读取公钥，不具有网页审批能力。
网页只绑定 `127.0.0.1`，固定校验 Host；会话 Cookie 为 HttpOnly、SameSite=Strict，
重启后失效，两小时后需重新打开网页。写操作另需精确 Origin 与自定义请求头，跨站调用拒绝。
页面禁止嵌入框架、只加载本地静态脚本，不开放 CORS，也不提供读取任意本地路径的接口。

这是当前用户独占电脑上的个人控制台，没有提供多人登录或不同 Windows 用户之间的网页身份隔离。
不要通过反向代理或隧道公开该控制入口。完整企业管理端、公开管理 API、生产 IAM 和精细角色仍未交付。
这些局部网页路由不增加公共 OpenAPI 契约。

## 备份与复现

日常迁移可使用 [离线备份与恢复工具](LOCAL_BACKUP.md)，自动配对数据库、完整状态和网页程序。

备份数据库和匹配的整个 `.local/runtime/`，包括原件与私钥。另保留源码/锁文件或构建好的
`dashboard/dist/`。恢复后重新配置模型、解析器和进程凭据，运行 `init` 核对绑定，再启动 `web`。

```powershell
# 需要可用的 Playwright 安装和本机 Edge；路径指向实际 playwright 包目录。
$env:MEMORYS_PLAYWRIGHT_MODULE = '<playwright包的绝对路径>'
python docs/architecture/check_local_web_windows.py --pg-bin 'D:\Program Files\PostgreSQL\18\bin'
python docs/architecture/check_architecture.py
```

独立网页运行器只使用自有临时 PostgreSQL 集群（默认 55441）及合成文件，验证真实浏览器操作、
独立原件引用、本地嵌入、DeepSeek 回答、跨站拒绝、摘要错误拒绝、禁用身份拒绝，以及数据库/原件/状态
恢复后的 HTTP 和语义检索。临时进程和目录均清理。报告为
`.local/architecture-check/local-web-postgres18-windows.json`；合成数据截图在
`.local/architecture-check/local-web-browser/`。验收包含前端源码、锁文件和构建产物摘要，
始终 `production_acceptance=false`。
