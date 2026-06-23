import { useMemo, useState } from 'react';
import {
  ArrowUpDown,
  Building2,
  CalendarDays,
  Cpu,
  CreditCard,
  Edit,
  Globe,
  HardDrive,
  Monitor,
  Plus,
  Server,
  Trash2,
  Wifi,
  X,
} from 'lucide-react';
import { CURRENCIES, OS_PRESETS } from '../utils/constants';
import {
  autoFillFromReport,
  autoNextRenewal,
  cycleMonths,
  currencySymbol,
  displayBandwidth,
  displayCPU,
  displayDisk,
  displayMemory,
  displayTraffic,
  emptyDraft,
  getBwDisplay,
  getDiskDisplay,
  getMemDisplay,
  getTrafficDisplay,
  getInstanceStatus,
  isExpired,
  setBwMbps,
  setDiskGB,
  setMemGB,
  setTrafficGB,
  toMonthly,
} from '../utils/format';
import { callBackend } from '../hooks/useBackend';
import { Field } from './ui/UIComponents';
import {
  SaveCostVPSInstance,
  DeleteCostVPSInstance,
} from '../../wailsjs/go/main/App';

function CostStatCard({ icon, className, label, value, sub }) {
  return (
    <div className={`cost-stat-card ${className || ''}`}>
      {icon && <div className="cost-stat-icon">{icon}</div>}
      <div className="cost-stat-value">{value}</div>
      <div style={{ fontSize: 12, color: '#64748b', marginTop: 2 }}>{label}</div>
      {sub && <div className="cost-stat-sub">{sub}</div>}
    </div>
  );
}

function FormSection({ title, collapsible = false, defaultOpen = true, badge, children }) {
  const [open, setOpen] = useState(defaultOpen);
  return (
    <div>
      <div
        className="form-section-title"
        style={collapsible ? { cursor: 'pointer', userSelect: 'none' } : {}}
        onClick={collapsible ? () => setOpen(o => !o) : undefined}
      >
        {title}
        {badge && (
          <span style={{
            background: '#dbeafe', color: '#1d4ed8', borderRadius: '999px',
            fontSize: '11px', padding: '1px 8px', fontWeight: 600,
            textTransform: 'none', letterSpacing: 0,
          }}>{badge}</span>
        )}
        {collapsible && (
          <span style={{ marginLeft: 'auto', fontSize: 14, color: '#94a3b8' }}>
            {open ? '\u2303' : '\u2304'}
          </span>
        )}
      </div>
      {(!collapsible || open) && (
        <div className="form-section-body">{children}</div>
      )}
    </div>
  );
}

function SpecChip({ icon, children }) {
  return (
    <span className="spec-chip">
      {icon && <span className="spec-chip-icon">{icon}</span>}
      {children}
    </span>
  );
}

function ExpiringBanner({ instances }) {
  const [collapsed, setCollapsed] = useState(false);
  const expiring = instances.filter(inst => {
    if (inst.billingCycle === 'lifetime' || !inst.nextRenewal) return false;
    return Math.ceil((new Date(inst.nextRenewal) - new Date()) / 86400000) <= 30;
  });
  if (expiring.length === 0) return null;
  const overdueCount = expiring.filter(i => new Date(i.nextRenewal) < new Date()).length;
  const soonCount = expiring.length - overdueCount;
  const summaryText = [
    overdueCount > 0 && `${overdueCount} \u53f0\u5df2\u8fc7\u671f`,
    soonCount > 0 && `${soonCount} \u53f0\u5373\u5c06\u5230\u671f`,
  ].filter(Boolean).join('\uff0c');
  const sorted = [...expiring].sort((a, b) => new Date(a.nextRenewal) - new Date(b.nextRenewal));
  return (
    <div className="expiry-banner">
      <div className="expiry-banner-header" onClick={() => setCollapsed(c => !c)} role="button" aria-expanded={!collapsed}>
        <span>{'\u26a0\ufe0f'} \u7eed\u8d39\u63d0\u9192</span>
        <span className="expiry-banner-count">{expiring.length}</span>
        <span className="expiry-banner-summary">{summaryText}</span>
        <span className="expiry-banner-toggle">{collapsed ? '\u5c55\u5f00 \u2304' : '\u6536\u8d77 \u2303'}</span>
      </div>
      {!collapsed && sorted.map(inst => {
        const diffDays = Math.ceil((new Date(inst.nextRenewal) - new Date()) / 86400000);
        const isOverdue = diffDays < 0;
        const statusClass = isOverdue ? 'overdue' : 'due-soon';
        const statusIcon = isOverdue ? '\ud83d\udd34' : '\ud83d\udfe1';
        const statusText = isOverdue
          ? `\u5df2\u8fc7\u671f ${Math.abs(diffDays)} \u5929`
          : diffDays === 0 ? '\u4eca\u65e5\u5230\u671f' : `${inst.nextRenewal}\uff08${diffDays} \u5929\u540e\uff09`;
        const currSym = CURRENCIES.find(c => c.code === inst.currency)?.symbol || '';
        const cycleLabel = { monthly: '\u6708', quarterly: '\u5b63', semiannual: '\u534a\u5e74', annual: '\u5e74' }[inst.billingCycle] || '';
        return (
          <div key={inst.id} className="expiry-item">
            <div className={`expiry-item-dot ${statusClass}`} />
            <span>{statusIcon}</span>
            <span className="expiry-item-name">{inst.vpsName}</span>
            <span className="expiry-item-meta">
              {inst.providerName ? `${inst.providerName} \u00b7 ` : ''}{statusText}
            </span>
            <span className="expiry-item-price">
              {currSym}{inst.price?.toFixed(2)}/{cycleLabel}
            </span>
          </div>
        );
      })}
    </div>
  );
}

