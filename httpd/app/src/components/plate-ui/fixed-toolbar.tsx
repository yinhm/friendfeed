import * as React from 'react';

import { cn } from 'components/cn';

import { Toolbar } from './toolbar';

export function FixedToolbar({
  className,
  ...props
}: React.ComponentProps<typeof Toolbar>) {
  return (
    <Toolbar
      className={cn(
        'supports-backdrop-blur:bg-background/60 sticky left-0 top-[57px] z-50 w-full justify-between overflow-x-auto rounded-t-lg border-b border-b-border bg-background/95 backdrop-blur',
        className
      )}
      {...props}
    />
  );
}
