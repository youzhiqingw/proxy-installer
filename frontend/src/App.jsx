import { useEffect, useMemo, useState } from 'react';
import QRCode from 'qrcode';
import appIcon from './assets/images/app-icon.png';
import {
  Activity,
  CheckCircle2,
  Clipboard,
  Cpu,
  Fingerprint,
  Gauge,
  Globe2,
  HardDrive,
  LayoutDashboard,
  Link2,
  ListChecks,
  Mail,
  Loader2,
  MemoryStick,
  Percent,
  Play,
  Plus,
  Radar,
  Rocket,
  Search,
  Server,
  Settings2,
  ShieldAlert,
  ShieldCheck,
  TerminalSquare,
  Tv,
  Trash2,
  Wifi,
  XCircle,
  Zap,
} from 'lucide-react';
import {
  CheckPorts,
  CleanupSelectedFootprint,
  InspectVPS,
  LoadAppState,
  MeasureLatency,
  RunIPQuality,
  RunNodeSpeedTest,
  RunSpeedTest,
  ScanFootprint,
  SaveAppState,
  StartDeploy,
  TestConnection,
  UninstallStarter,
} from '../wailsjs/go/main/App';
import { EventsOn } from '../wailsjs/runtime/runtime';
import { domainIconMap, labelIconMap } from './siteIcons';
import './App.css';

const tabs = [
  { id: 'dashboard', label: '仪表盘', desc: '状态总览', icon: LayoutDashboard },
  { id: 'configs', label: 'VPS 管理', desc: 'SSH 与体检', icon: Server },
  { id: 'deploy', label: '节点部署', desc: '协议与订阅', icon: Rocket },
  { id: 'speed', label: '测速中心', desc: '延迟与出口', icon: Gauge },
  { id: 'maintenance', label: '维护清理', desc: '印记与卸载', icon: Trash2 },
  { id: 'progress', label: '进度日志', desc: '部署事件', icon: TerminalSquare },
  { id: 'result', label: '节点信息', desc: '客户端订阅', icon: Link2 },
];

const protocols = [
  { id: 'vless-reality', label: 'VLESS Reality', desc: '推荐，TLS 伪装', port: 443, tone: 'blue', icon: 'shield' },
  { id: 'hy2', label: 'Hysteria2', desc: 'UDP 传输，抗丢包', port: 8443, tone: 'green', icon: 'wave' },
  { id: 'tuic', label: 'TUIC', desc: 'QUIC 低延迟', port: 8444, tone: 'amber', icon: 'bolt' },
  { id: 'trojan', label: 'Trojan', desc: '小火箭兼容强', port: 8445, tone: 'rose', icon: 'lock' },
  { id: 'ss', label: 'Shadowsocks', desc: '轻量通用', port: 8388, tone: 'slate', icon: 'layers' },
  { id: 'vmess', label: 'VMess', desc: 'V2rayNG 兼容', port: 2083, tone: 'violet', icon: 'nodes' },
];

const clients = [
  ['shadowrocket', 'Shadowrocket'],
  ['mihomo', 'Clash Meta'],
  ['v2rayng', 'V2rayNG'],
  ['singbox', 'sing-box'],
];

const defaultPorts = Object.fromEntries(protocols.map((item) => [item.id, item.port]));
const qualitySiteMeta = {
  ippure: { tone: 'blue' },
  ping0: { tone: 'green' },
  iplark: { tone: 'amber' },
};

const qualitySectionMeta = {
  basic: { icon: Globe2, tone: 'blue' },
  type: { icon: Fingerprint, tone: 'green' },
  risk: { icon: ShieldAlert, tone: 'rose' },
  factor: { icon: ListChecks, tone: 'amber' },
  stream: { icon: Tv, tone: 'violet' },
  mail: { icon: Mail, tone: 'slate' },
};

