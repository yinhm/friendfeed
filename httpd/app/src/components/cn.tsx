import { cx } from 'class-variance-authority';
import type { ClassValue } from 'class-variance-authority/types';
import React, { forwardRef } from 'react';
import { twMerge } from 'tailwind-merge';

// Local replacement for @udecode/cn helpers that Plate 49 does not
// provide. Components are plain function components in the current
// Plate UI registry style; `cn` merges classes and `withProps` attaches
// default props (used by plate-plugins.ts for PlateLeaf marks).

export function cn(...inputs: ClassValue[]) {
  return twMerge(cx(inputs));
}

export function withProps<T extends React.ElementType>(
  Component: T,
  defaultProps: Partial<React.ComponentPropsWithoutRef<T>>
) {
  return forwardRef<React.ComponentRef<T>, React.ComponentPropsWithoutRef<T>>(
    function ExtendComponent(props, ref) {
      const C = Component as React.ComponentType<any>;
      return (
        <C
          ref={ref}
          {...defaultProps}
          {...props}
          className={cn(
            (defaultProps as { className?: string }).className,
            (props as { className?: string }).className
          )}
        />
      );
    }
  );
}
