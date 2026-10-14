# 候选组件准入与部署边界 V1

本规范补充[组件矩阵](DECISIONS.md)和[受控构建锁](BUILD_LOCK.md)。候选能力、实际部署组成和契约运行证据分别记录；一个项目的功能表不证明每个模块可独立替换。

## 1. WeKnora固定版本核验（2026-10-13）

基线仍为v0.8.2，commit `3e8b0bfc80b845b2d4b2ed683994748741450a97`。本次对LICENSE、Compose、Go依赖声明、第三方声明及两个Dockerfile保存摘要，见[核验清单](evidence/weknora-deployment-review.json)。这些是下载源文件与解析配置的证据，没有启动服务。

| 检查项 | 已观察事实 | 架构结论 |
|---|---|---|
| 项目许可 | LICENSE说明项目MIT，并明确第三方例外及补充声明 | 主项目许可不能代替所有依赖、镜像、数据与模型许可评估 |
| 基础部署 | Compose无profile服务为frontend/app/docreader/postgres/redis；app依赖后三项 | 私有复合引擎包含自身持久库和任务组件，不能把它们当平台权威仓储 |
| 数据库镜像 | postgres服务用paradedb/paradedb:v0.22.6-pg17 | PostgreSQL License判断不能直接覆盖该镜像及其扩展 |
| 制品锁定 | app/docreader/frontend默认WEKNORA_VERSION为latest | 锁住源码commit还不等于锁住运行制品，部署须另解析OCI摘要和架构 |
| 可选后端 | Qdrant/Milvus/Weaviate/Doris等由不同profile启用 | profile提供部署选择，不证明平台IndexWriter/Retriever独立端口通过验收；full也不代表全部后端 |
| 解析依赖 | DocReader Dockerfile引用docreader/pyproject.toml与uv.lock | 下一步核对这两个真实依赖文件，不沿用不存在的requirements.txt假设 |
| 应用构建 | Dockerfile含AnyDoc构建和build tag；Compose默认WITH_ANYDOC=1 | 制品需记录构建开关；关闭或替换解析实现后格式覆盖重新验收 |

上述事实依据固定[Compose](https://github.com/Tencent/WeKnora/blob/3e8b0bfc80b845b2d4b2ed683994748741450a97/docker-compose.yml)、[LICENSE](https://github.com/Tencent/WeKnora/blob/3e8b0bfc80b845b2d4b2ed683994748741450a97/LICENSE)、[第三方声明](https://github.com/Tencent/WeKnora/blob/3e8b0bfc80b845b2d4b2ed683994748741450a97/THIRD_PARTY_NOTICES.md)和[应用Dockerfile](https://github.com/Tencent/WeKnora/blob/3e8b0bfc80b845b2d4b2ed683994748741450a97/docker/Dockerfile.app)。具体依赖许可尚未逐项核验，不依据字符串匹配作商业授权结论。

GitHub API当次返回仓库未归档、未禁用，最近push为2026-10-12；latest正式release为v0.8.2，发布时间2026-09-24。它们只证明观察时的活动与发布元数据，不证明固定版本获持续安全支持、main修复已回补或维护SLA。[官方发布](https://github.com/Tencent/WeKnora/releases/tag/v0.8.2)

## 2. 与平台数据归属的隔离

平台PostgreSQL保存权限、Revision、审核、发布、任务事实和映射。WeKnora私有数据库保存引擎对象与衍生索引，外部Redis不是平台任务事实仓储。禁止平台模块直接读取或写入WeKnora表；通过复合适配器及内部BackendBinding映射访问。

基础实验可以使用隔离的引擎数据库及Redis，即使服务类型重复，也不能让引擎数据库迁移改变平台表。是否共用实例只是后续运维配置选择，必须隔离数据库/凭据/配额并证明备份恢复影响边界；不为了减少容器而合并权威领域。

引擎原生frontend、MCP及对象直链不对产品用户开放；平台REST/MCP统一执行当前权限与发布检查。停用frontend的实验override须先验证启动依赖，不把本页当作可直接执行的Compose文件。图谱、联网搜索、可观测整栈和其它可选后端按试验目的逐一启用，不依赖full隐式扩展制品集合。

## 3. 每个候选的共同准入记录

| 记录维度 | 必需内容 | 通过证据 |
|---|---|---|
| 源码/发行 | 项目、tag解析commit、下载源、文件摘要 | 可复核的源码与发布关联 |
| 部署制品 | OCI digest/CPU架构、构建参数、启用profile、模型/字典版本 | 实际运行身份与BuildLock一致 |
| 权利边界 | 项目、第三方、镜像、模型、数据分别记录 | 实际交付集合的许可清单与必要声明 |
| 能力粒度 | 独立端口/复合组、输入输出、过滤与错误语义 | 同一公共契约测试，缺少必需能力明确拒绝 |
| 数据出口 | 权威/衍生归属、标准内容、ID映射、删除与快照 | 完整导出/重建及引用对照 |
| 替换恢复 | 新实现、配置切换、保留窗口、新写入回退 | 真实替换/故障/恢复报告 |
| 维护 | 当次发布、修复/支持政策、固定版本升级路径 | 来源与日期；活动量不能代替支持承诺 |
| 效果成本 | 相同封存资料、过滤范围、硬件与并发 | Recall、引用、延迟及成本对照 |

准入阶段依次为待核验、源码核验、部署核验、契约核验、替换核验。只有适用门槛全部通过才列为正式实现；无状态模块可注明迁移不适用，但协议、权限、效果和配置替换仍须检查。WeKnora当前处于源码核验部分完成，保持复合候选定位。

D-05尚未关闭：本次未完成所有候选、完整依赖/SBOM/许可证清单、镜像摘要及安全修复核对；W-01至07、替换试验和中文召回/性能也未执行。下一项调研应核验解析依赖与独立检索候选，再用同一记录维度对照。

解析依赖的固定核验见[解析部署包](PARSER_DEPLOYMENT.md)：DocReader有独立Python锁、Office/Java/浏览器系统依赖；OCR/VLM不能根据整个项目能力推定。依赖库存不等于镜像SBOM或解析效果证明。

独立向量候选见[Qdrant适配器](QDRANT_ADAPTER.md)及[固定源码清单](evidence/qdrant-v1.16.2-source-review.json)。主项目Apache-2.0、显式向量/过滤/删除/scroll入口已有源码证据；原生快照不替代平台可移植重建和迁移回退。

中文关键词候选见[OpenSearch固定源码与适配边界](KEYWORD_ADAPTER.md)。主项目许可及SmartCN注册/依赖声明已核验，实际插件安装、analyzer效果、制品/维护/许可全集及6项运行门槛未验；固定旧版仅用于兼容对照，不替代正式部署选型。
