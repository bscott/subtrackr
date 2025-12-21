// Theme initialization - runs immediately to prevent flash
(function() {
    const theme = localStorage.getItem('subtrackr-theme') || 'default';
    document.documentElement.setAttribute('data-theme', theme);
})();
