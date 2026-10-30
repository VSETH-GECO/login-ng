(function () {
    // Without fetch/AbortController the arrival cannot be checked, so the
    // cautious #requested message stays visible.
    if (!window.fetch || !window.AbortController) { return; }

    var panels = {
        requested: document.getElementById('requested'),
        pending: document.getElementById('pending'),
        done: document.getElementById('done'),
        timeout: document.getElementById('timeout'),
        expired: document.getElementById('expired'),
        failed: document.getElementById('failed')
    };
    var statusURL = panels.pending.getAttribute('data-status-url');

    var POLL_INTERVAL_MS = 2000;
    var REQUEST_TIMEOUT_MS = 4000;
    var GIVE_UP_AFTER_MS = 5 * 60 * 1000;
    var MAX_SERVER_ERRORS = 5; // in a row, about 10 seconds
    var startedAt = Date.now();
    var serverErrors = 0;

    function show(name) {
        for (var key in panels) {
            panels[key].classList.toggle('d-none', key !== name);
        }
    }

    function schedule() {
        if (Date.now() - startedAt > GIVE_UP_AFTER_MS) {
            show('timeout');
            return;
        }
        setTimeout(poll, POLL_INTERVAL_MS);
    }

    // Network errors and timeouts are expected while the port is bounced (the
    // client has no network at all) and just mean "not there yet". Answers from
    // the server are different: they are definitive or point to a real problem.
    function handle(resp) {
        if (resp.status === 401) { show('expired'); return; }
        if (resp.status === 404) {
            // no pending switch in this session, so nothing can be verified
            show('requested');
            return;
        }
        if (resp.status >= 500) {
            // proxies may answer 502/503/504 for a moment while the network flaps
            serverErrors++;
            if (serverErrors >= MAX_SERVER_ERRORS) { show('failed'); } else { schedule(); }
            return;
        }
        if (!resp.ok) { schedule(); return; }
        return resp.json().then(function (body) {
            serverErrors = 0;
            if (body.connected) { show('done'); } else { schedule(); }
        }, function () {
            // not JSON, e.g. a captive portal page instead of our answer
            schedule();
        });
    }

    function poll() {
        var controller = new AbortController();
        var abort = setTimeout(function () { controller.abort(); }, REQUEST_TIMEOUT_MS);
        fetch(statusURL, { cache: 'no-store', credentials: 'same-origin', signal: controller.signal })
            .then(function (resp) {
                clearTimeout(abort);
                return handle(resp);
            }, function () {
                clearTimeout(abort);
                serverErrors = 0;
                schedule();
            });
    }

    show('pending');
    setTimeout(poll, POLL_INTERVAL_MS);
})();
