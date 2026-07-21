import React from 'react';
import { withRef } from 'platejs/react';
import { cn } from 'components/cn';
import { PlateLeaf } from 'platejs/react';

export const HighlightLeaf = withRef<typeof PlateLeaf>(
  ({ className, children, ...props }, ref) => (
    <PlateLeaf
      ref={ref}
      as="mark"
      className={cn('bg-primary/20 text-inherit dark:bg-primary/40', className)}
      {...props}
    >
      {children}
    </PlateLeaf>
  )
);
