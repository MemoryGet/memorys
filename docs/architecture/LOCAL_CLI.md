# 本地命令行使用闭环

`memorys-local` 是个人、单知识库的本地开发工具，使用已有 PostgreSQL。
初始化、导入和发布均须显式运行；HTTP 服务启动时只加载公钥和搜索配置。
它不会启动外部身份服务、模型服务、Docker 或向量数据库。

可选的 [多格式与 AI 扩展](LOCAL_AI.md) 使用本机 Ollama 嵌入和 DeepSeek 问答，
需显式运行 `enable-ai`；以下基础命令不依赖模型。
需要浏览器操作可使用 [本地网页工作台](LOCAL_WEB.md) 的 `web` 命令。

## 第一次使用

在仓库根目录的 PowerShell 执行：

```powershell
New-Item -ItemType Directory -Force .local/bin | Out-Null
& '.\.tools\go\bin\go.exe' build -o .local/bin/memorys-local.exe ./cmd/memorys-local
$env:MEMORYS_DATABASE_URL = 'postgres://postgres@127.0.0.1:5432/memorys?sslmode=disable'
```

数据库须已存在，并具有匹配的 `withdraw_contract_v1` 迁移账本。
当前已经初始化的本机 `memorys` 库可以直接使用；新空库先运行：

```powershell
& '.\.tools\go\bin\go.exe' run ./cmd/memorys-migrate
```

密码使用 PostgreSQL 的 pgpass 文件，或在当前 PowerShell 中安全输入：

```powershell
$databasePassword = Read-Host 'PostgreSQL 密码' -AsSecureString
$env:PGPASSWORD = [System.Net.NetworkCredential]::new('', $databasePassword).Password
```

不要将密码放入命令参数、数据库 URL、仓库文件或运行报告。每个新终端都需要
重新设置连接环境；使用 pgpass 则无须重复输入密码。工具不自动读取 `.env`。

```powershell
& .local/bin/memorys-local.exe init --with-samples
& .local/bin/memorys-local.exe doctor
& .local/bin/memorys-local.exe serve
```

`--with-samples` 表达对内置示例的明确发布意图，依次执行真实提交、个人所有者审批、
索引准备和最终发布。省略该选项只准备身份、权限和配置，不发布文档。
在另一个已配置数据库连接的终端搜索：

```powershell
& .local/bin/memorys-local.exe search memoryslocalsample
```

默认 HTTP 地址为 `127.0.0.1:8080`。需要改变端口时，在 `serve` 和 `search`
同时指定 `--address 127.0.0.1:8081`。工具拒绝监听非回环地址，搜索客户端不使用
HTTP 代理或跟随重定向。`Ctrl+C` 停止服务并等待请求退出。

## 导入自己的文本

支持 `.txt`、`.md`、`.markdown`，UTF-8 编码，最多 256 KiB。
空白、非法 UTF-8、NUL 或超限内容会在写入前拒绝。
Markdown 按正文文本保存，不抓取图片或链接、不执行 HTML。

```powershell
$draft = & .local/bin/memorys-local.exe import 'E:\资料\说明.md' | ConvertFrom-Json
$draft | Format-List
# 先查看 preview 和 content_hash，确认发布这份内容。
& .local/bin/memorys-local.exe publish $draft.draft_id --expect-hash $draft.content_hash
& .local/bin/memorys-local.exe search '正文中的关键词'
```

命令输出 JSON；搜索输出包含真实正文摘录及 document/revision/block 引用。
草稿不进入搜索。相同安装、相同内容复用同一个文档和草稿；修改内容后生成新文档，
不是按文件路径更新旧版本。所有文本以 `revision` 来源发布，不声明原件文件保真读取能力。
切分固定为单文本块、每片 1024 Unicode 码点、无重叠。

中文沿用 PostgreSQL 原生 `simple` 全文处理，不能保证任意子串、中文分词或语义召回。
内置 ASCII 示例关键词用于确认链路，不能替代中文检索效果评测。

## 重试与诊断

- 重复 `init` 核对匹配的安装和数据库，复用原配置；已封存 ACL/Profile 不覆盖。
- Windows 初始化提交目录时，短暂文件占用会进行有限重试（最多8次、等待合计630毫秒）；
  持续拒绝仍报错并保留受保护的临时目录。错误只显示操作阶段和系统码，不包含用户路径或密钥。
  若另一个初始化已创建目标目录，本次提交会拒绝覆盖；随后可用原目标目录重新运行 `init` 核对。
