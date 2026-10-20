// escapeHTML makes a value safe to interpolate into markup built as a string,
// both as text content and inside a quoted attribute. Templates that render
// API JSON into innerHTML must pass every value through it.
function escapeHTML(value) {
    return String(value).replace(/[&<>"']/g, ch => ({
        '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;'
    })[ch]);
}
