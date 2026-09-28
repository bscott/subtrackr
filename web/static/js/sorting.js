// Subscription sorting state and preference persistence.
const SORT_STORAGE_KEY = 'subtrackr-sort';
const VALID_SORT_FIELDS = ['name', 'category', 'cost', 'schedule', 'status', 'renewal_date', 'created_at'];
const VALID_SORT_ORDERS = ['asc', 'desc'];
let pendingSortSpec = null;
let latestSortRequestID = 0;

function normalizeSortRules(rules) {
    const seen = new Set();
    return rules.filter(rule => {
        if (!VALID_SORT_FIELDS.includes(rule.field) || !VALID_SORT_ORDERS.includes(rule.direction) || seen.has(rule.field)) {
            return false;
        }
        seen.add(rule.field);
        return true;
    });
}

function parseSortSpec(spec) {
    if (!spec) return [];
    const rules = spec.split(',').map(item => {
        const [field, direction, ...extra] = item.split(':');
        return extra.length === 0 ? { field, direction } : null;
    }).filter(Boolean);
    return normalizeSortRules(rules);
}

function serializeSortRules(rules) {
    return normalizeSortRules(rules).map(rule => `${rule.field}:${rule.direction}`).join(',');
}

function getSortPreference() {
    const stored = localStorage.getItem(SORT_STORAGE_KEY);
    if (!stored) return null;

    try {
        const preference = JSON.parse(stored);
        if (preference && typeof preference.sortSpec === 'string') {
            return serializeSortRules(parseSortSpec(preference.sortSpec));
        }

        // Migrate preferences saved by the earlier status-aware sorting UI.
        if (preference && VALID_SORT_FIELDS.includes(preference.sortBy) && VALID_SORT_ORDERS.includes(preference.order)) {
            const rules = [];
            if (preference.sortBy !== 'status' && VALID_SORT_ORDERS.includes(preference.statusOrder)) {
                rules.push({ field: 'status', direction: preference.statusOrder });
            }
            rules.push({ field: preference.sortBy, direction: preference.order });
            return serializeSortRules(rules);
        }
    } catch (error) {
        console.error('Failed to parse sort preference:', error);
    }
    return null;
}

function saveSortPreference(sortSpec) {
    localStorage.setItem(SORT_STORAGE_KEY, JSON.stringify({ sortSpec }));
}

function updateSortIndicators() {
    const list = document.getElementById('subscription-list');
    if (!list) return;

    const rules = parseSortSpec(list.dataset.sortSpec || '');
    list.querySelectorAll('[data-sort-field]').forEach(button => {
        const field = button.dataset.sortField;
        const index = rules.findIndex(rule => rule.field === field);
        const rule = index >= 0 ? rules[index] : null;
        const arrow = button.querySelector('[data-sort-arrow]');
        const priority = button.querySelector('[data-sort-priority]');

        button.setAttribute('aria-pressed', rule ? 'true' : 'false');
        if (arrow) {
            arrow.style.opacity = rule ? '1' : '0.3';
            arrow.style.transform = rule && rule.direction === 'asc' ? 'rotate(180deg)' : '';
        }
        if (priority) {
            priority.textContent = rule ? String(index + 1) : '';
            priority.classList.toggle('hidden', !rule);
        }
    });
}

function applySavedSortPreference() {
    const preference = getSortPreference();
    if (preference === null) return;

    const currentUrl = new URL(window.location.href);
    if (currentUrl.searchParams.has('sort') || currentUrl.searchParams.has('sorts')) return;
    if (!preference || typeof htmx === 'undefined') return;

    requestSortUpdate(preference);
}

function requestSortUpdate(sortSpec) {
    pendingSortSpec = sortSpec;
    const requestID = ++latestSortRequestID;
    htmx.ajax('GET', `/api/subscriptions?sorts=${encodeURIComponent(sortSpec)}&sort_request=${requestID}`, {
        target: '#subscription-list',
        swap: 'outerHTML'
    });
}

document.addEventListener('click', function(event) {
    const button = event.target.closest('[data-sort-field]');
    if (!button) return;

    const list = document.getElementById('subscription-list');
    if (!list || typeof htmx === 'undefined') return;

    event.preventDefault();
    const rules = parseSortSpec(pendingSortSpec === null ? list.dataset.sortSpec || '' : pendingSortSpec);
    const index = rules.findIndex(rule => rule.field === button.dataset.sortField);
    if (index < 0) {
        rules.push({ field: button.dataset.sortField, direction: 'asc' });
    } else if (rules[index].direction === 'asc') {
        rules[index].direction = 'desc';
    } else {
        rules.splice(index, 1);
    }

    const sortSpec = serializeSortRules(rules);
    requestSortUpdate(sortSpec);
});

function getSortRequestURL(detail) {
    const path = (detail.xhr && detail.xhr.responseURL) || (detail.requestConfig && detail.requestConfig.path);
    if (!path) return null;
    try {
        return new URL(path, window.location.origin);
    } catch (error) {
        return null;
    }
}

document.addEventListener('htmx:beforeSwap', function(event) {
    const url = getSortRequestURL(event.detail);
    if (!url || url.pathname !== '/api/subscriptions' || !url.searchParams.has('sorts')) return;

    const requestID = Number(url.searchParams.get('sort_request'));
    const responseSpec = serializeSortRules(parseSortSpec(url.searchParams.get('sorts') || ''));
    if (!Number.isInteger(requestID) || requestID !== latestSortRequestID || responseSpec !== pendingSortSpec) {
        event.detail.shouldSwap = false;
        return;
    }
    pendingSortSpec = null;
});

document.addEventListener('htmx:afterRequest', function(event) {
    if (!event.detail.failed) return;
    const url = getSortRequestURL(event.detail);
    if (url && url.pathname === '/api/subscriptions' && url.searchParams.has('sorts') && Number(url.searchParams.get('sort_request')) === latestSortRequestID) {
        pendingSortSpec = null;
    }
});

document.addEventListener('htmx:configRequest', function(event) {
    const path = event.detail.path;
    if (!path || !path.includes('/api/subscriptions')) return;

    try {
        const url = new URL(path, window.location.origin);
        if (url.searchParams.has('sorts')) {
            saveSortPreference(serializeSortRules(parseSortSpec(url.searchParams.get('sorts') || '')));
        }
    } catch (error) {
        console.error('Failed to extract sort preference:', error);
    }
});

document.addEventListener('DOMContentLoaded', function() {
    updateSortIndicators();
    applySavedSortPreference();
});

document.addEventListener('htmx:afterSwap', updateSortIndicators);
