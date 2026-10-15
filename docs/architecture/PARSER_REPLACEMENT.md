# PDF解析器双实现替换验证

平台Parser端口现有两个独立文本抽取后端：MarkItDown 0.1.3（PDF使用pdfminer）与pypdf 6.19.0。两者使用各自venv，依赖分别固定在markitdown-requirements.lock.txt与pypdf-requirements.lock.txt；版本锁不等于制品签名、SBOM或完整许可审查。

`OfficeFactory`与`PyPDFFactory`复用标准BlobStore读取、摘要核验、候选Block和受控生命周期。后者只声明format.pdf/source.coarse/projection.text，不能替代要求Office、Markdown结构或OCR的流水线。工厂选择和进程入口由可信宿主提供；Settings仍不允许传可执行路径。两个进程实现共享有界请求/响应、有限context与严格JSON校验，后端版本分别固定核对。

原件仍由平台拥有。更换后端生成新的候选结果，块标识包含后端版本，不能覆盖已发布Revision；当前仅验证解析候选，未连接审核CAS或正式部署配置。粗粒度来源继续指向原Blob，不伪造页码、字符或单元格定位。

使用MarkItDown环境执行：

```powershell
.local/markitdown-env/Scripts/python.exe docs/architecture/check_parser_replacement.py
```

同一个Go调用入口通过provider配置选择后端，依次重建运行实例。三份原生文本ASCII PDF在两个独立引擎上运行，核对规范化正文、原件SHA、来源精度、具体后端版本、工厂装配、关闭后的旧引用拒绝及损坏摘要拒绝。输出位于`.local/architecture-check/parser-replacement.json`，包含输入和探针摘要。独立临时目录容纳每次探针及每份原件的存储，避免不同内容复用fixture幂等键。

pypdf worker拒绝非PDF、加密PDF、空文本层及超过200页/8MiB输入/64MiB解码内容/4MiB输出的资料。解码后大小检查不能阻止解压前的内存耗尽，Python socket拦截也不是OS网络隔离；仍需进程树、CPU/内存配额与外部网络沙箱。

本试验只覆盖PDF文本能力交集。它不证明中文复杂排版、扫描件、表格结构、质量/延迟、全量Parser公共契约、管理界面/REST/MCP兼容、候选审核发布、正式部署切换或迁移回退。第二解析实现已实际接入，但完整解析替换验收保持开放。

官方依据：[pypdf文本抽取及限制](https://pypdf.readthedocs.io/en/stable/user/extract-text.html)、[固定版本许可](https://github.com/py-pdf/pypdf/blob/6.19.0/LICENSE)。该引擎没有OCR能力。

公共契约增量：同一套Go测试通过两个工厂和实际LocalBlobStore核对十三类非法请求（缺失/错误配置摘要、请求/原件/草稿标识、摘要/长度错误、空间/动作/访问范围错误、期限过期及OCR）、授权拒绝不进入转换器、候选无虚构发布引用、稳定块ID、配置变化隔离和关闭引用拒绝。转换器和授权回调是fixture，不能据此宣称真实IAM或成熟引擎的全部契约通过。Office/PDF请求现在必须携带非空请求/原件ID和规范的小写SHA256流水线摘要，缺失请求在读取原件前拒绝。

实际后端负例：使用pypdf Writer生成空文本和加密PDF，另构造截断PDF，以正确摘要调用两个真实worker。六次调用全部非零退出且标准输出为空；negative_records保存样本摘要与拒绝结果。空文本样本不是扫描图像，不能把这个结果当作扫描OCR质量验收。
