import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen, fireEvent, waitFor } from '@testing-library/react';
import { useState } from 'react';
import CostCenter from '../CostCenter';

// Mock lucide-react icons used by CostCenter and its sub-components
vi.mock('lucide-react', () => {
  const names = [
    // Icons used directly by CostCenter.jsx
    'ArrowUpDown', 'Building2', 'CalendarDays', 'Cpu', 'CreditCard',
    'Edit', 'Globe', 'HardDrive', 'Monitor', 'Plus', 'Server',
    'Trash2', 'Wifi', 'X',
    // Icons used by constants.js (transitively imported via tabs, etc.)
    'DollarSign', 'Fingerprint', 'Gauge', 'Globe2', 'LayoutDashboard',
    'Link2', 'ListChecks', 'Mail', 'Rocket', 'ShieldAlert',
    'TerminalSquare', 'Tv',
  ];
  const mock = {};
  for (const name of names) {
    mock[name] = (props) => <div data-testid={`icon-${name}`} {...props} />;
  }
  return mock;
});

// Wrapper component that manages instances state via React useState,
// so that save/delete operations correctly update the rendered output.
function CostCenterWithState({ initialInstances = [], profiles = [] }) {
  const [instances, setInstances] = useState(initialInstances);
  return (
    <CostCenter
      profiles={profiles}
      instances={instances}
      setInstances={setInstances}
    />
  );
}

// Factory for test VPS instance data with sensible defaults.
const makeInstance = (overrides = {}) => ({
  id: 'inst-1',
  vpsName: 'LA CN2 Lite',
  providerName: 'Vultr',
  planName: 'Lite-One',
  os: 'Debian 12',
  cpu: 2,
  memory_gb: 2,
  disk_gb: 30,
  bandwidth_mbps: 500,
  traffic_gb: 1000,
  ipv4Count: 1,
  price: 12,
  currency: 'USD',
  billingCycle: 'monthly',
  purchaseDate: '2024-01-01',
  nextRenewal: '2099-12-01',
  manualRenewal: false,
  providerURL: '',
  profileId: '',
  host: '',
  notes: '',
  ...overrides,
});

