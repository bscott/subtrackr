// Filters the already-rendered subscription rows without disturbing HTMX sort state.
(function () {
    function applySubscriptionSearch() {
        const input = document.getElementById('subscription-search');
        const list = document.getElementById('subscription-list');
        if (!input || !list) return;

        const query = input.value.trim().toLocaleLowerCase();
        const rows = list.querySelectorAll('tbody tr[data-search-name]');
        let visibleRows = 0;

        rows.forEach((row) => {
            const name = (row.dataset.searchName || '').toLocaleLowerCase();
            const matches = query === '' || name.includes(query);
            row.hidden = !matches;
            if (matches) visibleRows++;
        });

        const emptyState = document.getElementById('subscription-search-empty');
        if (emptyState) {
            const noMatches = query !== '' && rows.length > 0 && visibleRows === 0;
            emptyState.classList.toggle('hidden', !noMatches);
        }
    }

    function initializeSubscriptionSearch() {
        const input = document.getElementById('subscription-search');
        if (!input) return;
        input.addEventListener('input', applySubscriptionSearch);
        applySubscriptionSearch();
    }

    document.addEventListener('DOMContentLoaded', initializeSubscriptionSearch);
    // An outerHTML swap replaces #subscription-list, so look the list up by id
    // instead of trusting the (now detached) swap target. Re-apply after settle
    // too: settling restores the server-rendered attributes of id-matched
    // elements such as #subscription-search-empty, undoing the after:swap pass.
    function reapplyAfterListSwap() {
        if (document.getElementById('subscription-list')) {
            applySubscriptionSearch();
        }
    }
    document.addEventListener('htmx:after:swap', reapplyAfterListSwap);
    document.addEventListener('htmx:after:settle', reapplyAfterListSwap);
})();
