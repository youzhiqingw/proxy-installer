import { useEffect, useMemo, useState } from 'react';
import appIcon from './assets/images/app-icon.png';
import {
  DollarSign,
  LayoutDashboard,
  Link2,
  Plus,
  Rocket,
  Server,
  Trash2,
} from 'lucide-react';
import {
  AcceptHostKey,
  CheckPorts,
  CleanupSelectedFootprint,
  GetCostV2Instances,
  GetProfileCredentials,
  InspectVPS,
  LoadAppState,
  MeasureLatency,
  RunIPQuality,
  RunNodeSpeedTest,
  RunSpeedTest,
  SaveAppState,
  ScanFootprint,
  StartDeploy,
  TestConnection,
  UninstallStarter,
} from '../wailsjs/go/main/App';
import { EventsOn } from '../wailsjs/runtime/runtime';
import { tabs, protocols, clients, defaultPorts } from './utils/constants';
import {
  clientPath,
  externalWebPort,
  formatHost,
  preferredEndpointHost,
} from './utils/format';
import { callBackend, wait, patchProfile } from './hooks/useBackend';
import { StatusPill, Overlay, HostKeyDialog } from './components/ui/UIComponents';
import Dashboard from './components/Dashboard';
import Configs from './components/Configs';
import Deploy from './components/Deploy';
import SpeedCenter from './components/SpeedCenter';
import Maintenance from './components/Maintenance';
import CostCenter from './components/CostCenter';
import Progress from './components/Progress';
import Result from './components/Result';
import './App.css';

