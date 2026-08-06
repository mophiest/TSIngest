import { FormEvent, ReactNode, useEffect, useMemo, useState } from 'react'
import {
  IconActivityHeartbeat as Activity, IconAlertTriangle as AlertTriangle,
  IconAntennaBars5 as Wifi, IconAntennaBarsOff as WifiOff, IconArchive as Library,
  IconBell as Bell, IconBroadcast as Radio, IconCheck as CheckCircle2,
  IconClock as Clock3, IconColumns3 as Columns3, IconDatabase as HardDrive,
  IconDeviceFloppy as Save, IconDownload as Download, IconEye as Eye,
  IconFilter as Filter, IconKey as KeyRound, IconLayoutDashboard as LayoutDashboard,
  IconLogout as LogOut, IconMenu2 as Menu, IconMovie as Film,
  IconPencil as Pencil, IconPlayerPlay as Play, IconPlayerStop as Square,
  IconPlus as Plus, IconRefresh as RefreshCw, IconServer as Server,
  IconSettings as SettingsIcon, IconShieldCheck as ShieldCheck,
  IconTrash as Trash2, IconX as X,
} from '@tabler/icons-react'
import { api, ApiError, type StreamForm } from './api'
import type { Dashboard, MediaFile, Recording, Settings, Stream, StreamDiagnosis, WorkerHeartbeat } from './types'

type View = 'dashboard' | 'streams' | 'recordings' | 'settings'
type Toast = { kind: 'success' | 'error'; text: string }

const emptyStream: StreamForm = {
  name: '', mode: 'listener', host: '', port: 9000, streamId: '', latencyMs: 200,
  timeoutMs: 30000, passphrase: '', clearPassphrase: false, autoMp4: false,
}

export default function App() {
  const [user, setUser] = useState<{id: string; username: string} | null | undefined>(undefined)
  const [dashboard, setDashboard] = useState<Dashboard | null>(null)
  const [view, setView] = useState<View>('dashboard')
  const [toast, setToast] = useState<Toast | null>(null)
  const [menuOpen, setMenuOpen] = useState(false)

  useEffect(() => { api.me().then(setUser).catch(() => setUser(null)) }, [])
  useEffect(() => {
    if (!user) return
    api.dashboard().then(setDashboard).catch(error => notifyError(error, setToast))
    const source = new EventSource('/api/v1/events')
    source.addEventListener('snapshot', event => {
      try { setDashboard(JSON.parse((event as MessageEvent).data)) } catch { /* ignore malformed event */ }
    })
    return () => source.close()
  }, [user])
  useEffect(() => { if (!toast) return; const timer = setTimeout(() => setToast(null), 4200); return () => clearTimeout(timer) }, [toast])

  if (user === undefined) return <LoadingScreen />
  if (!user) return <Login onLogin={setUser} />

  const refresh = async () => setDashboard(await api.dashboard())
  const logout = async () => { try { await api.logout() } finally { setUser(null); setDashboard(null) } }
  const notify = (kind: Toast['kind'], text: string) => setToast({ kind, text })

  return (
    <div className="app-shell">
      <aside className={`sidebar ${menuOpen ? 'open' : ''}`}>
        <Brand />
        <nav>
          <NavItem active={view === 'dashboard'} icon={<LayoutDashboard />} label="运行总览" onClick={() => { setView('dashboard'); setMenuOpen(false) }} />
          <NavItem active={view === 'streams'} icon={<Radio />} label="通道监看" onClick={() => { setView('streams'); setMenuOpen(false) }} />
          <NavItem active={view === 'recordings'} icon={<Library />} label="录制文件" onClick={() => { setView('recordings'); setMenuOpen(false) }} />
          <NavItem active={view === 'settings'} icon={<SettingsIcon />} label="系统设置" onClick={() => { setView('settings'); setMenuOpen(false) }} />
        </nav>
        <div className="sidebar-foot">
          <div className="admin-badge"><div className="avatar">{user.username.slice(0, 1).toUpperCase()}</div><div><strong>{user.username}</strong><span>系统管理员</span></div></div>
          <button className="icon-button" title="退出登录" onClick={logout}><LogOut size={18} /></button>
        </div>
      </aside>
      {menuOpen && <button className="scrim" aria-label="关闭菜单" onClick={() => setMenuOpen(false)} />}
      <main className="main">
        <header className="topbar">
          <button className="mobile-menu icon-button" onClick={() => setMenuOpen(true)}><Menu /></button>
          <div className="system-strip">
            <div className="system-item clock"><Clock3 /><span>系统时间 (UTC+8)</span><b className="mono">{dashboard ? formatFullTime(dashboard.serverTime) : '--:--:--'}</b></div>
            <div className={`system-item ${dashboard && !workerIsOnline(dashboard.workers[0]) ? 'hard' : ''}`}><Server /><span>Worker 状态</span><b className={dashboard && workerIsOnline(dashboard.workers[0]) ? 'healthy' : ''}>{dashboard ? workerIsOnline(dashboard.workers[0]) ? '在线' : '离线' : '检测中'}</b></div>
            <div className={`system-item ${dashboard ? diskGuardLevel(dashboard.workers[0], dashboard.settings) : ''}`}><ShieldCheck /><span>磁盘保护</span><b className={dashboard && diskGuardLevel(dashboard.workers[0], dashboard.settings) === 'normal' ? 'healthy' : ''}>{dashboard ? diskGuardLabel(diskGuardLevel(dashboard.workers[0], dashboard.settings)) : '检测中'}</b></div>
            <div className="system-item alarm"><Bell /><span>未处理告警</span><b>{dashboard?.failedLast24h || 0}</b></div>
          </div>
          <button className="icon-button refresh-button" title="刷新" onClick={() => refresh().catch(error => notifyError(error, setToast))}><RefreshCw size={18} /></button>
        </header>
        <div className="content">
          {!dashboard ? <PageSkeleton /> : view === 'dashboard' ? <DashboardPage data={dashboard} setView={setView} refresh={refresh} notify={notify} />
            : view === 'streams' ? <StreamsPage data={dashboard} refresh={refresh} notify={notify} />
            : view === 'recordings' ? <RecordingsPage data={dashboard} refresh={refresh} notify={notify} />
            : <SettingsPage data={dashboard} refresh={refresh} notify={notify} />}
        </div>
      </main>
      {toast && <div className={`toast ${toast.kind}`}>{toast.kind === 'success' ? <CheckCircle2 /> : <AlertTriangle />}<span>{toast.text}</span></div>}
    </div>
  )
}

