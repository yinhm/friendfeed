import { getLinkAttributes } from '@platejs/link';
import type { TLinkElement } from 'platejs';
import { type PlateElementProps, PlateElement } from 'platejs/react';

import { cn } from 'components/cn';

export function LinkElement(props: PlateElementProps<TLinkElement>) {
  return (
    <PlateElement
      {...props}
      as="a"
      className={cn(
        'font-medium text-primary underline decoration-primary underline-offset-4',
        props.className
      )}
      attributes={{
        ...props.attributes,
        ...getLinkAttributes(props.editor, props.element),
        // quick fix: hovering <a> with href loses the editor focus
        onMouseOver: (e) => {
          e.stopPropagation();
        },
      }}
    >
      {props.children}
    </PlateElement>
  );
}
