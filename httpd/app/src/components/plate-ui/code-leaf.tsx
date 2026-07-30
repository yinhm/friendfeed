'use client';

import { type PlateLeafProps, PlateLeaf } from 'platejs/react';

import { cn } from 'components/cn';

export function CodeLeaf({ className, children, ...props }: PlateLeafProps) {
  return (
    <PlateLeaf
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