function Login({ onLogin }: { onLogin: (user: {id: string; username: string}) => void }) {
  const [username, setUsername] = useState('admin')
  const [password, setPassword] = useState('')
  const [error, setError] = useState('')
  const [busy, setBusy] = useState(false)
  const submit = async (event: FormEvent) => {
    event.preventDefault(); setBusy(true); setError('')
    try { onLogin(await api.login(username, password)) }
    catch (err) { setError(messageOf(err)) }
    finally { setBusy(false) }
  }
  return <div className="login-page">
    <div className="login-glow one" /><div className="login-glow two" />
    <section className="login-panel">
      <Brand large />
      <div className="login-copy"><span className="eyebrow">SECURE CONTROL PLANE</span><h1>让每一路信号<br />安全落地。</h1><p>多路 SRT 收录、完整 TS 母版和可靠的 MP4 交付，在一个清晰的工作台中完成。</p></div>
      <div className="login-signals"><span><ShieldCheck />单管理员保护</span><span><Activity />实时录制状态</span><span><HardDrive />磁盘水位防护</span></div>
    </section>
    <form className="login-card" onSubmit={submit}>
      <div className="login-icon"><KeyRound /></div><h2>登录管理台</h2><p>使用部署时配置的管理员凭据</p>
      <label>管理员账号<input autoFocus autoComplete="username" value={username} onChange={e => setUsername(e.target.value)} /></label>
      <label>密码<input type="password" autoComplete="current-password" value={password} onChange={e => setPassword(e.target.value)} /></label>
      {error && <div className="form-error"><AlertTriangle size={16} />{error}</div>}
      <button className="button primary wide" disabled={busy || !username || !password}>{busy ? <><span className="spinner" />正在验证</> : '进入管理台'}</button>
      <small>仅限已授权的内网管理员访问</small>
    </form>
  </div>
}

function DashboardPage({ data, setView, refresh, notify }: { data: Dashboard; setView: (view: View) => void; refresh: () => Promise<void>; notify: PageProps['notify'] }) {
  const [filter, setFilter] = useState<'all' | 'recording' | 'waiting' | 'alert'>('all')
  const [busy, setBusy] = useState('')
  const worker = data.workers[0]
  const diskUsed = worker?.diskTotalBytes ? Math.max(0, 100 - worker.diskFreeBytes * 100 / worker.diskTotalBytes) : 0
  const diskGuard = diskGuardLevel(worker, data.settings)
  const recordingCount = data.recordingCount ?? data.recordings.filter(recording => recording.status === 'recording').length
  const rows = data.streams.map((stream, index) => {
    const active = activeFor(data.recordings, stream.id)
    const latest = data.recordings.find(recording => recording.streamId === stream.id)
    const state = active?.status || (latest?.status === 'failed' ? 'failed' : 'idle')
    const signal = signalHealth(stream, active, latest, data.serverTime)
    const displayStatus = state === 'recording' && signal === 'stalled' ? 'stalled' : state
    return { stream, active, latest, state, signal, displayStatus, index }
  }).filter(row => filter === 'all' || (filter === 'recording' && row.state === 'recording' && row.signal === 'locked') || (filter === 'waiting' && ['waiting_input', 'idle'].includes(row.state)) || (filter === 'alert' && (row.state === 'failed' || row.signal === 'stalled')))
  const run = async (key: string, action: () => Promise<unknown>, success: string) => {
    setBusy(key)
    try { await action(); notify('success', success); await refresh() }
    catch (error) { notify('error', messageOf(error)) }
    finally { setBusy('') }
  }
  const queue = data.recordings.filter(recording => recording.mp4Job).slice(0, 3)
  const events = buildEvents(data)
  return <div className="ops-layout">
    <section className="ops-channel-panel">
      <header className="ops-section-head">
        <div className="ops-title"><h2>通道监看</h2><span className="count">{recordingCount}</span><small>正在写入 / {data.activeCount} 个活动任务 / 共 {data.streams.length} 路</small></div>
        <div className="ops-tools">
          <label className="ops-filter"><Filter /><select aria-label="通道状态" value={filter} onChange={event => setFilter(event.target.value as typeof filter)}><option value="all">全部通道</option><option value="recording">正在收录</option><option value="waiting">等待输入</option><option value="alert">异常通道</option></select></label>
          <button className="tool-button" title="列设置"><Columns3 /></button>
          <button className="text-button" onClick={() => setView('streams')}>管理通道</button>
        </div>
      </header>
      <div className="ops-table-wrap">
        <table className="ops-table">
          <thead><tr><th>通道名称</th><th>端点信息</th><th>信号状态</th><th>录制状态</th><th>运行时间 (TC)</th><th>码率</th><th>文件大小</th><th>音频轨</th><th>操作</th></tr></thead>
          <tbody>{rows.map(({stream, active, latest, state, signal, displayStatus, index}) => {
            const recording = active || latest
            const waiting = state === 'waiting_input'
            return <tr key={stream.id} className={state === 'failed' ? 'alert-row' : ''}>
              <td><div className="channel-name"><span className="channel-no">{String(index + 1).padStart(2, '0')}</span><div><strong>{stream.name}</strong><small className="mono">{stream.streamId || shortId(stream.id)}</small></div></div></td>
              <td><div className="endpoint-cell"><span>{stream.mode === 'listener' ? 'Listener' : 'Caller'}</span><strong className="mono">{stream.mode === 'listener' ? `0.0.0.0:${stream.port}` : `${stream.host}:${stream.port}`}</strong><small>{endpointHint(stream, state, signal)}</small></div></td>
              <td><span className={`signal-state ${signal}`} title={signalTitle(recording, data.serverTime)}>{signal === 'locked' ? <Wifi /> : signal === 'stalled' ? <AlertTriangle /> : <WifiOff />}{signal === 'locked' ? '媒体正常' : signal === 'stalled' ? '进度停滞' : state === 'failed' ? '信号中断' : state === 'finalizing' ? '已停止输入' : waiting ? '等待信号' : '未连接'}</span></td>
              <td><StatusBadge status={displayStatus} /></td>
              <td className="timecode mono">{recording ? formatTimecode(recording.progressMs) : '--:--:--:--'}</td>
              <td className="telemetry mono">{recording?.progressBitrate || '0 bps'}</td>
              <td className="telemetry mono">{formatBytes(recording?.progressSize || (recording ? fileOf(recording, 'ts')?.sizeBytes : 0) || 0)}</td>
              <td className="track-count">{audioCount(recording)}</td>
              <td>{active ? <button className="row-action stop" disabled={busy === stream.id} onClick={() => run(stream.id, () => api.stopRecording(active.id), '停止命令已发送')}><Square />停止录制</button> : <button className="row-action start" disabled={busy === stream.id} onClick={() => run(stream.id, () => api.startRecording(stream.id), '录制任务已创建')}><Play />开始录制</button>}</td>
            </tr>
          })}</tbody>
        </table>
        {!rows.length && <EmptyState icon={<Radio />} title={data.streams.length ? '没有符合条件的通道' : '暂无输入通道'} text={data.streams.length ? '切换状态筛选查看其他通道。' : '进入通道监看添加 Listener 或 Caller 输入。'} action={!data.streams.length ? <button className="button primary" onClick={() => setView('streams')}><Plus />添加通道</button> : undefined} />}
      </div>
    </section>
    <aside className="ops-events">
      <header className="ops-section-head"><div className="ops-title"><h2>事件 / 告警</h2><small>最近状态变化</small></div><button className="text-button" onClick={() => setView('recordings')}>全部</button></header>
      <div className="event-list">{events.map((event, index) => <div className={`event ${event.tone}`} key={`${event.time}-${index}`}><i /><div><time className="mono">{event.time}</time><strong>{event.title}</strong><p>{event.text}</p></div></div>)}</div>
      <div className="worker-rack"><div><Server /><span>执行节点</span><strong>{worker?.workerId || '等待上线'}</strong></div><div><span>活动进程</span><b>{worker?.activeRecordings || 0}</b></div><div><span>MP4 任务</span><b>{worker?.activeConversions || 0}</b></div></div>
    </aside>
    <div className="ops-bottom">
      <section className="console-panel storage-console">
        <header><h3>磁盘保护</h3><span className={diskGuard}><ShieldCheck />{diskGuardLabel(diskGuard)}</span></header>
        <div className="storage-values"><div><span>总容量</span><b>{worker ? formatBytes(worker.diskTotalBytes) : '—'}</b></div><div><span>已使用</span><b>{diskUsed.toFixed(1)}%</b></div><div><span>可用空间</span><b>{worker ? formatBytes(worker.diskFreeBytes) : '—'}</b></div></div>
        <div className="storage-bar"><span style={{width:`${Math.min(diskUsed, 100)}%`}} /></div>
        <small>硬保护保留 {data.settings.hardFreeGiB} GiB / {data.settings.hardFreePercent}%</small>
      </section>
      <section className="console-panel queue-console">
        <header><h3>MP4 队列</h3><div className="queue-counts"><span>等待中 <b>{data.queuedMp4}</b></span><span>并发 <b>{data.settings.mp4Concurrency}</b></span><button className="text-button" onClick={() => setView('recordings')}>查看全部</button></div></header>
        {queue.length ? <div className="queue-list">{queue.map(item => <div key={item.id}><strong>{item.streamName}</strong><span className="mono">{shortId(item.id)}.mp4</span><StatusBadge status={item.mp4Job?.status || 'ready'} /><div className="queue-progress"><i style={{width:`${item.mp4Job?.progress || 100}%`}} /></div><b>{Math.round(item.mp4Job?.progress || 100)}%</b></div>)}</div> : <div className="queue-empty"><CheckCircle2 />当前没有待处理的 MP4 任务</div>}
      </section>
    </div>
  </div>
}

