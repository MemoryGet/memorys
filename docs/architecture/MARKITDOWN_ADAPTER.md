# MarkItDown成熟组件接入试验

已实现[Go OfficeParser](../../pkg/parser/office_parser.go)、[进程适配器](../../pkg/parserprocess/markitdown.go)和[Python worker](../../tools/parsers/markitdown_worker.py)。固定MarkItDown 0.1.3，实验环境在.local/markitdown-env，仅安装docx/pptx/xlsx/pdf extras；实际依赖版本见[锁定清单](markitdown-requirements.lock.txt)。

## 组件核验与选择

[微软官方仓库](https://github.com/microsoft/markitdown)说明可将Office/PDF等文件转为Markdown，并建议使用较窄的convert_stream/convert_local入口。[0.1.3源码](https://github.com/microsoft/markitdown/blob/v0.1.3/packages/markitdown/src/markitdown/_markitdown.py)提供二进制流与StreamInfo；[导出API](https://github.com/microsoft/markitdown/blob/v0.1.3/packages/markitdown/src/markitdown/__init__.py)包含MarkItDown/StreamInfo。

选择它作为Office文本候选适配，不将Markdown等同于高保真结构解析。[Docling官方格式说明](https://docling-project.github.io/docling/usage/supported_formats/)覆盖同类输入，后续作为结构化/精确定位候选；本轮未安装或验证Docling。

## 平台映射

Go只通过BlobStore读取授权原件，核对完整字节长度与SHA-256，再调用OfficeConverter。纯parser包不导入进程或供应商SDK；parserprocess隔离Python启动与有界协议。输出为一个标准TextBlock候选，绑定原件BlobID并标注coarse来源，不伪造页面/单元格/原始字符范围，也不创建发布Revision。

只接受docx/xlsx/pptx/pdf，要求有限请求预算；OCR请求明确拒绝。ID绑定Draft、原件摘要、格式、ProfileDigest与转换版本。输出诊断说明粗粒度Markdown未提供精确表格/页/单元格语义，必须进入编辑审核流程。

## 实际运行约束

worker仅接收bytes/base64和固定格式，不接收URL/原件路径；禁用通用内置/插件，仅登记对应格式转换器。只允许MarkItDown 0.1.3。输入8MiB、OOXML条目1000/展开64MiB，输出4MiB；Go捕获输出有上限，进程失败返回通用错误，不暴露供应商异常。协议明确UTF-8，避免Windows默认编码损坏中文。

worker封锁Python socket连接，未启用LLM/云识别。该措施不是OS级网络/文件系统沙箱。CommandContext取消当前Python进程并限制等待，但尚无完整子进程树、CPU/RSS和临时盘配额证明；生产需要隔离部署与PARSER-DEPLOY资源试验。

## 证据与范围

真实转换及Go BlobStore→OfficeParser→Python进程→MarkItDown链路，分别运行中文DOCX、中文XLSX、中文PPTX和ASCII原生文本PDF，检查预期内容、原件绑定及coarse定位。测试权限与文件为fixture，不是生产授权。首次运行暴露Windows输出编码问题，已改为UTF-8字节并复验。

依赖安装首个PyPI任务在Magika轮子下载时反复超时，核对自身进程并主动停止，原句柄终止后才切换清华镜像；未并行安装或改系统Python。锁文件记录实际版本，不是wheel哈希、完整SBOM/商业许可或漏洞审计证明。

没有验证中文/扫描/多栏PDF、页级定位、合并表格/单位/公式/隐藏sheet保真、附件候选、真实草稿CAS/任务fencing或第二解析实现替换。Go单元检查拒绝坏摘要/OCR，并确保不报告exact源位置；正式PARSER-DEPLOY-01..06、整体V1和生产验收仍开放。

真实试验入口：.local/markitdown-env/Scripts/python.exe docs/architecture/check_markitdown_runtime.py。运行器捕获worker/输入/输出SHA和Go链路结果，临时文件与探针二进制在成功后清理。失败路径清理、全解析部署包身份和受控注册工厂尚需完善。


本轮证据：四类真实转换及Go解析/文件存储链路通过；全量Go测试/vet、223项Python和三份OpenAPI检查通过。worker/依赖锁及当前源文件哈希已归档。实际转换证据见evidence/markitdown-runtime-review.json，完整解析/生产验收仍未通过。


已补[多格式受控装配](MARKITDOWN_ASSEMBLY.md)，四类真实格式通过Kernel构建和Parser解析端口，并验证关闭引用拒绝；实际转换能力范围保持粗粒度候选。
