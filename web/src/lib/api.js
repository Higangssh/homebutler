const BASE = '';
const TOKEN_KEY = 'homebutler.token';

// UnauthorizedError separates "this dashboard needs a token" from every other
// failure a card can have, so one screen can answer it instead of each card
// reporting a 401 it cannot explain.
// StaleConfigError means somebody edited the config file while this page had
// it open. Nothing about the request was wrong, so the page reloads rather
// than asking the person to try again.
export class StaleConfigError extends Error {
  constructor(message) {
    super(message);
    this.name = 'StaleConfigError';
    this.status = 409;
  }
}

// errorText pulls the server's sentence out of its JSON, falling back to the
// body when it is not JSON.
function errorText(body) {
  try {
    const parsed = JSON.parse(body);
    if (parsed && parsed.error) return parsed.error;
  } catch {
    // not JSON; the body is the message
  }
  return body;
}

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
  if (res.status === 409) {
    // The file moved on under us. Distinguished by type for the same reason
    // 401 is: the answer is to reload, not to change what was sent.
    const body = await res.text();
    throw new StaleConfigError(errorText(body));
  }
  if (!res.ok) {
    const body = await res.text();
    throw new Error(errorText(body) || res.statusText);
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

// One request for every server, rather than the list and then a round trip per
// server. Each reading carries its own freshness, so a machine that did not
// answer is labelled rather than missing.
export function getOverview() {
  return fetchJSON('/api/overview');
}

// Saves carry the revision the page was built from. The server refuses a save
// made against a file that has changed since, which is how an edit made in an
// editor while the dashboard was open survives.
export function saveAlerts(revision, thresholds) {
  return fetchJSON('/api/config/alerts', {
    method: 'PUT',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ revision, ...thresholds }),
  });
}

export function saveNotify(revision, channels) {
  return fetchJSON('/api/config/notify', {
    method: 'PUT',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ revision, channels }),
  });
}

// Sends one real message through every configured channel. It is a write — it
// leaves the machine — so it needs the token like every other write here.
export function testNotify() {
  return fetchJSON('/api/notify/test', { method: 'POST' });
}

// Wake targets travel in both directions: a MAC address is on the network
// already, so unlike a notification token there is nothing here to withhold.
export function saveWake(revision, targets) {
  return fetchJSON('/api/config/wake', {
    method: 'PUT',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ revision, targets }),
  });
}
