import {
  type PlateElementProps,
  PlateElement,
  useFocused,
  useSelected,
} from 'platejs/react';

import { cn } from 'components/cn';

export function HrElement({ className, ...props }: PlateElementProps) {
  const { children } = props;

  const selected = useSelected();
  const focused = useFocused();

  return (
    <PlateElement {...props}>
      <div className="py-6" contentEditable={false}>
        <hr
          className={cn(
            'h-0.5 cursor-pointer rounded-sm border-none bg-muted bg-clip-content',
            selected && focused && 'ring-2 ring-ring ring-offset-2',
            className
          )}
        />
      </div>
      {children}
    </PlateElement>
  );
}
