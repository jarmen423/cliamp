// The V2 protocol client of the cliamp remote gateway.
//
// This module touches no DOM, so a React Native app can import it as it is:
// it needs only WebSocket, fetch, setTimeout and setInterval, which both
// runtimes have. It opens two WebSockets to /v2, because the server streams
// events on a socket after a subscribe and reads no more requests from it:
//
//   requests  request/response lines, matched by id
//   events    one subscribe, then runtime.state and runtime.job events
//
// Each WebSocket message is one line of V2 JSON, the same as the Unix socket.

const TERMINAL = new Set(['succeeded', 'failed', 'canceled'])
const KEEPALIVE_MS = 25000 // the server closes a request socket idle for 60s
const JOB_POLL_MS = 2000 // fallback when the events socket misses a job
const REQUEST_TIMEOUT_MS = 10000 // a request with no answer means a dead socket
const DEFAULT_TIMEOUT_MS = 30000
const TOPICS = ['runtime.state', 'runtime.job']

// RemoteError carries the V2 error code, such as conflict or unavailable.
export class RemoteError extends Error {
  constructor(code, message) {
    super(message || code)
    this.code = code
  }
}

function v2Error(err) {
  return new RemoteError(err?.code || 'error', err?.message)
}

// wsURL turns the http(s) base of the gateway into its /v2 WebSocket URL.
export function wsURL(base) {
  return base.replace(/^http/, 'ws').replace(/\/+$/, '') + '/v2'
}

// Connection keeps both sockets open, reconnecting with backoff. Status is
// one of: connecting, open, unauthorized, closed.
export class Connection {
  constructor({ base, token, onStatus, onEvent, WebSocketImpl, fetchImpl }) {
    this.base = base.replace(/\/+$/, '')
    this.token = token
    this.onStatus = onStatus || (() => {})
    this.onEvent = onEvent || (() => {})
    this.WS = WebSocketImpl || globalThis.WebSocket
    this.fetch = fetchImpl || globalThis.fetch?.bind(globalThis)
    this.status = 'closed'
    this.nextID = 1
    this.pending = new Map() // request id -> {resolve, reject}
    this.jobs = new Map() // job id -> {resolve, reject, timer}
    this.finished = new Map() // job id -> job, for events that beat the response
    this.req = null
    this.evt = null
    this.backoff = 500
    this.failedOpens = 0
    this.stopped = true
  }

  start() {
    if (!this.stopped) return
    this.stopped = false
    this.setStatus('connecting')
    this.openRequests()
    this.openEvents()
  }

  stop() {
    this.stopped = true
    clearInterval(this.keepalive)
    clearTimeout(this.retry)
    clearTimeout(this.retryEvents)
    this.req?.close()
    this.evt?.close()
    this.req = this.evt = null
    this.failAll(new RemoteError('disconnected', 'Disconnected'))
    this.setStatus('closed')
  }

  setStatus(status) {
    if (this.status === status) return
    this.status = status
    this.onStatus(status)
  }

  socket() {
    // A browser cannot set headers on a WebSocket, so the token rides in a
    // subprotocol. The server answers with cliamp.v2.
    return new this.WS(wsURL(this.base), ['cliamp.v2', 'cliamp.token.' + this.token])
  }

  openRequests() {
    const ws = this.socket()
    this.req = ws
    let opened = false
    ws.onopen = () => {
      opened = true
      this.failedOpens = 0
      this.backoff = 500
      // After a reconnect the events socket may be as dead as the old
      // request socket was, so resubscribe; that also resends the state.
      if (this.everOpened) this.dropEvents()
      this.everOpened = true
      this.setStatus('open')
      clearInterval(this.keepalive)
      this.keepalive = setInterval(() => this.request('capabilities').catch(() => {}), KEEPALIVE_MS)
    }
    ws.onmessage = (e) => this.readLines(e.data, (msg) => this.onResponse(msg))
    ws.onclose = () => {
      if (this.req !== ws) return
      clearInterval(this.keepalive)
      this.req = null
      this.failAll(new RemoteError('disconnected', 'Disconnected'))
      this.reconnect(opened)
    }
  }

  // dropRequests abandons a request socket that stopped answering. A
  // half-open socket, such as after the phone slept or the network changed,
  // may never fire close, so the client reconnects without waiting for it.
  dropRequests() {
    const ws = this.req
    if (!ws) return
    ws.onclose = null
    ws.onmessage = null
    try { ws.close() } catch {}
    this.req = null
    clearInterval(this.keepalive)
    this.failAll(new RemoteError('disconnected', 'Connection lost'))
    this.reconnect(true)
  }

  dropEvents() {
    const ws = this.evt
    if (!ws) return
    ws.onclose = null
    ws.onmessage = null
    try { ws.close() } catch {}
    this.evt = null
    if (!this.stopped) this.openEvents()
  }

