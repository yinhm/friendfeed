import { cx } from 'class-variance-authority';
import type { cva, VariantProps } from 'class-variance-authority';
import type { ClassValue } from 'class-variance-authority/types';
import React, { forwardRef } from 'react';
import { twMerge } from 'tailwind-merge';

// Local replacement for @udecode/cn helpers that Plate 49 does not
// provide (cn/withProps/withCn/withVariants). withRef and
// createPrimitiveElement are imported from 'platejs/react' instead.

export function cn(...inputs: ClassValue[]) {
  return twMerge(cx(inputs));
}

export function withProps<T extends React.ElementType>(
  Component: T,
  defaultProps: Partial<React.ComponentPropsWithoutRef<T>>
) {
  return forwardRef<React.ComponentRef<T>, React.ComponentPropsWithoutRef<T>>(
    function ExtendComponent(props, ref) {
      const C = Component as React.ComponentType<any>;
      return (
        <C
          ref={ref}
          {...defaultProps}
          {...props}
          className={cn(
            (defaultProps as { className?: string }).className,
            (props as { className?: string }).className
          )}
        />
      );
    }
  );
}

export function withCn<T extends React.ElementType>(
  Component: T,
  ...inputs: ClassValue[]
) {
  return withProps(Component, {
    className: cn(inputs),
  } as unknown as Partial<React.ComponentPropsWithoutRef<T>>);
}

export function withVariants<
  T extends React.ElementType,
  V extends ReturnType<typeof cva>,
>(Component: T, variants: V, onlyVariantsProps?: (keyof VariantProps<V>)[]) {
  return forwardRef<
    React.ComponentRef<T>,
    React.ComponentPropsWithoutRef<T> & VariantProps<V>
  >(function ExtendComponent(allProps, ref) {
    const { className, ...props } = allProps as Record<string, any> & {
      className?: string;
    };
    const rest = { ...props };
    if (onlyVariantsProps) {
      onlyVariantsProps.forEach((key) => {
        if (props[key as string] !== undefined) {
          delete rest[key as string];
        }
      });
    }
    const C = Component as React.ComponentType<any>;
    return <C ref={ref} className={cn(variants(props), className)} {...rest} />;
  });
}
