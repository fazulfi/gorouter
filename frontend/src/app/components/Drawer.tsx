import { useRef, useState } from 'react';
import type { MutableRefObject } from 'react';
import Sidebar from '@/app/components/Sidebar';
import { useFocusTrap } from '@/app/components/useFocusTrap';
import styles from './Drawer.module.css';

/**
 * Mobile nav drawer (a11y A4, A10-A12). Trigger exposes aria-expanded and
 * aria-controls="primary-nav"; the drawer renders as <aside>+<nav>, traps
 * focus (A11), closes on Esc, restores focus to the trigger on close.
 */
export default function Drawer() {
  const [open, setOpen] = useState(false);
  const triggerRef = useRef<HTMLButtonElement | null>(null);
  const panelRef = useRef<HTMLElement | null>(null);
  useFocusTrap(
    panelRef,
    open,
    () => setOpen(false),
    triggerRef as MutableRefObject<HTMLElement | null>,
  );

  const close = () => setOpen(false);

  return (
    <>
      <button
        type="button"
        className={styles.trigger}
        aria-expanded={open}
        aria-controls="primary-nav"
        aria-label="Open navigation menu"
        onClick={() => setOpen((v) => !v)}
        ref={triggerRef}
      >
        Menu
      </button>
      {open && (
        <div className={styles.backdrop} onClick={close} />
      )}
      {open && (
        <aside id="primary-nav" ref={panelRef} className={styles.drawer}>
          <div className={styles.head}>
            <span className={styles.logo}>gorouter</span>
            <button
              type="button"
              className={styles.close}
              onClick={close}
              aria-label="Close navigation menu"
            >
              ×
            </button>
          </div>
          <Sidebar />
        </aside>
      )}
    </>
  );
}
