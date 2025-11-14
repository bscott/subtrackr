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
    document.addEventListener('keydown', function(e) {
        if (e.key === 'Escape') {
            const mobileMenu = document.getElementById('mobile-menu');
            if (mobileMenu && !mobileMenu.classList.contains('hidden')) {
                closeMobileMenu();
            }
            // Also close modal if open
            const modal = document.getElementById('modal');
            if (modal) {
                modal.classList.add('hidden');
            }
        }
    });
});

