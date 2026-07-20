'use client';

import React from 'react';
import { cn, withRef } from '@udecode/cn';
import { PlateElement } from 'platejs/react';

export const BlockquoteElement = withRef<typeof PlateElement>(
  ({ className, children, ...props }, ref) => {
    return (
      <PlateElement
        ref={ref}
        as="blockquote"
        className={cn('my-1 border-l-2 pl-6 italic', className)}
        {...props}
      >
        {children}
      </PlateElement>
    );
  }
);