function StreamsPage({ data, refresh, notify }: PageProps) {
  const [editing, setEditing] = useState<Stream | 'new' | null>(null)
  const [busy, setBusy] = useState<string | null>(null)
  const [confirmDelete, setConfirmDelete] = useState<Stream | null>(null)
  const [diagnosis, setDiagnosis] = useState<{ stream: Stream; result?: StreamDiagnosis; error?: string } | null>(null)
  const run = async (key: string, action: () => Promise<unknown>, success: string) => { setBusy(key); try { await action(); notify('success', success); await refresh() } catch (e) { notify('error', messageOf(e)) } finally { setBusy(null) } }
  const diagnose = async (stream: Stream) => { setDiagnosis({ stream }); try { setDiagnosis({ stream, result: await api.diagnoseStream(stream.id) }) } catch (e) { setDiagnosis({ stream, error: messageOf(e) }) } }
  const nextPort = useMemo(() => { const used = new Set(data.streams.filter(s => s.mode === 'listener').map(s => s.port)); for (let p = 9000; p <= 9099; p++) if (!used.has(p)) return p; return 9000 }, [data.streams])
  return <>
    <section className="page-heading"><div><h2>SRT 输入通道</h2><p>配置 Listener 或 Caller，并按需开始完整 TS 收录。</p></div><button className="button primary" onClick={() => setEditing('new')}><Plus />添加流</button></section>
    <div className="stream-grid">
      {data.streams.map(stream => { const active = activeFor(data.recordings, stream.id); const isBusy = busy === stream.id; return <article className="stream-card" key={stream.id}>
        <div className="stream-card-head"><div className={`channel-icon ${active ? 'active' : ''}`}>{active ? <Wifi /> : <Radio />}</div><div className="stream-actions">{stream.mode === 'caller' && <button className="icon-button" title="诊断 SRT 输入" onClick={() => diagnose(stream)} disabled={!!active || !!diagnosis && diagnosis.stream.id === stream.id && !diagnosis.result && !diagnosis.error}><Activity size={17} /></button>}<button className="icon-button" title="编辑" onClick={() => setEditing(stream)} disabled={!!active}><Pencil size={17} /></button><button className="icon-button danger-ghost" title="删除" onClick={() => setConfirmDelete(stream)} disabled={!!active}><Trash2 size={17} /></button></div></div>
        <h3>{stream.name}</h3><div className="endpoint mono">{displaySRTURL(stream)}</div>
        <div className="stream-meta"><span><b>{stream.mode.toUpperCase()}</b> 模式</span><span>{stream.latencyMs} ms 延迟</span><span>{stream.timeoutMs / 1000}s 超时</span><span>{stream.autoMp4 ? '自动 MP4' : '手动 MP4'}</span></div>
        <div className="stream-card-foot"><StatusBadge status={active?.status || 'idle'} />{active ? <button className="button stop" disabled={isBusy} onClick={() => run(stream.id, () => api.stopRecording(active.id), '停止命令已发送')}><Square />停止录制</button> : <button className="button primary compact" disabled={isBusy} onClick={() => run(stream.id, () => api.startRecording(stream.id), '录制任务已创建')}>{isBusy ? <span className="spinner" /> : <Play />}开始录制</button>}</div>
      </article> })}
      {!data.streams.length && <div className="panel full"><EmptyState icon={<Radio />} title="添加第一路 SRT 输入" text="Listener 会在指定 UDP 端口等待推流，Caller 会主动连接上游。" action={<button className="button primary" onClick={() => setEditing('new')}><Plus />添加流</button>} /></div>}
    </div>
    {editing && <StreamModal stream={editing === 'new' ? undefined : editing} defaultPort={nextPort} onClose={() => setEditing(null)} onSaved={async () => { setEditing(null); notify('success', editing === 'new' ? '流已添加' : '流配置已更新'); await refresh() }} />}
    {diagnosis && <StreamDiagnosisModal diagnosis={diagnosis} onClose={() => setDiagnosis(null)} />}
    {confirmDelete && <ConfirmModal title="删除流配置" text={`确定删除“${confirmDelete.name}”吗？已有录制文件不会被删除。`} confirmText="删除流" danger onClose={() => setConfirmDelete(null)} onConfirm={async () => { await run(confirmDelete.id, () => api.deleteStream(confirmDelete.id), '流配置已删除'); setConfirmDelete(null) }} />}
  </>
}

