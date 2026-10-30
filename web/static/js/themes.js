// SubTrackr Theme System
const themes = {
    default: {
        name: 'Default',
        description: 'Clean and professional',
        colors: {
            primary: '#3b82f6',
            primaryHover: '#2563eb',
            secondary: '#64748b',
            success: '#10b981',
            warning: '#f59e0b',
            danger: '#ef4444',
            background: '#f9fafb',
            surface: '#ffffff',
            surfaceHover: '#f3f4f6',
            text: '#111827',
            textSecondary: '#6b7280',
            border: '#e5e7eb',
        }
    },
    dark: {
        name: 'Dark',
        description: 'Easy on the eyes',
        colors: {
            primary: '#3b82f6',
            primaryHover: '#60a5fa',
            secondary: '#64748b',
            success: '#10b981',
            warning: '#f59e0b',
            danger: '#ef4444',
            background: '#111827',
            surface: '#1f2937',
            surfaceHover: '#374151',
            text: '#f9fafb',
            textSecondary: '#9ca3af',
            border: '#374151',
        }
    },
    'dark-classic': {
        name: 'Dark Classic',
        description: 'Original dark mode',
        useTailwindDark: true,  // Special flag to use Tailwind's dark mode
        colors: {
            primary: '#3b82f6',
            primaryHover: '#60a5fa',
            secondary: '#64748b',
            success: '#10b981',
            warning: '#f59e0b',
            danger: '#ef4444',
            background: '#111827',
            surface: '#1f2937',
            surfaceHover: '#374151',
            text: '#f9fafb',
            textSecondary: '#9ca3af',
            border: '#374151',
        }
    },
    christmas: {
        name: 'Christmas',
        description: 'Festive and jolly! 🎄',
        colors: {
            primary: '#c41e3a',      // Christmas red
            primaryHover: '#a01729',
            secondary: '#165b33',     // Forest green
            success: '#10b981',
            warning: '#ffd700',       // Gold
            danger: '#ef4444',
            background: '#fef3f3',    // Light snow white with hint of red
            surface: '#ffffff',
            surfaceHover: '#fef2f2',
            text: '#1f2937',
            textSecondary: '#6b7280',
            border: '#fecaca',        // Light red border
        },
        special: {
            accent: '#ffd700',        // Gold accents
            snow: '#ffffff',
            holly: '#165b33',
            berry: '#c41e3a'
        }
    },
    midnight: {
        name: 'Midnight',
        description: 'Deep and mysterious',
        colors: {
            primary: '#8b5cf6',       // Purple
            primaryHover: '#7c3aed',
            secondary: '#64748b',
            success: '#10b981',
            warning: '#f59e0b',
            danger: '#ef4444',
            background: '#0f172a',    // Very dark blue
            surface: '#1e293b',
            surfaceHover: '#334155',
            text: '#f1f5f9',
            textSecondary: '#94a3b8',
            border: '#334155',
        }
    },
    ocean: {
        name: 'Ocean',
        description: 'Cool and refreshing',
        colors: {
            primary: '#0891b2',       // Cyan
            primaryHover: '#06b6d4',
            secondary: '#64748b',
            success: '#10b981',
            warning: '#f59e0b',
            danger: '#ef4444',
            background: '#f0f9ff',    // Light cyan
            surface: '#ffffff',
            surfaceHover: '#e0f2fe',
            text: '#0c4a6e',
            textSecondary: '#475569',
            border: '#bae6fd',
        }
    }
};

const DEFAULT_THEME = 'dark-classic';
const THEME_STORAGE_KEY = 'subtrackr-theme';

function isKnownTheme(themeName) {
    return Object.prototype.hasOwnProperty.call(themes, themeName);
}

// Apply theme to document. Purely visual: nothing is stored or sent anywhere.
// Fires "subtrackr:themechange" on document so page scripts (the settings
// picker) can reflect the active theme.
function applyTheme(themeName) {
    if (!isKnownTheme(themeName)) themeName = DEFAULT_THEME;
    const theme = themes[themeName];
    const root = document.documentElement;

    // Set theme data attribute
    root.setAttribute('data-theme', themeName);

    // Handle Tailwind dark mode for dark-classic theme
    if (theme.useTailwindDark) {
        root.classList.add('dark');
    } else {
        root.classList.remove('dark');
    }

    // Apply CSS variables
    Object.entries(theme.colors).forEach(([key, value]) => {
        root.style.setProperty(`--theme-${key}`, value);
    });

    // Apply special properties for Christmas theme
    if (themeName === 'christmas' && theme.special) {
        Object.entries(theme.special).forEach(([key, value]) => {
            root.style.setProperty(`--theme-special-${key}`, value);
        });

        // Enable snow animation
        enableSnowfall();
    } else {
        // Disable snow animation for other themes
        disableSnowfall();
    }

    document.dispatchEvent(new CustomEvent('subtrackr:themechange', { detail: { theme: themeName } }));
    return themeName;
}

