export function callBackend(fn, ...args) {
  if (!window.go?.main?.App) {
    return Promise.reject(new Error('桌面后端未连接，请打开 exe 使用'));
  }
  return fn(...args);
}

export function wait(ms) {
  return new Promise((resolve) => setTimeout(resolve, ms));
}

export function patchProfile(current, id, patch) {
  return current.map((item) => item.id === id ? { ...item, ...patch } : item);
}
