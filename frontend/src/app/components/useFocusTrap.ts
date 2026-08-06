import { useEffect, type MutableRefObject } from 'react';

/**
 * Constrain keyboard focus within the subtree so keyboard and screen-reader
 * users cannot tab out of a modal drawer (a11y A11). Esc calls onClose; focus
 * returns to the trigger on close. Caller owns triggerRef and decides active.
 */
export function useFocusTrap(
  containerRef: MutableRefObject<HTMLElement | null>,
  active: boolean,
  onClose: () => void,
  triggerRef?: MutableRefObject<HTMLElement | null>,
): void {
  useEffect(() => {
    if (!active) return;
    const lastActive = document.activeElement as HTMLElement | null;

    const keydown = (e: KeyboardEvent) => {
      if (e.key === 'Escape') {
        onClose();
        return;
      }
      if (e.key !== 'Tab') return;
      const container = containerRef.current;
      if (!container) return;
      const focusables = Array.from(
        container.querySelectorAll<HTMLElement>(
          'a[href], button, input, select, textarea, [tabindex]:not([tabindex="-1"])',
        ),
      ).filter((el) => !el.hasAttribute('disabled'));
      if (focusables.length === 0) return;

      const first = focusables[0];
      const last = focusables[focusables.length - 1];
      if (!first || !last) return;
      if (e.shiftKey && document.activeElement === first) {
        e.preventDefault();
        last.focus();
      } else if (!e.shiftKey && document.activeElement === last) {
        e.preventDefault();
        first.focus();
      }
    };

    document.addEventListener('keydown', keydown);
    return () => {
      document.removeEventListener('keydown', keydown);
      if (triggerRef?.current) triggerRef.current.focus();
      else if (lastActive) lastActive.focus();
    };
  }, [active, onClose, containerRef, triggerRef]);
}
