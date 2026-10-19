# Codex 驱动功能验收

`ai_acceptance_manifest.json` 是验收项的唯一清单，`run_ai_acceptance.py` 是统一执行入口，输出 JSON 证据供 Codex 判定。

## 执行

先执行静态门禁：

```powershell
python docs/architecture/run_ai_acceptance.py --tier static
```

再在已准备好 PostgreSQL/fixture/外部依赖后执行运行时场景：

```powershell
python docs/architecture/run_ai_acceptance.py --tier runtime
```

需要完整执行时使用 `--tier all`；预览命令使用 `--dry-run`；单项复跑使用 `--only current-read`。结果写入 `docs/architecture/evidence/ai-acceptance-latest.json`。

## Codex 判定规则

Codex 读取结果文件后按以下规则输出结论：

1. 只接受 `ai_decision=pass` 且所有必需套件均为 `passed`。
2. 任一必需项为 `failed`，结论为 **不通过**，列出命令、stderr 和失败场景。
3. 环境缺失、超时或依赖不可用时，结论为 **无法判定**，不能折算为通过。
4. `production_acceptance` 固定为 `false`；本工具的通过只表示清单所覆盖的本地功能范围通过。
5. 对每个通过项报告套件、用例、耗时和证据文件；对未验证项原样列出。

## 功能验收项

- 代码回归：Go test、Go vet、Python 契约测试、OpenAPI 结构校验。
- 业务链路：准备、审核、发布任务、最终发布、当前读、历史读、删除、任务状态、删除队列、关键词检索。
- 负面路径：越权、撤销 token、重复请求、超时、取消、重试、事务失败和跨资源隔离。
- 数据证据：状态转换、版本/CAS、游标、哈希、审计、Outbox、Schema 和资源清理。

## 给 Codex 的固定提示词

```text
读取 docs/architecture/evidence/ai-acceptance-latest.json。
只依据 JSON 中的 results 和 ai_decision 判定，不猜测未执行的功能。
必需项全部 passed 才能说本地功能验收通过；failed 说不通过；inconclusive 或环境错误说无法判定。
明确区分 local_functional_acceptance 与 production_acceptance，并逐项列出失败、未验证和证据路径。
```
