import React, { useEffect, useRef, useState } from 'react';
import { createRoot } from 'react-dom/client';
import { Activity, ArrowRight, BookOpen, CheckCircle2, ChevronRight, FileCheck2, FileText, LoaderCircle, RefreshCw, Search, ShieldCheck, Sparkles, UploadCloud, X } from 'lucide-react';
import './styles.css';

const navigation = [{ id: 'knowledge', label: '我的知识库', icon: BookOpen }, { id: 'search', label: '检索与问答', icon: Search }, { id: 'status', label: '运行状态', icon: Activity }];
const stateNames = { draft: '待发布', submitted: '待审核', approved: '已批准', rejected: '已拒绝', published: '已发布', queued: '排队中', running: '处理中', paused: '已暂停', completed: '已完成', failed: '失败', cancelled: '已取消' };
async function request(path, { body, signal } = {}) {
  const form = body instanceof FormData;
  const response = await fetch(`/local/api/${path}`, { method: body ? 'POST' : 'GET', credentials: 'same-origin', signal,
    headers: body ? { 'X-Memorys-Local': '1', ...(form ? {} : { 'Content-Type': 'application/json' }) } : {}, body: body ? (form ? body : JSON.stringify(body)) : undefined });
  const data = await response.json().catch(() => ({ error: '服务响应不完整，请刷新页面或检查运行状态。' }));
  if (!response.ok || data.error) throw new Error(data.error || '操作未完成，请重试。');
  return data;
}
function Button({ children, primary = false, busy = false, disabled, ...props }) { return <button className={`action-button ${primary ? 'primary' : ''}`} disabled={busy || disabled} {...props}>{busy && <LoaderCircle size={16} className="spin" />}{children}</button>; }
function Status({ state }) { return <span className={`status ${state === 'published' || state === 'completed' ? 'green' : state === 'failed' ? 'red' : 'amber'}`}><span className="status-dot" />{stateNames[state] || state}</span>; }
function Empty({ title, text }) { return <div className="empty-state panel"><div className="placeholder-icon"><BookOpen size={27} /></div><h2>{title}</h2><p>{text}</p></div>; }
function Metric({ label, value, icon }) { return <div className="metric-card"><div className="metric-icon blue">{icon}</div><span className="metric-label">{label}</span><strong className="metric-value">{value}</strong></div>; }

