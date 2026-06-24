import {
  DollarSign,
  Fingerprint,
  Gauge,
  Globe2,
  LayoutDashboard,
  Link2,
  ListChecks,
  Mail,
  Rocket,
  Server,
  ShieldAlert,
  TerminalSquare,
  Trash2,
  Tv,
} from 'lucide-react';

export const tabs = [
  { id: 'dashboard', label: '仪表盘', desc: '状态总览', icon: LayoutDashboard },
  { id: 'cost', label: '成本中心', desc: '厂商与账单', icon: DollarSign },
  { id: 'configs', label: 'VPS 管理', desc: 'SSH 与体检', icon: Server },
  { id: 'deploy', label: '节点部署', desc: '协议与订阅', icon: Rocket },
  { id: 'speed', label: '测速中心', desc: '延迟与出口', icon: Gauge },
  { id: 'maintenance', label: '维护清理', desc: '印记与卸载', icon: Trash2 },
  { id: 'progress', label: '进度日志', desc: '部署事件', icon: TerminalSquare },
  { id: 'result', label: '节点信息', desc: '客户端订阅', icon: Link2 },
];

export const protocols = [
  { id: 'vless-reality', label: 'VLESS Reality', desc: '推荐，TLS 伪装', port: 443, tone: 'blue', icon: 'shield' },
  { id: 'hy2', label: 'Hysteria2', desc: 'UDP 传输，抗丢包', port: 8443, tone: 'green', icon: 'wave' },
  { id: 'tuic', label: 'TUIC', desc: 'QUIC 低延迟', port: 8444, tone: 'amber', icon: 'bolt' },
  { id: 'trojan', label: 'Trojan', desc: '小火箭兼容强', port: 8445, tone: 'rose', icon: 'lock' },
  { id: 'ss', label: 'Shadowsocks', desc: '轻量通用', port: 8388, tone: 'slate', icon: 'layers' },
  { id: 'vmess', label: 'VMess', desc: 'V2rayNG 兼容', port: 2083, tone: 'violet', icon: 'nodes' },
];

export const clients = [
  ['shadowrocket', 'Shadowrocket'],
  ['mihomo', 'Clash Meta'],
  ['v2rayng', 'V2rayNG'],
  ['singbox', 'sing-box'],
];

export const defaultPorts = Object.fromEntries(protocols.map((item) => [item.id, item.port]));

export const qualitySiteMeta = {
  ippure: { tone: 'blue' },
  ping0: { tone: 'green' },
  iplark: { tone: 'amber' },
};

export const qualitySectionMeta = {
  basic: { icon: Globe2, tone: 'blue' },
  type: { icon: Fingerprint, tone: 'green' },
  risk: { icon: ShieldAlert, tone: 'rose' },
  factor: { icon: ListChecks, tone: 'amber' },
  stream: { icon: Tv, tone: 'violet' },
  mail: { icon: Mail, tone: 'slate' },
};

export const CURRENCIES = [
  { code: 'CNY', symbol: '¥' }, { code: 'USD', symbol: '$' }, { code: 'EUR', symbol: '€' },
  { code: 'JPY', symbol: '¥' }, { code: 'HKD', symbol: 'HK$' }, { code: 'GBP', symbol: '£' },
  { code: 'CAD', symbol: 'CA$' }, { code: 'AUD', symbol: 'A$' }, { code: 'SGD', symbol: 'S$' },
  { code: 'TWD', symbol: 'NT$' },
];

export const OS_PRESETS = [
  { label: 'Debian 12', value: 'Debian 12' },
  { label: 'Debian 11', value: 'Debian 11' },
  { label: 'Ubuntu 24.04', value: 'Ubuntu 24.04' },
  { label: 'Ubuntu 22.04', value: 'Ubuntu 22.04' },
  { label: 'Ubuntu 20.04', value: 'Ubuntu 20.04' },
  { label: 'CentOS 7', value: 'CentOS 7' },
  { label: 'CentOS Stream', value: 'CentOS Stream' },
  { label: 'AlmaLinux 9', value: 'AlmaLinux 9' },
  { label: 'AlmaLinux 8', value: 'AlmaLinux 8' },
  { label: 'Rocky Linux 9', value: 'Rocky Linux 9' },
  { label: 'Rocky Linux 8', value: 'Rocky Linux 8' },
  { label: 'Arch Linux', value: 'Arch Linux' },
  { label: '其他', value: '', custom: true },
];
