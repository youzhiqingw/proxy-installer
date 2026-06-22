import { useEffect, useState } from 'react';
import {
  Activity,
  Gauge,
  Loader2,
  Percent,
  Plus,
  Radar,
  ShieldAlert,
  Tv,
  Zap,
} from 'lucide-react';
import { protocols, qualitySiteMeta, qualitySectionMeta } from '../utils/constants';
import {
  domainFromText,
  externalPort,
  externalWebPort,
  formatMbps,
  formatNumber,
  formatPortMappings,
  iconDomainForRow,
  normalizeReportStatus,
  serviceTone,
  speedLossPercent,
  statusText,
} from '../utils/format';
import { PanelTitle, Detail, DataTable } from './ui/UIComponents';
import { ProtocolGlyph, SiteGlyph, ServiceGlyph, LocalLogo } from './ui/Icons';

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

export default SpeedCenter;