describe('CostCenter', () => {
  beforeEach(() => {
    vi.clearAllMocks();
    // Default mock: save succeeds with a new id, delete succeeds silently
    window.go.main.App.SaveCostVPSInstance.mockResolvedValue({ ok: true, id: 'new-inst' });
    window.go.main.App.DeleteCostVPSInstance.mockResolvedValue(undefined);
  });

  // T-14: Renders empty state when no instances
  it('renders empty state when no instances exist', () => {
    render(<CostCenterWithState initialInstances={[]} />);

    expect(screen.getByText('还没有 VPS 记录')).toBeInTheDocument();
    expect(screen.getByText('添加你的第一台 VPS，开始追踪成本与续费日期')).toBeInTheDocument();
    expect(screen.getByText(/添加第一台 VPS/)).toBeInTheDocument();

    // Stat cards still render with zero values
    expect(screen.getByText('厂商数')).toBeInTheDocument();
    expect(screen.getByText('VPS 数')).toBeInTheDocument();
    expect(screen.getByText('本月支出')).toBeInTheDocument();
    expect(screen.getByText('预计年支出')).toBeInTheDocument();

    // Monthly and annual costs show dashes when no instances
    expect(screen.getAllByText('--').length).toBeGreaterThanOrEqual(2);
  });

  // T-14: Displays instance cards with correct data
  it('displays instance cards with correct data', () => {
    const inst = makeInstance();
    const { container } = render(<CostCenterWithState initialInstances={[inst]} />);

    // Instance name is visible (unique to the instance card when no expiry banner)
    expect(screen.getByText('LA CN2 Lite')).toBeInTheDocument();

    // Provider and plan appear in the instance card subtitle
    expect(screen.getByText(/Vultr · Lite-One/)).toBeInTheDocument();

    // Price with currency symbol and billing cycle label: $12.00/月
    expect(screen.getByText('$12.00/月')).toBeInTheDocument();

    // Spec chips render correctly
    expect(screen.getByText('2 核心')).toBeInTheDocument();      // CPU
    expect(screen.getByText('2.0 GB')).toBeInTheDocument();      // Memory
    expect(screen.getByText('30 GB')).toBeInTheDocument();       // Disk
    expect(screen.getByText('500 Mbps')).toBeInTheDocument();    // Bandwidth
    expect(screen.getByText('1000 GB/月')).toBeInTheDocument();  // Traffic
    expect(screen.getByText('1 IPv4')).toBeInTheDocument();      // IPv4

    // Edit and delete action buttons are present on the card
    expect(container.querySelector('.btn-icon-sm:not(.danger)')).toBeInTheDocument(); // Edit
    expect(container.querySelector('.btn-icon-sm.danger')).toBeInTheDocument();       // Delete
  });

  // T-14: Form validation (required fields, save button state)
  it('validates form inputs and toggles save button disabled state', () => {
    render(<CostCenterWithState initialInstances={[]} />);

    // Open the form via the empty-state CTA
    fireEvent.click(screen.getByText(/添加第一台 VPS/));

    // Form heading appears
    expect(screen.getByRole('heading', { name: '添加 VPS' })).toBeInTheDocument();

    // Save button is disabled when VPS name is empty (required field)
    const saveBtn = screen.getByText('保存').closest('button');
    expect(saveBtn).toBeDisabled();

    // Fill in the VPS name — save should become enabled
    const nameInput = screen.getByPlaceholderText('如：洛杉矶 CN2 轻量 A 型');
    fireEvent.change(nameInput, { target: { value: 'My VPS' } });
    expect(nameInput.value).toBe('My VPS');

    // Re-query after re-render to get updated disabled state
    const saveBtnAfter = screen.getByText('保存').closest('button');
    expect(saveBtnAfter).not.toBeDisabled();

    // Fill price input
    const priceInput = screen.getByPlaceholderText('0.00');
    fireEvent.change(priceInput, { target: { value: '9.99' } });
    expect(priceInput.value).toBe('9.99');

    // Change billing cycle from monthly to annual
    const cycleSelect = screen.getByDisplayValue('月付');
    fireEvent.change(cycleSelect, { target: { value: 'annual' } });
    // After change, the annual option should be selected
    expect(screen.getByDisplayValue('年付')).toBeInTheDocument();
  });

  // T-14: Submitting form calls SaveCostVPSInstance Wails binding
  it('calls SaveCostVPSInstance when form is submitted with valid data', async () => {
    render(<CostCenterWithState initialInstances={[]} />);

    // Open the form via the top action bar button
    fireEvent.click(screen.getByText('添加 VPS'));

    // Fill the required VPS name
    const nameInput = screen.getByPlaceholderText('如：洛杉矶 CN2 轻量 A 型');
    fireEvent.change(nameInput, { target: { value: 'Test VPS' } });

    // Fill the price
    const priceInput = screen.getByPlaceholderText('0.00');
    fireEvent.change(priceInput, { target: { value: '15.50' } });

    // Click save
    const saveBtn = screen.getByText('保存').closest('button');
    fireEvent.click(saveBtn);

    // Verify the Wails binding was called with correct payload
    await waitFor(() => {
      expect(window.go.main.App.SaveCostVPSInstance).toHaveBeenCalledTimes(1);
    });

    const payload = window.go.main.App.SaveCostVPSInstance.mock.calls[0][0];
    expect(payload.vpsName).toBe('Test VPS');
    expect(payload.price).toBe(15.5);
    expect(payload.currency).toBe('CNY');           // Default currency
    expect(payload.billingCycle).toBe('monthly');   // Default billing cycle
    expect(payload.cpu).toBe(2);                    // Default CPU from emptyDraft

    // After successful save, the new instance card appears in the list
    await waitFor(() => {
      expect(screen.getByText('Test VPS')).toBeInTheDocument();
    });
  });

  // T-14: Delete button calls DeleteCostVPSInstance Wails binding
  it('calls DeleteCostVPSInstance and removes the card on delete', async () => {
    const inst = makeInstance({ id: 'del-1', vpsName: 'Server To Delete' });
    const { container } = render(<CostCenterWithState initialInstances={[inst]} />);

    // Verify the instance is rendered before deletion
    expect(screen.getByText('Server To Delete')).toBeInTheDocument();

    // Click the delete button (identified by .danger class on the icon button)
    const deleteBtn = container.querySelector('.btn-icon-sm.danger');
    expect(deleteBtn).toBeInTheDocument();
    fireEvent.click(deleteBtn);

    // Verify the Wails binding was called with the correct instance id
    await waitFor(() => {
      expect(window.go.main.App.DeleteCostVPSInstance).toHaveBeenCalledWith('del-1');
    });

    // After deletion, the instance card should be removed from the DOM
    await waitFor(() => {
      expect(screen.queryByText('Server To Delete')).not.toBeInTheDocument();
    });

    // Empty state should now be visible
    expect(screen.getByText('还没有 VPS 记录')).toBeInTheDocument();
  });

  // T-14: Monthly cost calculation (price / billing_cycle_months)
  it('calculates monthly cost by dividing price by billing cycle months', () => {
    // Quarterly $12 -> monthly = 12 / 3 = $4.00, annual = $48.00
    const instances = [
      makeInstance({ id: 'q1', price: 12, currency: 'USD', billingCycle: 'quarterly' }),
    ];
    render(<CostCenterWithState initialInstances={instances} />);

    // Monthly cost stat: $12 / 3 months = $4.00
    // This text appears both as the stat card value and inside the annual card's sub label
    const monthlyMatches = screen.getAllByText('$4.00');
    expect(monthlyMatches.length).toBeGreaterThanOrEqual(1);

    // Annual cost stat: $4.00 * 12 = $48.00
    expect(screen.getByText('$48.00')).toBeInTheDocument();

    // Instance card shows original price with cycle label: $12.00/季
    expect(screen.getByText('$12.00/季')).toBeInTheDocument();
  });

  // T-14: Summary statistics (total instances, vendor count, monthly by currency)
  it('displays summary statistics grouped by vendor and currency', () => {
    const instances = [
      makeInstance({ id: 'a', providerName: 'Vultr', price: 10, currency: 'USD', billingCycle: 'monthly' }),
      makeInstance({ id: 'b', providerName: 'Vultr', price: 20, currency: 'USD', billingCycle: 'monthly' }),
      makeInstance({ id: 'c', providerName: 'RackNerd', price: 5, currency: 'USD', billingCycle: 'monthly' }),
    ];
    const { container } = render(<CostCenterWithState initialInstances={instances} />);

    // Action bar summary and stat card sub both contain "共 3 台 VPS"
    expect(screen.getAllByText(/共 3 台 VPS/).length).toBeGreaterThanOrEqual(2);
    expect(screen.getByText(/2 家厂商/)).toBeInTheDocument();

    // Vendor group names are displayed (sorted alphabetically: RackNerd, Vultr)
    const vendorNames = container.querySelectorAll('.vendor-group-name');
    expect(vendorNames.length).toBe(2);
    expect(vendorNames[0].textContent).toBe('RackNerd');
    expect(vendorNames[1].textContent).toBe('Vultr');

    // Vendor group headers show per-vendor instance counts and costs
    // (text is split across JSX text nodes, so check textContent directly)
    const vendorMetas = container.querySelectorAll('.vendor-group-meta');
    expect(vendorMetas.length).toBe(2);
    expect(vendorMetas[0].textContent).toContain('1');       // RackNerd: 1 instance
    expect(vendorMetas[0].textContent).toContain('$5.00');   // RackNerd: $5.00/mo
    expect(vendorMetas[1].textContent).toContain('2');       // Vultr: 2 instances
    expect(vendorMetas[1].textContent).toContain('$30.00');  // Vultr: $30.00/mo

    // Total monthly cost: (10 + 20 + 5) / 1 = $35.00
    expect(screen.getByText('$35.00')).toBeInTheDocument();
  });
});
