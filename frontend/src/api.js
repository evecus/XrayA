const BASE = ''

let _token = localStorage.getItem('xraya_token') || ''
let _onUnauthorized = null

export function setToken(t) {
  _token = t
  if (t) localStorage.setItem('xraya_token', t)
  else localStorage.removeItem('xraya_token')
}

export function getToken() { return _token }

export function onUnauthorized(fn) { _onUnauthorized = fn }

async function request(method, path, body) {
  const headers = { 'Content-Type': 'application/json' }
  if (_token) headers['X-Session-Token'] = _token

  const res = await fetch(BASE + '/api' + path, {
    method,
    headers,
    body: body !== undefined ? JSON.stringify(body) : undefined,
  })

  if (res.status === 401) {
    _onUnauthorized?.()
    throw new Error('unauthorized')
  }

  const data = await res.json()
  if (!data.ok) throw new Error(data.error || 'unknown error')
  return data
}

// Auth
export const authStatus  = ()       => request('GET',  '/auth/status')
export const login       = (pw)     => request('POST', '/login',         { password: pw })
export const logout      = ()       => request('POST', '/logout')
export const setPassword = (pw)     => request('POST', '/auth/password', { password: pw })

// Nodes
export const listNodes   = ()       => request('GET',    '/nodes')
export const deleteNode  = (id)     => request('DELETE', `/nodes/${id}`)
export const importLinks = (links)  => request('POST',   '/nodes/import', { links })

// Subscriptions
export const listSubs    = ()       => request('GET',    '/subscriptions')
export const addSub      = (s)      => request('POST',   '/subscriptions',           s)
export const deleteSub   = (id)     => request('DELETE', `/subscriptions/${id}`)
export const updateSub   = (id)     => request('POST',   `/subscriptions/${id}/update`)
export const editSub     = (id, s)  => request('PUT',    `/subscriptions/${id}`,     s)

// Core
export const connect     = (nodeId) => request('POST', '/connect',    { nodeId })
export const disconnect  = ()       => request('POST', '/disconnect')
export const getStatus   = ()       => request('GET',  '/status')
export const getLogs     = ()       => request('GET',  '/logs')

// Settings
export const getSettings = ()       => request('GET', '/settings')
export const putSettings = (s)      => request('PUT', '/settings', s)
