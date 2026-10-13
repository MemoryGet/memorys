# AI Knowledge OS：GitHub 与官方资料选型调研

调研日期：2026-10-11（北京时间）。需求：多格式上传、反思纠错、知识编辑审核、权限划分、向外部 Agent 开放 MCP、响应快、业务召回率达到 90% 以上。

本次搜索覆盖 GitHub 仓库、官方产品文档、发布说明、许可证和公开评测资料，重点查看 13 个项目。能力判断主要基于文档和部分源码；没有部署项目，也没有用用户数据进行性能测试。官方文档及 main 分支可能领先于发布版，所有采购和部署应按选定版本重新验证。

**选型结论**

优先实测 WeKnora；接受商业授权时，将 FastGPT 商业版、Onyx 企业版纳入对照。DocsGPT 是值得验证的 MIT 许可证备选。Dify 适合承担人工审核和流程编排；RAGFlow 适合进入复杂文档解析与检索的对照测试。完整需求可以用现有组件组合实现，但严格的知识审核发布、调用者权限传递和实际性能仍需要配置、集成与验收。

本次没有找到能够直接保证“任意企业资料、任意模型、任意负载下业务 Recall@K 超过 90%，并同时满足全部治理要求”的可复现证据。应将 90% 写入指定数据集、K、版本、负载下的验收目标。

**1. 需求应如何判断**

| 需求 | 应核验的实际行为 |
|---|---|
| 多格式上传 | 文件能进入解析链路；扫描 PDF、表格、图片、音频分别配置并验证解析能力 |
| 反思纠错 | 能检测证据不足、重新检索或修正答案；反馈生成的知识更新需经审核 |
| 编辑审核 | 编辑权限、草稿、审核人、发布版本、撤回和重新索引；工具执行审批不能替代知识发布审批 |
| 权限划分 | 在检索、原文读取、Wiki、图谱、缓存、导出和 MCP 调用时均生效 |
| 开放 MCP | 项目作为 MCP Server 向外部 Agent 提供检索或应用能力；MCP Client 功能不等同于此 |
| 响应快 | 分别测量纯检索耗时、首个有效回答内容的时间、完整回答耗时、多轮反思耗时 |
| 召回率 90%+ | 指定 Recall@K 和人工标注的相关证据集合；同时报告无答案、权限和多跳问题表现 |

**2. 主要候选对照**

表内“需扩展/未确认”表示未按完整需求核验，不能直接判定项目完全没有该能力。速度和业务召回率全部未实测。