- 发布失败后，重跑同一 `publish` 命令和摘要。已提交草稿、审批、原始发布意图及完成回执用于恢复。
- 尚有效的任务租约沿用同一个 attempt；租约过期才重新接管，使用新的 fencing token。
  已完成索引任务直接进入最终发布，不重新 claim。
- 同时运行两个发布进程时，任务锁会拒绝重复编排；重试仍会重新认证及核对当前权限。
- `doctor` 是只读检查，不发令牌、不自动修复权限。它报告数据库、迁移、身份、权限、
  配置和发布数量；HTTP 是否已启动、索引是否正确仍需真实 `search` 验证。
- 身份禁用、ACL 或 Profile 改变会拒绝操作。请检查明确的配置冲突，不要删除状态目录后
  对已有业务数据库重新初始化。缺失私钥/安装文件必须恢复匹配的备份。

## 状态、身份和迁移

可用 [离线备份与恢复工具](LOCAL_BACKUP.md) 保存数据库、匹配的状态目录和构建后的应用，
并在新位置/新数据库恢复；下方说明仍适用于手工备份。

默认状态目录 `.local/runtime/`，可在任意命令通过 `--state <目录>` 指定。
文件名相对于该目录解析，目录可以移动；数据库连接始终来自当前进程环境。

| 文件 | 内容 |
| --- | --- |
| `manifest.json` | 安装标识、个人空间/知识库、所有者/worker、配置摘要和数据库 store epoch |
| `profile.json` | 封存的原生关键词配置 |
| `jwks.json` | RS256 公钥 |
| `private.pem` | 本地签名私钥，只供控制命令签发短期令牌 |
| `blobs/`、`vectors.json` | 启用 AI 后的不可变原件和可重建向量缓存；备份必须包含原件 |

首次创建通过同级临时目录整体改名，随后在一个数据库事务完成配置初始化，最后标记 ready。
文件已就绪但数据库事务未完成时，可以用同一目录重试。创建阶段异常留下的
`.runtime-pending-*` 不含已接管数据库的证明，不能用来自动接管已有安装。
初始化不增加业务表，也不接受没有迁移账本的未知 schema。

Windows 使用受保护 DACL，仅当前用户 SID 有访问权限；私钥加载时重新应用目录和密钥权限，
适用于移动/复制后的目录。其他系统使用目录 0700、文件 0600。
该目录已在 Git 忽略范围内。令牌仅在命令内存存在，有效期 15 分钟，并登记到数据库接受当前状态检查。
本地签名密钥初始有效期一年，临近到期由 `doctor` 报错；本轮没有提供密钥轮换或企业 OAuth 管理。
个人所有者执行提交/审批/发布，独立 service 身份仅准备索引，不具有审批权限。

迁移需要同时保存数据库和**匹配的整个状态目录**。只复制源码、只复制数据库或新生成私钥均不足以恢复。
建议先停止写入和服务，再用 PostgreSQL 18 的 `pg_dump -Fc` 备份数据库。
状态目录包含私钥，应复制到当前账户专用、访问受限的备份目录或加密备份中。
不要将它加入 Git、公共网盘或普通运行报告。

恢复到新空库使用 `pg_restore --exit-on-error --no-owner --no-acl`，保留数据库内的迁移账本、
业务权限快照和安装 store epoch。复制原状态目录后，更新 `MEMORYS_DATABASE_URL`，依次运行：

```powershell
& .local/bin/memorys-local.exe init --state 'E:\新位置\runtime'
& .local/bin/memorys-local.exe doctor --state 'E:\新位置\runtime'
& .local/bin/memorys-local.exe serve --state 'E:\新位置\runtime'
```

`init` 在恢复的目录重新收紧私钥权限并核对数据库绑定；不会采用新的安装标识覆盖旧库。
随后用另一终端 `search --state <同一目录> <关键词>` 重新签发短期令牌并核对实际引用。
pgpass 和数据库服务配置须在目标机器单独配置，不包含在仓库中。
当前仍使用验证原型 schema；本流程是本地开发恢复，不是生产跨版本升级迁移承诺。

## 可复现实库检查

```powershell
python docs/architecture/check_local_cli_windows.py --pg-bin 'D:\Program Files\PostgreSQL\18\bin'
python docs/architecture/check_architecture.py
```

实库检查仅使用独立临时集群（默认 55439），不连接用户 5432。覆盖真实 CLI、草稿隔离、
逐阶段故障恢复、租约过期、令牌/ACL 撤销、256 KiB 文档、服务重启以及 `pg_dump`/`pg_restore`
到新库和状态目录重定位。报告输出到 `.local/architecture-check/local-cli-postgres18-windows.json`，
不保存私钥、访问令牌、数据库 URL 或正文，始终 `production_acceptance=false`。
