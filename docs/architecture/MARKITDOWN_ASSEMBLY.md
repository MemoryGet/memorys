# 多格式解析器受控装配

[OfficeFactory](../../pkg/adapters/office_factory.go)将真实OfficeParser挂到既有Parser端口。实现ID platform.markitdown-office，显式依赖blob_store槽位与stream.read能力；Python/worker入口及Health由可信宿主提供，不接受客户端路径或Settings中的可执行文件。

Settings沿用严格max_bytes配置（1..8MiB，额外字段/重复键/错误类型拒绝）。声明format.docx/xlsx/pptx/pdf、source.coarse、projection.markdown；没有声明OCR、精确页码/字符位置或结构化表格。调用所需特性必须先经Kernel.Requirement核对，格式能力声明不代替解析效果验收。

创建要求非空/非typed-nil converter、Health和正确BlobStore依赖。进程Health核对受控绝对Python/脚本入口、MarkItDown 0.1.3和对应转换器依赖；健康请求/输出有界，错误返回通用信息。它没有证明OS隔离、完整模型/字体/制品身份或真实授权，应继续完成ParserDeployment/BuildLock门槛。

实例关闭只使解析门面失效，不关闭宿主文件存储。宿主必须先停止新任务并排空进行中调用；当前没有热替换或强制中断任意子进程树的能力。转换请求仍绑定context/原件摘要/Profile/格式，转换仅生成审核候选。

四种真实格式的离线Go试验已经改为Kernel.Build → Resolve[Parser] → BlobStore读取 →真实Python转换，验证候选来源及关闭后的旧引用拒绝。实验BlobStore/授权是fixture，真实转换器与文件读取代码已执行。

测试拒绝typed-nil converter、额外可执行设置和未声明OCR；OCR测试提供合法BlobStore依赖，避免把缺失依赖错误误认为特性拒绝。

尚未完成外部部署Schema归一化、完整制品哈希/SBOM/许可、真实身份/草稿CAS、结构/OCR、第二独立解析实现同数据替换、孤立进程资源试验和生产Bootstrap。PARSER-DEPLOY及整体生产验收保持开放。


本轮证据：四格式真实装配/解析与关闭引用试验通过；全量Go测试/vet、223项Python及三份OpenAPI检查通过，worker/当前输入哈希与归档一致。正式解析与生产验收保持未通过。

2026-10-15进程边界强化：Convert与Health均要求可信绝对入口及有限context，Convert在启动前拒绝非docx/xlsx/pptx/pdf请求。两类响应均拒绝重复字段、未知字段、嵌套值、尾随JSON及非法UTF-8；转换还要求非空文本与4MiB正文上限。错误只返回通用ErrConversion，不转发子进程诊断。Go子进程测试覆盖真实启动、请求传递、失败退出、输出溢出和超时终止；这些测试使用测试二进制模拟进程协议，真实转换效果另由四格式运行试验验证。它们不证明操作系统沙箱、子进程树终止或内存配额。

运行验证工具使用每次独立的临时目录容纳探针程序、样本和文件存储，构建/转换/证据检查失败也通过finally清理。报告在启动时替换旧成功状态，失败写passed=false；成功要求Go明确返回configured_factory、closed_reference_rejected、对应格式与production_acceptance=false，保存各格式结果、探针程序摘要和全量输入前后摘要。运行中输入变化即拒绝成功报告。资源清理标记仅在cleanup实际返回后记录，不代表工作目录外的子进程树或生产资源已隔离。四项自动化测试核对构建失败/超时清理、不完整证据、源码变化及旧报告失效。
