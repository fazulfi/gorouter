export const ariaLiveRegions = {
  announcement: 'aria-live="polite" aria-busy="false"',
  status: 'role="status"',
  log: 'role="log"' as const,
};

export const getAnnouncementAttrs = (message: string) => ({
  'aria-live': 'polite',
  'aria-atomic': 'true' as const,
  role: 'status',
});

export function announce(message: string) {
  if (typeof document === 'undefined') return;
  
  const el = document.createElement('div');
  Object.assign(el, getAnnouncementAttrs(message));
  el.textContent = message;
  document.body.appendChild(el);
  
  setTimeout(() => el.remove(), 3000);
}