import React from 'react';
import { cn, withRef } from '@udecode/cn';
import { PlateElement } from 'platejs/react';
import { useFocused, useSelected } from 'slate-react';

export const MentionInputElement = withRef<
  typeof PlateElement,
  {
    onClick?: (mentionNode: any) => void;
  }
>(({ attributes, className, onClick, ...props }, ref) => {
  const { children, element } = props;

  const selected = useSelected();
  const focused = useFocused();

  return (
    <PlateElement
      ref={ref}
      as="span"
      data-slate-value={element.value}
      attributes={{
        ...attributes,
        onClick: () => onClick?.(element),
      }}
      className={cn(
        'inline-block rounded-md bg-muted px-1.5 py-0.5 align-baseline text-sm',
        selected && focused && 'ring-2 ring-ring',
        className
      )}
      {...props}
    >
      {children}
    </PlateElement>
  );
});
