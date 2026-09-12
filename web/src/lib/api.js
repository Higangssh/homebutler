const BASE = '';
const TOKEN_KEY = 'homebutler.token';

// UnauthorizedError separates "this dashboard needs a token" from every other
// failure a card can have, so one screen can answer it instead of each card
// reporting a 401 it cannot explain.
export class UnauthorizedError extends Error {
  constructor() {
    super('unauthorized');
    this.name = 'UnauthorizedError';
    this.status = 401;
  }
}

// The token is held here and attached by this page to requests it makes itself.
// Not a cookie: the browser attaches those to requests the page did not make,
// which becomes CSRF surface the moment anything here writes. Not the URL
// either — that survives in shell history, server logs and Referer, and a
// dashboard address is exactly the kind of thing that gets pasted to someone.
let memoryToken = '';
let unauthorizedHandler = null;

export function getToken() {
  if (memoryToken) return memoryToken;
  try {
    return localStorage.getItem(TOKEN_KEY) || '';
  } catch {
    // Private windows and blocked site data both throw on access.
    return '';
  }
}

export function setToken(token) {
  memoryToken = token;
  try {
    localStorage.setItem(TOKEN_KEY, token);
  } catch {
    // The token still applies for as long as this page is open; it just will
    // not survive a reload. That is better than refusing to accept it.
  }
}

export function clearToken() {
  memoryToken = '';
  try {
    localStorage.removeItem(TOKEN_KEY);
  } catch {
    // Nothing to undo: the in-memory copy is already gone.
  }
}

// onUnauthorized is how a 401 arriving mid-session — the server restarted under
// a different token — gets back to the one screen that can ask for a new one.
export function onUnauthorized(handler) {
  unauthorizedHandler = handler;
}

async function fetchJSON(path, opts = {}) {
  const headers = { ...(opts.headers || {}) };
  const token = getToken();
  if (token) headers.Authorization = `Bearer ${token}`;

  const res = await fetch(`${BASE}${path}`, { ...opts, headers });
  if (res.status === 401) {
    if (unauthorizedHandler) unauthorizedHandler();
    throw new UnauthorizedError();
  }
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

export function getProxmoxEndpoints() {
  return fetchJSON('/api/proxmox/endpoints');
}

export function getProxmoxStatus(endpoint) {
  const query = endpoint ? `?endpoint=${encodeURIComponent(endpoint)}` : '';
  return fetchJSON(`/api/proxmox/status${query}`);
}

export function getServerStatus(name) {
  return fetchJSON(`/api/servers/${encodeURIComponent(name)}/status`);
}

export function getVersion() {
  return fetchJSON('/api/version');
}

export function getConfig() {
  return fetchJSON('/api/config');
}

export function getWatch() {
  return fetchJSON('/api/watch');
}

export function getWatchIncidents(limit) {
  const query = limit ? `?limit=${encodeURIComponent(limit)}` : '';
  return fetchJSON(`/api/watch/incidents${query}`);
}

// Fetched one at a time: the list deliberately carries no logs, and an incident
// holds two hundred lines of them.
export function getWatchIncident(id) {
  return fetchJSON(`/api/watch/incidents/${encodeURIComponent(id)}`);
}
