// Page-wide HTMX 4 behaviour shared by every template that loads htmx.
//
// HTMX 4 swaps every response except 204/304, including 4xx/5xx. Handlers that
// answer HTMX requests with an HTML fragment (form errors, login/reset errors,
// settings messages) rely on that. Many settings and API endpoints answer with
// JSON instead, and the auth middleware answers expired sessions with a JSON
// 401 plus HX-Redirect. None of that JSON (or plain-text error pages) is meant
// to land in the page, so only text/html responses are allowed to swap.
document.addEventListener('htmx:before:swap', function(event) {
    const response = event.detail.ctx && event.detail.ctx.response;
    if (!response || !response.headers) return;

    const type = (response.headers.get('Content-Type') || '').toLowerCase();
    if (type.includes('text/html')) return;
    if (type !== '' || response.status >= 400) {
        event.preventDefault();
    }
});

// JSON endpoints use swap:none (or the guard above). Failures still need
// visible feedback. Treat messages as text, never as HTML supplied by an API.
function showRequestError(message) {
    var alert = document.getElementById('request-error');
    if (!alert) {
        alert = document.createElement('div');
        alert.id = 'request-error';
        alert.setAttribute('role', 'alert');
        alert.className = 'fixed bottom-4 left-4 right-4 z-[100] rounded-lg border border-red-300 bg-red-50 p-4 text-red-800 shadow-lg dark:border-red-800 dark:bg-gray-900 dark:text-red-300';
        document.body.appendChild(alert);
    }
    alert.textContent = message;
}

document.addEventListener('htmx:before:request', function() {
    var alert = document.getElementById('request-error');
    if (alert) alert.remove();
});

document.addEventListener('htmx:after:request', function(event) {
    var ctx = event.detail && event.detail.ctx;
    if (!ctx || !ctx.response || ctx.response.status < 400) return;
    if (ctx.response.headers.get('HX-Redirect')) return;
    var type = (ctx.response.headers.get('Content-Type') || '').toLowerCase();
    if (type.includes('text/html')) return; // Rendered beside its form.
    var message = 'Unable to complete this request. Please try again.';
    if (ctx.response.status < 500 && type.includes('application/json')) {
        try {
            var data = JSON.parse(ctx.text);
            if (typeof data.error === 'string' && data.error) message = data.error;
        } catch (error) { /* Keep the fallback for malformed error responses. */ }
    }
    showRequestError(message);
});

// Helpers for page scripts that react to HTMX 4 request events.
window.subtrackrHtmx = {
    // Path of the request behind an htmx event, e.g. "/api/settings/base-url".
    requestPath: function(event) {
        const ctx = event.detail && event.detail.ctx;
        const action = ctx && ctx.request && ctx.request.action;
        if (!action) return null;
        try {
            return new URL(action, window.location.origin).pathname;
        } catch (error) {
            return null;
        }
    },

    // Parsed JSON body of a successful (2xx) response, or null.
    responseJSON: function(event) {
        const ctx = event.detail && event.detail.ctx;
        if (!ctx || !ctx.response || ctx.response.status < 200 || ctx.response.status >= 300) return null;
        try {
            return JSON.parse(ctx.text);
        } catch (error) {
            return null;
        }
    }
};
