import { protocols } from './constants';

// ─── Domain & URL ────────────────────────────────────────────────────────────

export function domainFromText(value = '') {
  const text = String(value).trim();
  if (!text) return '';
  try {
    if (/^https?:\/\//i.test(text)) {
      return new URL(text).hostname;
    }
  } catch {
    return '';
  }
  const match = text.match(/(?:^|[\s/])((?:[a-z0-9-]+\.)+[a-z]{2,})(?=$|[\s/:])/i);
  return match ? match[1] : '';
}

export function normalizeDomain(domain = '') {
  return String(domain)
    .trim()
    .replace(/^https?:\/\//i, '')
    .replace(/\/.*$/, '')
    .replace(/:\d+$/, '')
    .toLowerCase();
}

// ─── Network & Profile ───────────────────────────────────────────────────────

export function formatHost(host) {
  return host.includes(':') && !host.startsWith('[') ? `[${host}]` : host;
}

export function preferredEndpointHost(profile) {
  return profile?.report?.network?.publicIpv4 || profile?.report?.network?.publicIpv6 || profile?.host || '';
}

export function networkStack(report) {
  if (!report) return '未体检';
  const has4 = Boolean(report.network?.publicIpv4 || report.network?.ipv4Route);
  const has6 = Boolean(report.network?.publicIpv6 || report.network?.ipv6Route || report.network?.ipv6Global);
  if (has4 && has6) return 'IPv4 / IPv6';
  if (has6) return 'IPv6';
  if (has4) return 'IPv4';
  return '未知';
}

export function reportSummary(report) {
  if (!report) return '未体检';
  const nat = report.network?.natLikely ? 'NAT' : '公网';
  return `${nat} / ${networkStack(report)} / ${report.runtime?.virtualization || 'unknown'}`;
}

export function toolSummary(tools = {}) {
  const required = ['curl', 'nginx', 'openssl', 'ss', 'systemctl'];
  const ok = required.filter((key) => tools[key]).length;
  return `${ok}/${required.length} 已就绪`;
}

// ─── Port & Protocol ─────────────────────────────────────────────────────────

export function protocolName(id) {
  return protocols.find((item) => item.id === id)?.label || id;
}

export function externalPort(config, id, fallback) {
  return config.publicPorts?.[id] || config.ports?.[id] || fallback;
}

export function externalWebPort(config) {
  return config.publicWebPort || config.webPort || 8080;
}

export function formatPortMappings(config) {
  if (!config.selected?.length) return '-';
  return config.selected.map((id) => {
    const def = protocols.find((item) => item.id === id)?.port || 0;
    const inside = config.ports?.[id] || def;
    const outside = externalPort(config, id, def);
    return `${protocolName(id)} ${inside}->${outside}`;
  }).join(' / ');
}

export function clientPath(key) {
  if (key === 'mihomo') return 'mihomo.yaml';
  if (key === 'singbox') return 'sing-box.json';
  return key;
}

// ─── Number Formatting ───────────────────────────────────────────────────────

export function formatNumber(value) {
  const num = Number(value || 0);
  return num ? num.toFixed(2) : '0.00';
}

export function formatMbps(value) {
  const num = Number(value || 0);
  return num ? num.toFixed(1) : '0.0';
}

export function speedLossPercent(remote, node) {
  const direct = Number(remote?.downloadMbps || 0);
  const proxied = Number(node?.downloadMbps || 0);
  if (!direct || !proxied) return null;
  return Math.max(0, Math.min(100, Math.round((1 - proxied / direct) * 100)));
}

// ─── Cost Display ────────────────────────────────────────────────────────────

export function displayCPU(v) { return `${v} 核心`; }
export function displayMemory(v) { return v < 1 ? `${Math.round(v * 1024)} MB` : `${v.toFixed(1)} GB`; }
export function displayDisk(v) { return v >= 1024 ? `${(v / 1024).toFixed(1)} TB` : `${v} GB`; }
export function displayBandwidth(v) { return v >= 1000 ? `${(v / 1000).toFixed(1)} Gbps` : `${v} Mbps`; }
export function displayTraffic(v) { return v === 0 ? '不限' : v >= 1024 ? `${(v / 1024).toFixed(1)} TB/月` : `${v} GB/月`; }
export function displayCount(v) { return `${v} 个`; }
export function cycleMonths(c) { return { monthly: 1, quarterly: 3, semiannual: 6, annual: 12, lifetime: 0 }[c] || 1; }
export function BillingLabel(c) { return { monthly: '月', quarterly: '季', semiannual: '半年', annual: '年', lifetime: '终身' }[c] || c; }

export function currencySymbol(c) {
  const sym = { CNY: '¥', USD: '$', EUR: '€', CAD: 'C$', JPY: '¥', GBP: '£', AUD: 'A$', SGD: 'S$', HKD: 'HK$', TWD: 'NT$' };
  return sym[c] || c;
}

export function currencyOptions() {
  return ['CNY', 'USD', 'EUR', 'CAD', 'JPY', 'GBP', 'AUD', 'SGD', 'HKD', 'TWD'];
}

// ─── Cost Calculation ────────────────────────────────────────────────────────

export function toMonthly(price, billingCycle) {
  const divisor = { monthly: 1, quarterly: 3, semiannual: 6, annual: 12, lifetime: 0 };
  const d = divisor[billingCycle] ?? 1;
  return d === 0 ? 0 : price / d;
}

export function autoNextRenewal(purchaseDate, billingCycle) {
  if (!purchaseDate || billingCycle === 'lifetime') return '';
  const map = { monthly: 1, quarterly: 3, semiannual: 6, annual: 12 };
  const months = map[billingCycle] || 1;
  const now = new Date();
  let d = new Date(purchaseDate);
  while (d <= now) { d = new Date(d.getFullYear(), d.getMonth() + months, d.getDate()); }
  return d.toISOString().split('T')[0];
}

export function calcFrontendRenewal(purchaseDate, billingCycle) {
  if (billingCycle === 'lifetime') return '';
  const months = { monthly: 1, quarterly: 3, semiannual: 6, annual: 12 }[billingCycle];
  if (!months) return '';
  const d = new Date(purchaseDate);
  if (isNaN(d.getTime())) return '';
  const now = new Date();
  let next = new Date(d);
  while (next <= now) {
    next.setMonth(next.getMonth() + months);
  }
  return next.toISOString().slice(0, 10);
}

export function getInstanceStatus(inst) {
  if (inst.billingCycle === 'lifetime') return 'lifetime';
  if (!inst.nextRenewal) return 'ok';
  const diffDays = Math.ceil((new Date(inst.nextRenewal) - new Date()) / (1000 * 60 * 60 * 24));
  if (diffDays < 0) return 'overdue';
  if (diffDays <= 7) return 'due-week';
  if (diffDays <= 30) return 'due-month';
  return 'ok';
}

export function isExpired(inst) { return getInstanceStatus(inst) === 'overdue'; }

export function formatMonthlyCost(monthly) {
  if (!monthly || Object.keys(monthly).length === 0) return '¥0';
  return Object.entries(monthly).map(([code, val]) => {
    const sym = CURRENCIES_INLINE[code] || code;
    return `${sym}${val.toFixed(2)}`;
  }).join(' / ');
}

export function formatAnnualCost(monthly) {
  if (!monthly || Object.keys(monthly).length === 0) return '¥0';
  return Object.entries(monthly).map(([code, val]) => {
    const sym = CURRENCIES_INLINE[code] || code;
    return `${sym}${(val * 12).toFixed(0)}`;
  }).join(' / ');
}

const CURRENCIES_INLINE = {
  CNY: '¥', USD: '$', EUR: '€', JPY: '¥', HKD: 'HK$', GBP: '£',
  CAD: 'CA$', AUD: 'A$', SGD: 'S$', TWD: 'NT$',
};

// ─── Spec Unit Conversion ────────────────────────────────────────────────────

export function getMemDisplay(val, unit) { return unit === 'MB' ? (val || 0) * 1024 : (val || 0); }
export function setMemGB(displayVal, unit) { return unit === 'MB' ? (Number(displayVal) || 0) / 1024 : Number(displayVal) || 0; }
export function getDiskDisplay(val, unit) { return unit === 'TB' ? (val || 0) / 1024 : (val || 0); }
export function setDiskGB(displayVal, unit) { return unit === 'TB' ? (Number(displayVal) || 0) * 1024 : Number(displayVal) || 0; }
export function getBwDisplay(val, unit) { return unit === 'Gbps' ? (val || 0) / 1000 : (val || 0); }
export function setBwMbps(displayVal, unit) { return unit === 'Gbps' ? (Number(displayVal) || 0) * 1000 : Number(displayVal) || 0; }
export function getTrafficDisplay(val, unit) { return unit === 'TB' ? (val || 0) / 1024 : (val || 0); }
export function setTrafficGB(displayVal, unit) { return unit === 'TB' ? (Number(displayVal) || 0) * 1024 : Number(displayVal) || 0; }

// ─── Status & Misc ───────────────────────────────────────────────────────────

export function normalizeReportStatus(status = '') {
  const value = String(status).toLowerCase();
  if (['ok', 'open', 'clean', 'success'].includes(value)) return 'ok';
  if (['fail', 'failed', 'listed', 'blocked'].includes(value)) return 'bad';
  return 'skip';
}

export function statusText(status) {
  if (status === 'ok') return '正常';
  if (status === 'bad') return '异常';
  return '跳过';
}

export function protocolStatusText(status) {
  if (status === 'complete') return '完整';
  if (status === 'partial') return '残留';
  return '未发现';
}

export function logKind(line = '') {
  const match = String(line).match(/^\[([^\]]+)\]/);
  const kind = match?.[1] || 'log';
  if (/error|failed/i.test(kind)) return 'error';
  if (/done|result|success/i.test(kind)) return 'done';
  if (/warn/i.test(kind)) return 'warn';
  if (/progress|ports/i.test(kind)) return 'progress';
  return 'log';
}