function App() {
  const [active, setActive] = useState('knowledge'), [documents, setDocuments] = useState([]), [more, setMore] = useState(false), [documentCursor, setDocumentCursor] = useState(''), [documentTag, setDocumentTag] = useState(''), [tagSummary, setTagSummary] = useState([]), [tasks, setTasks] = useState([]), [tasksMore, setTasksMore] = useState(false), [info, setInfo] = useState(null);
  const [loading, setLoading] = useState(true), [error, setError] = useState(''), [notice, setNotice] = useState(''), [uploading, setUploading] = useState(false);
  const [preview, setPreview] = useState(null), [previewError, setPreviewError] = useState(''), [cleanupTask, setCleanupTask] = useState(null), [cleanupPlan, setCleanupPlan] = useState(null);
  const fileInput = useRef(null), toastTimer = useRef(null), page = navigation.find(n => n.id === active);
  function documentsPath(tag = documentTag, cursor = '') {
    const params = new URLSearchParams();
    if (tag.trim()) params.set('tags', tag.trim());
    if (cursor) params.set('cursor', cursor);
    const query = params.toString();
    return query ? `documents?${query}` : 'documents';
  }
  async function refresh() {
    setLoading(true); setError('');
    try { const [list, status, tags] = await Promise.all([request(documentsPath()), request('status'), request('tags').catch(() => ({ tags: [] }))]); setDocuments(list.documents); setMore(list.has_more); setDocumentCursor(list.next_cursor || ''); setInfo(status.checks); setTagSummary(tags.tags || []); try { const taskList = await request('jobs'); setTasks(taskList.tasks || []); setTasksMore(Boolean(taskList.has_more)); } catch (_) {} return list.documents; }
    catch (e) { setError(e.message); } finally { setLoading(false); }
  }
  async function changeDocumentTag(value) {
    setDocumentTag(value); setDocumentCursor(''); setDocuments([]); setMore(false); setLoading(true); setError('');
    try { const list = await request(documentsPath(value)); setDocuments(list.documents || []); setMore(Boolean(list.has_more)); setDocumentCursor(list.next_cursor || ''); }
    catch (e) { setError(e.message); } finally { setLoading(false); }
  }
  async function loadMoreDocuments() {
    if (!documentCursor || loading) return;
    try { const list = await request(documentsPath(documentTag, documentCursor)); setDocuments(items => [...items, ...(list.documents || [])]); setMore(Boolean(list.has_more)); setDocumentCursor(list.next_cursor || ''); }
    catch (e) { setError(e.message); }
  }
  useEffect(() => { refresh(); return () => clearTimeout(toastTimer.current); }, []);
  function toast(text) { setNotice(text); clearTimeout(toastTimer.current); toastTimer.current = setTimeout(() => setNotice(''), 5000); }
  async function openPreview(id, published = false) { setPreviewError(''); try { setPreview({ ...(await request('preview', { body: { draft_id: id } })), published }); } catch (e) { setError(e.message); } }
  async function upload(event) {
    const files = [...(event.target.files || [])]; event.target.value = ''; if (!files.length) return;
    setUploading(true); setError('');
    try {
      const imported = [], failed = [], accepted = files.slice(0, 20), form = new FormData();
      for (const file of accepted) form.append('file', file, file.name);
      if (files.length > accepted.length) failed.push(`最多一次导入 ${accepted.length} 个文件`);
      const batch = await request('import/batch', { body: form });
      for (const item of batch.items || []) {
        if (item.result) imported.push(item.result);
        if (item.error) failed.push(`${item.filename || '文件'}：${item.error}`);
      }
      const current = await refresh();
      if (imported.length) await openPreview(imported[0].draft_id, current?.some(d => d.draft_id === imported[0].draft_id && d.state === 'published'));
      toast(`已处理 ${imported.length} 个文件${failed.length ? `，${failed.length} 个失败` : ''}。`);
      if (failed.length) setError(failed.join('；'));
    } catch (e) { setError(e.message); } finally { setUploading(false); }
  }
  return <div className="app-shell"><aside className="sidebar"><div className="brand"><div className="brand-mark"><Sparkles size={17} /></div><div><strong>memorys</strong><span>Knowledge OS</span></div></div><div className="workspace-switch"><div className="workspace-avatar">我</div><div><b>个人知识空间</b><span>本地工作台</span></div></div><div className="nav-label">工作台</div><nav>{navigation.map(({ id, label, icon: Icon }) => <button key={id} className={`nav-item ${active === id ? 'active' : ''}`} onClick={() => setActive(id)} aria-current={active === id ? 'page' : undefined}><Icon size={17} /><span>{label}</span></button>)}</nav><div className="sidebar-bottom"><div className="agent-card"><ShieldCheck size={20} /><div><b>个人所有者</b><span>文档经你批准后发布</span></div></div></div></aside>
    <main className="main"><header className="topbar"><div className="breadcrumbs"><span>个人空间</span><ChevronRight size={13} /><b>{page.label}</b></div><div className="top-actions"><span className="muted-text">仅在本机访问</span><button className="icon-btn" onClick={refresh} aria-label="刷新数据" disabled={loading}><RefreshCw size={17} className={loading ? 'spin' : ''} /></button></div></header><div className="content"><div className="hero-row"><div><div className="eyebrow"><span className="eyebrow-line" />YOUR PERSONAL KNOWLEDGE</div><h1>{page.label} <span>✦</span></h1><p>{active === 'knowledge' ? '从一份文档开始，让知识有来源、可检索。' : active === 'search' ? '找到已发布的内容，或根据来源形成答案。' : '查看本地服务与知识库的当前状态。'}</p></div>{active === 'knowledge' && <Button primary busy={uploading} onClick={() => fileInput.current.click()}><UploadCloud size={16} />导入文档</Button>}</div>
    <input ref={fileInput} type="file" aria-label="选择文档" className="file-input" multiple accept=".txt,.md,.markdown,.pdf,.docx,.xlsx,.pptx,.png,.jpg,.jpeg,.wav,.mp3,.m4a" onChange={upload} disabled={uploading} />
    {error && <div className="error-banner" role="alert"><span>{error}</span><button onClick={refresh}>重新检查</button><button onClick={() => setError('')} aria-label="关闭错误"><X size={16} /></button></div>}
    {active === 'knowledge' && <><section className="metric-grid local-metrics"><Metric label="知识库" value={info ? '1' : '—'} icon={<BookOpen />} /><Metric label="已发布文档" value={info?.published_documents ?? '—'} icon={<FileCheck2 />} /><Metric label="当前列表待发布" value={loading ? '—' : documents.filter(d => d.state !== 'published').length} icon={<FileText />} /></section>{cleanupTask && <CleanupTask task={cleanupTask} setTask={setCleanupTask} />}{cleanupPlan && <CleanupPlanTask task={cleanupPlan} setTask={setCleanupPlan} />}{tasks.length > 0 && <TaskList tasks={tasks} more={tasksMore} />}<Knowledge documents={documents} loading={loading} more={more} loadMore={loadMoreDocuments} openPreview={openPreview} upload={() => fileInput.current.click()} uploading={uploading} refresh={refresh} notify={toast} tagFilter={documentTag} tagSummary={tagSummary} onTagFilterChange={changeDocumentTag} /><div className="scope-note"><ShieldCheck size={15} />草稿仅供预览，明确发布后才会进入检索。支持 TXT、Markdown、PDF、DOCX、XLSX、PPTX、PNG、JPG、WAV、MP3、M4A；图片 OCR 需要 Tesseract，音频转写需要 whisper。</div></>}
    {active === 'search' && <SearchWorkbench ai={info?.ai_enabled} notify={toast} />}{active === 'status' && <><Diagnostics info={info} loading={loading} refresh={refresh} /><MetricsPanel /><FeedbackPanel notify={toast} openPreview={openPreview} /><AuditPanel /></>}<div className="footer-note"><Sparkles size={14} /><span>知识有出处，答案可核对</span></div></div></main>
    {preview && <Preview key={preview.draft_id} value={preview} error={previewError} setError={setPreviewError} close={() => setPreview(null)} saved={async item => { setPreview(null); toast('新草稿已保存，可以继续核对或发布。'); await refresh(); await openPreview(item.draft_id, false); }} published={async () => { setPreview(null); toast('发布完成，现在可以检索这份文档。'); await refresh(); }} withdrawn={async result => { setPreview(null); setCleanupPlan({ document_id: result.document_id }); toast('文档已撤回，检索索引正在清理。'); await refresh(); }} deleted={async result => { setPreview(null); setCleanupTask({ document_id: result.document_id, job: { job_id: result.job_id, state: 'queued', progress: 0, attempt: 0, row_version: 1 } }); toast('文档已删除，后台正在清理索引和缓存。'); await refresh(); }} />}{notice && <div role="status" className="toast"><CheckCircle2 size={15} />{notice}</div>}</div>;
}
function CleanupTask({ task, setTask }) {
  const [error, setError] = useState(''), [busy, setBusy] = useState(false);
  useEffect(() => {
    let stopped = false;
    const poll = async () => { try { const data = await request(`delete-jobs/${task.document_id}`); if (!stopped && data.job) setTask({ ...task, job: data.job }); } catch (e) { if (!stopped) setError(e.message); } };
    poll();
    if (task.job?.state === 'completed' || task.job?.state === 'cancelled') return () => { stopped = true; };
    const timer = setInterval(poll, 1500);
    return () => { stopped = true; clearInterval(timer); };
  }, [task.document_id, task.job?.state, task.job?.row_version]);
  async function control(path) {
    if (!task.job?.job_id || !task.job?.row_version || busy) return;
    setBusy(true); setError('');
    try { const data = await request(`delete-jobs/${path}`, { body: { document_id: task.document_id, job_id: task.job.job_id, expect_version: task.job.row_version } }); setTask({ ...task, job: data.job }); }
    catch (e) { setError(e.message); }
    finally { setBusy(false); }
  }
  const job = task.job || {}, terminal = job.state === 'completed' || job.state === 'cancelled';
  return <section className="panel cleanup-task" aria-live="polite"><div><div className="result-label"><RefreshCw size={15} className={terminal ? '' : 'spin'} />索引清理任务</div><p>文档 {task.document_id} · {stateNames[job.state] || job.state || '查询中'} · 第 {job.attempt || 0} 次尝试 · {Math.round((job.progress || 0) * 100)}%</p>{job.error?.message && <p className="error-text">{job.error.message}</p>}{error && <p className="error-text">{error}</p>}</div><div className="cleanup-actions">{job.state === 'failed' && <Button onClick={() => control('retry')} busy={busy}><RefreshCw size={15} />重试</Button>}{(job.state === 'queued' || job.state === 'running' || job.state === 'failed') && <Button onClick={() => control('cancel')} busy={busy}>取消任务</Button>}<Button onClick={() => setTask(null)}>关闭</Button></div></section>;
}
function CleanupPlanTask({ task, setTask }) {
  const [plan, setPlan] = useState(null), [error, setError] = useState('');
  useEffect(() => {
    let stopped = false;
    const poll = async () => { try { const data = await request(`cleanup/${task.document_id}`); if (!stopped) setPlan(data.plan); } catch (e) { if (!stopped) setError(e.message); } };
    poll();
    if (plan?.state === 'completed' || plan?.state === 'failed') return () => { stopped = true; };
    const timer = setInterval(poll, 1500);
    return () => { stopped = true; clearInterval(timer); };
  }, [task.document_id, plan?.state, plan?.completed]);
  const terminal = plan?.state === 'completed' || plan?.state === 'failed';
  return <section className="panel cleanup-task" aria-live="polite"><div><div className="result-label"><RefreshCw size={15} className={terminal ? '' : 'spin'} />撤回清理任务</div><p>文档 {task.document_id} · {stateNames[plan?.state] || plan?.state || '查询中'} · {plan ? `${plan.completed}/${plan.total} 个目标` : '正在读取任务'}</p>{error && <p className="error-text">{error}</p>}</div><div className="cleanup-actions"><Button onClick={() => setTask(null)}>关闭</Button></div></section>;
}
function TaskList({ tasks, more }) {
  const [rows, setRows] = useState(tasks), [busy, setBusy] = useState(''), [filter, setFilter] = useState('all'), [createdAfter, setCreatedAfter] = useState(''), [createdBefore, setCreatedBefore] = useState(''), [controlError, setControlError] = useState('');
  function jobsPath() {
    const params = new URLSearchParams();
    if (createdAfter) params.set('created_after', `${createdAfter}T00:00:00Z`);
    if (createdBefore) { const end = new Date(`${createdBefore}T00:00:00Z`); end.setUTCDate(end.getUTCDate() + 1); params.set('created_before', end.toISOString()); }
    const query = params.toString();
    return query ? `jobs?${query}` : 'jobs';
  }
  useEffect(() => { setRows(tasks); }, [tasks]);
  useEffect(() => {
    let stopped = false, fallback;
    const refresh = async () => { try { const data = await request(jobsPath()); if (!stopped && data.tasks) setRows(data.tasks); } catch (_) {} };
    const stream = typeof EventSource === 'undefined' ? null : new EventSource('/local/api/jobs/events');
    if (!stream) { fallback = setInterval(refresh, 2500); return () => { stopped = true; clearInterval(fallback); }; }
    stream.addEventListener('tasks', event => { try { const data = JSON.parse(event.data); if (!stopped && data.tasks) setRows(data.tasks); } catch (_) {} });
    stream.onerror = () => { if (!fallback && !stopped) { stream.close(); refresh(); fallback = setInterval(refresh, 2500); } };
    return () => { stopped = true; stream.close(); clearInterval(fallback); };
  }, [createdAfter, createdBefore]);
  async function retry(item) { if (busy || !item.job?.job_id || !item.job?.row_version || !['publication', 'parse'].includes(item.kind) || !['failed', 'paused'].includes(item.job.state)) return; setBusy(item.job.job_id); setControlError(''); try { if (item.kind === 'parse') await request('parse-jobs/retry', { body: { task_id: item.job.job_id, expect_version: item.job.row_version } }); else if (item.draft_id && item.content_hash) await request('retry', { body: { draft_id: item.draft_id, expect_hash: item.content_hash } }); const data = await request(jobsPath()); setRows(data.tasks || []); } catch (error) { setControlError(error.message); } finally { setBusy(''); } }
  async function pause(item) { if (busy || !item.job?.job_id || !item.job?.row_version || !['publication', 'parse'].includes(item.kind) || !['queued', 'running'].includes(item.job.state)) return; setBusy(item.job.job_id); setControlError(''); try { if (item.kind === 'parse') await request('parse-jobs/pause', { body: { task_id: item.job.job_id, expect_version: item.job.row_version } }); else if (item.kind === 'publication' && item.draft_id) await request('pause', { body: { draft_id: item.draft_id, job_id: item.job.job_id, expect_version: item.job.row_version } }); const data = await request(jobsPath()); setRows(data.tasks || []); } catch (error) { setControlError(error.message); } finally { setBusy(''); } }
  async function cancel(item) { if (busy || !item.job?.job_id || !item.job?.row_version || !['queued', 'running'].includes(item.job.state)) return; setBusy(item.job.job_id); setControlError(''); try { if (item.kind === 'parse') await request('parse-jobs/cancel', { body: { task_id: item.job.job_id, expect_version: item.job.row_version } }); else if (item.kind === 'publication' && item.draft_id) await request('cancel', { body: { draft_id: item.draft_id, job_id: item.job.job_id, expect_version: item.job.row_version } }); const data = await request(jobsPath()); setRows(data.tasks || []); } catch (error) { setControlError(error.message); } finally { setBusy(''); } }
  const visible = rows.filter(item => { if (filter !== 'all' && filter !== item.kind && filter !== item.job.state) return false; const created = item.job?.created_at ? new Date(item.job.created_at) : null; if (createdAfter && (!created || Number.isNaN(created.getTime()) || created < new Date(`${createdAfter}T00:00:00Z`))) return false; if (createdBefore && (!created || Number.isNaN(created.getTime()) || created >= new Date(`${createdBefore}T00:00:00Z`).setUTCDate(new Date(`${createdBefore}T00:00:00Z`).getUTCDate() + 1))) return false; return true; });
  return <section className="panel task-list"><div className="panel-header"><div><h2>异步任务</h2><span>解析、发布与删除清理任务的当前状态。</span></div><div className="segmented"><button className={filter === 'all' ? 'selected' : ''} onClick={() => setFilter('all')}>全部</button><button className={filter === 'publication' ? 'selected' : ''} onClick={() => setFilter('publication')}>发布</button><button className={filter === 'deletion' ? 'selected' : ''} onClick={() => setFilter('deletion')}>清理</button><button className={filter === 'parse' ? 'selected' : ''} onClick={() => setFilter('parse')}>解析</button><button className={filter === 'paused' ? 'selected' : ''} onClick={() => setFilter('paused')}>已暂停</button><button className={filter === 'failed' ? 'selected' : ''} onClick={() => setFilter('failed')}>失败</button></div></div><div className="task-date-filters"><label>创建起点<input type="date" aria-label="任务创建起点" value={createdAfter} onChange={e => setCreatedAfter(e.target.value)} /></label><label>创建终点<input type="date" aria-label="任务创建终点" value={createdBefore} onChange={e => setCreatedBefore(e.target.value)} /></label></div>{controlError && <p className="error-text" role="alert">{controlError}</p>}{visible.map((item, index) => <details className="task-row task-detail" key={`${item.kind}-${item.job.job_id}-${index}`}><summary><div><b>{item.kind === 'publication' ? '发布' : item.kind === 'parse' ? '解析' : '删除清理'}</b><span>文档 {item.document_id || '待解析绑定'}</span></div><div className="task-state"><Status state={item.job.state} /><span>{Math.round((item.job.progress || 0) * 100)}%</span></div></summary><div className="task-detail-body"><dl><dt>任务 ID</dt><dd>{item.job.job_id}</dd><dt>阶段</dt><dd>{item.job.stage || '—'}</dd><dt>创建时间</dt><dd>{item.job.created_at ? new Date(item.job.created_at).toLocaleString() : '—'}</dd><dt>尝试次数</dt><dd>{item.job.attempt || 0}</dd><dt>行版本</dt><dd>{item.job.row_version || 0}</dd><dt>租约截止</dt><dd>{item.job.lease_until ? new Date(item.job.lease_until).toLocaleString() : '—'}</dd></dl>{item.job.error?.safe_message && <p className="error-text">{item.job.error.safe_message}</p>}<div className="task-detail-actions">{['publication', 'parse'].includes(item.kind) && ['failed', 'paused'].includes(item.job.state) && <Button onClick={() => retry(item)} busy={busy === item.job.job_id}>恢复</Button>}{['publication', 'parse'].includes(item.kind) && ['queued', 'running'].includes(item.job.state) && <><Button onClick={() => pause(item)} busy={busy === item.job.job_id}>暂停</Button><Button onClick={() => cancel(item)} busy={busy === item.job.job_id}>取消</Button></>}</div></div></details>)}{more && <p className="hint">仅显示最近 100 个任务。</p>}</section>;
}