function App() {
  const [activeTab, setActiveTab] = useState('dashboard');
  const [activeClient, setActiveClient] = useState('shadowrocket');
  const [deploying, setDeploying] = useState(false);
  const [showLogs, setShowLogs] = useState(false);
  const [stateLoaded, setStateLoaded] = useState(false);
  const [progress, setProgress] = useState({ value: 0, status: 'idle', message: '等待操作', logs: [] });
  const [profiles, setProfiles] = useState([]);
  const [draft, setDraft] = useState({ name: '', host: '', user: 'root', port: 22, password: '' });
  const [speed, setSpeed] = useState({ running: false, qualityRunning: false, items: [], remote: null, node: null, quality: null, error: '', notice: '' });
  const [maintenance, setMaintenance] = useState({ running: false, footprint: null, logs: [], error: '' });
  const [deployConfig, setDeployConfig] = useState({
    profileId: '',
    nodeName: 'starter-node',
    selected: ['vless-reality', 'hy2', 'ss'],
    ports: defaultPorts,
    publicPorts: {},
    webPort: 8080,
    publicWebPort: 0,
    token: 'starter2026',
    rule: '/sub/{token}/{client}',
    sni: 'www.bing.com',
    advanced: false,
  });

  const activeMeta = tabs.find((item) => item.id === activeTab) || tabs[0];
  const profile = profiles.find((item) => item.id === deployConfig.profileId) || profiles[0];
  const reportProfile = profiles.find((item) => item.report) || profile;
  const report = reportProfile?.report;

  const subscriptions = useMemo(() => {
    const host = preferredEndpointHost(profile);
    const base = host ? `http://${formatHost(host)}:${externalWebPort(deployConfig)}` : '';
    return Object.fromEntries(clients.map(([key, label]) => [
      key,
      {
        label,
        url: base ? `${base}${deployConfig.rule.replace('{token}', deployConfig.token).replace('{client}', clientPath(key))}` : '等待 VPS',
      },
    ]));
  }, [profile, deployConfig]);

  useEffect(() => {
    let alive = true;
    (async () => {
      try {
        const saved = await callBackend(LoadAppState);
        if (!alive) return;
        if (Array.isArray(saved?.profiles)) {
          setProfiles(saved.profiles);
        }
        if (saved?.deployConfig && Object.keys(saved.deployConfig).length) {
          setDeployConfig((current) => ({
            ...current,
            ...saved.deployConfig,
            ports: { ...defaultPorts, ...(saved.deployConfig.ports || {}) },
            publicPorts: saved.deployConfig.publicPorts || {},
            selected: saved.deployConfig.selected?.length ? saved.deployConfig.selected : current.selected,
          }));
        }
        if (saved?.activeClient) {
          setActiveClient(saved.activeClient);
        }
      } catch (error) {
        console.warn('Load app state failed', error);
      } finally {
        if (alive) setStateLoaded(true);
      }
    })();
    return () => { alive = false; };
  }, []);

  useEffect(() => {
    if (!stateLoaded) return undefined;
    const timer = window.setTimeout(() => {
      callBackend(SaveAppState, {
        profiles,
        deployConfig,
        activeClient,
      }).catch((error) => console.warn('Save app state failed', error));
    }, 450);
    return () => window.clearTimeout(timer);
  }, [stateLoaded, profiles, deployConfig, activeClient]);

  useEffect(() => {
    if (!window.runtime?.EventsOnMultiple) {
      return undefined;
    }
    const off = EventsOn('deploy:event', (event) => {
      setProgress((current) => ({
        value: typeof event.percent === 'number' && event.percent > 0 ? event.percent : current.value,
        status: event.type === 'done' ? 'success' : event.type === 'error' ? 'failed' : 'running',
        message: event.message || current.message,
        logs: event.message ? [...current.logs, `[${event.type}] ${event.message}`] : current.logs,
      }));
      if (event.type === 'done') {
        setTimeout(() => setActiveTab('result'), 700);
      }
    });
    return () => off();
  }, []);

  const addProfile = () => {
    if (!draft.host.trim()) return;
    const next = { ...draft, host: draft.host.trim(), id: crypto.randomUUID(), status: '未体检' };
    setProfiles((current) => [...current, next]);
    setDeployConfig((current) => ({ ...current, profileId: next.id }));
    setDraft({ name: '', host: '', user: 'root', port: 22, password: '' });
  };

  const testProfile = async (item) => {
    try {
      setProfiles((current) => patchProfile(current, item.id, { status: '连接中' }));
      const result = await callBackend(TestConnection, item);
      setProfiles((current) => patchProfile(current, item.id, { status: result.message || '已连接' }));
    } catch (error) {
      setProfiles((current) => patchProfile(current, item.id, { status: `失败: ${error}` }));
    }
  };

  const inspectProfile = async (item) => {
    try {
      setProfiles((current) => patchProfile(current, item.id, { status: '体检中' }));
      const result = await callBackend(InspectVPS, item);
      setProfiles((current) => patchProfile(current, item.id, { status: '体检完成', report: result.report }));
    } catch (error) {
      setProfiles((current) => patchProfile(current, item.id, { status: `体检失败: ${error}` }));
    }
  };

  const checkPorts = async () => {
    if (!profile) return;
    const portList = [...deployConfig.selected.map((id) => deployConfig.ports[id]), deployConfig.webPort];
    try {
      const result = await callBackend(CheckPorts, profile, portList);
      setProgress((current) => ({
        ...current,
        status: 'idle',
        message: `内部端口检查完成: ${Object.entries(result.statuses || {}).map(([port, status]) => `${port}:${status}`).join(', ')}`,
        logs: [...current.logs, `[ports] ${JSON.stringify(result.statuses || {})}`],
      }));
    } catch (error) {
      setProgress((current) => ({ ...current, status: 'failed', message: `端口检查失败: ${error}`, logs: [...current.logs, `[error] ${error}`] }));
    }
  };

  const runDeploy = async () => {
    if (!profile) return;
    setDeploying(true);
    setProgress({ value: 4, status: 'running', message: '正在创建部署任务', logs: [] });
    await wait(650);
    setDeploying(false);
    setActiveTab('progress');
    try {
      const result = await callBackend(StartDeploy, profile, deployConfig);
      if (!result.ok) {
        setProgress((current) => ({ ...current, status: 'failed', message: `部署失败，退出码 ${result.code}` }));
      }
    } catch (error) {
      setProgress((current) => ({ ...current, status: 'failed', message: String(error), logs: [...current.logs, `[error] ${error}`] }));
    }
  };

  const runLatency = async () => {
    if (!profile) return;
    setSpeed((current) => ({ ...current, running: true, error: '', notice: '' }));
    try {
      const result = await callBackend(MeasureLatency, profile, deployConfig);
      setSpeed((current) => ({ ...current, running: false, items: result.items || [], error: '', notice: '' }));
    } catch (error) {
      setSpeed((current) => ({ ...current, running: false, error: String(error) }));
    }
  };

  const runRemoteSpeed = async () => {
    if (!profile) return;
    setSpeed((current) => ({ ...current, running: true, error: '', notice: '' }));
    try {
      const result = await callBackend(RunSpeedTest, profile);
      setSpeed((current) => ({ ...current, running: false, remote: result, error: '', notice: '' }));
    } catch (error) {
      setSpeed((current) => ({ ...current, running: false, error: String(error) }));
    }
  };

  const runSpeedCompare = async () => {
    if (!profile) return;
    setSpeed((current) => ({ ...current, running: true, error: '', notice: '', remote: null, node: null }));
    const [directResult, nodeResult] = await Promise.allSettled([
      callBackend(RunSpeedTest, profile),
      callBackend(RunNodeSpeedTest, profile, deployConfig),
    ]);
    const nodeValue = nodeResult.status === 'fulfilled' ? nodeResult.value : null;
    const nodeSkipped = Boolean(nodeValue?.skipped);
    setSpeed((current) => ({
      ...current,
      running: false,
      remote: directResult.status === 'fulfilled' ? directResult.value : null,
      node: nodeSkipped ? null : nodeValue,
      notice: nodeSkipped ? `节点代理测速已跳过：${nodeValue.reason}` : '',
      error: [
        directResult.status === 'rejected' ? `VPS 直连: ${directResult.reason}` : '',
        nodeResult.status === 'rejected' ? `节点代理: ${nodeResult.reason}` : '',
        nodeValue && nodeValue.ok === false && !nodeSkipped ? `节点代理: ${nodeValue.error || '所有协议测速失败'}` : '',
      ].filter(Boolean).join('；'),
    }));
  };

  const runIPQuality = async () => {
    if (!profile) return;
    setSpeed((current) => ({ ...current, qualityRunning: true, error: '', notice: '' }));
    try {
      const result = await callBackend(RunIPQuality, profile);
      setSpeed((current) => ({ ...current, qualityRunning: false, quality: result, error: '', notice: '' }));
    } catch (error) {
      setSpeed((current) => ({ ...current, qualityRunning: false, error: String(error) }));
    }
  };

  const scanFootprint = async () => {
    if (!profile) return;
    setMaintenance((current) => ({ ...current, running: true, error: '' }));
    try {
      const result = await callBackend(ScanFootprint, profile);
      setMaintenance({ running: false, footprint: result, logs: [], error: '' });
    } catch (error) {
      setMaintenance((current) => ({ ...current, running: false, error: String(error) }));
    }
  };

  const cleanupFootprint = async (removeRuntime = false) => {
    if (!profile) return;
    setMaintenance((current) => ({ ...current, running: true, error: '' }));
    try {
      const result = await callBackend(UninstallStarter, profile, removeRuntime);
      setMaintenance({ running: false, footprint: result, logs: result.logs || [], error: '' });
    } catch (error) {
      setMaintenance((current) => ({ ...current, running: false, error: String(error) }));
    }
  };

  const cleanupSelectedFootprint = async (protocolIDs, removeRuntime = false) => {
    if (!profile || !protocolIDs?.length) return;
    setMaintenance((current) => ({ ...current, running: true, error: '' }));
    try {
      const result = await callBackend(CleanupSelectedFootprint, profile, protocolIDs, removeRuntime);
      setMaintenance({ running: false, footprint: result, logs: result.logs || [], error: '' });
    } catch (error) {
      setMaintenance((current) => ({ ...current, running: false, error: String(error) }));
    }
  };

  return (
    <main className="app-shell">
      <aside className="sidebar">
        <div className="brand">
          <span className="brand-mark"><img src={appIcon} alt="" /></span>
          <div>
            <strong>Proxy Installer</strong>
            <small>VPS Node Starter</small>
          </div>
        </div>
        <nav className="side-nav">
          {tabs.map((item) => {
            const Icon = item.icon;
            return (
              <button key={item.id} className={activeTab === item.id ? 'active' : ''} onClick={() => setActiveTab(item.id)}>
                <Icon size={18} />
                <span>{item.label}</span>
                <small>{item.desc}</small>
              </button>
            );
          })}
        </nav>
        <div className="side-status">
          <span className={`dot ${window.go?.main?.App ? 'online' : ''}`} />
          <div>
            <strong>{window.go?.main?.App ? '桌面后端已连接' : '浏览器预览'}</strong>
            <small>{profiles.length} 台 VPS / {deployConfig.selected.length} 个协议</small>
          </div>
        </div>
      </aside>

      <section className="workspace">
        <header className="topbar">
          <div>
            <span className="eyebrow">CONTROL SURFACE</span>
            <h1>{activeMeta.label}</h1>
          </div>
          <div className="top-actions">
            <StatusPill status={progress.status} text={progress.message} />
            <button className="icon-button" onClick={() => setActiveTab('configs')} title="添加 VPS"><Plus size={18} /></button>
            <button className="primary" onClick={() => setActiveTab('deploy')}><Rocket size={16} />部署</button>
          </div>
        </header>

        <section className="page" key={activeTab}>
          {activeTab === 'dashboard' && (
            <Dashboard
              profiles={profiles}
              profile={reportProfile}
              report={report}
              progress={progress}
              speed={speed}
              setActiveTab={setActiveTab}
            />
          )}
          {activeTab === 'configs' && (
            <Configs
              profiles={profiles}
              draft={draft}
              setDraft={setDraft}
              addProfile={addProfile}
              setProfiles={setProfiles}
              setDeployConfig={setDeployConfig}
              testProfile={testProfile}
              inspectProfile={inspectProfile}
            />
          )}
          {activeTab === 'deploy' && (
            <Deploy
              profiles={profiles}
              config={deployConfig}
              setConfig={setDeployConfig}
              subscriptions={subscriptions}
              runDeploy={runDeploy}
              checkPorts={checkPorts}
              setActiveTab={setActiveTab}
            />
          )}
          {activeTab === 'speed' && (
            <SpeedCenter
              profile={profile}
              config={deployConfig}
              speed={speed}
              runLatency={runLatency}
              runRemoteSpeed={runRemoteSpeed}
              runSpeedCompare={runSpeedCompare}
              runIPQuality={runIPQuality}
              setActiveTab={setActiveTab}
            />
          )}
          {activeTab === 'maintenance' && (
            <Maintenance
              profile={profile}
              maintenance={maintenance}
              scanFootprint={scanFootprint}
              cleanupFootprint={cleanupFootprint}
              cleanupSelectedFootprint={cleanupSelectedFootprint}
              setActiveTab={setActiveTab}
            />
          )}
          {activeTab === 'progress' && <Progress progress={progress} showLogs={showLogs} setShowLogs={setShowLogs} />}
          {activeTab === 'result' && (
            <Result
              activeClient={activeClient}
              setActiveClient={setActiveClient}
              subscriptions={subscriptions}
              speed={speed}
            />
          )}
        </section>
      </section>

      {deploying && <Overlay />}
    </main>
  );
}

