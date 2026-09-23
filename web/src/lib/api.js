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

// Servers and Proxmox endpoints. A password or a token is sent the way a
// notification credential is: not at all, a new value, or an explicit clear.
export function saveServers(revision, servers) {
  return fetchJSON('/api/config/servers', {
    method: 'PUT',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ revision, servers }),
  });
}

export function saveProxmox(revision, endpoints) {
  return fetchJSON('/api/config/proxmox', {
    method: 'PUT',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ revision, endpoints }),
  });
}

// The comparison, without saving one: a dashboard that polls must not turn
// into a snapshot every fifteen seconds and prune the baseline somebody
// wanted.
export function getReport() {
  return fetchJSON('/api/report');
}

// Saving is its own request, and a write like any other.
export function saveSnapshot() {
  return fetchJSON('/api/report/snapshot', { method: 'POST' });
}

export function getDoctor() {
  return fetchJSON('/api/doctor');
}

// Actions.
//
// Every one of these is a POST the server gates on a protection tier, and the
// tier is the reason the calls do not share one helper: an action that runs on
// the click that asked for it and an action that needs the target's name typed
// back are not the same interaction with a flag set.
//
// A dashboard with no token does not get a 401 from these — the routes are not
// registered at all, so the answer is 404. That is deliberate on the server
// side, and it means the screen has to know from /api/config whether it can
// act, rather than finding out by calling and reading a status code.

// Restart runs on the click. The container comes back, so a second question
// would be asking about something that undoes itself.
export function restartContainer(name, server) {
  return fetchJSON(withServer(`/api/docker/${encodeURIComponent(name)}/restart`, server), {
    method: 'POST',
  });
}

// Stop takes an explicit confirm, because nothing here starts it again: there
// is no start tool, and the service is down until somebody goes and does it.
export function stopContainer(name, server) {
  return fetchJSON(withServer(`/api/docker/${encodeURIComponent(name)}/stop`, server), {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ confirm: true }),
  });
}

// The watch list. All three run on the click that asks for them: a target
// removed by mistake is a target added back, and a check that was not needed
// costs one poll.
export function addWatchTarget(target) {
  return fetchJSON('/api/watch/targets', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(target),
  });
}

export function removeWatchTarget(name) {
  return fetchJSON(`/api/watch/targets/${encodeURIComponent(name)}/remove`, { method: 'POST' });
}

// Polls every target once now, rather than waiting for the service's next
// round. It is a write because it can record an incident and send a message.
export function checkWatchTargets() {
  return fetchJSON('/api/watch/check', { method: 'POST' });
}

// The app catalogue. install_list is a static map compiled into the binary, so
// this is the same list install_app will accept.
export function getInstallable() {
  return fetchJSON('/api/install');
}

export function getInstallStatus(app) {
  return fetchJSON(`/api/install/${encodeURIComponent(app)}`);
}

// Installing answers 200 with status "failed" and a list of issues when the
// pre-flight refuses — a taken port is an answer, not an error, and the
// operator changes the port and asks again. Reading it as a failure would put
// that sentence in a toast instead of beside the field that fixes it.
export function installApp(app, port) {
  return fetchJSON(`/api/install/${encodeURIComponent(app)}`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(port ? { port } : {}),
  });
}

// Stops the app and leaves its data. install_purge is the one that does not.
export function uninstallApp(app) {
  return fetchJSON(`/api/install/${encodeURIComponent(app)}/uninstall`, { method: 'POST' });
}

// The third tier: the app's own name, echoed back. A click cannot say which
// thing the operator meant to lose, and this one does not come back.
export function purgeApp(app) {
  return fetchJSON(`/api/install/${encodeURIComponent(app)}/purge`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ confirm_name: app }),
  });
}