function StreamModal({ stream, defaultPort, onClose, onSaved }: { stream?: Stream; defaultPort: number; onClose: () => void; onSaved: () => void }) {
  const [form, setForm] = useState<StreamForm>(stream ? { name: stream.name, mode: stream.mode, host: stream.host, port: stream.port, streamId: stream.streamId, latencyMs: stream.latencyMs, timeoutMs: stream.timeoutMs, passphrase: '', clearPassphrase: false, autoMp4: stream.autoMp4, sourceUrl: stream.mode === 'caller' ? displaySRTURL(stream) : '' } : { ...emptyStream, port: defaultPort })
  const [sourceTouched, setSourceTouched] = useState(false)
  const [advanced, setAdvanced] = useState(false), [busy, setBusy] = useState(false), [error, setError] = useState('')
  const submit = async (event: FormEvent) => { event.preventDefault(); setBusy(true); setError(''); const payload = sourceTouched ? form : { ...form, sourceUrl: '' }; try { stream ? await api.updateStream(stream.id, payload) : await api.createStream(payload); onSaved() } catch (e) { setError(messageOf(e)) } finally { setBusy(false) } }
  const set = <K extends keyof StreamForm>(key: K, value: StreamForm[K]) => { const clearsURL = ['mode', 'host', 'port', 'streamId'].includes(String(key)); if (clearsURL) setSourceTouched(false); setForm(current => ({ ...current, [key]: value, sourceUrl: clearsURL ? '' : current.sourceUrl })) }
  const setSourceUrl = (value: string) => { setSourceTouched(!!value.trim()); setForm(current => ({ ...current, ...parseSRTURL(value, current), sourceUrl: value })) }
  return <Modal onClose={onClose}><form className="modal-form" onSubmit={submit}><div className="modal-head"><div><span className="eyebrow">SRT CHANNEL</span><h2>{stream ? '编辑输入通道' : '添加输入通道'}</h2></div><button type="button" className="icon-button" onClick={onClose}><X /></button></div>
    <label>完整 SRT URL<input value={form.sourceUrl || ''} onChange={e => setSourceUrl(e.target.value)} placeholder="例如：srt://192.168.1.30:8890?streamid=read:27srt-h1" /><small>粘贴转发服务地址后会自动识别为 Caller，并提取 Stream ID 和通道名称；手动修改主机、端口或 Stream ID 时会以分项参数为准。</small></label>
    <label>通道名称<input autoFocus value={form.name} onChange={e => set('name', e.target.value)} placeholder="例如：演播室主输出" /></label>
    <div className="segmented"><button type="button" className={form.mode === 'listener' ? 'active' : ''} onClick={() => set('mode', 'listener')}>Listener 接收推流</button><button type="button" className={form.mode === 'caller' ? 'active' : ''} onClick={() => set('mode', 'caller')}>Caller 连接上游</button></div>
    <div className="form-grid">{form.mode === 'caller' && <label className="span-two">上游主机<input value={form.host} onChange={e => set('host', e.target.value)} placeholder="192.168.1.30 或域名" /></label>}<label>{form.mode === 'listener' ? '监听 UDP 端口' : '上游端口'}<input type="number" value={form.port} onChange={e => set('port', Number(e.target.value))} min={form.mode === 'listener' ? 9000 : 1} max={form.mode === 'listener' ? 9099 : 65535} /></label><label>录制结束后<input className="hidden-input" /><span className="switch-row">自动生成 MP4 <button type="button" className={`switch ${form.autoMp4 ? 'on' : ''}`} onClick={() => set('autoMp4', !form.autoMp4)}><span /></button></span></label></div>
    <button type="button" className="advanced-toggle" onClick={() => setAdvanced(v => !v)}>高级 SRT 参数 <span>{advanced ? '−' : '+'}</span></button>
    {advanced && <div className="advanced-panel"><div className="form-grid"><label>延迟（毫秒）<input type="number" value={form.latencyMs} min={20} max={8000} onChange={e => set('latencyMs', Number(e.target.value))} /></label><label>无数据超时（毫秒）<input type="number" value={form.timeoutMs} min={5000} max={300000} step={1000} onChange={e => set('timeoutMs', Number(e.target.value))} /></label><label className="span-two">Stream ID<input value={form.streamId} onChange={e => set('streamId', e.target.value)} placeholder="可选" /></label><label className="span-two">SRT 加密口令<input type="password" value={form.passphrase} onChange={e => set('passphrase', e.target.value)} placeholder={stream?.hasPassphrase ? '已设置；留空表示保持不变' : '可选，10–79个字符'} /></label>{stream?.hasPassphrase && <label className="checkbox span-two"><input type="checkbox" checked={form.clearPassphrase} onChange={e => set('clearPassphrase', e.target.checked)} />清除已有加密口令</label>}</div></div>}
    {error && <div className="form-error"><AlertTriangle size={16} />{error}</div>}<div className="modal-actions"><button type="button" className="button secondary" onClick={onClose}>取消</button><button className="button primary" disabled={busy}>{busy ? <span className="spinner" /> : <Save />}{stream ? '保存配置' : '创建通道'}</button></div>
  </form></Modal>
}

