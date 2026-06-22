import { describe, it, expect, vi } from 'vitest';
import { render, screen } from '@testing-library/react';
import Dashboard from '../Dashboard';

vi.mock('lucide-react', () => {
  const names = [
    'Activity', 'Clipboard', 'Cpu', 'DollarSign', 'Fingerprint',
    'Gauge', 'Globe2', 'HardDrive', 'LayoutDashboard', 'Link2',
    'ListChecks', 'Mail', 'MemoryStick', 'Percent', 'Radar',
    'Rocket', 'Search', 'Server', 'ShieldAlert', 'ShieldCheck',
    'TerminalSquare', 'Trash2', 'Tv', 'Wifi', 'Zap',
  ];
  const mock = {};
  for (const name of names) {
    mock[name] = (props) => <div data-testid={`icon-${name}`} {...props} />;
  }
  return mock;
});

const baseSpeed = {
  items: [],
  remote: null,
  node: null,
  quality: null,
};

const baseProgress = { status: 'idle', message: '' };

const defaultProps = {
  profiles: [],
  profile: null,
  report: null,
  progress: baseProgress,
  speed: baseSpeed,
  setActiveTab: vi.fn(),
};

describe('Dashboard', () => {
  it('renders stat cards with correct labels', () => {
    render(<Dashboard {...defaultProps} />);

    expect(screen.getByText('VPS')).toBeInTheDocument();
    expect(screen.getByText('系统状态')).toBeInTheDocument();
    expect(screen.getByText('链路延迟')).toBeInTheDocument();
    expect(screen.getByText('速度损耗')).toBeInTheDocument();
    expect(screen.getByText('IP 纯净度')).toBeInTheDocument();
    expect(screen.getByText('部署状态')).toBeInTheDocument();
  });

  it('displays profile host when a profile is available', () => {
    const props = {
      ...defaultProps,
      profiles: [{ name: 'us-1', host: '1.2.3.4', user: 'root', port: 22 }],
      profile: { name: 'us-1', host: '1.2.3.4', user: 'root', port: 22 },
    };

    render(<Dashboard {...props} />);

    expect(screen.getByText('1.2.3.4')).toBeInTheDocument();
    expect(screen.getByText('1')).toBeInTheDocument();
  });

  it('shows fallback text when no profiles are provided', () => {
    render(<Dashboard {...defaultProps} />);

    expect(screen.getAllByText('未添加').length).toBeGreaterThanOrEqual(1);
    expect(screen.getByText('0')).toBeInTheDocument();
  });

  it('renders speed test data when present', () => {
    const props = {
      ...defaultProps,
      speed: {
        items: [{ status: 'ok', latencyMs: 42 }],
        remote: { downloadMbps: 500 },
        node: { downloadMbps: 350 },
        quality: null,
      },
    };

    render(<Dashboard {...props} />);

    expect(screen.getByText('42 ms')).toBeInTheDocument();
    expect(screen.getByText('30%')).toBeInTheDocument();
    expect(screen.getByText('VPS 500.0 Mbps')).toBeInTheDocument();
    expect(screen.getByText('节点 350.0 Mbps')).toBeInTheDocument();
  });
});
