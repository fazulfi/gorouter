import en from '../i18n/catalogs/en.json';
import id from '../i18n/catalogs/id.json';

export const locales = ['en', 'id'] as const;
export type Locale = typeof locales[number];

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
