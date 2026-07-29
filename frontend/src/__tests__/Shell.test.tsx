import { describe, it, expect } from 'vitest';
import { render, screen } from '@testing-library/react';
import Shell from '@/components/layout/Shell';

describe('Shell layout', () => {
  it('renders the sidebar with gorouter logo', () => {
    render(<Shell />);
    expect(screen.getByText('gorouter')).toBeTruthy();
  });

  it('renders all five navigation links', () => {
    render(<Shell />);
    const links = screen.getAllByRole('link');
    expect(links).toHaveLength(5);
  });

  it('renders the header with Dashboard title alongside the sidebar nav link', () => {
    render(<Shell />);
    expect(screen.getAllByText('Dashboard')).toHaveLength(2);
  });

  it('renders the connection status indicator', () => {
    render(<Shell />);
    expect(screen.getByTitle('Connected')).toBeTruthy();
  });

  it('renders the placeholder content message', () => {
    render(<Shell />);
    expect(
      screen.getByText('Select a view from the sidebar.'),
    ).toBeTruthy();
  });
});
