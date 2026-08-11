import { describe, it, expect, vi } from 'vitest';
import { useState } from 'react';
import { render, screen, fireEvent, act } from '@testing-library/react';
import { Button } from '@/components/ui/button';
import { Input } from '@/components/ui/input';
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from '@/components/ui/card';
import { Separator } from '@/components/ui/separator';
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs';

function classes(el: Element): string[] {
  return Array.from(el.classList);
}

describe('Button', () => {
  it('renders children and handles clicks', () => {
    const handleClick = vi.fn();
    render(<Button onClick={handleClick}>Click me</Button>);
    const button = screen.getByRole('button', { name: 'Click me' });
    fireEvent.click(button);
    expect(handleClick).toHaveBeenCalledTimes(1);
  });

  it('defaults to type=button', () => {
    render(<Button>Default type</Button>);
    expect(screen.getByRole('button').getAttribute('type')).toBe('button');
  });

  it('blocks clicks when disabled', () => {
    const handleClick = vi.fn();
    render(
      <Button disabled onClick={handleClick}>
        Disabled
      </Button>,
    );
    const button = screen.getByRole('button') as HTMLButtonElement;
    expect(button.disabled).toBe(true);
    fireEvent.click(button);
    expect(handleClick).not.toHaveBeenCalled();
  });

  it('applies distinct classes for the outline variant', () => {
    const { container: defaultContainer } = render(<Button>Default</Button>);
    const { container: outlineContainer } = render(
      <Button variant="outline">Outline</Button>,
    );
    const defaultClasses = classes(defaultContainer.firstElementChild as Element);
    const outlineClasses = classes(outlineContainer.firstElementChild as Element);
    expect(outlineClasses.some((c) => c.includes('outline'))).toBe(true);
    expect(defaultClasses).not.toEqual(outlineClasses);
  });

  it('applies size variant classes', () => {
    const { container } = render(<Button size="sm">Small</Button>);
    const buttonClasses = classes(container.firstElementChild as Element);
    expect(buttonClasses.some((c) => c.includes('sm'))).toBe(true);
  });

  it('renders as child element with asChild', () => {
    render(
      <Button asChild>
        <a href="/next">Link button</a>
      </Button>,
    );
    const link = screen.getByRole('link', { name: 'Link button' });
    expect(link.getAttribute('href')).toBe('/next');
  });
});

describe('Input', () => {
  it('renders with placeholder and forwards aria-label', () => {
    render(<Input aria-label="Email address" placeholder="name@example.com" />);
    const input = screen.getByLabelText('Email address') as HTMLInputElement;
    expect(input.getAttribute('placeholder')).toBe('name@example.com');
  });

  it('supports controlled value changes', () => {
    function Harness() {
      const [value, setValue] = useState('');
      return (
        <Input
          aria-label="Field"
          value={value}
          onChange={(e) => setValue(e.target.value)}
        />
      );
    }
    render(<Harness />);
    const input = screen.getByLabelText('Field') as HTMLInputElement;
    fireEvent.change(input, { target: { value: 'hello' } });
    expect(input.value).toBe('hello');
  });
});

describe('Card family', () => {
  it('renders header, title, description, and content', () => {
    render(
      <Card>
        <CardHeader>
          <CardTitle>Test Title</CardTitle>
          <CardDescription>Description text</CardDescription>
        </CardHeader>
        <CardContent>Content here</CardContent>
      </Card>,
    );
    expect(screen.getByText('Test Title')).toBeTruthy();
    expect(screen.getByText('Description text')).toBeTruthy();
    expect(screen.getByText('Content here')).toBeTruthy();
  });

  it('renders CardTitle as a heading element', () => {
    render(
      <Card>
        <CardTitle>Heading</CardTitle>
      </Card>,
    );
    expect(screen.getByText('Heading').tagName.toLowerCase()).toBe('h3');
  });
});

describe('Separator', () => {
  it('is decorative by default (no separator role)', () => {
    const { container } = render(<Separator />);
    expect((container.firstElementChild as Element).getAttribute('role')).toBeNull();
  });

  it('exposes role=separator with aria-orientation when not decorative', () => {
    const { container } = render(<Separator decorative={false} />);
    const el = container.firstElementChild as Element;
    expect(el.getAttribute('role')).toBe('separator');
    expect(el.getAttribute('aria-orientation')).toBe('horizontal');
  });

  it('supports vertical orientation', () => {
    const { container } = render(<Separator decorative={false} orientation="vertical" />);
    expect(
      (container.firstElementChild as Element).getAttribute('aria-orientation'),
    ).toBe('vertical');
  });
});

  describe('Tabs', () => {
    function renderTabs() {
      return render(
        <Tabs defaultValue="overview">
          <TabsList>
            <TabsTrigger value="overview">Overview</TabsTrigger>
            <TabsTrigger value="providers">Providers</TabsTrigger>
          </TabsList>
          <TabsContent value="overview">Overview content</TabsContent>
          <TabsContent value="providers">Provider content</TabsContent>
        </Tabs>,
      );
    }

    it('renders tablist, tabs, and the active panel', () => {
      renderTabs();
      expect(screen.getByRole('tablist')).toBeTruthy();
      const tabs = screen.getAllByRole('tab');
      expect(tabs.length).toBe(2);
      expect(screen.getByText('Overview content')).toBeTruthy();
    });

    it('switches panels on pointer selection', () => {
      renderTabs();
      const providersTab = screen.getByRole('tab', { name: 'Providers' });
      // radix tabs select on mousedown (button 0), not on the click event
      fireEvent.mouseDown(providersTab);
      expect(screen.getByText('Provider content')).toBeTruthy();
      expect(screen.queryByText('Overview content')).toBeNull();
    });

    it('selects a focused tab via Enter key', () => {
      renderTabs();
      const providersTab = screen.getByRole('tab', { name: 'Providers' });
      providersTab.focus();
      fireEvent.keyDown(providersTab, { key: 'Enter' });
      expect(providersTab.getAttribute('aria-selected')).toBe('true');
      expect(screen.getByText('Provider content')).toBeTruthy();
    });

    it('moves selection to the next tab with ArrowRight', () => {
      vi.useFakeTimers();
      try {
        renderTabs();
        const first = screen.getByRole('tab', { name: 'Overview' });
        first.focus();
        fireEvent.keyDown(first, { key: 'ArrowRight' });
        // radix defers the focus move with setTimeout for reliability
        act(() => {
          vi.runAllTimers();
        });
        const second = screen.getByRole('tab', { name: 'Providers' });
        expect(second.getAttribute('aria-selected')).toBe('true');
        expect(screen.getByText('Provider content')).toBeTruthy();
      } finally {
        vi.useRealTimers();
      }
    });
  });
