import { type PlateLeafProps, PlateLeaf } from 'platejs/react';

import { cn } from 'components/cn';

export function HighlightLeaf({
  className,
  children,
  ...props
}: PlateLeafProps) {
  // No background override: keep the browser-default <mark> style so the
  // editor matches the display page.
  return (
    <PlateLeaf as="mark" className={cn(className)} {...props}>
      {children}
    </PlateLeaf>
  );
}