function InstanceCard({ inst, profiles, onEdit, onDelete }) {
  const status = getInstanceStatus(inst);
  const stripeColor = { overdue: '#f43f5e', 'due-week': '#f59e0b', 'due-month': '#f59e0b', lifetime: '#8b5cf6', ok: '#22c55e' }[status];
  const badge = {
    overdue: { text: '\u5df2\u8fc7\u671f', cls: 'badge-overdue' },
    'due-week': { text: '7\u5929\u5185\u5230\u671f', cls: 'badge-due-week' },
    'due-month': { text: '\u5373\u5c06\u5230\u671f', cls: 'badge-due-month' },
    lifetime: { text: '\u7ec8\u8eab', cls: 'badge-lifetime' },
    ok: null,
  }[status];
  const currSymbol = CURRENCIES.find(c => c.code === inst.currency)?.symbol || '';
  const cycleLabel = { monthly: '\u6708', quarterly: '\u5b63', semiannual: '\u534a\u5e74', annual: '\u5e74', lifetime: '\u4e70\u65ad' }[inst.billingCycle] || '';
  return (
    <div className="instance-card">
      <div className="instance-card-stripe" style={{ background: stripeColor }} />
      <div className="instance-card-header">
        <span className="instance-card-name">{inst.vpsName}</span>
        {badge && <span className={`instance-status-badge ${badge.cls}`}>{badge.text}</span>}
        <div className="instance-actions">
          <button className="btn-icon-sm" title="\u7f16\u8f91" onClick={() => onEdit(inst)}><Edit size={13} /></button>
          <button className="btn-icon-sm danger" title="\u5220\u9664" onClick={() => onDelete(inst.id)}><Trash2 size={13} /></button>
        </div>
      </div>
      {(inst.providerName || inst.planName) && (
        <div className="instance-card-subtitle">
          {[inst.providerName, inst.planName].filter(Boolean).join(' \u00b7 ')}
          {inst.os && <span>\u00b7 {inst.os}</span>}
        </div>
      )}
      <div className="spec-chips-row">
        {inst.cpu > 0 && <SpecChip icon={<Cpu size={10} />}>{displayCPU(inst.cpu)}</SpecChip>}
        {inst.memory_gb > 0 && <SpecChip icon={<Server size={10} />}>{displayMemory(inst.memory_gb)}</SpecChip>}
        {inst.disk_gb > 0 && <SpecChip icon={<HardDrive size={10} />}>{displayDisk(inst.disk_gb)}</SpecChip>}
        {inst.bandwidth_mbps > 0 && <SpecChip icon={<Wifi size={10} />}>{displayBandwidth(inst.bandwidth_mbps)}</SpecChip>}
        <SpecChip icon={<ArrowUpDown size={10} />}>{displayTraffic(inst.traffic_gb)}</SpecChip>
        {inst.ipv4Count > 0 && <SpecChip icon={<Globe size={10} />}>{inst.ipv4Count} IPv4</SpecChip>}
      </div>
      <div className="instance-card-footer">
        <span className="instance-price">
          {currSymbol}{inst.price?.toFixed(2)}/{cycleLabel}
        </span>
        {inst.nextRenewal && inst.billingCycle !== 'lifetime' && (
          <span className="instance-dates">
            \u8d2d\u4e8e {inst.purchaseDate} \u00b7 \u7eed\u8d39 {inst.nextRenewal}
          </span>
        )}
      </div>
      {inst.notes && (
        <div className="instance-notes">{'\ud83d\udcdd'} {inst.notes}</div>
      )}
      {inst.profileId && (() => {
        const linked = profiles?.find(p => p.id === inst.profileId);
        return linked ? (
          <div className="instance-linked-profile">{'\ud83d\udd17'} {linked.name || linked.host}</div>
        ) : null;
      })()}
    </div>
  );
}

