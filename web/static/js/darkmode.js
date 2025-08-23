// Enhanced Dark Mode Management for SubTrackr
class DarkModeManager {
    constructor() {
        this.init();
    }
    
    init() {
        // Check system preference first, then saved preference
        const systemPrefersDark = window.matchMedia('(prefers-color-scheme: dark)').matches;
        const savedPreference = localStorage.getItem('darkMode');
        const shouldBeDark = savedPreference ? savedPreference === 'true' : systemPrefersDark;
        
        this.setDarkMode(shouldBeDark, false); // Don't save on init
        this.setupSystemPreferenceListener();
    }
    
    setDarkMode(enabled, save = true) {
        document.documentElement.classList.toggle('dark', enabled);
        if (save) {
            localStorage.setItem('darkMode', enabled.toString());
            this.syncWithServer(enabled);
        }
        
        // Update toggle switch to match current state (if it exists on current page)
        const toggle = document.querySelector('input[hx-post="/api/settings/dark-mode"]');
        if (toggle) {
            toggle.checked = enabled;
        }
    }
    
    toggle() {
        const isDark = document.documentElement.classList.contains('dark');
        this.setDarkMode(!isDark);
    }
    
    syncWithServer(enabled) {
        fetch('/api/settings/dark-mode', {
            method: 'POST',
            headers: { 'Content-Type': 'application/x-www-form-urlencoded' },
            body: `enabled=${enabled}`
        }).catch(err => console.log('Failed to sync dark mode with server:', err));
    }
    
    setupSystemPreferenceListener() {
        window.matchMedia('(prefers-color-scheme: dark)')
              .addEventListener('change', (e) => {
                  // Only auto-switch if user hasn't set a manual preference
                  if (!localStorage.getItem('darkMode')) {
                      this.setDarkMode(e.matches, false);
                  }
              });
    }
}

// Global dark mode manager
let darkModeManager;

// Legacy toggle function for backward compatibility
function toggleDarkMode() {
    if (darkModeManager) {
        darkModeManager.toggle();
    }
}

// Initialize on DOM ready
document.addEventListener('DOMContentLoaded', function() {
    darkModeManager = new DarkModeManager();
});