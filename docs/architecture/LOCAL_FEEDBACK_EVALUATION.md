# 本地反馈评测样本

本地工作台的来源反馈记录现在可选地保存触发该引用的原始检索问题。旧版 `feedback.json` 没有该字段时仍能正常读取；新反馈由检索界面自动绑定当前 query。导出接口保留问题、文档/版本/块引用、反馈类型、评论和处理状态，可直接转换成离线 Recall、引用准确率和帮助率评测输入。

反馈只记录评价，不修改发布正文、审核状态或检索结果。正式多用户部署仍需要把同一字段绑定到数据库反馈表、主体、知识库和幂等键，并在写入前重新验证引用的当前授权。

本地工作台还提供 `GET /local/api/evaluation` 统计接口。它复用反馈筛选和当前会话权限，支持 `state`、`kind`、`document_id`、`revision_id`、`tags`、`from`、`to`（RFC3339）参数，标签按当前文档 manifest 的全部匹配规则执行，返回有界样本数、已判断数、helpful/issue 比率、唯一查询数、文档数和版本数；不返回原始查询、评论或凭据。报告格式为 `memorys.retrieval-evaluation/1`，作为后续 Recall@K 和引用准确率评测集的汇总入口。

Dashboard 可以把当前相同筛选保存为本地评测集草稿：`POST /local/api/evaluation-sets` 保存创建时的反馈快照和筛选条件，`GET /local/api/evaluation-sets` 返回集合摘要，`GET /local/api/evaluation-sets/{id}` 读取完整样本。集合最多保留 100 个，每个最多 10000 条，写入使用原子替换；后续反馈状态变化不会改写已保存样本。

对 `incorrect`、`missing`、`outdated` 反馈，Dashboard 还可调用 `POST /local/api/feedback/proposal` 创建修订草稿。服务端只复制当前可编辑文本并记录 `proposal_draft_id`，不会改变已发布版本；草稿必须重新经过摘要校验、审核和发布。
