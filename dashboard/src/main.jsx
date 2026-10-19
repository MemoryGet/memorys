import React, { useState } from 'react';
import { createRoot } from 'react-dom/client';
import { Button, Chip } from '@heroui/react';
import { Activity, ArrowUpRight, BookOpen, Bot, CheckCircle2, ChevronRight, CircleDot, FileText, Gauge, Layers3, Plus, Search, Settings2, ShieldCheck, Sparkles, Timer, UploadCloud, Workflow, XCircle } from 'lucide-react';
import './styles.css';

const nav = [
  { id: 'overview', label: '总览', icon: Gauge },
  { id: 'knowledge', label: '知识库', icon: BookOpen },
  { id: 'jobs', label: '任务中心', icon: Workflow, badge: '3' },
  { id: 'search', label: '检索工作台', icon: Search },
  { id: 'acceptance', label: 'AI 验收', icon: ShieldCheck },
];

const jobs = [
  { name: '产品手册 · v1.4', type: '解析 + 索引', progress: 78, state: '运行中', tone: 'cyan', time: '刚刚' },
  { name: 'API 参考 · 2026 Q4', type: '发布校验', progress: 100, state: '已完成', tone: 'green', time: '12 分钟前' },
  { name: '旧版 FAQ', type: '删除清理', progress: 42, state: '需关注', tone: 'amber', time: '28 分钟前' },
];

function Status({ children, tone = 'neutral' }) { return <span className={`status ${tone}`}><span className="status-dot" />{children}</span>; }

function App() {
  const [active, setActive] = useState('overview');
  const page = nav.find((item) => item.id === active);
  return <div className="app-shell">
    <aside className="sidebar">
      <div className="brand"><div className="brand-mark"><Sparkles size={17} /></div><div><strong>memorys</strong><span>Knowledge OS</span></div></div>
      <div className="workspace-switch"><div className="workspace-avatar">A</div><div><b>AI Platform</b><span>生产镜像 · v1.0</span></div><ChevronRight size={15} /></div>
      <div className="nav-label">工作台</div>
      <nav>{nav.map(({ id, label, icon: Icon, badge }) => <button key={id} className={`nav-item ${active === id ? 'active' : ''}`} onClick={() => setActive(id)}><Icon size={17} /><span>{label}</span>{badge && <em>{badge}</em>}</button>)}</nav>
      <div className="nav-label">系统</div>
      <button className="nav-item"><Settings2 size={17} /><span>模块与配置</span></button>
      <button className="nav-item"><Layers3 size={17} /><span>BuildLock</span></button>
      <div className="sidebar-bottom"><div className="agent-card"><div className="agent-orb"><Bot size={17} /></div><div><b>Codex Agent</b><span>随时可以验收</span></div><CircleDot size={13} className="online" /></div><div className="user-row"><div className="user-avatar">L</div><span>Lin · Admin</span><ChevronRight size={14} /></div></div>
    </aside>
    <main className="main">
      <header className="topbar"><div className="breadcrumbs"><span>Workspace</span><ChevronRight size={13} /><b>{page.label}</b></div><div className="top-actions"><div className="live"><span /> Live system</div><button className="icon-btn"><Activity size={17} /></button><button className="icon-btn"><Search size={17} /></button><div className="top-avatar">L</div></div></header>
      <div className="content">
        <div className="hero-row"><div><div className="eyebrow"><span className="eyebrow-line" /> MONDAY · 19 OCT 2026</div><h1>{active === 'overview' ? <>早上好，Lin <span>✦</span></> : page.label}</h1><p>{active === 'overview' ? '系统状态清晰，今天适合推进一次完整的知识发布。' : '统一查看、操作和验收 Knowledge OS 的运行状态。'}</p></div><div className="hero-actions"><Button variant="secondary" startContent={<UploadCloud size={16} />}>上传文档</Button><Button color="primary" startContent={<Plus size={16} />}>新建任务</Button></div></div>
        {active === 'overview' ? <Overview /> : <Placeholder title={page.label} icon={page.icon} />}
      </div>
    </main>
  </div>;
}

