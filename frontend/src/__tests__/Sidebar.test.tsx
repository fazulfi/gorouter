import { describe, it, expect } from 'vitest';
import { render, screen } from '@testing-library/react';
import Sidebar from '@/components/layout/Sidebar';

describe('Sidebar', () => {
  it('renders the gorouter logo text', () => {
    render(<Sidebar />);
    expect(screen.getByText('gorouter')).toBeTruthy();
  });

  it('renders all five navigation links', () => {
    render(<Sidebar />);
    const links = screen.getAllByRole('link');
    expect(links).toHaveLength(5);
  });

  it('renders each navigation item with its label', () => {
    render(<Sidebar />);
    expect(screen.getByText('Dashboard')).toBeTruthy();
    expect(screen.getByText('Providers')).toBeTruthy();
    expect(screen.getByText('API Keys')).toBeTruthy();
    expect(screen.getByText('Tokens')).toBeTruthy();
    expect(screen.getByText('Settings')).toBeTruthy();
  });

  it('sets the correct href on each navigation link', () => {
    render(<Sidebar />);
    expect(
      screen.getByText('Dashboard').closest('a')?.getAttribute('href'),
    ).toBe('/');
    expect(
      screen.getByText('Providers').closest('a')?.getAttribute('href'),
    ).toBe('/providers');
    expect(
      screen.getByText('API Keys').closest('a')?.getAttribute('href'),
    ).toBe('/api-keys');
    expect(
      screen.getByText('Tokens').closest('a')?.getAttribute('href'),
    ).toBe('/pats');
    expect(
      screen.getByText('Settings').closest('a')?.getAttribute('href'),
    ).toBe('/settings');
  });

  it('renders within an <aside> semantic element', () => {
    render(<Sidebar />);
    expect(document.querySelector('aside')).toBeTruthy();
  });
});
