import { describe, it, expect, afterEach } from 'vitest';
import { renderHook, act } from '@testing-library/react';
import { setLocale, t, useTranslations } from '@/i18n';

describe('i18n useTranslations hook', () => {
  afterEach(() => {
    act(() => {
      setLocale('en');
    });
  });

  it('resolves keys against the default en catalog', () => {
    const { result } = renderHook(() => useTranslations());
    expect(result.current('dashboard.title')).toBe('Dashboard');
    expect(result.current('dashboard.welcome')).toBe('Welcome back');
    expect(result.current('auth.login')).toBe('Login');
  });

  it('returns the key itself when the key does not exist', () => {
    const { result } = renderHook(() => useTranslations());
    expect(result.current('non.existent.key')).toBe('non.existent.key');
  });

  it('re-renders when setLocale switches the active locale', () => {
    const { result } = renderHook(() => useTranslations());
    expect(result.current('dashboard.title')).toBe('Dashboard');

    act(() => {
      setLocale('id');
    });

    expect(result.current('dashboard.title')).toBe('Dasbor');
    expect(result.current('auth.login')).toBe('Masuk');
  });

  it('honors a locale override parameter', () => {
    const { result } = renderHook(() => useTranslations('id'));
    expect(result.current('dashboard.title')).toBe('Dasbor');
  });

  it('ignores setLocale calls for unknown locales', () => {
    const { result } = renderHook(() => useTranslations());

    act(() => {
      setLocale('fr' as never);
    });

    expect(result.current('dashboard.title')).toBe('Dashboard');
  });

  it('keeps the module-level t(locale, key) lookup intact', () => {
    expect(t('en', 'auth.logout')).toBe('Logout');
    expect(t('id', 'auth.logout')).toBe('Keluar');
    expect(t('en', 'missing.key')).toBe('missing.key');
  });
});
