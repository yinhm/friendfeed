import {
  useTodoListElement,
  useTodoListElementState,
} from '@platejs/list-classic/react';
import { type PlateElementProps, PlateElement } from 'platejs/react';

import { cn } from 'components/cn';

import { Checkbox } from './checkbox';

export function TodoListElement({
  className,
  children,
  ...props
}: PlateElementProps) {
  const { element } = props;
  const state = useTodoListElementState({ element });
  const { checkboxProps } = useTodoListElement(state);

  return (
    <PlateElement className={cn('flex flex-row py-1', className)} {...props}>
      <div
        className="mr-1.5 flex select-none items-center justify-center"
        contentEditable={false}
      >
        <Checkbox {...checkboxProps} />
      </div>
      <span
        className={cn(
          'flex-1 focus:outline-none',
          state.checked && 'text-muted-foreground line-through'
        )}
        contentEditable={!state.readOnly}
        suppressContentEditableWarning
      >
        {children}
      </span>
    </PlateElement>
  );
}
