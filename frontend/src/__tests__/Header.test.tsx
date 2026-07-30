import { describe, it, expect } from 'vitest';
import { render, screen } from '@testing-library/react';
import Header from '@/components/layout/Header';

describe('Header', () => {
  it('renders the dashboard title', () => {
    render(<Header />);
    expect(screen.getByText('Dashboard')).toBeTruthy();
  });

  it('renders a connection status indicator with title', () => {
    render(<Header />);
    const dot = screen.getByTitle('Connected');
    expect(dot).toBeTruthy();
  });

  it('renders a <header> semantic element', () => {
    render(<Header />);
    expect(document.querySelector('header')).toBeTruthy();
  });
});