function StreamDiagnosisModal({ diagnosis, onClose }: { diagnosis: { stream: Stream; result?: StreamDiagnosis; error?: string }; onClose: () => void }) {
  const result = diagnosis.result
  const videos = result?.streams?.video || []
  const audios = result?.streams?.audio || []
  return <Modal onClose={onClose}><div className="preview-modal">
    <div className="modal-head"><div><span className="eyebrow">SRT DIAGNOSTIC</span><h2>{diagnosis.stream.name}</h2></div><button className="icon-button" onClick={onClose}><X /></button></div>
    {!result && !diagnosis.error && <div className="diagnostic-note"><Activity /><div><strong>正在探测输入</strong><p>系统会用当前通道参数连接上游，最多等待 12 秒，看看是否能拿到媒体流。</p></div></div>}
    {diagnosis.error && <div className="error-box"><AlertTriangle /><div><strong>诊断请求失败</strong><p>{diagnosis.error}</p></div></div>}
    {result && <div className="diagnostic-result">
      <div className={`diagnostic-banner ${result.ok ? 'ok' : 'fail'}`}>{result.ok ? <CheckCircle2 /> : <AlertTriangle />}<div><strong>{result.ok ? '已探测到媒体流' : '未收到可识别媒体'}</strong><p>{result.hint}</p></div></div>
      <div className="detail-grid"><Detail label="实际探测 URL" value={result.url} mono /><Detail label="耗时" value={`${result.durationMs} ms`} mono /></div>
      {result.error && <pre className="diagnostic-error">{result.error}</pre>}
      {videos.length > 0 && <section className="detail-section"><h3>视频</h3>{videos.map((video, index) => <div className="codec-card" key={index}><Film /><div><strong>{(video.codec || 'unknown').toUpperCase()} · {video.profile || 'Unknown profile'}</strong><span>{video.width || '—'} × {video.height || '—'}</span></div></div>)}</section>}
      {audios.length > 0 && <section className="detail-section"><h3>音轨 <span>{audios.length}</span></h3>{audios.map((audio, index) => <div className="audio-row" key={index}><span>A{index + 1}</span><div><strong>{(audio.codec || 'unknown').toUpperCase()}</strong><small>{audio.channels || '—'} 声道 · {audio.sampleRate || '—'} Hz {audio.language ? `· ${audio.language}` : ''}</small></div></div>)}</section>}
    </div>}
  </div></Modal>
}

function RecordingsPage({ data, refresh, notify }: PageProps) {
  const [status, setStatus] = useState(''), [streamId, setStreamId] = useState(''), [selected, setSelected] = useState<Recording | null>(null), [preview, setPreview] = useState<Recording | null>(null), [confirmDelete, setConfirmDelete] = useState<{recording: Recording; kind: 'ts' | 'mp4'} | null>(null), [confirmClear, setConfirmClear] = useState<Recording | null>(null), [busy, setBusy] = useState('')
  const items = data.recordings.filter(r => (!status || r.status === status) && (!streamId || r.streamId === streamId))
  const run = async (key: string, action: () => Promise<unknown>, success: string) => { setBusy(key); try { await action(); notify('success', success); await refresh() } catch (e) { notify('error', messageOf(e)) } finally { setBusy('') } }
  return <>
    <section className="page-heading"><div><h2>录制文件</h2><p>管理完整 TS 母版与录制结束后生成的 MP4。</p></div><div className="recording-total"><Library />共 {data.recordings.length} 条任务</div></section>
    <section className="panel filters"><label>输入通道<select value={streamId} onChange={e => setStreamId(e.target.value)}><option value="">全部通道</option>{data.streams.map(s => <option key={s.id} value={s.id}>{s.name}</option>)}</select></label><label>录制状态<select value={status} onChange={e => setStatus(e.target.value)}><option value="">全部状态</option><option value="waiting_input">等待输入</option><option value="recording">录制中</option><option value="finalizing">正在收尾</option><option value="ready">已完成</option><option value="failed">异常</option></select></label><div className="filter-spacer" /><button className="button secondary" onClick={() => { setStatus(''); setStreamId('') }}>清除筛选</button></section>
    <section className="panel recordings-panel">{items.length ? <div className="recording-list">{items.map(item => { const ts = fileOf(item, 'ts'), mp4 = fileOf(item, 'mp4'); return <article className="recording-item" key={item.id}>
      <button className="recording-main" onClick={() => setSelected(item)}><div className={`recording-icon ${item.status}`}><Film /></div><div className="recording-name"><strong>{item.streamName}</strong><span>{formatDate(item.startedAt || item.requestedAt, true)} · <span className="mono">{shortId(item.id)}</span></span></div><StatusBadge status={item.status} /><div className="recording-stat"><span>时长</span><b>{formatDuration(item.progressMs || ts?.durationMs || 0)}</b></div><div className="recording-stat"><span>TS 大小</span><b>{formatBytes(ts?.sizeBytes || item.progressSize)}</b></div><div className="file-chips"><span className={ts ? 'ready' : ''}>TS</span><span className={mp4 ? 'ready' : item.mp4Job?.status === 'running' ? 'working' : ''}>MP4</span></div></button>
      <div className="recording-actions">{item.status === 'recording' || item.status === 'waiting_input' ? <button className="button stop compact" disabled={busy === item.id} onClick={() => run(item.id, () => api.stopRecording(item.id), '停止命令已发送')}><Square />停止</button> : ts && !mp4 && item.mp4Job?.status !== 'queued' && item.mp4Job?.status !== 'running' ? <button className="button secondary compact" disabled={busy === item.id} onClick={() => run(item.id, () => api.generateMp4(item.id), 'MP4任务已加入队列')}><Film />生成 MP4</button> : null}{canClearRecording(item) && <button className="icon-button danger-ghost" title={item.status === 'failed' ? '清除告警' : '删除空记录'} disabled={busy === item.id} onClick={() => setConfirmClear(item)}><Trash2 size={18} /></button>}{mp4 && <button className="icon-button" title="预览MP4" onClick={() => setPreview(item)}><Eye size={18} /></button>}{ts && <a className="icon-button" title="下载TS" href={`/api/v1/recordings/${item.id}/files/ts?download=1`}><Download size={18} /></a>}</div>
      {item.mp4Job?.status === 'running' && <div className="job-progress"><span style={{ width: `${item.mp4Job.progress}%` }} /><b>{item.mp4Job.progress.toFixed(0)}%</b></div>}
    </article> })}</div> : <EmptyState icon={<Library />} title="没有符合条件的录制" text="调整筛选条件，或从流管理页面开始录制。" />}</section>
    {selected && <RecordingDrawer recording={selected} onClose={() => setSelected(null)} onPreview={() => setPreview(selected)} onDelete={(kind) => { setSelected(null); setConfirmDelete({recording:selected,kind}) }} />}
    {preview && fileOf(preview, 'mp4') && <Modal onClose={() => setPreview(null)}><div className="preview-modal"><div className="modal-head"><div><span className="eyebrow">MP4 PREVIEW</span><h2>{preview.streamName}</h2></div><button className="icon-button" onClick={() => setPreview(null)}><X /></button></div><video controls autoPlay src={`/api/v1/recordings/${preview.id}/files/mp4`} /><div className="preview-meta">{formatDate(preview.startedAt || preview.requestedAt, true)} · {formatDuration(fileOf(preview,'mp4')?.durationMs || 0)}</div></div></Modal>}
    {confirmDelete && <ConfirmModal danger title={`删除 ${confirmDelete.kind.toUpperCase()} 文件`} text="此操作会永久删除宿主机数据盘中的文件，且无法恢复。另一种格式不会被删除。" confirmText="永久删除" onClose={() => setConfirmDelete(null)} onConfirm={async () => { const target=confirmDelete; setConfirmDelete(null); await run(target.recording.id+target.kind, () => api.deleteFile(target.recording.id,target.kind), '删除任务已提交') }} />}
    {confirmClear && <ConfirmModal danger title={confirmClear.status === 'failed' ? '清除异常告警' : '删除空录制记录'} text={confirmClear.status === 'failed' ? `将“${confirmClear.streamName}”这条失败录制从告警和录制列表中移除，不会删除宿主机上的任何残留文件。` : `将“${confirmClear.streamName}”这条无媒体文件的已结束任务从录制列表中移除。`} confirmText={confirmClear.status === 'failed' ? '清除告警' : '删除记录'} onClose={() => setConfirmClear(null)} onConfirm={async () => { const target=confirmClear; setConfirmClear(null); await run(target.id, () => api.deleteRecording(target.id), target.status === 'failed' ? '异常告警已清除' : '空录制记录已删除') }} />}
  </>
}

