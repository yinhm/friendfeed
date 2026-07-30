import { useLink } from '@platejs/link/react';
import type { TLinkElement } from 'platejs';
import { type PlateElementProps, PlateElement, useElement } from 'platejs/react';

import { cn } from 'components/cn';

export function LinkElement({
  className,
  children,
  ...props
}: PlateElementProps) {
  const element = useElement<TLinkElement>();
  const { props: linkProps } = useLink({ element });

  return (
    <PlateElement
      asChild
      className={cn(
        'font-medium text-primary underline decoration-primary underline-offset-4',
        className
      )}
      {...(linkProps as any)}
      {...props}
    >
      <a>{children}</a>
    </PlateElement>
  );
}
