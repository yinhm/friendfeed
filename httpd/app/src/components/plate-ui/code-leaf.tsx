'use client';

import React from 'react';
import { cn, withRef } from '@udecode/cn';
import { PlateLeaf } from 'platejs/react';

export const CodeLeaf = withRef<typeof PlateLeaf>(
  ({ className, children, ...props }, ref) => {
    return (
      <PlateLeaf
        ref={ref}
        as="code"
        className={cn(
          'whitespace-pre-wrap rounded-md bg-muted px-[0.3em] py-[0.2em] font-mono text-sm',
          className
        )}
        {...props}
      >
        {children}
      </PlateLeaf>
    );
  }
);