function Dashboard({ profiles, profile, report, progress, speed, setActiveTab }) {
  const bestLatency = speed.items.find((item) => item.status === 'ok')?.latencyMs;
  const loss = speedLossPercent(speed.remote, speed.node);
  return (
    <div className="dashboard-grid">
      <StatCard icon={Server} label="VPS" value={profiles.length} hint={profile?.host || '未添加'} tone="blue" />
      <StatCard icon={ShieldCheck} label="系统状态" value={report?.os?.packageManager || 'unknown'} hint={report?.os?.prettyName || '等待体检'} tone="green" />
      <StatCard icon={Radar} label="链路延迟" value={bestLatency ? `${bestLatency} ms` : '未测速'} hint={speed.node ? `节点 ${formatMbps(speed.node.downloadMbps)} Mbps` : '节点测速待运行'} tone="amber" />
      <StatCard icon={Percent} label="速度损耗" value={loss === null ? '--' : `${loss}%`} hint={speed.remote ? `VPS ${formatMbps(speed.remote.downloadMbps)} Mbps` : '等待对比测速'} tone="rose" />
      <StatCard icon={ShieldAlert} label="IP 纯净度" value={speed.quality?.summary?.headline || '--'} hint={speed.quality?.summary?.primaryIP || '未检测'} tone="green" />
      <StatCard icon={Activity} label="部署状态" value={progress.status === 'success' ? '成功' : progress.status === 'failed' ? '失败' : '待命'} hint={progress.message} tone="blue" />

      <section className="panel span-8">
        <PanelTitle icon={LayoutDashboard} title="VPS 状态矩阵" action={<button onClick={() => setActiveTab('configs')}><Search size={15} />体检</button>} />
        <div className="health-grid">
          <Metric icon={Cpu} label="CPU" value={report?.resources?.cpuCores ? `${report.resources.cpuCores} 核` : '未体检'} />
          <Metric icon={MemoryStick} label="内存" value={report?.resources?.memory || '未体检'} />
          <Metric icon={HardDrive} label="硬盘" value={report?.resources?.disk || '未体检'} />
          <Metric icon={Wifi} label="网络" value={reportSummary(report)} />
        </div>
        <div className="resource-row">
          <span>IPv6</span><strong>{report?.network?.publicIpv6 || '-'}</strong>
          <span>Stack</span><strong>{networkStack(report)}</strong>
          <span>虚拟化</span><strong>{report?.runtime?.virtualization || 'unknown'}</strong>
          <span>防火墙</span><strong>{report?.runtime?.firewall || 'unknown'}</strong>
          <span>公网 IPv4</span><strong>{report?.network?.publicIpv4 || '-'}</strong>
        </div>
      </section>

      <section className="panel span-4">
        <PanelTitle icon={Zap} title="快捷操作" />
        <div className="quick-stack">
          <button className="primary" onClick={() => setActiveTab('deploy')}><Rocket size={16} />新建部署</button>
          <button onClick={() => setActiveTab('speed')}><Gauge size={16} />打开测速中心</button>
          <button onClick={() => setActiveTab('result')}><Clipboard size={16} />查看订阅</button>
        </div>
      </section>

      <section className="panel span-12">
        <PanelTitle icon={Server} title="配置概览" />
        <DataTable
          columns={['名称', '地址', '状态', '系统', 'NAT']}
          rows={profiles.map((item) => [
            item.name || item.host,
            `${item.user}@${item.host}:${item.port}`,
            item.status,
            item.report?.os?.prettyName || '-',
            item.report ? (item.report.network?.natLikely ? 'NAT' : '公网') : '-',
          ])}
          empty="暂无 VPS"
        />
      </section>
    </div>
  );
}

function Configs({ profiles, draft, setDraft, addProfile, setProfiles, setDeployConfig, testProfile, inspectProfile }) {
  const [selectedId, setSelectedId] = useState('');
  useEffect(() => {
    if (!profiles.some((item) => item.id === selectedId)) {
      setSelectedId(profiles[0]?.id || '');
    }
  }, [profiles, selectedId]);
  const selected = profiles.find((item) => item.id === selectedId) || profiles.find((item) => item.report) || profiles[0];
  const report = selected?.report;

  const deleteProfile = (id) => {
    setProfiles((current) => current.filter((item) => item.id !== id));
    setDeployConfig((current) => current.profileId === id ? { ...current, profileId: '' } : current);
  };

  return (
    <div className="config-layout">
      <section className="panel form-panel">
        <PanelTitle icon={Plus} title="添加 SSH" />
        <Field label="名称"><input value={draft.name} onChange={(e) => setDraft({ ...draft, name: e.target.value })} /></Field>
        <Field label="主机 / IP"><input value={draft.host} onChange={(e) => setDraft({ ...draft, host: e.target.value })} /></Field>
        <div className="grid2">
          <Field label="用户"><input value={draft.user} onChange={(e) => setDraft({ ...draft, user: e.target.value })} /></Field>
          <Field label="端口"><input type="number" value={draft.port} onChange={(e) => setDraft({ ...draft, port: Number(e.target.value) })} /></Field>
        </div>
        <Field label="密码"><input type="password" value={draft.password} onChange={(e) => setDraft({ ...draft, password: e.target.value })} /></Field>
        <button className="primary wide-button" onClick={addProfile}><Plus size={16} />保存配置</button>
      </section>

      <section className="panel table-panel">
        <PanelTitle icon={Server} title="VPS 列表" />
        <div className="server-list">
          {profiles.length === 0 && <div className="empty-state">暂无 VPS</div>}
          {profiles.map((item) => (
            <button className={`server-row ${selected?.id === item.id ? 'selected' : ''}`} key={item.id} onClick={() => setSelectedId(item.id)}>
              <span className="server-dot" />
              <div className="server-main">
                <strong>{item.name || item.host}</strong>
                <span>{item.user}@{item.host}:{item.port}</span>
              </div>
              <em>{item.status}</em>
              <div className="row-actions">
                <button onClick={(event) => { event.stopPropagation(); testProfile(item); }} title="连接"><Wifi size={15} /></button>
                <button onClick={(event) => { event.stopPropagation(); inspectProfile(item); }} title="体检"><Search size={15} /></button>
                <button onClick={(event) => { event.stopPropagation(); deleteProfile(item.id); }} title="删除"><Trash2 size={15} /></button>
              </div>
            </button>
          ))}
        </div>
      </section>

      <section className="panel status-panel">
        <PanelTitle icon={Activity} title="体检详情" />
        <div className="detail-list">
          <Detail label="系统" value={report?.os?.prettyName || '未体检'} />
          <Detail label="内核" value={report?.os?.kernel || '未体检'} />
          <Detail label="CPU" value={report?.resources?.cpuModel || '未体检'} />
          <Detail label="内存" value={report?.resources?.memory || '未体检'} />
          <Detail label="硬盘" value={report?.resources?.disk || '未体检'} />
          <Detail label="公网/NAT" value={report ? `${report.network?.publicIpv4 || selected?.host || '-'} / ${report.network?.natLikely ? 'NAT' : '公网'}` : '未体检'} />
          <Detail label="虚拟化" value={report?.runtime?.virtualization || '未体检'} />
          <Detail label="工具" value={report ? toolSummary(report.tools) : '未体检'} />
        </div>
      </section>
    </div>
  );
}

