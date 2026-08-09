import { useSyncExternalStore } from 'react';
import en from '../i18n/catalogs/en.json';
import id from '../i18n/catalogs/id.json';

export const locales = ['en', 'id'] as const;
export type Locale = (typeof locales)[number];

export const translations: Record<Locale, typeof en> = {
  en,
  id,
};

export function t(locale: Locale, key: string): string {
  const parts = key.split('.');
  let obj: any = translations[locale];
  for (const part of parts) {
    obj = obj?.[part];
    if (!obj) return key;
  }
  return obj;
}

let activeLocale: Locale = 'en';
const localeListeners = new Set<() => void>();

export function getLocale(): Locale {
  return activeLocale;
}

export function setLocale(locale: Locale): void {
  if (!locales.includes(locale) || locale === activeLocale) return;
  activeLocale = locale;
  for (const listener of [...localeListeners]) listener();
}

function subscribeLocale(listener: () => void): () => void {
  localeListeners.add(listener);
  return () => {
    localeListeners.delete(listener);
  };
}

/**
 * Translation hook: returns a `t(key)` lookup bound to the active locale
 * (or an explicit override). Components re-render automatically when
 * `setLocale` changes the active locale.
 */
export function useTranslations(localeOverride?: Locale): (key: string) => string {
  const storedLocale = useSyncExternalStore(subscribeLocale, getLocale, getLocale);
  const locale = localeOverride ?? storedLocale;
  return (key: string) => t(locale, key);
}