function RecordingDrawer({ recording, onClose, onPreview, onDelete }: { recording: Recording; onClose: () => void; onPreview: () => void; onDelete: (kind: 'ts' | 'mp4') => void }) {
  const ts=fileOf(recording,'ts'),mp4=fileOf(recording,'mp4'),video=ts?.codecs.video?.[0],audios=ts?.codecs.audio || []
  return <><button className="drawer-scrim" onClick={onClose} /><aside className="drawer"><div className="modal-head"><div><span className="eyebrow">RECORDING DETAIL</span><h2>{recording.streamName}</h2></div><button className="icon-button" onClick={onClose}><X /></button></div><StatusBadge status={recording.status} /><div className="detail-grid"><Detail label="任务 ID" value={recording.id} mono /><Detail label="开始时间" value={formatDate(recording.startedAt || recording.requestedAt,true)} /><Detail label="结束时间" value={recording.endedAt ? formatDate(recording.endedAt,true) : '—'} /><Detail label="结束原因" value={reasonLabel(recording.endReason)} /><Detail label="录制时长" value={formatDuration(recording.progressMs || ts?.durationMs || 0)} /><Detail label="自动 MP4" value={recording.autoMp4 ? '已启用' : '未启用'} /></div>{video && <section className="detail-section"><h3>视频</h3><div className="codec-card"><Film /><div><strong>{video.codec.toUpperCase()} · {video.profile || 'Unknown profile'}</strong><span>{video.width} × {video.height}</span></div></div></section>}<section className="detail-section"><h3>音轨 <span>{audios.length}</span></h3>{audios.map((audio,index)=><div className="audio-row" key={index}><span>A{index+1}</span><div><strong>{audio.codec.toUpperCase()}</strong><small>{audio.channels} 声道 · {audio.sampleRate || '—'} Hz {audio.language ? `· ${audio.language}` : ''}</small></div></div>)}</section><section className="detail-section"><h3>文件</h3>{[ts,mp4].filter(Boolean).map(file=><div className="file-row" key={file!.id}><span className="file-kind">{file!.kind.toUpperCase()}</span><div><strong>{file!.name}</strong><small>{formatBytes(file!.sizeBytes)}</small></div><div className="file-row-actions">{file!.kind==='mp4'&&<button className="icon-button" onClick={onPreview}><Eye /></button>}<a className="icon-button" href={`/api/v1/recordings/${recording.id}/files/${file!.kind}?download=1`}><Download /></a><button className="icon-button danger-ghost" onClick={()=>onDelete(file!.kind)}><Trash2 /></button></div></div>)}</section>{recording.error&&<div className="error-box"><AlertTriangle /><div><strong>任务异常</strong><p>{recording.error}</p></div></div>}</aside></>
}

function SettingsPage({ data, refresh, notify }: PageProps) {
  const [settings,setSettings]=useState<Settings>(data.settings),[busy,setBusy]=useState(false),[current,setCurrent]=useState(''),[next,setNext]=useState(''),[confirm,setConfirm]=useState('')
  useEffect(()=>setSettings(data.settings),[data.settings])
  const save=async()=>{setBusy(true);try{await api.saveSettings(settings);notify('success','系统设置已保存');await refresh()}catch(e){notify('error',messageOf(e))}finally{setBusy(false)}}
  const changePassword=async(event:FormEvent)=>{event.preventDefault();if(next!==confirm){notify('error','两次输入的新密码不一致');return}setBusy(true);try{await api.changePassword(current,next);notify('success','密码已修改，请重新登录');location.reload()}catch(e){notify('error',messageOf(e))}finally{setBusy(false)}}
  return <><section className="page-heading"><div><h2>系统设置</h2><p>调整转封装资源、磁盘保护水位并查看运行诊断。</p></div></section><div className="settings-grid"><section className="panel"><PanelHeader title="转封装与磁盘保护" subtitle="Worker 每15秒同步一次" /><div className="settings-form"><label>MP4 最大并发<input type="number" min={1} max={8} value={settings.mp4Concurrency} onChange={e=>setSettings({...settings,mp4Concurrency:Number(e.target.value)})}/><small>建议本地机械盘使用 1–2，NVMe 可适当提高。</small></label><div className="settings-divider" /><h3>软水位 · 禁止新任务</h3><div className="form-grid"><label>剩余百分比<input type="number" value={settings.softFreePercent} onChange={e=>setSettings({...settings,softFreePercent:Number(e.target.value)})}/></label><label>剩余容量 GiB<input type="number" value={settings.softFreeGiB} onChange={e=>setSettings({...settings,softFreeGiB:Number(e.target.value)})}/></label></div><h3>硬水位 · 停止活动录制</h3><div className="form-grid"><label>剩余百分比<input type="number" value={settings.hardFreePercent} onChange={e=>setSettings({...settings,hardFreePercent:Number(e.target.value)})}/></label><label>剩余容量 GiB<input type="number" value={settings.hardFreeGiB} onChange={e=>setSettings({...settings,hardFreeGiB:Number(e.target.value)})}/></label></div><button className="button primary" onClick={save} disabled={busy}><Save />保存系统设置</button></div></section><section className="panel"><PanelHeader title="Worker 诊断" subtitle="录制执行节点" /><WorkerDetail worker={data.workers[0]} detailed /><div className="diagnostic-note"><ShieldCheck /><div><strong>数据保护已启用</strong><p>不自动删除旧录像。达到硬水位时会优雅停止所有活动录制。</p></div></div></section><section className="panel"><PanelHeader title="修改管理员密码" subtitle="修改后所有会话都会退出" /><form className="settings-form" onSubmit={changePassword}><label>当前密码<input type="password" value={current} onChange={e=>setCurrent(e.target.value)} autoComplete="current-password" /></label><label>新密码<input type="password" value={next} onChange={e=>setNext(e.target.value)} minLength={12} autoComplete="new-password" /><small>至少12个字符。</small></label><label>确认新密码<input type="password" value={confirm} onChange={e=>setConfirm(e.target.value)} autoComplete="new-password" /></label><button className="button secondary" disabled={busy||!current||!next}><KeyRound />更新密码</button></form></section><section className="panel"><PanelHeader title="部署信息" subtitle="当前版本与接口" /><div className="detail-grid single"><Detail label="应用版本" value={data.workers[0]?.version || '—'} /><Detail label="录制根目录" value="/data/recordings" mono /><Detail label="SRT Listener范围" value="9000–9099 / UDP" /><Detail label="管理接口" value={location.origin} mono /></div></section></div></>
}

