import {
  Activity,
  CheckCircle2,
  ShieldAlert,
  XCircle,
} from 'lucide-react';

export function PanelTitle({ icon: Icon, title, action }) {
  return (
    <div className="panel-title">
      <div><Icon size={18} /><h3>{title}</h3></div>
      {action}
    </div>
  );
}

export function StatCard({ icon: Icon, label, value, hint, tone }) {
  return (
    <section className={`panel stat-card ${tone}`}>
      <Icon size={20} />
      <span>{label}</span>
      <strong>{value}</strong>
      <small>{hint}</small>
    </section>
  );
}

export function Metric({ icon: Icon, label, value }) {
  return <div className="metric"><Icon size={17} /><span>{label}</span><strong>{value}</strong></div>;
}

export function Field({ label, required, children }) {
  return (
    <div className="field">
      <span>{label}{required && <span style={{ color: '#f43f5e', marginLeft: 3 }}>*</span>}</span>
      {children}
    </div>
  );
}

export function Detail({ label, value }) {
  return <div className="detail"><span>{label}</span><strong>{value}</strong></div>;
}

export function StatusPill({ status, text }) {
  const Icon = status === 'success' ? CheckCircle2 : status === 'failed' ? XCircle : Activity;
  return <span className={`status-pill ${status}`}><Icon size={15} />{text}</span>;
}

export function DataTable({ columns, rows, empty }) {
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

export function Overlay() {
  return (
    <div className="overlay">
      <div className="loader"><i /><i /></div>
      <strong>正在准备部署</strong>
      <span>生成配置、订阅路径和远程任务</span>
    </div>
  );
}

export function HostKeyDialog({ info, onAccept, onCancel }) {
  return (
    <div className="overlay" style={{ zIndex: 1000 }}>
      <div className="panel" style={{ maxWidth: 420, padding: '1.5rem', textAlign: 'center' }}>
        <ShieldAlert size={36} style={{ color: 'var(--amber)', marginBottom: '0.75rem' }} />
        <strong style={{ display: 'block', fontSize: '1.1rem', marginBottom: '0.5rem' }}>
          首次连接服务器
        </strong>
        <p style={{ margin: '0.5rem 0', fontSize: '0.9rem', opacity: 0.8 }}>
          即将信任以下服务器的 SSH 指纹，请确认是否为您的服务器：
        </p>
        <div style={{
          background: 'var(--surface)', borderRadius: 8, padding: '0.75rem',
          margin: '0.75rem 0', textAlign: 'left', fontSize: '0.85rem', fontFamily: 'monospace',
        }}>
          <div><strong>主机：</strong>{info.host}:{info.port}</div>
          <div><strong>类型：</strong>{info.keyType}</div>
          <div style={{ wordBreak: 'break-all', marginTop: 4 }}>
            <strong>指纹：</strong>{info.fingerprint}
          </div>
        </div>
        <p style={{ margin: '0.5rem 0', fontSize: '0.8rem', opacity: 0.6 }}>
          如果您不确定此指纹，请取消操作并核实服务器身份。
        </p>
        <div style={{ display: 'flex', gap: '0.75rem', justifyContent: 'center', marginTop: '1rem' }}>
          <button className="btn secondary" onClick={onCancel} style={{ padding: '0.5rem 1.5rem' }}>
            取消
          </button>
          <button className="btn primary" onClick={onAccept} style={{ padding: '0.5rem 1.5rem' }}>
            信任并继续
          </button>
        </div>
      </div>
    </div>
  );
}
