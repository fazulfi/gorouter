import { forwardRef } from 'react';
import type { ComponentPropsWithoutRef, ElementRef } from 'react';
import * as TabsPrimitive from '@radix-ui/react-tabs';
import { cn } from '@/lib/utils';
import styles from './tabs.module.css';

/**
 * Accessible tabs built on Radix Tabs: roving tabindex, arrow-key
 * navigation, and proper tab/tablist/tabpanel roles come from the
 * primitive; styling follows the repository design tokens.
 */
export const Tabs = TabsPrimitive.Root;

export const TabsList = forwardRef<
  ElementRef<typeof TabsPrimitive.List>,
  ComponentPropsWithoutRef<typeof TabsPrimitive.List>
>(({ className, ...props }, ref) => (
  <TabsPrimitive.List ref={ref} className={cn(styles.list, className)} {...props} />
));
TabsList.displayName = TabsPrimitive.List.displayName;

export const TabsTrigger = forwardRef<
  ElementRef<typeof TabsPrimitive.Trigger>,
  ComponentPropsWithoutRef<typeof TabsPrimitive.Trigger>
>(({ className, ...props }, ref) => (
  <TabsPrimitive.Trigger ref={ref} className={cn(styles.trigger, className)} {...props} />
));
TabsTrigger.displayName = TabsPrimitive.Trigger.displayName;

export const TabsContent = forwardRef<
  ElementRef<typeof TabsPrimitive.Content>,
  ComponentPropsWithoutRef<typeof TabsPrimitive.Content>
>(({ className, ...props }, ref) => (
  <TabsPrimitive.Content ref={ref} className={cn(styles.content, className)} {...props} />
));
TabsContent.displayName = TabsPrimitive.Content.displayName;
