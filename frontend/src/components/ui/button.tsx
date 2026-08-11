import { forwardRef } from 'react';
import type { ButtonHTMLAttributes } from 'react';
import { Slot } from '@radix-ui/react-slot';
import { cva } from 'class-variance-authority';
import type { VariantProps } from 'class-variance-authority';
import { cn } from '@/lib/utils';
import styles from './button.module.css';

const buttonVariants = cva(styles.button, {
  variants: {
    variant: {
      default: styles.default,
      outline: styles.outline,
      secondary: styles.secondary,
      ghost: styles.ghost,
      destructive: styles.destructive,
    },
    size: {
      default: '',
      sm: styles.sm,
      lg: styles.lg,
      icon: styles.icon,
    },
  },
  defaultVariants: {
    variant: 'default',
    size: 'default',
  },
});

export interface ButtonProps
  extends ButtonHTMLAttributes<HTMLButtonElement>,
    VariantProps<typeof buttonVariants> {
  asChild?: boolean;
}

/**
 * Primary action control (shadcn-compatible API). Supports variants, sizes,
 * and `asChild` rendering via Radix Slot; focus is always visible for
 * keyboard users and disabled state blocks activation.
 */
export const Button = forwardRef<HTMLButtonElement, ButtonProps>(
  ({ className, variant, size, asChild = false, type = 'button', ...props }, ref) => {
    const Comp = asChild ? Slot : 'button';
    return (
      <Comp
        className={cn(buttonVariants({ variant, size }), className)}
        ref={ref}
        {...(asChild ? {} : { type })}
        {...props}
      />
    );
  },
);
Button.displayName = 'Button';
