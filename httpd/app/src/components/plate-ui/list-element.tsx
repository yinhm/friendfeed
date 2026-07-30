import { type VariantProps, cva } from 'class-variance-authority';
import { type PlateElementProps, PlateElement } from 'platejs/react';

import { cn } from 'components/cn';

const listVariants = cva('m-0 ps-6', {
  variants: {
    variant: {
      ul: 'list-disc [&_ul]:list-[circle] [&_ul_ul]:list-[square]',
      ol: 'list-decimal',
    },
  },
});

export function ListElement({
  className,
  variant = 'ul',
  children,
  ...props
}: PlateElementProps & VariantProps<typeof listVariants>) {
  return (
    <PlateElement
      as={variant ?? 'ul'}
      className={cn(listVariants({ variant }), className)}
      {...props}
    >
      {children}
    </PlateElement>
  );
}
