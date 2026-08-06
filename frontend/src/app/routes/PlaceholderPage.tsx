import Page from '@/app/components/Page';

/**
 * Empty shell page for a route whose feature surface arrives in a later atom.
 * Keeps the single-h1 shell contract (a11y A16) with an honest "not available
 * yet" message and the page's document title driven by the shell.
 */
export default function PlaceholderPage({ title }: { title: string }) {
  return (
    <Page title={title} description="This view is not available yet. Check back soon." />
  );
}