function Overview() { return <>
  <section className="metric-grid"><Metric icon={<BookOpen />} label="知识库" value="12" detail="+2 本周" tone="blue" /><Metric icon={<FileText />} label="已发布文档" value="1,284" detail="+8.4%" tone="purple" /><Metric icon={<Timer />} label="运行中任务" value="03" detail="2 个正常" tone="orange" /><Metric icon={<ShieldCheck />} label="本地验收" value="100%" detail="4 / 4 通过" tone="green" /></section>
  <section className="dashboard-grid"><div className="panel health-panel"><PanelHeader title="系统健康" subtitle="过去 24 小时" action="查看详情" /><div className="health-score"><div className="score-ring"><strong>98</strong><span>/ 100</span></div><div><Status tone="green">All systems operational</Status><p>所有核心模块均已连接，最近一次检查在 2 分钟前完成。</p></div></div><div className="health-bars"><HealthBar label="API Gateway" value="99.99%" width="99"/><HealthBar label="PostgreSQL" value="99.97%" width="97"/><HealthBar label="Event Runner" value="98.40%" width="84"/></div></div><div className="panel acceptance-panel"><PanelHeader title="AI 验收进度" subtitle="Codex · 本地功能套件" action="打开报告" /><div className="acceptance-head"><div className="mini-score">4<span>/4</span></div><Status tone="green">通过</Status></div><div className="progress-track"><span style={{width: '100%'}} /></div><div className="acceptance-list"><CheckRow label="Go 全量测试" detail="32.6s"/><CheckRow label="Go vet" detail="通过"/><CheckRow label="Python 契约" detail="232 tests"/><CheckRow label="OpenAPI 结构" detail="3 specs"/></div><div className="scope-note"><ShieldCheck size={15} /> 本地通过 ≠ 生产通过</div></div></section>
  <section className="lower-grid"><div className="panel jobs-panel"><PanelHeader title="最近任务" subtitle="实时更新" action="全部任务" /><div className="job-list">{jobs.map((job) => <div className="job-row" key={job.name}><div className={`job-icon ${job.tone}`}><FileText size={17} /></div><div className="job-info"><div><b>{job.name}</b><span>{job.type}</span></div><div className="job-progress"><div className="progress-track"><span className={job.tone} style={{width: `${job.progress}%`}} /></div><small>{job.progress}%</small></div></div><div className="job-state"><Status tone={job.tone === 'green' ? 'green' : job.tone === 'amber' ? 'amber' : 'cyan'}>{job.state}</Status><small>{job.time}</small></div><ChevronRight size={15} className="muted" /></div>)}</div></div><div className="panel activity-panel"><PanelHeader title="事件流" subtitle="刚刚发生" action="查看日志" /><div className="activity-list"><ActivityRow icon={<CheckCircle2 />} tone="green" title="发布完成" desc="API 参考 · 2026 Q4 已进入 current" time="12m"/><ActivityRow icon={<ArrowUpRight />} tone="cyan" title="索引更新" desc="产品手册 · v1.4 正在写入向量索引" time="18m"/><ActivityRow icon={<XCircle />} tone="amber" title="需要关注" desc="旧版 FAQ 清理任务等待重试" time="28m"/></div></div></section>
  <div className="footer-note"><Sparkles size={14} /> <span>由 Codex 驱动的验收工作台</span><span className="footer-divider" /> <span>最后同步：刚刚</span></div>
</> }

function Metric({ icon, label, value, detail, tone }) { return <div className="metric-card"><div className={`metric-icon ${tone}`}>{React.cloneElement(icon, { size: 18 })}</div><span className="metric-label">{label}</span><strong className="metric-value">{value}</strong><span className={`metric-detail ${tone}`}>{detail} <ArrowUpRight size={12}/></span></div> }
function PanelHeader({ title, subtitle, action }) { return <div className="panel-header"><div><h2>{title}</h2><span>{subtitle}</span></div><button>{action}<ArrowUpRight size={13}/></button></div> }
function HealthBar({ label, value, width }) { return <div className="health-bar"><div><span>{label}</span><b>{value}</b></div><div className="progress-track"><span style={{width: `${width}%`}} /></div></div> }
function CheckRow({ label, detail }) { return <div className="check-row"><CheckCircle2 size={15}/><span>{label}</span><small>{detail}</small></div> }
function ActivityRow({ icon, tone, title, desc, time }) { return <div className="activity-row"><div className={`activity-icon ${tone}`}>{icon}</div><div><b>{title}</b><p>{desc}</p></div><small>{time}</small></div> }
function Placeholder({ title, icon: Icon }) { return <div className="placeholder panel"><div className="placeholder-icon"><Icon size={28}/></div><h2>{title}正在准备中</h2><p>这个工作台会复用同一套 REST/MCP 能力，并由 Codex 验收每个关键路径。</p><Button color="primary">查看设计范围</Button></div> }

createRoot(document.getElementById('root')).render(<App />);
