import {
  CheckCircle2,
  Gauge,
  Link2,
  Play,
  Plus,
  Rocket,
  Search,
  Settings2,
  ShieldAlert,
  XCircle,
} from 'lucide-react';
import { protocols, defaultPorts } from '../utils/constants';
import { externalPort, externalWebPort } from '../utils/format';
import { PanelTitle, Field } from './ui/UIComponents';
import { ProtocolGlyph } from './ui/Icons';
import RuleEnginePanel from './deploy/RuleEnginePanel';

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
            <RuleEnginePanel />
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

export default Deploy;
