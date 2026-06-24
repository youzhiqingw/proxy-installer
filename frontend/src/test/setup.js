import '@testing-library/jest-dom'

// Mock Wails bindings
window.go = {
  main: {
    App: {}
  }
}

// Mock window.go.main.App methods used in tests
const mockBackend = {
  GetAppState: vi.fn().mockResolvedValue('{}'),
  SaveAppState: vi.fn().mockResolvedValue(null),
  TestConnection: vi.fn().mockResolvedValue(null),
  StartDeploy: vi.fn().mockResolvedValue(null),
  SaveCostVPSInstance: vi.fn().mockResolvedValue(null),
  DeleteCostVPSInstance: vi.fn().mockResolvedValue(null),
  MeasureLatency: vi.fn().mockResolvedValue(null),
  RunSpeedTest: vi.fn().mockResolvedValue(null),
  RunIPQuality: vi.fn().mockResolvedValue(null),
  InspectVPS: vi.fn().mockResolvedValue(null),
  GetCostV2Instances: vi.fn().mockResolvedValue([]),
  GetCostV2Summary: vi.fn().mockResolvedValue({ Vendors: 0, Total: 0, Monthly: {} }),
}

window.go.main.App = mockBackend

// Expose mock for tests
window.__mockBackend = mockBackend
