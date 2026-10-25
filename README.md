# memorys

[本地网页工作台](docs/architecture/LOCAL_WEB.md)：导入文件、核对正文、明确发布，并进行带来源的检索和问答。
在已配置本地环境的仓库根目录运行 `.local/bin/memorys-local.exe web` 后打开 `http://127.0.0.1:8080/local/`。
已实现功能与验收证据的对应关系见 [本地交付核对](docs/architecture/LOCAL_DELIVERY.md)。

本地开发：通过 [本地命令行](docs/architecture/LOCAL_CLI.md) 初始化个人身份和配置、导入 TXT/Markdown、
明确发布并真实检索。手动配置见 [本地关键词搜索](docs/architecture/LOCAL_KEYWORD_SEARCH.md)。
可选 [多格式与 AI 扩展](docs/architecture/LOCAL_AI.md) 提供 PDF/Office、本机语义检索及带引用的 DeepSeek 问答。
迁移和换机可使用 [本地离线备份与恢复](docs/architecture/LOCAL_BACKUP.md)，同时保存数据库、原件、身份状态和构建好的应用。
环境变量方式启动的默认服务仍未开启业务能力。

AI Knowledge OS：面向个人与企业的模块化知识平台。设计目标包含多格式接入、编辑审核、空间/知识库权限、带引用检索问答、MCP接入、有限反思，以及可迁移和可回退的组件替换。

当前处于架构设计、契约验证与局部可运行原型阶段。入口见 [设计文档](docs/README.md)，完整目标与进度见 [实施状态](docs/IMPLEMENTATION_STATUS.md)。已提供个人本地网页和命令行发布/检索/问答链路，企业网页管理、Recall@10和生产性能仍未验收。

架构规范校验可运行 `python docs/architecture/check_manifest_design.py`，需要本机Python与jsonschema包；它仅验证模块清单设计样例，不是产品测试。
