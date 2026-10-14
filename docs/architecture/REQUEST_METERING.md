# 全请求 usage、费用与预算契约

本规范细化[反思应用接入](REFLECTION_PIPELINE_BINDING.md)的REF-BIND-05。当前ExecutionBudget只有deadline、单次MaxOutputTokens和MaxExtraRounds，检索执行器只计一次执行的候选字节；它们不能证明全请求费用与跨轮调用上限。已有设计、Go usage/金额类型及精确算式；统一计量器、持久账本和真实usage映射尚未实现。

## 1. 计量归属与请求身份

RequestMeter由自有应用持有，模型/解析/检索适配器只提交标准化usage。每次真实外部调用具有唯一Invocation/Attempt、父RequestID、模块实例/实现版本、操作及阶段/轮次。框架内部重试、子Agent和模型调用也必须登记，不能把一次Reflector方法当成一次真实模型请求。

父预算在请求准入时冻结，包含deadline、总检索/嵌入/模型调用数、候选处理字节、输出tokens及费用额度。阶段和子调用只能分配剩余额度；轮次变化不能重新创建同额度的预算。主体、空间与计量账户来源由平台授权配置确定，模型不得改计量账户或扩大额度。

计量账户不代替AccessScope。用尽费用或调用预算时停止新调用，已取得证据仍须通过当前状态核验；不能因费用不足取消权限检查或将请求转入其他用户额度。

## 2. usage标准模型

| 指标 | 统一语义 |
|---|---|
| retrieval_calls / embedding_calls / model_calls | 实际调用数，每次重试分别记录，预检失败不假算成供应商调用 |
| input_tokens_total | 供应商报告的完整模型输入，含模板/系统消息/实际上下文；本地估计单独标识 |
| input_tokens_cached | 已报告总输入的子集，不与total重复相加；不同缓存计费由费率版本决定 |
| output_tokens_total | 实际模型输出，供应商内部计费部分按可核验分类另记录，不能无提示漏计 |
| embedding_input_tokens | 实际嵌入计量；改写文本、批次和重试分别归属 |
| candidate_bytes / blob_bytes / ocr_pages | 平台已处理数据与解析规模，单位明确，失败/丢弃工作仍记录 |
| duration / status | 开始/完成或取消时间、完成/错误/超时/usage未知；不能只统计成功请求 |

UsageReceipt同时标记来源（provider_reported/platform_measured/estimated/unknown）、完整性、供应商请求关联、计量实现版本和原始受控记录引用。unknown不是0；模型自报数字不能当作计量事实。供应商字段在适配器映射，产品接口使用平台指标。

媒体、工具或特有计费维度不能强行折算成tokens。新增维度须登记名称、单位、关系及费用规则后才能进入严格费用Profile；不能使用无版本的浮点map作为权威费用记录。

## 3. 费用快照与精确计算

费用使用Currency＋Scale＋非负整数Units，允许不同计量货币，但单份预算只在明确货币/尺度下比较。费率冻结为RateProfile ID/版本/摘要及单位规则，记录有效窗口和计费类别。不得读取“当前默认费率”重新解释历史请求，也不得将不同币种直接相加。

费率可以采用明确分子/分母计算，按规定方向舍入；所有运算验证整数范围，禁止binary float造成额度漂移。缓存输入属于总输入的子集，先按费率类别拆分，避免重复收费。换算或折扣要有独立版本与来源，不能由模型生成。

平台估算费用与供应商最终账单分开记录。严格准入依赖经过验证的计量上界、模型参数限制和费率快照；供应商未提供这些能力时，只能暴露已声明的估算模式或拒绝严格Profile，不能声称外部账单必然不超过本地估算。

## 4. 预留、结算与未知状态

RequestMeter拟定Reserve、Begin、Reconcile、AbortBeforeDispatch和Load五项写/读语义。Reserve按有界调用计划原子预留额度；Begin记录调用已经可能发出；Reconcile按可核验usage结算并释放确定未使用部分。每条Reservation与Invocation/Attempt唯一关联。

| 情况 | 预算处理 |
|---|---|
| 预检失败，确认未触达供应商 | 标记aborted_before_dispatch并释放相应未使用预留 |
| 调用成功且usage完整 | 精确结算，保留估算与真实usage差异 |
| 超时/取消/断连，是否执行或计费未知 | 保持unknown及保守占额；不因为无回答就退回全部额度 |
| 降级或丢弃候选/输出 | 已执行调用/处理成本照计，不返还已消耗部分 |
| 同Invocation/Receipt相同摘要重复交付 | 返回既有结算，不重复扣减 |
| 同身份不同usage/费率摘要 | conflict，不能覆盖首次事实 |

