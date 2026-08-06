import { create } from 'zustand';

export interface Toast {
  id: number;
  kind: 'error' | 'warning' | 'info' | 'success';
  message: string;
}

interface ToastStore {
  toasts: Toast[];
  push: (kind: Toast['kind'], message: string) => number;
  dismiss: (id: number) => void;
  clear: () => void;
}

let nextId = 1;

export const useToastStore = create<ToastStore>((set) => ({
  toasts: [],
  push: (kind, message) => {
    const id = nextId++;
    set((s) => ({ toasts: [...s.toasts, { id, kind, message }] }));
    return id;
  },
  dismiss: (id) =>
    set((s) => ({ toasts: s.toasts.filter((t) => t.id !== id) })),
  clear: () => set({ toasts: [] }),
}));
