import { describe, it, expect, vi } from 'vitest';
import { render, screen } from '@testing-library/react';
import {
  PanelTitle,
  StatCard,
  Field,
  StatusPill,
  Overlay,
} from '../ui/UIComponents';

vi.mock('lucide-react', () => {
  const names = ['Activity', 'CheckCircle2', 'ShieldAlert', 'XCircle'];
  const mock = {};
  for (const name of names) {
    mock[name] = (props) => <div data-testid={`icon-${name}`} {...props} />;
  }
  return mock;
});

const MockIcon = (props) => <div data-testid="icon-mock" {...props} />;

describe('PanelTitle', () => {
  it('renders icon and title text', () => {
    render(<PanelTitle icon={MockIcon} title="Section Title" />);

    expect(screen.getByTestId('icon-mock')).toBeInTheDocument();
    expect(screen.getByText('Section Title')).toBeInTheDocument();
  });

  it('renders optional action when provided', () => {
    render(
      <PanelTitle
        icon={MockIcon}
        title="With Action"
        action={<button>Action Btn</button>}
      />
    );

    expect(screen.getByText('Action Btn')).toBeInTheDocument();
  });
});

describe('StatCard', () => {
  it('displays label, value, and hint', () => {
    render(
      <StatCard
        icon={MockIcon}
        label="CPU"
        value="4 cores"
        hint="healthy"
        tone="green"
      />
    );

    expect(screen.getByText('CPU')).toBeInTheDocument();
    expect(screen.getByText('4 cores')).toBeInTheDocument();
    expect(screen.getByText('healthy')).toBeInTheDocument();
  });

  it('applies tone class to the section element', () => {
    const { container } = render(
      <StatCard icon={MockIcon} label="Memory" value="8 GB" hint="ok" tone="amber" />
    );

    const section = container.querySelector('section');
    expect(section.className).toContain('amber');
  });
});

describe('Field', () => {
  it('renders label and children content', () => {
    render(
      <Field label="Username">
        <input data-testid="username-input" />
      </Field>
    );

    expect(screen.getByText('Username')).toBeInTheDocument();
    expect(screen.getByTestId('username-input')).toBeInTheDocument();
  });

  it('shows required indicator when required is true', () => {
    render(
      <Field label="Password" required>
        <input />
      </Field>
    );

    expect(screen.getByText('*')).toBeInTheDocument();
  });
});

describe('StatusPill', () => {
  it('renders the status text', () => {
    render(<StatusPill status="success" text="Deployed" />);

    expect(screen.getByText('Deployed')).toBeInTheDocument();
  });

  it('applies status class name', () => {
    const { container } = render(<StatusPill status="failed" text="Error" />);

    const pill = container.querySelector('.status-pill');
    expect(pill.className).toContain('failed');
  });
});

describe('Overlay', () => {
  it('renders overlay content', () => {
    render(<Overlay />);

    expect(screen.getByText('正在准备部署')).toBeInTheDocument();
    expect(screen.getByText('生成配置、订阅路径和远程任务')).toBeInTheDocument();
  });
});