未知状态只在取得供应商查询结果、受控对账或明确到期处置证据后改变。留存策略需说明未知占额的最大支持窗口与人工/自动对账方式，不能无限占额而没有产品状态。

Reserve/状态/结算与所需审计/outbox在共同权威事务中推进。供应商调用及对账网络请求在事务外完成；提交未知时先读持久记录再重试，不用内存标志断言“已经扣过”。

## 5. 并发、恢复与模块替换

并发子调用共享同一父预算，预留采用CAS或已验收的原子事务，不能各自读取同一余额后都通过。请求阶段/轮数和额度变更有统一版本，不允许内部框架绕过Meter直连供应商。

取消阻止后续调用，但无法追回已发生的外部费用。重启/任务恢复加载同一Request预算及未决Invocation，不通过新ID重新获得完整额度；如业务需要新请求，须新的显式准入及关联审计。

替换模型/编排/解析实现时复用同一usage与费用契约，验证单位、完整性、重试与取消。计量实现与费率可分别替换，权威Request额度和历史结算由平台保存。供应商不能成为唯一费用账本，缓存日志或Redis投递不决定消费事实。

## 6. 验收门槛

| 门槛 | 所需证据 |
|---|---|
| METER-01 全调用可见 | 初始/反思/嵌入/生成/重试/内部节点所有实际调用关联同一Request，漏报不被当作0 |
| METER-02 精确计算 | 大整数、费率分数、舍入、缓存子集及币种/尺度错配可验证，无溢出或重复计费 |
| METER-03 并发准入 | 多进程争用父额度，预留/扣减不超额，子阶段不重置预算 |
| METER-04 未知与幂等 | 调用取消/断连和提交未知，相同回执不重复结算，冲突回执拒绝，未知占额可追溯 |
| METER-05 恢复替换 | 持久重启/对账/留存与两个供应商或编排实现通过同一契约 |
| METER-06 产品评测 | 固定条件报告成功/错误/超时的实际费用、延迟及质量，区分估算和真实供应商账单 |

这些门槛尚未执行。先定义类型化预算/Receipt/费率与机器契约，再实现事务计量器及真实供应商映射；已有时间限制、请求内循环和单轮字节预算不关闭METER或整体架构验收。

## 7. Go金额/usage类型与精确算式

[metering.go](../../pkg/contracts/metering.go)定义MoneyAmount、UnitRate、UsageQuantity/Source和UsageReceipt。金额Units/数量为uint64，Scale为0–9；Currency只核对三位大写语法，不证明货币/费率目录获准。UnitRate记录Profile版本/摘要、计量种类、QuantityPerRate及Price，不引用供应商类型。

CalculateUsageCharge用标准big.Int计算ceil(quantity×Units/QuantityPerRate)，精确中间乘积后拒绝超过uint64的结果；不使用float。AddMoney拒绝币种/尺度不同及溢出，不执行换算。SplitInputTokenUsage返回未缓存/缓存两桶，cached大于total拒绝，避免total+cached重复计费。费率例子为合成测试参数，不代表任何供应商现价。

ValidateUsageReceipt只检查给定形状/关系：unknown数量必须nil且不complete，known/estimated数量明确，估算不能标完整实测，provider_reported需受控记录引用；metric唯一并核验缓存子集。记录引用非空不证明来源真实，complete仍需所用模块的完整指标规则和认证证据，不能仅以该字段自动结算。

6项Go检查覆盖零值/向上舍入、大整数中间乘积与溢出、币种/尺度/分母、超double精确缓存分桶、unknown不当0/完整、估算及缺供应商引用。全Go测试/vet通过，见[证据](evidence/metering-arithmetic-review.json)。没有Reserve/Reconcile、权威费用/费率来源、持久并发/恢复或真实计量，METER与整体保持开放。


## 8. 计量应用端口与持久事务边界

[request_meter.go](../../pkg/contracts/request_meter.go)定义RequestMeter端口和供应商无关类型，首版为接口声明，不是已运行的计量器。Open是准入应用执行的补充操作：只能建立一次不可变Allowance。Reserve/Begin/Reconcile/AbortBeforeDispatch/Load不得更换账户、费率、父额度或用新RequestID规避旧请求的未决调用。

MeterScope独立表达经过认证的主体、空间、账户与策略版本；它不授予知识访问权，结构字段本身也不是授权证明。所有操作须核验当前账户权限、Request绑定与作用域有效期。预算停止或执行截止后仍允许经授权的Load/对账；对账调用使用新的操作deadline，但不得借此允许新的业务调用。

