# memorys 基础功能测试文档

## 1. 文档目的

本文用于验证当前工程的基础可运行性、契约边界和 HTTP 入口行为。测试对象是当前代码仓库，而不是已完成的生产产品。仓库说明明确指出：项目仍处于架构设计与契约验证阶段，完整的解析、发布、检索、权限、迁移和生产装配尚未验收。

## 2. 测试范围

### 2.1 本轮覆盖

- Go 包的单元测试、接口契约测试和内部装配测试。
- 架构清单、Schema、OpenAPI 等 Python 静态校验。
- `memorys-server` 的健康检查和未装配能力时的失败闭环。
- 已存在的 REST/MCP 处理器测试：上传、搜索、当前读取、删除、任务状态、认证、错误响应、证据字段和取消/超时。
- 基本安全边界：缺少凭据、非法 JSON、未知字段、重复 ID、超出数量/大小限制、错误 HTTP 方法。

### 2.2 本轮不作通过结论

- 真实 PostgreSQL、Redis、对象存储、向量库或模型服务的生产联调。
- 完整链路“上传→解析→编辑→审核→索引→发布→检索→撤回”。
- Recall@10、回答质量、生产容量、迁移回退和跨版本数据库验收。
- 仅依赖测试替身的测试结果不能证明真实后端能力。

## 3. 测试环境

| 项目 | 要求 |
|---|---|
| 操作系统 | Windows，PowerShell |
| Go | 优先使用仓库内 `.tools/go/bin/go.exe`；当前系统 PATH 未提供 `go` 时不能直接使用 `go test` |
| Python | Python 3.13 或兼容版本；架构检查需要 `jsonschema`，OpenAPI 校验按 `docs/architecture/VALIDATION.md` 准备固定虚拟环境 |
| 工作目录 | 工程根目录 `E:\ai\memorys` |
| 时间 | 测试报告记录本地时间、提交号和依赖版本 |

## 4. 执行命令

### 4.1 Go 全量测试

```powershell
& '.\.tools\go\bin\go.exe' test ./...
```

预期：所有有测试的包返回 `ok`；允许出现明确标注的 `[no test files]`。任意编译错误、测试失败或超时均判定失败。

### 4.2 静态检查

```powershell
& '.\.tools\go\bin\go.exe' vet ./...
python docs/architecture/check_manifest_design.py
python docs/architecture/check_architecture.py
```

`check_architecture.py` 是统一入口，覆盖 Go 测试、`go vet`、Python 架构测试及契约校验。若缺少工具或依赖，必须记录为环境失败，不能改写成通过。

### 4.3 启动健康检查

当前启动程序默认监听 `127.0.0.1:8080`，业务授权和服务装配为空。可执行：

```powershell
& '.\.tools\go\bin\go.exe' run ./cmd/memorys-server
Invoke-RestMethod http://127.0.0.1:8080/health
```

预期返回 HTTP 200，JSON 至少包含 `status: "ok"` 和 UTC 时间字段。停止服务后，端口应释放。

## 5. 基础功能测试用例

