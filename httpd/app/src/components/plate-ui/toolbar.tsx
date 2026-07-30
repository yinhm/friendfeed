'use client';

import * as React from 'react';

import * as ToolbarPrimitive from '@radix-ui/react-toolbar';
import { type VariantProps, cva } from 'class-variance-authority';

import { cn } from 'components/cn';
import { Icons } from 'components/icons';

import { Separator } from './separator';
import { withTooltip } from './tooltip';

export function Toolbar({
  className,
  ...props
}: React.ComponentProps<typeof ToolbarPrimitive.Root>) {
  return (
    <ToolbarPrimitive.Root
      className={cn('relative flex select-none items-center', className)}
      {...props}
    />
  );
}

export function ToolbarToggleGroup({
  className,
  ...props
}: React.ComponentProps<typeof ToolbarPrimitive.ToolbarToggleGroup>) {
  return (
    <ToolbarPrimitive.ToolbarToggleGroup
      className={cn('flex items-center', className)}
      {...props}
    />
  );
}

export function ToolbarLink({
  className,
  ...props
}: React.ComponentProps<typeof ToolbarPrimitive.Link>) {
  return (
    <ToolbarPrimitive.Link
      className={cn('font-medium underline underline-offset-4', className)}
      {...props}
    />
  );
}

export function ToolbarSeparator({
  className,
  ...props
}: React.ComponentProps<typeof ToolbarPrimitive.Separator>) {
  return (
    <ToolbarPrimitive.Separator
      className={cn('mx-2 my-1 w-px shrink-0 bg-border', className)}
      {...props}
    />
  );
}

const toolbarButtonVariants = cva(
  // aria-invalid rules from the official base are dropped: toolbar buttons
  // never carry an invalid form state, and the rules cost ~0.7 kB of CSS.
  "inline-flex cursor-pointer items-center justify-center gap-2 whitespace-nowrap rounded-md font-medium text-sm outline-none transition-[color,box-shadow] hover:bg-muted hover:text-muted-foreground focus-visible:border-ring focus-visible:ring-[3px] focus-visible:ring-ring/50 disabled:pointer-events-none disabled:opacity-50 aria-checked:bg-accent aria-checked:text-accent-foreground [&_svg:not([class*='size-'])]:size-4 [&_svg]:pointer-events-none [&_svg]:shrink-0",
  {
    defaultVariants: {
      size: 'default',
      variant: 'default',
    },
    variants: {
      size: {
        default: 'h-9 min-w-9 px-2',
        lg: 'h-10 min-w-10 px-2.5',
        sm: 'h-8 min-w-8 px-1.5',
      },
      variant: {
        default: 'bg-transparent',
        outline:
          'border border-input bg-transparent shadow-xs hover:bg-accent hover:text-accent-foreground',
      },
    },
  }
);

type ToolbarButtonProps = {
  isDropdown?: boolean;
  pressed?: boolean;
} & Omit<React.ComponentProps<typeof ToolbarToggleItem>, 'asChild' | 'value'> &
  VariantProps<typeof toolbarButtonVariants>;

export const ToolbarButton = withTooltip(function ToolbarButton({
  children,
  className,
  isDropdown,
  pressed,
  size = 'sm',
  variant,
  ...props
}: ToolbarButtonProps) {
  return typeof pressed === 'boolean' ? (
    <ToolbarToggleGroup disabled={props.disabled} type="single" value="single">
      <ToolbarToggleItem
        className={cn(
          toolbarButtonVariants({
            size,
            variant,
          }),
          isDropdown && 'justify-between gap-1 pr-1',
          className
        )}
        value={pressed ? 'single' : ''}
        {...props}
      >
        {isDropdown ? (
          <>
            <div className="flex flex-1 items-center gap-2 whitespace-nowrap">
              {children}
            </div>
            <div>
              <Icons.arrowDown
                className="size-3.5 text-muted-foreground"
                data-icon
              />
            </div>
          </>
        ) : (
          children
        )}
      </ToolbarToggleItem>
    </ToolbarToggleGroup>
  ) : (
    <ToolbarPrimitive.Button
      className={cn(
        toolbarButtonVariants({
          size,
          variant,
        }),
        isDropdown && 'pr-1',
        className
      )}
      {...props}
    >
      {children}
    </ToolbarPrimitive.Button>
  );
});

export function ToolbarToggleItem({
  className,
  size = 'sm',
  variant,
  ...props
}: React.ComponentProps<typeof ToolbarPrimitive.ToggleItem> &
  VariantProps<typeof toolbarButtonVariants>) {
  return (
    <ToolbarPrimitive.ToggleItem
      className={cn(toolbarButtonVariants({ size, variant }), className)}
      {...props}
    />
  );
}

export function ToolbarGroup({
  children,
  className,
}: React.ComponentProps<'div'>) {
  return (
    <div
      className={cn(
        'group/toolbar-group',
        'relative hidden has-[button]:flex',
        className
      )}
    >
      <div className="flex items-center">{children}</div>

      <div className="mx-1.5 py-0.5 group-last/toolbar-group:hidden!">
        <Separator orientation="vertical" />
      </div>
    </div>
  );
}