  openEvents() {
    const ws = this.socket()
    this.evt = ws
    ws.onopen = () => {
      ws.send(JSON.stringify({ version: 2, id: 'events', method: 'subscribe', topics: TOPICS }) + '\n')
    }
    ws.onmessage = (e) => this.readLines(e.data, (msg) => this.onEventLine(msg))
    ws.onclose = () => {
      if (this.evt !== ws) return
      this.evt = null
      if (!this.stopped) this.retryEvents = setTimeout(() => !this.stopped && this.openEvents(), this.backoff)
    }
  }

  async reconnect(opened) {
    if (this.stopped) return
    if (!opened) this.failedOpens++
    // A WebSocket hides the HTTP status of a refused upgrade, so after two
    // refusals ask /art, which answers 401 for a bad token and 404 otherwise.
    if (this.failedOpens >= 2 && (await this.tokenRejected())) {
      this.stop()
      this.setStatus('unauthorized')
      return
    }
    this.setStatus('connecting')
    this.retry = setTimeout(() => !this.stopped && this.openRequests(), this.backoff)
    this.backoff = Math.min(this.backoff * 2, 8000)
  }

  async tokenRejected() {
    if (!this.fetch) return false
    try {
      const res = await this.fetch(this.base + '/art/' + '0'.repeat(64) + '.png', {
        headers: { Authorization: 'Bearer ' + this.token },
      })
      return res.status === 401
    } catch {
      return false
    }
  }

  readLines(data, fn) {
    for (const line of String(data).split('\n')) {
      if (!line) continue
      try {
        fn(JSON.parse(line))
      } catch {
        // A malformed line is the server's bug; drop it and keep reading.
      }
    }
  }

  onResponse(msg) {
    const waiter = this.pending.get(msg.id)
    if (!waiter) return
    this.pending.delete(msg.id)
    if (msg.ok === false) waiter.reject(v2Error(msg.error))
    else waiter.resolve(msg)
  }

  onEventLine(msg) {
    if (msg.event === undefined) return // the subscribe acknowledgement
    if (msg.event === 'system.overflow') {
      // The server drops a slow subscriber. Resubscribing resends the
      // retained state, which resyncs the client.
      this.evt?.close()
      return
    }
    if (msg.event === 'runtime.job') {
      const job = msg.data?.job
      if (job && TERMINAL.has(job.state)) this.finishJob(job)
      return
    }
    this.onEvent(msg.event, msg.data, msg)
  }

  failAll(err) {
    for (const w of this.pending.values()) w.reject(err)
    this.pending.clear()
    for (const w of this.jobs.values()) {
      clearInterval(w.timer)
      w.reject(err)
    }
    this.jobs.clear()
  }

  // request sends one V2 method and resolves with the response envelope.
  request(method, fields = {}) {
    if (!this.req || this.req.readyState !== 1) {
      return Promise.reject(new RemoteError('disconnected', 'Not connected'))
    }
    const id = 'r' + this.nextID++
    const line = JSON.stringify({ version: 2, id, method, ...fields }) + '\n'
    return new Promise((resolve, reject) => {
      const timer = setTimeout(() => {
        if (!this.pending.has(id)) return
        this.pending.delete(id)
        reject(new RemoteError('timeout', 'cliamp did not answer'))
        this.dropRequests()
      }, REQUEST_TIMEOUT_MS)
      const done = (fn) => (v) => {
        clearTimeout(timer)
        fn(v)
      }
      this.pending.set(id, { resolve: done(resolve), reject: done(reject) })
      this.req.send(line)
    })
  }

  // submit runs an operation and resolves with its finished job: result and
  // snapshot. It rejects with the job error.
  async submit(operation, params = {}, { timeout = DEFAULT_TIMEOUT_MS } = {}) {
    const res = await this.request('operation.submit', { operation, params })
    const job = res.job
    if (!job) return { result: res.result, snapshot: res.snapshot }
    if (TERMINAL.has(job.state)) return settle(job)
    const early = this.finished.get(job.id)
    if (early) return settle(early)
    return new Promise((resolve, reject) => {
      const waiter = { resolve, reject, timer: null }
      const started = Date.now()
      waiter.timer = setInterval(async () => {
        if (Date.now() - started > timeout) {
          this.dropJob(job.id)
          reject(new RemoteError('timeout', 'The operation took too long'))
          return
        }
        try {
          const polled = await this.request('job.get', { job_id: job.id })
          if (polled.job && TERMINAL.has(polled.job.state)) this.finishJob(polled.job)
        } catch {
          // The next tick or the event finishes it.
        }
      }, JOB_POLL_MS)
      this.jobs.set(job.id, waiter)
    }).then(settle)
  }

  finishJob(job) {
    const waiter = this.jobs.get(job.id)
    if (!waiter) {
      this.finished.set(job.id, job)
      if (this.finished.size > 64) this.finished.delete(this.finished.keys().next().value)
      return
    }
    this.dropJob(job.id)
    waiter.resolve(job)
  }

  dropJob(id) {
    clearInterval(this.jobs.get(id)?.timer)
    this.jobs.delete(id)
  }
}

function settle(job) {
  if (job.state !== 'succeeded') {
    throw job.error ? v2Error(job.error) : new RemoteError(job.state, 'The operation was ' + job.state)
  }
  return { result: job.result || {}, snapshot: job.snapshot }
}