function MetricCard({ title,value,detail,icon,tone }: {title:string;value:string|number;detail:string;icon:ReactNode;tone:string}) { return <article className={`metric-card ${tone}`}><div className="metric-icon">{icon}</div><div><span>{title}</span><strong>{value}</strong><small>{detail}</small></div></article> }
function PanelHeader({title,subtitle,action}:{title:string;subtitle:string;action?:ReactNode}){return <header className="panel-head"><div><h3>{title}</h3><p>{subtitle}</p></div>{action}</header>}
function NavItem({active,icon,label,onClick}:{active:boolean;icon:ReactNode;label:string;onClick:()=>void}){return <button className={`nav-item ${active?'active':''}`} onClick={onClick}>{icon}<span>{label}</span></button>}
function Brand({large=false}:{large?:boolean}){return <div className={`brand ${large?'large':''}`}><img src="/logo.png" alt="Live Media Mesh" /><div><strong>TS Ingest</strong><span>Live Media Mesh</span></div></div>}
function WorkerPill({worker}:{worker?:WorkerHeartbeat}){const online=workerIsOnline(worker);return <div className={`worker-pill ${online?'online':'offline'}`}><span />{online?'Worker 在线':'Worker 离线'}</div>}
function workerIsOnline(worker:WorkerHeartbeat|undefined){return !!worker&&Date.now()-new Date(worker.lastSeenAt).getTime()<15000}
function StatusBadge({status}:{status:string}){return <span className={`status ${status}`}><i />{statusLabel(status)}</span>}
function EmptyState({icon,title,text,action}:{icon:ReactNode;title:string;text:string;action?:ReactNode}){return <div className="empty-state"><div>{icon}</div><h3>{title}</h3><p>{text}</p>{action}</div>}
function DiskGauge({worker,settings}:{worker?:WorkerHeartbeat;settings:Settings}){if(!worker)return <EmptyState icon={<Server/>} title="等待 Worker" text="节点上线后会显示磁盘容量。"/>;const used=worker.diskTotalBytes?100-worker.diskFreeBytes*100/worker.diskTotalBytes:0;return <div className="disk-gauge"><div className="disk-readout"><strong className="mono">{used.toFixed(1)}%</strong><span>空间已使用</span><div className="storage-bar"><i style={{width:`${Math.min(used,100)}%`}} /></div></div><div className="disk-legend"><div><span>总容量</span><b>{formatBytes(worker.diskTotalBytes)}</b></div><div><span>可用空间</span><b>{formatBytes(worker.diskFreeBytes)}</b></div><div><span>硬保护</span><b>{settings.hardFreeGiB} GiB / {settings.hardFreePercent}%</b></div></div></div>}
function WorkerDetail({worker,detailed=false}:{worker?:WorkerHeartbeat;detailed?:boolean}){if(!worker)return <EmptyState icon={<Server/>} title="Worker 未上线" text="请检查 worker 容器和数据库连接。"/>;const online=Date.now()-new Date(worker.lastSeenAt).getTime()<15000;return <div className="worker-detail"><div className="worker-title"><div className={`server-icon ${online?'online':''}`}><Server/></div><div><strong>{worker.workerId}</strong><span>v{worker.version} · {online?'最近刚刚上报':'心跳已超时'}</span></div></div><div className="worker-stats"><div><span>录制进程</span><b>{worker.activeRecordings}</b></div><div><span>MP4任务</span><b>{worker.activeConversions}</b></div>{detailed&&<><div><span>磁盘总量</span><b>{formatBytes(worker.diskTotalBytes)}</b></div><div><span>磁盘可用</span><b>{formatBytes(worker.diskFreeBytes)}</b></div></>}</div></div>}
function Modal({children,onClose}:{children:ReactNode;onClose:()=>void}){return <div className="modal-layer"><button className="modal-scrim" onClick={onClose}/><div className="modal">{children}</div></div>}
function ConfirmModal({title,text,confirmText,danger=false,onClose,onConfirm}:{title:string;text:string;confirmText:string;danger?:boolean;onClose:()=>void;onConfirm:()=>Promise<void>}){const[busy,setBusy]=useState(false),[error,setError]=useState('');return <Modal onClose={onClose}><div className="confirm-modal"><div className={`confirm-icon ${danger?'danger':''}`}>{danger?<Trash2/>:<AlertTriangle/>}</div><h2>{title}</h2><p>{text}</p>{error&&<div className="form-error">{error}</div>}<div className="modal-actions"><button className="button secondary" onClick={onClose}>取消</button><button className={`button ${danger?'danger':'primary'}`} disabled={busy} onClick={async()=>{setBusy(true);try{await onConfirm()}catch(e){setError(messageOf(e));setBusy(false)}}}>{busy?<span className="spinner"/>:danger?<Trash2/>:<CheckCircle2/>}{confirmText}</button></div></div></Modal>}
function Detail({label,value,mono=false}:{label:string;value:string;mono?:boolean}){return <div className="detail"><span>{label}</span><strong className={mono?'mono':''}>{value}</strong></div>}
function PageSkeleton(){return <div className="skeleton-grid"><div/><div/><div/><div/><section/><section/></div>}
function LoadingScreen(){return <div className="loading-screen"><div className="loading-mark"><Activity/></div><strong>TS Ingest</strong><span>正在连接管理服务</span></div>}

