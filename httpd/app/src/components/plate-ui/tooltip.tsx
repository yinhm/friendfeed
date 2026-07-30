'use client';

import * as React from 'react';

import * as TooltipPrimitive from '@radix-ui/react-tooltip';

import { cn } from 'components/cn';

export const TooltipProvider = TooltipPrimitive.Provider;
export const Tooltip = TooltipPrimitive.Root;
export const TooltipTrigger = TooltipPrimitive.Trigger;
export const TooltipPortal = TooltipPrimitive.Portal;

export function TooltipContent({
  className,
  sideOffset = 4,
  ...props
}: React.ComponentProps<typeof TooltipPrimitive.Content>) {
  return (
    <TooltipPrimitive.Content
      data-slot="tooltip-content"
      sideOffset={sideOffset}
      className={cn(
        'z-50 overflow-hidden rounded-md border bg-popover px-3 py-1.5 text-sm text-popover-foreground shadow-md',
        className
      )}
      {...props}
    />
  );
}

export function withTooltip<
  T extends React.ElementType,
>(Component: T) {
  return React.forwardRef<
    React.ComponentRef<T>,
    React.ComponentPropsWithoutRef<T> & {
      tooltip?: React.ReactNode;
      tooltipContentProps?: Omit<
        React.ComponentPropsWithoutRef<typeof TooltipPrimitive.Content>,
        'children'
      >;
      tooltipProps?: Omit<
        React.ComponentPropsWithoutRef<typeof TooltipPrimitive.Root>,
        'children'
      >;
    }
  >(function ExtendComponent(
    { tooltip, tooltipContentProps, tooltipProps, ...props },
    ref
  ) {
    const [mounted, setMounted] = React.useState(false);

    React.useEffect(() => {
      setMounted(true);
    }, []);

    const component = <Component ref={ref} {...(props as any)} />;

    if (tooltip && mounted) {
      return (
        <Tooltip {...tooltipProps}>
          <TooltipTrigger asChild>{component}</TooltipTrigger>

          <TooltipPortal>
            <TooltipContent {...tooltipContentProps}>{tooltip}</TooltipContent>
          </TooltipPortal>
        </Tooltip>
      );
    }

    return component;
  });
}
