# 模块依赖边界与自动检查

本规范落实[分层设计](DESIGN.md#2-分层及依赖规则)。当前自动检查覆盖pkg/与cmd/下所有非测试Go源文件，包含未被当前平台build tags选中的文件；不覆盖尚未建设的internal/业务层或frontend/。检查通过证明静态import边界，不能证明实际数据隔离、远程授权或数据迁移。

## 1. 当前允许的依赖

下列路径省略仓库模块前缀。依赖按完整包路径匹配；不允许用目录前缀自动放行子包。未登记包及新增外部依赖默认拒绝。

| 包 | 允许的仓库内依赖 | 用途 |
|---|---|---|
| contracts / canonical | 无 | 标准端口、领域类型与确定性编码 |
| kernel | contracts | 注册、配置与生命周期；不能导入具体实现 |
| retrievalplan / reflection / queryread | contracts | 编排只使用端口，不识别具体适配器 |
| publicationprojection | contracts、canonical | 标准投影与请求关系校验 |
| modulewire | contracts、canonical | 显式数据协议映射 |
| moduletransport | contracts、modulewire | HTTP与流接收实现 |
| accessgrant | canonical | 隔离签名验证实现 |
| fusion | contracts、kernel | 本地融合实现及其受控工厂 |

fusion导入kernel是实现提供注册工厂的依赖，kernel不得反向导入fusion。retrievalplan不得直接导入fusion；测试可以组合两者验证标准端口。后续应用服务的装配在入口完成，不能为调用具体实现而扩大编排包的依赖。

contracts、canonical和查询/反思/投影编排包禁止net、net/*、database/*和os/exec。所有当前登记包禁止C、plugin和unsafe，符合首版受控编译注册、重启替换的选择。标准库通过实际GOROOT解析识别，不能因import路径没有点就把它当成标准库。

唯一登记的第三方Go依赖是accessgrant中的go-jose/v4；它是签名实现依赖，禁止泄漏到contracts或其他包。SDK版本仍由go.mod/go.sum及构建锁规则固定。未来供应商SDK应登记在独立适配器包，经过许可、类型和依赖审查后扩展规则。

cmd/architecture-*为离线工具，各入口有精确依赖清单；它们不是正式REST/MCP服务器。允许的数据编解码依赖不授予业务应用访问适配器的权限。

## 2. 可重复检查

检查代码：[dependencies_test.go](../../tests/architecture/dependencies_test.go)。

```powershell
.\.tools\go\bin\go.exe test -count=1 -v ./tests/architecture
.\.tools\go\bin\go.exe test -count=1 ./...
.\.tools\go\bin\go.exe vet ./...
```

扫描使用Go AST读取import，别名、匿名import及注释不会绕过规则。测试替身不受生产依赖清单限制，原因是它们需组合多个实现进行契约对照；该豁免不适用于普通Go文件。新增包需要先明确职责和允许依赖，修改清单并提供拒绝场景，不能直接放行整个模块前缀。

## 3. 本次证据与剩余门槛

87个非测试Go源文件通过扫描。三项测试覆盖生产扫描、九种拒绝场景及允许端口/隔离SDK场景。拒绝场景包含供应商类型、SQL/HTTP耦合、编排依赖具体模块、新包未登记、伪标准库路径、匿名导入、加密SDK泄漏和动态插件。

完整Go测试与vet通过后保存[证据](evidence/dependency-boundaries-review.json)。这不是反射、运行时服务定位器、SQL跨模块读表或配置路由的检测器；尚无业务层、Vue、真实后端适配器及两种实现的迁移/回退证据。进入业务实现时须扩展到internal/和前端的依赖门，结合共同契约与真实替换试验验收。整体阶段B仍部分完成。


2026-10-14登记pkg/contentverify：仅依赖contracts/canonical与标准库，作为标准冻结JSON摘要核验纯包，不允许数据库、HTTP、进程调用或供应商SDK。该登记不证明读取提供者或实际授权行为。
