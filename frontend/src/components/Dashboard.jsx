import {
  Activity,
  Clipboard,
  Cpu,
  Gauge,
  HardDrive,
  LayoutDashboard,
  MemoryStick,
  Percent,
  Radar,
  Rocket,
  Search,
  Server,
  ShieldAlert,
  ShieldCheck,
  Wifi,
  Zap,
} from 'lucide-react';
import { PanelTitle, StatCard, Metric, DataTable } from './ui/UIComponents';
import { formatMbps, networkStack, reportSummary, speedLossPercent } from '../utils/format';

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

export default Dashboard;