type PageProps={data:Dashboard;refresh:()=>Promise<void>;notify:(kind:Toast['kind'],text:string)=>void}
function activeFor(recordings:Recording[],streamId:string){return recordings.find(r=>r.streamId===streamId&&['waiting_input','recording','finalizing'].includes(r.status))}
function fileOf(recording:Recording,kind:'ts'|'mp4'):MediaFile|undefined{return recording.files?.find(file=>file.kind===kind)}
function canClearRecording(recording:Recording){return recording.status==='failed'||(!['waiting_input','recording','finalizing'].includes(recording.status)&&!recording.files?.length)}
function formatBytes(bytes:number){if(!bytes)return '0 B';const units=['B','KB','MB','GB','TB','PB'];const i=Math.min(Math.floor(Math.log(bytes)/Math.log(1024)),units.length-1);return `${(bytes/1024**i).toFixed(i>2?2:i>0?1:0)} ${units[i]}`}
function diskGuardLevel(worker:WorkerHeartbeat|undefined,settings:Settings):'normal'|'soft'|'hard'{if(!worker?.diskTotalBytes)return 'normal';const freePercent=worker.diskFreeBytes*100/worker.diskTotalBytes;const freeGiB=worker.diskFreeBytes/1024/1024/1024;if(freePercent<=settings.hardFreePercent||freeGiB<=settings.hardFreeGiB)return 'hard';if(freePercent<=settings.softFreePercent||freeGiB<=settings.softFreeGiB)return 'soft';return 'normal'}
function diskGuardLabel(level:'normal'|'soft'|'hard'){return level==='hard'?'硬保护':level==='soft'?'限制新任务':'正常'}
function formatDuration(ms:number){if(!ms)return '00:00:00';const total=Math.floor(ms/1000),h=Math.floor(total/3600),m=Math.floor(total%3600/60),s=total%60;return [h,m,s].map(v=>String(v).padStart(2,'0')).join(':')}
function formatTimecode(ms:number){return `${formatDuration(ms)}:${String(Math.floor((ms%1000)/40)).padStart(2,'0')}`}
function formatFullTime(value:string){return new Intl.DateTimeFormat('zh-CN',{year:'numeric',month:'2-digit',day:'2-digit',hour:'2-digit',minute:'2-digit',second:'2-digit',hour12:false}).format(new Date(value)).replaceAll('/','-')}
function formatDate(value:string|undefined,withSeconds=false){if(!value)return '—';return new Intl.DateTimeFormat('zh-CN',{month:'2-digit',day:'2-digit',hour:'2-digit',minute:'2-digit',second:withSeconds?'2-digit':undefined,hour12:false}).format(new Date(value))}
function audioCount(recording?:Recording){if(!recording)return '—';const ts=fileOf(recording,'ts');return ts?.codecs.audio?.length ?? '—'}
function endpointHint(stream:Stream,state:string,signal:string){
  if(stream.mode==='caller') return state==='recording'&&signal==='locked'?'上游已连接 · 正在接收媒体':`延迟 ${stream.latencyMs} ms`
  if(state==='recording'&&signal==='locked') return 'Caller 已连接 · 正在接收媒体'
  if(state==='recording'&&signal==='stalled') return 'Caller 已连接 · 媒体进度停滞'
  if(state==='waiting_input') return '监听中 · 等待 Caller 推流'
  if(state==='finalizing') return '输入已停止 · 正在收尾'
  if(state==='failed') return '连接中断或录制异常'
  return '监听端口空闲'
}
function signalHealth(stream:Stream,active:Recording|undefined,latest:Recording|undefined,serverTime:string):'locked'|'stalled'|'waiting'|'lost'|'idle'|'finalizing'{if(!active)return latest?.status==='failed'?'lost':'idle';if(active.status==='waiting_input')return 'waiting';if(active.status==='finalizing')return 'finalizing';if(active.status!=='recording')return 'idle';if(!active.lastProgressAt||active.progressSize<=0)return 'stalled';const age=new Date(serverTime).getTime()-new Date(active.lastProgressAt).getTime();const warningAfter=Math.max(5000,Math.min(stream.timeoutMs/3,10000));return age>warningAfter?'stalled':'locked'}
function signalTitle(recording:Recording|undefined,serverTime:string){if(!recording?.lastProgressAt)return '尚未收到有效媒体进度';const age=Math.max(0,new Date(serverTime).getTime()-new Date(recording.lastProgressAt).getTime());return `最后媒体进度：${Math.round(age/1000)} 秒前`}
function displaySRTURL(stream:Stream){if(stream.mode==='listener')return `srt://0.0.0.0:${stream.port}`;return `srt://${stream.host}:${stream.port}${stream.streamId?`?streamid=${stream.streamId}`:''}`}
function parseSRTURL(value:string,current:StreamForm):Partial<StreamForm>{
  const raw=value.trim()
  if(!raw)return {}
  try{
    const parsed=new URL(raw)
    if(parsed.protocol!=='srt:')return {}
    const streamId=parsed.searchParams.get('streamid')||parsed.searchParams.get('streamId')||''
    const name=current.name.trim()?current.name:streamNameFromSRT(streamId,parsed.hostname,Number(parsed.port))
    return {mode:'caller',host:parsed.hostname,port:Number(parsed.port)||current.port,streamId,name}
  }catch{return {}}
}
function streamNameFromSRT(streamId:string,host:string,port:number){const last=streamId.split(':').map(v=>v.trim()).filter(Boolean).pop();return last||`${host}-${port||''}`.replace(/-$/,'')}
function buildEvents(data:Dashboard){
  const items=data.recordings.slice(0,7).map(recording=>({time:formatEventTime(recording.endedAt||recording.startedAt||recording.requestedAt),title:recording.streamName,text:recording.status==='failed'?`${reasonLabel(recording.endReason)} · ${recording.error||'请检查输入信号'}`:recording.status==='recording'?`开始录制 · ${shortId(recording.id)}`:recording.status==='ready'?`TS 已完成 · ${formatBytes(fileOf(recording,'ts')?.sizeBytes||recording.progressSize)}`:statusLabel(recording.status),tone:recording.status==='failed'?'danger':recording.status==='recording'?'recording':'ok'}))
  if(!items.length)items.push({time:formatEventTime(data.serverTime),title:'系统',text:'Worker 已连接，等待收录任务',tone:'ok'})
  return items
}
function formatEventTime(value:string|undefined){if(!value)return '--:--:--';return new Intl.DateTimeFormat('zh-CN',{hour:'2-digit',minute:'2-digit',second:'2-digit',hour12:false}).format(new Date(value))}
function shortId(id:string){return id.slice(0,8)}
function statusLabel(status:string){return ({waiting_input:'等待输入',recording:'正在录制',stalled:'录制停滞',finalizing:'正在收尾',ready:'已完成',failed:'异常',idle:'空闲',queued:'排队中',running:'处理中'} as Record<string,string>)[status]||status}
function reasonLabel(reason:string){return ({operator_stop:'管理员停止',source_disconnect:'SRT源断开',disk_guard:'磁盘保护停止',worker_shutdown:'Worker停止',process_error:'FFmpeg异常'} as Record<string,string>)[reason]||reason||'—'}
function viewTitle(view:View){return ({dashboard:'运行总览',streams:'通道监看',recordings:'录制文件',settings:'系统设置'})[view]}
function messageOf(error:unknown){return error instanceof ApiError||error instanceof Error?error.message:'操作失败，请稍后重试'}
function notifyError(error:unknown,set:(toast:Toast)=>void){set({kind:'error',text:messageOf(error)})}
