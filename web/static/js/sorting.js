// SubTrackr Sort Preference Persistence
// Saves and restores user's sort preference using localStorage

const SORT_STORAGE_KEY = 'subtrackr-sort';
const VALID_SORT_FIELDS = ['name', 'cost', 'renewal_date', 'status', 'category', 'schedule', 'created_at'];
const VALID_SORT_ORDERS = ['asc', 'desc'];

// Validate sort parameters
function isValidSortPreference(sortBy, order) {
    return VALID_SORT_FIELDS.includes(sortBy) && VALID_SORT_ORDERS.includes(order);
}

// Save sort preference to localStorage
function saveSortPreference(sortBy, order) {
    if (!isValidSortPreference(sortBy, order)) return;
    const preference = { sortBy, order };
    localStorage.setItem(SORT_STORAGE_KEY, JSON.stringify(preference));
}

// Get saved sort preference
function getSortPreference() {
    const stored = localStorage.getItem(SORT_STORAGE_KEY);
    if (stored) {
        try {
            return JSON.parse(stored);
        } catch (e) {
            console.error('Failed to parse sort preference:', e);
            return null;
        }
    }
    return null;
}

// Extract sort params from URL
function extractSortParams(url) {
    try {
        const urlObj = new URL(url, window.location.origin);
        const sortBy = urlObj.searchParams.get('sort');
        const order = urlObj.searchParams.get('order');
        if (sortBy && order) {
            return { sortBy, order };
        }
    } catch (e) {
        console.error('Failed to extract sort params:', e);
    }
    return null;
}

// Apply saved sort preference on page load
function applySavedSortPreference() {
    const preference = getSortPreference();
    if (!preference) return;

    const subscriptionList = document.getElementById('subscription-list');
    if (!subscriptionList) return;

    // Check if we're on the subscriptions page and not already sorted
    const currentUrl = new URL(window.location.href);
    const currentSort = currentUrl.searchParams.get('sort');

    // Validate preference before using
    if (!isValidSortPreference(preference.sortBy, preference.order)) return;

    // Only apply if no sort is currently specified in URL
    if (!currentSort && typeof htmx !== 'undefined') {
        // Trigger HTMX request with saved sort preference
        const sortUrl = `/api/subscriptions?sort=${encodeURIComponent(preference.sortBy)}&order=${encodeURIComponent(preference.order)}`;
        htmx.ajax('GET', sortUrl, {
            target: '#subscription-list',
            swap: 'outerHTML'
        });
    }
}

// Listen for HTMX requests to capture sort changes
document.addEventListener('htmx:configRequest', function(event) {
    const path = event.detail.path;

    // Check if this is a sort request to subscriptions API
    if (path && path.includes('/api/subscriptions')) {
        const params = extractSortParams(path);
        if (params) {
            saveSortPreference(params.sortBy, params.order);
        }
    }
});

// Initialize on page load
document.addEventListener('DOMContentLoaded', function() {
    // Apply saved sort preference once HTMX is ready
    if (typeof htmx !== 'undefined') {
        applySavedSortPreference();
    }
});