function VendorGroup({ vendor, instances, profiles, onEdit, onDelete }) {
  const [collapsed, setCollapsed] = useState(false);
  const monthlyCost = instances.reduce((acc, inst) => {
    const monthly = toMonthly(Number(inst.price || 0), inst.billingCycle);
    acc[inst.currency] = (acc[inst.currency] || 0) + monthly;
    return acc;
  }, {});
  const costStr = Object.entries(monthlyCost).map(([code, val]) => {
    const sym = CURRENCIES.find(c => c.code === code)?.symbol || '';
    return `${sym}${val.toFixed(2)}`;
  }).join(' / ');
  return (
    <div className="vendor-group">
      <div className="vendor-group-head" onClick={() => setCollapsed(c => !c)}>
        <span>{'\ud83c\udfe2'}</span>
        <span className="vendor-group-name">{vendor || '\u672a\u5206\u7ec4'}</span>
        <span className="vendor-group-meta">
          {instances.length} \u53f0 VPS \u00b7 {costStr}/\u6708
        </span>
        <span className="vendor-group-toggle">
          {collapsed ? '\u2304' : '\u2303'}
        </span>
      </div>
      {!collapsed && (
        <div className="vendor-group-body">
          {instances.map(inst => (
            <InstanceCard key={inst.id} inst={inst} profiles={profiles} onEdit={onEdit} onDelete={onDelete} />
          ))}
        </div>
      )}
    </div>
  );
}

