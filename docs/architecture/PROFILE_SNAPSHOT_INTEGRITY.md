# 配置快照内容与摘要校验

更新：2026-10-16。实现 `pkg/configprofile`；原生运行验证及独立Python摘要复算已通过。

## 快照对象

`akos.profile-snapshot/1` 保存完整平台类型的 PipelineProfile、IndexProfile、QueryProfile。快照包含模块绑定、路由、检索/生成预算及反思策略；不包含密钥、供应商运行对象或部署实例设置。

Seal 对输入建立独立副本，检查引用关系、绑定、查询预算和路由约束，重算三个摘要。Encode 验证当前字段与摘要一致；Decode 拒绝重复键、未知字段、字段名别名、尾随内容、超限内容及摘要不一致。快照大小上限 1 MiB。

返回的 Go 结构仍是普通值，调用方可以修改自己的副本；修改后必须重新 Seal，否则 Encode 拒绝。数据库目录通过不可变触发器保持已存字节不能更新或删除。

## 摘要格式

均采用现有 AKOS-CJSON/1 编码和 SHA-256：

- 索引摘要沿用 `akos.index-profile/1`：移除 IndexProfile.Digest，规范化包含 format/profile 的对象，前缀为该格式名和 NUL。
- 查询摘要为 `akos.query-profile/1`：QueryProfile.Digest 设为空字符串，规范化完整 QueryProfile，使用格式名及 NUL 前缀。
- 流水线摘要为 `akos.pipeline-profile/1`：PipelineProfile.Digest 设为空字符串，规范化 `{Pipeline, IndexDigest, QueryDigest}`，使用格式名及 NUL 前缀。

流水线摘要同时承诺子配置的内容摘要；仅保持子配置 ID/版本号不变而修改内容，仍会产生不同流水线摘要。子引用 ID/版本必须匹配快照中实际配置。

## 数据库接入

`query_profile_catalog` 以流水线摘要为键保存快照原始字节。当前配置引用通过外键绑定该目录。`QueryFacts` 每次从目录加载、解码、重算并核对流水线摘要，再返回当前事实。只有摘要登记而没有实际内容的引用不能进入当前查询链路。

原生探针已经把检索请求和投影中的流水线/查询摘要改为从真实配置计算。探针会尝试将正确快照放到错误摘要键下，确认当前事实读取拒绝该条目，并验证配置切换拒绝旧请求。独立 Python 复核重算全部三层摘要与引用关系。

## 验收边界

这是平台配置内容完整性及一部分结构/预算约束，不是完整部署编译器。IndexProfile 现有摘要算法不会自动证明分块器、索引器或嵌入模型在实际运行时满足配置；模块制品、依赖、完整模式兼容、凭据引用、实际能力和实例状态仍必须由后续编译与装配阶段验证。

当前尚未完成完整索引模式语义校验、所有可选模块的语义组合、配置管理权限/审计、制品锁与生产启动装配。不得以摘要通过代替模块替换、迁移、回退或产品效果验收。

当前验证：489项输入，PG15/16/18各208项原生检查。见[快照证据](evidence/profile-snapshot-runtime-review.json)和[独立当前证据后端复核](evidence/evidence-backend-runtime-review.json)。完整配置编译/装配仍开放。

查询阶段已接入[实际模块装配](PROFILE_MODULE_ASSEMBLY.md)，当前证据提升到493项输入/每版211项检查。完整索引阶段、制品锁与生产Bootstrap仍待完成，先前489/208属于快照完整性增量结果。
