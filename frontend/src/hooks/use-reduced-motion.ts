import { useEffect } from 'react';

export function useReducedMotion() {
  useEffect(() => {
    const query = window.matchMedia('(prefers-reduced-motion: reduce)');
    
    return () => {
      query.removeEventListener('change', handleQueryChange);
    };
  }, []);
  
  function handleQueryChange(event: MediaQueryListEvent) {
    document.body.dataset.motion = event.matches ? 'reduced' : 'normal';
  }
  
  const prefersReduced = window.matchMedia('(prefers-reduced-motion: reduce)').matches;
  document.body.dataset.motion = prefersReduced ? 'reduced' : 'normal';
  
  return prefersReduced;
}