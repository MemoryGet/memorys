# 公钥信任激活、防回滚与恢复 V1

本规范衔接[Grant令牌与快照](GRANT_TOKEN_PROFILE.md)、[构建锁](BUILD_LOCK.md)和[一致性协议](CONSISTENCY.md)。严格JSON加载只能证明配置与给定期望一致；配置来源、持久版本与所有进程的生效需要以下控制面协议。当前为架构设计，未实现仓储、分发或激活协调器。

## 1. 权威数据和信任根

| 记录 | 所有者与字段 | 不变量 |
|---|---|---|
| TrustDomain | 部署治理：domain_id、issuer_id、授权发布者/控制面信任根、row_version | Grant验签公钥不自动成为配置发布者；请求不得指定信任根 |
| TrustHead | 部署治理：domain/issuer、active SnapshotIdentity、active BuildLock、high_water_version、row_version | active版本等于已提交高水位；同版本只允许同摘要，已使用ID/版本不能改内容 |
| TrustSnapshotRecord | 部署治理：SnapshotIdentity、不可变公钥配置引用、来源证明/审批、状态 | 引用不是任意URL；固定字节与独立摘要一致，未激活候选不接单 |
| KeyRetirement | 安全治理：domain/issuer/kid、公钥指纹、原因、永久退役/受损标记、操作/审计关联 | kid不得复用；受损公钥不能换kid复活，正常退役须按既定窗口结束 |
| TrustActivation | 部署治理：operation、旧Head期望、新Snapshot/BuildLock、维护状态、观察/审计/日志关联 | 操作幂等、CAS，失败不覆盖已活动配置或降低高水位 |
| RuntimeTrustReceipt | 运行协调：实例/制品、SnapshotIdentity、BuildLock、启动代际、检查时间 | receipt是实例观察，必须核对受信实例身份，不能由用户JSON代替 |

元数据仓储为这些记录提供独立服务契约，事务仍通过平台UnitOfWork共同提交；其它模块不直读表。现有知识MetadataRepository不承担部署治理；新增独立契约见下文。导出记录族和持久实现仍待扩展，不把草稿权限表冒充配置管理。

配置发布者通过独立控制面信任根/批准证明认证，不能仅验证新公钥对自己配置的签名。Bootstrap显式登记TrustDomain与初始Head，取得可追溯批准；没有记录时拒绝接单，不能把磁盘上的任意第一份配置当作最高信任。

## 2. 激活与重启流程

```mermaid
flowchart LR
    A[控制面授权及不可变候选] --> B[许可/制品/快照/公钥历史核验]
    B --> C[关闭相关新调用并等待可恢复状态]
    C --> D[事务CAS提交TrustHead及高水位]
    D --> E[分发固定BuildLock与快照并重启]
    E --> F[所有必需实例受信receipt核对]
    F --> G[恢复新调用与签发]
```

1. 验证控制面操作者/来源证明，加载候选固定字节。LoadVerifier的独立Expectation来自批准记录和锁，而非候选JSON自填。候选同issuer且version严格大于Head高水位；相同已提交Identity的恢复另走幂等分支，不重新提升版本。
2. 核验key ID/指纹历史、key窗口及模型/模块兼容配置。公钥指纹按固定算法标签与原始公钥字节计算，受损公钥禁止在新kid下重新加入。正常轮换先分发含新旧公钥的更高版本配置；签发器只有在新公钥已准备且窗口允许后才使用新kid。
3. 首版允许重启，因此采用相关信任域维护窗口：暂停新Grant签发及模块新调用，等待在途操作进入已知或隔离可恢复状态。任务与字节不因配置暂停自动撤销，结果未知仍按调用账本保留。紧急受损事件先停止相关新请求/输出闸门，再推动配置切换。
4. 一个元数据事务核验Head row_version、高水位、审批和操作摘要，更新active Identity/BuildLock及high_water_version，同时保存激活事实、KeyRetirement变更、幂等结果、审计/outbox/MigrationJournal。事务内不执行网络分发；发布者不能跳过退役登记。
5. 已提交后分发固定制品/快照并重启。实例从受信控制面及权威Head取得相同期望，核对锁、来源及LoadVerifier结果。数据库不可读或三者不一致保持不就绪，不自动加载旧快照。
6. 所有必需实例和签发器receipt核验完成后恢复业务。旧进程仅凭本地尚未过期快照不具备恢复资格；入口/访问闸门还核对运行信任代际，阻止过期实例继续接收新业务。

本地Verifier不会自动监听Head，也不阻止拥有旧进程访问路径的人继续使用旧公钥。部署隔离、受信入口及当前代际闸门是必需条件，不能用“已向实例发送通知”替代切换完成证明。已传出的在途内容遵循既有不可追回边界。

## 3. 中断、回退及历史恢复

| 中断位置 | 允许恢复 | 禁止行为 |
|---|---|---|
| 事务前 | 重新验证候选/来源/Head和窗口，旧活动版继续受原规则控制 | 仅因候选存在而让实例接单 |
| 事务提交结果未知 | 按唯一operation查询Head、幂等结果及完整关联；保持维护 | 将超时当未提交，或重复写另一Head |
| Head已提交但分发部分失败 | 保持新Head/高水位，恢复同一operation；未匹配实例不就绪 | 降低版本来迎合仍运行的旧实例 |
| 实例已重启、receipt不可信/缺失 | 保持维护，重新核对受信运行身份与制品 | 用候选自己提供的receipt作为通过证据 |
| 新版本存在配置错误 | 经批准生成更高版本修复配置，再执行激活 | 将active直接切回低版本 |
| 需要恢复旧的正常公钥集合 | 以更高版本重新批准兼容集合，核对退役/受损记录 | 恢复受损key或复用已退役kid |
| 从历史数据库备份恢复 | 在隔离环境补齐之后的TrustHead/退役/授权日志，核对独立控制面锚点 | 仅以备份内高水位宣称无回滚后直接上线 |

