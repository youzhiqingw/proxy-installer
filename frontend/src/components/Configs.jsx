import { useEffect, useState } from 'react';
import { Activity, Plus, Search, Server, Trash2, Wifi } from 'lucide-react';
import { PanelTitle, Field, Detail } from './ui/UIComponents';
import { toolSummary } from '../utils/format';

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

export default Configs;
