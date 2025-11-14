// Mobile menu functions for responsive navigation
// Used across all page templates to provide consistent mobile menu behavior

function openMobileMenu() {
    const mobileMenu = document.getElementById('mobile-menu');
    if (mobileMenu) {
        mobileMenu.classList.remove('hidden');
        document.body.style.overflow = 'hidden'; // Prevent body scroll when menu is open
    }
}

function closeMobileMenu() {
    const mobileMenu = document.getElementById('mobile-menu');
    if (mobileMenu) {
        mobileMenu.classList.add('hidden');
        document.body.style.overflow = ''; // Restore body scroll
    }
}

// Close mobile menu and execute callback after menu is closed
// Uses requestAnimationFrame to ensure DOM updates are processed
function closeMobileMenuAndThen(callback) {
    closeMobileMenu();
    // Use double requestAnimationFrame to ensure browser has processed the DOM changes
    // This is more reliable than setTimeout and adapts to browser rendering speed
    requestAnimationFrame(() => {
        requestAnimationFrame(() => {
            if (callback) callback();
        });
    });
}

// Initialize mobile menu functionality when DOM is ready
document.addEventListener('DOMContentLoaded', function() {
    // Restore body scroll on page load (handles navigation before closeMobileMenu completes)
    document.body.style.overflow = '';

    // Open mobile menu when hamburger button is clicked
    const mobileMenuButton = document.getElementById('mobile-menu-button');
    if (mobileMenuButton) {
        mobileMenuButton.addEventListener('click', openMobileMenu);
    }

    // Close mobile menu on escape key
    // Close only the topmost element (modal first, then menu)
    document.addEventListener('keydown', function(e) {
        if (e.key === 'Escape') {
            const modal = document.getElementById('modal');
            const mobileMenu = document.getElementById('mobile-menu');
            
            // If modal is open, close it (modal is topmost)
            if (modal && !modal.classList.contains('hidden')) {
                modal.classList.add('hidden');
            } 
            // Otherwise, if mobile menu is open, close it
            else if (mobileMenu && !mobileMenu.classList.contains('hidden')) {
                closeMobileMenu();
            }
        }
    });
});