// The user picked a theme: apply it, remember it in this browser and save it
// on the server so other browsers pick it up. This is the only place the
// preference is written.
let themeChoiceVersion = 0;
function setTheme(themeName) {
    themeChoiceVersion++;
    const applied = applyTheme(themeName);
    rememberTheme(applied);
    return saveThemePreference(applied);
}

function rememberTheme(themeName) {
    try {
        localStorage.setItem(THEME_STORAGE_KEY, themeName);
    } catch (err) {
        // Storage can be unavailable (private mode, quota); the server copy still works.
    }
}

// Save theme preference to server
function saveThemePreference(themeName) {
    return fetch('/api/settings/theme', {
        method: 'POST',
        keepalive: true,
        headers: {
            'Content-Type': 'application/json',
        },
        body: JSON.stringify({ theme: themeName })
    })
    .then(response => {
        if (!response.ok) throw new Error('Theme save failed');
    })
    .catch(() => {
        if (typeof showRequestError === 'function') {
            showRequestError('Unable to save your theme. Please try again before leaving this page.');
        }
    });
}

// Theme remembered in this browser, if any.
function getLocalTheme() {
    try {
        const localTheme = localStorage.getItem(THEME_STORAGE_KEY);
        return isKnownTheme(localTheme) ? localTheme : null;
    } catch (err) {
        return null;
    }
}

// Theme saved on the server (the preference shared by every browser), or null
// when it cannot be read.
function fetchServerTheme() {
    if (['/login', '/forgot-password', '/reset-password'].includes(location.pathname)) return Promise.resolve(null);
    return fetch('/api/settings/theme', { headers: { 'Accept': 'application/json' } })
        .then(response => (response.ok ? response.json() : null))
        .then(data => (data && data.saved !== false && isKnownTheme(data.theme) ? data.theme : null))
        .catch(err => {
            console.error('Failed to load theme:', err);
            return null;
        });
}

// Load saved theme on page load.
//
// theme-init.js has already applied the browser's remembered theme before
// first paint. The server copy is the source of
// truth: a choice made in another browser wins over a stale local value, and
// nothing is written back during a plain page view.
function loadSavedTheme() {
    const choiceVersion = themeChoiceVersion;
    const localTheme = getLocalTheme();
    applyTheme(localTheme || 'default');
    return fetchServerTheme().then(serverTheme => {
        if (!serverTheme || themeChoiceVersion !== choiceVersion) return;
        if (serverTheme !== localTheme) {
            applyTheme(serverTheme);
            rememberTheme(serverTheme);
        }
    });
}

// Snowfall animation for Christmas theme
function enableSnowfall() {
    // Remove existing snowflakes
    disableSnowfall();

    const snowContainer = document.createElement('div');
    snowContainer.id = 'snowfall-container';
    snowContainer.style.cssText = `
        position: fixed;
        top: 0;
        left: 0;
        width: 100%;
        height: 100%;
        pointer-events: none;
        z-index: 9999;
        overflow: hidden;
    `;

    // Create snowflakes
    for (let i = 0; i < 50; i++) {
        createSnowflake(snowContainer);
    }

    document.body.appendChild(snowContainer);
}

function createSnowflake(container) {
    const snowflake = document.createElement('div');
    snowflake.className = 'snowflake';
    snowflake.innerHTML = '❄';

    // Random properties
    const size = Math.random() * 0.5 + 0.5; // 0.5 to 1em
    const left = Math.random() * 100; // 0 to 100%
    const animationDuration = Math.random() * 3 + 2; // 2 to 5 seconds
    const opacity = Math.random() * 0.5 + 0.3; // 0.3 to 0.8
    const delay = Math.random() * 5; // 0 to 5 seconds delay

    snowflake.style.cssText = `
        position: absolute;
        top: -10%;
        left: ${left}%;
        font-size: ${size}em;
        opacity: ${opacity};
        animation: snowfall ${animationDuration}s linear ${delay}s infinite;
        user-select: none;
    `;

    container.appendChild(snowflake);
}

function disableSnowfall() {
    const snowContainer = document.getElementById('snowfall-container');
    if (snowContainer) {
        snowContainer.remove();
    }
}

// Initialize on page load
document.addEventListener('DOMContentLoaded', () => {
    loadSavedTheme();
});
