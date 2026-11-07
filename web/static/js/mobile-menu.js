// Mobile menu functions for responsive navigation
// Used across all page templates to provide consistent mobile menu behavior
//
// Markup (see the page templates):
//   <button id="mobile-menu-button" aria-expanded="false" aria-controls="mobile-menu">
//   <div id="mobile-menu" class="hidden ..."> backdrop + <div role="dialog" aria-modal="true"> ... </div></div>
// Opening the menu moves keyboard focus into the drawer, Tab stays inside it,
// and closing it returns focus to the hamburger button.

function mobileMenuFocusable(menu) {
    const selector = 'a[href], button:not([disabled]), input:not([disabled]), select:not([disabled]), textarea:not([disabled]), [tabindex]:not([tabindex="-1"])';
    return Array.prototype.filter.call(menu.querySelectorAll(selector), el => el.offsetParent !== null);
}

function isMobileMenuOpen() {
    const mobileMenu = document.getElementById('mobile-menu');
    return !!mobileMenu && !mobileMenu.classList.contains('hidden');
}

function openMobileMenu() {
    const mobileMenu = document.getElementById('mobile-menu');
    if (mobileMenu) {
        mobileMenu.classList.remove('hidden');
        document.body.style.overflow = 'hidden'; // Prevent body scroll when menu is open
        const button = document.getElementById('mobile-menu-button');
        if (button) button.setAttribute('aria-expanded', 'true');
        // Land on the first navigation link rather than the close button so the
        // next Tab walks the menu items; fall back to anything focusable.
        const items = mobileMenuFocusable(mobileMenu);
        const target = mobileMenu.querySelector('nav a, nav button') || items[0];
        if (target) target.focus({ preventScroll: true });
    }
}

function closeMobileMenu(options) {
    const mobileMenu = document.getElementById('mobile-menu');
    if (mobileMenu) {
        const wasOpen = !mobileMenu.classList.contains('hidden');
        mobileMenu.classList.add('hidden');
        document.body.style.overflow = ''; // Restore body scroll
        const button = document.getElementById('mobile-menu-button');
        if (button) {
            button.setAttribute('aria-expanded', 'false');
            const restoreFocus = !(options && options.restoreFocus === false);
            if (wasOpen && restoreFocus) button.focus({ preventScroll: true });
        }
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

    document.addEventListener('keydown', function(e) {
        // Close only the topmost element (modal first, then menu)
        if (e.key === 'Escape') {
            const modal = document.getElementById('modal');
            const mobileMenu = document.getElementById('mobile-menu');

            // If modal is open, close it (modal is topmost)
            if (modal && !modal.classList.contains('hidden')) {
                if (typeof closeModal === 'function') closeModal();
                else modal.classList.add('hidden');
            }
            // Otherwise, if mobile menu is open, close it
            else if (mobileMenu && !mobileMenu.classList.contains('hidden')) {
                closeMobileMenu();
            }
            return;
        }

        // Keep Tab inside the open drawer while no modal sits on top of it.
        // The drawer holds only links and buttons, which WebKit (Safari) skips
        // on Tab, so the move is made here rather than left to the browser.
        if (e.key === 'Tab' && isMobileMenuOpen()) {
            const modal = document.getElementById('modal');
            if (modal && !modal.classList.contains('hidden')) return;
            const mobileMenu = document.getElementById('mobile-menu');
            const items = mobileMenuFocusable(mobileMenu);
            if (items.length === 0) return;
            e.preventDefault();
            const index = items.indexOf(document.activeElement);
            let next;
            if (index === -1) next = e.shiftKey ? items.length - 1 : 0;
            else next = (index + (e.shiftKey ? -1 : 1) + items.length) % items.length;
            items[next].focus();
        }
    });
});
