import * as React from 'react';

import { type VariantProps, cva } from 'class-variance-authority';
import { type PlateContentProps, PlateContent } from 'platejs/react';

import { cn } from 'components/cn';

const editorVariants = cva(
  cn(
    'relative overflow-x-auto whitespace-pre-wrap break-words',
    'min-h-[80px] w-full rounded-md bg-background px-3 py-2 text-sm ring-offset-background placeholder:text-muted-foreground focus-visible:outline-none',
    '[&_[data-slate-placeholder]]:text-muted-foreground [&_[data-slate-placeholder]]:!opacity-100',
    '[&_[data-slate-placeholder]]:top-[auto_!important]',
    '[&_strong]:font-bold'
  ),
  {
    variants: {
      variant: {
        outline: 'border border-input',
        ghost: '',
      },
      focused: {
        true: 'ring-2 ring-ring ring-offset-2',
      },
      disabled: {
        true: 'cursor-not-allowed opacity-50',
      },
      focusRing: {
        true: 'focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2',
        false: '',
      },
      size: {
        sm: 'text-sm',
        md: 'text-base',
      },
    },
    defaultVariants: {
      variant: 'outline',
      focusRing: true,
      size: 'sm',
    },
  }
);

export type EditorProps = PlateContentProps &
  VariantProps<typeof editorVariants>;

export function Editor({
  className,
  disabled,
  focused,
  focusRing,
  readOnly,
  size,
  variant,
  ref,
  ...props
}: EditorProps & { ref?: React.Ref<HTMLDivElement> }) {
  return (
    <div ref={ref} className="relative w-full" data-slot="editor-container">
      <PlateContent
        data-slot="editor"
        className={cn(
          editorVariants({
            disabled,
            focused,
            focusRing,
            size,
            variant,
          }),
          className
        )}
        disableDefaultStyles
        readOnly={disabled ?? readOnly}
        aria-disabled={disabled}
        {...props}
      />
    </div>
  );
}
