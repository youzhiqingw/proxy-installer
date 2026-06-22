import { useEffect, useState } from 'react';
import {
  Activity,
  Loader2,
  Plus,
  Search,
  Server,
  TerminalSquare,
  Trash2,
} from 'lucide-react';
import { protocols } from '../utils/constants';
import { protocolStatusText } from '../utils/format';
import { PanelTitle, Detail, DataTable } from './ui/UIComponents';
import { ProtocolGlyph } from './ui/Icons';

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

export default Maintenance;
