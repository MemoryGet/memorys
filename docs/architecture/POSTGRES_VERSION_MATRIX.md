# 已实现SQL与事务的PostgreSQL多版本验证

2026-10-15，在独立WSL临时实例中执行相同SQL脚本和150项Go仓储/应用链路检查。三次使用相同461项源码输入和同一个Go探针二进制，每次从空库初始化，不进行跨版本数据目录复用。

| 服务端实际版本 | SQL脚本 | 同一套150项检查 | 服务停止/目录清理 |
|---|---|---|---|
| 15.19（PGDG Ubuntu包） | 通过 | 通过 | 已核对 |
| 16.15（Ubuntu包） | 通过 | 通过 | 已核对 |
| 18.6（PGDG Ubuntu包） | 通过 | 通过 | 已核对 |

覆盖当前实现的撤回事务、历史/任务/清理、索引删除与快照交接、候选CAS/回执/审计/outbox/读取，以及真实文件解析后的候选应用链路，以及数据库草稿到冻结Revision和submitted/版本0审核请求的同步原子提交，以及批准/拒绝/撤销批准的持久决定与当前指针，以及当前规则/后端绑定与发布准备登记、持久租约/接管/续租及已授权冻结内容读取，以及文本关键词索引准备、实际投影读回与attempt隔离，以及重新核验后的任务完成、原子回执/审计/事件和租约到期后的历史对账。包含回滚、重放、迟到结果保留与提交后丢失响应的受控故障试验。权限回调、初始业务元数据及故障注入仍是fixture。

```powershell
python docs/architecture/check_postgres_wsl.py --major 15 --port 55433
python docs/architecture/check_postgres_wsl.py --major 16 --port 55432
python docs/architecture/check_postgres_wsl.py --major 18 --port 55434
```

`--major`只允许15/16/18，且运行后核对SHOW server_version，拒绝请求与实际主版本不一致。各版本使用工作区内独立解包目录、回环端口、私有数据目录和所有权标记。PG18所需liburing也仅解包到该版本的运行目录，未安装系统服务或修改系统数据库。

PGDG来源与仓库公钥参照[PostgreSQL官方Ubuntu下载说明](https://www.postgresql.org/download/linux/ubuntu/)。使用独立apt索引、软件源文件和Signed-By文件，不注册系统软件源；软件包与公钥摘要保存于[PGDG制品记录](evidence/postgres-pgdg-packages.json)。这些身份记录不是完整运行环境/SBOM证明。

汇总证据：[postgres-version-matrix.json](evidence/postgres-version-matrix.json)。原始版本报告分别为[PG15](evidence/postgres15-wsl-review.json)、[PG16](evidence/postgres16-wsl-review.json)、[PG18](evidence/postgres18-wsl-review.json)，包含源文件、服务程序和Go二进制摘要。清理后另行确认私有目录不存在、测试端口已关闭。

该项通过的是**当前已实现子集的版本兼容验证**。不等同于完整Metadata UoW、审核发布、真实IAM/RLS、HTTP/MCP、生产性能、全量元数据迁移/回退验收。它不证明直接升级/降级已有数据目录，也不替代旧Docker镜像的精确发行版验证；旧Docker报告保留为历史记录。

当前追加[最终发布与替换](FINAL_PUBLICATION.md)16项检查，覆盖实时索引再核验、发布CAS及原子历史/回执/审计/outbox、固定旧源清理和既有撤回互操作。6份发布结果、12个公共投影和6个命令摘要通过独立对照；仍不代表正式服务、真实权限、更多检索通道或完整发布/检索产品验收。

当前再追加[查询权威读层](POSTGRES_QUERY_READS.md)12项场景：真实Repeatable Read多页、当前批准/代际、并发撤回与新读取失效、句柄关闭、游标与返回对象隔离、当前权限回调、取消、store轮换及实际撤销批准；3组真实观察投影通过独立关系校验。只证明元数据读取子集，不证明实际检索结果、完整QueryAuthority/CHECK、真实IAM或仓储替换。
