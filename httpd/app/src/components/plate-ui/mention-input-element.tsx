import {
  type PlateElementProps,
  PlateElement,
  useFocused,
  useSelected,
} from 'platejs/react';

import { cn } from 'components/cn';

export function MentionInputElement({
  attributes,
  className,
  onClick,
  ...props
}: PlateElementProps & {
  onClick?: (mentionNode: any) => void;
}) {
  const { children, element } = props;

  const selected = useSelected();
  const focused = useFocused();

  return (
    <PlateElement
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
}
