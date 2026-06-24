import { Clipboard, TerminalSquare } from 'lucide-react';
import { logKind } from '../utils/format';
import { PanelTitle } from './ui/UIComponents';

function Progress({ progress, showLogs, setShowLogs }) {
  const logText = progress.logs.join('\n');
  return (
    <div className="progress-page">
      <section className={`panel progress-card ${progress.status}`}>
        <PanelTitle icon={TerminalSquare} title="部署进度" />
        <div className="progress-track"><span style={{ width: `${progress.value}%` }} /></div>
        <div className="progress-number">{progress.value}%</div>
        <h2>{progress.message}</h2>
        {progress.status === 'failed' && <p className="error-text">请复制日志提交给 AI 或者是认识的技术人员。</p>}
        <div className="actions">
          <button onClick={() => setShowLogs(!showLogs)}><TerminalSquare size={16} />{showLogs ? '隐藏日志' : '查看日志'}</button>
          <button onClick={() => navigator.clipboard.writeText(logText)} disabled={!logText}><Clipboard size={16} />复制日志</button>
        </div>
      </section>
      {showLogs && (
        <section className="panel log-chat">
          <PanelTitle icon={TerminalSquare} title="日志事件" />
          <div className="log-chat-scroll">
            {progress.logs.map((line, index) => (
              <div className={`log-bubble ${logKind(line)}`} key={`${line}-${index}`}>
                <span>{logKind(line)}</span>
                <code>{line.replace(/^\[[^\]]+\]\s*/, '')}</code>
              </div>
            ))}
            {progress.logs.length === 0 && <div className="log-empty">暂无日志</div>}
          </div>
        </section>
      )}
    </div>
  );
}

export default Progress;