function Deploy({ profiles, config, setConfig, subscriptions, runDeploy, checkPorts, setActiveTab }) {
  const selectedProfile = profiles.find((item) => item.id === config.profileId) || profiles[0];
  const natLikely = selectedProfile?.report?.network?.natLikely;
  const toggle = (id) => {
    setConfig({
      ...config,
      selected: config.selected.includes(id) ? config.selected.filter((item) => item !== id) : [...config.selected, id],
    });
  };
  if (profiles.length === 0) {
    return (
      <section className="panel empty-deploy">
        <PanelTitle icon={Rocket} title="节点部署" />
        <div className="empty-state large">先添加一台 VPS</div>
        <button className="primary" onClick={() => setActiveTab('configs')}><Plus size={16} />添加 SSH</button>
      </section>
    );
  }
  return (
    <div className="deploy-layout">
      <section className="panel span-main">
        <PanelTitle icon={Rocket} title="部署配置" action={<button onClick={checkPorts}><Search size={15} />检查内部端口</button>} />
        {natLikely && (
          <div className="nat-banner">
            <ShieldAlert size={17} />
            <div>
              <strong>检测到 NAT 倾向</strong>
              <span>内部端口写 VPS 实际监听端口，公网端口写服务商面板转发出来的端口；订阅和测速会自动使用公网端口。</span>
            </div>
          </div>
        )}
        <div className="grid2">
          <Field label="选择 VPS">
            <select value={config.profileId} onChange={(e) => setConfig({ ...config, profileId: e.target.value })}>
              {profiles.map((item) => <option key={item.id} value={item.id}>{item.name || item.host}</option>)}
            </select>
          </Field>
          <Field label="节点名称">
            <input value={config.nodeName} onChange={(e) => setConfig({ ...config, nodeName: e.target.value })} />
          </Field>
        </div>

        <div className="protocol-grid">
          {protocols.map((item) => (
            <button key={item.id} className={`protocol-card ${item.tone} ${config.selected.includes(item.id) ? 'selected' : ''}`} onClick={() => toggle(item.id)}>
              <span className="protocol-icon"><ProtocolGlyph id={item.icon} /></span>
              <span className="protocol-check">{config.selected.includes(item.id) ? <CheckCircle2 size={16} /> : <XCircle size={16} />}</span>
              <strong>{item.label}</strong>
              <small>{item.desc}</small>
            </button>
          ))}
        </div>

        <div className="port-map-table">
          <div className="port-map-head">
            <span>协议</span>
            <span>内部端口</span>
            <span>公网端口</span>
          </div>
          {protocols.filter((item) => config.selected.includes(item.id)).map((item) => (
            <div className="port-map-row" key={item.id}>
              <strong>{item.label}</strong>
              <input type="number" value={config.ports[item.id] || item.port} onChange={(e) => setConfig({ ...config, ports: { ...config.ports, [item.id]: Number(e.target.value) } })} />
              <input type="number" value={externalPort(config, item.id, item.port)} onChange={(e) => setConfig({ ...config, publicPorts: { ...(config.publicPorts || {}), [item.id]: Number(e.target.value) } })} />
            </div>
          ))}
        </div>

        <button className="ghost" onClick={() => setConfig({ ...config, advanced: !config.advanced })}><Settings2 size={16} />高级设置</button>
        {config.advanced && (
          <div className="advanced">
            <div className="grid2">
              <Field label="订阅内部端口"><input type="number" value={config.webPort} onChange={(e) => setConfig({ ...config, webPort: Number(e.target.value) })} /></Field>
              <Field label="订阅公网端口"><input type="number" value={externalWebPort(config)} onChange={(e) => setConfig({ ...config, publicWebPort: Number(e.target.value) })} /></Field>
              <Field label="订阅 Token"><input value={config.token} onChange={(e) => setConfig({ ...config, token: e.target.value })} /></Field>
              <Field label="路径规则"><input value={config.rule} onChange={(e) => setConfig({ ...config, rule: e.target.value })} /></Field>
              <Field label="Reality / TLS SNI"><input value={config.sni} onChange={(e) => setConfig({ ...config, sni: e.target.value })} /></Field>
            </div>
          </div>
        )}

        <div className="actions end">
          <button onClick={() => setActiveTab('speed')}><Gauge size={16} />测速中心</button>
          <button className="primary" onClick={runDeploy} disabled={config.selected.length === 0}><Play size={16} />开始部署</button>
        </div>
      </section>

      <section className="panel side-panel">
        <PanelTitle icon={Link2} title="订阅预览" />
        <div className="subs">
          {Object.values(subscriptions).map((item) => (
            <div className="sub" key={item.label}>
              <strong>{item.label}</strong>
              <span>{item.url}</span>
            </div>
          ))}
        </div>
      </section>
    </div>
  );
}

