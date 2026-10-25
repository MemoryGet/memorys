import React, { useEffect, useRef, useState } from 'react';
import { createRoot } from 'react-dom/client';
import { Activity, ArrowRight, BookOpen, CheckCircle2, ChevronRight, FileCheck2, FileText, LoaderCircle, RefreshCw, Search, ShieldCheck, Sparkles, UploadCloud, X } from 'lucide-react';
import './styles.css';

const navigation = [{ id: 'knowledge', label: '我的知识库', icon: BookOpen }, { id: 'search', label: '检索与问答', icon: Search }, { id: 'status', label: '运行状态', icon: Activity }];
const stateNames = { draft: '待发布', submitted: '发布待完成', published: '已发布' };
async function request(path, { body, signal } = {}) {
  const form = body instanceof FormData;
  const response = await fetch(`/local/api/${path}`, { method: body ? 'POST' : 'GET', credentials: 'same-origin', signal,
    headers: body ? { 'X-Memorys-Local': '1', ...(form ? {} : { 'Content-Type': 'application/json' }) } : {}, body: body ? (form ? body : JSON.stringify(body)) : undefined });
  const data = await response.json().catch(() => ({ error: '服务响应不完整，请刷新页面或检查运行状态。' }));
  if (!response.ok || data.error) throw new Error(data.error || '操作未完成，请重试。');
  return data;
}
function Button({ children, primary = false, busy = false, disabled, ...props }) { return <button className={`action-button ${primary ? 'primary' : ''}`} disabled={busy || disabled} {...props}>{busy && <LoaderCircle size={16} className="spin" />}{children}</button>; }
function Status({ state }) { return <span className={`status ${state === 'published' ? 'green' : 'amber'}`}><span className="status-dot" />{stateNames[state] || state}</span>; }
function Empty({ title, text }) { return <div className="empty-state panel"><div className="placeholder-icon"><BookOpen size={27} /></div><h2>{title}</h2><p>{text}</p></div>; }
function Metric({ label, value, icon }) { return <div className="metric-card"><div className="metric-icon blue">{icon}</div><span className="metric-label">{label}</span><strong className="metric-value">{value}</strong></div>; }

