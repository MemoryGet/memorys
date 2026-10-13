# memorys

AI Knowledge OS：面向个人与企业的模块化知识平台。设计目标包含多格式接入、编辑审核、空间/知识库权限、带引用检索问答、MCP接入、有限反思，以及可迁移和可回退的组件替换。

当前处于架构设计与契约验证阶段。入口见 [设计文档](docs/README.md)，完整目标与进度见 [实施状态](docs/IMPLEMENTATION_STATUS.md)。标准契约与内部模块装配内核已通过Go测试和静态检查；仓库尚无可运行的产品服务，也未达到Recall@10或生产性能验收。

架构规范校验可运行 `python docs/architecture/check_manifest_design.py`，需要本机Python与jsonschema包；它仅验证模块清单设计样例，不是产品测试。