function CostCenter({ profiles, instances, setInstances }) {
  const [draft, setDraft] = useState(() => emptyDraft());
  const [editingId, setEditingId] = useState('');
  const [showForm, setShowForm] = useState(false);
  const [showProviderSection, setShowProviderSection] = useState(false);
  const [memUnit, setMemUnit] = useState('GB');
  const [diskUnit, setDiskUnit] = useState('GB');
  const [bwUnit, setBwUnit] = useState('Mbps');
  const [trafficUnit, setTrafficUnit] = useState('GB');

  const providerMap = useMemo(() => {
    const map = {};
    for (const inst of instances) {
      const key = inst.providerName || '未指定厂商';
      if (!map[key]) map[key] = [];
      map[key].push(inst);
    }
    return map;
  }, [instances]);

  const providers = Object.keys(providerMap).sort();
  const providerCount = providers.length;

  const monthlyByCurrency = instances.reduce((acc, p) => {
    const m = cycleMonths(p.billingCycle);
    const monthly = m ? Number(p.price || 0) / m : 0;
    const cur = p.currency || 'CNY';
    acc[cur] = (acc[cur] || 0) + monthly;
    return acc;
  }, {});

  const monthlyLabel = Object.keys(monthlyByCurrency).length
    ? Object.entries(monthlyByCurrency).map(([c, v]) => `${currencySymbol(c)}${v.toFixed(2)}`).join(' / ')
    : '--';

  const annualLabel = Object.keys(monthlyByCurrency).length
    ? `${Object.entries(monthlyByCurrency).map(([c, v]) => `${currencySymbol(c)}${(v * 12).toFixed(2)}`).join(' / ')}`
    : '--';

  const openForm = (inst) => {
    if (inst) {
      setDraft({ ...emptyDraft(), ...inst, price: String(inst.price) });
      setEditingId(inst.id);
      setMemUnit(Number(inst.memory_gb) > 0 && Number(inst.memory_gb) < 1 ? 'MB' : 'GB');
      setDiskUnit(Number(inst.disk_gb) > 500 ? 'TB' : 'GB');
      setBwUnit(Number(inst.bandwidth_mbps) > 1000 ? 'Gbps' : 'Mbps');
      setTrafficUnit(Number(inst.traffic_gb) > 500 ? 'TB' : 'GB');
    } else {
      setDraft(emptyDraft());
      setEditingId('');
      setMemUnit('GB'); setDiskUnit('GB'); setBwUnit('Mbps'); setTrafficUnit('GB');
    }
    setShowForm(true);
    setShowProviderSection(false);
  };

  const saveInstance = async () => {
    const payload = {
      ...draft,
      cpu: Number(draft.cpu) || 0,
      memory_gb: Number(draft.memory_gb) || 0,
      disk_gb: Number(draft.disk_gb) || 0,
      bandwidth_mbps: Number(draft.bandwidth_mbps) || 0,
      traffic_gb: Number(draft.traffic_gb) || 0,
      ipv4Count: Number(draft.ipv4Count) || 0,
      price: Number(draft.price) || 0,
    };
    try {
      const r = await callBackend(SaveCostVPSInstance, payload);
      if (r?.ok) {
        setInstances((prev) => {
          if (editingId) {
            return prev.map((v) => v.id === editingId ? { ...payload, id: editingId } : v);
          }
          return [...prev, { ...payload, id: r.id }];
        });
        setShowForm(false);
        setEditingId('');
      }
    } catch (e) { console.warn(e); }
  };

  const deleteInstance = async (id) => {
    try {
      await callBackend(DeleteCostVPSInstance, id);
      setInstances((prev) => prev.filter((v) => v.id !== id));
    } catch (e) { console.warn(e); }
  };

  return (
    <div className="cost-layout">
      {/* v3.0 StatCards */}
      <div className="stat-cards-grid">
        <CostStatCard icon={<Building2 size={18} />} className="cost-stat-blue" label="厂商数" value={providerCount} sub={`共 ${instances.length} 台 VPS`} />
        <CostStatCard icon={<Monitor size={18} />} className="cost-stat-green" label="VPS 数" value={instances.length} sub={`${instances.filter(i => !isExpired(i)).length} 个活跃`} />
        <CostStatCard icon={<CreditCard size={18} />} className="cost-stat-amber" label="本月支出" value={monthlyLabel} sub="按货币分组" />
        <CostStatCard icon={<CalendarDays size={18} />} className="cost-stat-rose" label="预计年支出" value={annualLabel} sub={`月均 ${monthlyLabel}`} />
      </div>

      {/* v3.0 Expiry Banner */}
      <ExpiringBanner instances={instances} />

      {/* v3.0 Top Action Bar */}
      <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', marginBottom: 12, padding: '0 2px' }}>
        <span style={{ fontSize: 13, color: '#64748b' }}>
          {showForm
            ? `正在${editingId ? '编辑' : '添加'}${editingId ? ' ' + instances.find(i => i.id === editingId)?.vpsName : ''}`
            : `共 ${instances.length} 台 VPS，${providerCount} 家厂商`
          }
        </span>
        <button
          className={showForm ? '' : 'primary'}
          onClick={() => {
            if (showForm) { setShowForm(false); setEditingId(''); setMemUnit('GB'); setDiskUnit('GB'); setBwUnit('Mbps'); setTrafficUnit('GB'); }
            else { openForm(null); }
          }}
        >
          {showForm ? <><X size={14} /> 取消填写</> : <><Plus size={14} /> 添加 VPS</>}
        </button>
      </div>

      {/* v3.0 Vendor Groups */}
      {providers.map((name) => (
        <VendorGroup
          key={name}
          vendor={name === '未指定厂商' ? '' : name}
          instances={providerMap[name]}
          profiles={profiles}
          onEdit={openForm}
          onDelete={deleteInstance}
        />
      ))}

      {/* v3.0 Empty State */}
      {instances.length === 0 && !showForm && (
        <div style={{
          display: 'flex', flexDirection: 'column', alignItems: 'center',
          padding: '56px 24px', gap: '12px',
          border: '1.5px dashed var(--line)',
          borderRadius: '12px', color: '#94a3b8',
        }}>
          <div style={{ fontSize: 48 }}>{'\ud83d\udcbb'}</div>
          <div style={{ fontSize: 16, fontWeight: 600, color: '#475569' }}>
            还没有 VPS 记录
          </div>
          <div style={{ fontSize: 13, textAlign: 'center', maxWidth: 280 }}>
            添加你的第一台 VPS，开始追踪成本与续费日期
          </div>
          <button className="primary" style={{ marginTop: 8 }} onClick={() => openForm(null)}>
            <Plus size={15} /> 添加第一台 VPS
          </button>
        </div>
      )}

      {/* v3.0 Add/Edit Form */}
      {showForm && (
        <div className="instance-form">
          <h3 style={{ fontSize: 16, margin: '0 0 4px' }}>{editingId ? '编辑 VPS' : '添加 VPS'}</h3>

          <FormSection title="基本信息">
            <Field label="VPS 名称" required>
              <input value={draft.vpsName} onChange={(e) => setDraft({ ...draft, vpsName: e.target.value })} placeholder="如：洛杉矶 CN2 轻量 A 型" />
            </Field>
            <Field label="关联 SSH 配置（可选）">
              <select value={draft.profileId || ''} onChange={(e) => {
                const pid = e.target.value;
                const p = profiles.find((item) => item.id === pid);
                setDraft(p ? autoFillFromReport(draft, p, OS_PRESETS) : { ...draft, profileId: pid });
              }}>
                <option value="">-- 不关联 --</option>
                {profiles.map((p) => <option key={p.id} value={p.id}>{p.name || p.host}</option>)}
              </select>
            </Field>
          </FormSection>

          <FormSection title="规格配置">
            <div className="spec-grid">
              <div className="spec-cell">
                <div className="spec-input-wrap">
                  <input type="number" min={1} max={256} value={draft.cpu} onChange={(e) => setDraft({ ...draft, cpu: +e.target.value })} />
                </div>
                <label>CPU（核）</label>
              </div>
              <div className="spec-cell">
                <div className="spec-input-wrap">
                  <input type="number" min={0.5} step={0.5} value={getMemDisplay(draft.memory_gb, memUnit)}
                    onChange={(e) => setDraft({ ...draft, memory_gb: setMemGB(e.target.value, memUnit) })} />
                  <select value={memUnit} onChange={(e) => setMemUnit(e.target.value)}>
                    <option value="GB">GB</option>
                    <option value="MB">MB</option>
                  </select>
                </div>
                <label>内存</label>
              </div>
              <div className="spec-cell">
                <div className="spec-input-wrap">
                  <input type="number" min={1} value={getDiskDisplay(draft.disk_gb, diskUnit)}
                    onChange={(e) => setDraft({ ...draft, disk_gb: setDiskGB(e.target.value, diskUnit) })} />
                  <select value={diskUnit} onChange={(e) => setDiskUnit(e.target.value)}>
                    <option value="GB">GB</option>
                    <option value="TB">TB</option>
                  </select>
                </div>
                <label>硬盘</label>
              </div>
              <div className="spec-cell">
                <div className="spec-input-wrap">
                  <input type="number" min={1} value={getBwDisplay(draft.bandwidth_mbps, bwUnit)}
                    onChange={(e) => setDraft({ ...draft, bandwidth_mbps: setBwMbps(e.target.value, bwUnit) })} />
                  <select value={bwUnit} onChange={(e) => setBwUnit(e.target.value)}>
                    <option value="Mbps">Mbps</option>
                    <option value="Gbps">Gbps</option>
                  </select>
                </div>
                <label>带宽</label>
              </div>
              <div className="spec-cell">
                <div className="spec-input-wrap">
                  <input type="number" min={0} value={getTrafficDisplay(draft.traffic_gb, trafficUnit)}
                    onChange={(e) => setDraft({ ...draft, traffic_gb: setTrafficGB(e.target.value, trafficUnit) })} />
                  <select value={trafficUnit} onChange={(e) => setTrafficUnit(e.target.value)}>
                    <option value="GB">GB</option>
                    <option value="TB">TB</option>
                  </select>
                </div>
                <label>流量（0=不限）</label>
              </div>
              <div className="spec-cell">
                <div className="spec-input-wrap">
                  <input type="number" min={0} max={16} value={draft.ipv4Count}
                    onChange={(e) => setDraft({ ...draft, ipv4Count: +e.target.value })} />
                </div>
                <label>IPv4 数量</label>
              </div>
            </div>
          </FormSection>

          <FormSection title="计费信息">
            <Field label="实付金额" required>
              <div className="price-row">
                <div className="price-input-wrap">
                  <input className="price-amount-input" type="number" min={0} step={0.01} placeholder="0.00"
                    value={draft.price || ''} onChange={(e) => setDraft({ ...draft, price: parseFloat(e.target.value) || 0 })} />
                  <select className="price-currency-select" value={draft.currency}
                    onChange={(e) => setDraft({ ...draft, currency: e.target.value })} title="选择货币">
                    {CURRENCIES.map((c) => <option key={c.code} value={c.code}>{c.symbol} {c.code}</option>)}
                  </select>
                </div>
                <select className="cycle-select" value={draft.billingCycle}
                  onChange={(e) => {
                    const bc = e.target.value;
                    setDraft({ ...draft, billingCycle: bc, nextRenewal: draft.manualRenewal ? draft.nextRenewal : autoNextRenewal(draft.purchaseDate, bc) });
                  }}>
                  <option value="monthly">月付</option>
                  <option value="quarterly">季付</option>
                  <option value="semiannual">半年付</option>
                  <option value="annual">年付</option>
                  <option value="lifetime">终身买断</option>
                </select>
              </div>
            </Field>
          </FormSection>

          <FormSection title="时间信息">
            <div className="date-group" style={{ display: 'grid', gridTemplateColumns: '1fr 1fr', gap: 12 }}>
              <Field label="购买日期" required>
                <input type="date" value={draft.purchaseDate} onChange={(e) => {
                  const pd = e.target.value;
                  const nr = draft.manualRenewal ? draft.nextRenewal : autoNextRenewal(pd, draft.billingCycle);
                  setDraft({ ...draft, purchaseDate: pd, nextRenewal: nr });
                }} />
              </Field>
              <div>
                <div className="date-field-label-row">
                  <label style={{ fontSize: 12, color: '#64748b' }}>续费日期</label>
                  <div className="renewal-mode-switch">
                    <button className={`renewal-mode-btn ${!draft.manualRenewal ? 'active' : ''}`} onClick={() => {
                      setDraft({ ...draft, manualRenewal: false, nextRenewal: autoNextRenewal(draft.purchaseDate, draft.billingCycle) });
                    }}>{'\ud83d\udd04'} 自动</button>
                    <button className={`renewal-mode-btn ${draft.manualRenewal ? 'active' : ''}`} onClick={() => {
                      setDraft({ ...draft, manualRenewal: true });
                    }}>{'\u270f\ufe0f'} 手动</button>
                  </div>
                </div>
                <input type="date" className={!draft.manualRenewal ? 'date-input-readonly' : ''}
                  value={draft.nextRenewal} readOnly={!draft.manualRenewal}
                  onChange={(e) => draft.manualRenewal && setDraft({ ...draft, nextRenewal: e.target.value })} />
              </div>
            </div>
          </FormSection>

          <FormSection title="服务商信息（可选）" collapsible defaultOpen={showProviderSection} badge={draft.providerName || undefined}>
            <div className="provider-grid">
              <Field label="厂商名称">
                <input value={draft.providerName} onChange={(e) => setDraft({ ...draft, providerName: e.target.value })} placeholder="如：Vultr、RackNerd" />
              </Field>
              <Field label="官网 URL">
                <input value={draft.providerURL} onChange={(e) => setDraft({ ...draft, providerURL: e.target.value })} placeholder="https://" />
              </Field>
              <Field label="套餐/计划名称">
                <input value={draft.planName} onChange={(e) => setDraft({ ...draft, planName: e.target.value })} placeholder="如：Lite-One" />
              </Field>
            </div>
          </FormSection>

          <FormSection title="系统与备注">
            <Field label="操作系统">
              <select value={OS_PRESETS.find((o) => o.value === draft.os) ? draft.os : ''} onChange={(e) => setDraft({ ...draft, os: e.target.value })}>
                <option value="">-- 选择 --</option>
                {OS_PRESETS.map((o) => <option key={o.value} value={o.value}>{o.label}</option>)}
              </select>
              {!OS_PRESETS.find((o) => o.value === draft.os) && draft.os && (
                <input value={draft.os} onChange={(e) => setDraft({ ...draft, os: e.target.value })} placeholder="自定义系统" className="custom-os-input" />
              )}
            </Field>
            <Field label="备注">
              <textarea value={draft.notes} onChange={(e) => setDraft({ ...draft, notes: e.target.value })} placeholder="线路优化、DDoS 防护、备份策略等" rows={3} />
            </Field>
          </FormSection>

          <div className="form-actions">
            <button onClick={() => { setShowForm(false); setEditingId(''); setMemUnit('GB'); setDiskUnit('GB'); setBwUnit('Mbps'); setTrafficUnit('GB'); }}>取消</button>
            <button className="primary" onClick={saveInstance} disabled={!draft.vpsName.trim()}><Plus size={14} /> {editingId ? '保存修改' : '保存'}</button>
          </div>
        </div>
      )}
    </div>
  );
}

export default CostCenter;
