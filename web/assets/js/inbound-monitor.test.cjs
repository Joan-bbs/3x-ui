// Run with: node --test web/assets/js/inbound-monitor.test.cjs
const { test } = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const vm = require('node:vm');

function mount() {
    const pending = [];
    const timers = new Map();
    const document = { hidden: false, addEventListener() {}, removeEventListener() {} };
    const context = vm.createContext({
        document, AbortController, themeSwitcher: {}, MediaQueryMixin: {},
        setTimeout(fn) { const id = Symbol(); timers.set(id, fn); return id; },
        clearTimeout(id) { timers.delete(id); },
        axios: { get(url, options) {
            assert.equal(url, '/panel/api/inbounds/speeds');
            return new Promise((resolve, reject) => pending.push({ resolve, reject, options }));
        } },
    });
    vm.runInContext(fs.readFileSync(__dirname + '/inbound-monitor.js', 'utf8'), context);
    const options = context.createInboundMonitorOptions({});
    const state = options.data();
    for (const [name, method] of Object.entries(options.methods)) state[name] = method.bind(state);
    for (const [name, method] of Object.entries(options.computed)) Object.defineProperty(state, name, { get: method.bind(state) });
    return { state, options, pending, timers, document };
}

const response = (extra = {}) => ({ data: { success: true, obj: {
    inbounds: [{ id: 1, port: 443, remark: 'TLS', protocol: 'vless', up: 2048, down: 1024, enable: true, ready: true }],
    running: true, statsEnabled: true, ready: true, sampledAt: 1000, ...extra,
} } });

test('format, search, totals and request deduplication', async () => {
    const { state, pending, timers } = mount();
    const work = state.refresh();
    await state.refresh();
    assert.equal(pending.length, 1);
    pending[0].resolve(response());
    await work;
    assert.equal(state.formatSpeed(state.totalUp), '2.00 KB/s');
    assert.equal(state.totalDown, 1024);
    assert.equal(state.formatSpeed(0), '0 B/s');
    state.search = '443';
    assert.equal(state.filteredRows.length, 1);
    state.search = 'no match';
    assert.equal(state.filteredRows.length, 0);
    assert.equal(state.totalUp, 2048);
    assert.equal(timers.size, 1);
});

test('pause aborts in-flight requests and ignores late results', async () => {
    const { state, pending, timers } = mount();
    const work = state.refresh();
    state.togglePaused();
    assert.equal(pending[0].options.signal.aborted, true);
    pending[0].resolve(response());
    await work;
    assert.equal(state.rows.length, 0);
    assert.equal(state.live, false);
    assert.equal(timers.size, 0);
});

test('background tabs stop requests; returning resumes and disposal cancels', async () => {
    const { state, options, pending, timers, document } = mount();
    document.hidden = true;
    state.visibilityChanged();
    await state.refresh();
    assert.equal(pending.length, 0);
    document.hidden = false;
    state.visibilityChanged();
    assert.equal(pending.length, 1);
    options.beforeDestroy.call(state);
    assert.equal(pending[0].options.signal.aborted, true);
    pending[0].resolve(response());
    await new Promise(resolve => setImmediate(resolve));
    assert.equal(state.rows.length, 0);
    assert.equal(timers.size, 0);
});

test('errors clear stale speeds and schedule one retry; stopped core has no live speed', async () => {
    const { state, pending, timers } = mount();
    let work = state.refresh();
    pending[0].resolve(response());
    await work;
    work = state.refresh();
    pending[1].reject(new Error('network unavailable'));
    await work;
    assert.equal(state.failed, true);
    assert.equal(state.ready, false);
    assert.equal(state.rows.length, 0);
    assert.equal(timers.size, 1);
    work = state.refresh();
    pending[2].resolve(response({ running: false, ready: false, sampledAt: 0 }));
    await work;
    assert.equal(state.failed, false);
    assert.equal(state.fetched, true);
    assert.equal(state.live, false);
});
