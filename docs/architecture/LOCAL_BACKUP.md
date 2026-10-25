# 本地离线备份与恢复

`tools/local_backup.py` 将 PostgreSQL、匹配的整个运行状态和构建好的应用放入一个目录。
它使用现有 Python 标准库和 PostgreSQL 18 工具，不增加服务。当前面向本机 Windows/PostgreSQL 18。

## 备份

先停止 `web`、`serve` 和其他导入/发布命令。保持 PostgreSQL 服务运行。
数据库连接与密码沿用 [本地命令行配置](LOCAL_CLI.md)，通过进程环境或 pgpass 提供；
连接 URL 不接受内嵌密码。确认停止其他写入后，在仓库根目录执行：

```powershell
$env:MEMORYS_DATABASE_URL = 'postgres://postgres@127.0.0.1:5432/memorys?sslmode=disable'
# PGPASSWORD 或 pgpass 仍需可用；不要把密码写进命令文件。
python tools/local_backup.py backup 'E:\backups\memorys-2026-10-23' `
  --pg-bin 'D:\Program Files\PostgreSQL\18\bin' --offline-confirmed
python tools/local_backup.py check 'E:\backups\memorys-2026-10-23'
```

备份目标的父目录须已存在，目标目录须不存在。可用 `--state <目录>` 指定其他运行状态目录。
工具先核对身份、权限、配置和数据库绑定；以 SHARE 表锁暂时阻止平台表写入，
同时执行 `pg_dump -Fc`、复制状态，并核对复制前后的文件摘要。文件变化或锁失效会失败。
`--offline-confirmed` 是你确认已停写；不能代替停止外部直接修改文件的程序。

完成的目录包括：

- `database.dump`：整个数据库，包含迁移记录、权限和已发布内容。
- `.local/runtime/`：同一安装的私钥、公钥、配置、原件及本地向量缓存。
- `.local/bin/memorys-local.exe`、`dashboard/dist/`：当前可执行程序和网页静态文件。
- 启动脚本、备份工具、解析器工作脚本和对应的依赖锁文件。
- `backup.json`：最后写入的完成标记与每个文件的长度/SHA-256。

Windows 新目录限制当前用户访问；不会继承普通用户组的访问权限。这个目录包含私钥、
原件和业务数据，应保存在受保护的本地磁盘或加密备份中。校验清单检测意外损坏，
不证明备份来源可信；只恢复自己的可信备份。不会把数据库密码或模型 API 密钥放入备份。

## 恢复到新位置和新数据库

新机器安装 PostgreSQL 18 和 Python 3.11 或更新版本，复制可信备份，然后从原仓库工具或源码副本运行：

```powershell
# 使用目标 PostgreSQL 的管理员/建库账户。该 URL 中已有数据库只用于连接检查。
$env:MEMORYS_DATABASE_URL = 'postgres://postgres@127.0.0.1:5432/postgres?sslmode=disable'
python tools/local_backup.py restore 'E:\backups\memorys-2026-10-23' 'E:\memorys-restored' `
  --database memorys_restored --pg-bin 'D:\Program Files\PostgreSQL\18\bin'

$env:MEMORYS_DATABASE_URL = 'postgres://postgres@127.0.0.1:5432/memorys_restored?sslmode=disable'
& E:\memorys-restored\tools\start-local-web.ps1
```

新位置和数据库都必须不存在。工具先验证全部文件，再复制到受保护目录；用 `createdb`
新建空库，执行 `pg_restore --exit-on-error --no-owner --no-acl`，最后只读核对原身份、
权限和配置。不会删除或覆盖任何现有数据库，不会自动执行迁移或生成新身份接管旧库。
失败时可能留下本次新建的部分目录或数据库，保留用于诊断；没有完整成功输出就不能当作恢复完成。

跨 Windows 账户恢复会为新副本设置当前账户权限，原安装密钥和数据库身份保持配对。
恢复后先验证关键词检索，再按 [AI 配置](LOCAL_AI.md) 重建解析虚拟环境、恢复相同摘要的
Ollama 嵌入模型并重新设置 DeepSeek 密钥。模型权重、Python 虚拟环境和 PostgreSQL 程序
不包含在备份中；不复制包含旧绝对路径的虚拟环境。解析依赖锁文件已随应用保存。

工具限制为 4 GiB、100000 个文件、PostgreSQL 18 与 IPv4 本机连接；拒绝符号链接、
Windows 重解析点、硬链接、跨目录路径、大小写冲突、缺失或额外文件。它是个人本地恢复工具，
不承诺跨 PostgreSQL 主版本升级、在线不停写备份或生产灾备。

## 验证

```powershell
python docs/architecture/check_local_backup_windows.py --pg-bin 'D:\Program Files\PostgreSQL\18\bin'
python docs/architecture/check_architecture.py
```

独立运行器只创建自己的临时 PostgreSQL 及合成文件，覆盖真实备份/恢复、独立连接写入屏障、
私钥损坏拒绝、已有数据库/目录拒绝、恢复后的原件 HTTP 引用与本地语义检索。
报告为 `.local/architecture-check/local-backup-postgres18-windows.json`。
