# 检索性能运行 Harness

`pkg/retrieval.RunBenchmark`提供与具体检索引擎无关的有界性能采样器。宿主传入已装配的检索回调和每次调用的候选字节/成本代理计量；Harness只负责固定请求数、并发上限、单请求deadline、失败分类和延迟分位数。

每个样本都在固定`RequestTimeout`下执行，worker数量不超过`Concurrency`，请求队列不会无限增长。回调必须响应context；父context取消后，Harness等待已启动worker退出并返回取消错误。回调失败进入`Failed`，deadline进入`TimedOut`，成功样本才累计候选字节与CostUnits。CostUnits是平台计量单位，不自动换算商业费用。

报告输出请求数、完成/失败/超时、峰值并发、候选字节、CostUnits和P50/P95/P99延迟。分位数按排序后的最终样本位置计算，失败和超时仍占用延迟样本但不计入成功输出成本。Warmup样本不进入报告。

这个Harness验证执行边界、取消和计量采集，不证明引擎质量、Recall@10、代表性中文数据、真实供应商费用或生产硬件吞吐。正式验收仍需固定封存集、权限/发布快照、模型/索引版本、缓存条件、并发矩阵和独立实验轨迹；Recall算术继续由`retrieval_evaluation.py`负责。

当前本地测试覆盖并发上限、分位数、失败/超时分类、计量累加和无deadline/无效预算拒绝。尚无真实中文封存集或生产规模性能结果。
