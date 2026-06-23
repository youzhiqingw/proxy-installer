import { useState } from 'react';
import { Copy, FileCode, Globe, Network, TriangleAlert } from 'lucide-react';
import { PanelTitle } from '../ui/UIComponents';
import { BuildRoutingRules } from '../../../wailsjs/go/main/App';

const actions = [
  { id: 'proxy', label: '代理 (proxy)' },
  { id: 'direct', label: '直连 (direct)' },
  { id: 'block', label: '拦截 (block)' },
];

function RuleEnginePanel() {
  const [action, setAction] = useState('proxy');
  const [domains, setDomains] = useState('');
  const [cidrs, setCidrs] = useState('');
  const [loading, setLoading] = useState(false);
  const [result, setResult] = useState('');
  const [error, setError] = useState('');
  const [copied, setCopied] = useState(false);

  const generate = async () => {
    setError('');
    setResult('');
    setCopied(false);
    if (!domains.trim() && !cidrs.trim()) {
      setError('请输入至少一个域名或 CIDR');
      return;
    }
    setLoading(true);
    try {
      const domainList = domains.split('\n').map((s) => s.trim()).filter(Boolean);
      const cidrList = cidrs.split('\n').map((s) => s.trim()).filter(Boolean);
      const json = await BuildRoutingRules(domainList, cidrList, action);
      setResult(json);
    } catch (err) {
      setError(String(err));
    } finally {
      setLoading(false);
    }
  };

  const copyResult = async () => {
    if (!result) return;
    try {
      await navigator.clipboard.writeText(result);
      setCopied(true);
      setTimeout(() => setCopied(false), 2000);
    } catch { /* ignore */ }
  };

  return (
    <div className="rule-engine-panel" style={{ marginTop: '0.75rem', borderTop: '1px solid var(--border)', paddingTop: '0.75rem' }}>
      <PanelTitle icon={FileCode} title="分流规则引擎" />

      <div className="grid2" style={{ marginBottom: '0.5rem' }}>
        {actions.map((a) => (
          <label key={a.id} className="radio-card" style={{
            display: 'flex', alignItems: 'center', gap: '0.4rem', padding: '0.35rem 0.6rem',
            background: action === a.id ? 'var(--accent-bg)' : 'var(--surface)',
            border: `1px solid ${action === a.id ? 'var(--accent)' : 'var(--border)'}`,
            borderRadius: 6, cursor: 'pointer', fontSize: '0.85rem',
          }}>
            <input type="radio" name="rule-action" value={a.id} checked={action === a.id}
              onChange={() => setAction(a.id)} style={{ accentColor: 'var(--accent)' }} />
            {a.label}
          </label>
        ))}
      </div>

      <div className="grid2">
        <div className="field">
          <span><Globe size={14} /> 域名 (每行一个, 支持 *.example.com)</span>
          <textarea value={domains} onChange={(e) => setDomains(e.target.value)}
            placeholder={"openai.com\n*.openai.net\nnetflix.com"}
            style={{ minHeight: 80, fontFamily: 'var(--mono)', fontSize: '0.8rem' }}
          />
        </div>
        <div className="field">
          <span><Network size={14} /> CIDR (每行一个)</span>
          <textarea value={cidrs} onChange={(e) => setCidrs(e.target.value)}
            placeholder={"10.0.0.0/8\n192.168.0.0/16"}
            style={{ minHeight: 80, fontFamily: 'var(--mono)', fontSize: '0.8rem' }}
          />
        </div>
      </div>

      <div className="actions" style={{ marginTop: '0.5rem', gap: '0.4rem' }}>
        <button className="primary" onClick={generate} disabled={loading} style={{ fontSize: '0.85rem' }}>
          {loading ? '生成中…' : '生成规则'}
        </button>
        {result && (
          <button onClick={copyResult} style={{ fontSize: '0.85rem' }}>
            <Copy size={14} />{copied ? '已复制' : '复制 JSON'}
          </button>
        )}
      </div>

      {error && (
        <div className="warn" style={{ fontSize: '0.8rem', marginTop: '0.4rem' }}>
          <TriangleAlert size={13} />{error}
        </div>
      )}

      {result && (
        <pre style={{
          background: 'var(--surface)', border: '1px solid var(--border)', borderRadius: 6,
          padding: '0.6rem', marginTop: '0.5rem', fontSize: '0.75rem', lineHeight: 1.5,
          overflow: 'auto', maxHeight: 240, fontFamily: 'var(--mono)',
        }}>{result}</pre>
      )}
    </div>
  );
}

export default RuleEnginePanel;
