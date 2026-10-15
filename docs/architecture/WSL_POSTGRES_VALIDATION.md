# 独立WSL PostgreSQL验证路径

Docker不可用时增加独立的PostgreSQL试验入口，支持15/16/18，默认16。它使用现有Ubuntu 24.04 WSL发行版，以普通用户运行从Ubuntu或PGDG签名包索引下载并解包的PostgreSQL及必要动态库，不安装系统包、不注册服务、不修改系统软件源。

软件包、独立apt索引和解包目录保存在工作区.local/postgres{major}-ubuntu及.local/pgdg。运行时环境变量只作用于当前验证进程。数据库数据目录位于WSL /tmp中每次新建的私有目录，使用唯一所有权标记；临时服务仅监听127.0.0.1专用端口，使用fixture trust认证，不能用于生产。

```powershell
python docs/architecture/check_postgres_wsl.py
```

入口检查本地端口未被占用，初始化独立集群，执行同一份SQL约束脚本与Go仓储探针。Go程序仅允许通过AKOS_TEST_PG_PORT改变回环端口，主机/用户/库/协议保持固定，拒绝连接串注入。探针包含候选CAS/回执/审计/outbox、读取授权、重放及故障回滚，同时保留原撤回/清理/索引验证。

报告记录真实版本、服务程序和Go探针摘要、输入前后摘要及SQL/仓储结果。清理先停止明确属于本次目录的服务，再验证目录前缀、所有权、标记和非符号链接后删除；停止或所有权校验失败时保留目录并报告失败，不触碰其他实例。psql使用-X避免加载用户初始化脚本。

WSL进程与宿主共享资源，此入口不提供Docker的CPU/内存隔离，也不证明生产部署、迁移回退或IAM。具体主版本兼容性必须分别执行并读取其运行报告，安装依赖成功或脚本编译成功不能代替运行证据。

2026-10-15当前运行结果：15.19/16.15/18.6在同一414项源码输入和同一Go二进制上的两份SQL与54项检查均通过；服务停止、目录清理与端口关闭已独立核对。见[多版本验收矩阵](POSTGRES_VERSION_MATRIX.md)。软件包文件摘要分别保存在[evidence/postgres16-ubuntu-packages.json](evidence/postgres16-ubuntu-packages.json)与[evidence/postgres-pgdg-packages.json](evidence/postgres-pgdg-packages.json)；缓存包清单不是完整动态库环境证明。

Docker故障诊断观察到旧dockerInference端点不可访问，与[官方仓库报告](https://github.com/docker/desktop-feedback/issues/527)的故障表现相同。确认Docker进程不存在且目录仅含零字节套接字reparse point后，将运行时目录保留重命名为run.stale-20261015084934与docker-secrets-engine.stale-20261015084934；容器/镜像目录未改动。备份映射保存于.local/architecture-check/docker-runtime-recovery.json。之后日志记录正常/app/quit，未继续反复启动Docker，也未将其标记为已恢复可用。未执行工厂重置、WSL注销或系统重启。
