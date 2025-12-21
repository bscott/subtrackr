// Theme initialization - runs immediately to prevent flash
(function() {
    const theme = localStorage.getItem('subtrackr-theme') || 'default';
    document.documentElement.setAttribute('data-theme', theme);

    // Handle Tailwind dark mode for dark-classic theme
    if (theme === 'dark-classic') {
        document.documentElement.classList.add('dark');
    }
})();
