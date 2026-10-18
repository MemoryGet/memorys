# 证据召回评测与模块对照 V1

本规范细化[验证方案第4节](VALIDATION.md#4-recall-与回答质量)。`retrieval_evaluation.py`提供局部离线评分，不调用检索服务、模型或权限系统。合成样例分数不证明业务Recall@10达到90%。

## 1. 独立封存集与运行记录

封存集包含dataset_id、corpus_digest、annotation_digest和cases。每题固定case_id、kind、request、allowed与relevant。request包含原始query_text、subject_id、policy_snapshot_digest和publication_snapshot_digest。kind为answerable/unanswerable/denied/withdrawn；只有answerable有非空相关证据。allowed是该主体在封存权限/发布快照下获准的来源范围，独立于后端返回结果。它不是权限令牌，也不代替运行期间的撤权检查。

证据锚点为document_id＋revision_id＋block_id＋start/end，使用非负精确整数、左闭右开Unicode码点区间。此版评分只处理标准Block检索文本投影的区间，表格/图片/音频题须提前建立经人工核验的规范文本投影及位置映射；不把任意页码或图片bbox转成字符范围。结构化原始定位继续保留供引用检查；尚未实现其它坐标评分。

run绑定同一集ID及两个摘要，记录实现标识、不可变profile摘要和每题request/status/items。request必须与该题完全一致；不隐式改写文本、归一化Unicode或替换主体/快照。每题必须出现且只出现一次，不得丢弃失败题。输入是最终提供给用户/模型的前10个证据项，保持实际顺序；重复项占用真实名额，不去重后再补第11项。每项包含item_id与一个或多个真实对应片段，不能用整篇正文替代返回片段。超过10项的窗口直接拒绝，要求导出器先准确选择最终窗口。

当前字段闭集由Python语义校验及两份JSON Schema检查；REST/Go导出器与密封集来源认证尚未实现。语料/标注摘要由封存流程独立计算；本评分器只检查关联，不重算原件或标注包哈希。正式流水线须冻结封存包、split及访问快照，保存人工标注版本和来源。

## 2. 覆盖与分母

同一版本、Block的返回片段可按区间并集合并。某相关证据的整个区间被覆盖才计为命中；缺一个字符范围也不能视作完整命中。重复或重叠片段不能增加相关证据命中数；旧Revision或另一Block即使文本相同也不匹配。多片段证据项可命中多个事先标注的单元，必须是真实返回范围；不可为了得分扩大返回或重新合并gold单元。

每题Recall＝命中相关单元数/相关单元总数；宏平均按可回答题等权，使用Fraction精确比较9/10。非成功请求即使保存中间片段也计0，不因它们曾被召回就算最终命中。超过10个相关单元的题仍保留，标注阶段规定聚合规则并另报上限；不删难题或事后调整单元提高均值。

无答案、越权、撤回题不进入Recall分母，单独记录总数和成功空结果数。空结果不是生成拒答率，也不证明授权正确。任何返回片段超出allowed范围记为泄露，包括旧版本、越权、未发布及部分越界；泄露检查也覆盖失败请求已输出的片段。正式验收要求泄露为0，不能拿高Recall抵消。

## 3. 报告与替换

报告提供精确宏平均、十进制值、分题命中/分母、失败调用、负例空结果及泄露片段数。`numeric_recall_target_met`仅说明输入记录的算术达到0.90；`formal_acceptance_passed`固定false，因为尚无真实封存来源、后端过滤、映射及质量/性能证据。

WeKnora复合路线与Qdrant独立流水线导出同一平台锚点，不把供应商chunk/point ID当gold。不改变用户、发布范围、封存集、K、预算和资料，分别记录重排前/后的窗口；主指标以最终窗口为准。分块策略改变允许内部Chunk变化，但不能修改gold版本和来源范围。

真实报告还需题型分组、置信区间、Hit/MRR/nDCG、完整证据链、回答盲评/引用支持率、硬件/并发/缓存条件、延迟分位数及费用。`pkg/retrieval.RunBenchmark`现在提供有界并发、deadline、失败分类、P50/P95/P99和成本代理计量采集，但它不产生Recall或商业费用结论。当前离线工具没有真实数据统计，也不能从已检索结果推断所有未返回证据的权限。动态撤权与撤回仍按AUTH/PUB运行场景另测。

## 4. 当前证据与后续门槛

7项合成测试检查完整/部分/有缺口覆盖、范围并集、错版本/Block/越界泄露、失败题分母、负例独立、重复gold/结果和缺题拒绝、重复返回不增命中、整数及摘要关联。见[执行证据](evidence/retrieval-evaluation-review.json)。

下一步固定封存包、独立人工标注及真实服务输出映射，再执行至少300个可回答中文问题和另设负例的共同实验，并使用[性能运行Harness](RETRIEVAL_PERFORMANCE_RUNTIME.md)记录固定硬件/缓存/并发条件下的分位数和成本代理。该规模是初始覆盖建议，代表性与置信区间决定正式数量。本次没有真实检索、中文Recall或生产性能结果，不关闭阶段F或Recall验收。

## 5. 机器Schema及请求绑定

[封存集Schema](schemas/retrieval-dataset.v1.schema.json)与[运行记录Schema](schemas/retrieval-run.v1.schema.json)由`build_retrieval_evaluation.py`生成；Draft2020-12闭集对象约束所有嵌套字段、非空标识、SHA-256字符串、区间整数边界和最多10项窗口。跨字段范围、精确整数、case唯一/覆盖和请求一致性由评分器执行，Schema本身不证明这些语义。

5项新增检查验证生成一致性、问题/主体/两快照的错配拒绝、两侧一致但无效的请求拒绝、供应商字段拒绝和Unicode文本精确匹配；另7项覆盖评分语义。见[执行证据](evidence/retrieval-schema-review.json)。本版是尚未冻结的V1设计增量；旧的局部评分样例需补request，旧证据保留为历史记录，不当作新增字段验证。

请求回显仍是运行方声明，不能证明后端实际使用了该请求或已应用该权限快照。正式实验须独立采集调用/配置/后端轨迹并核对来源；封存annotation摘要须覆盖题目、主体、快照、allowed及gold，不允许只哈希gold文本。快照变更建立新的封存实验，不原地改动同一摘要。

```powershell
python docs/architecture/build_retrieval_evaluation.py
python -m unittest discover -s docs/architecture -p test_retrieval_schema.py
```

## 6. 封存投影包完整性

`retrieval_seal.py`提供只读package_digests、validate_package及evaluate_package。评测语料清单为format=akos.retrieval-corpus/1和按document/revision/block元组排序的blocks，每条保存text_sha256、byte_size、codepoint_count；标识唯一，精确整数，字段封闭。清单不包含文件路径/下载URL，操作者另外显式提供元组到本地文件的完整映射。当前清单有Python语义检查，独立JSON Schema尚未提供。

语料摘要＝SHA-256(`akos.retrieval-corpus/1` UTF-8字节＋一个NUL＋AKOS-CJSON/1清单字节)。标注摘要＝SHA-256(`akos.retrieval-annotation/1` UTF-8字节＋一个NUL＋AKOS-CJSON/1评测集字节)，评测集只排除annotation_digest自身，corpus_digest绑定上述计算值。数组顺序保留，不隐式排序题目/gold；中文、emoji、换行和原始Unicode不归一化。原件/投影文字改变必须改变清单和语料摘要；问题、主体、权限/发布快照、allowed、gold及题目顺序改变必须改变标注摘要。

校验器先核对清单和显式文件映射，再读原始UTF-8投影字节、长度、SHA-256及码点数，核对打开句柄及路径可观察的身份/大小/修改时间；所有allowed/gold范围必须落在清单中的同一版本Block码点边界。重复gold、越界、旧版本、gold不在allowed、缺少可回答题及摘要不符均拒绝。evaluate_package先完成此校验，再核对run绑定并评分，纯evaluate仍保留为算术接口。

这是本地离线工具，最多10000个Block，默认累计256MiB，可由操作者显式调整预算；每个投影在预算内读取到内存，不是生产海量语料读取器。未执行复制/删除/下载，也不根据清单字符串打开路径。校验后文件仍可能变化，因此不证明不可变存储或真实服务随后使用相同字节。正式实验需固定句柄/版本化不可变资产和独立实验轨迹。

语料摘要定义覆盖的是评分所用的标准文本投影，尚未包含原始PDF/附件及解析模型/字体映射包；这些必须另在完整实验封存清单核验，不能把投影包完整性称为原始企业资料真实性。SHA-256也不证明数据来源、标注质量、主体/快照权威、split无泄漏或样本代表性。重新计算摘要可以合法创建新实验，不能保留同一旧实验身份冒充未修改输入。

8项专项测试使用真实临时UTF-8投影文件及合成标注，覆盖字节篡改、问题/快照/gold变化、码点与字节偏移、缺/旧Block、完整库存/预算、精确整数、规范键序/换行、重复/越权gold及评分绑定。见[执行证据](evidence/retrieval-seal-review.json)。没有真实中文封存集、来源审查或后端召回结果，阶段F及90%目标仍未通过。
