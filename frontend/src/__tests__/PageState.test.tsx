import { describe, it, expect, vi } from 'vitest';
import { render, screen, fireEvent } from '@testing-library/react';
import PageState from '@/app/components/PageState';

describe('PageState five-state contract (a11y A34-A38)', () => {
  it('renders a loading skeleton with aria-busy', () => {
    render(<PageState kind="loading" />);
    const busy = document.querySelector('[aria-busy="true"]');
    expect(busy).toBeTruthy();
  });

  it('renders an empty state with a primary action', () => {
    render(
      <PageState
        kind="empty"
        title="No keys"
        message="Create your first key."
        primaryAction={<button>Create key</button>}
      />,
    );
    expect(screen.getByText('No keys')).toBeTruthy();
    expect(screen.getByText('Create your first key.')).toBeTruthy();
    expect(screen.getByRole('button', { name: 'Create key' })).toBeTruthy();
  });

  it('renders an error state with retry and report link', () => {
    const retry = vi.fn();
    render(
      <PageState kind="error" title="Load failed" message="Could not load data." retry={retry} />,
    );
    fireEvent.click(screen.getByRole('button', { name: 'Try again' }));
    expect(retry).toHaveBeenCalled();
    expect(screen.getByRole('button', { name: 'Report an issue' })).toBeTruthy();
  });

  it('renders permission-denied distinct from a data error', () => {
    render(
      <PageState
        kind="permission"
        message="You do not have access to this page."
      />,
    );
    expect(screen.getByText('You do not have access')).toBeTruthy();
    expect(screen.getByText(/contact a workspace administrator/i)).toBeTruthy();
  });

  it('renders data children directly', () => {
    render(<PageState kind="data">content</PageState>);
    expect(screen.getByText('content')).toBeTruthy();
  });
});