高水位只能防止当前权威状态之上的版本回退，无法单独检测整个数据库和日志一起被恢复到旧备份。恢复必须核对独立于该备份的受信控制面Head/检查点；没有独立锚点时停止上线并保持隔离，不伪造“防回滚已通过”。

配置回退与索引/文件回退不同：安全退役事实不可倒退。更高版本修复同样要记录来源、状态和审计；不能以紧急运维权限绕过受损公钥禁止重新启用的规则。

## 4. 导出、迁移和可替换性

TrustDomain/Head、快照权威引用、激活操作、退役记录及运行核对事实进入平台元数据导出/迁移日志；私钥仍留凭据系统，导出仅含受控引用及必要版本证明。切换数据库后所有待处理激活租约重新领取，核对控制面锚点后才能恢复。

SDK/Verifier替换使用同一SnapshotIdentity与令牌Profile测试；不能改变业务动作或重新授予退役key。控制面或KeyResolver替换须证明独立期望来源、历史高水位、退役集合、维护闸门及receipt验证兼容。BuildLock目前尚无专用trust snapshot条目，后续应以版本化扩展加入配置来源/期望绑定，不塞到任意settings中声称已经锁定。

## 5. 验收矩阵

| 编号 | 必须取得的运行证据 |
|---|---|
| TRUST-01 | 合法来源可激活；自签配置、未批准操作者及候选自填期望拒绝 |
| TRUST-02 | 并发激活和重试只提交一个Head，高水位及审计/日志无半提交 |
| TRUST-03 | 同版本换摘要、低版本、退役kid、受损公钥换kid全部拒绝 |
| TRUST-04 | 在CAS前后、分发中途、重启/receipt阶段杀进程，恢复同operation，无旧实例绕行 |
| TRUST-05 | 正常新旧公钥轮换、紧急退役、过期实例及在途未知调用受控恢复 |
| TRUST-06 | 数据库/控制面替换和历史备份恢复，独立锚点发现倒退并阻止上线 |

这些是新设计门槛，尚未执行。现有SDK和JSON加载测试不能证明持久CAS、来源信任、分发或防回滚；对应机器记录、部署锁扩展、仓储和协调器仍待实现，D-03/D-04保持开放。

## 6. 平台仓储与部署事务端口

`pkg/contracts/trust.go`新增标准库类型：TrustDomain、TrustHead、TrustSnapshotIdentity、TrustSnapshotRecord、KeyRetirement、TrustActivation，以及DeploymentScope/审计/事件。PublicConfig是不可变公钥JSON的BlobRef，不能存放私钥；公钥配置身份摘要与Blob原始字节SHA-256分别核对，不混为一个摘要。

部署授权使用DeploymentAuthorizer及DomainID范围，独立于知识Authorizer/AccessScope。主体仍用平台Principal，DeploymentScope本身不是已认证证明。部署审计/事件携带DomainID，不把部署操作塞进某个用户空间。公共合同不引用SDK、数据库驱动或accessgrant实现类型。

TrustRepository提供读取Head/快照/退役/操作、快照创建、激活版本保存、Head CAS及追加退役/审计/outbox/journal；它仅在TrustUnitOfWork.WithinTrust回调中使用，不逃逸或执行网络分发。所有写入由同一元数据库适配器事务保证：不要把AppendAudit/Outbox换成独立网络服务后仍宣称原子提交。通知发布者可另替换，权威事务事实保持平台控制。

`ValidateTrustHeadAdvance`只验证非空域/issuer、不变归属、有效Identity/锁摘要、row_version精确加一且不溢出、前后高水位等于各自active版本，以及新高水位严格增加。相同已提交版本的幂等恢复先读原操作，不调用推进检查来重复提交；Bootstrap是另一个获批准的创建流程。此函数不执行CAS、检查key退役、来源或审批，也不返回授权许可。

3项Go测试覆盖超过double精度的版本推进、低/同版本、CAS输入与高水位矛盾、跨域/issuer、错误摘要和计数溢出，见[执行证据](evidence/trust-ports-review.json)。仓储和事务端口仍只有声明，未有生产数据库实现。新增记录族已在[元数据1.1库存](EXPORT_CONTRACT.md)加入ExportManifest和Journal包络清单；typed payload及真实导入仍待完成，不能用旧导出PASS恢复信任治理数据。

TrustHead首个完整字段映射与导出payload检查见[类型化导出记录](EXPORT_CONTRACT.md)。它只检查单记录结构与关系，不核验独立控制面锚点或真实CAS。其它信任族和Journal类型化重放仍待完成。

Head/高水位保留规则已落实为[Journal局部语义检查](JOURNAL_PROTOCOL.md)：不接受trust_head墓碑，域停用须保留Head并以独立治理状态控制。链内正确不证明备份外部锚点或首次源Head已核验。

[日志起始Head库存](JOURNAL_PROTOCOL.md)已提供独立参数核对，不接受缺省空库存或假Bootstrap。库存来源、真实ExportManifest/控制面锚点及目标事务仍未验证，不能将合成起点当作防回滚证明。
