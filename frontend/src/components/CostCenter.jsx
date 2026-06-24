import { useMemo, useState } from 'react';
import {
  ArrowUpDown,
  Building2,
  CalendarDays,
  Check,
  Cpu,
  CreditCard,
  Edit,
  Globe,
  HardDrive,
  Monitor,
  Plus,
  Server,
  Tag,
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
            {open ? '⌃' : '⌄'}
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
    overdueCount > 0 && `${overdueCount} 台已过期`,
    soonCount > 0 && `${soonCount} 台即将到期`,
  ].filter(Boolean).join('，');
  const sorted = [...expiring].sort((a, b) => new Date(a.nextRenewal) - new Date(b.nextRenewal));
  return (
    <div className="expiry-banner">
      <div className="expiry-banner-header" onClick={() => setCollapsed(c => !c)} role="button" aria-expanded={!collapsed}>
        <span>{'⚠️'} 续费提醒</span>
        <span className="expiry-banner-count">{expiring.length}</span>
        <span className="expiry-banner-summary">{summaryText}</span>
        <span className="expiry-banner-toggle">{collapsed ? '展开 ⌄' : '收起 ⌃'}</span>
      </div>
      {!collapsed && sorted.map(inst => {
        const diffDays = Math.ceil((new Date(inst.nextRenewal) - new Date()) / 86400000);
        const isOverdue = diffDays < 0;
        const statusClass = isOverdue ? 'overdue' : 'due-soon';
        const statusIcon = isOverdue ? '\ud83d\udd34' : '\ud83d\udfe1';
        const statusText = isOverdue
          ? `已过期 ${Math.abs(diffDays)} 天`
          : diffDays === 0 ? '今日到期' : `${inst.nextRenewal}（${diffDays} 天后）`;
        const currSym = CURRENCIES.find(c => c.code === inst.currency)?.symbol || '';
        const cycleLabel = { monthly: '月', quarterly: '季', semiannual: '半年', annual: '年' }[inst.billingCycle] || '';
        return (
          <div key={inst.id} className="expiry-item">
            <div className={`expiry-item-dot ${statusClass}`} />
            <span>{statusIcon}</span>
            <span className="expiry-item-name">{inst.vpsName}</span>
            <span className="expiry-item-meta">
              {inst.providerName ? `${inst.providerName} · ` : ''}{statusText}
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

function InstanceCard({ inst, profiles, onEdit, onDelete, onQuickProvider }) {
  const status = getInstanceStatus(inst);
  const stripeColor = { overdue: '#f43f5e', 'due-week': '#f59e0b', 'due-month': '#f59e0b', lifetime: '#8b5cf6', ok: '#22c55e' }[status];
  const badge = {
    overdue: { text: '已过期', cls: 'badge-overdue' },
    'due-week': { text: '7天内到期', cls: 'badge-due-week' },
    'due-month': { text: '即将到期', cls: 'badge-due-month' },
    lifetime: { text: '终身', cls: 'badge-lifetime' },
    ok: null,
  }[status];
  const currSymbol = CURRENCIES.find(c => c.code === inst.currency)?.symbol || '';
  const cycleLabel = { monthly: '月', quarterly: '季', semiannual: '半年', annual: '年', lifetime: '买断' }[inst.billingCycle] || '';
  const [editingProvider, setEditingProvider] = useState(false);
  const [providerInput, setProviderInput] = useState('');

  const startProviderEdit = () => {
    setProviderInput(inst.providerName || '');
    setEditingProvider(true);
  };
  const saveProvider = async () => {
    setEditingProvider(false);
    if (providerInput !== (inst.providerName || '')) {
      onQuickProvider?.(inst.id, providerInput);
    }
  };
  const cancelProviderEdit = () => {
    setEditingProvider(false);
    setProviderInput('');
  };

  return (
    <div className="instance-card">
      <div className="instance-card-stripe" style={{ background: stripeColor }} />
      <div className="instance-card-header">
        <span className="instance-card-name">{inst.vpsName}</span>
        {badge && <span className={`instance-status-badge ${badge.cls}`}>{badge.text}</span>}
        <div className="instance-actions">
          <button className="btn-icon-sm" title="编辑" onClick={() => onEdit(inst)}><Edit size={13} /></button>
          <button className="btn-icon-sm danger" title="删除" onClick={() => onDelete(inst.id)}><Trash2 size={13} /></button>
        </div>
      </div>
      {editingProvider ? (
        <div className="instance-provider-inline" style={{ display: 'flex', alignItems: 'center', gap: 4, padding: '2px 0' }}>
          <Tag size={11} style={{ color: '#64748b', flexShrink: 0 }} />
          <input
            autoFocus
            value={providerInput}
            onChange={(e) => setProviderInput(e.target.value)}
            onKeyDown={(e) => { if (e.key === 'Enter') saveProvider(); if (e.key === 'Escape') cancelProviderEdit(); }}
            placeholder="输入厂商名称"
            style={{
              flex: 1, fontSize: 12, padding: '2px 6px', borderRadius: 4,
              border: '1px solid var(--line)', outline: 'none', background: 'var(--bg)',
            }}
          />
          <button onClick={saveProvider} style={{ padding: 2, color: '#22c55e', background: 'none', border: 'none', cursor: 'pointer' }} title="保存"><Check size={14} /></button>
          <button onClick={cancelProviderEdit} style={{ padding: 2, color: '#94a3b8', background: 'none', border: 'none', cursor: 'pointer' }} title="取消"><X size={14} /></button>
        </div>
      ) : (
        <div
          className="instance-card-subtitle"
          onClick={startProviderEdit}
          style={{ cursor: 'pointer' }}
          title="点击修改厂商分组"
        >
          {inst.providerName ? (
            <>
              <span>{inst.providerName}</span>
              <Edit size={10} style={{ color: '#94a3b8' }} />
            </>
          ) : (
            <span style={{ color: '#94a3b8' }}>
              <Tag size={10} style={{ marginRight: 3, verticalAlign: -1 }} />未分组（点击设置）
            </span>
          )}
          {inst.planName && <span>{inst.planName}</span>}
          {inst.os && <span>· {inst.os}</span>}
        </div>
      )}
      <div className="spec-chips-row">
        {inst.cpu > 0 && <SpecChip icon={<Cpu size={10} />}>{displayCPU(inst.cpu)}</SpecChip>}
        {inst.memory_gb > 0 && <SpecChip icon={<Server size={10} />}>{displayMemory(inst.memory_gb)}</SpecChip>}
        {inst.disk_gb > 0 && <SpecChip icon={<HardDrive size={10} />}>{displayDisk(inst.disk_gb)}</SpecChip>}
        {inst.bandwidth_mbps > 0 && <SpecChip icon={<Wifi size={10} />}>{displayBandwidth(inst.bandwidth_mbps)}</SpecChip>}
        <SpecChip icon={<ArrowUpDown size={10} />}>{displayTraffic(inst.traffic_gb)}</SpecChip>
        {inst.ipv4Count > 0 && <SpecChip icon={<Globe size={10} />}>{inst.ipv4Count} IPv4{inst.ipv4Address ? ` (${inst.ipv4Address})` : ''}</SpecChip>}
        {inst.ipv6Count > 0 && <SpecChip icon={<Globe size={10} />}>{inst.ipv6Count} IPv6{inst.ipv6Address ? ` (${inst.ipv6Address})` : ''}</SpecChip>}
      </div>
      <div className="instance-card-footer">
        <span className="instance-price">
          {currSymbol}{inst.price?.toFixed(2)}/{cycleLabel}
        </span>
        {inst.nextRenewal && inst.billingCycle !== 'lifetime' && (
          <span className="instance-dates">
            购于 {inst.purchaseDate} · 续费 {inst.nextRenewal}
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

function VendorGroup({ vendor, instances, profiles, onEdit, onDelete, onQuickProvider }) {
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
        <span className="vendor-group-name">{vendor || '未分组'}</span>
        <span className="vendor-group-meta">
          {instances.length} 台 VPS · {costStr}/月
        </span>
        <span className="vendor-group-toggle">
          {collapsed ? '⌄' : '⌃'}
        </span>
      </div>
      {!collapsed && (
        <div className="vendor-group-body">
          {instances.map(inst => (
            <InstanceCard key={inst.id} inst={inst} profiles={profiles} onEdit={onEdit} onDelete={onDelete} onQuickProvider={onQuickProvider} />
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
  const [formError, setFormError] = useState('');
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
      let nextDraft = { ...emptyDraft(), ...inst, price: Number(inst.price) || 0 };
      // Sync fresh report data from linked profile if available
      if (inst.profileId) {
        const linkedProfile = profiles.find(p => p.id === inst.profileId);
        if (linkedProfile?.report) {
          nextDraft = autoFillFromReport(nextDraft, linkedProfile, OS_PRESETS);
        }
      }
      setDraft(nextDraft);
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
    setFormError('');
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
      ipv4Address: draft.ipv4Address || '',
      ipv6Count: Number(draft.ipv6Count) || 0,
      ipv6Address: Number(draft.ipv6Count) > 0 ? (draft.ipv6Address || '') : '',
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
        setFormError('');
      } else {
        setFormError(r?.error || '保存失败：未知错误');
      }
    } catch (e) {
      setFormError(String(e));
    }
  };

  const deleteInstance = async (id) => {
    try {
      await callBackend(DeleteCostVPSInstance, id);
      setInstances((prev) => prev.filter((v) => v.id !== id));
    } catch (e) { console.warn(e); }
  };

  const quickProvider = async (id, providerName) => {
    const inst = instances.find(v => v.id === id);
    if (!inst) return;
    const updated = { ...inst, providerName };
    try {
      const r = await callBackend(SaveCostVPSInstance, updated);
      if (r?.ok) {
        setInstances((prev) => prev.map((v) => v.id === id ? updated : v));
      } else {
        console.error('quickProvider failed:', r?.error);
        alert('修改厂商失败：' + (r?.error || '未知错误'));
      }
    } catch (e) {
      console.error('quickProvider error:', e);
      alert('修改厂商失败：' + String(e));
    }
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
          onQuickProvider={quickProvider}
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
          {formError && (
            <div style={{
              background: '#fef2f2', border: '1px solid #fecaca', borderRadius: 8,
              color: '#dc2626', fontSize: 13, padding: '8px 12px', margin: '8px 0',
            }}>
              {formError}
            </div>
          )}

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
                  <input type="number" min={0} value={getBwDisplay(draft.bandwidth_mbps, bwUnit)}
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
              <div className="spec-cell">
                <div className="spec-input-wrap">
                  <input type="text" value={draft.ipv4Address} placeholder="如 1.2.3.4"
                    onChange={(e) => setDraft({ ...draft, ipv4Address: e.target.value })} />
                </div>
                <label>IPv4 地址</label>
              </div>
              <div className="spec-cell">
                <div className="spec-input-wrap">
                  <input type="number" min={0} max={16} value={draft.ipv6Count}
                    onChange={(e) => setDraft({ ...draft, ipv6Count: +e.target.value })} />
                </div>
                <label>IPv6 数量</label>
              </div>
              {draft.ipv6Count > 0 && (
                <div className="spec-cell">
                  <div className="spec-input-wrap">
                    <input type="text" value={draft.ipv6Address} placeholder="如 2001:db8::1"
                      onChange={(e) => setDraft({ ...draft, ipv6Address: e.target.value })} />
                  </div>
                  <label>IPv6 地址</label>
                </div>
              )}
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
                    }}>{'✏️'} 手动</button>
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