function App() {
  const [activeTab, setActiveTab] = useState('dashboard');
  const [activeClient, setActiveClient] = useState('shadowrocket');
  const [deploying, setDeploying] = useState(false);
  const [showLogs, setShowLogs] = useState(false);
  const [stateLoaded, setStateLoaded] = useState(false);
  const [costInstances, setCostInstances] = useState([]);
  const [progress, setProgress] = useState({ value: 0, status: 'idle', message: '等待操作', logs: [] });
  const [profiles, setProfiles] = useState([]);
  const [draft, setDraft] = useState({ name: '', host: '', user: 'root', port: 22, password: '', authMode: '', privateKeyContent: '', keyPassphrase: '' });
  const [editingId, setEditingId] = useState(null);
  const [speed, setSpeed] = useState({ running: false, qualityRunning: false, items: [], remote: null, node: null, quality: null, error: '', notice: '' });
  const [maintenance, setMaintenance] = useState({ running: false, footprint: null, logs: [], error: '' });
  const [hostKeyDialog, setHostKeyDialog] = useState(null);
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
          // Restore cached IP quality result from active profile
          const activeId = saved.deployConfig?.profileId;
          const qp = saved.profiles.find(p => p.id === activeId && p.quality_result)
            || saved.profiles.find(p => p.quality_result);
          if (qp?.quality_result) {
            setSpeed((current) => ({ ...current, quality: qp.quality_result }));
          }
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
    if (!stateLoaded) return;
    callBackend(GetCostV2Instances).then((r) => {
      if (r?.instances) setCostInstances(r.instances);
    }).catch(() => {});
  }, [stateLoaded]);

  // Clear or restore quality result when active profile changes
  useEffect(() => {
    const qp = profiles.find(p => p.id === deployConfig.profileId && p.quality_result);
    setSpeed((current) => ({ ...current, quality: qp?.quality_result || null }));
  }, [deployConfig.profileId, profiles]);

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

  const emptyDraft = { name: '', host: '', user: 'root', port: 22, password: '', authMode: '', privateKeyContent: '', keyPassphrase: '' };

  const addProfile = () => {
    if (!draft.host.trim()) return;
    if (editingId) {
      setProfiles((current) => current.map((item) =>
        item.id === editingId ? { ...item, ...draft, host: draft.host.trim() } : item
      ));
      setEditingId(null);
    } else {
      const next = { ...draft, host: draft.host.trim(), id: crypto.randomUUID(), status: '未体检' };
      setProfiles((current) => [...current, next]);
      setDeployConfig((current) => ({ ...current, profileId: next.id }));
    }
    setDraft(emptyDraft);
  };

  const startEditProfile = async (item) => {
    let creds = { password: '', keyPassphrase: '', authMode: item.authMode || '' };
    try {
      const result = await callBackend(GetProfileCredentials, item.id);
      if (result) {
        creds = {
          ...creds,
          password: result.password || '',
          keyPassphrase: result.keyPassphrase || '',
        };
      }
    } catch (err) {
      console.warn('凭据加载失败:', err);
    }
    setDraft({
      name: item.name || '', host: item.host || '', user: item.user || 'root', port: item.port || 22,
      password: creds.password, authMode: creds.authMode,
      privateKeyContent: item.privateKeyContent || '', keyPassphrase: creds.keyPassphrase,
    });
    setEditingId(item.id);
  };

  const cancelEdit = () => {
    setEditingId(null);
    setDraft(emptyDraft);
  };

  // 检测 HostKey 确认错误并弹出确认对话框，确认后自动重试
  const callSSH = async (fn, ...args) => {
    try {
      return await callBackend(fn, ...args);
    } catch (err) {
      const msg = String(err);
      if (msg.includes('HOSTKEY_CONFIRM:')) {
        const match = msg.match(/HOSTKEY_CONFIRM:(\S+):(\d+)\s.*?指纹\s+(\S+)\s+\((\S+)\)/);
        return new Promise((resolve, reject) => {
          setHostKeyDialog({
            host: match?.[1] || '',
            port: Number(match?.[2] || 22),
            fingerprint: match?.[3] || '',
            keyType: match?.[4] || '',
            onRetry: async () => {
              try {
                resolve(await callBackend(fn, ...args));
              } catch (e) {
                reject(e);
              }
            },
          });
        });
      }
      throw err;
    }
  };

  const testProfile = async (item) => {
    try {
      setProfiles((current) => patchProfile(current, item.id, { status: '连接中' }));
      const result = await callSSH(TestConnection, item);
      setProfiles((current) => patchProfile(current, item.id, { status: result.message || '已连接' }));
    } catch (error) {
      setProfiles((current) => patchProfile(current, item.id, { status: `失败: ${error}` }));
    }
  };

  const inspectProfile = async (item) => {
    try {
      setProfiles((current) => patchProfile(current, item.id, { status: '体检中' }));
      const result = await callSSH(InspectVPS, item);
      setProfiles((current) => patchProfile(current, item.id, { status: '体检完成', report: result.report }));
    } catch (error) {
      setProfiles((current) => patchProfile(current, item.id, { status: `体检失败: ${error}` }));
    }
  };

  const checkPorts = async () => {
    if (!profile) return;
    const portList = [...deployConfig.selected.map((id) => deployConfig.ports[id]), deployConfig.webPort];
    try {
      const result = await callSSH(CheckPorts, profile, portList);
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
      const result = await callSSH(StartDeploy, profile, deployConfig);
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
      const result = await callSSH(MeasureLatency, profile, deployConfig);
      setSpeed((current) => ({ ...current, running: false, items: result.items || [], error: '', notice: '' }));
    } catch (error) {
      setSpeed((current) => ({ ...current, running: false, error: String(error) }));
    }
  };

  const runRemoteSpeed = async () => {
    if (!profile) return;
    setSpeed((current) => ({ ...current, running: true, error: '', notice: '' }));
    try {
      const result = await callSSH(RunSpeedTest, profile);
      setSpeed((current) => ({ ...current, running: false, remote: result, error: '', notice: '' }));
    } catch (error) {
      setSpeed((current) => ({ ...current, running: false, error: String(error) }));
    }
  };

  const runSpeedCompare = async () => {
    if (!profile) return;
    setSpeed((current) => ({ ...current, running: true, error: '', notice: '', remote: null, node: null }));
    const [directResult, nodeResult] = await Promise.allSettled([
      callSSH(RunSpeedTest, profile),
      callSSH(RunNodeSpeedTest, profile, deployConfig),
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
      const result = await callSSH(RunIPQuality, profile);
      setSpeed((current) => ({ ...current, qualityRunning: false, quality: result, error: '', notice: '' }));
      setProfiles((current) => patchProfile(current, profile.id, { quality_result: result }));
    } catch (error) {
      setSpeed((current) => ({ ...current, qualityRunning: false, error: String(error) }));
    }
  };

  const scanFootprint = async () => {
    if (!profile) return;
    setMaintenance((current) => ({ ...current, running: true, error: '' }));
    try {
      const result = await callSSH(ScanFootprint, profile);
      setMaintenance({ running: false, footprint: result, logs: [], error: '' });
    } catch (error) {
      setMaintenance((current) => ({ ...current, running: false, error: String(error) }));
    }
  };

  const cleanupFootprint = async (removeRuntime = false) => {
    if (!profile) return;
    setMaintenance((current) => ({ ...current, running: true, error: '' }));
    try {
      const result = await callSSH(UninstallStarter, profile, removeRuntime);
      setMaintenance({ running: false, footprint: result, logs: result.logs || [], error: '' });
    } catch (error) {
      setMaintenance((current) => ({ ...current, running: false, error: String(error) }));
    }
  };

  const cleanupSelectedFootprint = async (protocolIDs, removeRuntime = false) => {
    if (!profile || !protocolIDs?.length) return;
    setMaintenance((current) => ({ ...current, running: true, error: '' }));
    try {
      const result = await callSSH(CleanupSelectedFootprint, profile, protocolIDs, removeRuntime);
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
          {activeTab === 'cost' && (
            <CostCenter
              profiles={profiles}
              instances={costInstances}
              setInstances={setCostInstances}
            />
          )}
          {activeTab === 'configs' && (
            <Configs
              profiles={profiles}
              draft={draft}
              setDraft={setDraft}
              editingId={editingId}
              addProfile={addProfile}
              startEditProfile={startEditProfile}
              cancelEdit={cancelEdit}
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
      {hostKeyDialog && (
        <HostKeyDialog
          info={hostKeyDialog}
          onAccept={async () => {
            try {
              await callBackend(AcceptHostKey, hostKeyDialog.host, hostKeyDialog.port);
              setHostKeyDialog(null);
              if (hostKeyDialog.onRetry) await hostKeyDialog.onRetry();
            } catch (err) {
              setHostKeyDialog(null);
            }
          }}
          onCancel={() => setHostKeyDialog(null)}
        />
      )}
    </main>
  );
}

export default App;