function Knowledge({ documents, loading, more, loadMore, openPreview, upload, uploading, refresh, notify, tagFilter, tagSummary, onTagFilterChange }) {
  const [query, setQuery] = useState(''), [filter, setFilter] = useState('all'), [tagQuery, setTagQuery] = useState(tagFilter || ''), [selected, setSelected] = useState([]), [batchFailures, setBatchFailures] = useState([]), [busy, setBusy] = useState(false), [tagBusy, setTagBusy] = useState(false);
  const filtered = documents.filter(d => (filter === 'all' || filter === 'published' && d.state === 'published' || filter === 'draft' && d.state !== 'published' && d.review_state === 'draft' || filter === 'submitted' && d.review_state === 'submitted' || filter === 'approved' && d.review_state === 'approved') && d.preview.toLowerCase().includes(query.toLowerCase()));
  const [tagEdit, setTagEdit] = useState(null), [tagValue, setTagValue] = useState('');
  const readTags = value => value.split(',').map(tag => tag.trim()).filter(Boolean);
  const editingTags = readTags(tagValue);
  const suggestions = (tagSummary || []).filter(item => !editingTags.some(tag => tag.toLowerCase() === item.tag)).slice(0, 12);
  function openTagEditor(edit, tags = []) {
    setTagValue(tags.join(', '));
    setTagEdit(edit);
  }
  async function saveTags(event) {
    event.preventDefault();
    if (!tagEdit || tagBusy || busy || (tagEdit.action && !editingTags.length)) return;
    setTagBusy(true);
    try {
      if (tagEdit.action) {
        const result = await request('tags/batch', { body: { document_ids: tagEdit.documentIDs, [tagEdit.action]: editingTags } });
        setSelected([]);
        notify(`标签操作完成，共更新 ${result.count || 0} 篇文档。`);
      } else {
        await request('tags', { body: { document_id: tagEdit.documentID, tags: editingTags } });
        notify('标签已保存。');
      }
      setTagEdit(null);
      await refresh();
    } catch (error) {
      notify(error.message);
    } finally {
      setTagBusy(false);
    }
  }
  async function batchAction(action) { const items = documents.filter(d => selected.includes(d.draft_id) && d.state !== 'published'); if (!items.length || busy) return; setBusy(true); setBatchFailures([]); const failed = []; let count = 0; for (const item of items) { try { if (action === 'submit') await request('submit', { body: { draft_id: item.draft_id, expect_hash: item.content_hash } }); else if (action === 'approve') await request('review', { body: { draft_id: item.draft_id, decision: 'approve', reason: '批量审核批准' } }); else await request('publish', { body: { draft_id: item.draft_id, expect_hash: item.content_hash, approved: true } }); count++; } catch (e) { failed.push(`${item.draft_id}：${e.message}`); } } setBusy(false); setSelected([]); setBatchFailures(failed); await refresh(); notify(`${action === 'submit' ? '提交审核' : action === 'approve' ? '批准' : '发布'} ${count} 个${failed.length ? `，${failed.length} 个失败` : ''}。`); }
  const selectedItems = documents.filter(d => selected.includes(d.draft_id) && d.state !== 'published'); const allSubmitted = selectedItems.length > 0 && selectedItems.every(d => d.review_state === 'submitted'); const allApproved = selectedItems.length > 0 && selectedItems.every(d => d.review_state === 'approved');
  const visibleDrafts = filtered.filter(d => d.state !== 'published'); const allVisibleSelected = visibleDrafts.length > 0 && visibleDrafts.every(d => selected.includes(d.draft_id));
  function applyTagFilter() { setSelected([]); onTagFilterChange?.(tagQuery.trim()); }
  return <section className="page-stack"><div className="page-toolbar"><div className="search-field"><Search size={15} /><input value={query} onChange={e => { setQuery(e.target.value); setSelected([]); }} placeholder="筛选当前文档" aria-label="筛选当前文档" /></div><div className="tag-filter"><label htmlFor="document-tag-filter">标签</label><input id="document-tag-filter" list="document-tag-suggestions" value={tagQuery} onChange={e => setTagQuery(e.target.value)} onKeyDown={e => { if (e.key === 'Enter') { e.preventDefault(); applyTagFilter(); } }} placeholder="例如：数据库, 运维" aria-label="文档标签筛选" /><datalist id="document-tag-suggestions">{(tagSummary || []).map(item => <option key={item.tag} value={item.tag}>{item.count} 篇文档</option>)}</datalist><Button onClick={applyTagFilter} disabled={tagQuery.trim() === (tagFilter || '').trim()}>应用</Button></div><div className="segmented">{[['all', '全部'], ['draft', '草稿'], ['submitted', '待审核'], ['approved', '已批准'], ['published', '已发布']].map(([id, name]) => <button key={id} className={filter === id ? 'selected' : ''} onClick={() => { setFilter(id); setSelected([]); }}>{name}</button>)}</div><Button onClick={() => setSelected(allVisibleSelected ? [] : visibleDrafts.map(d => d.draft_id))}>{allVisibleSelected ? '取消全选' : '全选草稿'}</Button>{selected.length > 0 && <div className="batch-actions"><Button onClick={() => openTagEditor({ action: 'add', documentIDs: selectedItems.map(d => d.document_id) })} busy={tagBusy}>批量加标签</Button><Button onClick={() => openTagEditor({ action: 'remove', documentIDs: selectedItems.map(d => d.document_id) })} busy={tagBusy}>批量移除标签</Button><Button onClick={() => batchAction('submit')} disabled={selectedItems.some(d => d.review_state !== 'draft') || selectedItems.length === 0} busy={busy}>提交审核</Button><Button onClick={() => batchAction('approve')} disabled={!allSubmitted} busy={busy}>批准</Button><Button primary onClick={() => batchAction('publish')} disabled={!allApproved} busy={busy}>发布</Button></div>}</div>{batchFailures.length > 0 && <div className="batch-failures" role="alert"><b>批量操作失败项</b>{batchFailures.map(item => <div key={item}>{item}</div>)}</div>}
    {tagEdit && <form className="panel tag-editor" onSubmit={saveTags} aria-label="标签编辑">
      <label htmlFor="edit-document-tags">{tagEdit.action === 'add' ? '批量添加标签' : tagEdit.action === 'remove' ? '批量移除标签' : '编辑文档标签'} · {tagEdit.documentIDs?.length || 1} 篇文档</label>
      <input id="edit-document-tags" value={tagValue} onChange={e => setTagValue(e.target.value)} disabled={tagBusy} placeholder="多个标签用逗号分隔" autoFocus />
      {suggestions.length > 0 && <div className="tag-editor-actions" aria-label="已有标签">
        {suggestions.map(item => <Button key={item.tag} type="button" disabled={tagBusy} onClick={() => setTagValue([...editingTags, item.tag].join(', '))}>{item.tag}（{item.count}）</Button>)}
      </div>}
      <div className="tag-editor-actions">
        <Button type="submit" primary busy={tagBusy} disabled={busy || Boolean(tagEdit.action && !editingTags.length)}>保存</Button>
        <Button type="button" disabled={tagBusy} onClick={() => setTagEdit(null)}>取消</Button>
      </div>
    </form>}
    {loading ? <Empty title="正在读取知识库…" text="正在核对当前文档与权限。" /> : filtered.length ? <div className="document-list panel">{filtered.map(d => <article className="document-item" key={d.draft_id}><input className="document-select" type="checkbox" aria-label={`选择 ${d.draft_id}`} checked={selected.includes(d.draft_id)} disabled={d.state === 'published' || busy} onChange={e => setSelected(items => e.target.checked ? [...items, d.draft_id] : items.filter(id => id !== d.draft_id))} /><div className={`library-icon ${d.state === 'published' ? 'green' : 'cyan'}`}><FileText size={20} /></div><div className="document-summary"><h2>{d.preview.split(/\r?\n/).find(Boolean)?.slice(0, 72) || '文档'}</h2><p>{d.preview}</p><small>{d.format.toUpperCase()} · <Status state={d.state} /> · 审核：{d.review_state === 'approved' ? '已批准' : d.review_state === 'submitted' ? '待审核' : d.review_state === 'rejected' ? '已拒绝' : '草稿'}</small>{d.tags?.length > 0 && <small className="tag-list">标签：{d.tags.join('、')}</small>}</div><Button disabled={tagBusy || busy} onClick={() => openTagEditor({ documentID: d.document_id }, d.tags)}>标签</Button><Button onClick={() => openPreview(d.draft_id, d.state === 'published')}>{d.state === 'published' ? '查看正文' : '预览并发布'}<ArrowRight size={14} /></Button></article>)}</div> : <><Empty title={documents.length ? '没有符合条件的文档' : '你的第一份知识，等你导入'} text={documents.length ? '调整关键词或文档状态后再试。' : '选择一份文件，预览正文，然后决定是否发布。'} />{!documents.length && <Button primary onClick={upload} busy={uploading}><UploadCloud size={16} />选择文件</Button>}</>}{more ? <div className="pagination"><Button onClick={loadMore} disabled={loading}>加载更多文档</Button><span>当前已加载 {documents.length} 份</span></div> : documents.length > 0 && <p className="hint">已加载全部文档。</p>}</section>;
}
function Preview({ value, close, saved, published, withdrawn, deleted, error, setError }) {
  const [text, setText] = useState(value.text);
  const [reviewState, setReviewState] = useState(value.review?.decision === 'approve' ? 'approved' : value.review?.decision === 'reject' ? 'rejected' : value.review_state || 'draft');
  const [reviewRecord, setReviewRecord] = useState(value.review);
  const [reviewHistory, setReviewHistory] = useState(value.review_history || []);
  const [job, setJob] = useState(value.job);
  const [comparison, setComparison] = useState(null);
  const [approved, setApproved] = useState(false);
  const [busy, setBusy] = useState(false);
  const panel = useRef(null);
  const previous = useRef(null);

  useEffect(() => {
    previous.current = document.activeElement;
    panel.current?.focus();
    return () => previous.current?.focus?.();
  }, []);
  useEffect(() => {
    if (!job || (job.state !== 'queued' && job.state !== 'running')) return undefined;
    let stopped = false;
    const poll = async () => {
      try {
        const latest = (await request(`jobs/${value.draft_id}`)).job;
        if (!stopped && latest) setJob(latest);
      } catch (_) {}
    };
    const timer = setInterval(poll, 1500);
    return () => { stopped = true; clearInterval(timer); };
  }, [job?.state, value.draft_id]);
  function keys(event) {
    if (event.key === 'Escape' && !busy) close();
    if (event.key !== 'Tab') return;
    const elements = [...panel.current.querySelectorAll('button:not(:disabled),input:not(:disabled)')];
    const first = elements[0], last = elements.at(-1);
    if (event.shiftKey && (document.activeElement === first || document.activeElement === panel.current)) {
      event.preventDefault();
      last?.focus();
    } else if (!event.shiftKey && document.activeElement === last) {
      event.preventDefault();
      first?.focus();
    }
  }
  async function save() {
    setBusy(true); setError('');
    try {
      const item = await request('edit', { body: { draft_id: value.draft_id, text, expect_hash: value.content_hash } });
      await saved(item);
    } catch (e) { setError(`${e.message} 可以保留当前编辑后重试。`); }
    finally { setBusy(false); }
  }
  async function submit() {
    setBusy(true); setError('');
    try { await request('submit', { body: { draft_id: value.draft_id, expect_hash: value.content_hash } }); setReviewState('submitted'); }
    catch (e) { setError(e.message); }
    finally { setBusy(false); }
  }
  async function review(decision) {
    const reason = decision === 'reject' ? window.prompt('请输入拒绝原因：', '')?.trim() : '';
    if (decision === 'reject' && !reason) return;
    setBusy(true); setError('');
    try {
      const result = await request('review', { body: { draft_id: value.draft_id, decision, reason } });
      const record = { decision, reason: result.reason, actor_id: result.actor_id, decided_at: result.decided_at, revision_id: result.revision_id };
      setReviewState(decision === 'approve' ? 'approved' : 'rejected');
      setReviewRecord(record);
      setReviewHistory(items => [...items, record]);
    } catch (e) { setError(e.message); }
    finally { setBusy(false); }
  }
  async function withdraw() {
    setBusy(true); setError('');
    try { await withdrawn(await request('withdraw', { body: { document_id: value.document_id, reason: '从本地工作台撤回当前发布版本' } })); }
    catch (e) { setError(e.message); }
    finally { setBusy(false); }
  }
  async function remove() {
    if (!window.confirm('删除后文档将从知识库中移除，并启动索引清理。确定继续吗？')) return;
    setBusy(true); setError('');
    try { await deleted(await request('delete', { body: { document_id: value.document_id } })); }
    catch (e) { setError(e.message); }
    finally { setBusy(false); }
  }
  async function compare(revisionID) {
    setBusy(true); setError('');
    try { setComparison(await request('compare', { body: { draft_id: value.draft_id, revision_id: revisionID } })); }
    catch (e) { setError(e.message); }
    finally { setBusy(false); }
  }
  async function publish() {
    setBusy(true); setError('');
    try { await request('publish', { body: { draft_id: value.draft_id, expect_hash: value.content_hash, approved: approved || reviewState === 'approved' } }); await published(); }
    catch (e) { setError(`${e.message} 可用同一草稿重试。`); try { setJob((await request(`jobs/${value.draft_id}`)).job); } catch (_) {} }
    finally { setBusy(false); }
  }
  async function retry() {
    setBusy(true); setError('');
    try { await request('retry', { body: { draft_id: value.draft_id, expect_hash: value.content_hash } }); await published(); }
    catch (e) { setError(`${e.message} 可以稍后再次重试。`); try { setJob((await request(`jobs/${value.draft_id}`)).job); } catch (_) {} }
    finally { setBusy(false); }
  }
  const changed = text !== value.text;
  return <div className="modal-backdrop">
    <section className="preview-modal panel" role="dialog" aria-modal="true" aria-labelledby="preview-title" tabIndex={-1} ref={panel} onKeyDown={keys}>
      <div className="panel-header"><div><h2 id="preview-title">{value.published ? '已发布正文' : '核对正文并发布'}</h2><span>{value.published ? '编辑会创建新的草稿版本，不会修改当前已发布内容。' : '可以先编辑并保存草稿，再批准发布。'}</span></div><button onClick={close} disabled={busy} aria-label="关闭预览"><X size={20} /></button></div>
      <textarea className="document-editor" value={text} onChange={e => setText(e.target.value)} disabled={busy} aria-label="文档正文" />
      <details className="source-details">
        <summary>文档标识与内容摘要</summary>
        <dl>
          <dt>草稿</dt><dd>{value.draft_id}</dd>
          <dt>SHA-256</dt><dd data-testid="preview-hash">{value.content_hash}</dd>
          <dt>审核状态</dt><dd>{reviewState}</dd>
          {reviewRecord && <>
            <dt>审核原因</dt><dd>{reviewRecord.reason || '—'}</dd>
            <dt>审核人</dt><dd>{reviewRecord.actor_id || '—'}</dd>
            <dt>审核时间</dt><dd>{reviewRecord.decided_at ? new Date(reviewRecord.decided_at).toLocaleString() : '—'}</dd>
          </>}
          {job && <><dt>发布任务</dt><dd>{stateNames[job.state] || job.state} · {job.stage} · 第 {job.attempt} 次尝试 · {Math.round((job.progress || 0) * 100)}%</dd></>}
          {value.original_available && <><dt>原件</dt><dd><a href={'/local/api/original/' + value.draft_id} target="_blank" rel="noreferrer">预览原件</a> · <a href={'/local/api/original/' + value.draft_id + '?download=1'} target="_blank" rel="noreferrer">下载原件</a></dd></>}
        </dl>
        <p className="hint"><a href={'/local/api/export/' + value.draft_id} target="_blank" rel="noreferrer">导出草稿 JSON</a>，包含正文、摘要、原件元数据和引用定位。</p>
      </details>
      {reviewHistory.length > 0 && <details className="source-details"><summary>审核历史</summary><ul className="revision-history">{reviewHistory.map((item, index) => <li key={String(item.id || index)}><b>{item.decision === 'approve' ? '批准' : item.decision === 'reject' ? '拒绝' : item.decision}</b><span>{item.decided_at ? new Date(item.decided_at).toLocaleString() : '—'} · {item.actor_id || '—'}</span>{item.reason && <small>{item.reason}</small>}</li>)}</ul></details>}
      {value.history?.length > 0 && <details className="source-details"><summary>发布版本历史</summary><ul className="revision-history">{value.history.map(item => <li key={String(item.revision_id) + '-' + String(item.action)}><b>第 {item.publication_epoch} 代 · {item.action === 'publish' ? '发布' : item.action === 'withdraw' ? '撤回' : item.action}</b><span>{new Date(item.occurred_at).toLocaleString()}</span>{item.reason && <small>{item.reason}</small>}{item.revision_id && <button className="text-button" onClick={() => compare(item.revision_id)} disabled={busy}>比较当前草稿</button>}</li>)}</ul></details>}
      {comparison && <details className="source-details diff-view" open><summary>当前草稿与历史版本差异</summary><p className="hint">草稿哈希：{comparison.draft_hash} · 历史版本哈希：{comparison.revision_hash}</p>{comparison.lines.map((line, index) => <div key={String(index) + '-' + String(line.kind)} className={`diff-line ${line.kind}`}><b>{line.kind === 'add' ? '+' : line.kind === 'remove' ? '−' : ' '}</b><span>{line.text || ' '}</span></div>)}</details>}
      {error && <div role="alert" className="error-banner">{error}</div>}
      {reviewState === 'draft' && !value.published && <label className="approve-label"><input type="checkbox" checked={approved} onChange={e => setApproved(e.target.checked)} disabled={busy} />我已核对全文，批准发布这份内容</label>}
      <div className="modal-actions"><Button onClick={close} disabled={busy}>关闭</Button>{changed && <Button onClick={save} busy={busy}>保存为新草稿</Button>}{reviewState === 'draft' && !changed && <Button onClick={submit} busy={busy}>提交审核</Button>}{reviewState === 'submitted' && <><Button onClick={() => review('reject')} busy={busy}>拒绝</Button><Button primary onClick={() => review('approve')} busy={busy}>批准</Button></>}{(reviewState === 'approved' || reviewState === 'draft') && !value.published && <Button primary onClick={publish} disabled={(reviewState === 'draft' && !approved) || changed} busy={busy}>{busy ? '正在发布…' : '发布到检索'}</Button>}{job && job.state !== 'completed' && reviewState === 'approved' && !value.published && <Button onClick={retry} busy={busy}><RefreshCw size={15} />重试发布</Button>}{value.published && <Button onClick={withdraw} busy={busy}>撤回发布</Button>}<Button onClick={remove} busy={busy}>删除文档</Button></div>
    </section>
  </div>;
}
function SearchWorkbench({ ai, notify }) {
  const [query, setQuery] = useState(''), [tagFilter, setTagFilter] = useState(''), [mode, setMode] = useState('semantic'), [source, setSource] = useState(''), [status, setStatus] = useState(''), [sort, setSort] = useState('rank'), [updatedAfter, setUpdatedAfter] = useState(''), [updatedBefore, setUpdatedBefore] = useState(''), [busy, setBusy] = useState(false), [error, setError] = useState(''), [result, setResult] = useState(null), [page, setPage] = useState(1), controller = useRef(null);
  useEffect(() => () => controller.current?.abort(), []);
  async function streamAnswer(event) {
    event.preventDefault(); if (!query.trim() || busy) return; controller.current?.abort(); const current = new AbortController(); controller.current = current; setBusy(true); setError(''); setPage(1); setResult({ mode: 'answer', status: 'streaming', answer: '', evidence: [], citations: [] }); const timeout = setTimeout(() => current.abort(), 125000);
    try {
      const response = await fetch('/local/api/search/stream', { method: 'POST', credentials: 'same-origin', signal: current.signal, headers: { 'Content-Type': 'application/json', 'X-Memorys-Local': '1' }, body: JSON.stringify({ query, source_kind: source, source_status: status, tags: tagFilter.split(',').map(tag => tag.trim()).filter(Boolean), updated_after: updatedAfter ? new Date(`${updatedAfter}T00:00:00Z`).toISOString() : '', updated_before: updatedBefore ? new Date(`${updatedBefore}T23:59:59Z`).toISOString() : '' }) });
      if (!response.ok || !response.body) { let message = `请求失败（HTTP ${response.status}）`; try { const data = await response.json(); message = data.error || message; } catch (_) {} throw new Error(message); }
      const reader = response.body.getReader(); const decoder = new TextDecoder(); let buffer = '';
      const consume = raw => { buffer += raw; const frames = buffer.split(/\r?\n\r?\n/); buffer = frames.pop() || ''; for (const frame of frames) { const eventName = frame.match(/^event: ([^\n]+)$/m)?.[1]; const line = frame.match(/^data: (.+)$/m)?.[1]; if (!line) continue; const data = JSON.parse(line); if (eventName === 'answer_delta') setResult(previous => ({ ...previous, answer: (previous?.answer || '') + (data.text || ''), status: 'streaming' })); else if (eventName === 'done') setResult(data); else if (eventName === 'error') throw new Error(data.message || '流式问答失败'); } };
      while (true) { const part = await reader.read(); if (part.done) break; consume(decoder.decode(part.value, { stream: true })); }
      consume(decoder.decode());
    } catch (e) { if (e.name !== 'AbortError') setError(e.message); } finally { clearTimeout(timeout); setBusy(false); }
  }
  async function search(event, requestedPage = 1, cursor = '') {
    if (mode === 'answer' && requestedPage === 1) return streamAnswer(event);
    event.preventDefault(); if (!query.trim() || busy) return; controller.current?.abort(); const current = new AbortController(); controller.current = current; setBusy(true); setError(''); setResult(null); setPage(requestedPage); const timeout = setTimeout(() => current.abort(), 125000);
    try { setResult(await request('search', { body: { query, mode, source_kind: source, source_status: status, tags: tagFilter.split(',').map(tag => tag.trim()).filter(Boolean), sort, updated_after: updatedAfter ? new Date(`${updatedAfter}T00:00:00Z`).toISOString() : '', updated_before: updatedBefore ? new Date(`${updatedBefore}T23:59:59Z`).toISOString() : '', page: requestedPage, page_size: 10, cursor }, signal: current.signal })); } catch (e) { setError(e.name === 'AbortError' ? '请求已取消或超时，可以重试。' : e.message); } finally { clearTimeout(timeout); setBusy(false); }
  }
  async function feedback(item, kind) { const comment = window.prompt('请说明反馈原因：', '')?.trim(); if (!comment) return; try { await request('feedback', { body: { query, document_id: item.citation.document_id, revision_id: item.citation.revision_id, block_id: item.citation.block_id, kind, comment } }); notify?.('反馈已记录。'); } catch (e) { setError(e.message); } }
    return <section className="page-stack"><form className="panel query-panel" onSubmit={search}><div className="segmented" aria-label="检索方式">{[['hybrid', '混合检索'], ['semantic', '语义检索'], ['keyword', '关键词检索'], ['answer', '知识问答']].map(([id, name]) => <button type="button" key={id} className={id === mode ? 'selected' : ''} onClick={() => { setMode(id); setPage(1); setResult(null); setError(''); }} disabled={busy}>{name}</button>)}</div><div className="query-input"><Search size={20} /><input aria-label="检索问题" placeholder="例如：遇到数据库故障应该怎样恢复？" value={query} onChange={e => setQuery(e.target.value)} required maxLength={4096} disabled={busy} /><Button primary type="submit" disabled={!query.trim()} busy={busy}>{busy ? '正在查询…' : mode === 'answer' ? '生成答案' : '开始检索'}</Button></div><div className="search-filters"><label>来源<select aria-label="来源筛选" value={source} onChange={e => { setSource(e.target.value); setPage(1); setResult(null); }} disabled={busy}><option value="">全部来源</option><option value="revision">发布正文</option><option value="original">原件文本</option></select></label><label>发布状态<select aria-label="发布状态筛选" value={status} onChange={e => { setStatus(e.target.value); setPage(1); setResult(null); }} disabled={busy}><option value="">全部状态</option><option value="published">当前发布</option><option value="historical">历史版本</option></select></label><label>排序<select aria-label="检索排序" value={sort} onChange={e => { setSort(e.target.value); setPage(1); setResult(null); }} disabled={busy}><option value="rank">相关性排名</option><option value="score">分数从高到低</option></select></label><label>标签<input aria-label="标签筛选" placeholder="例如：数据库, 运维" value={tagFilter} onChange={e => { setTagFilter(e.target.value); setPage(1); setResult(null); }} disabled={busy} /></label><label>更新起点<input type="date" aria-label="更新时间起点" value={updatedAfter} onChange={e => { setUpdatedAfter(e.target.value); setPage(1); setResult(null); }} disabled={busy} /></label><label>更新终点<input type="date" aria-label="更新时间终点" value={updatedBefore} onChange={e => { setUpdatedBefore(e.target.value); setPage(1); setResult(null); }} disabled={busy} /></label></div><p className="hint">{mode === 'answer' ? '问答会将问题及当前获权的文档片段发送给 DeepSeek；答案附来源供你核对。' : mode === 'hybrid' ? '同时执行关键词和语义检索，再按两路排名合并已发布来源。' : mode === 'semantic' ? '在本机理解问题含义，检索你已发布的内容。' : '按已发布正文中的关键词检索；中文关键词需与正文匹配。'}</p>{ai === false && (mode === 'semantic' || mode === 'hybrid' || mode === 'answer') && <p className="hint">尚未启用 AI，请先在本机运行 enable-ai，或使用关键词检索。</p>}</form>
    {busy && <div className="busy-line" role="status"><LoaderCircle className="spin" size={18} />{mode === 'answer' ? '正在检索来源并生成答案…' : '正在检索已发布文档…'}<button onClick={() => controller.current?.abort()}>取消</button></div>}{error && <div role="alert" className="error-banner">{error}</div>}
    {result && <>{result.mode === 'answer' && <article className="panel answer-panel"><div className="result-label"><Sparkles size={16} />{result.model || '知识问答'}</div><h2>{result.status === 'insufficient_evidence' ? '现有证据不足' : '根据文档的回答'}</h2><p className="answer-text">{result.answer}</p>{result.citations?.length > 0 && <div className="answer-citations">{result.citations.map((citation, i) => { const found = result.evidence.findIndex(e => e.citation.document_id === citation.document_id && e.citation.revision_id === citation.revision_id && e.citation.block_id === citation.block_id); return <a key={i} href={`#evidence-${found}`}><FileCheck2 size={14} />来源 {found + 1}</a>; })}</div>}</article>}{result.evidence.length ? <section className="page-stack" aria-label="检索来源"><h2 className="section-title">检索来源 <span>{result.evidence.length}</span></h2>{result.evidence.map((e, i) => <Evidence key={`${e.citation.block_id}-${i}`} item={e} index={i} onFeedback={feedback} />)}</section> : <Empty title="没有找到可用的已发布内容" text="调整问题，或先导入并发布相关文档。" />}{result.mode !== 'answer' && (result.has_more || result.page > 1) && <div className="pagination" aria-label="检索分页"><Button onClick={event => search(event, result.page - 1)} disabled={result.page <= 1}>上一页</Button><span>第 {result.page} 页</span><Button onClick={event => search(event, result.page + 1, result.next_cursor)} disabled={!result.has_more}>下一页</Button></div>}</>}{!result && !busy && !error && <Empty title="开始一次有来源的检索" text="输入问题，查看真实正文和来源引用。" />}</section>;
}
function formatAudioTime(milliseconds) { const total = Math.max(0, Math.floor(Number(milliseconds || 0) / 1000)); const minutes = Math.floor(total / 60); const seconds = String(total % 60).padStart(2, '0'); return `${minutes}:${seconds}`; }
function locatorFragment(locator = {}) {
  const params = new URLSearchParams();
  if (locator.page) params.set('page', locator.page);
  if (locator.slide) params.set('slide', locator.slide);
  if (locator.worksheet) params.set('worksheet', locator.worksheet);
  if (locator.cell_range) params.set('cell', locator.cell_range);
  if (locator.paragraph_path?.length) params.set('paragraph', locator.paragraph_path.join('.'));
  if (locator.start_ms != null) params.set('start_ms', locator.start_ms);
  if (locator.end_ms != null) params.set('end_ms', locator.end_ms);
  return params.toString() ? `#${params.toString()}` : '';
}
function AudioCitation({ src, startMs, endMs }) {
  const audio = useRef(null);
  const [ready, setReady] = useState(false);
  const [error, setError] = useState('');
  const start = startMs / 1000;
  const end = Number.isFinite(endMs) && endMs > startMs ? endMs / 1000 : Infinity;
  const loadError = '无法播放这段音频。请下载原件核对。';

  function prepare(event) {
    const element = event.currentTarget;
    if (start >= element.duration) {
      setError('引用起点超出音频时长。请下载原件核对。');
      return;
    }
    element.currentTime = start;
    setReady(true);
  }
  async function replay() {
    const element = audio.current;
    try {
      element.currentTime = start;
      await element.play();
      setError('');
    } catch {
      setError(loadError);
    }
  }
  function stopAtEnd(event) {
    const element = event.currentTarget;
    if (element.currentTime >= end) element.pause();
  }

  return <div>
    <audio ref={audio} controls preload="metadata" src={src} aria-label="播放引用音频"
      onLoadedMetadata={prepare} onTimeUpdate={stopAtEnd} onError={() => { setReady(false); setError(loadError); }} />
    <Button type="button" disabled={!ready} onClick={replay}>从引用起点播放</Button>
    {error && <p className="error-text" role="alert">{error}</p>}
  </div>;
}
function Evidence({ item, index, onFeedback }) {
  const excerpt = item.source_excerpt || '—';
  const published = item.source_status === 'published';
  const locator = item.citation.locator || {};
  const original = item.citation.source_kind === 'original' && item.citation.blob_id
    ? { blob_id: item.citation.blob_id, locator }
    : item.citation.derived_from?.[0];
  const originalBase = original?.blob_id
    ? `/local/api/original/${encodeURIComponent(item.citation.document_id)}/${encodeURIComponent(item.citation.revision_id)}/${encodeURIComponent(original.blob_id)}`
    : '';
  const audioLocator = original?.locator;
  const hasAudioStart = Number.isFinite(audioLocator?.start_ms) && audioLocator.start_ms >= 0;
  const originalHref = originalBase ? `${originalBase}${locatorFragment(original.locator)}` : '';
  const location = [locator.page && `第 ${locator.page} 页`, locator.slide && `第 ${locator.slide} 张幻灯片`, locator.start_ms != null && `起始 ${formatAudioTime(locator.start_ms)}`, locator.end_ms != null && `结束 ${formatAudioTime(locator.end_ms)}`, locator.confidence != null && `OCR 置信度 ${Number(locator.confidence).toFixed(1)}%`, locator.worksheet && `工作表 ${locator.worksheet}`, locator.cell_range && `单元格 ${locator.cell_range}`, locator.paragraph_path?.length && `段落 ${locator.paragraph_path.join('.')}`].filter(Boolean).join(' · ');
  return <article className="panel evidence-card" id={`evidence-${index}`}>
    <div className="evidence-heading"><span className="source-number">{String(index + 1).padStart(2, '0')}</span><h3>{item.source_title || excerpt.split(/\r?\n/).find(Boolean)?.slice(0, 90)}</h3><span className="muted-text">{item.citation.source_kind === 'original' ? '原件文本' : '发布正文'}</span></div>
    <p>{excerpt}</p>
    <details className="source-details"><summary>核对引用</summary><dl>
      <dt>文档</dt><dd>{item.source_title || item.citation.document_id}</dd><dt>版本</dt><dd>{item.citation.revision_id}</dd><dt>内容块</dt><dd>{item.citation.block_id}</dd><dt>检索排名</dt><dd>{item.rank || '—'}</dd><dt>相关分数</dt><dd>{item.score != null ? Number(item.score).toFixed(4) : '—'}</dd><dt>检索路径</dt><dd>{item.retrieval_routes?.join('、') || '—'}</dd><dt>发布状态</dt><dd>{published ? '当前发布版本' : '历史版本'}</dd><dt>发布代次</dt><dd>{item.publication_epoch || '—'}</dd><dt>更新时间</dt><dd>{item.updated_at ? new Date(item.updated_at).toLocaleString() : '—'}</dd><dt>标签</dt><dd>{item.tags?.join('、') || '—'}</dd><dt>原文位置</dt><dd>{location || (locator.precision || '正文级引用')}</dd>
      {originalBase && <><dt>原件</dt><dd><a href={originalHref} target="_blank" rel="noreferrer">打开原件并保留定位</a> · <a href={`${originalBase}?download=1`} target="_blank" rel="noreferrer">下载原件</a>{hasAudioStart && <AudioCitation key={`${originalBase}:${audioLocator.start_ms}:${audioLocator.end_ms}`} src={originalBase} startMs={audioLocator.start_ms} endMs={audioLocator.end_ms} />}</dd></>}
    </dl><p className="hint">当前引用会保留解析器提供的页码、幻灯片、工作表、单元格或段落定位；打开原件后可按页码或定位信息核对。</p></details>
    <div className="feedback-actions" aria-label="Feedback source quality"><span className="muted-text">来源反馈</span><Button onClick={() => onFeedback(item, 'helpful')}>有帮助</Button><Button onClick={() => onFeedback(item, 'incorrect')}>引用不准确</Button><Button onClick={() => onFeedback(item, 'outdated')}>内容过期</Button></div>
  </article>;
}
function Diagnostics({ info, loading, refresh }) {
  const names = { database: '数据库连接', schema: '数据库版本记录', identity: '本地身份', permissions: '当前权限', configuration: '知识库配置', searchable: '有已发布内容', office_parser: '文档解析器', ocr_parser: 'OCR 解析器', transcription_parser: '音频转写器', embedding_model: '本地嵌入模型', answer_key_configured: 'DeepSeek 密钥已配置' };
  const expiresAt = info?.session_expires_at ? Date.parse(info.session_expires_at) : 0;
  const sessionMinutes = expiresAt ? Math.ceil((expiresAt - Date.now()) / 60000) : 0;
  return <section className="page-stack"><div className="panel-header"><div><h2>当前检查</h2><span>只读取状态，不修改配置或运行模型问答。</span></div><Button onClick={refresh} busy={loading}><RefreshCw size={15} />重新检查</Button></div>{!info ? <Empty title="暂未读取到运行状态" text="请重试，或检查终端中的服务日志。" /> : <div className="panel">{sessionMinutes > 0 && sessionMinutes <= 10 && <p className="hint">本地会话将在约 {sessionMinutes} 分钟后过期，请刷新页面以延长会话。</p>}{sessionMinutes <= 0 && expiresAt > 0 && <p className="error-text">本地会话已过期，请刷新页面重新建立会话。</p>}{Object.entries(names).filter(([key]) => key in info).map(([key, name]) => <div className="diagnostic-row" key={key}><span>{name}</span><span className={`status ${info[key] ? 'green' : 'amber'}`}><span className="status-dot" />{info[key] ? '通过' : '待处理'}</span></div>)}{info.embedding && <p className="hint">嵌入模型：{info.embedding.ProviderModel} · {info.embedding.Dimension} 维</p>}<p className="hint">密钥是否配置不代表远程服务一定可用。当前提供本地功能，不代表生产验收通过。</p></div>}</section>;
}
function MetricsPanel() {
  const [metrics, setMetrics] = useState(null), [error, setError] = useState('');
  useEffect(() => { (async () => { try { setMetrics(await request('metrics')); } catch (e) { setError(e.message); } })(); }, []);
  return <section className="panel metrics-panel"><div className="panel-header"><div><h2>运行指标</h2><span>当前本地工作台进程的请求和延迟快照。</span></div></div>{error ? <p className="error-text">{error}</p> : !metrics ? <p className="hint">正在读取指标…</p> : <div className="metrics-inline"><span>请求 {metrics.requests}</span><span>错误 {metrics.errors}</span><span>流式问答 {metrics.stream_requests}</span><span>平均 {Number(metrics.average_ms || 0).toFixed(1)} ms</span><span>最大 {Number(metrics.max_ms || 0).toFixed(1)} ms</span></div>}</section>;
}
function FeedbackPanel({ notify, openPreview }) {
  const [rows, setRows] = useState([]), [summary, setSummary] = useState({}), [quality, setQuality] = useState(null), [evaluation, setEvaluation] = useState(null), [sets, setSets] = useState([]), [filter, setFilter] = useState('open'), [from, setFrom] = useState(''), [to, setTo] = useState(''), [documentFilter, setDocumentFilter] = useState(''), [revisionFilter, setRevisionFilter] = useState(''), [tagFilter, setTagFilter] = useState(''), [loading, setLoading] = useState(true), [busy, setBusy] = useState(''), [error, setError] = useState('');
  function timeQuery() { const params = new URLSearchParams({ state: filter }); if (from) params.set('from', new Date(`${from}T00:00:00Z`).toISOString()); if (to) { const end = new Date(`${to}T23:59:59Z`); params.set('to', end.toISOString()); } if (documentFilter.trim()) params.set('document_id', documentFilter.trim()); if (revisionFilter.trim()) params.set('revision_id', revisionFilter.trim()); if (tagFilter.trim()) params.set('tags', tagFilter.trim()); return params.toString(); }
  async function refresh() { setLoading(true); setError(''); try { const query = timeQuery(); const [data, report, saved] = await Promise.all([request(`feedback?${query}`), request(`evaluation?${query}`), request('evaluation-sets')]); setRows(data.feedback || []); setSummary(data.summary || {}); setQuality(data.quality || null); setEvaluation(report); setSets(saved.sets || []); } catch (e) { setError(e.message); } finally { setLoading(false); } }
  useEffect(() => { refresh(); }, [filter, from, to, documentFilter, revisionFilter, tagFilter]);
  async function saveEvaluationSet() { const title = window.prompt('评测集名称：', `反馈评测 ${new Date().toLocaleDateString()}`)?.trim(); if (!title) return; try { const body = { title, state: filter, document_id: documentFilter.trim(), revision_id: revisionFilter.trim(), tags: tagFilter.split(',').map(tag => tag.trim()).filter(Boolean), limit: 1000 }; if (from) body.from = new Date(`${from}T00:00:00Z`).toISOString(); if (to) body.to = new Date(`${to}T23:59:59Z`).toISOString(); const result = await request('evaluation-sets', { body }); notify?.(`评测集已保存：${result.title}（${result.samples} 条）`); } catch (e) { setError(e.message); } }
  async function resolve(row, state) { const resolution = window.prompt(state === 'resolved' ? '请输入处理说明：' : '请输入忽略原因：', '')?.trim(); if (!resolution || busy) return; setBusy(row.feedback_id); try { await request('feedback/resolve', { body: { feedback_id: row.feedback_id, state, resolution } }); await refresh(); } catch (e) { setError(e.message); } finally { setBusy(''); } }
  async function propose(row) { if (busy || row.proposal_draft_id) return; setBusy(row.feedback_id); try { const result = await request('feedback/proposal', { body: { feedback_id: row.feedback_id } }); notify?.('已创建修订草稿，可以继续编辑和审核。'); await refresh(); await openPreview?.(result.draft_id, false); } catch (e) { setError(e.message); } finally { setBusy(''); } }
  return <section className="panel feedback-panel"><div className="panel-header"><div><h2>来源反馈</h2><span>查看用户对检索来源的反馈，并记录处理结果。</span></div><div className="feedback-panel-actions"><Button onClick={saveEvaluationSet}>保存评测集</Button><a className="text-button" href={`/local/api/feedback/export?${timeQuery()}`} target="_blank" rel="noreferrer">导出评测数据</a><div className="segmented"><button className={filter === 'open' ? 'selected' : ''} onClick={() => setFilter('open')}>待处理</button><button className={filter === 'resolved' ? 'selected' : ''} onClick={() => setFilter('resolved')}>已处理</button><button className={filter === 'dismissed' ? 'selected' : ''} onClick={() => setFilter('dismissed')}>已忽略</button></div></div></div><div className="feedback-date-filters"><label>起始日期<input type="date" aria-label="反馈起始日期" value={from} onChange={e => setFrom(e.target.value)} /></label><label>结束日期<input type="date" aria-label="反馈结束日期" value={to} onChange={e => setTo(e.target.value)} /></label></div><div className="feedback-scope-filters"><label>文档<input aria-label="反馈文档筛选" value={documentFilter} onChange={e => setDocumentFilter(e.target.value)} placeholder="文档 ID" /></label><label>版本<input aria-label="反馈版本筛选" value={revisionFilter} onChange={e => setRevisionFilter(e.target.value)} placeholder="版本 ID" /></label><label>标签<input aria-label="反馈标签筛选" value={tagFilter} onChange={e => setTagFilter(e.target.value)} placeholder="例如：运维" /></label></div>{sets.length > 0 && <div className="evaluation-set-list"><span>已保存评测集：</span>{sets.slice(-3).map(set => <a key={set.evaluation_set_id} href={`/local/api/evaluation-sets/${encodeURIComponent(set.evaluation_set_id)}`} target="_blank" rel="noreferrer">{set.title}（{set.samples} 条）</a>)}</div>}<div className="feedback-summary">有帮助 {summary.helpful || 0} · 引用不准确 {summary.incorrect || 0} · 缺少来源 {summary.missing || 0} · 内容过期 {summary.outdated || 0}{quality && ` · 有帮助率 ${(quality.helpful_rate * 100).toFixed(1)}% · 问题率 ${(quality.issue_rate * 100).toFixed(1)}%`}{evaluation && ` · 覆盖 ${evaluation.unique_queries || 0} 个问题 / ${evaluation.unique_documents || 0} 个文档 / ${evaluation.unique_revisions || 0} 个版本`}</div>{error && <p className="error-text">{error}</p>}{loading ? <p className="hint">正在读取反馈…</p> : rows.length === 0 ? <p className="hint">当前筛选没有反馈。</p> : <div className="feedback-list">{rows.map(row => <article className="feedback-row" key={row.feedback_id}><div><b>{({ helpful: '有帮助', incorrect: '引用不准确', missing: '缺少来源', outdated: '内容过期', other: '其他' })[row.kind] || row.kind}</b><p>{row.comment}</p><small>文档 {row.document_id} · 版本 {row.revision_id} · 内容块 {row.block_id} · {new Date(row.created_at).toLocaleString()}</small>{row.resolution && <small>处理：{row.resolution}</small>}{row.proposal_draft_id && <small>修订草稿：{row.proposal_draft_id}</small>}{row.proposal_publication && <small>修订发布版本：{row.proposal_publication.revision_id} · 发布记录：{row.proposal_publication.history_id} · {new Date(row.proposal_publication.published_at).toLocaleString()}</small>}</div>{filter === 'open' && <div className="feedback-row-actions">{['incorrect', 'missing', 'outdated'].includes(row.kind) && <Button onClick={() => propose(row)} busy={busy === row.feedback_id}>{row.proposal_draft_id ? '已建草稿' : '创建修订草稿'}</Button>}<Button onClick={() => resolve(row, 'resolved')} busy={busy === row.feedback_id}>已处理</Button><Button onClick={() => resolve(row, 'dismissed')} busy={busy === row.feedback_id}>忽略</Button></div>}</article>)}</div>}</section>;
}
function AuditPanel() {
  const [rows, setRows] = useState([]), [loading, setLoading] = useState(true), [error, setError] = useState('');
  useEffect(() => { (async () => { try { const data = await request('audit?limit=50'); setRows(data.audit || []); } catch (e) { setError(e.message); } finally { setLoading(false); } })(); }, []);
  return <section className="panel audit-panel"><div className="panel-header"><div><h2>操作审计</h2><span>只读显示当前知识库的关键生命周期操作。</span></div></div>{error && <p className="error-text">{error}</p>}{loading ? <p className="hint">正在读取审计…</p> : rows.length === 0 ? <p className="hint">暂时没有可显示的审计记录。</p> : <div className="audit-list">{rows.map((row, index) => <div className="audit-row" key={`${row.action}-${row.request_id}-${index}`}><b>{row.action}</b><span>{row.resource_id}</span><small>{row.actor_id} · {row.occurred_at}</small></div>)}</div>}</section>;
}
createRoot(document.getElementById('root')).render(<App />);