| 项目 | 已核验的匹配能力 | 主要缺口或边界 | 定位建议 |
|---|---|---|---|
| [WeKnora](https://github.com/Tencent/WeKnora) | 多格式解析、分块及 Wiki 编辑与版本历史、空间 RBAC、知识库范围 API Key、内置 MCP Server、多步 Agent | 未找到完整的“草稿→指定审核人→批准后对外检索”通用知识发布流程；业务反思评判器需配置 | 首轮主平台候选 |
| [FastGPT](https://github.com/labring/FastGPT) | 知识库、检索测试、数据修改、工作流、双向 MCP | 团队权限及文件级权限存在商业版边界；正式知识审核发布需配置或扩展 | 接受商业版时优先对照 |
| [Onyx](https://github.com/onyx-dot-app/onyx) | 企业数据源连接、企业搜索、多轮检索、原系统权限同步、MCP Server | 用户级检索权限、权限同步和细粒度 RBAC 需企业版；知识编辑审核通常由来源系统承担 | 已有大量企业系统资料时优先 |
| [RAGFlow](https://github.com/infiniflow/ragflow) | 文档解析、可视化分块与人工干预、混合检索重排、Agent 反思设置、官方 MCP Server | 最新 v1.0.0-rc1 有权限功能缺口；知识审核发布流程需补充 | 复杂文档解析及检索对照 |
| [Dify](https://github.com/langgenius/dify) | RAG Pipeline、知识维护、循环工作流、Human Input 人工审核节点、原生双向 MCP | 工作流审核需接通知识发布接口；企业细粒度权限需按版本和授权核对 | 审核与反思编排层候选 |
| [DocsGPT](https://github.com/arc53/DocsGPT) | 文档解析、Agent、Wiki 人机编辑、团队资源 Viewer/Editor 权限、MCP Server；官方说明包括 SSO/团队在内使用 MIT | Wiki 编辑后会更新检索；工作流不能暂停等待工具审批，完整知识审核发布需外接 | 宽松许可证备选 |
| [MaxKB](https://github.com/1Panel-dev/MaxKB) | 文件知识库、分段维护、工作流、资源授权；v2 源码有 MCP 应用服务 | MCP 源码按已发布应用 API Key 调用单个 Agent，不能据此推定继承外部调用者的个人文档权限；审核流程需核对 | 中文企业交付备选 |
| [Arkon](https://github.com/nduckmink/arkon) | 文档编译为 Wiki、Refine/Verify、草稿与编辑审核、部门 RBAC、OAuth MCP Server | 项目较年轻；格式覆盖和性能未验证；PolyForm Internal Use 许可证 | 治理架构参考与试验候选 |

**3. 关键项目的官方依据**

**WeKnora：优先验证，但需要补齐发布治理。**

v0.8.2 文档列出 PDF、Word、PPT、Excel、EPUB、XMind、TXT、Markdown、CSV、JSON、HTML/MHTML、图片和音频。图片与扫描件需要相应视觉能力，音频需要语音识别配置。[解析文档](https://github.com/Tencent/WeKnora/blob/v0.8.2/website-docs/03-features/03-document-parsing.md)

Wiki 可人工编辑、比较版本和回滚。权限以空间、角色、资源归属和共享关系为基础，程序身份可限定知识库范围。内置 MCP Server 可对外发布知识检索、阅读、问答及 Wiki 能力。[Wiki](https://github.com/Tencent/WeKnora/blob/v0.8.2/website-docs/03-features/14-wiki.md)、[权限](https://github.com/Tencent/WeKnora/blob/v0.8.2/website-docs/03-features/01-tenant-auth.md)、[MCP](https://github.com/Tencent/WeKnora/blob/v0.8.2/website-docs/03-features/08-mcp.md)

建议把“草稿库/候选修改”与“正式可检索版本”分开：审核通过后发布到正式范围，对外 MCP 默认仅开放正式知识的读取。上述流程是集成建议，不能视为 WeKnora 已开箱提供的完整功能。

**FastGPT：功能覆盖广，商业版边界必须计入。**

官方商业版对照表将团队空间与权限列为商业版能力；文件级权限文档明确标注为商业版特有。文件权限的检索过滤还要求按当前成员身份鉴权，预选知识库但未按成员身份鉴权的工作流不做同样的文件级过滤。[版本对照](https://doc.fastgpt.cn/zh-CN/guide/version/commercial)、[文件级权限](https://doc.fastgpt.cn/zh-CN/guide/dataset/collection_permission)

官方 README 已列出双向 MCP、知识库测试和数据修改能力。官方教程中的内容审核与人工确认可以作为工作流起点；是否严格阻止未审核知识进入生产索引，仍需单独验收。[README](https://github.com/labring/FastGPT)、[工作流教程](https://doc.fastgpt.io/zh-CN/guide/getting-started/quick-start)

**Onyx：企业搜索与既有系统权限整合。**

官方资料明确用户级检索权限可配置于企业版，权限同步连接器及 group-based RBAC 也列入企业版。MCP Server 通过调用者令牌访问主应用搜索接口，权限由主应用执行。[搜索](https://docs.onyx.app/overview/core_features/internal_search)、[企业版](https://docs.onyx.app/admins/getting_started/enterprise_edition)、[MCP 实现说明](https://github.com/onyx-dot-app/onyx/blob/main/backend/onyx/mcp_server/README.md)

如果内容原本存放在 Confluence、SharePoint 等系统，可评估在来源系统完成编辑审核，再同步到 Onyx。是否只同步已发布内容、多久同步撤回、原文引用是否继续受权限限制，需要实测。

**RAGFlow：版本差异会直接影响权限验收。**

官方 v1.0.0-rc1 发布说明称其为 Go 重写的预览版，并列出 Team/Me 权限模型尚未支持、DeepDoc 当前仅支持 CPU、升级不可逆等边界。不能用旧版团队协作文档推定新版本具备相同权限。依赖该权限模型时，应将 v0.27.2 等明确版本纳入对照，而不是直接采用 latest。[发布说明](https://github.com/infiniflow/ragflow/blob/main/docs/release_notes.md)

官方 Agent 组件有 Max reflection rounds 设置，同时指出反思轮数和工具调用增加响应时间。官方 MCP Server 的 Self-host 模式共享一把 RAGFlow API Key，Host 模式允许客户端分别提供身份；多用户权限场景应重点验证 Host 模式。[Agent 组件](https://ragflow.io/docs/v1.0.0-rc1/basic_component)、[MCP 模式](https://github.com/infiniflow/ragflow/blob/main/docs/develop/mcp/use_ragflow_as_mcp_server.md)

**Dify：审核编排与 MCP 的当前能力。**

v1.6.0 已引入原生双向 MCP；现行自部署文档可在应用 Access Point 中启用 MCP Server。无需沿用早期必须安装社区 MCP Server 插件的判断。[官方公告](https://dify.ai/blog/v1-6-0-built-in-two-way-mcp-support)、[发布文档](https://github.com/langgenius/dify-docs/blob/main/en/self-host/use-dify/publish/publish-mcp.mdx)

v1.13.0 的 Human Input 节点可暂停流程，允许人审核、编辑并决定分支。它能承担审批步骤，但需要接通知识库正式版本的发布接口和权限检查。[Human Input](https://dify.ai/blog/the-human-input-node-bringing-human-judgment-into-automated-workflows)

**DocsGPT：当前能力比传统文档问答更完整。**

官方文档支持团队分享及 Viewer/Editor 权限、Wiki 人机编辑和对外 MCP。Wiki 编辑后异步重新嵌入，完整草稿审核发布需要另外设计。其工作流文档明确：要求工具审批时会使节点失败，工作流不能暂停等待审批。[团队](https://docs.docsgpt.cloud/Using/teams-and-sharing)、[Wiki](https://docs.docsgpt.cloud/Sources/Wiki-sources)、[MCP](https://docs.docsgpt.cloud/API/mcp-server)、[工作流限制](https://docs.docsgpt.cloud/Agents/nodes)

**MaxKB：应区分应用权限和调用者身份。**

v2 源码的 MCPToolHandler 检查应用 Key 是否有效、应用是否已发布，再提供 agent_<application_id> 工具。这是对外 MCP 的源码证据；不代表已验证外部用户身份到知识检索的全链路权限传递。[处理器](https://github.com/1Panel-dev/MaxKB/blob/v2/apps/chat/mcp/tools.py)、[入口](https://github.com/1Panel-dev/MaxKB/blob/v2/apps/chat/views/mcp.py)

**Arkon：治理设计贴近需求，成熟度与许可约束需分开判断。**

README 描述 Map→Reduce→Plan-review→Refine→Verify→Commit、计划审核、Wiki 草稿批准、部门权限和带范围约束的 MCP 工具。格式列出 PDF、DOC/DOCX、文本、URL 和嵌入图片。GitHub 仓库创建于 2026-04，当前公开版本 v0.9.1；不能仅凭 README 的 enterprise-grade 表述认定已满足生产需求。[项目与许可证说明](https://github.com/nduckmink/arkon)

**4. 其他检索到的项目**

| 项目 | 本次判断 |
|---|---|
| [BISHENG](https://github.com/dataelement/bisheng) | 企业工作流、Human in the loop、文档解析与 RBAC 值得评估；本次未确认完整的对外知识 MCP 与审核发布契约，不列为已满足全部条件。[官方能力说明](https://github.com/dataelement/bisheng/blob/main/README.md) |
| [AnythingLLM](https://github.com/Mintplex-Labs/anything-llm) | 适合快速部署文档助手和多用户工作区；官方 MCP 文档主要描述消费外部服务，尚不足以证明完整的知识审核发布与对外知识 MCP。[MCP 文档](https://docs.anythingllm.com/mcp-compatibility/overview) |
| [Open WebUI](https://github.com/open-webui/open-webui) | 可作为模型交互和知识入口候选；本次未完成其严格知识审批发布与对外知识 MCP 的核验，暂不作为首轮知识主平台 |
| [QAnything](https://github.com/netease-youdao/QAnything) | 可作为本地文档问答与两阶段检索参考；默认分支是 qanything-v2，不能直接以旧 master 文档判断当前版本。完整权限、审核发布、对外 MCP 本次未确认 |
| [NVIDIA RAG Blueprint](https://github.com/NVIDIA-AI-Blueprints/rag) | 官方有 MCP 与评测工具，适合作为性能和多模态 RAG 参考；完整知识治理产品层需要另外建设。[MCP](https://github.com/NVIDIA-AI-Blueprints/rag/blob/main/docs/mcp.md)、[评测](https://github.com/NVIDIA-AI-Blueprints/rag/blob/main/docs/evaluate.md) |

**5. 成熟度的客观参考**

以下为本次读取 GitHub 公共 API 的近似 Star 数，以及当时 latest-release 返回的标签。Star 仅代表关注度，不代表权限或性能验收通过；标签中的 rc 仍应按预览版本处理，即使 GitHub prerelease 字段未标记。

| 项目 | 约 Star | latest-release 标签 |
|---|---:|---|
| WeKnora | 33.1k | [v0.8.2](https://github.com/Tencent/WeKnora/releases/tag/v0.8.2) |
| RAGFlow | 92.0k | [v1.0.0-rc1](https://github.com/infiniflow/ragflow/releases/tag/v1.0.0-rc1) |
| FastGPT | 29.8k | [v4.17.2](https://github.com/labring/FastGPT/releases/tag/v4.17.2) |
| Onyx | 32.4k | [v4.9.0](https://github.com/onyx-dot-app/onyx/releases/tag/v4.9.0) |
| Dify | 158.1k | [1.17.1](https://github.com/langgenius/dify/releases/tag/1.17.1) |
| MaxKB | 22.9k | [v2.10.6-lts](https://github.com/1Panel-dev/MaxKB/releases/tag/v2.10.6-lts) |
| DocsGPT | 18.3k | [1.0.0](https://github.com/arc53/DocsGPT/releases/tag/1.0.0) |
| Arkon | 1.5k | [v0.9.1](https://github.com/nduckmink/arkon/releases/tag/v0.9.1) |

**6. 商业化需要计入的许可边界**

- WeKnora 主项目采用 MIT，第三方组件有各自条款。[LICENSE](https://github.com/Tencent/WeKnora/blob/v0.8.2/LICENSE)
- RAGFlow 采用 Apache-2.0。[仓库](https://github.com/infiniflow/ragflow)
- DocsGPT 官方声明 SSO、团队等也在 MIT 范围内。[README](https://github.com/arc53/DocsGPT)
- Onyx 普通代码采用 MIT，ee 目录适用企业许可证。[LICENSE](https://github.com/onyx-dot-app/onyx/blob/main/LICENSE)
- Dify 是附加条件的许可证，明确对未经授权的多租户运行及前端标识修改设置限制。[LICENSE](https://github.com/langgenius/dify/blob/main/LICENSE)
- FastGPT 是附加条件的许可证，对类似 FastGPT 的多租户 SaaS 及控制台标识修改设置商业授权条件。[LICENSE](https://github.com/labring/FastGPT/blob/main/LICENSE)
- MaxKB README 标注 GPLv3；交付形态需要按所用版本条款确认。[仓库](https://github.com/1Panel-dev/MaxKB)
- Arkon 标注 PolyForm Internal Use 1.0.0，应先确认拟定用途是否允许。[仓库](https://github.com/nduckmink/arkon)

**7. 90% 召回与响应速度的验收方案**

建议先准备 300–500 条人工标注的行业问题进行选型，并留出独立测试集。覆盖中文制度、扫描件、表格数值、型号/编号、跨文档、多跳、历史版本、无答案和权限隔离。试验规模只是起点，最终样本量应由覆盖范围与置信区间决定。

对每条可回答问题 q 定义相关证据集合 Gq 和最终返回的前 K 条证据 Rq：

`Recall@K(q) = |Rq ∩ Gq| / |Gq|`

固定文档或证据单位、K、去重规则与权限范围，报告各问题的宏平均，以及不同文档类型和问题类型的分项结果。无答案问题另测误答率；多跳问题另测完整证据覆盖率。Hit@K、回答正确率、LLM 判分的 Context Recall、向量 ANN 索引 recall 都应分开报告。

公开评测的数值不能直接迁移。NVIDIA 的 accuracy-benchmarks 明确主要指标是端到端回答准确度，同一配置在不同数据集有明显差异；其 0.90 级别结果不能直接写成业务 Recall@10 超过 90%。[评测方法与结果](https://github.com/NVIDIA-AI-Blueprints/rag/blob/main/docs/accuracy-benchmarks.md)

| 指标 | 建议初始验收要求（不是项目已实现的性能） |
|---|---|
| 检索 | 行业独立测试集最终 Recall@10 ≥ 90%；同时报告候选召回和最终重排结果 |
| 普通检索延迟 | 在约定硬件、模型、10 万分块、10 并发下，P95 ≤ 1.5 秒 |
| 首个有效回答内容 | 普通问答 P95 ≤ 3 秒；排除占位提示，并固定模型与输出策略 |
| 复杂反思 | 单独记录时延、调用次数、成本与正确性增益；设置轮数和时间预算 |
| 权限 | 所有测试身份与访问入口下越权检索、原文读取和引用泄漏为 0 |
| 审核发布 | 未批准知识不会进入生产检索；修改、撤回、删除后在约定时间内一致生效 |

检索优化从解析、分块、关键词/向量混合召回、融合、重排开始。查询改写、图谱与多轮检索通过消融测试决定，避免把组件数量当作效果指标。

**8. 推荐的最小组合**

`文件与来源系统 → WeKnora 解析/知识维护 → 候选修改或草稿 → 人工审核 → 正式可检索知识 → 只读 MCP → 外部 Agent`

WeKnora 承担通用知识底座。审核步骤可用 Dify Human Input 或现有企业流程实现，但需连接正式知识发布接口。普通问题走一次混合检索与重排；证据不足或复杂问题再启用有预算的反思检索。反馈先生成候选知识修改，审核通过后发布。

第一轮实测应比较同一批文件、同一评测问题和尽量相同的模型配置：WeKnora、FastGPT 商业版；已有企业连接器和权限同步需求时增加 Onyx 企业版；复杂文档问题显著时增加 RAGFlow 明确版本。只有实测证明底座解析不足，才增加独立解析引擎或更换底座。
