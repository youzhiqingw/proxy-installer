import {
  Clipboard,
  Link2,
} from 'lucide-react';
import { useEffect, useState } from 'react';
import QRCode from 'qrcode';
import { clients } from '../utils/constants';
import { formatMbps } from '../utils/format';
import { PanelTitle, Detail } from './ui/UIComponents';

function Result({ activeClient, setActiveClient, subscriptions, speed }) {
  const item = subscriptions[activeClient];
  const [qrData, setQrData] = useState('');
  useEffect(() => {
    if (!item?.url?.startsWith('http')) {
      setQrData('');
      return;
    }
    QRCode.toDataURL(item.url, {
      margin: 1,
      width: 180,
      color: { dark: '#111827', light: '#ffffff' },
    }).then(setQrData).catch(() => setQrData(''));
  }, [item?.url]);

  return (
    <div className="result-layout">
      <section className="panel result-main">
        <PanelTitle icon={Link2} title="客户端订阅" />
        <div className="client-tabs">
          {clients.map(([key, label]) => <button key={key} className={activeClient === key ? 'active' : ''} onClick={() => setActiveClient(key)}>{label}</button>)}
        </div>
        <div className="result-card">
          <div>
            <span>订阅 URL</span>
            <strong>{item.url}</strong>
          </div>
          <button className="primary" onClick={() => navigator.clipboard.writeText(item.url)} disabled={!item.url.startsWith('http')}><Clipboard size={16} />复制订阅</button>
        </div>
        <div className="detail-list horizontal">
          <Detail label="最近延迟" value={speed.items.find((entry) => entry.status === 'ok')?.latencyMs ? `${speed.items.find((entry) => entry.status === 'ok')?.latencyMs} ms` : '-'} />
          <Detail label="出口下载" value={speed.remote ? `${formatMbps(speed.remote.downloadMbps)} Mbps` : '-'} />
        </div>
      </section>
      <section className="panel qr-panel">
        <div className="qr-box">
          {qrData ? <img src={qrData} alt={`${item.label} QR`} /> : <div className="qr-placeholder" />}
        </div>
        <strong>{item.label}</strong>
      </section>
    </div>
  );
}

export default Result;