| 操作 | 原子条件与结果 |
|---|---|
| Open | 授权准入、费率冻结；同Request唯一，登记额度与初始版本；初始ExpectedBudgetVersion为0 |
| Reserve | 检查父预算版本、开放状态和剩余额度；记录Invocation/Attempt唯一预留并增加Held；严格模式核对BoundEvidenceDigest指向的上界证据 |
| Begin | 检查预算/预留版本和未过期调用窗口；reserved变dispatched，写唯一DispatchID；账本提交后才允许一次外部发送 |
| Reconcile | 验证回执来源、所需指标完整性、身份、费率和计量关系；unknown保留占额，完整实测按冻结规则结算；实测收费由计量器计算，不能信任客户端提交费用 |
| AbortBeforeDispatch | 仅reserved且具有可核验未发送证据时释放Held；dispatched/unknown不得走此路径 |
| Load | 从权威持久记录读回预算及可选预留，用于提交未知后的恢复；不能以缓存或内存结果证明结算 |

每次命令含CommandID、CommandDigest与预算期望版本；变更既有预留还含预留期望版本。摘要在服务端按已约定编码重算，并包含所有语义参数、调用账户和费率关联，不能只信任客户端字符串。幂等记录先于版本冲突判定读取：同账户/Request/CommandID且同摘要返回首次持久结果，不同摘要返回conflict。不同命令争用同版本最多一个事务成功；budget+reservation+idempotency+audit/outbox共同提交。精确CAS、唯一约束及事务隔离须由真实仓储验证。

Begin返回Replayed不表示允许重发。发送者崩溃于Begin之后而外部执行情况不明时，恢复流程查询供应商幂等键或执行记录并登记unknown；禁止仅因本地没有回答而再次发送同Invocation。若供应商无法证明一次性执行，必须保留未知状态；显式新重试采用新的Invocation/Attempt和额外预留。声明端口并不能实现跨数据库与供应商的原子发送。

### 8.1 余额与回执状态

余额按指标维护Committed和Held，费用维护同币种/尺度的CommittedCharge和HeldCharge；准入使用检查溢出的加减比较。输入total/cached/uncached是相关事实，指标上限独立检查；计费只用选定互斥桶，不能把三者费用相加。估算超额不截断真实用量：保留实际事实、标记Overrun并停止新增调用，报告差额。strict_bound同样记录实际异常并拒绝继续调用，不能靠截断账本维持不超额的假象。

预留状态为reserved→dispatched→unknown或settled，reserved也可aborted_before_dispatch。unknown允许取得对账证据后settled；部分信息保持unknown及保守占额。时间到期不是外部免费或可退额证据。已settled/aborted不允许原地修改；后续纠正通过独立、可审计调整记录，首版端口不支持调整，不能覆盖历史回执。

一次预留可能先收到unknown后收到最终实测。Reconcile的SupersedesReceiptDigest必须匹配当前未决回执；原始回执作为不可变历史保留，新摘要经CAS推进。相同ReceiptID/摘要重复不再消费，不同摘要复用同ReceiptID拒绝。Receipt/费率/引用存在不证明真实性；完整性规则与供应商记录映射须独立验证。

### 8.2 替换与验证要求

计量实现可替换，但须导出父Allowance、费率快照、所有预留/回执链、幂等结果、余额、审计和待投递事件。暂停新增准入及发送，保留对账窗口，校验每项余额与历史、未决调用和幂等重放后切换；恢复不能重置RowVersion或丢失旧命令结果。生产注册清单/装配检查与远程wire映射尚未接入本端口，不宣称已完成部署级替换。

下一步验证同一契约的持久实现：多进程预留竞争、各事务提交前后崩溃、Begin提交与发送之间崩溃、未知回执转实测、重复/冲突回执、估算超额及迁移后重复命令。接口编译只证明类型关系，不证明这些METER门槛。


## 9. 输入用量分桶关系复核

发现并修复Receipt校验缺口：此前仅核验cached≤total，未校验uncached，适配器可提交相互矛盾的三项数字。当前任一已知子桶须存在已知total并不大于total；两个子桶均已知时，强制uncached=total−cached。采用已验证子集后的减法，拒绝相加溢出造成的错误等式。

允许total已知、一个子桶未知的未完整记录，保留unknown，不自动推导成实测。缺total或total未知但子桶已知拒绝；即使估算记录也不能违反显式分桶关系。来源真实性和模块要求的指标完整性仍需独立检查。

新增两项Go测试：七组分桶案例包含零值、uint64上限、子桶超额、和不足/超额及相加溢出；另覆盖缺失/未知total及部分cached未知。现有计量测试共八项，全Go测试与vet通过。见[证据](evidence/metering-input-partition-review.json)。这只关闭局部关系校验缺口，不关闭持久账本/实际计量门槛。
