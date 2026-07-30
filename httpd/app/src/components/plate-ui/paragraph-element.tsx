import { type PlateElementProps, PlateElement } from 'platejs/react';

import { cn } from 'components/cn';

export function ParagraphElement({ className, ...props }: PlateElementProps) {
  return (
    <PlateElement className={cn('m-0 px-0 py-1', className)} {...props} />
  );
}
