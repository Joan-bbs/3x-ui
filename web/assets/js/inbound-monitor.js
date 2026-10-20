// Vue 2 options shared by the monitor page and its polling lifecycle tests.
function createInboundMonitorOptions(labels) {
    return {
        el: '#app',
        delimiters: ['[[', ']]'],
        mixins: [MediaQueryMixin],
        data() {
            return {
                themeSwitcher,
                rows: [],
                search: '',
                running: false,
                statsEnabled: true,
                ready: false,
                sampledAt: 0,
                paused: false,
                failed: false,
                fetched: false,
                loading: false,
                timer: null,
                request: null,
                generation: 0,
                disposed: false,
                allColumns: [
                    { title: labels.remark, dataIndex: 'remark', scopedSlots: { customRender: 'remark' } },
                    { title: labels.port, dataIndex: 'port', sorter: (a, b) => a.port - b.port },
                    { title: labels.listen, dataIndex: 'listen', scopedSlots: { customRender: 'listen' } },
                    { title: labels.protocol, dataIndex: 'protocol' },
                    { title: labels.status, key: 'status', scopedSlots: { customRender: 'status' } },
                    { title: labels.upload, dataIndex: 'up', sorter: (a, b) => a.up - b.up, scopedSlots: { customRender: 'speed' } },
                    { title: labels.download, dataIndex: 'down', sorter: (a, b) => a.down - b.down, scopedSlots: { customRender: 'speed' } },
                ],
            };
        },
        computed: {
            columns() {
                if (!this.isMobile) return this.allColumns;
                // Keep the port and both speeds visible first on narrow screens.
                return [1, 5, 6, 0, 4, 2, 3].map((index, position) => ({
                    ...this.allColumns[index], width: position === 0 ? 70 : 125,
                }));
            },
            filteredRows() {
                const query = this.search.trim().toLowerCase();
                return this.rows.filter(row => [row.remark, row.port, row.listen, row.protocol]
                    .some(value => String(value || '').toLowerCase().includes(query)));
            },
            live() {
                return !this.paused && !document.hidden && !this.failed && this.running && this.statsEnabled;
            },
            totalUp() {
                return this.rows.reduce((total, row) => total + (row.enable && row.ready ? row.up : 0), 0);
            },
            totalDown() {
                return this.rows.reduce((total, row) => total + (row.enable && row.ready ? row.down : 0), 0);
            },
        },
        methods: {
            formatSpeed(value) {
                if (!Number.isFinite(value) || value <= 0) return '0 B/s';
                const units = ['B/s', 'KB/s', 'MB/s', 'GB/s', 'TB/s'];
                const index = Math.min(Math.floor(Math.log(value) / Math.log(1024)), units.length - 1);
                return `${(value / Math.pow(1024, index)).toFixed(index ? 2 : 0)} ${units[index]}`;
            },
            stopRequest() {
                this.generation++;
                clearTimeout(this.timer);
                this.timer = null;
                if (this.request) this.request.abort();
                this.request = null;
                this.loading = false;
            },
            togglePaused() {
                this.paused = !this.paused;
                this.stopRequest();
                if (!this.paused) {
                    this.ready = false;
                    this.refresh();
                }
            },
            visibilityChanged() {
                this.stopRequest();
                if (!document.hidden && !this.paused) {
                    this.ready = false;
                    this.refresh();
                }
            },
            async refresh() {
                if (this.disposed || this.paused || document.hidden || this.request) return;
                clearTimeout(this.timer);
                const generation = ++this.generation;
                const request = new AbortController();
                this.request = request;
                this.loading = true;
                try {
                    const response = await axios.get('/panel/api/inbounds/speeds', {
                        signal: request.signal,
                        timeout: 15000,
                    });
                    if (generation !== this.generation || this.disposed) return;
                    if (!response.data || !response.data.success || !response.data.obj) throw new Error('Invalid speed response');
                    const snapshot = response.data.obj;
                    this.rows = snapshot.inbounds || [];
                    this.running = snapshot.running;
                    this.statsEnabled = snapshot.statsEnabled;
                    this.ready = snapshot.ready;
                    this.sampledAt = snapshot.sampledAt;
                    this.failed = false;
                    this.fetched = true;
                } catch (error) {
                    if (generation !== this.generation || this.disposed) return;
                    this.failed = true;
                    this.ready = false;
                    this.rows = [];
                } finally {
                    if (generation === this.generation && !this.disposed) {
                        this.request = null;
                        this.loading = false;
                        this.timer = setTimeout(() => this.refresh(), 2000);
                    }
                }
            },
        },
        mounted() {
            document.addEventListener('visibilitychange', this.visibilityChanged);
            this.refresh();
        },
        beforeDestroy() {
            this.disposed = true;
            this.stopRequest();
            document.removeEventListener('visibilitychange', this.visibilityChanged);
        },
    };
}
