import { domainIconMap, labelIconMap } from '../../siteIcons';
import { domainFromText, normalizeDomain } from '../../utils/format';

export function ProtocolGlyph({ id }) {
  const common = { viewBox: '0 0 24 24', fill: 'none', stroke: 'currentColor', strokeWidth: '2', strokeLinecap: 'round', strokeLinejoin: 'round', 'aria-hidden': true };
  if (id === 'shield') {
    return <svg {...common}><path d="M12 3l7 3v5c0 4.6-2.8 8.1-7 10-4.2-1.9-7-5.4-7-10V6l7-3z" /><path d="M9 12l2 2 4-5" /></svg>;
  }
  if (id === 'wave') {
    return <svg {...common}><path d="M3 12c2.2-4 4.4-4 6.6 0s4.4 4 6.6 0S20.6 8 22 10" /><path d="M3 17c2.2-4 4.4-4 6.6 0s4.4 4 6.6 0" /></svg>;
  }
  if (id === 'bolt') {
    return <svg {...common}><path d="M13 2L4 14h7l-1 8 10-13h-7l0-7z" /></svg>;
  }
  if (id === 'lock') {
    return <svg {...common}><rect x="5" y="10" width="14" height="10" rx="2" /><path d="M8 10V7a4 4 0 0 1 8 0v3" /><path d="M12 14v2" /></svg>;
  }
  if (id === 'layers') {
    return <svg {...common}><path d="M12 3l9 5-9 5-9-5 9-5z" /><path d="M3 13l9 5 9-5" /><path d="M3 17l9 5 9-5" /></svg>;
  }
  return <svg {...common}><circle cx="6" cy="12" r="3" /><circle cx="18" cy="7" r="3" /><circle cx="18" cy="17" r="3" /><path d="M8.6 10.7l6.8-2.4" /><path d="M8.6 13.3l6.8 2.4" /></svg>;
}

export function SiteGlyph({ id }) {
  const common = { viewBox: '0 0 24 24', fill: 'none', stroke: 'currentColor', strokeWidth: '2', strokeLinecap: 'round', strokeLinejoin: 'round', 'aria-hidden': true };
  if (id === 'ippure') {
    return <svg {...common}><path d="M12 3l7 3v5c0 4.6-2.8 8.1-7 10-4.2-1.9-7-5.4-7-10V6l7-3z" /><path d="M9 12h6" /><path d="M12 9v6" /></svg>;
  }
  if (id === 'ping0') {
    return <svg {...common}><circle cx="12" cy="12" r="3" /><path d="M12 2v4" /><path d="M12 18v4" /><path d="M2 12h4" /><path d="M18 12h4" /><path d="M5 5l3 3" /><path d="M16 16l3 3" /></svg>;
  }
  return <svg {...common}><path d="M7 18a5 5 0 1 1 1.8-9.6A6 6 0 0 1 20 11.5 3.5 3.5 0 0 1 19.5 18H7z" /><path d="M9 13h6" /></svg>;
}

export function ServiceGlyph({ label = '' }) {
  const text = String(label);
  const common = { viewBox: '0 0 24 24', fill: 'none', stroke: 'currentColor', strokeWidth: '2', strokeLinecap: 'round', strokeLinejoin: 'round', 'aria-hidden': true };
  if (/YouTube|Disney|Netflix|TikTok|Reddit/i.test(text)) {
    return <svg {...common}><rect x="3" y="5" width="18" height="14" rx="3" /><path d="M10 9l5 3-5 3V9z" fill="currentColor" stroke="none" /></svg>;
  }
  if (/OpenAI|ChatGPT|AI/i.test(text)) {
    return <svg {...common}><circle cx="12" cy="12" r="2.5" /><path d="M12 3c3 0 4 2 4 4 2 0 4 1.5 4 4s-2 4-4 4c0 2-1 4-4 4s-4-2-4-4c-2 0-4-1.5-4-4s2-4 4-4c0-2 1-4 4-4z" /></svg>;
  }
  if (/Gmail|Outlook|Yahoo|iCloud|QQ|Mail|SMTP|邮局/i.test(text)) {
    return <svg {...common}><rect x="3" y="5" width="18" height="14" rx="3" /><path d="M4 7l8 6 8-6" /></svg>;
  }
  if (/DNSBL|风险|Fraud|Proxy|Hosting|Mobile|住宅|广播|ISP|WARP|Gateway/i.test(text)) {
    return <svg {...common}><path d="M12 3l7 3v5c0 4.6-2.8 8.1-7 10-4.2-1.9-7-5.4-7-10V6l7-3z" /><path d="M12 8v5" /><path d="M12 17h.01" /></svg>;
  }
  return <svg {...common}><circle cx="12" cy="12" r="9" /><path d="M3 12h18" /><path d="M12 3c2.5 2.7 3.7 5.7 3.7 9S14.5 18.3 12 21c-2.5-2.7-3.7-5.7-3.7-9S9.5 5.7 12 3z" /></svg>;
}

export function LocalLogo({ domain, label, fallback }) {
  const icon = localIconFor(domain, label);
  if (!icon) {
    return fallback;
  }
  return <img className="local-logo" src={icon} alt={`${label || domain || 'site'} icon`} loading="lazy" />;
}

function localIconFor(domain = '', label = '') {
  const exactLabel = String(label || '').trim();
  if (labelIconMap[exactLabel]) {
    return labelIconMap[exactLabel];
  }
  const labelKey = Object.keys(labelIconMap).find((key) => exactLabel && exactLabel.toLowerCase().includes(key.toLowerCase()));
  if (labelKey) {
    return labelIconMap[labelKey];
  }
  const cleanDomain = normalizeDomain(domain);
  if (!cleanDomain) {
    return '';
  }
  if (domainIconMap[cleanDomain]) {
    return domainIconMap[cleanDomain];
  }
  const domainKey = Object.keys(domainIconMap)
    .sort((a, b) => b.length - a.length)
    .find((key) => cleanDomain === key || cleanDomain.endsWith(`.${key}`));
  return domainKey ? domainIconMap[domainKey] : '';
}