export function serviceTone(label = '') {
  const text = String(label);
  if (/Netflix|YouTube|Disney|TikTok|Reddit/i.test(text)) return 'rose';
  if (/OpenAI|ChatGPT|AI/i.test(text)) return 'green';
  if (/Gmail|Outlook|Yahoo|iCloud|QQ|Mail|SMTP|邮局/i.test(text)) return 'blue';
  if (/DNSBL|风险|Fraud|Scamalytics/i.test(text)) return 'amber';
  if (/Proxy|Hosting|Mobile|住宅|广播|ISP/i.test(text)) return 'violet';
  return 'slate';
}

export const serviceIconDomains = {
  Netflix: 'www.netflix.com',
  YouTube: 'www.youtube.com',
  'Disney+': 'www.disneyplus.com',
  TikTok: 'www.tiktok.com',
  Reddit: 'www.reddit.com',
  'OpenAI API': 'platform.openai.com',
  ChatGPT: 'chat.openai.com',
  Gmail: 'mail.google.com',
  Outlook: 'outlook.com',
  Yahoo: 'mail.yahoo.com',
  iCloud: 'www.icloud.com',
  'QQ Mail': 'mail.qq.com',
  'Mail.ru': 'mail.ru',
  AOL: 'mail.aol.com',
  GMX: 'www.gmx.net',
  'Mail.com': 'www.mail.com',
};

export function iconDomainForRow(row = {}) {
  if (serviceIconDomains[row.label]) {
    return serviceIconDomains[row.label];
  }
  return domainFromText(row.source || row.value || '');
}

export function emptyDraft() {
  return {
    id: '',
    vpsName: '',
    host: '',
    cpu: 2,
    memory_gb: 2,
    disk_gb: 30,
    bandwidth_mbps: 20,
    traffic_gb: 1000,
    ipv4Count: 1,
    price: '',
    currency: 'CNY',
    billingCycle: 'monthly',
    purchaseDate: new Date().toISOString().slice(0, 10),
    nextRenewal: '',
    manualRenewal: false,
    providerName: '',
    providerURL: '',
    planName: '',
    os: '',
    profileId: '',
    notes: '',
  };
}
