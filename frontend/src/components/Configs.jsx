import { useEffect, useRef, useState } from 'react';
import { Activity, Loader2, Pencil, Plus, Search, Server, Trash2, Upload, Wifi, X } from 'lucide-react';
import { PanelTitle, Field, Detail } from './ui/UIComponents';
import { toolSummary } from '../utils/format';

function Configs({ profiles, draft, setDraft, editingId, addProfile, startEditProfile, cancelEdit, setProfiles, setDeployConfig, testProfile, inspectProfile }) {
  const [selectedId, setSelectedId] = useState('');
  useEffect(() => {
    if (!profiles.some((item) => item.id === selectedId)) {
      setSelectedId(profiles[0]?.id || '');
    }
  }, [profiles, selectedId]);
  const selected = profiles.find((item) => item.id === selectedId) || profiles.find((item) => item.report) || profiles[0];
  const report = selected?.report;
  const keyFileRef = useRef(null);

  const handleKeyFile = (event) => {
    const file = event.target.files?.[0];
    if (!file) return;
    const reader = new FileReader();
    reader.onload = () => setDraft((d) => ({ ...d, privateKeyContent: reader.result }));
    reader.readAsText(file);
    event.target.value = '';
  };

  const deleteProfile = (id) => {
    setProfiles((current) => current.filter((item) => item.id !== id));
    setDeployConfig((current) => current.profileId === id ? { ...current, profileId: '' } : current);
  };

  return (
    <div className="config-layout">
      <section className="panel form-panel">
        <PanelTitle icon={editingId ? Pencil : Plus} title={editingId ? '编辑 SSH' : '添加 SSH'} />
        <Field label="名称"><input value={draft.name} onChange={(e) => setDraft({ ...draft, name: e.target.value })} /></Field>
        <Field label="主机 / IP"><input value={draft.host} onChange={(e) => setDraft({ ...draft, host: e.target.value })} /></Field>
        <div className="grid2">
          <Field label="用户"><input value={draft.user} onChange={(e) => setDraft({ ...draft, user: e.target.value })} /></Field>
          <Field label="端口"><input type="number" value={draft.port} onChange={(e) => setDraft({ ...draft, port: Number(e.target.value) })} /></Field>
        </div>
        <Field label="认证方式">
          <div className="auth-mode-toggle">
            <button type="button" className={draft.authMode !== 'key' ? 'active' : ''}
              onClick={() => setDraft({ ...draft, authMode: '', password: '', privateKeyContent: '', keyPassphrase: '' })}>
              密码
            </button>
            <button type="button" className={draft.authMode === 'key' ? 'active' : ''}
              onClick={() => setDraft({ ...draft, authMode: 'key', password: '' })}>
              密钥
            </button>
          </div>
        </Field>

        {draft.authMode !== 'key' ? (
          <Field label="密码"><input type="password" value={draft.password} placeholder={draft._hasPassword ? '已保存（留空保持不变）' : ''} onChange={(e) => setDraft({ ...draft, password: e.target.value })} /></Field>
        ) : (
          <>
            <Field label="私钥内容">
              <div className="key-input-group">
                <textarea rows={4} placeholder="-----BEGIN OPENSSH PRIVATE KEY-----&#10;..."
                  value={draft.privateKeyContent || ''}
                  onChange={(e) => setDraft({ ...draft, privateKeyContent: e.target.value })} />
                <input ref={keyFileRef} type="file" accept=".pem,.key,.rsa,.ssh,.txt" hidden onChange={handleKeyFile} />
                <button type="button" className="secondary key-upload-btn" onClick={() => keyFileRef.current?.click()}>
                  <Upload size={15} />上传文件
                </button>
              </div>
            </Field>
            <Field label="密钥口令（可选）"><input type="password" value={draft.keyPassphrase || ''} placeholder={draft._hasKeyPassphrase ? '已保存（留空保持不变）' : ''} onChange={(e) => setDraft({ ...draft, keyPassphrase: e.target.value })} /></Field>
          </>
        )}
        <div className="form-actions">
          <button className="primary wide-button" onClick={addProfile}>
            {editingId ? <Pencil size={16} /> : <Plus size={16} />}
            {editingId ? '更新配置' : '保存配置'}
          </button>
          {editingId && (
            <button className="secondary wide-button" onClick={cancelEdit}>
              <X size={16} />取消编辑
            </button>
          )}
        </div>
      </section>

      <section className="panel table-panel">
        <PanelTitle icon={Server} title="VPS 列表" />
        <div className="server-list">
          {profiles.length === 0 && <div className="empty-state">暂无 VPS</div>}
          {profiles.map((item) => {
            const busy = item.status === '连接中' || item.status === '体检中';
            return (
            <button className={`server-row ${selected?.id === item.id ? 'selected' : ''} ${editingId === item.id ? 'editing' : ''}`} key={item.id} onClick={() => setSelectedId(item.id)}>
              <span className={`server-dot ${busy ? 'busy' : ''}`} />
              <div className="server-main">
                <strong>{item.name || item.host}</strong>
                <span>{item.user}@{item.host}:{item.port}</span>
              </div>
              <em className={busy ? 'status-busy' : ''}>
                {busy && <Loader2 size={12} />}
                {item.status}
              </em>
              <div className="row-actions">
                <button onClick={(event) => { event.stopPropagation(); startEditProfile(item); }} title="编辑"><Pencil size={15} /></button>
                <button onClick={(event) => { event.stopPropagation(); testProfile(item); }} title="连接" disabled={busy}><Wifi size={15} /></button>
                <button onClick={(event) => { event.stopPropagation(); inspectProfile(item); }} title="体检" disabled={busy}><Search size={15} /></button>
                <button onClick={(event) => { event.stopPropagation(); deleteProfile(item.id); }} title="删除"><Trash2 size={15} /></button>
              </div>
            </button>
            );
          })}
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