| 编号 | 功能 | 操作/输入 | 预期结果 |
|---|---|---|---|
| B-API-01 | 健康检查 | `GET /health` | 200；`status=ok`；响应为 JSON |
| B-API-02 | 上传未装配 | `POST /api/documents/upload` | 503；不能在未配置授权、准备器和存储时写入文件 |
| B-API-03 | 搜索未装配 | `GET /api/knowledge/search` | 503；返回 `search not configured` |
| B-API-04 | 伙伴搜索方法校验 | 对 `/api/v1/knowledge-os/search` 发送 GET | 405；`Allow: POST`；错误体含安全错误码和 `request_id` |
| B-API-05 | 伙伴搜索认证 | POST JSON，但缺少/伪造 Bearer Token | 401；包含 `WWW-Authenticate: Bearer`；不泄露内部信息 |
| B-API-06 | 伙伴搜索输入校验 | 空查询、非法 UTF-8、未知字段、重复 `kb_ids`、`top_k=0/101` | 400；错误体为稳定安全格式 |
| B-API-07 | 伙伴读取输入校验 | 缺少 `document_id`、重复 `block_ids`、过长 cursor、未知字段 | 400；不调用读取服务 |
| B-API-08 | 删除输入校验 | 错误方法、非法文档 ID、缺失幂等/授权信息 | 405 或 400/401；不产生删除副作用 |
| B-API-09 | 任务状态隔离 | 查询不存在或无权访问的 job | 返回约定错误；不暴露内部绑定、凭据或供应商字段 |
| B-API-10 | MCP 未装配 | `POST /mcp` | 503；明确表示 MCP 未配置 |
| B-API-11 | MCP 协议边界 | 非 POST、错误 Origin、错误版本、未知参数、Tasks/session 请求 | 分别返回 400/403/405；不执行应用调用 |
| B-CORE-01 | 规范 JSON | canonical JSON 的字段顺序、数字、Unicode、未知字段 | 合法输入稳定编码；歧义或非法输入拒绝 |
| B-CORE-02 | 内容证据 | 正确/越界/伪造 excerpt、错误 revision、损坏原件 | 正确证据通过；其他情况 fail closed |
| B-CORE-03 | 发布状态 | 过期版本、重复提交、租约失效、旧 worker 回写 | CAS/fencing 拒绝旧状态；不可覆盖新版本 |
| B-CORE-04 | 授权令牌 | 篡改签名、错误 claims、过期 key、算法混淆 | 全部拒绝；不得泄露密钥或内部堆栈 |
| B-CORE-05 | 取消与超时 | context 取消、deadline 到期、上传大小超限 | 返回对应错误分类；失败不产生伪造成功 receipt |

## 6. 已有自动化测试映射

执行 `go test ./...` 时重点关注以下包：

- `pkg/api`：HTTP 方法、认证、上传、搜索、读取、删除和 job 状态。
- `pkg/mcpgateway`：MCP 初始化、工具列表、搜索/读取、协议和并发凭据隔离。
- `pkg/contracts`、`pkg/modulewire`、`pkg/canonical`：DTO、wire 映射、严格 JSON 和关系约束。
- `pkg/accessgrant`、`pkg/contentverify`、`pkg/queryread`：授权、原件/证据核验、当前和历史读取。
- `pkg/document`、`pkg/preparation`、`pkg/postgres`、`pkg/cleanup`：草稿、审核、发布任务、清理和幂等状态机。
- `pkg/parser`、`pkg/parserprocess`、`pkg/adapters`：解析器契约、MarkItDown/pypdf 适配边界和关闭语义。
- `tests/architecture` 与 `docs/architecture/test_*.py`：架构依赖和契约样例。

## 7. 手工联调记录格式

每次测试至少记录：

```text
测试日期：
提交号：
执行人：
环境：OS / Go / Python / 数据库版本
命令或请求：
输入摘要：
实际状态码和响应摘要：
预期结果：
结论：PASS / FAIL / BLOCKED
证据文件：日志、测试输出、报告路径
缺陷编号：
```

请求和响应日志不得记录 Bearer Token、私钥、原件全文或其他敏感数据；只保留脱敏摘要、request ID 和必要的哈希。

## 8. 通过标准

基础测试轮次满足以下条件时，才能标记为“基础契约通过”：

1. Go 全量测试和 `go vet` 均通过。
2. 架构清单、Schema 和 OpenAPI 校验通过，或环境缺失被明确标记为 BLOCKED。
3. `/health` 返回 200，未装配业务入口按设计返回 503，不出现未捕获 panic。
4. 认证、未知字段、边界数量、大小限制和错误方法用例全部通过。
5. 失败响应不包含凭据、内部路径、SQL、堆栈或供应商内部标识。
6. 报告明确区分本地契约通过与生产验收未完成，不能用单元测试替代真实后端验收。

## 9. 当前基线结果

2026-10-18 在本工程环境执行：

- `.tools/go/bin/go.exe test ./...`：通过；所有带测试包返回 `ok`，部分命令包/工具包为 `[no test files]`。
- `python docs/architecture/check_manifest_design.py`：通过，输出为 schema、manifest fixtures、拒绝样例和 settings boundaries 均通过。
- 系统 PATH 中未安装 `go` 命令，因此文档统一使用仓库内 Go；直接执行 `go test` 或 `go vet` 应记录为环境配置错误。
- 生产服务、真实数据库、完整治理闭环和 Recall/性能目标仍未完成验收。
