import { describe, it, expect } from 'vitest';
import { render, screen } from '@testing-library/react';
import App from '@/App';

describe('App shell', () => {
  it('renders the sidebar with gorouter logo', () => {
    render(<App />);
    expect(screen.getByText('gorouter')).toBeTruthy();
  });

  it('renders navigation links', () => {
    render(<App />);
    const navLinks = screen.getAllByRole('link');
    expect(navLinks.length).toBeGreaterThanOrEqual(5);
    expect(navLinks.some((l) => l.textContent?.includes('Dashboard'))).toBe(true);
    expect(navLinks.some((l) => l.textContent?.includes('Providers'))).toBe(true);
    expect(navLinks.some((l) => l.textContent?.includes('API Keys'))).toBe(true);
    expect(navLinks.some((l) => l.textContent?.includes('Tokens'))).toBe(true);
    expect(navLinks.some((l) => l.textContent?.includes('Settings'))).toBe(true);
  });

  it('renders the header title', () => {
    render(<App />);
    const headings = screen.getAllByText('Dashboard');
    expect(headings.length).toBeGreaterThanOrEqual(1);
  });

  it('renders the placeholder content', () => {
    render(<App />);
    expect(screen.getByText('Select a view from the sidebar.')).toBeTruthy();
  });
});
