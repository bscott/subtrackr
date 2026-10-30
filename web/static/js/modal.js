// Shared behaviour for the #modal dialog that hosts the subscription form.
//
// The markup lives in every page template:
//   <div id="modal" role="dialog" aria-modal="true" aria-labelledby="modal-title" class="hidden ...">
//     <div class="...panel..."><div id="modal-content">...loaded by htmx...</div></div>
//   </div>
// openModal(opener) shows it and remembers which control opened it; focus moves
// into the dialog as soon as its content is there, Tab and Shift+Tab stay
// inside it, and closeModal() hides it and gives focus back to the opener.

(function () {
    var opener = null;

    function modalElement() {
        return document.getElementById('modal');
    }

    function isOpen(modal) {
        return !!modal && !modal.classList.contains('hidden');
    }

    function focusableIn(container) {
        var selector = 'a[href], button:not([disabled]), input:not([disabled]):not([type="hidden"]), ' +
            'select:not([disabled]), textarea:not([disabled]), [tabindex]:not([tabindex="-1"])';
        return Array.prototype.filter.call(container.querySelectorAll(selector), function (el) {
            return el.offsetParent !== null || el === document.activeElement;
        });
    }

    function isVisible(el) {
        return !!el && el.isConnected && el.offsetParent !== null;
    }

    // Prefer the first form field so keyboard users land where they type; fall
    // back to any control (the close button), then the dialog itself.
    function focusContent(modal) {
        if (!isOpen(modal)) return;
        var content = document.getElementById('modal-content') || modal;
        var fields = focusableIn(content).filter(function (el) {
            return /^(INPUT|SELECT|TEXTAREA)$/.test(el.tagName);
        });
        var target = fields[0] || focusableIn(content)[0];
        if (!target) {
            modal.setAttribute('tabindex', '-1');
            target = modal;
        }
        target.focus({ preventScroll: true });
    }

    function openModal(from) {
        var modal = modalElement();
        if (!modal) return;
        opener = isVisible(from) ? from : (document.activeElement !== document.body ? document.activeElement : null);
        modal.classList.remove('hidden');
        // Focus the dialog itself for now. The fields of the form that is on
        // its way are focused once it has arrived (see the settle listener below).
        // The stale form from an earlier open must not get focus: htmx keeps
        // the focused input's value across its swap, which would carry the
        // previous subscription's name into the new form.
        modal.setAttribute('tabindex', '-1');
        modal.focus({ preventScroll: true });
    }

    function closeModal() {
        var modal = modalElement();
        if (!modal || !isOpen(modal)) return;
        modal.classList.add('hidden');
        var target = opener;
        opener = null;
        if (!isVisible(target)) target = document.getElementById('mobile-menu-button');
        if (isVisible(target)) target.focus({ preventScroll: true });
    }

    window.openModal = openModal;
    window.closeModal = closeModal;

    document.addEventListener('DOMContentLoaded', function () {
        var modal = modalElement();
        if (!modal) return;

        // Clicking the dimmed backdrop (the dialog element itself) closes it.
        modal.addEventListener('click', function (e) {
            if (e.target === modal) closeModal();
        });

        // Move focus out of old inputs before HTMX morphs them: focused input
        // values are intentionally preserved by HTMX. Focus the new form only
        // after the entire swap has settled, never during intermediate mutations.
        document.addEventListener('htmx:before:swap', function (event) {
            var target = event.detail && event.detail.ctx && event.detail.ctx.target;
            if (target && target.id === 'modal-content' && isOpen(modal)) {
                modal.focus({ preventScroll: true });
            }
        });
        document.addEventListener('htmx:after:settle', function (event) {
            var target = event.detail && event.detail.task && event.detail.task.target;
            if (target && target.id === 'modal-content' && isOpen(modal)) focusContent(modal);
        });

        // Keep Tab inside the open dialog: wrap at the ends of the control list.
        var lastTabShift = null;
        document.addEventListener('keydown', function (e) {
            if (e.key !== 'Tab' || !isOpen(modal)) return;
            lastTabShift = e.shiftKey;
            var items = focusableIn(modal);
            if (items.length === 0) {
                e.preventDefault();
                return;
            }
            var first = items[0];
            var last = items[items.length - 1];
            var active = document.activeElement;
            if (!modal.contains(active)) {
                e.preventDefault();
                (e.shiftKey ? last : first).focus();
            } else if (e.shiftKey && active === first) {
                e.preventDefault();
                last.focus();
            } else if (!e.shiftKey && active === last) {
                e.preventDefault();
                first.focus();
            }
        });

        // WebKit (Safari) skips buttons and links on Tab, so its own order can
        // step past the close button or Save straight out of the dialog. When
        // focus leaves while the dialog is open, pull it back to the end it
        // should have wrapped to.
        document.addEventListener('focusin', function (e) {
            if (!isOpen(modal) || modal.contains(e.target)) return;
            var items = focusableIn(modal);
            if (items.length === 0) return;
            (lastTabShift ? items[items.length - 1] : items[0]).focus();
        });
        modal.addEventListener('focusout', function (e) {
            if (!isOpen(modal) || e.relatedTarget) return; // null: focus went to the document/body
            var items = focusableIn(modal);
            if (items.length === 0) return;
            setTimeout(function () {
                if (isOpen(modal) && !modal.contains(document.activeElement)) {
                    modal.focus({ preventScroll: true });
                }
            }, 0);
        });
    });
})();
