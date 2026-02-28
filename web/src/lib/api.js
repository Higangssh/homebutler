const BASE = '';

async function fetchJSON(path, opts) {
  const res = await fetch(`${BASE}${path}`, opts);
  if (!res.ok) {
    const body = await res.text();
    throw new Error(body || res.statusText);
  }
  return res.json();
}

function withServer(path, server) {
  if (server) return `${path}?server=${encodeURIComponent(server)}`;
  return path;
}

export function getStatus(server) {
  return fetchJSON(withServer('/api/status', server));
}

export function getDocker(server) {
  return fetchJSON(withServer('/api/docker', server));
}

export function getProcesses(server) {
  return fetchJSON(withServer('/api/processes', server));
}

export function getAlerts(server) {
  return fetchJSON(withServer('/api/alerts', server));
}

export function getPorts(server) {
  return fetchJSON(withServer('/api/ports', server));
}

export function getWake() {
  return fetchJSON('/api/wake');
}

export function postWake(name) {
  return fetchJSON(`/api/wake/${encodeURIComponent(name)}`, { method: 'POST' });
}

export function getServers() {
  return fetchJSON('/api/servers');
}

export function getServerStatus(name) {
  return fetchJSON(`/api/servers/${encodeURIComponent(name)}/status`);
}

export function getVersion() {
  return fetchJSON('/api/version');
}
