import React from 'react';
import { withRef } from 'platejs/react';
import { cn } from 'components/cn';
import { PlateLeaf } from 'platejs/react';

export const HighlightLeaf = withRef<typeof PlateLeaf>(
  ({ className, children, ...props }, ref) => (
    // No background override: keep the browser-default <mark> style so the
    // editor matches the display page.
    <PlateLeaf
      ref={ref}
      as="mark"
      className={cn(className)}
      {...props}
    >
      {children}
    </PlateLeaf>
  )
);