function SpeedCenter({ profile, config, speed, runLatency, runRemoteSpeed, runSpeedCompare, runIPQuality, setActiveTab }) {
  if (!profile) {
    return (
      <section className="panel empty-deploy">
        <PanelTitle icon={Gauge} title="测速中心" />
        <div className="empty-state large">先添加一台 VPS</div>
        <button className="primary" onClick={() => setActiveTab('configs')}><Plus size={16} />添加 SSH</button>
      </section>
    );
  }
  const loss = speedLossPercent(speed.remote, speed.node);
  const protocolSpeeds = speed.node?.protocols || [];
  const nodeSuccess = Boolean(speed.node?.ok && Number(speed.node?.downloadMbps || 0) > 0);
  return (
    <div className="speed-layout">
      <section className="panel">
        <PanelTitle icon={Gauge} title="测速任务" />
        <div className="target-strip">
          <Detail label="目标 VPS" value={profile.name || profile.host} />
          <Detail label="端口映射" value={formatPortMappings(config)} />
          <Detail label="订阅映射" value={`${config.webPort} -> ${externalWebPort(config)}`} />
        </div>
        <div className="actions">
          <button className="primary" onClick={runLatency} disabled={speed.running}><Radar size={16} />链路延迟</button>
          <button className="primary" onClick={runSpeedCompare} disabled={speed.running}><Percent size={16} />速度损耗对比</button>
          <button onClick={runRemoteSpeed} disabled={speed.running}><Zap size={16} />仅测 VPS 出口</button>
          <button onClick={runIPQuality} disabled={speed.qualityRunning}><ShieldAlert size={16} />IP 纯净度</button>
          {speed.running && <span className="inline-loading"><Loader2 size={16} />正在测试</span>}
          {speed.qualityRunning && <span className="inline-loading"><Loader2 size={16} />正在检测 IP</span>}
        </div>
        {speed.error && <p className="error-text">{speed.error}</p>}
        {speed.error && /hysteria|hy2|tuic|udp|no recent network activity|timeout|eof|forcibly closed|connection reset|subscription|订阅请求失败/i.test(speed.error) && (
          <p className="warning-text">HY2/TUIC 需要公网 UDP 转发；如果是 NAT 机器，请确认面板里 UDP 公网端口已经映射到 VPS 内部端口，并且订阅端口能正常返回配置。</p>
        )}
        {speed.notice && <p className="warning-text">{speed.notice}</p>}
        <p className="hint-text">节点代理测速会临时启动独立的本机 sing-box mixed 端口，不修改系统代理；优先使用随包内置的 sing-box.exe，缺失时会尝试自动下载。</p>
      </section>

      <section className="panel">
        <PanelTitle icon={Percent} title="速度损耗对比" />
        <div className="compare-grid">
          <div className="speed-score compact-score">
            <span>VPS 直连出口</span>
            <strong>{speed.remote ? `${formatMbps(speed.remote.downloadMbps)} Mbps` : '--'}</strong>
            <small>{speed.remote ? `${formatNumber(speed.remote.downloadMBps)} MB/s` : '等待测试'}</small>
          </div>
          <div className="speed-score compact-score">
            <span>通过节点代理{speed.node?.bestProtocol ? ` / ${speed.node.bestProtocol}` : ''}</span>
            <strong>{nodeSuccess ? `${formatMbps(speed.node.downloadMbps)} Mbps` : '--'}</strong>
            <small>{nodeSuccess ? `${formatNumber(speed.node.downloadMBps)} MB/s` : '等待成功的协议测速'}</small>
          </div>
          <div className={`loss-card ${loss !== null && loss <= 25 ? 'good' : loss !== null && loss <= 55 ? 'warn' : ''}`}>
            <Percent size={22} />
            <span>速度损耗</span>
            <strong>{loss === null ? '--' : `${loss}%`}</strong>
          </div>
        </div>
      </section>

      <section className="panel">
        <PanelTitle icon={Activity} title="按协议测速" />
        <div className="protocol-speed-grid">
          {protocolSpeeds.map((item) => {
            const proto = protocols.find((protocol) => protocol.id === item.protocolID) || { icon: 'nodes', tone: 'slate', label: item.protocol };
            const itemLoss = item.ok && speed.remote ? speedLossPercent(speed.remote, item) : null;
            return (
              <article key={item.protocolID || item.protocol} className={`protocol-speed-card ${proto.tone} ${item.ok ? 'ok' : 'failed'}`}>
                <div className="protocol-speed-head">
                  <ProtocolGlyph id={proto.icon} />
                  <div>
                    <strong>{item.protocol || proto.label}</strong>
                    <small>公网端口 {item.port || '-'}</small>
                  </div>
                  <em>{item.ok ? 'ok' : 'failed'}</em>
                </div>
                <div className="protocol-speed-body">
                  <strong>{item.ok ? `${formatMbps(item.downloadMbps)} Mbps` : '--'}</strong>
                  <span>{item.ok ? `${formatNumber(item.downloadMBps)} MB/s` : (item.error || '未完成测速')}</span>
                </div>
                <div className="mini-progress">
                  <span style={{ width: `${itemLoss === null ? 0 : Math.max(0, Math.min(100, 100 - itemLoss))}%` }} />
                </div>
                <small className="protocol-speed-foot">{itemLoss === null ? '损耗待计算' : `速度损耗 ${itemLoss}%`}</small>
              </article>
            );
          })}
          {protocolSpeeds.length === 0 && <div className="empty-state">点击速度损耗对比后显示每个协议的真实节点测速</div>}
        </div>
      </section>

      <section className="panel">
        <PanelTitle icon={Radar} title="延迟结果" />
        <DataTable
          columns={['类型', '目标', '端口', '延迟', '状态']}
          rows={speed.items.map((item) => [
            item.protocol,
            item.target,
            item.port,
            item.latencyMs ? `${item.latencyMs} ms` : '-',
            item.status,
          ])}
          empty="暂无延迟数据"
        />
      </section>

      <section className="panel">
        <PanelTitle icon={ShieldAlert} title="IP 纯净度" />
        <QualityPanel quality={speed.quality} />
      </section>
    </div>
  );
}

function QualityPanel({ quality }) {
  const sites = quality?.sites || [];
  const [activeSiteId, setActiveSiteId] = useState(sites[0]?.id || 'ippure');
  useEffect(() => {
    if (sites.length && !sites.some((site) => site.id === activeSiteId)) {
      setActiveSiteId(sites[0].id);
    }
  }, [sites, activeSiteId]);
  if (!quality) {
    return (
      <div className="quality-empty">
        <ShieldAlert size={28} />
        <strong>未检测</strong>
        <span>运行 IP 纯净度后分别展示 IPPure、ping0、IPLark 三个站点的返回结果。</span>
      </div>
    );
  }
  const summary = quality.summary || {};
  const sections = quality.sections || [];
  const activeSite = sites.find((site) => site.id === activeSiteId) || sites[0];
  const purityPercent = Number(summary.purityPercent);
  const hasPurity = Number.isFinite(purityPercent) && purityPercent >= 0;
  return (
    <div className="quality-panel">
      <div className="quality-summary">
        <Detail label="报告模块" value={`${summary.moduleTotal || sections.length || 0} 个`} />
        <Detail label="探测项" value={`${summary.checkOK || 0}/${summary.checkTotal || 0} 正常`} />
        <Detail label="检测 IP" value={summary.primaryIP || '-'} />
      </div>
      <div className="quality-purity-card">
        <div>
          <strong>{hasPurity ? `${purityPercent}%` : '--'}</strong>
          <span>纯净度</span>
        </div>
        <div className="purity-meter">
          <span style={{ width: `${hasPurity ? Math.max(0, Math.min(100, purityPercent)) : 0}%` }} />
        </div>
        <small>{summary.puritySource || '等待站点返回可量化分数'}</small>
      </div>
      <div className="quality-site-tabs">
        {sites.map((site) => (
          <button key={site.id || site.name} className={site.id === activeSite?.id ? 'active' : ''} onClick={() => setActiveSiteId(site.id)}>
            <LocalLogo domain={domainFromText(site.url)} label={site.name} fallback={<SiteGlyph id={site.id} />} />
            <span>{site.name}</span>
            <em>{site.status === 'success' ? '成功' : '失败'}</em>
          </button>
        ))}
      </div>
      {activeSite && <QualitySourceCard site={activeSite} expanded />}
      <div className="quality-report-grid">
        {sections.map((section) => <QualityReportSection key={section.id || section.title} section={section} />)}
      </div>
    </div>
  );
}