function App() {
  const [active, setActive] = useState('knowledge'), [documents, setDocuments] = useState([]), [more, setMore] = useState(false), [info, setInfo] = useState(null);
  const [loading, setLoading] = useState(true), [error, setError] = useState(''), [notice, setNotice] = useState(''), [uploading, setUploading] = useState(false);
  const [preview, setPreview] = useState(null), [previewError, setPreviewError] = useState('');
  const fileInput = useRef(null), toastTimer = useRef(null), page = navigation.find(n => n.id === active);
  async function refresh() {
    setLoading(true); setError('');
    try { const [list, status] = await Promise.all([request('documents'), request('status')]); setDocuments(list.documents); setMore(list.has_more); setInfo(status.checks); return list.documents; }
    catch (e) { setError(e.message); } finally { setLoading(false); }
  }
  useEffect(() => { refresh(); return () => clearTimeout(toastTimer.current); }, []);
  function toast(text) { setNotice(text); clearTimeout(toastTimer.current); toastTimer.current = setTimeout(() => setNotice(''), 5000); }
  async function openPreview(id, published = false) { setPreviewError(''); try { setPreview({ ...(await request('preview', { body: { draft_id: id } })), published }); } catch (e) { setError(e.message); } }
  async function upload(event) {
    const file = event.target.files?.[0]; event.target.value = ''; if (!file) return;
    if (file.size > 8 * 1024 * 1024) { setError('文件不能超过 8 MiB。'); return; }
    setUploading(true); setError('');
    try { const form = new FormData(); form.append('file', file); const imported = await request('import', { body: form }); const current = await refresh(); await openPreview(imported.draft_id, current?.some(d => d.draft_id === imported.draft_id && d.state === 'published')); toast(imported.reused ? '已找到相同内容，复用已有文档。' : '草稿已保存，核对正文后即可发布。'); }
    catch (e) { setError(e.message); } finally { setUploading(false); }
  }
  return <div className="app-shell"><aside className="sidebar"><div className="brand"><div className="brand-mark"><Sparkles size={17} /></div><div><strong>memorys</strong><span>Knowledge OS</span></div></div><div className="workspace-switch"><div className="workspace-avatar">我</div><div><b>个人知识空间</b><span>本地工作台</span></div></div><div className="nav-label">工作台</div><nav>{navigation.map(({ id, label, icon: Icon }) => <button key={id} className={`nav-item ${active === id ? 'active' : ''}`} onClick={() => setActive(id)} aria-current={active === id ? 'page' : undefined}><Icon size={17} /><span>{label}</span></button>)}</nav><div className="sidebar-bottom"><div className="agent-card"><ShieldCheck size={20} /><div><b>个人所有者</b><span>文档经你批准后发布</span></div></div></div></aside>
    <main className="main"><header className="topbar"><div className="breadcrumbs"><span>个人空间</span><ChevronRight size={13} /><b>{page.label}</b></div><div className="top-actions"><span className="muted-text">仅在本机访问</span><button className="icon-btn" onClick={refresh} aria-label="刷新数据" disabled={loading}><RefreshCw size={17} className={loading ? 'spin' : ''} /></button></div></header><div className="content"><div className="hero-row"><div><div className="eyebrow"><span className="eyebrow-line" />YOUR PERSONAL KNOWLEDGE</div><h1>{page.label} <span>✦</span></h1><p>{active === 'knowledge' ? '从一份文档开始，让知识有来源、可检索。' : active === 'search' ? '找到已发布的内容，或根据来源形成答案。' : '查看本地服务与知识库的当前状态。'}</p></div>{active === 'knowledge' && <Button primary busy={uploading} onClick={() => fileInput.current.click()}><UploadCloud size={16} />导入文档</Button>}</div>
    <input ref={fileInput} type="file" aria-label="选择文档" className="file-input" accept=".txt,.md,.markdown,.pdf,.docx,.xlsx,.pptx" onChange={upload} disabled={uploading} />
    {error && <div className="error-banner" role="alert"><span>{error}</span><button onClick={refresh}>重新检查</button><button onClick={() => setError('')} aria-label="关闭错误"><X size={16} /></button></div>}
    {active === 'knowledge' && <><section className="metric-grid local-metrics"><Metric label="知识库" value={info ? '1' : '—'} icon={<BookOpen />} /><Metric label="已发布文档" value={info?.published_documents ?? '—'} icon={<FileCheck2 />} /><Metric label="当前列表待发布" value={loading ? '—' : documents.filter(d => d.state !== 'published').length} icon={<FileText />} /></section><Knowledge documents={documents} loading={loading} more={more} openPreview={openPreview} upload={() => fileInput.current.click()} uploading={uploading} /><div className="scope-note"><ShieldCheck size={15} />草稿仅供预览，明确发布后才会进入检索。支持 TXT、Markdown、PDF、DOCX、XLSX、PPTX。</div></>}
    {active === 'search' && <SearchWorkbench ai={info?.ai_enabled} />}{active === 'status' && <Diagnostics info={info} loading={loading} refresh={refresh} />}<div className="footer-note"><Sparkles size={14} /><span>知识有出处，答案可核对</span></div></div></main>
    {preview && <Preview key={preview.draft_id} value={preview} error={previewError} setError={setPreviewError} close={() => setPreview(null)} published={async () => { setPreview(null); toast('发布完成，现在可以检索这份文档。'); await refresh(); }} />}{notice && <div role="status" className="toast"><CheckCircle2 size={15} />{notice}</div>}</div>;
}
function Knowledge({ documents, loading, more, openPreview, upload, uploading }) {
  const [query, setQuery] = useState(''), [filter, setFilter] = useState('all');
  const filtered = documents.filter(d => (filter === 'all' || (filter === 'published' ? d.state === 'published' : d.state !== 'published')) && d.preview.toLowerCase().includes(query.toLowerCase()));
  return <section className="page-stack"><div className="page-toolbar"><div className="search-field"><Search size={15} /><input value={query} onChange={e => setQuery(e.target.value)} placeholder="筛选当前文档" aria-label="筛选当前文档" /></div><div className="segmented">{[['all', '全部'], ['draft', '待发布'], ['published', '已发布']].map(([id, name]) => <button key={id} className={filter === id ? 'selected' : ''} onClick={() => setFilter(id)}>{name}</button>)}</div></div>
    {loading ? <Empty title="正在读取知识库…" text="正在核对当前文档与权限。" /> : filtered.length ? <div className="document-list panel">{filtered.map(d => <article className="document-item" key={d.draft_id}><div className={`library-icon ${d.state === 'published' ? 'green' : 'cyan'}`}><FileText size={20} /></div><div className="document-summary"><h2>{d.preview.split(/\r?\n/).find(Boolean)?.slice(0, 72) || '文档'}</h2><p>{d.preview}</p><small>{d.format.toUpperCase()} · <Status state={d.state} /></small></div><Button onClick={() => openPreview(d.draft_id, d.state === 'published')}>{d.state === 'published' ? '查看正文' : '预览并发布'}<ArrowRight size={14} /></Button></article>)}</div> : <><Empty title={documents.length ? '没有符合条件的文档' : '你的第一份知识，等你导入'} text={documents.length ? '调整关键词或文档状态后再试。' : '选择一份文件，预览正文，然后决定是否发布。'} />{!documents.length && <Button primary onClick={upload} busy={uploading}><UploadCloud size={16} />选择文件</Button>}</>}{more && <p className="hint">仅显示前 100 份文档；其余文档仍可通过已发布内容检索。</p>}</section>;
}
function Preview({ value, close, published, error, setError }) {
  const [approved, setApproved] = useState(false), [busy, setBusy] = useState(false), panel = useRef(null), previous = useRef(null);
  useEffect(() => { previous.current = document.activeElement; panel.current?.focus(); return () => previous.current?.focus?.(); }, []);
  function keys(event) {
    if (event.key === 'Escape' && !busy) close(); if (event.key !== 'Tab') return;
    const elements = [...panel.current.querySelectorAll('button:not(:disabled),input:not(:disabled)')], first = elements[0], last = elements.at(-1);
    if (event.shiftKey && (document.activeElement === first || document.activeElement === panel.current)) { event.preventDefault(); last?.focus(); } else if (!event.shiftKey && document.activeElement === last) { event.preventDefault(); first?.focus(); }
  }
  async function publish() { setBusy(true); setError(''); try { await request('publish', { body: { draft_id: value.draft_id, expect_hash: value.content_hash, approved } }); await published(); } catch (e) { setError(`${e.message} 可用同一草稿重试。`); } finally { setBusy(false); } }
  return <div className="modal-backdrop"><section className="preview-modal panel" role="dialog" aria-modal="true" aria-labelledby="preview-title" tabIndex={-1} ref={panel} onKeyDown={keys}><div className="panel-header"><div><h2 id="preview-title">{value.published ? '已发布正文' : '核对正文并发布'}</h2><span>发布的将是下方这份内容。</span></div><button onClick={close} disabled={busy} aria-label="关闭预览"><X size={20} /></button></div><pre className="document-preview">{value.text}</pre><details className="source-details"><summary>文档标识与内容摘要</summary><dl><dt>草稿</dt><dd>{value.draft_id}</dd><dt>SHA-256</dt><dd data-testid="preview-hash">{value.content_hash}</dd></dl></details>{error && <div role="alert" className="error-banner">{error}</div>}{!value.published && <label className="approve-label"><input type="checkbox" checked={approved} onChange={e => setApproved(e.target.checked)} disabled={busy} />我已核对全文，批准发布这份内容</label>}<div className="modal-actions"><Button onClick={close} disabled={busy}>关闭</Button>{!value.published && <Button primary onClick={publish} disabled={!approved} busy={busy}>{busy ? '正在发布…' : '批准并发布'}</Button>}</div></section></div>;
}
function SearchWorkbench({ ai }) {
  const [query, setQuery] = useState(''), [mode, setMode] = useState('semantic'), [busy, setBusy] = useState(false), [error, setError] = useState(''), [result, setResult] = useState(null), controller = useRef(null);
  useEffect(() => () => controller.current?.abort(), []);
  async function search(event) {
    event.preventDefault(); if (!query.trim() || busy) return; controller.current?.abort(); const current = new AbortController(); controller.current = current; setBusy(true); setError(''); setResult(null); const timeout = setTimeout(() => current.abort(), 125000);
    try { setResult(await request('search', { body: { query, mode }, signal: current.signal })); } catch (e) { setError(e.name === 'AbortError' ? '请求已取消或超时，可以重试。' : e.message); } finally { clearTimeout(timeout); setBusy(false); }
  }
  return <section className="page-stack"><form className="panel query-panel" onSubmit={search}><div className="segmented" aria-label="检索方式">{[['semantic', '语义检索'], ['keyword', '关键词检索'], ['answer', '知识问答']].map(([id, name]) => <button type="button" key={id} className={id === mode ? 'selected' : ''} onClick={() => { setMode(id); setResult(null); setError(''); }} disabled={busy}>{name}</button>)}</div><div className="query-input"><Search size={20} /><input aria-label="检索问题" placeholder="例如：遇到数据库故障应该怎样恢复？" value={query} onChange={e => setQuery(e.target.value)} required maxLength={4096} disabled={busy} /><Button primary type="submit" disabled={!query.trim()} busy={busy}>{busy ? '正在查询…' : mode === 'answer' ? '生成答案' : '开始检索'}</Button></div><p className="hint">{mode === 'answer' ? '问答会将问题及当前获权的文档片段发送给 DeepSeek；答案附来源供你核对。' : mode === 'semantic' ? '在本机理解问题含义，检索你已发布的内容。' : '按已发布正文中的关键词检索；中文关键词需与正文匹配。'}</p>{ai === false && mode !== 'keyword' && <p className="hint">尚未启用 AI，请先在本机运行 enable-ai，或使用关键词检索。</p>}</form>
    {busy && <div className="busy-line" role="status"><LoaderCircle className="spin" size={18} />{mode === 'answer' ? '正在检索来源并生成答案…' : '正在检索已发布文档…'}<button onClick={() => controller.current?.abort()}>取消</button></div>}{error && <div role="alert" className="error-banner">{error}</div>}
    {result && <>{result.mode === 'answer' && <article className="panel answer-panel"><div className="result-label"><Sparkles size={16} />{result.model || '知识问答'}</div><h2>{result.status === 'insufficient_evidence' ? '现有证据不足' : '根据文档的回答'}</h2><p className="answer-text">{result.answer}</p>{result.citations?.length > 0 && <div className="answer-citations">{result.citations.map((citation, i) => { const found = result.evidence.findIndex(e => e.citation.document_id === citation.document_id && e.citation.revision_id === citation.revision_id && e.citation.block_id === citation.block_id); return <a key={i} href={`#evidence-${found}`}><FileCheck2 size={14} />来源 {found + 1}</a>; })}</div>}</article>}{result.evidence.length ? <section className="page-stack" aria-label="检索来源"><h2 className="section-title">检索来源 <span>{result.evidence.length}</span></h2>{result.evidence.map((e, i) => <Evidence key={`${e.citation.block_id}-${i}`} item={e} index={i} />)}</section> : <Empty title="没有找到可用的已发布内容" text="调整问题，或先导入并发布相关文档。" />}</>}{!result && !busy && !error && <Empty title="开始一次有来源的检索" text="输入问题，查看真实正文和来源引用。" />}</section>;
}
function Evidence({ item, index }) { return <article className="panel evidence-card" id={`evidence-${index}`}><div className="evidence-heading"><span className="source-number">{String(index + 1).padStart(2, '0')}</span><h3>{item.source_excerpt.split(/\r?\n/).find(Boolean)?.slice(0, 90)}</h3><span className="muted-text">{item.citation.source_kind === 'original' ? '原件文本' : '发布正文'}</span></div><p>{item.source_excerpt}</p><details className="source-details"><summary>核对引用</summary><dl><dt>文档</dt><dd>{item.citation.document_id}</dd><dt>版本</dt><dd>{item.citation.revision_id}</dd><dt>内容块</dt><dd>{item.citation.block_id}</dd></dl><p className="hint">当前为正文级引用，未提供精确页码或单元格位置。</p></details></article>; }
function Diagnostics({ info, loading, refresh }) {
  const names = { database: '数据库连接', schema: '数据库版本记录', identity: '本地身份', permissions: '当前权限', configuration: '知识库配置', searchable: '有已发布内容', office_parser: '文档解析器', embedding_model: '本地嵌入模型', answer_key_configured: 'DeepSeek 密钥已配置' };
  return <section className="page-stack"><div className="panel-header"><div><h2>当前检查</h2><span>只读取状态，不修改配置或运行模型问答。</span></div><Button onClick={refresh} busy={loading}><RefreshCw size={15} />重新检查</Button></div>{!info ? <Empty title="暂未读取到运行状态" text="请重试，或检查终端中的服务日志。" /> : <div className="panel">{Object.entries(names).filter(([key]) => key in info).map(([key, name]) => <div className="diagnostic-row" key={key}><span>{name}</span><span className={`status ${info[key] ? 'green' : 'amber'}`}><span className="status-dot" />{info[key] ? '通过' : '待处理'}</span></div>)}{info.embedding && <p className="hint">嵌入模型：{info.embedding.ProviderModel} · {info.embedding.Dimension} 维</p>}<p className="hint">密钥是否配置不代表远程服务一定可用。当前提供本地功能，不代表生产验收通过。</p></div>}</section>;
}
createRoot(document.getElementById('root')).render(<App />);
