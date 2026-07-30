'use client';

import { type PlateElementProps, PlateElement } from 'platejs/react';

import { cn } from 'components/cn';

export function BlockquoteElement({
  className,
  children,
  ...props
}: PlateElementProps) {
  return (
    <PlateElement
      as="blockquote"
      className={cn('my-1 border-l-2 pl-6 italic', className)}
      {...props}
    >
      {children}
    </PlateElement>
  );
}
