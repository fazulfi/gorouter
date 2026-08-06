import { create } from 'zustand';
import { getSessionStatus, getCurrentUser } from '@/shared/api/client';
import type { User } from '@/generated/admin-v1';

export type AuthState = 'loading' | 'authenticated' | 'unauthenticated';

interface AuthStore {
  state: AuthState;
  user: User | null;
  error: string | null;
  check: () => Promise<AuthState>;
  setAuthenticated: (user: User) => void;
  setUnauthenticated: () => boolean;
  clear: () => void;
}

export const useAuthStore = create<AuthStore>((set) => ({
  state: 'loading',
  user: null,
  error: null,

  check: async () => {
    set({ state: 'loading', error: null });
    try {
      const status = await getSessionStatus();
      if (status.authenticated) {
        const user = status.user ?? (await getCurrentUser().catch(() => undefined));
        set({ state: 'authenticated', user: user ?? null });
        return 'authenticated';
      }
      set({ state: 'unauthenticated', user: null });
      return 'unauthenticated';
    } catch {
      set({ state: 'unauthenticated', user: null, error: 'Could not verify session.' });
      return 'unauthenticated';
    }
  },

  setAuthenticated: (user) => set({ state: 'authenticated', user, error: null }),

  setUnauthenticated: () => {
    set({ state: 'unauthenticated', user: null });
    return true;
  },

  clear: () => set({ state: 'loading', user: null, error: null }),
}));