function QualitySourceCard({ site, expanded = false }) {
  const meta = qualitySiteMeta[site.id] || { badge: 'IP', tone: 'slate' };
  const rows = site.rows || [];
  const ok = site.status === 'success';
  return (
    <article className={`quality-source-card ${expanded ? 'expanded' : ''} ${ok ? 'ok' : 'failed'} ${meta.tone}`}>
      <div className="quality-source-head">
        <span className={`site-badge ${meta.tone}`}>
          <LocalLogo domain={domainFromText(site.url)} label={site.name} fallback={<SiteGlyph id={site.id} />} />
        </span>
        <div>
          <strong>{site.name}</strong>
          <a href={site.url} target="_blank" rel="noreferrer">{site.url?.replace(/^https?:\/\//, '').replace(/\/$/, '')}</a>
        </div>
        <em>{ok ? '成功' : '失败'}</em>
      </div>
      <div className="quality-source-metric">
        <span>{site.metric || '--'}</span>
        <small>{site.summary || site.error || '暂无摘要'}</small>
      </div>
      <div className="quality-source-rows">
        {rows.slice(0, expanded ? 22 : 10).map((row) => (
          <div key={`${site.id}-${row.label}`}>
            <span>{row.label}</span>
            <strong>{row.value}</strong>
          </div>
        ))}
        {rows.length === 0 && <div><span>返回</span><strong>{site.error || '暂无字段'}</strong></div>}
      </div>
    </article>
  );
}

function QualityReportSection({ section }) {
  const meta = qualitySectionMeta[section.id] || { icon: Activity, tone: 'blue' };
  const Icon = meta.icon;
  const rows = section.rows || [];
  return (
    <article className={`quality-report-section ${meta.tone}`}>
      <div className="report-section-head">
        <span><Icon size={17} /></span>
        <strong>{section.title}</strong>
      </div>
      <div className="report-row-list">
        {rows.map((row) => <QualityReportRow key={`${section.id}-${row.label}`} row={row} />)}
        {rows.length === 0 && <div className="report-row empty"><span>暂无结果</span></div>}
      </div>
    </article>
  );
}

function QualityReportRow({ row }) {
  const tone = serviceTone(row.label);
  const status = normalizeReportStatus(row.status);
  const iconDomain = iconDomainForRow(row);
  return (
    <div className={`report-row ${status}`}>
      <span className={`service-badge ${tone}`}>
        <LocalLogo domain={iconDomain} label={row.label} fallback={<ServiceGlyph label={row.label} />} />
      </span>
      <div>
        <strong>{row.label}</strong>
        <small>{row.source || '-'}</small>
      </div>
      <code>{row.value || '-'}</code>
      <em>{statusText(status)}</em>
    </div>
  );
}

function ProtocolGlyph({ id }) {
  const common = { viewBox: '0 0 24 24', fill: 'none', stroke: 'currentColor', strokeWidth: '2', strokeLinecap: 'round', strokeLinejoin: 'round', 'aria-hidden': true };
  if (id === 'shield') {
    return <svg {...common}><path d="M12 3l7 3v5c0 4.6-2.8 8.1-7 10-4.2-1.9-7-5.4-7-10V6l7-3z" /><path d="M9 12l2 2 4-5" /></svg>;
  }
  if (id === 'wave') {
    return <svg {...common}><path d="M3 12c2.2-4 4.4-4 6.6 0s4.4 4 6.6 0S20.6 8 22 10" /><path d="M3 17c2.2-4 4.4-4 6.6 0s4.4 4 6.6 0" /></svg>;
  }
  if (id === 'bolt') {
    return <svg {...common}><path d="M13 2L4 14h7l-1 8 10-13h-7l0-7z" /></svg>;
  }
  if (id === 'lock') {
    return <svg {...common}><rect x="5" y="10" width="14" height="10" rx="2" /><path d="M8 10V7a4 4 0 0 1 8 0v3" /><path d="M12 14v2" /></svg>;
  }
  if (id === 'layers') {
    return <svg {...common}><path d="M12 3l9 5-9 5-9-5 9-5z" /><path d="M3 13l9 5 9-5" /><path d="M3 17l9 5 9-5" /></svg>;
  }
  return <svg {...common}><circle cx="6" cy="12" r="3" /><circle cx="18" cy="7" r="3" /><circle cx="18" cy="17" r="3" /><path d="M8.6 10.7l6.8-2.4" /><path d="M8.6 13.3l6.8 2.4" /></svg>;
}

function SiteGlyph({ id }) {
  const common = { viewBox: '0 0 24 24', fill: 'none', stroke: 'currentColor', strokeWidth: '2', strokeLinecap: 'round', strokeLinejoin: 'round', 'aria-hidden': true };
  if (id === 'ippure') {
    return <svg {...common}><path d="M12 3l7 3v5c0 4.6-2.8 8.1-7 10-4.2-1.9-7-5.4-7-10V6l7-3z" /><path d="M9 12h6" /><path d="M12 9v6" /></svg>;
  }
  if (id === 'ping0') {
    return <svg {...common}><circle cx="12" cy="12" r="3" /><path d="M12 2v4" /><path d="M12 18v4" /><path d="M2 12h4" /><path d="M18 12h4" /><path d="M5 5l3 3" /><path d="M16 16l3 3" /></svg>;
  }
  return <svg {...common}><path d="M7 18a5 5 0 1 1 1.8-9.6A6 6 0 0 1 20 11.5 3.5 3.5 0 0 1 19.5 18H7z" /><path d="M9 13h6" /></svg>;
}

function LocalLogo({ domain, label, fallback }) {
  const icon = localIconFor(domain, label);
  if (!icon) {
    return fallback;
  }
  return <img className="local-logo" src={icon} alt={`${label || domain || 'site'} icon`} loading="lazy" />;
}

function ServiceGlyph({ label = '' }) {
  const text = String(label);
  const common = { viewBox: '0 0 24 24', fill: 'none', stroke: 'currentColor', strokeWidth: '2', strokeLinecap: 'round', strokeLinejoin: 'round', 'aria-hidden': true };
  if (/YouTube|Disney|Netflix|TikTok|Reddit/i.test(text)) {
    return <svg {...common}><rect x="3" y="5" width="18" height="14" rx="3" /><path d="M10 9l5 3-5 3V9z" fill="currentColor" stroke="none" /></svg>;
  }
  if (/OpenAI|ChatGPT|AI/i.test(text)) {
    return <svg {...common}><circle cx="12" cy="12" r="2.5" /><path d="M12 3c3 0 4 2 4 4 2 0 4 1.5 4 4s-2 4-4 4c0 2-1 4-4 4s-4-2-4-4c-2 0-4-1.5-4-4s2-4 4-4c0-2 1-4 4-4z" /></svg>;
  }
  if (/Gmail|Outlook|Yahoo|iCloud|QQ|Mail|SMTP|邮局/i.test(text)) {
    return <svg {...common}><rect x="3" y="5" width="18" height="14" rx="3" /><path d="M4 7l8 6 8-6" /></svg>;
  }
  if (/DNSBL|风险|Fraud|Proxy|Hosting|Mobile|住宅|广播|ISP|WARP|Gateway/i.test(text)) {
    return <svg {...common}><path d="M12 3l7 3v5c0 4.6-2.8 8.1-7 10-4.2-1.9-7-5.4-7-10V6l7-3z" /><path d="M12 8v5" /><path d="M12 17h.01" /></svg>;
  }
  return <svg {...common}><circle cx="12" cy="12" r="9" /><path d="M3 12h18" /><path d="M12 3c2.5 2.7 3.7 5.7 3.7 9S14.5 18.3 12 21c-2.5-2.7-3.7-5.7-3.7-9S9.5 5.7 12 3z" /></svg>;
}

const serviceIconDomains = {
  Netflix: 'www.netflix.com',
  YouTube: 'www.youtube.com',
  'Disney+': 'www.disneyplus.com',
  TikTok: 'www.tiktok.com',
  Reddit: 'www.reddit.com',
  'OpenAI API': 'platform.openai.com',
  ChatGPT: 'chat.openai.com',
  Gmail: 'mail.google.com',
  Outlook: 'outlook.com',
  Yahoo: 'mail.yahoo.com',
  iCloud: 'www.icloud.com',
  'QQ Mail': 'mail.qq.com',
  'Mail.ru': 'mail.ru',
  AOL: 'mail.aol.com',
  GMX: 'www.gmx.net',
  'Mail.com': 'www.mail.com',
};

function iconDomainForRow(row = {}) {
  if (serviceIconDomains[row.label]) {
    return serviceIconDomains[row.label];
  }
  return domainFromText(row.source || row.value || '');
}

function domainFromText(value = '') {
  const text = String(value).trim();
  if (!text) return '';
  try {
    if (/^https?:\/\//i.test(text)) {
      return new URL(text).hostname;
    }
  } catch {
    return '';
  }
  const match = text.match(/(?:^|[\s/])((?:[a-z0-9-]+\.)+[a-z]{2,})(?=$|[\s/:])/i);
  return match ? match[1] : '';
}

function normalizeDomain(domain = '') {
  return String(domain)
    .trim()
    .replace(/^https?:\/\//i, '')
    .replace(/\/.*$/, '')
    .replace(/:\d+$/, '')
    .toLowerCase();
}

function localIconFor(domain = '', label = '') {
  const exactLabel = String(label || '').trim();
  if (labelIconMap[exactLabel]) {
    return labelIconMap[exactLabel];
  }
  const labelKey = Object.keys(labelIconMap).find((key) => exactLabel && exactLabel.toLowerCase().includes(key.toLowerCase()));
  if (labelKey) {
    return labelIconMap[labelKey];
  }
  const cleanDomain = normalizeDomain(domain);
  if (!cleanDomain) {
    return '';
  }
  if (domainIconMap[cleanDomain]) {
    return domainIconMap[cleanDomain];
  }
  const domainKey = Object.keys(domainIconMap)
    .sort((a, b) => b.length - a.length)
    .find((key) => cleanDomain === key || cleanDomain.endsWith(`.${key}`));
  return domainKey ? domainIconMap[domainKey] : '';
}

function serviceTone(label = '') {
  const text = String(label);
  if (/Netflix|YouTube|Disney|TikTok|Reddit/i.test(text)) return 'rose';
  if (/OpenAI|ChatGPT|AI/i.test(text)) return 'green';
  if (/Gmail|Outlook|Yahoo|iCloud|QQ|Mail|SMTP|邮局/i.test(text)) return 'blue';
  if (/DNSBL|风险|Fraud|Scamalytics/i.test(text)) return 'amber';
  if (/Proxy|Hosting|Mobile|住宅|广播|ISP/i.test(text)) return 'violet';
  return 'slate';
}

function protocolStatusText(status) {
  if (status === 'complete') return '完整';
  if (status === 'partial') return '残留';
  return '未发现';
}

function logKind(line = '') {
  const match = String(line).match(/^\[([^\]]+)\]/);
  const kind = match?.[1] || 'log';
  if (/error|failed/i.test(kind)) return 'error';
  if (/done|result|success/i.test(kind)) return 'done';
  if (/warn/i.test(kind)) return 'warn';
  if (/progress|ports/i.test(kind)) return 'progress';
  return 'log';
}

function Maintenance({ profile, maintenance, scanFootprint, cleanupFootprint, cleanupSelectedFootprint, setActiveTab }) {
  const [cleanupSelection, setCleanupSelection] = useState([]);
  useEffect(() => {
    const ids = (maintenance.footprint?.protocols || [])
      .filter((item) => item.status === 'complete' || item.status === 'partial')
      .map((item) => item.id);
    setCleanupSelection(ids);
  }, [maintenance.footprint?.checkedAt]);
  if (!profile) {
    return (
      <section className="panel empty-deploy">
        <PanelTitle icon={Trash2} title="维护清理" />
        <div className="empty-state large">先添加一台 VPS</div>
        <button className="primary" onClick={() => setActiveTab('configs')}><Plus size={16} />添加 SSH</button>
      </section>
    );
  }
  const footprint = maintenance.footprint || {};
  const summary = footprint.summary || {};
  const services = footprint.services || {};
  const flags = footprint.flags || {};
  const protocolRows = footprint.protocols || [];
  const toggleCleanupProtocol = (id) => {
    setCleanupSelection((current) => (
      current.includes(id) ? current.filter((item) => item !== id) : [...current, id]
    ));
  };
  return (
    <div className="maintenance-layout">
      <section className="panel">
        <PanelTitle icon={Trash2} title="印记扫描与卸载" />
        <div className="target-strip">
          <Detail label="目标 VPS" value={profile.name || profile.host} />
          <Detail label="发现项目" value={summary.present ?? '--'} />
          <Detail label="临时日志" value={summary.tmpLogCount ?? flags.tmpLogCount ?? '--'} />
        </div>
        <div className="actions">
          <button className="primary" onClick={scanFootprint} disabled={maintenance.running}><Search size={16} />扫描印记</button>
          <button onClick={() => cleanupFootprint(false)} disabled={maintenance.running}><Trash2 size={16} />清理本工具</button>
          <button className="danger" onClick={() => cleanupFootprint(true)} disabled={maintenance.running}><Trash2 size={16} />深度清理 sing-box</button>
          {maintenance.running && <span className="inline-loading"><Loader2 size={16} />正在处理</span>}
        </div>
        {maintenance.error && <p className="error-text">{maintenance.error}</p>}
        <p className="hint-text">普通清理只删除本工具目录、订阅文件、nginx 片段、临时日志，并在确认 sing-box 配置属于本工具时停止该服务。深度清理会额外尝试删除 sing-box 二进制和 systemd service。</p>
        <div className="protocol-footprints">
          <div className="subhead">
            <strong>协议印记</strong>
            <span>{cleanupSelection.length} 个已选</span>
          </div>
          <div className="protocol-footprint-list">
            {protocolRows.map((item) => {
              const proto = protocols.find((protocol) => protocol.id === item.id) || { icon: 'nodes', tone: 'slate', label: item.label };
              const selectable = item.status === 'complete' || item.status === 'partial';
              return (
                <button
                  key={item.id}
                  className={`protocol-footprint ${proto.tone} ${cleanupSelection.includes(item.id) ? 'selected' : ''}`}
                  onClick={() => selectable && toggleCleanupProtocol(item.id)}
                  disabled={!selectable || maintenance.running}
                >
                  <ProtocolGlyph id={proto.icon} />
                  <span>
                    <strong>{item.label}</strong>
                    <small>端口 {item.port || '-'} / 配置 {item.configPresent ? '存在' : '缺失'} / 订阅 {item.subscriptionPresent ? '存在' : '缺失'}</small>
                  </span>
                  <em>{protocolStatusText(item.status)}</em>
                </button>
              );
            })}
            {protocolRows.length === 0 && <div className="empty-state">点击扫描印记后显示协议残留</div>}
          </div>
          <div className="actions">
            <button onClick={() => cleanupSelectedFootprint(cleanupSelection, false)} disabled={maintenance.running || cleanupSelection.length === 0}><Trash2 size={16} />清理选中协议</button>
            <button className="danger" onClick={() => cleanupSelectedFootprint(cleanupSelection, true)} disabled={maintenance.running || cleanupSelection.length === 0}><Trash2 size={16} />清理选中并移除运行时</button>
          </div>
        </div>
      </section>

      <section className="panel">
        <PanelTitle icon={Activity} title="服务状态" />
        <DataTable
          columns={['服务', '运行状态', '开机启动']}
          rows={Object.entries(services).map(([name, value]) => [name, value.active || '-', value.enabled || '-'])}
          empty="暂无服务状态"
        />
      </section>

      <section className="panel">
        <PanelTitle icon={Server} title="文件与目录" />
        <DataTable
          columns={['项目', '路径', '状态', '大小']}
          rows={(footprint.items || []).map((item) => [
            item.label,
            item.path,
            item.status === 'present' ? '存在' : '未发现',
            item.size,
          ])}
          empty="点击扫描印记后显示"
        />
      </section>

      <section className="panel">
        <PanelTitle icon={TerminalSquare} title="日志与清理记录" />
        <div className="cleanup-log-grid">
          <div>
            <strong>远程临时日志</strong>
            <DataTable
              columns={['路径', '字节']}
              rows={(footprint.logFiles || []).map((item) => [item.path, item.bytes])}
              empty="暂无临时日志"
            />
          </div>
          <div>
            <strong>本次操作</strong>
            <div className="mini-log">
              {(maintenance.logs || []).map((line, index) => <span key={`${line}-${index}`}>{line}</span>)}
              {(!maintenance.logs || maintenance.logs.length === 0) && <span>暂无操作记录</span>}
            </div>
          </div>
        </div>
      </section>
    </div>
  );
}

function Progress({ progress, showLogs, setShowLogs }) {
  const logText = progress.logs.join('\n');
  return (
    <div className="progress-page">
      <section className={`panel progress-card ${progress.status}`}>
        <PanelTitle icon={TerminalSquare} title="部署进度" />
        <div className="progress-track"><span style={{ width: `${progress.value}%` }} /></div>
        <div className="progress-number">{progress.value}%</div>
        <h2>{progress.message}</h2>
        {progress.status === 'failed' && <p className="error-text">请复制日志提交给 AI 或者是认识的技术人员。</p>}
        <div className="actions">
          <button onClick={() => setShowLogs(!showLogs)}><TerminalSquare size={16} />{showLogs ? '隐藏日志' : '查看日志'}</button>
          <button onClick={() => navigator.clipboard.writeText(logText)} disabled={!logText}><Clipboard size={16} />复制日志</button>
        </div>
      </section>
      {showLogs && (
        <section className="panel log-chat">
          <PanelTitle icon={TerminalSquare} title="日志事件" />
          <div className="log-chat-scroll">
            {progress.logs.map((line, index) => (
              <div className={`log-bubble ${logKind(line)}`} key={`${line}-${index}`}>
                <span>{logKind(line)}</span>
                <code>{line.replace(/^\[[^\]]+\]\s*/, '')}</code>
              </div>
            ))}
            {progress.logs.length === 0 && <div className="log-empty">暂无日志</div>}
          </div>
        </section>
      )}
    </div>
  );
}

function Result({ activeClient, setActiveClient, subscriptions, speed }) {
  const item = subscriptions[activeClient];
  const [qrData, setQrData] = useState('');
  useEffect(() => {
    if (!item?.url?.startsWith('http')) {
      setQrData('');
      return;
    }
    QRCode.toDataURL(item.url, {
      margin: 1,
      width: 180,
      color: { dark: '#111827', light: '#ffffff' },
    }).then(setQrData).catch(() => setQrData(''));
  }, [item?.url]);

  return (
    <div className="result-layout">
      <section className="panel result-main">
        <PanelTitle icon={Link2} title="客户端订阅" />
        <div className="client-tabs">
          {clients.map(([key, label]) => <button key={key} className={activeClient === key ? 'active' : ''} onClick={() => setActiveClient(key)}>{label}</button>)}
        </div>
        <div className="result-card">
          <div>
            <span>订阅 URL</span>
            <strong>{item.url}</strong>
          </div>
          <button className="primary" onClick={() => navigator.clipboard.writeText(item.url)} disabled={!item.url.startsWith('http')}><Clipboard size={16} />复制订阅</button>
        </div>
        <div className="detail-list horizontal">
          <Detail label="最近延迟" value={speed.items.find((entry) => entry.status === 'ok')?.latencyMs ? `${speed.items.find((entry) => entry.status === 'ok')?.latencyMs} ms` : '-'} />
          <Detail label="出口下载" value={speed.remote ? `${formatMbps(speed.remote.downloadMbps)} Mbps` : '-'} />
        </div>
      </section>
      <section className="panel qr-panel">
        <div className="qr-box">
          {qrData ? <img src={qrData} alt={`${item.label} QR`} /> : <div className="qr-placeholder" />}
        </div>
        <strong>{item.label}</strong>
      </section>
    </div>
  );
}

function Overlay() {
  return (
    <div className="overlay">
      <div className="loader"><i /><i /></div>
      <strong>正在准备部署</strong>
      <span>生成配置、订阅路径和远程任务</span>
    </div>
  );
}

function StatCard({ icon: Icon, label, value, hint, tone }) {
  return (
    <section className={`panel stat-card ${tone}`}>
      <Icon size={20} />
      <span>{label}</span>
      <strong>{value}</strong>
      <small>{hint}</small>
    </section>
  );
}

function PanelTitle({ icon: Icon, title, action }) {
  return (
    <div className="panel-title">
      <div><Icon size={18} /><h3>{title}</h3></div>
      {action}
    </div>
  );
}

function Metric({ icon: Icon, label, value }) {
  return <div className="metric"><Icon size={17} /><span>{label}</span><strong>{value}</strong></div>;
}

function Field({ label, children }) {
  return <label className="field"><span>{label}</span>{children}</label>;
}

function Detail({ label, value }) {
  return <div className="detail"><span>{label}</span><strong>{value}</strong></div>;
}

function StatusPill({ status, text }) {
  const Icon = status === 'success' ? CheckCircle2 : status === 'failed' ? XCircle : Activity;
  return <span className={`status-pill ${status}`}><Icon size={15} />{text}</span>;
}

function DataTable({ columns, rows, empty }) {
  return (
    <div className="data-table" style={{ '--cols': columns.length }}>
      <div className="thead">{columns.map((item) => <span key={item}>{item}</span>)}</div>
      {rows.length === 0 && <div className="empty-row">{empty}</div>}
      {rows.map((row, index) => (
        <div className="tr" key={`${row[0]}-${index}`}>
          {row.map((cell, cellIndex) => <span key={`${cell}-${cellIndex}`}>{cell}</span>)}
        </div>
      ))}
    </div>
  );
}

function patchProfile(current, id, patch) {
  return current.map((item) => item.id === id ? { ...item, ...patch } : item);
}

function clientPath(key) {
  if (key === 'mihomo') return 'mihomo.yaml';
  if (key === 'singbox') return 'sing-box.json';
  return key;
}

function formatHost(host) {
  return host.includes(':') && !host.startsWith('[') ? `[${host}]` : host;
}

function preferredEndpointHost(profile) {
  return profile?.report?.network?.publicIpv4 || profile?.report?.network?.publicIpv6 || profile?.host || '';
}

function networkStack(report) {
  if (!report) return '未体检';
  const has4 = Boolean(report.network?.publicIpv4 || report.network?.ipv4Route);
  const has6 = Boolean(report.network?.publicIpv6 || report.network?.ipv6Route || report.network?.ipv6Global);
  if (has4 && has6) return 'IPv4 / IPv6';
  if (has6) return 'IPv6';
  if (has4) return 'IPv4';
  return '未知';
}

function reportSummary(report) {
  if (!report) return '未体检';
  const nat = report.network?.natLikely ? 'NAT' : '公网';
  return `${nat} / ${networkStack(report)} / ${report.runtime?.virtualization || 'unknown'}`;
}

function toolSummary(tools = {}) {
  const required = ['curl', 'nginx', 'openssl', 'ss', 'systemctl'];
  const ok = required.filter((key) => tools[key]).length;
  return `${ok}/${required.length} 已就绪`;
}

function protocolName(id) {
  return protocols.find((item) => item.id === id)?.label || id;
}

function externalPort(config, id, fallback) {
  return config.publicPorts?.[id] || config.ports?.[id] || fallback;
}

function externalWebPort(config) {
  return config.publicWebPort || config.webPort || 8080;
}

function formatPortMappings(config) {
  if (!config.selected?.length) return '-';
  return config.selected.map((id) => {
    const def = protocols.find((item) => item.id === id)?.port || 0;
    const inside = config.ports?.[id] || def;
    const outside = externalPort(config, id, def);
    return `${protocolName(id)} ${inside}->${outside}`;
  }).join(' / ');
}

function formatNumber(value) {
  const num = Number(value || 0);
  return num ? num.toFixed(2) : '0.00';
}

function formatMbps(value) {
  const num = Number(value || 0);
  return num ? num.toFixed(1) : '0.0';
}

function speedLossPercent(remote, node) {
  const direct = Number(remote?.downloadMbps || 0);
  const proxied = Number(node?.downloadMbps || 0);
  if (!direct || !proxied) return null;
  return Math.max(0, Math.min(100, Math.round((1 - proxied / direct) * 100)));
}

function normalizeReportStatus(status = '') {
  const value = String(status).toLowerCase();
  if (['ok', 'open', 'clean', 'success'].includes(value)) return 'ok';
  if (['fail', 'failed', 'listed', 'blocked'].includes(value)) return 'bad';
  return 'skip';
}

function statusText(status) {
  if (status === 'ok') return '正常';
  if (status === 'bad') return '异常';
  return '跳过';
}

function callBackend(fn, ...args) {
  if (!window.go?.main?.App) {
    return Promise.reject(new Error('桌面后端未连接，请打开 exe 使用'));
  }
  return fn(...args);
}

function wait(ms) {
  return new Promise((resolve) => setTimeout(resolve, ms));
}

export default App;
