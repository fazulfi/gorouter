import type { ReactNode } from 'react';
import styles from './Page.module.css';

/**
 * Feature placeholder root. Renders exactly one <h1> (a11y A16) so heading
 * order matches the visual order of the shell. Feature atoms replace the body
 * content but keep this single-h1 contract.
 */
export default function Page({
  title,
  description,
  children,
}: {
  title: string;
  description?: string;
  children?: ReactNode;
}) {
  return (
    <section className={styles.page}>
      <h1 className={styles.title}>{title}</h1>
      {description && <p className={styles.description}>{description}</p>}
      {children}
    </section>
  );
}
